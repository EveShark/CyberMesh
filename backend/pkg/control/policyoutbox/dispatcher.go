package policyoutbox

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/utils"
)

type Publisher interface {
	PublishFromOutbox(ctx context.Context, height uint64, ts int64, raw []byte) (string, int32, int64, error)
}

type storeOps interface {
	TryAcquireLease(ctx context.Context, leaseKey, holderID string, ttl time.Duration) (bool, int64, error)
	ClaimPending(ctx context.Context, holderID string, epoch int64, limit int, reclaimAfter time.Duration) ([]Row, error)
	MarkPublished(ctx context.Context, id, holderID string, epoch int64, topic string, partition int32, offset int64) error
	MarkRetry(ctx context.Context, id, holderID string, epoch int64, retries int, nextRetryAt time.Time, errMsg string) error
	MarkTerminal(ctx context.Context, id, holderID string, epoch int64, retries int, errMsg string) error
}

type Dispatcher struct {
	cfg    Config
	store  storeOps
	pub    Publisher
	logger *utils.Logger
	audit  *utils.AuditLogger
	holder string

	stopCh chan struct{}
	doneCh chan struct{}

	leaseAcquireAttempts uint64
	leaseAcquireSuccess  uint64
	leaseAcquireErrors   uint64
	rowsClaimedTotal     uint64
	currentBatchSize     uint64
	batchScaleUpTotal    uint64
	batchScaleDownTotal  uint64
	publishQueueWaits    uint64
	markQueueWaits       uint64
	publishedTotal       uint64
	retryTotal           uint64
	terminalTotal        uint64
	fencedFailures       uint64
	throttledLogs        uint64
	ticksTotal           uint64
	skewCorrections      uint64
	leaseHeld            uint32
	lastLeaseEpoch       int64
	publishLatency       *utils.LatencyHistogram
	claimLatency         *utils.LatencyHistogram
	markLatency          *utils.LatencyHistogram
	tickLatency          *utils.LatencyHistogram
	aiToPublishLatency   *utils.LatencyHistogram
	commitToPublish      *utils.LatencyHistogram
	lastLeaseErrLogAt    int64
	lastClaimErrLogAt    int64
	lastMarkErrLogAt     int64
}

func NewDispatcher(cfg Config, store storeOps, pub Publisher, logger *utils.Logger, audit *utils.AuditLogger, holderID string) (*Dispatcher, error) {
	if store == nil {
		return nil, fmt.Errorf("policy outbox: store required")
	}
	if pub == nil {
		return nil, fmt.Errorf("policy outbox: publisher required")
	}
	if holderID == "" {
		return nil, fmt.Errorf("policy outbox: holder id required")
	}

	if cfg.LeaseKey == "" {
		cfg.LeaseKey = "control.policy.dispatcher"
	}
	if cfg.PolicyTopic == "" {
		cfg.PolicyTopic = "control.policy.v1"
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 10 * time.Second
	}
	if cfg.ReclaimAfter <= 0 {
		cfg.ReclaimAfter = maxDuration(3*cfg.LeaseTTL, 30*time.Second)
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.DrainMaxDuration <= 0 {
		cfg.DrainMaxDuration = 2 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.BatchSizeMin <= 0 {
		cfg.BatchSizeMin = maxInt(1, cfg.BatchSize/2)
	}
	if cfg.BatchSizeMax <= 0 {
		cfg.BatchSizeMax = maxInt(cfg.BatchSize, cfg.BatchSize*4)
	}
	if cfg.BatchSizeMin > cfg.BatchSize {
		cfg.BatchSizeMin = cfg.BatchSize
	}
	if cfg.BatchSizeMin > cfg.BatchSizeMax {
		cfg.BatchSizeMin, cfg.BatchSizeMax = cfg.BatchSizeMax, cfg.BatchSizeMin
	}
	cfg.BatchSize = clampInt(cfg.BatchSize, cfg.BatchSizeMin, cfg.BatchSizeMax)
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = 8
	}
	if cfg.MarkWorkers <= 0 {
		cfg.MarkWorkers = maxInt(1, cfg.MaxInFlight/2)
	}
	if cfg.InternalQueue <= 0 {
		cfg.InternalQueue = maxInt(64, cfg.MaxInFlight*4)
	}
	if cfg.DrainMaxBatches <= 0 {
		cfg.DrainMaxBatches = 4
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 8
	}
	if cfg.RetryInitialBack <= 0 {
		cfg.RetryInitialBack = 250 * time.Millisecond
	}
	if cfg.RetryMaxBack <= 0 {
		cfg.RetryMaxBack = 30 * time.Second
	}
	if cfg.RetryJitterRatio < 0 || cfg.RetryJitterRatio > 1 {
		cfg.RetryJitterRatio = 0.2
	}
	if cfg.LogThrottle <= 0 {
		cfg.LogThrottle = 5 * time.Second
	}

	return &Dispatcher{
		cfg:    cfg,
		store:  store,
		pub:    pub,
		logger: logger,
		audit:  audit,
		holder: holderID,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		publishLatency: utils.NewLatencyHistogram([]float64{
			1, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 5000,
		}),
		claimLatency: utils.NewLatencyHistogram([]float64{
			1, 5, 10, 25, 50, 100, 250, 500, 1000,
		}),
		markLatency: utils.NewLatencyHistogram([]float64{
			1, 5, 10, 25, 50, 100, 250, 500, 1000,
		}),
		tickLatency: utils.NewLatencyHistogram([]float64{
			10, 25, 50, 100, 250, 500, 1000, 2000, 5000,
		}),
		aiToPublishLatency: utils.NewLatencyHistogram([]float64{
			10, 25, 50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000,
		}),
		commitToPublish: utils.NewLatencyHistogram([]float64{
			10, 25, 50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000,
		}),
		currentBatchSize: uint64(cfg.BatchSize),
	}, nil
}

func (d *Dispatcher) Start(ctx context.Context) error {
	if !d.cfg.Enabled {
		return nil
	}
	go d.run(ctx)
	return nil
}

func (d *Dispatcher) Stop() {
	if d == nil {
		return
	}
	select {
	case <-d.stopCh:
		return
	default:
		close(d.stopCh)
	}
	<-d.doneCh
}

func (d *Dispatcher) run(ctx context.Context) {
	defer close(d.doneCh)
	t := time.NewTicker(d.cfg.PollInterval)
	defer t.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			d.tick(ctx)
		}
	}
}

