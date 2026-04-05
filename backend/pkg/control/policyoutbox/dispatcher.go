package policyoutbox

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/control/policytrace"
	"backend/pkg/utils"
)

type Publisher interface {
	PublishFromOutbox(ctx context.Context, height uint64, ts int64, raw []byte) (string, int32, int64, error)
}

type storeOps interface {
	TryAcquireLease(ctx context.Context, leaseKey, holderID string, ttl time.Duration) (bool, int64, error)
	ReleaseLease(ctx context.Context, leaseKey, holderID string, epoch int64) error
	ClaimPending(ctx context.Context, holderID string, epoch int64, limit int, reclaimAfter time.Duration, leaseKey string, dispatchShard string, dispatchShardCount int, shardCompat bool) ([]Row, error)
	MarkPublished(ctx context.Context, id, holderID string, epoch int64, leaseKey string, topic string, partition int32, offset int64) error
	MarkRetry(ctx context.Context, id, holderID string, epoch int64, retries int, nextRetryAt time.Time, errMsg string) error
	MarkTerminal(ctx context.Context, id, holderID string, epoch int64, retries int, errMsg string) error
}

type reclaimTimeoutCounter interface {
	ReclaimTimeoutsTotal() uint64
}

type Dispatcher struct {
	cfg    Config
	store  storeOps
	pub    Publisher
	logger *utils.Logger
	audit  *utils.AuditLogger
	holder string
	trace  *policytrace.Collector

	stopCh    chan struct{}
	doneCh    chan struct{}
	wakeCh    chan struct{}
	publishQ  chan publishTask
	markQ     chan markTask
	publishWG sync.WaitGroup
	markWG    sync.WaitGroup

	leaseAcquireAttempts uint64
	leaseAcquireSuccess  uint64
	leaseAcquireErrors   uint64
	leaseAcquireTimeouts uint64
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
	wakeSignals          uint64
	wakeSignalsDropped   uint64
	wakeQueueDepthTotal  uint64
	wakeQueueDepthSample uint64
	skewCorrections      uint64
	aiEventUnitFixes     uint64
	aiEventInvalid       uint64
	sourceEventUnitFixes uint64
	sourceEventInvalid   uint64
	claimTimeouts        uint64
	claimBudgetExhausted uint64
	markTimeouts         uint64
	leaseHeld            uint32
	lastLeaseEpoch       int64
	publishLatency       *utils.LatencyHistogram
	claimLatency         *utils.LatencyHistogram
	leaseAcquireLatency  *utils.LatencyHistogram
	markLatency          *utils.LatencyHistogram
	tickLatency          *utils.LatencyHistogram
	aiToPublishLatency   *utils.LatencyHistogram
	sourceToPublish      *utils.LatencyHistogram
	commitToPublish      *utils.LatencyHistogram
	outboxToClaimLatency *utils.LatencyHistogram
	wakeToClaimLatency   *utils.LatencyHistogram
	lastWakeSignalAtNs   int64
	lastActiveTickAtNs   int64
	lastLeaseErrLogAt    int64
	lastClaimErrLogAt    int64
	lastMarkErrLogAt     int64
	scopeMu              sync.Mutex
	leaseStateMu         sync.Mutex
	leaseState           map[string]leaseRenewState
	publishScopeTotals   map[string]uint64
	publishScopeRoutes   map[string]uint64
	publishScopeFallback uint64
	publishResultTotals  map[string]uint64
	publishPartitions    map[string]uint64
}

type leaseRenewState struct {
	Held      bool
	Epoch     int64
	RenewAt   time.Time
	ExpiresAt time.Time
}

type leasedShard struct {
	shard    string
	leaseKey string
	epoch    int64
	disabled bool
	// timeoutStrikes tracks consecutive timeout errors for this shard during
	// the current tick. After one retry we disable the shard for the rest of
	// the tick to prevent a single bad shard from degrading others.
	timeoutStrikes int
	// timedOutThisTick keeps timeout-prone shards out of fair-share budget
	// calculations so healthy shards are not penalized within the same tick.
	timedOutThisTick bool
}

type publishTask struct {
	row      Row
	deadline time.Time
	done     *sync.WaitGroup
}

