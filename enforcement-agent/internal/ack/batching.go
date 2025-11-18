package ack

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"
)

// BatchingPublisher buffers payloads and flushes them via PublishBatch.
type BatchingPublisher struct {
	backend   Publisher
	metrics   BatchingMetrics
	logger    *zap.Logger
	flushSize int
	interval  time.Duration

	mu       sync.Mutex
	payloads []Payload
	cond     *sync.Cond
	closing  bool
	closed   chan struct{}
}

// BatchingMetrics captures batch observability hooks.
type BatchingMetrics interface {
	ObserveAckBatchSize(int)
	ObserveAckBatchFlush(status string)
}

// BatchingOptions configure the batching wrapper.
type BatchingOptions struct {
	Backend   Publisher
	Metrics   BatchingMetrics
	Logger    *zap.Logger
	FlushSize int
	Interval  time.Duration
}

// NewBatchingPublisher builds a new batching wrapper.
func NewBatchingPublisher(opts BatchingOptions) (*BatchingPublisher, error) {
	if opts.Backend == nil {
		return nil, errors.New("ack batcher: backend required")
	}
	flushSize := opts.FlushSize
	if flushSize <= 0 {
		flushSize = 1
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	b := &BatchingPublisher{
		backend:   opts.Backend,
		metrics:   opts.Metrics,
		logger:    opts.Logger,
		flushSize: flushSize,
		interval:  interval,
		closed:    make(chan struct{}),
	}
	b.cond = sync.NewCond(&b.mu)
	go b.loop()
	return b, nil
}

// Publish buffers the payload until flush conditions trigger.
func (b *BatchingPublisher) Publish(ctx context.Context, payload Payload) error {
	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		return errors.New("ack batcher: closed")
	}
	b.payloads = append(b.payloads, payload)
	b.cond.Broadcast()
	b.mu.Unlock()
	return nil
}

// PublishBatch enqueues each payload individually (batching handles accumulation).
func (b *BatchingPublisher) PublishBatch(ctx context.Context, payloads []Payload) error {
	var firstErr error
	for _, payload := range payloads {
		if err := b.Publish(ctx, payload); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (b *BatchingPublisher) loop() {
	defer close(b.closed)
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	for {
		batch := b.collect(false)
		if len(batch) == 0 {
			if b.isClosing() {
				batch = b.collect(true)
			} else {
				<-ticker.C
				batch = b.collect(true)
			}
		}
		if len(batch) == 0 {
			if b.isClosing() {
				return
			}
			continue
		}
		b.flush(batch)
	}
}

func (b *BatchingPublisher) isClosing() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closing
}

func (b *BatchingPublisher) collect(force bool) []Payload {
	b.mu.Lock()
	defer b.mu.Unlock()
	for len(b.payloads) == 0 && !b.closing {
		if force {
			return nil
		}
		b.cond.Wait()
	}
	if len(b.payloads) == 0 {
		return nil
	}
	if !force && !b.closing && len(b.payloads) < b.flushSize {
		return nil
	}
	batch := append([]Payload(nil), b.payloads...)
	b.payloads = b.payloads[:0]
	return batch
}

func (b *BatchingPublisher) flush(batch []Payload) {
	if b.metrics != nil {
		b.metrics.ObserveAckBatchSize(len(batch))
	}
	ctx := context.Background()
	if err := b.backend.PublishBatch(ctx, batch); err != nil {
		if b.logger != nil {
			b.logger.Error("ack batcher: flush failed", zap.Error(err), zap.Int("batch_size", len(batch)))
		}
		if b.metrics != nil {
			b.metrics.ObserveAckBatchFlush("failure")
		}
		// retry individually to avoid loss
		for _, payload := range batch {
			if err := b.backend.Publish(ctx, payload); err != nil && b.logger != nil {
				b.logger.Error("ack batcher: fallback publish failed", zap.Error(err))
			}
		}
		return
	}
	if b.metrics != nil {
		b.metrics.ObserveAckBatchFlush("success")
	}
}

// Close drains pending payloads and stops the worker.
func (b *BatchingPublisher) Close(ctx context.Context) error {
	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		select {
		case <-b.closed:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	b.closing = true
	b.cond.Broadcast()
	b.mu.Unlock()
	select {
	case <-b.closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
