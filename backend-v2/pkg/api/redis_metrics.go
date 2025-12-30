package api

import (
	"context"
	"sync/atomic"
	"time"

	"backend/pkg/utils"

	redis "github.com/redis/go-redis/v9"
)

type redisTelemetry struct {
	latency *utils.LatencyHistogram
	errors  atomic.Uint64
}

func newRedisTelemetry() *redisTelemetry {
	return &redisTelemetry{latency: utils.NewLatencyHistogram([]float64{1, 5, 20, 50, 100, 250, 500, 1000, 5000, 10000})}
}

func (t *redisTelemetry) observe(duration time.Duration) {
	if t == nil {
		return
	}
	if duration < 0 {
		duration = 0
	}
	t.latency.Observe(float64(duration) / float64(time.Millisecond))
}

func (t *redisTelemetry) recordError() {
	if t == nil {
		return
	}
	t.errors.Add(1)
}

func (t *redisTelemetry) snapshot() ([]utils.HistogramBucket, uint64, float64, uint64) {
	if t == nil {
		return nil, 0, 0, 0
	}
	buckets, count, sum := t.latency.Snapshot()
	return buckets, count, sum, t.errors.Load()
}

func (t *redisTelemetry) quantile(q float64) float64 {
	if t == nil {
		return 0
	}
	return t.latency.Quantile(q)
}

type redisTelemetryHook struct {
	telemetry *redisTelemetry
}

func (h *redisTelemetryHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *redisTelemetryHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if h == nil || h.telemetry == nil {
			return next(ctx, cmd)
		}
		start := time.Now()
		err := next(ctx, cmd)
		h.telemetry.observe(time.Since(start))
		if err != nil && err != redis.Nil {
			h.telemetry.recordError()
		} else if cmdErr := cmd.Err(); cmdErr != nil && cmdErr != redis.Nil {
			h.telemetry.recordError()
		}
		return err
	}
}

func (h *redisTelemetryHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if h == nil || h.telemetry == nil {
			return next(ctx, cmds)
		}
		start := time.Now()
		err := next(ctx, cmds)
		duration := time.Since(start)
		count := len(cmds)
		if count == 0 {
			h.telemetry.observe(duration)
		} else {
			avg := duration / time.Duration(count)
			for _, cmd := range cmds {
				h.telemetry.observe(avg)
				if cmdErr := cmd.Err(); cmdErr != nil && cmdErr != redis.Nil {
					h.telemetry.recordError()
				}
			}
		}
		if err != nil && err != redis.Nil {
			h.telemetry.recordError()
		}
		return err
	}
}