func NewDispatcher(cfg Config, store storeOps, pub Publisher, logger *utils.Logger, audit *utils.AuditLogger, holderID string, trace *policytrace.Collector) (*Dispatcher, error) {
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
		cfg.PolicyTopic = "control.policy.v2"
	}
	if cfg.DispatchShardCount <= 0 {
		if cfg.ClusterShardBuckets > 1 {
			cfg.DispatchShardCount = cfg.ClusterShardBuckets
		} else {
			cfg.DispatchShardCount = 1
		}
	}
	if cfg.DispatchShardCount < 1 {
		cfg.DispatchShardCount = 1
	}
	if cfg.DispatchOwnerCount <= 0 {
		cfg.DispatchOwnerCount = 1
	}
	if cfg.DispatchOwnerCount == 1 {
		cfg.DispatchOwnerIndex = 0
	} else if cfg.DispatchOwnerIndex < 0 || cfg.DispatchOwnerIndex >= cfg.DispatchOwnerCount {
		// Invalid ownership index means "no ownership filter" to avoid accidental
		// global dispatch freeze from bad config.
		cfg.DispatchOwnerCount = 1
		cfg.DispatchOwnerIndex = 0
	}
	if cfg.MaxLeaseShards <= 0 {
		if cfg.DispatchShardCount > 1 {
			cfg.MaxLeaseShards = minInt(cfg.DispatchShardCount, 4)
		} else {
			cfg.MaxLeaseShards = 1
		}
	}
	if cfg.MaxLeaseShards > cfg.DispatchShardCount {
		cfg.MaxLeaseShards = cfg.DispatchShardCount
	}
	rawShardingMode := strings.TrimSpace(cfg.ClusterShardingMode)
	normalizedRouting, unknownMode := NormalizeRoutingOptions(RoutingOptions{
		ClusterShardingMode: cfg.ClusterShardingMode,
		ClusterShardBuckets: cfg.ClusterShardBuckets,
	})
	cfg.ClusterShardingMode = normalizedRouting.ClusterShardingMode
	cfg.ClusterShardBuckets = normalizedRouting.ClusterShardBuckets
	if unknownMode {
		if logger != nil {
			logger.Warn("Policy outbox dispatcher received unsupported CONTROL_POLICY_CLUSTER_SHARDING_MODE; disabling cluster sharding",
				utils.ZapString("mode", rawShardingMode))
		}
		if audit != nil {
			_ = audit.Warn("policy_cluster_sharding_mode_invalid", map[string]interface{}{
				"component": "dispatcher",
				"mode":      rawShardingMode,
			})
		}
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 10 * time.Second
	}
	if cfg.LeaseAcquireTimeout <= 0 {
		cfg.LeaseAcquireTimeout = minDurationBound(5*time.Second, maxDuration(1*time.Second, cfg.LeaseTTL/2))
	}
	if cfg.ClaimTimeout <= 0 {
		cfg.ClaimTimeout = minDurationBound(8*time.Second, maxDuration(2*time.Second, cfg.LeaseTTL))
	}
	if cfg.ReclaimAfter <= 0 {
		cfg.ReclaimAfter = maxDuration(3*cfg.LeaseTTL, 30*time.Second)
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 250 * time.Millisecond
	}
	if cfg.PollIntervalHot <= 0 {
		cfg.PollIntervalHot = 75 * time.Millisecond
	}
	if cfg.PollIntervalHot > cfg.PollInterval {
		cfg.PollIntervalHot = cfg.PollInterval
	}
	if cfg.PollIntervalHotWindow <= 0 {
		cfg.PollIntervalHotWindow = 2 * time.Second
	}
	if cfg.DrainMaxDuration <= 0 {
		cfg.DrainMaxDuration = 8 * time.Second
	}
	if cfg.WakeDrainMaxDuration <= 0 {
		cfg.WakeDrainMaxDuration = minDurationBound(200*time.Millisecond, cfg.DrainMaxDuration)
	}
	if cfg.WakeDrainMaxDuration > cfg.DrainMaxDuration {
		cfg.WakeDrainMaxDuration = cfg.DrainMaxDuration
	}
	if cfg.WakeDrainMaxTicks <= 0 {
		cfg.WakeDrainMaxTicks = 2
	}
	if cfg.WakeDrainMaxTicks > 8 {
		cfg.WakeDrainMaxTicks = 8
	}
	if cfg.WakeChannelSize <= 0 {
		cfg.WakeChannelSize = 2
	}
	if cfg.WakeChannelSize > 8 {
		cfg.WakeChannelSize = 8
	}
	if cfg.LeaseAcquireTimeout >= cfg.DrainMaxDuration {
		cfg.LeaseAcquireTimeout = maxDuration(500*time.Millisecond, cfg.DrainMaxDuration/6)
	}
	if cfg.ClaimTimeout >= cfg.DrainMaxDuration {
		cfg.ClaimTimeout = maxDuration(2*time.Second, cfg.DrainMaxDuration-500*time.Millisecond)
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
		cfg.MaxInFlight = 16
	}
	if cfg.MarkWorkers <= 0 {
		cfg.MarkWorkers = maxInt(1, cfg.MaxInFlight/2)
	}
	if cfg.InternalQueue <= 0 {
		cfg.InternalQueue = maxInt(64, cfg.MaxInFlight*4)
	}
	if cfg.DrainMaxBatches <= 0 {
		cfg.DrainMaxBatches = 8
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
	if logger != nil {
		logger.Info("Policy outbox shard compatibility mode configured",
			utils.ZapBool("dispatch_shard_compat", cfg.DispatchShardCompat))
	}

	return &Dispatcher{
		cfg:    cfg,
		store:  store,
		pub:    pub,
		logger: logger,
		audit:  audit,
		holder: holderID,
		trace:  trace,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		wakeCh: make(chan struct{}, cfg.WakeChannelSize),
		publishLatency: utils.NewLatencyHistogram([]float64{
			1, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 5000,
		}),
		claimLatency: utils.NewLatencyHistogram([]float64{
			1, 5, 10, 25, 50, 100, 250, 500, 1000,
		}),
		leaseAcquireLatency: utils.NewLatencyHistogram([]float64{
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
		sourceToPublish: utils.NewLatencyHistogram([]float64{
			10, 25, 50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000,
		}),
		commitToPublish: utils.NewLatencyHistogram([]float64{
			10, 25, 50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000,
		}),
		outboxToClaimLatency: utils.NewLatencyHistogram([]float64{
			10, 25, 50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000,
		}),
		wakeToClaimLatency: utils.NewLatencyHistogram([]float64{
			1, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 5000,
		}),
		currentBatchSize:    uint64(cfg.BatchSize),
		leaseState:          make(map[string]leaseRenewState),
		publishScopeTotals:  make(map[string]uint64),
		publishScopeRoutes:  make(map[string]uint64),
		publishResultTotals: make(map[string]uint64),
		publishPartitions:   make(map[string]uint64),
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
	d.startPublishWorkers(ctx)
	d.startMarkWorkers(ctx)
	defer d.stopPublishWorkers()
	defer d.stopMarkWorkers()

	for {
		// Ticks are intentionally non-overlapping. If tick processing runs longer
		// than the poll interval, the next timer wake is serviced immediately after
		// the current tick returns. This keeps claim/drain state single-threaded
		// while preserving poll fallback when wake notifications are coalesced.
		interval := d.currentPollInterval()
		timer := time.NewTimer(interval)
		select {
		case <-d.stopCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
			d.tick(ctx, 0)
		case <-d.wakeCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			wakeAt := atomic.SwapInt64(&d.lastWakeSignalAtNs, 0)
			d.drainOnWake(ctx, wakeAt)
		}
	}
}

func (d *Dispatcher) startPublishWorkers(ctx context.Context) {
	publishWorkers := d.cfg.MaxInFlight
	if publishWorkers <= 0 {
		publishWorkers = 1
	}
	qSize := d.cfg.InternalQueue
	if qSize <= 0 {
		qSize = maxInt(64, publishWorkers*4)
	}
	d.publishQ = make(chan publishTask, qSize)
	for i := 0; i < publishWorkers; i++ {
		d.publishWG.Add(1)
		go func() {
			defer d.publishWG.Done()
			for task := range d.publishQ {
				func() {
					defer func() {
						if task.done != nil {
							task.done.Done()
						}
					}()
					mark, ok := d.publishRow(ctx, task.row, task.deadline)
					if !ok {
						return
					}
					mark.deadline = task.deadline
					select {
					case d.markQ <- mark:
					default:
						atomic.AddUint64(&d.markQueueWaits, 1)
						d.markQ <- mark
					}
				}()
			}
		}()
	}
}

func (d *Dispatcher) stopPublishWorkers() {
	if d.publishQ == nil {
		return
	}
	close(d.publishQ)
	d.publishWG.Wait()
}

func (d *Dispatcher) startMarkWorkers(ctx context.Context) {
	markWorkers := d.cfg.MarkWorkers
	if markWorkers <= 0 {
		markWorkers = 1
	}
	qSize := d.cfg.InternalQueue
	if qSize <= 0 {
		qSize = maxInt(64, maxInt(1, d.cfg.MaxInFlight)*4)
	}
	d.markQ = make(chan markTask, qSize)
	for i := 0; i < markWorkers; i++ {
		d.markWG.Add(1)
		go func() {
			defer d.markWG.Done()
			for task := range d.markQ {
				d.handleMarkTask(ctx, task, task.deadline)
			}
		}()
	}
}

func (d *Dispatcher) stopMarkWorkers() {
	if d.markQ == nil {
		return
	}
	close(d.markQ)
	d.markWG.Wait()
}

// Notify wakes the dispatcher loop for immediate claim/drain.
// The signal is coalesced to avoid storms under burst commits.
func (d *Dispatcher) Notify() {
	if d == nil {
		return
	}
	depth := len(d.wakeCh)
	atomic.AddUint64(&d.wakeQueueDepthTotal, uint64(depth))
	atomic.AddUint64(&d.wakeQueueDepthSample, 1)
	select {
	case d.wakeCh <- struct{}{}:
		atomic.StoreInt64(&d.lastWakeSignalAtNs, time.Now().UnixNano())
		atomic.AddUint64(&d.wakeSignals, 1)
	default:
		atomic.AddUint64(&d.wakeSignalsDropped, 1)
	}
}

type tickOutcome struct {
	claimedRows int
}

func (d *Dispatcher) currentPollInterval() time.Duration {
	base := d.cfg.PollInterval
	if base <= 0 {
		base = 250 * time.Millisecond
	}
	hot := d.cfg.PollIntervalHot
	if hot <= 0 || hot > base {
		hot = base
	}
	window := d.cfg.PollIntervalHotWindow
	if window <= 0 {
		return base
	}
	lastActive := atomic.LoadInt64(&d.lastActiveTickAtNs)
	if lastActive == 0 {
		return base
	}
	if time.Since(time.Unix(0, lastActive)) <= window {
		return hot
	}
	return base
}

func (d *Dispatcher) drainOnWake(ctx context.Context, wakeAtNs int64) {
	maxTicks := d.cfg.WakeDrainMaxTicks
	if maxTicks <= 0 {
		maxTicks = 1
	}
	drainBudget := d.cfg.WakeDrainMaxDuration
	if drainBudget <= 0 {
		drainBudget = 150 * time.Millisecond
	}
	start := time.Now()
	for i := 0; i < maxTicks; i++ {
		outcome := d.tick(ctx, wakeAtNs)
		if outcome.claimedRows <= 0 {
			return
		}
		if time.Since(start) >= drainBudget {
			return
		}
	}
}

func (d *Dispatcher) leaseRenewInterval() time.Duration {
	ttl := d.cfg.LeaseTTL
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	interval := ttl / 3
	if interval < 500*time.Millisecond {
		interval = 500 * time.Millisecond
	}
	if interval > ttl-time.Second {
		interval = maxDuration(500*time.Millisecond, ttl/2)
	}
	return interval
}

func (d *Dispatcher) getCachedLeaseEpoch(leaseKey string, now time.Time) (int64, bool) {
	d.leaseStateMu.Lock()
	defer d.leaseStateMu.Unlock()
	st, ok := d.leaseState[leaseKey]
	if !ok || !st.Held {
		return 0, false
	}
	if !st.ExpiresAt.IsZero() && now.After(st.ExpiresAt.Add(-500*time.Millisecond)) {
		delete(d.leaseState, leaseKey)
		return 0, false
	}
	if st.RenewAt.IsZero() || !now.Before(st.RenewAt) {
		return 0, false
	}
	return st.Epoch, true
}

func (d *Dispatcher) getFallbackLeaseEpochAfterRenewError(leaseKey string, now time.Time) (int64, bool) {
	d.leaseStateMu.Lock()
	defer d.leaseStateMu.Unlock()
	st, ok := d.leaseState[leaseKey]
	if !ok || !st.Held {
		return 0, false
	}
	if !st.ExpiresAt.IsZero() && now.Before(st.ExpiresAt.Add(-500*time.Millisecond)) {
		remaining := time.Until(st.ExpiresAt)
		retryAfter := 500 * time.Millisecond
		if remaining > 2*time.Second {
			retryAfter = time.Second
		}
		st.RenewAt = now.Add(retryAfter)
		d.leaseState[leaseKey] = st
		return st.Epoch, true
	}
	delete(d.leaseState, leaseKey)
	return 0, false
}

func (d *Dispatcher) recordLeaseRenewed(leaseKey string, epoch int64, now time.Time) {
	d.leaseStateMu.Lock()
	defer d.leaseStateMu.Unlock()
	renew := now.Add(d.leaseRenewInterval())
	expiry := now.Add(d.cfg.LeaseTTL)
	d.leaseState[leaseKey] = leaseRenewState{
		Held:      true,
		Epoch:     epoch,
		RenewAt:   renew,
		ExpiresAt: expiry,
	}
}

func (d *Dispatcher) forceLeaseRefresh(leaseKey string) {
	if leaseKey == "" {
		return
	}
	d.leaseStateMu.Lock()
	defer d.leaseStateMu.Unlock()
	st, ok := d.leaseState[leaseKey]
	if !ok {
		return
	}
	st.RenewAt = time.Time{}
	d.leaseState[leaseKey] = st
}

func (d *Dispatcher) clearLeaseState(leaseKey string) {
	if leaseKey == "" {
		return
	}
	d.leaseStateMu.Lock()
	defer d.leaseStateMu.Unlock()
	delete(d.leaseState, leaseKey)
}

func (d *Dispatcher) releaseLeaseBestEffort(ctx context.Context, leaseKey string, epoch int64, deadline time.Time, shard string) {
	if d == nil || d.store == nil || leaseKey == "" || epoch <= 0 {
		return
	}
	releaseCtx, cancel := context.WithTimeout(ctx, d.leaseAcquireTimeout(deadline))
	defer cancel()
	if err := d.store.ReleaseLease(releaseCtx, leaseKey, d.holder, epoch); err != nil {
		if d.logger != nil && d.shouldLog("lease", &d.lastLeaseErrLogAt) {
			d.logger.WarnContext(ctx, "policy outbox lease release failed",
				utils.ZapError(err),
				utils.ZapString("dispatch_shard", shard))
		}
		return
	}
	d.clearLeaseState(leaseKey)
}

func (d *Dispatcher) tick(ctx context.Context, wakeAtNs int64) tickOutcome {
	tickStart := time.Now()
	outcome := tickOutcome{}
	defer func() {
		atomic.AddUint64(&d.ticksTotal, 1)
		d.tickLatency.Observe(float64(time.Since(tickStart).Milliseconds()))
	}()

	if d.cfg.RefreshSafeMode != nil {
		if enabled, err := d.cfg.RefreshSafeMode(ctx); err == nil && d.cfg.DispatchSafeMode != nil {
			d.cfg.DispatchSafeMode.Store(enabled)
		}
	}

	if d.cfg.DispatchSafeMode != nil && d.cfg.DispatchSafeMode.Load() {
		atomic.StoreUint32(&d.leaseHeld, 0)
		return outcome
	}

	deadline := tickStart.Add(d.cfg.DrainMaxDuration)
	heldShards := d.acquireLeasedShards(ctx, deadline)
	held := len(heldShards)

	for batch := 0; batch < d.cfg.DrainMaxBatches; batch++ {
		if d.cfg.DrainMaxDuration > 0 && time.Now().After(deadline) {
			break
		}
		claimedThisRound := 0
		timeoutThisRound := false
		for i := range heldShards {
			if heldShards[i].disabled {
				continue
			}
			if d.cfg.DrainMaxDuration > 0 && time.Now().After(deadline) {
				break
			}

			batchSize := int(atomic.LoadUint64(&d.currentBatchSize))
			if batchSize <= 0 {
				batchSize = d.cfg.BatchSize
			}
			activeShards := 0
			for j := range heldShards {
				if heldShards[j].disabled {
					continue
				}
				if j != i && heldShards[j].timedOutThisTick {
					continue
				}
				if !heldShards[j].disabled {
					activeShards++
				}
			}
			claimBudget := d.claimTimeoutForShard(deadline, activeShards)
			claimStart := time.Now()
			claimCtx, claimCancel := context.WithTimeout(ctx, claimBudget)
			rows, err := d.store.ClaimPending(claimCtx, d.holder, heldShards[i].epoch, batchSize, d.cfg.ReclaimAfter, heldShards[i].leaseKey, heldShards[i].shard, d.cfg.DispatchShardCount, d.cfg.DispatchShardCompat)
			claimCancel()
			claimMs := float64(time.Since(claimStart).Milliseconds())
			d.claimLatency.Observe(claimMs)
			if err != nil {
				timeoutErr := isTimeoutErr(err)
				budgetExhaustedErr := errors.Is(err, errClaimBudgetExhausted)
				connectErr := isClaimConnectErr(err)
				tickRemaining := time.Until(deadline)
				zeroBudget := tickRemaining <= 0
				if timeoutErr && !budgetExhaustedErr {
					atomic.AddUint64(&d.claimTimeouts, 1)
					timeoutThisRound = true
					heldShards[i].timeoutStrikes++
					heldShards[i].timedOutThisTick = true
				}
				if budgetExhaustedErr {
					atomic.AddUint64(&d.claimBudgetExhausted, 1)
				}
				if d.logger != nil && d.shouldLog("claim", &d.lastClaimErrLogAt) {
					d.logger.WarnContext(ctx, "policy outbox claim failed",
						utils.ZapError(err),
						utils.ZapString("dispatch_shard", heldShards[i].shard),
						utils.ZapDuration("claim_timeout_budget", claimBudget),
						utils.ZapDuration("tick_remaining", tickRemaining),
						utils.ZapBool("zero_budget", zeroBudget),
						utils.ZapBool("timeout_error", timeoutErr),
						utils.ZapBool("connect_error", connectErr),
						utils.ZapBool("budget_exhausted", budgetExhaustedErr),
					)
				}
				d.adjustBatchSize(batchSize, 0, claimMs)
				if connectErr {
					// Cloud DB connect pressure should not trigger same-tick retries
					// for this shard; cooldown immediately and release lease so other
					// holders can make progress when DB pressure subsides.
					heldShards[i].disabled = true
					d.releaseLeaseBestEffort(ctx, heldShards[i].leaseKey, heldShards[i].epoch, deadline, heldShards[i].shard)
					continue
				}
				if budgetExhaustedErr {
					// The shard-level phase budget is exhausted for this tick; avoid
					// retry spin across remaining batches and retry on next poll tick.
					heldShards[i].disabled = true
					continue
				}
				// Keep timeout shards eligible in later rounds of the same tick so
				// one transient timeout does not strand the shard for a full poll cycle.
				if !timeoutErr {
					heldShards[i].disabled = true
					d.forceLeaseRefresh(heldShards[i].leaseKey)
					continue
				}
				// Allow one retry for timeout errors, then disable this shard for
				// the remainder of the tick to protect healthy shards. Also release
				// the lease best-effort so another holder can take over this shard
				// if this holder is distressed.
				if heldShards[i].timeoutStrikes >= 2 {
					heldShards[i].disabled = true
					d.releaseLeaseBestEffort(ctx, heldShards[i].leaseKey, heldShards[i].epoch, deadline, heldShards[i].shard)
				}
				continue
			}
			heldShards[i].timeoutStrikes = 0
			heldShards[i].timedOutThisTick = false
			d.adjustBatchSize(batchSize, len(rows), claimMs)
			if len(rows) == 0 {
				continue
			}
			claimedThisRound += len(rows)
			outcome.claimedRows += len(rows)
			atomic.AddUint64(&d.rowsClaimedTotal, uint64(len(rows)))
			nowMs := time.Now().UnixMilli()
			for _, row := range rows {
				if row.CreatedAtMs > 0 {
					delayMs := nowMs - row.CreatedAtMs
					if delayMs < 0 {
						delayMs = 0
					}
					d.outboxToClaimLatency.Observe(float64(delayMs))
				}
				d.recordStageMarker("t_outbox_claimed", row, nowMs, nil, nil)
			}
			d.processBatch(ctx, rows, deadline)
		}
		if claimedThisRound == 0 && !timeoutThisRound {
			break
		}
	}

	if held > 0 {
		atomic.StoreUint32(&d.leaseHeld, 1)
	} else {
		atomic.StoreUint32(&d.leaseHeld, 0)
	}
	if outcome.claimedRows > 0 {
		atomic.StoreInt64(&d.lastActiveTickAtNs, time.Now().UnixNano())
	}
	if wakeAtNs > 0 && outcome.claimedRows > 0 {
		delay := time.Since(time.Unix(0, wakeAtNs))
		if delay >= 0 {
			d.wakeToClaimLatency.Observe(float64(delay.Milliseconds()))
		}
	}
	return outcome
}

func (d *Dispatcher) acquireLeasedShards(ctx context.Context, deadline time.Time) []leasedShard {
	maxHeld := d.cfg.MaxLeaseShards
	if maxHeld <= 0 {
		maxHeld = 1
	}
	heldShards := make([]leasedShard, 0, maxHeld)
	pending := make([]leasedShard, 0, maxHeld)

	for _, shard := range d.dispatchShardsForTick() {
		if d.cfg.DrainMaxDuration > 0 && time.Now().After(deadline) {
			break
		}
		if len(heldShards)+len(pending) >= maxHeld {
			break
		}
		leaseKey := d.cfg.LeaseKey
		if d.cfg.DispatchShardCount > 1 {
			leaseKey = leaseKey + ":" + shard
		}
		now := time.Now()
		if epoch, ok := d.getCachedLeaseEpoch(leaseKey, now); ok {
			heldShards = append(heldShards, leasedShard{
				shard:    shard,
				leaseKey: leaseKey,
				epoch:    epoch,
			})
			continue
		}
		pending = append(pending, leasedShard{
			shard:    shard,
			leaseKey: leaseKey,
		})
	}
	if len(pending) == 0 || len(heldShards) >= maxHeld {
		return heldShards
	}

	type leaseAcquireResult struct {
		leaseKey string
		shard    string
		epoch    int64
		ok       bool
	}
	resultsCh := make(chan leaseAcquireResult, len(pending))
	var wg sync.WaitGroup
	for _, job := range pending {
		wg.Add(1)
		go func(job leasedShard) {
			defer wg.Done()
			atomic.AddUint64(&d.leaseAcquireAttempts, 1)
			leaseCtx, leaseCancel := context.WithTimeout(ctx, d.leaseAcquireTimeout(deadline))
			leaseStart := time.Now()
			acquired, epoch, err := d.store.TryAcquireLease(leaseCtx, job.leaseKey, d.holder, d.cfg.LeaseTTL)
			d.leaseAcquireLatency.Observe(float64(time.Since(leaseStart).Milliseconds()))
			leaseCancel()
			if err != nil {
				atomic.AddUint64(&d.leaseAcquireErrors, 1)
				if isTimeoutErr(err) {
					atomic.AddUint64(&d.leaseAcquireTimeouts, 1)
				}
				if d.logger != nil && d.shouldLog("lease", &d.lastLeaseErrLogAt) {
					d.logger.WarnContext(ctx, "policy outbox lease acquire failed", utils.ZapError(err), utils.ZapString("dispatch_shard", job.shard))
				}
				if fallbackEpoch, ok := d.getFallbackLeaseEpochAfterRenewError(job.leaseKey, time.Now()); ok {
					resultsCh <- leaseAcquireResult{
						leaseKey: job.leaseKey,
						shard:    job.shard,
						epoch:    fallbackEpoch,
						ok:       true,
					}
				}
				return
			}
			if !acquired {
				d.clearLeaseState(job.leaseKey)
				return
			}
			atomic.AddUint64(&d.leaseAcquireSuccess, 1)
			atomic.StoreInt64(&d.lastLeaseEpoch, epoch)
			d.recordLeaseRenewed(job.leaseKey, epoch, time.Now())
			resultsCh <- leaseAcquireResult{
				leaseKey: job.leaseKey,
				shard:    job.shard,
				epoch:    epoch,
				ok:       true,
			}
		}(job)
	}
	wg.Wait()
	close(resultsCh)

	acquiredByKey := make(map[string]leaseAcquireResult, len(pending))
	for res := range resultsCh {
		if !res.ok {
			continue
		}
		acquiredByKey[res.leaseKey] = res
	}
	for _, job := range pending {
		if len(heldShards) >= maxHeld {
			break
		}
		res, ok := acquiredByKey[job.leaseKey]
		if !ok || !res.ok {
			continue
		}
		heldShards = append(heldShards, leasedShard{
			shard:    res.shard,
			leaseKey: res.leaseKey,
			epoch:    res.epoch,
		})
	}
	return heldShards
}

func (d *Dispatcher) processBatch(ctx context.Context, rows []Row, deadline time.Time) {
	if len(rows) == 0 {
		return
	}

	if d.publishQ != nil && d.markQ != nil {
		var done sync.WaitGroup
		done.Add(len(rows))
		for _, row := range rows {
			task := publishTask{
				row:      row,
				deadline: deadline,
				done:     &done,
			}
			select {
			case d.publishQ <- task:
			default:
				atomic.AddUint64(&d.publishQueueWaits, 1)
				d.publishQ <- task
			}
		}
		done.Wait()
		return
	}

	// Local fallback keeps tests that call processBatch directly deterministic.
	for _, row := range rows {
		task, ok := d.publishRow(ctx, row, deadline)
		if !ok {
			continue
		}
		task.deadline = deadline
		if d.markQ != nil {
			select {
			case d.markQ <- task:
			default:
				atomic.AddUint64(&d.markQueueWaits, 1)
				d.markQ <- task
			}
			continue
		}
		d.handleMarkTask(ctx, task, deadline)
	}
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
	deadline    time.Time
	publishAck  int64
	retries     int
	nextRetryAt time.Time
	errMsg      string
	pubStart    time.Time
}

func (d *Dispatcher) publishRow(ctx context.Context, row Row, deadline time.Time) (markTask, bool) {
	opCtx := ctx
	cancel := func() {}
	if d.cfg.DrainMaxDuration > 0 {
		opCtx, cancel = context.WithTimeout(ctx, d.timeoutFromDeadline(deadline))
	}
	defer cancel()

	start := time.Now()
	d.recordStageMarker("t_control_publish_start", row, start.UnixMilli(), nil, nil)
	scopeKind, scopeIdentifier, scopeFallback := "legacy", "", false
	scopeBucketed := false
	if d.cfg.ScopeRoutingEnabled {
		scopeKind, scopeIdentifier, scopeFallback = DeriveScopeIdentifierWithOptions(row.Payload, RoutingOptions{
			ClusterShardingMode: d.cfg.ClusterShardingMode,
			ClusterShardBuckets: d.cfg.ClusterShardBuckets,
		})
		scopeBucketed = scopeKind == "cluster" && scopeIdentifier != "cluster"
	}
	if d.logger != nil && row.PolicyID != "" {
		d.logger.Info("policy publish scope derived",
			utils.ZapString("policy_id", row.PolicyID),
			utils.ZapString("trace_id", row.TraceID),
			utils.ZapString("scope_kind", scopeKind),
			utils.ZapString("scope_identifier", scopeIdentifier),
			utils.ZapBool("scope_bucketed", scopeBucketed),
			utils.ZapBool("scope_fallback", scopeFallback))
	}
	_, partition, offset, err := d.pub.PublishFromOutbox(opCtx, row.BlockHeight, row.BlockTS, row.Payload)
	if err != nil {
		d.observePublishScope(scopeKind, "error", scopeBucketed, scopeFallback)
		d.observePublishResult(d.cfg.PolicyTopic, "error", nil)
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
	d.observePublishScope(scopeKind, "success", scopeBucketed, scopeFallback)
	d.observePublishResult(d.cfg.PolicyTopic, "success", &partition)
	publishAck := time.Now().UnixMilli()
	return markTask{
		kind:       markTaskPublished,
		row:        row,
		partition:  partition,
		offset:     offset,
		publishAck: publishAck,
		pubStart:   start,
	}, true
}

func (d *Dispatcher) observePublishScope(scopeKind, result string, bucketed bool, fallback bool) {
	if d == nil {
		return
	}
	if scopeKind == "" {
		scopeKind = "unknown"
	}
	if result == "" {
		result = "unknown"
	}
	key := scopeKind + ":" + result
	bucketedLabel := "false"
	if bucketed {
		bucketedLabel = "true"
	}
	routeKey := scopeKind + ":" + bucketedLabel + ":" + result
	d.scopeMu.Lock()
	d.publishScopeTotals[key]++
	d.publishScopeRoutes[routeKey]++
	if fallback {
		d.publishScopeFallback++
	}
	d.scopeMu.Unlock()
}

func (d *Dispatcher) observePublishResult(topic, result string, partition *int32) {
	if d == nil {
		return
	}
	if topic == "" {
		topic = "unknown"
	}
	if result == "" {
		result = "unknown"
	}
	partitionLabel := "unknown"
	if partition != nil {
		partitionLabel = fmt.Sprintf("%d", *partition)
	}
	resultKey := topic + ":" + result
	partitionKey := topic + ":" + partitionLabel + ":" + result
	d.scopeMu.Lock()
	d.publishResultTotals[resultKey]++
	d.publishPartitions[partitionKey]++
	d.scopeMu.Unlock()
}

func (d *Dispatcher) recordStageMarker(stage string, row Row, tsMs int64, partition *int32, offset *int64) {
	if d == nil || row.PolicyID == "" {
		return
	}
	if d.logger != nil {
		if partition != nil && offset != nil {
			d.logger.Info("policy stage marker",
				utils.ZapString("stage", stage),
				utils.ZapString("policy_id", row.PolicyID),
				utils.ZapString("trace_id", row.TraceID),
				utils.ZapInt64("t_ms", tsMs),
				utils.ZapString("outbox_id", row.ID),
				utils.ZapUint64("height", row.BlockHeight),
				utils.ZapInt32("partition", *partition),
				utils.ZapInt64("offset", *offset))
		} else {
			d.logger.Info("policy stage marker",
				utils.ZapString("stage", stage),
				utils.ZapString("policy_id", row.PolicyID),
				utils.ZapString("trace_id", row.TraceID),
				utils.ZapInt64("t_ms", tsMs),
				utils.ZapString("outbox_id", row.ID),
				utils.ZapUint64("height", row.BlockHeight))
		}
	}
	if d.trace != nil {
		marker := policytrace.Marker{
			Stage:       stage,
			PolicyID:    row.PolicyID,
			TraceID:     row.TraceID,
			TimestampMs: tsMs,
			Height:      row.BlockHeight,
			OutboxID:    row.ID,
		}
		if partition != nil {
			marker.Partition = *partition
		}
		if offset != nil {
			marker.Offset = *offset
		}
		d.trace.Record(marker)
	}
}

func (d *Dispatcher) handleMarkTask(ctx context.Context, task markTask, deadline time.Time) {
	opCtx := ctx
	cancel := func() {}
	if d.cfg.DrainMaxDuration > 0 {
		opCtx, cancel = context.WithTimeout(ctx, d.markTimeout(deadline))
	}
	defer cancel()

	switch task.kind {
	case markTaskPublished:
		markStart := time.Now()
		markCtx, markCancel := context.WithTimeout(ctx, d.markPublishBudget(deadline))
		err := d.store.MarkPublished(markCtx, task.row.ID, d.holder, task.row.LeaseEpoch, d.leaseKeyForRow(task.row), d.cfg.PolicyTopic, task.partition, task.offset)
		markCancel()
		d.markLatency.Observe(float64(time.Since(markStart).Milliseconds()))
		if err != nil {
			if strings.Contains(err.Error(), "fenced/no-op") {
				atomic.AddUint64(&d.fencedFailures, 1)
				d.forceLeaseRefresh(d.leaseKeyForRow(task.row))
				return
			}
			if isTimeoutErr(err) {
				atomic.AddUint64(&d.markTimeouts, 1)
			}
			if d.logger != nil && d.shouldLog("mark", &d.lastMarkErrLogAt) {
				d.logger.WarnContext(ctx, "policy outbox mark published failed",
					utils.ZapError(err),
					utils.ZapString("outbox_id", task.row.ID))
			}
			return
		}
		ackMs := task.publishAck
		if ackMs <= 0 {
			ackMs = markStart.UnixMilli()
		}
		markDoneMs := time.Now().UnixMilli()
		d.recordStageMarker("t_control_publish_ack", task.row, ackMs, &task.partition, &task.offset)
		d.recordStageMarker("t_outbox_mark_published_done", task.row, markDoneMs, &task.partition, &task.offset)
		d.publishLatency.Observe(float64(time.Since(task.pubStart).Milliseconds()))
		if task.row.BlockTS > 0 {
			commitStartMs := task.row.BlockTS * 1000
			if commitLatency, ok, negative := utils.DurationMillis(commitStartMs, time.Now().UnixMilli()); ok {
				d.commitToPublish.Observe(float64(commitLatency))
			} else if negative {
				atomic.AddUint64(&d.skewCorrections, 1)
				d.commitToPublish.Observe(0)
			}
		}
		if task.row.AIEventTsMs > 0 {
			aiTSMs, corrected, valid := utils.NormalizeEpochMillis(task.row.AIEventTsMs)
			if !valid {
				atomic.AddUint64(&d.aiEventInvalid, 1)
			} else {
				if corrected {
					atomic.AddUint64(&d.aiEventUnitFixes, 1)
				}
				if aiLatency, ok, negative := utils.DurationMillis(aiTSMs, time.Now().UnixMilli()); ok {
					d.aiToPublishLatency.Observe(float64(aiLatency))
				} else if negative {
					atomic.AddUint64(&d.skewCorrections, 1)
					d.aiToPublishLatency.Observe(0)
				}
			}
		}
		if task.row.SourceEventTsMs > 0 {
			sourceTSMs, corrected, valid := utils.NormalizeEpochMillis(task.row.SourceEventTsMs)
			if !valid {
				atomic.AddUint64(&d.sourceEventInvalid, 1)
			} else {
				if corrected {
					atomic.AddUint64(&d.sourceEventUnitFixes, 1)
				}
				if sourceLatency, ok, negative := utils.DurationMillis(sourceTSMs, time.Now().UnixMilli()); ok {
					d.sourceToPublish.Observe(float64(sourceLatency))
				} else if negative {
					atomic.AddUint64(&d.skewCorrections, 1)
					d.sourceToPublish.Observe(0)
				}
			}
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

func (d *Dispatcher) leaseKeyForRow(row Row) string {
	if d == nil {
		return ""
	}
	base := strings.TrimSpace(d.cfg.LeaseKey)
	if base == "" {
		base = "control.policy.dispatcher"
	}
	if d.cfg.DispatchShardCount <= 1 {
		return base
	}
	shard := strings.TrimSpace(row.DispatchShard)
	if shard == "" {
		shard = dispatchShardLabel(0)
	}
	return base + ":" + shard
}

// Stats returns a point-in-time dispatcher metrics snapshot.
func (d *Dispatcher) Stats() DispatcherStats {
	if d == nil {
		return DispatcherStats{}
	}
	pubBuckets, pubCount, pubSumMs := d.publishLatency.Snapshot()
	claimBuckets, claimCount, claimSumMs := d.claimLatency.Snapshot()
	leaseBuckets, leaseCount, leaseSumMs := d.leaseAcquireLatency.Snapshot()
	markBuckets, markCount, markSumMs := d.markLatency.Snapshot()
	tickBuckets, tickCount, tickSumMs := d.tickLatency.Snapshot()
	aiPubBuckets, aiPubCount, aiPubSumMs := d.aiToPublishLatency.Snapshot()
	sourcePubBuckets, sourcePubCount, sourcePubSumMs := d.sourceToPublish.Snapshot()
	commitPubBuckets, commitPubCount, commitPubSumMs := d.commitToPublish.Snapshot()
	outboxClaimBuckets, outboxClaimCount, outboxClaimSumMs := d.outboxToClaimLatency.Snapshot()
	wakeClaimBuckets, wakeClaimCount, wakeClaimSumMs := d.wakeToClaimLatency.Snapshot()
	d.scopeMu.Lock()
	scopeTotals := make(map[string]uint64, len(d.publishScopeTotals))
	for key, count := range d.publishScopeTotals {
		scopeTotals[key] = count
	}
	scopeRouteTotals := make(map[string]uint64, len(d.publishScopeRoutes))
	for key, count := range d.publishScopeRoutes {
		scopeRouteTotals[key] = count
	}
	scopeFallbacks := d.publishScopeFallback
	resultTotals := make(map[string]uint64, len(d.publishResultTotals))
	for key, count := range d.publishResultTotals {
		resultTotals[key] = count
	}
	partitionTotals := make(map[string]uint64, len(d.publishPartitions))
	for key, count := range d.publishPartitions {
		partitionTotals[key] = count
	}
	timeoutCauseTotals := map[string]uint64{
		"lease_acquire_timeout": atomic.LoadUint64(&d.leaseAcquireTimeouts),
		"claim_timeout":         atomic.LoadUint64(&d.claimTimeouts),
		"claim_budget_exhausted": atomic.LoadUint64(&d.claimBudgetExhausted),
		"mark_timeout":          atomic.LoadUint64(&d.markTimeouts),
		"wake_signal_dropped":   atomic.LoadUint64(&d.wakeSignalsDropped),
	}
	var reclaimTimeouts uint64
	if c, ok := d.store.(reclaimTimeoutCounter); ok {
		reclaimTimeouts = c.ReclaimTimeoutsTotal()
		timeoutCauseTotals["reclaim_timeout"] = reclaimTimeouts
	}
	d.scopeMu.Unlock()
	wakeDepthSamples := atomic.LoadUint64(&d.wakeQueueDepthSample)
	wakeDepthAvg := 0.0
	if wakeDepthSamples > 0 {
		wakeDepthAvg = float64(atomic.LoadUint64(&d.wakeQueueDepthTotal)) / float64(wakeDepthSamples)
	}
	return DispatcherStats{
		LeaseAcquireAttempts:          atomic.LoadUint64(&d.leaseAcquireAttempts),
		LeaseAcquireSuccesses:         atomic.LoadUint64(&d.leaseAcquireSuccess),
		LeaseAcquireErrors:            atomic.LoadUint64(&d.leaseAcquireErrors),
		LeaseAcquireTimeouts:          atomic.LoadUint64(&d.leaseAcquireTimeouts),
		LeaseHeld:                     atomic.LoadUint32(&d.leaseHeld) == 1,
		LastLeaseEpoch:                atomic.LoadInt64(&d.lastLeaseEpoch),
		TicksTotal:                    atomic.LoadUint64(&d.ticksTotal),
		WakeSignalsTotal:              atomic.LoadUint64(&d.wakeSignals),
		WakeSignalsDroppedTotal:       atomic.LoadUint64(&d.wakeSignalsDropped),
		WakeQueueDepth:                len(d.wakeCh),
		WakeQueueDepthSamples:         wakeDepthSamples,
		WakeQueueDepthAvg:             wakeDepthAvg,
		RowsClaimedTotal:              atomic.LoadUint64(&d.rowsClaimedTotal),
		CurrentBatchSize:              int(atomic.LoadUint64(&d.currentBatchSize)),
		BatchScaleUpTotal:             atomic.LoadUint64(&d.batchScaleUpTotal),
		BatchScaleDownTotal:           atomic.LoadUint64(&d.batchScaleDownTotal),
		PublishQueueWaits:             atomic.LoadUint64(&d.publishQueueWaits),
		MarkQueueWaits:                atomic.LoadUint64(&d.markQueueWaits),
		PublishedTotal:                atomic.LoadUint64(&d.publishedTotal),
		RetryTotal:                    atomic.LoadUint64(&d.retryTotal),
		TerminalFailedTotal:           atomic.LoadUint64(&d.terminalTotal),
		FencedUpdateFailures:          atomic.LoadUint64(&d.fencedFailures),
		ThrottledLogsTotal:            atomic.LoadUint64(&d.throttledLogs),
		PublishLatencyBuckets:         pubBuckets,
		PublishLatencyCount:           pubCount,
		PublishLatencySumMs:           pubSumMs,
		PublishLatencyP95Ms:           d.publishLatency.Quantile(0.95),
		ClaimLatencyBuckets:           claimBuckets,
		ClaimLatencyCount:             claimCount,
		ClaimLatencySumMs:             claimSumMs,
		ClaimLatencyP95Ms:             d.claimLatency.Quantile(0.95),
		ClaimTimeouts:                 atomic.LoadUint64(&d.claimTimeouts),
		ClaimBudgetExhausted:          atomic.LoadUint64(&d.claimBudgetExhausted),
		ReclaimTimeouts:               reclaimTimeouts,
		LeaseAcquireLatencyBuckets:    leaseBuckets,
		LeaseAcquireLatencyCount:      leaseCount,
		LeaseAcquireLatencySumMs:      leaseSumMs,
		LeaseAcquireLatencyP95Ms:      d.leaseAcquireLatency.Quantile(0.95),
		MarkLatencyBuckets:            markBuckets,
		MarkLatencyCount:              markCount,
		MarkLatencySumMs:              markSumMs,
		MarkLatencyP95Ms:              d.markLatency.Quantile(0.95),
		MarkTimeouts:                  atomic.LoadUint64(&d.markTimeouts),
		TickLatencyBuckets:            tickBuckets,
		TickLatencyCount:              tickCount,
		TickLatencySumMs:              tickSumMs,
		TickLatencyP95Ms:              d.tickLatency.Quantile(0.95),
		AIToPublishBuckets:            aiPubBuckets,
		AIToPublishCount:              aiPubCount,
		AIToPublishSumMs:              aiPubSumMs,
		AIToPublishP95Ms:              d.aiToPublishLatency.Quantile(0.95),
		SourceToPublishBuckets:        sourcePubBuckets,
		SourceToPublishCount:          sourcePubCount,
		SourceToPublishSumMs:          sourcePubSumMs,
		SourceToPublishP95Ms:          d.sourceToPublish.Quantile(0.95),
		CommitToPublishBuckets:        commitPubBuckets,
		CommitToPublishCount:          commitPubCount,
		CommitToPublishSumMs:          commitPubSumMs,
		CommitToPublishP95Ms:          d.commitToPublish.Quantile(0.95),
		OutboxCreatedToClaimedBuckets: outboxClaimBuckets,
		OutboxCreatedToClaimedCount:   outboxClaimCount,
		OutboxCreatedToClaimedSumMs:   outboxClaimSumMs,
		OutboxCreatedToClaimedP95Ms:   d.outboxToClaimLatency.Quantile(0.95),
		WakeToClaimBuckets:            wakeClaimBuckets,
		WakeToClaimCount:              wakeClaimCount,
		WakeToClaimSumMs:              wakeClaimSumMs,
		WakeToClaimP95Ms:              d.wakeToClaimLatency.Quantile(0.95),
		SkewCorrectionsTotal:          atomic.LoadUint64(&d.skewCorrections),
		AIEventUnitCorrections:        atomic.LoadUint64(&d.aiEventUnitFixes),
		AIEventInvalidTotal:           atomic.LoadUint64(&d.aiEventInvalid),
		SourceEventUnitCorrections:    atomic.LoadUint64(&d.sourceEventUnitFixes),
		SourceEventInvalidTotal:       atomic.LoadUint64(&d.sourceEventInvalid),
		PublishScopeResultTotals:      scopeTotals,
		PublishScopeRouteTotals:       scopeRouteTotals,
		PublishScopeFallbacks:         scopeFallbacks,
		PublishResultTotals:           resultTotals,
		PublishPartitionTotals:        partitionTotals,
		TimeoutCauseTotals:            timeoutCauseTotals,
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
	jitterUnit := (rand.Float64() * 2.0) - 1.0
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

func (d *Dispatcher) timeoutFromDeadline(deadline time.Time) time.Duration {
	fallback := d.cfg.RetryInitialBack
	if fallback <= 0 {
		fallback = 250 * time.Millisecond
	}
	if fallback < 100*time.Millisecond {
		fallback = 100 * time.Millisecond
	}
	if fallback > 2*time.Second {
		fallback = 2 * time.Second
	}
	if deadline.IsZero() {
		return fallback
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return fallback
	}
	return remaining
}

func (d *Dispatcher) leaseAcquireTimeout(deadline time.Time) time.Duration {
	timeout := d.phaseBudget(
		d.cfg.LeaseAcquireTimeout,
		d.leaseAcquireLatency.Quantile(0.95),
		300*time.Millisecond,
		8*time.Second,
		100*time.Millisecond,
		deadline,
	)
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if deadline.IsZero() {
		return timeout
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return minDurationBound(timeout, 250*time.Millisecond)
	}
	return minDurationBound(timeout, remaining)
}

func (d *Dispatcher) claimTimeout(deadline time.Time) time.Duration {
	timeout := d.phaseBudget(
		d.cfg.ClaimTimeout,
		d.claimLatency.Quantile(0.95),
		400*time.Millisecond,
		8*time.Second,
		150*time.Millisecond,
		deadline,
	)
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if deadline.IsZero() {
		return timeout
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return minDurationBound(timeout, 250*time.Millisecond)
	}
	return minDurationBound(timeout, remaining)
}

func (d *Dispatcher) claimTimeoutForShard(deadline time.Time, activeShards int) time.Duration {
	timeout := d.claimTimeout(deadline)
	if activeShards <= 1 || deadline.IsZero() {
		return timeout
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return minDurationBound(timeout, 250*time.Millisecond)
	}
	// Preserve per-shard fairness in a sequential claim loop so one slow shard
	// cannot consume most of the tick and delay claim turns for others.
	fairShare := remaining / time.Duration(activeShards+1)
	if fairShare < 400*time.Millisecond {
		fairShare = 400 * time.Millisecond
	}
	if timeout > fairShare {
		return fairShare
	}
	return timeout
}

func (d *Dispatcher) markTimeout(deadline time.Time) time.Duration {
	timeout := d.phaseBudget(
		8*time.Second,
		d.markLatency.Quantile(0.95),
		3*time.Second,
		30*time.Second,
		250*time.Millisecond,
		deadline,
	)
	// Marking publish/terminal state is the durable completion edge. Give it a
	// small minimum budget so a near-expired tick deadline does not strand rows
	// in publishing after Kafka already accepted the payload.
	if timeout < 8*time.Second {
		timeout = 8 * time.Second
	}
	if timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	return timeout
}

func (d *Dispatcher) markPublishBudget(deadline time.Time) time.Duration {
	budget := d.phaseBudget(
		30*time.Second,
		d.markLatency.Quantile(0.95),
		10*time.Second,
		60*time.Second,
		500*time.Millisecond,
		deadline,
	)
	if budget < 5*time.Second {
		budget = 5 * time.Second
	}
	if budget > 60*time.Second {
		budget = 60 * time.Second
	}
	if !deadline.IsZero() {
		remaining := time.Until(deadline)
		if remaining > 0 && budget > remaining {
			budget = remaining
		}
	}
	if budget < 500*time.Millisecond {
		budget = 500 * time.Millisecond
	}
	return budget
}

func (d *Dispatcher) phaseBudget(base time.Duration, p95Ms float64, minBudget time.Duration, maxBudget time.Duration, reserve time.Duration, deadline time.Time) time.Duration {
	if base <= 0 {
		base = minBudget
	}
	target := base
	if p95Ms > 0 && !math.IsNaN(p95Ms) && !math.IsInf(p95Ms, 0) {
		// Keep ~1.5x p95 + fixed headroom for jitter/conflicts.
		adaptive := time.Duration((p95Ms*1.5)+100) * time.Millisecond
		if adaptive > target {
			target = adaptive
		}
	}
	if target < minBudget {
		target = minBudget
	}
	if maxBudget > 0 && target > maxBudget {
		target = maxBudget
	}
	if !deadline.IsZero() {
		remaining := time.Until(deadline) - reserve
		if remaining <= 0 {
			remaining = minBudget
		}
		if target > remaining {
			target = remaining
		}
	}
	if target < minBudget {
		target = minBudget
	}
	if maxBudget > 0 && target > maxBudget {
		target = maxBudget
	}
	return target
}

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "timeout")
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (d *Dispatcher) dispatchShardsForTick() []string {
	count := d.cfg.DispatchShardCount
	if count <= 1 {
		return []string{dispatchShardLabel(0)}
	}
	start := 0
	if count > 0 {
		start = int(atomic.LoadUint64(&d.ticksTotal) % uint64(count))
	}
	shards := make([]string, 0, count)
	for i := 0; i < count; i++ {
		shard := (start + i) % count
		shards = append(shards, dispatchShardLabel(shard))
	}
	if d.cfg.DispatchOwnerCount > 1 {
		owned := make([]string, 0, len(shards))
		for _, shard := range shards {
			idx := parseDispatchShardIndex(shard)
			if idx < 0 {
				continue
			}
			if idx%d.cfg.DispatchOwnerCount == d.cfg.DispatchOwnerIndex {
				owned = append(owned, shard)
			}
		}
		if len(owned) > 0 {
			return owned
		}
	}
	return shards
}

func parseDispatchShardIndex(shard string) int {
	shard = strings.TrimSpace(shard)
	if !strings.HasPrefix(shard, "shard:") {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(shard, "shard:")))
	if err != nil || n < 0 {
		return -1
	}
	return n
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func minDurationBound(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
