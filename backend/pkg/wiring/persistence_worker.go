package wiring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"backend/pkg/block"
	"backend/pkg/ingest/kafka"
	"backend/pkg/state"
	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"
	"github.com/google/uuid"
)

const maxCommitAnomalyIDs = 10000

// PersistenceTask represents a block persistence task
type PersistenceTask struct {
	Block     *block.AppBlock
	Receipts  []state.Receipt
	StateRoot [32]byte
	Attempt   int
}

// PersistenceSuccessCallback is called after successful block persistence
// Used to trigger downstream actions like Kafka publishing
type PersistenceSuccessCallback func(ctx context.Context, height uint64, hash [32]byte, stateRoot [32]byte, txCount int, ts int64, anomalyCount int, evidenceCount int, policyCount int, anomalyIDs []string)

// PersistenceWorkerConfig holds configuration for the persistence worker
type PersistenceWorkerConfig struct {
	QueueSize       int                        // Bounded queue size (default: 1000)
	RetryMax        int                        // Maximum retry attempts (default: 3)
	RetryBackoffMS  int                        // Initial backoff in milliseconds (default: 100)
	MaxBackoffMS    int                        // Maximum backoff in milliseconds (default: 5000)
	WorkerCount     int                        // Number of concurrent workers (default: 1)
	ShutdownTimeout time.Duration              // Graceful shutdown timeout (default: 30s)
	OnSuccess       PersistenceSuccessCallback // Optional callback after successful persistence (for Kafka, etc.)
}

// PersistenceWorker handles async block persistence with retry logic
type PersistenceWorker struct {
	cfg         PersistenceWorkerConfig
	adapter     cockroach.Adapter
	logger      *utils.Logger
	auditLogger interface {
		Info(event string, fields map[string]interface{}) error
		Warn(event string, fields map[string]interface{}) error
		Error(event string, fields map[string]interface{}) error
		Security(event string, fields map[string]interface{}) error
	}

	queue      chan *PersistenceTask
	stopCh     chan struct{}
	wg         sync.WaitGroup
	mu         sync.RWMutex
	running    bool
	taskCount  uint64                     // Total tasks received
	errorCount uint64                     // Total errors
	onSuccess  PersistenceSuccessCallback // Callback after successful persistence
}

// NewPersistenceWorker creates a new async persistence worker
func NewPersistenceWorker(cfg PersistenceWorkerConfig, adapter cockroach.Adapter, logger *utils.Logger, auditLogger interface {
	Info(event string, fields map[string]interface{}) error
	Warn(event string, fields map[string]interface{}) error
	Error(event string, fields map[string]interface{}) error
	Security(event string, fields map[string]interface{}) error
}) (*PersistenceWorker, error) {
	if adapter == nil {
		return nil, errors.New("persistence: adapter is required")
	}

	// Apply defaults
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1000
	}
	if cfg.RetryMax <= 0 {
		cfg.RetryMax = 3
	}
	if cfg.RetryBackoffMS <= 0 {
		cfg.RetryBackoffMS = 100
	}
	if cfg.MaxBackoffMS <= 0 {
		cfg.MaxBackoffMS = 5000
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 1
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 30 * time.Second
	}

	pw := &PersistenceWorker{
		cfg:         cfg,
		adapter:     adapter,
		logger:      logger,
		auditLogger: auditLogger,
		queue:       make(chan *PersistenceTask, cfg.QueueSize),
		stopCh:      make(chan struct{}),
		running:     false,
		onSuccess:   cfg.OnSuccess, // Store callback
	}

	return pw, nil
}

// Start starts the persistence workers
func (pw *PersistenceWorker) Start(ctx context.Context) error {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if pw.running {
		return errors.New("persistence: worker already running")
	}

	pw.running = true

	// Start worker goroutines
	for i := 0; i < pw.cfg.WorkerCount; i++ {
		pw.wg.Add(1)
		go pw.workerLoop(ctx, i)
	}

	if pw.logger != nil {
		pw.logger.InfoContext(ctx, "persistence worker started",
			utils.ZapInt("workers", pw.cfg.WorkerCount),
			utils.ZapInt("queue_size", pw.cfg.QueueSize))
	}

	return nil
}

