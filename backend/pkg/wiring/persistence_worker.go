package wiring

import (
	"backend/pkg/consensus/messages"
	ctypes "backend/pkg/consensus/types"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fxamacker/cbor/v2"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/block"
	"backend/pkg/ingest/kafka"
	"backend/pkg/state"
	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"
	"github.com/google/uuid"
)

const (
	maxCommitAnomalyIDs = 10000
	maxAttemptTimeout   = 120 * time.Second
)

// PersistenceTask represents a block persistence task
type PersistenceTask struct {
	Block     *block.AppBlock
	Receipts  []state.Receipt
	StateRoot [32]byte
	QCData    []byte
	Attempt   int
}

type persistenceTaskKey struct {
	Height uint64
	Hash   [32]byte
}

func encodeCommittedQC(qc ctypes.QC) ([]byte, error) {
	if qc == nil {
		return nil, nil
	}
	encMode, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		return nil, fmt.Errorf("init qc encoder: %w", err)
	}
	msgQC := &messages.QC{
		View:         qc.GetView(),
		Height:       qc.GetHeight(),
		Round:        0,
		BlockHash:    qc.GetBlockHash(),
		Signatures:   append([]ctypes.Signature(nil), qc.GetSignatures()...),
		Timestamp:    qc.GetTimestamp(),
		AggregatorID: ctypes.ValidatorID{},
	}
	data, err := encMode.Marshal(msgQC)
	if err != nil {
		return nil, fmt.Errorf("marshal qc: %w", err)
	}
	return data, nil
}

// PersistenceSuccessCallback is called after successful block persistence
// Used to trigger downstream actions like Kafka publishing
type PersistenceSuccessCallback func(ctx context.Context, height uint64, hash [32]byte, stateRoot [32]byte, txCount int, ts int64, anomalyCount int, evidenceCount int, policyCount int, anomalyIDs []string, policyPayloads [][]byte)

// PersistencePersistedCallback is called immediately after durable block persistence succeeds.
// It is intended for low-latency wake signals (for example, outbox dispatcher notify).
type PersistencePersistedCallback func(ctx context.Context, height uint64, txCount int)

