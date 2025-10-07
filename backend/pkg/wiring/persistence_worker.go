package wiring

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"backend/pkg/block"
	"backend/pkg/state"
	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"
)

// PersistenceTask represents a block persistence task
type PersistenceTask struct {
	Block     *block.AppBlock
	Receipts  []state.Receipt
	StateRoot [32]byte
	Attempt   int
}

// PersistenceSuccessCallback is called after successful block persistence
// Used to trigger downstream actions like Kafka publishing
type PersistenceSuccessCallback func(ctx context.Context, height uint64, hash [32]byte, stateRoot [32]byte, txCount int, ts int64)

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
	taskCount  uint64 // Total tasks received
	errorCount uint64 // Total errors
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
			if pw.auditLogger != nil {
				_ = pw.auditLogger.Info("block_persisted_async", map[string]interface{}{
					"height":     task.Block.GetHeight(),
					"hash":       fmt.Sprintf("%x", bh[:8]),
					"attempts":   attempt + 1,
					"state_root": fmt.Sprintf("%x", task.StateRoot[:8]),
				})
			}

			// Invoke onSuccess callback (e.g., for Kafka publishing)
			if pw.onSuccess != nil {
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

					// Call callback with block metadata
					pw.onSuccess(ctx, task.Block.GetHeight(), bh, task.StateRoot, task.Block.GetTransactionCount(), task.Block.GetTimestamp().Unix())
				}()
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