// Stop gracefully stops the persistence worker
func (pw *PersistenceWorker) Stop() error {
	pw.mu.Lock()
	if !pw.running {
		pw.mu.Unlock()
		return nil
	}
	pw.running = false
	pw.mu.Unlock()

	// Signal stop
	close(pw.stopCh)

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		pw.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if pw.logger != nil {
			pw.logger.Info("persistence worker stopped gracefully")
		}
		return nil
	case <-time.After(pw.cfg.ShutdownTimeout):
		if pw.logger != nil {
			pw.logger.Warn("persistence worker shutdown timeout",
				utils.ZapDuration("timeout", pw.cfg.ShutdownTimeout))
		}
		return errors.New("persistence: shutdown timeout")
	}
}

// Enqueue adds a persistence task to the queue (non-blocking with timeout)
func (pw *PersistenceWorker) Enqueue(ctx context.Context, task *PersistenceTask) error {
	pw.mu.RLock()
	if !pw.running {
		pw.mu.RUnlock()
		return errors.New("persistence: worker not running")
	}
	pw.mu.RUnlock()

	// Try to enqueue with context timeout
	select {
	case pw.queue <- task:
		pw.incrementTaskCount()
		return nil
	case <-ctx.Done():
		return fmt.Errorf("persistence: enqueue timeout: %w", ctx.Err())
	case <-pw.stopCh:
		return errors.New("persistence: worker stopped")
	}
}

// workerLoop processes persistence tasks with retry logic
func (pw *PersistenceWorker) workerLoop(ctx context.Context, workerID int) {
	defer pw.wg.Done()

	if pw.logger != nil {
		pw.logger.InfoContext(ctx, "persistence worker started",
			utils.ZapInt("worker_id", workerID))
	}

	for {
		select {
		case <-pw.stopCh:
			if pw.logger != nil {
				pw.logger.InfoContext(ctx, "persistence worker draining queue before stop",
					utils.ZapInt("worker_id", workerID))
			}
			// Best-effort drain without blocking
			drainStart := time.Now()
			drained := 0
			for {
				select {
				case task := <-pw.queue:
					if task == nil {
						if pw.logger != nil {
							pw.logger.WarnContext(ctx, "persistence worker queue closed during drain",
								utils.ZapInt("worker_id", workerID))
						}
						return
					}
					pw.processTask(ctx, task)
					drained++
				default:
					if pw.logger != nil {
						pw.logger.InfoContext(ctx, "persistence worker drained queue",
							utils.ZapInt("worker_id", workerID),
							utils.ZapInt("drained", drained),
							utils.ZapDuration("duration", time.Since(drainStart)))
					}
					return
				}
			}

		case task := <-pw.queue:
			pw.processTask(ctx, task)
		}
	}
}

