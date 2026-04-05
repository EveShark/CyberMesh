package controller

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type executionLane string

const (
	laneCritical    executionLane = "critical"
	laneMaintenance executionLane = "maintenance"
)

func (c *Controller) enterLane(ctx context.Context, lane executionLane) (func(), error) {
	if c == nil {
		return func() {}, nil
	}

	slots, depthPtr := c.laneControls(lane)
	if slots == nil {
		return func() {}, nil
	}
	if c.metrics != nil {
		queued := atomic.AddInt64(depthPtr, 1)
		c.metrics.SetLaneQueueDepth(string(lane), queued)
	}
	start := time.Now()
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		if c.metrics != nil {
			queued := atomic.AddInt64(depthPtr, -1)
			if queued < 0 {
				queued = 0
			}
			c.metrics.SetLaneQueueDepth(string(lane), queued)
		}
		return nil, fmt.Errorf("lane %s wait canceled: %w", lane, ctx.Err())
	}
	wait := time.Since(start)
	if c.metrics != nil {
		queued := atomic.AddInt64(depthPtr, -1)
		if queued < 0 {
			queued = 0
		}
		c.metrics.SetLaneQueueDepth(string(lane), queued)
		c.metrics.ObserveLaneDispatchWait(string(lane), wait)
		if c.laneStarveAfter > 0 && wait >= c.laneStarveAfter {
			c.metrics.ObserveLaneStarvation(string(lane))
		}
	}

	return func() {
		select {
		case <-slots:
		default:
		}
	}, nil
}

func (c *Controller) laneControls(lane executionLane) (chan struct{}, *int64) {
	switch lane {
	case laneMaintenance:
		return c.maintenanceSlots, &c.maintQueueDepth
	default:
		return c.criticalSlots, &c.criticalQueueDepth
	}
}

func (c *Controller) effectiveLaneMode(lane executionLane) string {
	if c == nil {
		return "enforce"
	}
	mode := c.criticalMode
	warned := &c.criticalGateWarned
	if lane == laneMaintenance {
		mode = c.maintenanceMode
		warned = &c.maintenanceGateWarned
	}
	if mode != "enforce" {
		return "dry_run"
	}
	if !c.lifecyclePromotionGateEnabled || c.lifecyclePromotionMinWindow <= 0 {
		return "enforce"
	}
	if time.Since(c.startedAt) >= c.lifecyclePromotionMinWindow {
		return "enforce"
	}
	if atomic.CompareAndSwapUint32(warned, 0, 1) && c.logger != nil {
		c.logger.Warn("lifecycle promotion gate forcing dry_run",
			zap.String("lane", string(lane)),
			zap.Duration("promotion_min_window", c.lifecyclePromotionMinWindow),
			zap.Duration("uptime", time.Since(c.startedAt)))
	}
	return "dry_run"
}