func (d *Dispatcher) tick(ctx context.Context) {
	tickStart := time.Now()
	defer func() {
		atomic.AddUint64(&d.ticksTotal, 1)
		d.tickLatency.Observe(float64(time.Since(tickStart).Milliseconds()))
	}()

	atomic.AddUint64(&d.leaseAcquireAttempts, 1)
	acquired, epoch, err := d.store.TryAcquireLease(ctx, d.cfg.LeaseKey, d.holder, d.cfg.LeaseTTL)
	if err != nil {
		atomic.AddUint64(&d.leaseAcquireErrors, 1)
		atomic.StoreUint32(&d.leaseHeld, 0)
		if d.logger != nil && d.shouldLog("lease", &d.lastLeaseErrLogAt) {
			d.logger.WarnContext(ctx, "policy outbox lease acquire failed", utils.ZapError(err))
		}
		return
	}
	if !acquired {
		atomic.StoreUint32(&d.leaseHeld, 0)
		return
	}
	atomic.AddUint64(&d.leaseAcquireSuccess, 1)
	atomic.StoreUint32(&d.leaseHeld, 1)
	atomic.StoreInt64(&d.lastLeaseEpoch, epoch)

	deadline := tickStart.Add(d.cfg.DrainMaxDuration)
	for batch := 0; batch < d.cfg.DrainMaxBatches; batch++ {
		if d.cfg.DrainMaxDuration > 0 && time.Now().After(deadline) {
			return
		}
		batchSize := int(atomic.LoadUint64(&d.currentBatchSize))
		if batchSize <= 0 {
			batchSize = d.cfg.BatchSize
		}
		claimStart := time.Now()
		rows, err := d.store.ClaimPending(ctx, d.holder, epoch, batchSize, d.cfg.ReclaimAfter)
		claimMs := float64(time.Since(claimStart).Milliseconds())
		d.claimLatency.Observe(claimMs)
		if err != nil {
			if d.logger != nil && d.shouldLog("claim", &d.lastClaimErrLogAt) {
				d.logger.WarnContext(ctx, "policy outbox claim failed", utils.ZapError(err))
			}
			d.adjustBatchSize(batchSize, 0, claimMs)
			return
		}
		d.adjustBatchSize(batchSize, len(rows), claimMs)
		if len(rows) == 0 {
			return
		}
		atomic.AddUint64(&d.rowsClaimedTotal, uint64(len(rows)))
		d.processBatch(ctx, rows, deadline)
		if len(rows) < batchSize {
			return
		}
	}
}