// processTask processes a single persistence task with exponential backoff retry
func (pw *PersistenceWorker) processTask(ctx context.Context, task *PersistenceTask) {
	if task == nil {
		return
	}

	backoff := time.Duration(pw.cfg.RetryBackoffMS) * time.Millisecond
	maxBackoff := time.Duration(pw.cfg.MaxBackoffMS) * time.Millisecond

	for attempt := task.Attempt; attempt < pw.cfg.RetryMax; attempt++ {
		// Create timeout context for this attempt
		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)

		err := pw.adapter.PersistBlock(attemptCtx, task.Block, task.Receipts, task.StateRoot)
		cancel()

		if err == nil {
			// Success - persistence completed
			bh := task.Block.GetHash()
			if pw.onSuccess != nil || pw.auditLogger != nil || pw.logger != nil {
				go func() {
					defer func() {
						if r := recover(); r != nil {
							if pw.logger != nil {
								pw.logger.ErrorContext(ctx, "persistence success callback panic",
									utils.ZapAny("panic", r),
									utils.ZapUint64("height", task.Block.GetHeight()))
							}
						}
					}()

					meta := extractCommitMetadata(task.Block, pw.logger)

					if pw.auditLogger != nil {
						_ = pw.auditLogger.Info("block_persisted_async", map[string]interface{}{
							"height":            task.Block.GetHeight(),
							"hash":              fmt.Sprintf("%x", bh[:8]),
							"attempts":          attempt + 1,
							"state_root":        fmt.Sprintf("%x", task.StateRoot[:8]),
							"anomaly_count":     meta.anomalyCount,
							"evidence_count":    meta.evidenceCount,
							"policy_count":      meta.policyCount,
							"tracked_anomalies": len(meta.anomalyIDs),
						})
					}

					if len(meta.anomalyIDs) == 0 && pw.logger != nil {
						pw.logger.Debug("no anomaly ids extracted for commit metadata",
							utils.ZapUint64("height", task.Block.GetHeight()),
							utils.ZapInt("anomaly_count", meta.anomalyCount))
					}

					if pw.onSuccess == nil {
						return
					}

					pw.onSuccess(ctx, task.Block.GetHeight(), bh, task.StateRoot, task.Block.GetTransactionCount(), task.Block.GetTimestamp().Unix(), meta.anomalyCount, meta.evidenceCount, meta.policyCount, meta.anomalyIDs)
				}()
			}

			if pw.auditLogger != nil && pw.onSuccess == nil {
				_ = pw.auditLogger.Info("block_persisted_async", map[string]interface{}{
					"height":     task.Block.GetHeight(),
					"hash":       fmt.Sprintf("%x", bh[:8]),
					"attempts":   attempt + 1,
					"state_root": fmt.Sprintf("%x", task.StateRoot[:8]),
				})
			}

			return
		}

		// Check if error is integrity violation (fail-closed, no retry)
		if errors.Is(err, cockroach.ErrIntegrityViolation) {
			pw.incrementErrorCount()
			if pw.auditLogger != nil {
				bh := task.Block.GetHash()
				_ = pw.auditLogger.Security("block_persist_integrity_violation", map[string]interface{}{
					"height":  task.Block.GetHeight(),
					"hash":    fmt.Sprintf("%x", bh[:8]),
					"error":   err.Error(),
					"attempt": attempt + 1,
				})
			}
			if pw.logger != nil {
				pw.logger.ErrorContext(ctx, "CRITICAL: integrity violation during persistence",
					utils.ZapError(err),
					utils.ZapUint64("height", task.Block.GetHeight()))
			}
			// FAIL-CLOSED: Integrity violations are not retried
			return
		}

		// Transient error, retry with backoff
		if attempt < pw.cfg.RetryMax-1 {
			if pw.logger != nil {
				pw.logger.WarnContext(ctx, "persistence failed, retrying",
					utils.ZapError(err),
					utils.ZapUint64("height", task.Block.GetHeight()),
					utils.ZapInt("attempt", attempt+1),
					utils.ZapDuration("backoff", backoff))
			}

			// Sleep with backoff
			time.Sleep(backoff)

			// Exponential backoff
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		} else {
			// Final attempt failed
			pw.incrementErrorCount()
			if pw.auditLogger != nil {
				bh := task.Block.GetHash()
				_ = pw.auditLogger.Error("block_persist_failed_final", map[string]interface{}{
					"height":   task.Block.GetHeight(),
					"hash":     fmt.Sprintf("%x", bh[:8]),
					"error":    err.Error(),
					"attempts": pw.cfg.RetryMax,
				})
			}
			if pw.logger != nil {
				pw.logger.ErrorContext(ctx, "CRITICAL: block persistence failed after retries",
					utils.ZapError(err),
					utils.ZapUint64("height", task.Block.GetHeight()),
					utils.ZapInt("attempts", pw.cfg.RetryMax))
			}
			return
		}
	}
}

// incrementTaskCount atomically increments task counter
func (pw *PersistenceWorker) incrementTaskCount() {
	pw.mu.Lock()
	pw.taskCount++
	pw.mu.Unlock()
}

// incrementErrorCount atomically increments error counter
func (pw *PersistenceWorker) incrementErrorCount() {
	pw.mu.Lock()
	pw.errorCount++
	pw.mu.Unlock()
}

// GetStats returns persistence worker statistics
func (pw *PersistenceWorker) GetStats() map[string]interface{} {
	pw.mu.RLock()
	defer pw.mu.RUnlock()

	return map[string]interface{}{
		"running":     pw.running,
		"queue_size":  len(pw.queue),
		"queue_cap":   cap(pw.queue),
		"task_count":  pw.taskCount,
		"error_count": pw.errorCount,
		"workers":     pw.cfg.WorkerCount,
	}
}

