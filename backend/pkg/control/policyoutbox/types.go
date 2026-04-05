package policyoutbox

import (
	"context"
	"sync/atomic"
	"time"

	"backend/pkg/utils"
)

// Row represents one durable policy publish intent.
type Row struct {
	ID              string
	DispatchShard   string
	BlockHeight     uint64
	BlockTS         int64
	CreatedAtMs     int64
	TxIndex         int
	PolicyID        string
	RequestID       string
	CommandID       string
	WorkflowID      string
	TraceID         string
	AIEventTsMs     int64
	SourceEventID   string
	SourceEventTsMs int64
	SentinelEventID string
	RuleHash        []byte
	Payload         []byte
	Status          string
	Retries         int
	LeaseEpoch      int64
}

// Config controls the dispatcher loop and retry behavior.
type Config struct {
	Enabled               bool
	LeaseKey              string
	PolicyTopic           string
	ScopeRoutingEnabled   bool
	DispatchOwnerCount    int
	DispatchOwnerIndex    int
	DispatchSafeMode      *atomic.Bool
	RefreshSafeMode       func(context.Context) (bool, error)
	ClusterShardingMode   string
	ClusterShardBuckets   int
	DispatchShardCount    int
	DispatchShardCompat   bool
	MaxLeaseShards        int
	LeaseTTL              time.Duration
	LeaseAcquireTimeout   time.Duration
	ClaimTimeout          time.Duration
	ReclaimAfter          time.Duration
	PollInterval          time.Duration
	PollIntervalHot       time.Duration
	PollIntervalHotWindow time.Duration
	DrainMaxDuration      time.Duration
	WakeDrainMaxDuration  time.Duration
	WakeDrainMaxTicks     int
	WakeChannelSize       int
	BatchSize             int
	BatchSizeMin          int
	BatchSizeMax          int
	AdaptiveBatch         bool
	MaxInFlight           int
	MarkWorkers           int
	InternalQueue         int
	DrainMaxBatches       int
	MaxRetries            int
	RetryInitialBack      time.Duration
	RetryMaxBack          time.Duration
	RetryJitterRatio      float64
	LogThrottle           time.Duration
}

// BacklogStats summarizes queue depth and age for publish-eligible rows.
type BacklogStats struct {
	Pending          int64
	Retry            int64
	Publishing       int64
	TotalRows        int64
	PublishedRows    int64
	AckedRows        int64
	TerminalRows     int64
	OldestPendingAge int64
	BacklogCacheAgeMs int64
}

// DispatcherStats captures runtime behavior of the outbox dispatcher.
type DispatcherStats struct {
	LeaseAcquireAttempts          uint64
	LeaseAcquireSuccesses         uint64
	LeaseAcquireErrors            uint64
	LeaseAcquireTimeouts          uint64
	LeaseHeld                     bool
	LastLeaseEpoch                int64
	TicksTotal                    uint64
	WakeSignalsTotal              uint64
	WakeSignalsDroppedTotal       uint64
	WakeQueueDepth                int
	WakeQueueDepthSamples         uint64
	WakeQueueDepthAvg             float64
	RowsClaimedTotal              uint64
	CurrentBatchSize              int
	BatchScaleUpTotal             uint64
	BatchScaleDownTotal           uint64
	PublishQueueWaits             uint64
	MarkQueueWaits                uint64
	PublishedTotal                uint64
	RetryTotal                    uint64
	TerminalFailedTotal           uint64
	FencedUpdateFailures          uint64
	ThrottledLogsTotal            uint64
	PublishLatencyBuckets         []utils.HistogramBucket
	PublishLatencyCount           uint64
	PublishLatencySumMs           float64
	PublishLatencyP95Ms           float64
	ClaimLatencyBuckets           []utils.HistogramBucket
	ClaimLatencyCount             uint64
	ClaimLatencySumMs             float64
	ClaimLatencyP95Ms             float64
	ClaimTimeouts                 uint64
	ClaimBudgetExhausted          uint64
	ReclaimTimeouts               uint64
	LeaseAcquireLatencyBuckets    []utils.HistogramBucket
	LeaseAcquireLatencyCount      uint64
	LeaseAcquireLatencySumMs      float64
	LeaseAcquireLatencyP95Ms      float64
	MarkLatencyBuckets            []utils.HistogramBucket
	MarkLatencyCount              uint64
	MarkLatencySumMs              float64
	MarkLatencyP95Ms              float64
	MarkTimeouts                  uint64
	TickLatencyBuckets            []utils.HistogramBucket
	TickLatencyCount              uint64
	TickLatencySumMs              float64
	TickLatencyP95Ms              float64
	AIToPublishBuckets            []utils.HistogramBucket
	AIToPublishCount              uint64
	AIToPublishSumMs              float64
	AIToPublishP95Ms              float64
	SourceToPublishBuckets        []utils.HistogramBucket
	SourceToPublishCount          uint64
	SourceToPublishSumMs          float64
	SourceToPublishP95Ms          float64
	CommitToPublishBuckets        []utils.HistogramBucket
	CommitToPublishCount          uint64
	CommitToPublishSumMs          float64
	CommitToPublishP95Ms          float64
	OutboxCreatedToClaimedBuckets []utils.HistogramBucket
	OutboxCreatedToClaimedCount   uint64
	OutboxCreatedToClaimedSumMs   float64
	OutboxCreatedToClaimedP95Ms   float64
	WakeToClaimBuckets            []utils.HistogramBucket
	WakeToClaimCount              uint64
	WakeToClaimSumMs              float64
	WakeToClaimP95Ms              float64
	SkewCorrectionsTotal          uint64
	AIEventUnitCorrections        uint64
	AIEventInvalidTotal           uint64
	SourceEventUnitCorrections    uint64
	SourceEventInvalidTotal       uint64
	PublishScopeResultTotals      map[string]uint64
	PublishScopeRouteTotals       map[string]uint64
	PublishScopeFallbacks         uint64
	PublishResultTotals           map[string]uint64
	PublishPartitionTotals        map[string]uint64
	TimeoutCauseTotals            map[string]uint64
}