func (d *Dispatcher) processBatch(ctx context.Context, rows []Row, deadline time.Time) {
	if len(rows) == 0 {
		return
	}

	publishWorkers := d.cfg.MaxInFlight
	if publishWorkers <= 0 {
		publishWorkers = 1
	}
	markWorkers := d.cfg.MarkWorkers
	if markWorkers <= 0 {
		markWorkers = 1
	}
	qSize := d.cfg.InternalQueue
	if qSize <= 0 {
		qSize = maxInt(64, publishWorkers*4)
	}

	publishQ := make(chan Row, qSize)
	markQ := make(chan markTask, qSize)
	var pubWG sync.WaitGroup
	var markWG sync.WaitGroup

	for i := 0; i < markWorkers; i++ {
		markWG.Add(1)
		go func() {
			defer markWG.Done()
			for task := range markQ {
				d.handleMarkTask(ctx, task, deadline)
			}
		}()
	}

	for i := 0; i < publishWorkers; i++ {
		pubWG.Add(1)
		go func() {
			defer pubWG.Done()
			for row := range publishQ {
				task, ok := d.publishRow(ctx, row, deadline)
				if !ok {
					continue
				}
				select {
				case markQ <- task:
				default:
					atomic.AddUint64(&d.markQueueWaits, 1)
					markQ <- task
				}
			}
		}()
	}

	for _, row := range rows {
		if d.cfg.DrainMaxDuration > 0 && time.Now().After(deadline) {
			break
		}
		select {
		case publishQ <- row:
		default:
			atomic.AddUint64(&d.publishQueueWaits, 1)
			publishQ <- row
		}
	}
	close(publishQ)
	pubWG.Wait()
	close(markQ)
	markWG.Wait()
}

type markTaskType int

const (
	markTaskPublished markTaskType = iota + 1
	markTaskRetry
	markTaskTerminal
)

type markTask struct {
	kind        markTaskType
	row         Row
	partition   int32
	offset      int64
	retries     int
	nextRetryAt time.Time
	errMsg      string
	pubStart    time.Time
}

func (d *Dispatcher) publishRow(ctx context.Context, row Row, deadline time.Time) (markTask, bool) {
	if d.cfg.DrainMaxDuration > 0 && time.Now().After(deadline) {
		return markTask{}, false
	}
	opCtx := ctx
	cancel := func() {}
	if d.cfg.DrainMaxDuration > 0 {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return markTask{}, false
		}
		opCtx, cancel = context.WithTimeout(ctx, remaining)
	}
	defer cancel()

	start := time.Now()
	_, partition, offset, err := d.pub.PublishFromOutbox(opCtx, row.BlockHeight, row.BlockTS, row.Payload)
	if err != nil {
		nextRetries := row.Retries + 1
		if nextRetries >= d.cfg.MaxRetries {
			return markTask{
				kind:     markTaskTerminal,
				row:      row,
				retries:  nextRetries,
				errMsg:   err.Error(),
				pubStart: start,
			}, true
		}
		backoff := d.cfg.RetryInitialBack * time.Duration(1<<uint(nextRetries-1))
		if backoff > d.cfg.RetryMaxBack {
			backoff = d.cfg.RetryMaxBack
		}
		backoff = d.jitterDuration(backoff)
		return markTask{
			kind:        markTaskRetry,
			row:         row,
			retries:     nextRetries,
			nextRetryAt: time.Now().Add(backoff),
			errMsg:      err.Error(),
			pubStart:    start,
		}, true
	}
	return markTask{
		kind:      markTaskPublished,
		row:       row,
		partition: partition,
		offset:    offset,
		pubStart:  start,
	}, true
}