// extractAnomalyIDs extracts anomaly IDs from EventTx transactions in a block
// Fix: Gap 2 - Enable COMMITTED state tracking by extracting anomaly UUIDs
type commitMetadata struct {
	anomalyIDs    []string
	anomalyCount  int
	evidenceCount int
	policyCount   int
}

func extractCommitMetadata(block *block.AppBlock, logger *utils.Logger) commitMetadata {
	if block == nil {
		return commitMetadata{}
	}

	txs := block.Transactions()
	if len(txs) == 0 {
		return commitMetadata{}
	}

	meta := commitMetadata{}
	limitReached := false

	for _, tx := range txs {
		switch tx.Type() {
		case state.TxEvent:
			meta.anomalyCount++

			eventTx, ok := tx.(*state.EventTx)
			if !ok {
				continue
			}

			anomalyID, usedFallback := extractAnomalyID(eventTx.Data, logger)
			if anomalyID == "" {
				continue
			}

			if len(meta.anomalyIDs) >= maxCommitAnomalyIDs {
				if !limitReached && logger != nil {
					logger.Warn("anomaly id limit reached for commit metadata",
						utils.ZapInt("limit", maxCommitAnomalyIDs))
				}
				limitReached = true
				continue
			}

			if !isValidUUIDv4(anomalyID) {
				if logger != nil {
					logger.Warn("invalid anomaly uuid dropped",
						utils.ZapString("id", anomalyID))
				}
				continue
			}

			if usedFallback && logger != nil {
				logger.Debug("anomaly id resolved via payload fallback")
			}

			meta.anomalyIDs = append(meta.anomalyIDs, anomalyID)
		case state.TxEvidence:
			meta.evidenceCount++
		case state.TxPolicy:
			meta.policyCount++
		}
	}

	return meta
}

func extractAnomalyID(data []byte, logger *utils.Logger) (string, bool) {
	msg, err := kafka.DecodeAnomalyMsg(data)
	if err == nil {
		anomalyID, usedFallback, resolveErr := resolveAnomalyID(msg)
		if resolveErr != nil {
			if logger != nil {
				logger.Debug("unable to resolve anomaly id for commit metadata",
					utils.ZapError(resolveErr))
			}
			return "", false
		}
		return anomalyID, usedFallback
	}

	if logger != nil {
		logger.Debug("failed to decode anomaly for commit metadata",
			utils.ZapError(err))
	}

	id, jsonErr := extractAnomalyIDFromJSON(data)
	if jsonErr != nil {
		if logger != nil {
			logger.Debug("unable to extract anomaly id from json payload",
				utils.ZapError(jsonErr))
		}
		return "", false
	}

	return id, true
}

func extractAnomalyIDFromJSON(data []byte) (string, error) {
	var payload struct {
		AnomalyID string `json:"anomaly_id"`
		ID        string `json:"id"`
	}

	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}

	if payload.AnomalyID != "" {
		return payload.AnomalyID, nil
	}
	if payload.ID != "" {
		return payload.ID, nil
	}

	return "", fmt.Errorf("anomaly id missing from payload")
}

func isValidUUIDv4(id string) bool {
	u, err := uuid.Parse(id)
	if err != nil {
		return false
	}
	return u.Version() == 4
}

// resolveAnomalyID attempts to obtain the anomaly UUID from the decoded message.
// Primary source is the protobuf field, with a JSON payload fallback for legacy messages.
func resolveAnomalyID(msg *kafka.AnomalyMsg) (string, bool, error) {
	if msg == nil {
		return "", false, fmt.Errorf("nil anomaly message")
	}

	if msg.ID != "" {
		return msg.ID, false, nil
	}

	if len(msg.Payload) == 0 {
		return "", false, fmt.Errorf("empty anomaly payload")
	}

	var payload struct {
		AnomalyID string `json:"anomaly_id"`
	}

	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return "", false, fmt.Errorf("payload decode failed: %w", err)
	}

	if payload.AnomalyID == "" {
		return "", true, fmt.Errorf("payload missing anomaly_id field")
	}

	return payload.AnomalyID, true, nil
}
