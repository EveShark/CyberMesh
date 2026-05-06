package ack

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
)

// RetryingPublisher wraps a Publisher with a durable queue.
type RetryingPublisher struct {
	queue           Queue
	backend         Publisher
	metrics         RetrierMetrics
	logger          *zap.Logger
	interval        time.Duration
	maxHeadAttempts int
	drainBatchSize  int
	stopCh          chan struct{}
	stopped         chan struct{}
}

// RetrierMetrics exposes queue observability hooks.
type RetrierMetrics interface {
	ObserveAckQueueDepth(int)
	ObserveAckLatency(float64)
	ObserveAckQueueFailure()
	ObserveAckQueueBlocked()
}

// RetrierOptions configure RetryingPublisher.
type RetrierOptions struct {
	Queue           Queue
	Backend         Publisher
	Metrics         RetrierMetrics
	Logger          *zap.Logger
	Interval        time.Duration
	MaxHeadAttempts int
	DrainBatchSize  int
}

// NewRetryingPublisher constructs wrapper.
func NewRetryingPublisher(opts RetrierOptions) (*RetryingPublisher, error) {
	if opts.Queue == nil {
		return nil, errors.New("ack retrier: queue required")
	}
	if opts.Backend == nil {
		return nil, errors.New("ack retrier: backend required")
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	maxHeadAttempts := opts.MaxHeadAttempts
	if maxHeadAttempts <= 0 {
		maxHeadAttempts = 3
	}
	drainBatchSize := opts.DrainBatchSize
	if drainBatchSize <= 0 {
		drainBatchSize = 1
	}
	r := &RetryingPublisher{
		queue:           opts.Queue,
		backend:         opts.Backend,
		metrics:         opts.Metrics,
		logger:          opts.Logger,
		interval:        interval,
		maxHeadAttempts: maxHeadAttempts,
		drainBatchSize:  drainBatchSize,
		stopCh:          make(chan struct{}),
		stopped:         make(chan struct{}),
	}
	go r.loop()
	return r, nil
}

// Publish enqueues payload and wakes worker.
func (r *RetryingPublisher) Publish(ctx context.Context, payload Payload) error {
	if _, err := r.queue.Enqueue(ctx, payload); err != nil {
		if errors.Is(err, ErrQueueFull) {
			if r.metrics != nil {
				r.metrics.ObserveAckQueueBlocked()
			}
		} else if r.metrics != nil {
			r.metrics.ObserveAckQueueFailure()
		}
		if r.logger != nil {
			r.logger.Error("ack retrier: enqueue failed", zap.Error(err), zap.String("policy_id", payload.Event.Spec.ID))
		}
		return err
	}
	r.observeDepth(ctx)
	return nil
}

// PublishBatch enqueues multiple payloads.
func (r *RetryingPublisher) PublishBatch(ctx context.Context, payloads []Payload) error {
	var firstErr error
	for _, payload := range payloads {
		if err := r.Publish(ctx, payload); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *RetryingPublisher) loop() {
	defer close(r.stopped)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	ctx := context.Background()
	r.drain(ctx)
	notify := r.queue.Notify()
	for {
		select {
		case <-r.stopCh:
			return
		case <-notify:
			r.drain(ctx)
		case <-ticker.C:
			r.drain(ctx)
		}
	}
}

func (r *RetryingPublisher) drain(ctx context.Context) {
	if r.drainBatchSize > 1 {
		if batchQueue, ok := r.queue.(BatchQueue); ok {
			r.drainBatches(ctx, batchQueue)
			return
		}
	}
	var (
		currentID uint64
		attempts  int
	)
	for {
		select {
		case <-r.stopCh:
			return
		default:
		}
		id, payload, err := r.queue.Peek(ctx)
		if errors.Is(err, ErrQueueEmpty) {
			r.observeDepth(ctx)
			return
		}
		if err != nil {
			if r.metrics != nil {
				r.metrics.ObserveAckQueueFailure()
			}
			if r.logger != nil {
				r.logger.Error("ack retrier: peek failed", zap.Error(err))
			}
			return
		}
		if id != currentID {
			currentID = id
			attempts = 0
		}
		attempts++
		if err := r.backend.Publish(ctx, payload); err != nil {
			if r.logger != nil {
				r.logger.Error("ack retrier: publish failed", zap.Error(err), zap.String("policy_id", payload.Event.Spec.ID), zap.Int("attempt", attempts))
			}
			if r.metrics != nil {
				r.metrics.ObserveAckQueueFailure()
			}
			if attempts >= r.maxHeadAttempts {
				if rotateErr := r.rotateHead(ctx, id, payload); rotateErr != nil {
					if r.logger != nil {
						r.logger.Error("ack retrier: rotate head failed", zap.Error(rotateErr), zap.Uint64("queue_id", id), zap.String("policy_id", payload.Event.Spec.ID))
					}
					r.sleepOrStop()
					continue
				}
				currentID = 0
				attempts = 0
				continue
			}
			r.sleepOrStop()
			continue
		}
		if err := r.queue.Delete(ctx, id); err != nil {
			if r.logger != nil {
				r.logger.Warn("ack retrier: delete failed", zap.Error(err), zap.Uint64("queue_id", id))
			}
			if r.metrics != nil {
				r.metrics.ObserveAckQueueFailure()
			}
		} else {
			r.observeDepth(ctx)
		}
		if r.metrics != nil && !payload.AppliedAt.IsZero() {
			ackObservedAt := time.Now().UTC()
			latency := ackObservedAt.Sub(payload.AppliedAt).Seconds()
			if latency >= 0 {
				r.metrics.ObserveAckLatency(latency)
			}
		}
		currentID = 0
		attempts = 0
	}
}

func (r *RetryingPublisher) drainBatches(ctx context.Context, batchQueue BatchQueue) {
	for {
		select {
		case <-r.stopCh:
			return
		default:
		}
		ids, payloads, err := batchQueue.PeekBatch(ctx, r.drainBatchSize)
		if errors.Is(err, ErrQueueEmpty) {
			r.observeDepth(ctx)
			return
		}
		if err != nil {
			if r.metrics != nil {
				r.metrics.ObserveAckQueueFailure()
			}
			if r.logger != nil {
				r.logger.Error("ack retrier: batch peek failed", zap.Error(err))
			}
			return
		}
		if len(ids) == 0 || len(ids) != len(payloads) {
			return
		}
		if err := r.backend.PublishBatch(ctx, payloads); err != nil {
			if r.logger != nil {
				r.logger.Error("ack retrier: batch publish failed", zap.Error(err), zap.Int("batch_size", len(payloads)))
			}
			if r.metrics != nil {
				r.metrics.ObserveAckQueueFailure()
			}
			r.sleepOrStop()
			continue
		}
		for _, id := range ids {
			if err := r.queue.Delete(ctx, id); err != nil {
				if r.logger != nil {
					r.logger.Warn("ack retrier: delete failed", zap.Error(err), zap.Uint64("queue_id", id))
				}
				if r.metrics != nil {
					r.metrics.ObserveAckQueueFailure()
				}
				break
			}
		}
		r.observeDepth(ctx)
		if r.metrics != nil {
			ackObservedAt := time.Now().UTC()
			for _, payload := range payloads {
				if !payload.AppliedAt.IsZero() {
					if latency := ackObservedAt.Sub(payload.AppliedAt).Seconds(); latency >= 0 {
						r.metrics.ObserveAckLatency(latency)
					}
				}
			}
		}
	}
}

func (r *RetryingPublisher) rotateHead(ctx context.Context, id uint64, payload Payload) error {
	if _, err := r.queue.Enqueue(ctx, payload); err != nil {
		return err
	}
	if err := r.queue.Delete(ctx, id); err != nil {
		return err
	}
	r.observeDepth(ctx)
	return nil
}

func (r *RetryingPublisher) sleepOrStop() {
	timer := time.NewTimer(r.interval)
	defer timer.Stop()
	select {
	case <-r.stopCh:
		return
	case <-timer.C:
		return
	}
}

func (r *RetryingPublisher) observeDepth(ctx context.Context) {
	if r.metrics == nil {
		return
	}
	size, err := r.queue.Len(ctx)
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("ack retrier: len failed", zap.Error(err))
		}
		r.metrics.ObserveAckQueueFailure()
		return
	}
	r.metrics.ObserveAckQueueDepth(size)
}

// Close stops worker and closes queue.
func (r *RetryingPublisher) Close(ctx context.Context) error {
	close(r.stopCh)
	<-r.stopped
	return r.queue.Close()
}