func (d *Dispatcher) handleMarkTask(ctx context.Context, task markTask, deadline time.Time) {
	opCtx := ctx
	cancel := func() {}
	if d.cfg.DrainMaxDuration > 0 {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		opCtx, cancel = context.WithTimeout(ctx, remaining)
	}
	defer cancel()

	switch task.kind {
	case markTaskPublished:
		markStart := time.Now()
		err := d.store.MarkPublished(opCtx, task.row.ID, d.holder, task.row.LeaseEpoch, d.cfg.PolicyTopic, task.partition, task.offset)
		d.markLatency.Observe(float64(time.Since(markStart).Milliseconds()))
		if err != nil {
			if strings.Contains(err.Error(), "fenced/no-op") {
				atomic.AddUint64(&d.fencedFailures, 1)
				return
			}
			if d.logger != nil && d.shouldLog("mark", &d.lastMarkErrLogAt) {
				d.logger.WarnContext(ctx, "policy outbox mark published failed",
					utils.ZapError(err),
					utils.ZapString("outbox_id", task.row.ID))
			}
			return
		}
		d.publishLatency.Observe(float64(time.Since(task.pubStart).Milliseconds()))
		if task.row.BlockTS > 0 {
			commitLatency := time.Now().UnixMilli() - (task.row.BlockTS * 1000)
			if commitLatency < 0 {
				commitLatency = 0
				atomic.AddUint64(&d.skewCorrections, 1)
			}
			d.commitToPublish.Observe(float64(commitLatency))
		}
		if task.row.AIEventTsMs > 0 {
			aiLatency := time.Now().UnixMilli() - task.row.AIEventTsMs
			if aiLatency < 0 {
				aiLatency = 0
				atomic.AddUint64(&d.skewCorrections, 1)
			}
			d.aiToPublishLatency.Observe(float64(aiLatency))
		}
		atomic.AddUint64(&d.publishedTotal, 1)
	case markTaskRetry:
		markStart := time.Now()
		err := d.store.MarkRetry(opCtx, task.row.ID, d.holder, task.row.LeaseEpoch, task.retries, task.nextRetryAt, task.errMsg)
		d.markLatency.Observe(float64(time.Since(markStart).Milliseconds()))
		if err != nil && strings.Contains(err.Error(), "fenced/no-op") {
			atomic.AddUint64(&d.fencedFailures, 1)
		}
		atomic.AddUint64(&d.retryTotal, 1)
	case markTaskTerminal:
		markStart := time.Now()
		err := d.store.MarkTerminal(opCtx, task.row.ID, d.holder, task.row.LeaseEpoch, task.retries, task.errMsg)
		d.markLatency.Observe(float64(time.Since(markStart).Milliseconds()))
		if err != nil && strings.Contains(err.Error(), "fenced/no-op") {
			atomic.AddUint64(&d.fencedFailures, 1)
		}
		atomic.AddUint64(&d.terminalTotal, 1)
		if d.audit != nil {
			_ = d.audit.Security("policy_outbox_terminal_failed", map[string]interface{}{
				"id":        task.row.ID,
				"policy_id": task.row.PolicyID,
				"height":    task.row.BlockHeight,
				"retries":   task.retries,
				"error":     task.errMsg,
			})
		}
	}
}