// PersistenceWorkerConfig holds configuration for the persistence worker
type PersistenceWorkerConfig struct {
	QueueSize       int                        // Bounded queue size (default: 1000)
	RetryMax        int                        // Maximum retry attempts (default: 3)
	RetryBackoffMS  int                        // Initial backoff in milliseconds (default: 100)
	MaxBackoffMS    int                        // Maximum backoff in milliseconds (default: 5000)
	AttemptTimeout  time.Duration              // Per-attempt DB persistence timeout (default: 25s)
	WorkerCount     int                        // Number of concurrent workers (default: 1)
	ShutdownTimeout time.Duration              // Graceful shutdown timeout (default: 30s)
	OnSuccess       PersistenceSuccessCallback // Optional callback after successful persistence (for Kafka, etc.)
	OnPersisted     PersistencePersistedCallback
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

	queue           chan *PersistenceTask
	stopCh          chan struct{}
	wg              sync.WaitGroup
	mu              sync.RWMutex
	running         bool
	taskCount       uint64                     // Total tasks received
	errorCount      uint64                     // Total errors
	deduped         uint64                     // Duplicate enqueue requests coalesced
	onSuccess       PersistenceSuccessCallback // Callback after successful persistence
	onPersisted     PersistencePersistedCallback
	inflight        map[persistenceTaskKey]struct{}
	enqueueInFlight atomic.Int64

	writerMu                    sync.RWMutex
	writerProposerOnly          bool
	writerNodeID                ctypes.ValidatorID
	writerPolicyConfigured      bool
	executeProposer             atomic.Uint64
	executeNonProposer          atomic.Uint64
	executeDroppedNonOwnerTotal atomic.Uint64
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
	if cfg.AttemptTimeout <= 0 {
		cfg.AttemptTimeout = 25 * time.Second
	}
	if cfg.AttemptTimeout > maxAttemptTimeout {
		cfg.AttemptTimeout = maxAttemptTimeout
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
		onPersisted: cfg.OnPersisted,
		inflight:    make(map[persistenceTaskKey]struct{}),
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
	pw.mu.Lock()
	if !pw.running {
		pw.mu.Unlock()
		return errors.New("persistence: worker not running")
	}
	key, hasKey := task.persistenceKey()
	if hasKey {
		if _, exists := pw.inflight[key]; exists {
			pw.deduped++
			pw.mu.Unlock()
			return nil
		}
		pw.inflight[key] = struct{}{}
	}
	pw.enqueueInFlight.Add(1)
	pw.mu.Unlock()
	defer pw.enqueueInFlight.Add(-1)

	// Try to enqueue with context timeout
	select {
	case pw.queue <- task:
		pw.incrementTaskCount()
		return nil
	case <-ctx.Done():
		if hasKey {
			pw.releaseInflightKey(key)
		}
		return fmt.Errorf("persistence: enqueue timeout: %w", ctx.Err())
	case <-pw.stopCh:
		if hasKey {
			pw.releaseInflightKey(key)
		}
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
					if pw.enqueueInFlight.Load() > 0 {
						time.Sleep(1 * time.Millisecond)
						continue
					}
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
	if key, ok := task.persistenceKey(); ok {
		defer pw.releaseInflightKey(key)
	}
	allowed, role, reason := pw.shouldExecuteTask(task)
	if !allowed {
		pw.executeDroppedNonOwnerTotal.Add(1)
		if pw.auditLogger != nil && task.Block != nil {
			_ = pw.auditLogger.Security("persistence_worker_dropped_non_owner_task", map[string]interface{}{
				"height": task.Block.GetHeight(),
				"role":   role,
				"reason": reason,
			})
		}
		if pw.logger != nil && task.Block != nil {
			pw.logger.WarnContext(ctx, "dropping persistence task due to writer ownership guard",
				utils.ZapUint64("height", task.Block.GetHeight()),
				utils.ZapString("role", role),
				utils.ZapString("reason", reason))
		}
		return
	}
	switch role {
	case "proposer":
		pw.executeProposer.Add(1)
	case "non_proposer":
		pw.executeNonProposer.Add(1)
	}

	backoff := time.Duration(pw.cfg.RetryBackoffMS) * time.Millisecond
	maxBackoff := time.Duration(pw.cfg.MaxBackoffMS) * time.Millisecond

	for attempt := task.Attempt; attempt < pw.cfg.RetryMax; attempt++ {
		// Create timeout context for this attempt. Timeout scales with block size
		// to reduce false deadline failures before outbox materialization on burst blocks.
		attemptCtx, cancel := context.WithTimeout(ctx, pw.effectiveAttemptTimeout(task, attempt))
		attemptCtx = cockroach.WithPersistAttempt(attemptCtx, attempt+1)

		err := pw.adapter.PersistBlock(attemptCtx, task.Block, task.Receipts, task.StateRoot)
		cancel()

		if err == nil {
			// Success - persistence completed
			bh := task.Block.GetHash()

			// Update consensus restart metadata only after durable block persistence.
			// This prevents consensus_metadata from advancing ahead of the blocks table.
			metaCtx, metaCancel := context.WithTimeout(ctx, 5*time.Second)
			if merr := pw.adapter.SaveCommittedBlock(metaCtx, task.Block.GetHeight(), bh[:], task.QCData); merr != nil {
				if pw.logger != nil {
					pw.logger.WarnContext(ctx, "failed to update consensus metadata after persistence",
						utils.ZapError(merr),
						utils.ZapUint64("height", task.Block.GetHeight()))
				}
				if pw.auditLogger != nil {
					_ = pw.auditLogger.Warn("consensus_metadata_update_failed", map[string]interface{}{
						"height": task.Block.GetHeight(),
						"hash":   fmt.Sprintf("%x", bh[:8]),
						"error":  merr.Error(),
					})
				}
			}
			metaCancel()

			if pw.onPersisted != nil {
				func() {
					defer func() {
						if r := recover(); r != nil {
							if pw.logger != nil {
								pw.logger.ErrorContext(ctx, "persistence persisted callback panic",
									utils.ZapAny("panic", r),
									utils.ZapUint64("height", task.Block.GetHeight()))
							}
						}
					}()
					pw.onPersisted(ctx, task.Block.GetHeight(), task.Block.GetTransactionCount())
				}()
			}

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

					pw.onSuccess(ctx, task.Block.GetHeight(), bh, task.StateRoot, task.Block.GetTransactionCount(), task.Block.GetTimestamp().Unix(), meta.anomalyCount, meta.evidenceCount, meta.policyCount, meta.anomalyIDs, meta.policyPayloads)
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

		// Fail closed on deterministic integrity/data violations.
		if errors.Is(err, cockroach.ErrIntegrityViolation) || errors.Is(err, cockroach.ErrInvalidData) {
			pw.incrementErrorCount()
			pw.emitPolicyPersistFailure(ctx, task, attempt+1, err, "integrity")
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

		// Retry only transient Cockroach/network errors.
		if !cockroach.IsRetryable(err) {
			pw.incrementErrorCount()
			pw.emitPolicyPersistFailure(ctx, task, attempt+1, err, "non_retryable")
			if pw.logger != nil {
				pw.logger.ErrorContext(ctx, "non-retryable persistence failure",
					utils.ZapError(err),
					utils.ZapUint64("height", task.Block.GetHeight()),
					utils.ZapInt("attempt", attempt+1))
			}
			return
		}

		// Transient error, retry with backoff
		if attempt < pw.cfg.RetryMax-1 {
			if pw.logger != nil {
				retryDelay := withRetryJitter(backoff)
				pw.logger.WarnContext(ctx, "persistence failed, retrying",
					utils.ZapError(err),
					utils.ZapUint64("height", task.Block.GetHeight()),
					utils.ZapInt("attempt", attempt+1),
					utils.ZapDuration("backoff", retryDelay))
				if !waitForRetry(ctx, pw.stopCh, retryDelay) {
					return
				}
			} else {
				retryDelay := withRetryJitter(backoff)
				if !waitForRetry(ctx, pw.stopCh, retryDelay) {
					return
				}
			}

			// Exponential backoff
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		} else {
			// Final attempt failed
			pw.incrementErrorCount()
			pw.emitPolicyPersistFailure(ctx, task, attempt+1, err, "retry_exhausted")
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

func (pw *PersistenceWorker) effectiveAttemptTimeout(task *PersistenceTask, attempt int) time.Duration {
	if pw == nil {
		return 25 * time.Second
	}
	timeout := pw.cfg.AttemptTimeout
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	if task == nil || task.Block == nil {
		if timeout > maxAttemptTimeout {
			return maxAttemptTimeout
		}
		return timeout
	}

	txCount := task.Block.GetTransactionCount()
	if txCount > 0 {
		extraMs := txCount * 30
		if extraMs > 45000 {
			extraMs = 45000
		}
		timeout += time.Duration(extraMs) * time.Millisecond

		policyCount := 0
		for _, tx := range task.Block.Transactions() {
			if tx != nil && tx.Type() == state.TxPolicy {
				policyCount++
			}
		}
		if policyCount > 0 {
			policyExtraMs := policyCount * 120
			if policyExtraMs > 60000 {
				policyExtraMs = 60000
			}
			timeout += time.Duration(policyExtraMs) * time.Millisecond
		}
	}

	if attempt >= pw.cfg.RetryMax-1 && pw.cfg.RetryMax > 1 {
		timeout += 10 * time.Second
	}
	if timeout > maxAttemptTimeout {
		timeout = maxAttemptTimeout
	}
	return timeout
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
	pw.writerMu.RLock()
	writerProposerOnly := pw.writerProposerOnly
	writerPolicyConfigured := pw.writerPolicyConfigured
	pw.writerMu.RUnlock()

	return map[string]interface{}{
		"running":                     pw.running,
		"queue_size":                  len(pw.queue),
		"queue_cap":                   cap(pw.queue),
		"task_count":                  pw.taskCount,
		"error_count":                 pw.errorCount,
		"deduped":                     pw.deduped,
		"workers":                     pw.cfg.WorkerCount,
		"execute_proposer":            pw.executeProposer.Load(),
		"execute_non_proposer":        pw.executeNonProposer.Load(),
		"execute_dropped_non_owner":   pw.executeDroppedNonOwnerTotal.Load(),
		"writer_policy_proposer_only": writerProposerOnly,
		"writer_policy_is_configured": writerPolicyConfigured,
	}
}

func (pw *PersistenceWorker) SetWriterPolicy(proposerOnly bool, nodeID ctypes.ValidatorID) {
	if pw == nil {
		return
	}
	pw.writerMu.Lock()
	pw.writerProposerOnly = proposerOnly
	pw.writerNodeID = nodeID
	pw.writerPolicyConfigured = true
	pw.writerMu.Unlock()
}

func (pw *PersistenceWorker) GetExecutionStats() (uint64, uint64, uint64) {
	if pw == nil {
		return 0, 0, 0
	}
	return pw.executeProposer.Load(), pw.executeNonProposer.Load(), pw.executeDroppedNonOwnerTotal.Load()
}

func (pw *PersistenceWorker) shouldExecuteTask(task *PersistenceTask) (bool, string, string) {
	if pw == nil || task == nil || task.Block == nil {
		return false, "unknown", "invalid_task"
	}
	pw.writerMu.RLock()
	proposerOnly := pw.writerProposerOnly
	localNodeID := pw.writerNodeID
	policyConfigured := pw.writerPolicyConfigured
	pw.writerMu.RUnlock()
	proposer := task.Block.Proposer()
	isProposer := localNodeID == proposer
	role := "non_proposer"
	if isProposer {
		role = "proposer"
	}
	if isZeroValidatorID(proposer) {
		return false, "unknown", "missing_proposer_id"
	}
	if !proposerOnly {
		return true, role, ""
	}
	if !policyConfigured {
		return false, role, "writer_policy_unset"
	}
	if !isProposer {
		return false, role, "non_proposer_guard"
	}
	return true, "proposer", ""
}

func isZeroValidatorID(v ctypes.ValidatorID) bool {
	for _, b := range v {
		if b != 0 {
			return false
		}
	}
	return true
}

func (pw *PersistenceWorker) releaseInflightKey(key persistenceTaskKey) {
	pw.mu.Lock()
	delete(pw.inflight, key)
	pw.mu.Unlock()
}

func (t *PersistenceTask) persistenceKey() (persistenceTaskKey, bool) {
	if t == nil || t.Block == nil {
		return persistenceTaskKey{}, false
	}
	return persistenceTaskKey{
		Height: t.Block.GetHeight(),
		Hash:   t.Block.GetHash(),
	}, true
}

// extractAnomalyIDs extracts anomaly IDs from EventTx transactions in a block
// Fix: Gap 2 - Enable COMMITTED state tracking by extracting anomaly UUIDs
type commitMetadata struct {
	anomalyIDs     []string
	anomalyCount   int
	evidenceCount  int
	policyCount    int
	policyPayloads [][]byte
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
			if policyTx, ok := tx.(*state.PolicyTx); ok && len(policyTx.Data) > 0 {
				payloadCopy := make([]byte, len(policyTx.Data))
				copy(payloadCopy, policyTx.Data)
				meta.policyPayloads = append(meta.policyPayloads, payloadCopy)
			}
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

func (pw *PersistenceWorker) emitPolicyPersistFailure(ctx context.Context, task *PersistenceTask, attempt int, err error, reason string) {
	if pw == nil || task == nil || task.Block == nil {
		return
	}
	ids := extractPolicyPersistIdentities(task.Block)
	if len(ids) == 0 {
		return
	}
	limit := len(ids)
	if limit > 10 {
		limit = 10
	}
	preview := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		preview = append(preview, ids[i].String())
	}
	if pw.logger != nil {
		pw.logger.ErrorContext(ctx, "policy persistence materialization failed",
			utils.ZapUint64("height", task.Block.GetHeight()),
			utils.ZapInt("attempt", attempt),
			utils.ZapString("reason", reason),
			utils.ZapString("error", err.Error()),
			utils.ZapInt("policy_count", len(ids)),
			utils.ZapAny("policy_preview", preview))
	}
	if pw.auditLogger != nil {
		_ = pw.auditLogger.Error("policy_persistence_materialization_failed", map[string]interface{}{
			"height":        task.Block.GetHeight(),
			"attempt":       attempt,
			"reason":        reason,
			"error":         err.Error(),
			"policy_count":  len(ids),
			"policy_sample": preview,
		})
	}
}

type policyPersistIdentity struct {
	PolicyID string
	TraceID  string
}

func (p policyPersistIdentity) String() string {
	if p.TraceID == "" {
		return p.PolicyID
	}
	return p.PolicyID + ":" + p.TraceID
}

func extractPolicyPersistIdentities(blk *block.AppBlock) []policyPersistIdentity {
	if blk == nil {
		return nil
	}
	txs := blk.Transactions()
	if len(txs) == 0 {
		return nil
	}
	ids := make([]policyPersistIdentity, 0, len(txs))
	seen := make(map[string]struct{}, len(txs))
	for _, tx := range txs {
		if tx == nil || tx.Type() != state.TxPolicy {
			continue
		}
		policyTx, ok := tx.(*state.PolicyTx)
		if !ok || len(policyTx.Data) == 0 {
			continue
		}
		policyID, traceID := parsePolicyIdentity(policyTx.Data)
		if policyID == "" {
			continue
		}
		key := policyID + "|" + traceID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		ids = append(ids, policyPersistIdentity{
			PolicyID: policyID,
			TraceID:  traceID,
		})
	}
	return ids
}

func parsePolicyIdentity(raw []byte) (string, string) {
	if len(raw) == 0 {
		return "", ""
	}
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return "", ""
	}
	policyID := strings.TrimSpace(stringValueFromMap(root, "policy_id"))
	traceID := strings.TrimSpace(stringValueFromMap(root, "trace_id"))
	if traceID == "" {
		if nested, ok := root["metadata"].(map[string]interface{}); ok {
			traceID = strings.TrimSpace(stringValueFromMap(nested, "trace_id"))
		}
	}
	if traceID == "" {
		if nested, ok := root["trace"].(map[string]interface{}); ok {
			traceID = strings.TrimSpace(stringValueFromMap(nested, "id"))
		}
	}
	return policyID, traceID
}

func stringValueFromMap(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return ""
	}
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

func withRetryJitter(backoff time.Duration) time.Duration {
	if backoff <= 0 {
		return 0
	}
	maxJitter := backoff / 5 // up to +20%
	if maxJitter <= 0 {
		return backoff
	}
	jitter := time.Duration(rand.Int63n(int64(maxJitter) + 1))
	return backoff + jitter
}

func waitForRetry(ctx context.Context, stopCh <-chan struct{}, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false
		case <-stopCh:
			return false
		default:
			return true
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	case <-stopCh:
		return false
	}
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