// Stats returns a point-in-time dispatcher metrics snapshot.
func (d *Dispatcher) Stats() DispatcherStats {
	if d == nil {
		return DispatcherStats{}
	}
	pubBuckets, pubCount, pubSumMs := d.publishLatency.Snapshot()
	claimBuckets, claimCount, claimSumMs := d.claimLatency.Snapshot()
	markBuckets, markCount, markSumMs := d.markLatency.Snapshot()
	tickBuckets, tickCount, tickSumMs := d.tickLatency.Snapshot()
	aiPubBuckets, aiPubCount, aiPubSumMs := d.aiToPublishLatency.Snapshot()
	commitPubBuckets, commitPubCount, commitPubSumMs := d.commitToPublish.Snapshot()
	return DispatcherStats{
		LeaseAcquireAttempts:   atomic.LoadUint64(&d.leaseAcquireAttempts),
		LeaseAcquireSuccesses:  atomic.LoadUint64(&d.leaseAcquireSuccess),
		LeaseAcquireErrors:     atomic.LoadUint64(&d.leaseAcquireErrors),
		LeaseHeld:              atomic.LoadUint32(&d.leaseHeld) == 1,
		LastLeaseEpoch:         atomic.LoadInt64(&d.lastLeaseEpoch),
		TicksTotal:             atomic.LoadUint64(&d.ticksTotal),
		RowsClaimedTotal:       atomic.LoadUint64(&d.rowsClaimedTotal),
		CurrentBatchSize:       int(atomic.LoadUint64(&d.currentBatchSize)),
		BatchScaleUpTotal:      atomic.LoadUint64(&d.batchScaleUpTotal),
		BatchScaleDownTotal:    atomic.LoadUint64(&d.batchScaleDownTotal),
		PublishQueueWaits:      atomic.LoadUint64(&d.publishQueueWaits),
		MarkQueueWaits:         atomic.LoadUint64(&d.markQueueWaits),
		PublishedTotal:         atomic.LoadUint64(&d.publishedTotal),
		RetryTotal:             atomic.LoadUint64(&d.retryTotal),
		TerminalFailedTotal:    atomic.LoadUint64(&d.terminalTotal),
		FencedUpdateFailures:   atomic.LoadUint64(&d.fencedFailures),
		ThrottledLogsTotal:     atomic.LoadUint64(&d.throttledLogs),
		PublishLatencyBuckets:  pubBuckets,
		PublishLatencyCount:    pubCount,
		PublishLatencySumMs:    pubSumMs,
		PublishLatencyP95Ms:    d.publishLatency.Quantile(0.95),
		ClaimLatencyBuckets:    claimBuckets,
		ClaimLatencyCount:      claimCount,
		ClaimLatencySumMs:      claimSumMs,
		ClaimLatencyP95Ms:      d.claimLatency.Quantile(0.95),
		MarkLatencyBuckets:     markBuckets,
		MarkLatencyCount:       markCount,
		MarkLatencySumMs:       markSumMs,
		MarkLatencyP95Ms:       d.markLatency.Quantile(0.95),
		TickLatencyBuckets:     tickBuckets,
		TickLatencyCount:       tickCount,
		TickLatencySumMs:       tickSumMs,
		TickLatencyP95Ms:       d.tickLatency.Quantile(0.95),
		AIToPublishBuckets:     aiPubBuckets,
		AIToPublishCount:       aiPubCount,
		AIToPublishSumMs:       aiPubSumMs,
		AIToPublishP95Ms:       d.aiToPublishLatency.Quantile(0.95),
		CommitToPublishBuckets: commitPubBuckets,
		CommitToPublishCount:   commitPubCount,
		CommitToPublishSumMs:   commitPubSumMs,
		CommitToPublishP95Ms:   d.commitToPublish.Quantile(0.95),
		SkewCorrectionsTotal:   atomic.LoadUint64(&d.skewCorrections),
	}
}

func (d *Dispatcher) shouldLog(_ string, last *int64) bool {
	now := time.Now().UnixNano()
	prev := atomic.LoadInt64(last)
	if prev == 0 || time.Duration(now-prev) >= d.cfg.LogThrottle {
		if atomic.CompareAndSwapInt64(last, prev, now) {
			return true
		}
	}
	atomic.AddUint64(&d.throttledLogs, 1)
	return false
}

func (d *Dispatcher) jitterDuration(base time.Duration) time.Duration {
	ratio := d.cfg.RetryJitterRatio
	if base <= 0 || ratio <= 0 {
		return base
	}
	// Fast deterministic jitter without shared RNG lock.
	jitterUnit := float64((time.Now().UnixNano()%2001)-1000) / 1000.0
	factor := 1.0 + (ratio * jitterUnit)
	if factor < 0.1 {
		factor = 0.1
	}
	out := time.Duration(float64(base) * factor)
	if out < time.Millisecond {
		return time.Millisecond
	}
	if out > d.cfg.RetryMaxBack && d.cfg.RetryMaxBack > 0 {
		return d.cfg.RetryMaxBack
	}
	if math.IsNaN(float64(out)) || math.IsInf(float64(out), 0) {
		return base
	}
	return out
}

func (d *Dispatcher) adjustBatchSize(current, claimed int, claimMs float64) {
	if !d.cfg.AdaptiveBatch {
		return
	}
	if current <= 0 {
		current = d.cfg.BatchSize
	}
	next := current

	// Scale up aggressively when claims are saturated and DB claim latency is healthy.
	if claimed >= current && claimMs <= 100 {
		next = int(float64(current)*1.25) + 1
	}
	// Scale down when queue is thin or claim latency starts to degrade.
	if claimed == 0 || claimed <= maxInt(1, current/2) || claimMs >= 250 {
		down := int(float64(current) * 0.8)
		if down >= current {
			down = current - 1
		}
		next = down
	}

	next = clampInt(next, d.cfg.BatchSizeMin, d.cfg.BatchSizeMax)
	if next == current {
		return
	}
	atomic.StoreUint64(&d.currentBatchSize, uint64(next))
	if next > current {
		atomic.AddUint64(&d.batchScaleUpTotal, 1)
	} else {
		atomic.AddUint64(&d.batchScaleDownTotal, 1)
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
