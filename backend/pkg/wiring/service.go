package wiring

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	apiserver "backend/pkg/api"
	"backend/pkg/block"
	"backend/pkg/config"
	"backend/pkg/consensus/api"
	"backend/pkg/consensus/messages"
	ctypes "backend/pkg/consensus/types"
	"backend/pkg/control/policyack"
	"backend/pkg/control/policyoutbox"
	"backend/pkg/control/policystate"
	"backend/pkg/control/policytrace"
	"backend/pkg/ingest/kafka"
	"backend/pkg/mempool"
	"backend/pkg/p2p"
	"backend/pkg/state"
	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"
	"github.com/IBM/sarama"
)

const controlRuntimeStateKeyMutationsSafeMode = "control_mutations_safe_mode"

type Config struct {
	BuildInterval       time.Duration
	ProposalCooldown    time.Duration
	BacklogCooldown     time.Duration
	PolicyFastCooldown  time.Duration
	BacklogTxThreshold  int
	MinMempoolTxs       int
	TimestampSkew       time.Duration
	GenesisHash         [32]byte
	BlockTimeout        time.Duration
	GenesisGracePeriod  time.Duration
	AllowSoloProposal   bool
	RequireActivePeers  bool
	StateRetainVersions uint64

	// Persistence configuration
	EnablePersistence bool                    // Enable async persistence (default: true)
	DBAdapter         cockroach.Adapter       // Database adapter (optional, created if nil)
	PersistenceWorker PersistenceWorkerConfig // Persistence worker config
	AuditLogger       *utils.AuditLogger      // Audit logger for persistence events

	// Kafka configuration (optional)
	EnableKafka      bool                 // Enable Kafka integration
	KafkaConsumerCfg kafka.ConsumerConfig // Kafka consumer config
	KafkaProducerCfg kafka.ProducerConfig // Kafka producer config
	ConfigManager    *utils.ConfigManager // Config manager for Kafka

	// API server configuration (optional)
	EnableAPI bool              // Enable read-only API server
	APIConfig *config.APIConfig // API server configuration

	// P2P consensus networking (optional)
	EnableP2P bool
	P2PRouter *p2p.Router
}

type Service struct {
	cfg     Config
	eng     *api.ConsensusEngine
	mp      *mempool.Mempool
	builder *block.Builder
	store   state.StateStore
	log     *utils.Logger
	audit   *utils.AuditLogger
	metrics *Metrics
	memMon  *utils.MemoryMonitor

	// Persistence (optional)
	persistWorker *PersistenceWorker

	// Kafka (optional)
	kafkaConsumer                               *kafka.Consumer
	kafkaProducer                               *kafka.Producer
	policyPublisher                             *policyPublisher
	policyPublishOnCommit                       bool
	policyPublishOnPersistence                  bool
	policyCommitProposerOnly                    bool
	persistCommitProposerOnly                   bool
	persistWriterMode                           string
	persistTakeoverLagThreshold                 uint64
	persistTakeoverDelay                        time.Duration
	persistTakeoverProbeTimeout                 time.Duration
	persistTakeoverProbeMinInterval             time.Duration
	persistBackfillEnabled                      bool
	persistBackfillLagThreshold                 uint64
	persistBackfillStaleAfter                   time.Duration
	persistBackfillPollInterval                 time.Duration
	persistBackfillMaxPerCycle                  int
	persistBackfillPendingMax                   int
	persistBackfillFastLaneEnabled              bool
	persistBackfillFastLaneMaxAge               time.Duration
	persistEnqueueTimeout                       time.Duration
	persistDirectFallbackEnabled                bool
	persistDirectFallbackTimeout                time.Duration
	persistDirectFallbackMaxInFlight            int
	persistDirectFallbackDisableOnQueuePressure bool
	persistDirectFallbackQueuePressurePct       int
	persistDirectFallbackDistressThreshold      int
	persistDirectFallbackDistressCooldown       time.Duration
	replayFilterEnabled                         bool
	replayFilterTTL                             time.Duration
	replayFilterMax                             int
	replayRefreshEnabled                        bool
	replayRefreshLeaderOnly                     bool
	replayRefreshInterval                       time.Duration
	replayRefreshHeightWindow                   uint64
	replayRefreshMaxHeightsTick                 int
	policyOutboxDispatcher                      *policyoutbox.Dispatcher
	policyOutboxStore                           *policyoutbox.Store
	policyAckCons                               *policyack.Consumer
	policyTraceCollector                        *policytrace.Collector
	controlDispatchSafeMode                     atomic.Bool

	// API server (optional)
	apiServer *apiserver.Server

	// P2P router (optional)
	router *p2p.Router

	mu                   sync.Mutex
	lastParent           [32]byte
	lastRoot             [32]byte // Last committed state root
	lastCommittedHeight  uint64   // Last successfully committed block height
	commitStateSynced    bool     // Whether commit validator is aligned with consensus height
	lastProposedView     uint64   // Last view we proposed in (debug visibility)
	lastProposedHeight   uint64   // Last height we proposed (for cooldown logging)
	lastProposalTime     time.Time
	proposalCooldown     time.Duration
	backlogCooldown      time.Duration
	policyFastCooldown   time.Duration
	backlogTxThreshold   int
	blockTimeout         time.Duration
	stopCh               chan struct{}
	commitLogEveryN      uint64
	commitWarnThrottle   time.Duration
	commitCounter        uint64
	commitLogsSuppressed uint64
	lastPersistNilWarnNs int64

	// Proposed tx hold prevents immediate tx re-proposal before commit cleanup.
	proposedTxHold         map[[32]byte]time.Time
	proposalTxHoldDuration time.Duration

	// Genesis coordination
	startTime          time.Time     // Service start time for genesis delay calculation
	genesisGracePeriod time.Duration // Grace period before first proposal

	// Proposer wakeup path: event-driven trigger with ticker fallback.
	proposeTriggerCh       chan struct{}
	proposeTriggerDropped  uint64
	proposeTriggerLogEvery uint64

	persistBackfillMu      sync.RWMutex
	persistBackfillPending map[uint64]pendingPersistenceEntry
	persistBackfillDropped uint64

	replayFilterMu        sync.RWMutex
	replayCommittedTx     map[[32]byte]time.Time
	replayRefreshNext     uint64
	replayRejectedAdmit   uint64
	replayRejectedBuild   uint64
	replayFilterEvictions uint64

	persistEnqueueCommitProposer         uint64
	persistEnqueueCommitNonProposer      uint64
	persistEnqueueBackfillProposer       uint64
	persistEnqueueBackfillNonProposer    uint64
	persistEnqueueDroppedNonProposer     uint64
	persistEnqueueErrors                 uint64
	persistEnqueueTimeouts               uint64
	persistEnqueueTimeoutsCommit         uint64
	persistEnqueueTimeoutsBackfill       uint64
	persistEnqueueWaitCommitSumMs        uint64
	persistEnqueueWaitCommitCount        uint64
	persistEnqueueWaitBackfillSumMs      uint64
	persistEnqueueWaitBackfillCount      uint64
	persistDirectFallbackAttempts        uint64
	persistDirectFallbackSuccess         uint64
	persistDirectFallbackFailures        uint64
	persistDirectFallbackThrottled       uint64
	persistDirectFallbackSkippedPressure uint64
	persistDirectFallbackSkippedDistress uint64
	persistDirectFallbackInFlight        int64
	persistCommitEnqueueTimeoutStreak    uint64
	persistDirectFallbackDistressUntilNs int64
	persistWriterTakeoverActivations     uint64
	persistBackfillNonRetryQuarantined   uint64
	persistExecuteProposer               uint64
	persistExecuteNonProposer            uint64
	persistExecuteDroppedNonOwner        uint64
	applyBlockObserverCleanup            func()

	// test hook for persistence writer ownership decision.
	persistStatusFn func() (ctypes.ValidatorID, bool)

	persistDetachedCtx    context.Context
	persistDetachedCancel context.CancelFunc
	persistFallbackSlots  chan struct{}
	persistTakeoverMu     sync.Mutex
	persistTakeoverSeen   map[uint64]time.Time
	persistTakeoverCached struct {
		latest uint64
		at     time.Time
		set    bool
	}
}

func hasConfiguredControlProducerTopic(topics kafka.ProducerTopics) bool {
	return topics.Commits != "" || topics.Policy != "" || topics.PolicyCommand != ""
}

func parseOutboxOwnerCount(consensusNodes string) int {
	parts := strings.Split(consensusNodes, ",")
	n := 0
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		n++
	}
	if n <= 0 {
		return 1
	}
	return n
}

func parseOutboxOwnerIndex(nodeID string) int {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return 0
	}
	id, err := strconv.Atoi(nodeID)
	if err != nil || id <= 0 {
		return 0
	}
	return id - 1
}

func NewService(cfg Config, eng *api.ConsensusEngine, mp *mempool.Mempool, builder *block.Builder, store state.StateStore, log *utils.Logger) (*Service, error) {
	var initOK bool
	s := &Service{
		cfg:                cfg,
		eng:                eng,
		mp:                 mp,
		builder:            builder,
		memMon:             utils.NewMemoryMonitor(log),
		store:              store,
		log:                log,
		audit:              cfg.AuditLogger,
		metrics:            NewMetrics(),
		lastParent:         cfg.GenesisHash, // Initialize with genesis instead of zero
		stopCh:             make(chan struct{}),
		blockTimeout:       cfg.BlockTimeout,
		proposalCooldown:   cfg.ProposalCooldown,
		backlogCooldown:    cfg.BacklogCooldown,
		policyFastCooldown: cfg.PolicyFastCooldown,
		backlogTxThreshold: cfg.BacklogTxThreshold,
		// startTime will be set in Start() after genesis completes
		genesisGracePeriod:     cfg.GenesisGracePeriod,
		proposedTxHold:         make(map[[32]byte]time.Time),
		proposeTriggerCh:       make(chan struct{}, 1),
		proposeTriggerLogEvery: 100,
		persistBackfillPending: make(map[uint64]pendingPersistenceEntry),
		persistTakeoverSeen:    make(map[uint64]time.Time),
		replayCommittedTx:      make(map[[32]byte]time.Time),
		policyTraceCollector:   policytrace.NewCollector(4096, 64),
	}
	s.persistDetachedCtx, s.persistDetachedCancel = context.WithCancel(context.Background())
	defer func() {
		if !initOK && s.applyBlockObserverCleanup != nil {
			s.applyBlockObserverCleanup()
			s.applyBlockObserverCleanup = nil
		}
	}()
	s.applyBlockObserverCleanup = state.SetApplyBlockObserver(s.metrics.RecordApplyBlockMetrics)
	if mp != nil {
		mp.SetTraceCollector(s.policyTraceCollector)
	}
	if s.blockTimeout <= 0 {
		s.blockTimeout = 5 * time.Second
	}
	if s.proposalCooldown <= 0 {
		cooldown := cfg.BuildInterval * 2
		if cooldown <= 0 {
			cooldown = s.blockTimeout
		}
		if cooldown < 100*time.Millisecond {
			cooldown = 100 * time.Millisecond
		}
		if cooldown > s.blockTimeout {
			cooldown = s.blockTimeout
		}
		s.proposalCooldown = cooldown
	}
	if s.backlogCooldown < 0 {
		s.backlogCooldown = 0
	}
	if s.policyFastCooldown < 0 {
		s.policyFastCooldown = 0
	}
	if s.backlogTxThreshold < 0 {
		s.backlogTxThreshold = 0
	}
	if s.backlogCooldown > 0 && s.backlogCooldown > s.proposalCooldown {
		s.backlogCooldown = s.proposalCooldown
	}
	if s.policyFastCooldown > 0 && s.policyFastCooldown > s.proposalCooldown {
		s.policyFastCooldown = s.proposalCooldown
	}
	if s.genesisGracePeriod < 0 {
		s.genesisGracePeriod = 0
	}
	if s.genesisGracePeriod == 0 {
		// Keep conservative default for multi-node startup unless explicitly tuned by env.
		s.genesisGracePeriod = 60 * time.Second
	}
	s.proposalTxHoldDuration = 10 * time.Second
	s.commitLogEveryN = 20
	s.commitWarnThrottle = 10 * time.Second
	if cfg.ConfigManager != nil {
		if every := cfg.ConfigManager.GetInt("CONTROL_COMMIT_LOG_EVERY_N", 20); every > 0 {
			s.commitLogEveryN = uint64(every)
		}
		if throttle := cfg.ConfigManager.GetDuration("CONTROL_COMMIT_WARN_LOG_THROTTLE", 10*time.Second); throttle > 0 {
			s.commitWarnThrottle = throttle
		}
		if hold := cfg.ConfigManager.GetDuration("PROPOSER_TX_HOLD_DURATION", 10*time.Second); hold > 0 {
			s.proposalTxHoldDuration = hold
		}
	}

	// Control-plane policy publish source selection.
	// Supported values: "commit" | "persistence" | "both" | "none"
	// Default remains "both" for backward compatibility.
	s.policyPublishOnCommit = true
	s.policyPublishOnPersistence = true
	s.policyCommitProposerOnly = false
	s.persistCommitProposerOnly = false
	s.persistWriterMode = "proposer"
	s.persistTakeoverLagThreshold = 2
	s.persistTakeoverDelay = 5 * time.Second
	s.persistTakeoverProbeTimeout = 250 * time.Millisecond
	s.persistTakeoverProbeMinInterval = 1 * time.Second
	s.persistBackfillEnabled = true
	s.persistBackfillLagThreshold = 1
	s.persistBackfillStaleAfter = 15 * time.Second
	s.persistBackfillPollInterval = 5 * time.Second
	s.persistBackfillMaxPerCycle = 4
	s.persistBackfillPendingMax = 2048
	s.persistBackfillFastLaneEnabled = true
	s.persistBackfillFastLaneMaxAge = 2 * time.Second
	s.persistEnqueueTimeout = 2 * time.Second
	s.persistDirectFallbackEnabled = true
	s.persistDirectFallbackTimeout = 5 * time.Second
	s.persistDirectFallbackMaxInFlight = 1
	s.persistDirectFallbackDisableOnQueuePressure = true
	s.persistDirectFallbackQueuePressurePct = 85
	s.persistDirectFallbackDistressThreshold = 3
	s.persistDirectFallbackDistressCooldown = 5 * time.Second
	s.replayFilterEnabled = true
	s.replayFilterTTL = 30 * time.Minute
	s.replayFilterMax = 200000
	s.replayRefreshEnabled = true
	s.replayRefreshLeaderOnly = true
	s.replayRefreshInterval = 30 * time.Second
	s.replayRefreshHeightWindow = 512
	s.replayRefreshMaxHeightsTick = 32
	if cfg.ConfigManager != nil {
		source := strings.ToLower(strings.TrimSpace(cfg.ConfigManager.GetString("CONTROL_POLICY_PUBLISH_SOURCE", "both")))
		switch source {
		case "commit":
			s.policyPublishOnCommit = true
			s.policyPublishOnPersistence = false
		case "persistence":
			s.policyPublishOnCommit = false
			s.policyPublishOnPersistence = true
		case "none":
			s.policyPublishOnCommit = false
			s.policyPublishOnPersistence = false
		case "both", "":
			s.policyPublishOnCommit = true
			s.policyPublishOnPersistence = true
		default:
			s.policyPublishOnCommit = true
			s.policyPublishOnPersistence = true
			if log != nil {
				log.Warn("Invalid CONTROL_POLICY_PUBLISH_SOURCE; defaulting to both",
					utils.ZapString("value", source))
			}
		}

		// Commit writer mode controls how many validators may publish control.policy
		// for a committed block.
		// Supported values:
		// - "all": any validator that commits may publish (legacy behavior)
		// - "proposer": only the committed block proposer may publish (single writer)
		writerMode := strings.ToLower(strings.TrimSpace(cfg.ConfigManager.GetString("CONTROL_POLICY_COMMIT_WRITER_MODE", "proposer")))
		switch writerMode {
		case "all", "":
			s.policyCommitProposerOnly = false
		case "proposer":
			s.policyCommitProposerOnly = true
		default:
			s.policyCommitProposerOnly = true
			if log != nil {
				log.Warn("Invalid CONTROL_POLICY_COMMIT_WRITER_MODE; defaulting to proposer",
					utils.ZapString("value", writerMode))
			}
		}
		if log != nil {
			log.Info("Configured policy publish source",
				utils.ZapBool("publish_on_commit", s.policyPublishOnCommit),
				utils.ZapBool("publish_on_persistence", s.policyPublishOnPersistence),
				utils.ZapBool("commit_writer_proposer_only", s.policyCommitProposerOnly))
		}

		// Persistence writer mode controls how many validators enqueue full block persistence
		// for a committed block.
		// Supported values:
		// - "all": any validator that commits may persist (legacy behavior)
		// - "proposer": only the committed block proposer persists (single writer)
		// - "proposer_primary": proposer first, non-proposer takeover after sustained lag
		persistWriterMode := strings.ToLower(strings.TrimSpace(cfg.ConfigManager.GetString("PERSIST_COMMIT_WRITER_MODE", "proposer")))
		switch persistWriterMode {
		case "all":
			s.persistCommitProposerOnly = false
			s.persistWriterMode = "all"
		case "proposer_primary":
			s.persistCommitProposerOnly = true
			s.persistWriterMode = "proposer_primary"
		case "proposer", "":
			s.persistCommitProposerOnly = true
			s.persistWriterMode = "proposer"
		default:
			s.persistCommitProposerOnly = true
			s.persistWriterMode = "proposer"
			if log != nil {
				log.Warn("Invalid PERSIST_COMMIT_WRITER_MODE; defaulting to proposer (fail-closed)",
					utils.ZapString("value", persistWriterMode))
			}
		}
		s.persistTakeoverLagThreshold = uint64(cfg.ConfigManager.GetInt("PERSIST_PROPOSER_PRIMARY_TAKEOVER_LAG_THRESHOLD", int(s.persistTakeoverLagThreshold)))
		if s.persistTakeoverLagThreshold == 0 {
			s.persistTakeoverLagThreshold = 1
		}
		s.persistTakeoverDelay = cfg.ConfigManager.GetDuration("PERSIST_PROPOSER_PRIMARY_TAKEOVER_DELAY", s.persistTakeoverDelay)
		if s.persistTakeoverDelay < 0 {
			s.persistTakeoverDelay = 0
		}
		s.persistTakeoverProbeTimeout = cfg.ConfigManager.GetDuration("PERSIST_PROPOSER_PRIMARY_TAKEOVER_PROBE_TIMEOUT", s.persistTakeoverProbeTimeout)
		if s.persistTakeoverProbeTimeout <= 0 {
			s.persistTakeoverProbeTimeout = 250 * time.Millisecond
		}
		s.persistTakeoverProbeMinInterval = cfg.ConfigManager.GetDuration("PERSIST_PROPOSER_PRIMARY_TAKEOVER_PROBE_MIN_INTERVAL", s.persistTakeoverProbeMinInterval)
		if s.persistTakeoverProbeMinInterval < 0 {
			s.persistTakeoverProbeMinInterval = 0
		}
		s.persistBackfillEnabled = cfg.ConfigManager.GetBool("PERSIST_BACKFILL_ENABLED", true)
		s.persistBackfillLagThreshold = uint64(cfg.ConfigManager.GetInt("PERSIST_BACKFILL_LAG_THRESHOLD", 1))
		s.persistBackfillStaleAfter = cfg.ConfigManager.GetDuration("PERSIST_BACKFILL_STALE_AFTER", 15*time.Second)
		if s.persistBackfillStaleAfter <= 0 {
			s.persistBackfillStaleAfter = 15 * time.Second
		}
		s.persistBackfillPollInterval = cfg.ConfigManager.GetDuration("PERSIST_BACKFILL_POLL_INTERVAL", 5*time.Second)
		if s.persistBackfillPollInterval <= 0 {
			s.persistBackfillPollInterval = 5 * time.Second
		}
		s.persistBackfillMaxPerCycle = cfg.ConfigManager.GetInt("PERSIST_BACKFILL_MAX_PER_CYCLE", 4)
		if s.persistBackfillMaxPerCycle <= 0 {
			s.persistBackfillMaxPerCycle = 4
		}
		s.persistBackfillPendingMax = cfg.ConfigManager.GetInt("PERSIST_BACKFILL_PENDING_MAX", 2048)
		if s.persistBackfillPendingMax <= 0 {
			s.persistBackfillPendingMax = 2048
		}
		s.persistBackfillFastLaneEnabled = cfg.ConfigManager.GetBool("PERSIST_BACKFILL_FASTLANE_ENABLED", true)
		s.persistBackfillFastLaneMaxAge = cfg.ConfigManager.GetDuration("PERSIST_BACKFILL_FASTLANE_MAX_AGE", 2*time.Second)
		if s.persistBackfillFastLaneMaxAge <= 0 {
			s.persistBackfillFastLaneMaxAge = 2 * time.Second
		}
		s.persistEnqueueTimeout = cfg.ConfigManager.GetDuration("PERSIST_ENQUEUE_TIMEOUT", 2*time.Second)
		if s.persistEnqueueTimeout <= 0 {
			s.persistEnqueueTimeout = 2 * time.Second
		}
		s.persistDirectFallbackEnabled = cfg.ConfigManager.GetBool("PERSIST_DIRECT_FALLBACK_ENABLED", true)
		s.persistDirectFallbackTimeout = cfg.ConfigManager.GetDuration("PERSIST_DIRECT_FALLBACK_TIMEOUT", 5*time.Second)
		if s.persistDirectFallbackTimeout <= 0 {
			s.persistDirectFallbackTimeout = 5 * time.Second
		}
		s.persistDirectFallbackMaxInFlight = cfg.ConfigManager.GetInt("PERSIST_DIRECT_FALLBACK_MAX_IN_FLIGHT", 1)
		s.persistDirectFallbackDisableOnQueuePressure = cfg.ConfigManager.GetBool("PERSIST_DIRECT_FALLBACK_DISABLE_ON_QUEUE_PRESSURE", true)
		s.persistDirectFallbackQueuePressurePct = cfg.ConfigManager.GetInt("PERSIST_DIRECT_FALLBACK_QUEUE_PRESSURE_PCT", s.persistDirectFallbackQueuePressurePct)
		if s.persistDirectFallbackQueuePressurePct <= 0 {
			s.persistDirectFallbackQueuePressurePct = 85
		}
		if s.persistDirectFallbackQueuePressurePct > 100 {
			s.persistDirectFallbackQueuePressurePct = 100
		}
		s.persistDirectFallbackDistressThreshold = cfg.ConfigManager.GetInt("PERSIST_DIRECT_FALLBACK_DISTRESS_THRESHOLD", s.persistDirectFallbackDistressThreshold)
		if s.persistDirectFallbackDistressThreshold < 0 {
			s.persistDirectFallbackDistressThreshold = 0
		}
		s.persistDirectFallbackDistressCooldown = cfg.ConfigManager.GetDuration("PERSIST_DIRECT_FALLBACK_DISTRESS_COOLDOWN", s.persistDirectFallbackDistressCooldown)
		if s.persistDirectFallbackDistressCooldown < 0 {
			s.persistDirectFallbackDistressCooldown = 0
		}
		s.replayFilterEnabled = cfg.ConfigManager.GetBool("TX_REPLAY_FILTER_ENABLED", true)
		s.replayFilterTTL = cfg.ConfigManager.GetDuration("TX_REPLAY_FILTER_TTL", 30*time.Minute)
		if s.replayFilterTTL <= 0 {
			s.replayFilterTTL = 30 * time.Minute
		}
		s.replayFilterMax = cfg.ConfigManager.GetInt("TX_REPLAY_FILTER_MAX", 200000)
		if s.replayFilterMax <= 0 {
			s.replayFilterMax = 200000
		}
		s.replayRefreshEnabled = cfg.ConfigManager.GetBool("TX_REPLAY_REFRESH_ENABLED", true)
		s.replayRefreshLeaderOnly = cfg.ConfigManager.GetBool("TX_REPLAY_REFRESH_LEADER_ONLY", true)
		s.replayRefreshInterval = cfg.ConfigManager.GetDuration("TX_REPLAY_REFRESH_INTERVAL", 30*time.Second)
		if s.replayRefreshInterval <= 0 {
			s.replayRefreshInterval = 30 * time.Second
		}
		s.replayRefreshHeightWindow = uint64(cfg.ConfigManager.GetInt("TX_REPLAY_REFRESH_HEIGHT_WINDOW", 512))
		if s.replayRefreshHeightWindow == 0 {
			s.replayRefreshHeightWindow = 512
		}
		s.replayRefreshMaxHeightsTick = cfg.ConfigManager.GetInt("TX_REPLAY_REFRESH_MAX_HEIGHTS_PER_TICK", 32)
		if s.replayRefreshMaxHeightsTick <= 0 {
			s.replayRefreshMaxHeightsTick = 32
		}
		if log != nil {
			log.Info("Configured persistence writer mode",
				utils.ZapString("persist_writer_mode", s.persistWriterMode),
				utils.ZapBool("persist_writer_proposer_only", s.persistCommitProposerOnly),
				utils.ZapUint64("persist_takeover_lag_threshold", s.persistTakeoverLagThreshold),
				utils.ZapDuration("persist_takeover_delay", s.persistTakeoverDelay),
				utils.ZapDuration("persist_takeover_probe_timeout", s.persistTakeoverProbeTimeout),
				utils.ZapDuration("persist_takeover_probe_min_interval", s.persistTakeoverProbeMinInterval),
				utils.ZapBool("persist_backfill_enabled", s.persistBackfillEnabled),
				utils.ZapDuration("persist_backfill_stale_after", s.persistBackfillStaleAfter),
				utils.ZapDuration("persist_backfill_poll_interval", s.persistBackfillPollInterval),
				utils.ZapUint64("persist_backfill_lag_threshold", s.persistBackfillLagThreshold),
				utils.ZapInt("persist_backfill_max_per_cycle", s.persistBackfillMaxPerCycle),
				utils.ZapInt("persist_backfill_pending_max", s.persistBackfillPendingMax),
				utils.ZapBool("persist_backfill_fastlane_enabled", s.persistBackfillFastLaneEnabled),
				utils.ZapDuration("persist_backfill_fastlane_max_age", s.persistBackfillFastLaneMaxAge),
				utils.ZapDuration("persist_enqueue_timeout", s.persistEnqueueTimeout),
				utils.ZapBool("persist_direct_fallback_enabled", s.persistDirectFallbackEnabled),
				utils.ZapDuration("persist_direct_fallback_timeout", s.persistDirectFallbackTimeout),
				utils.ZapInt("persist_direct_fallback_max_in_flight", s.persistDirectFallbackMaxInFlight),
				utils.ZapBool("persist_direct_fallback_disable_on_queue_pressure", s.persistDirectFallbackDisableOnQueuePressure),
				utils.ZapInt("persist_direct_fallback_queue_pressure_pct", s.persistDirectFallbackQueuePressurePct),
				utils.ZapInt("persist_direct_fallback_distress_threshold", s.persistDirectFallbackDistressThreshold),
				utils.ZapDuration("persist_direct_fallback_distress_cooldown", s.persistDirectFallbackDistressCooldown),
				utils.ZapBool("tx_replay_filter_enabled", s.replayFilterEnabled),
				utils.ZapDuration("tx_replay_filter_ttl", s.replayFilterTTL),
				utils.ZapInt("tx_replay_filter_max", s.replayFilterMax),
				utils.ZapBool("tx_replay_refresh_enabled", s.replayRefreshEnabled),
				utils.ZapBool("tx_replay_refresh_leader_only", s.replayRefreshLeaderOnly),
				utils.ZapDuration("tx_replay_refresh_interval", s.replayRefreshInterval),
				utils.ZapUint64("tx_replay_refresh_height_window", s.replayRefreshHeightWindow),
				utils.ZapInt("tx_replay_refresh_max_heights_per_tick", s.replayRefreshMaxHeightsTick))
		}
	}
	if s.persistDirectFallbackMaxInFlight <= 0 {
		s.persistDirectFallbackMaxInFlight = 1
	}
	s.persistFallbackSlots = make(chan struct{}, s.persistDirectFallbackMaxInFlight)

	var dbHandle *sql.DB
	if provider, ok := cfg.DBAdapter.(interface{ GetDB() *sql.DB }); ok {
		dbHandle = provider.GetDB()
		if dbHandle != nil {
			if err := policystate.Prime(context.Background(), dbHandle); err != nil {
				log.Warn("policy state projection prime failed", utils.ZapError(err))
			}
		}
	}

	if cfg.DBAdapter != nil {
		if persistenceWorker := s.persistWorker; persistenceWorker != nil {
			eng.SetPersistence(persistenceWorker)
		}
	}

	// Ensure first proposal in view 0 isn't suppressed by cooldown guard
	s.lastProposedView = ^uint64(0)
	s.lastProposedHeight = ^uint64(0)

	// Initialize Kafka producer if enabled (must be before persistence worker)
	var kafkaProducer *kafka.Producer
	if cfg.EnableKafka {
		// Set up the shared control producer when any publishable control topic is configured.
		if hasConfiguredControlProducerTopic(cfg.KafkaProducerCfg.Topics) {
			if cfg.ConfigManager == nil {
				return nil, fmt.Errorf("kafka producer requires config manager")
			}

			keyPath := cfg.ConfigManager.GetString("CONTROL_SIGNING_KEY_PATH", "")
			if keyPath == "" {
				env := cfg.ConfigManager.GetString("ENVIRONMENT", "production")
				if env == "production" || env == "staging" {
					return nil, fmt.Errorf("CONTROL_SIGNING_KEY_PATH is required when Kafka producer enabled in %s", env)
				}
				// Dev mode: skip producer if no key
				if log != nil {
					log.Warn("Kafka producer disabled: CONTROL_SIGNING_KEY_PATH not set (dev mode)")
				}
			} else {
				keyPath = filepath.Clean(keyPath)

				signerCfg := kafka.CommitSignerConfig{
					KeyPath:    keyPath,
					KeyID:      cfg.ConfigManager.GetString("CONTROL_SIGNING_KEY_ID", cfg.ConfigManager.GetString("NODE_ID", "")),
					Domain:     cfg.ConfigManager.GetString("CONTROL_SIGNING_DOMAIN", "control.commits.v1"),
					ProducerID: cfg.ConfigManager.GetString("CONTROL_PRODUCER_ID", ""),
					Logger:     log,
				}

				signer, err := kafka.NewCommitSigner(signerCfg)
				if err != nil {
					return nil, fmt.Errorf("failed to initialize commit signer: %w", err)
				}

				cfg.KafkaProducerCfg.Signer = signer

				policyKeyPath := cfg.ConfigManager.GetString("CONTROL_POLICY_SIGNING_KEY_PATH", keyPath)
				if policyKeyPath != "" {
					policyKeyPath = filepath.Clean(policyKeyPath)
					policyDomainDefault := "control.policy.v2"
					if strings.TrimSpace(cfg.KafkaProducerCfg.Topics.PolicyCommand) != "" {
						policyDomainDefault = "control.enforcement_command.v1"
					}
					policySignerCfg := kafka.CommitSignerConfig{
						KeyPath:    policyKeyPath,
						KeyID:      cfg.ConfigManager.GetString("CONTROL_POLICY_SIGNING_KEY_ID", signerCfg.KeyID),
						Domain:     cfg.ConfigManager.GetString("CONTROL_POLICY_SIGNING_DOMAIN", policyDomainDefault),
						ProducerID: cfg.ConfigManager.GetString("CONTROL_POLICY_PRODUCER_ID", cfg.ConfigManager.GetString("CONTROL_PRODUCER_ID", "")),
						Logger:     log,
					}

					policySigner, err := kafka.NewCommitSigner(policySignerCfg)
					if err != nil {
						return nil, fmt.Errorf("failed to initialize policy signer: %w", err)
					}
					cfg.KafkaProducerCfg.PolicySigner = policySigner
				} else if (cfg.KafkaProducerCfg.Topics.Policy != "" || cfg.KafkaProducerCfg.Topics.PolicyCommand != "") && log != nil {
					log.Warn("Kafka policy publishing disabled: CONTROL_POLICY_SIGNING_KEY_PATH not set")
				}

				// Build sarama config
				saramaCfg, err := kafka.BuildSaramaConfig(context.Background(), cfg.ConfigManager, log, cfg.AuditLogger)
				if err != nil {
					return nil, fmt.Errorf("failed to build Kafka config: %w", err)
				}
				cfg.KafkaProducerCfg.LogThrottle = cfg.ConfigManager.GetDuration("KAFKA_PRODUCER_LOG_THROTTLE", 10*time.Second)

				// Create producer
				kafkaProducer, err = kafka.NewProducer(context.Background(), cfg.KafkaProducerCfg, saramaCfg, log, cfg.AuditLogger)
				if err != nil {
					return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
				}
				s.kafkaProducer = kafkaProducer

				policyPublishTopic := strings.TrimSpace(cfg.KafkaProducerCfg.Topics.PolicyCommand)
				if policyPublishTopic == "" {
					policyPublishTopic = strings.TrimSpace(cfg.KafkaProducerCfg.Topics.Policy)
				}
				if policyPublishTopic != "" {
					pp, err := newPolicyPublisher(kafkaProducer, cfg.ConfigManager, policyPublishTopic, log, cfg.AuditLogger, s.policyTraceCollector)
					if err != nil {
						kafkaProducer.Close()
						return nil, fmt.Errorf("failed to initialize policy publisher: %w", err)
					}
					s.policyPublisher = pp

					outboxEnabled := cfg.ConfigManager.GetBool("CONTROL_POLICY_OUTBOX_ENABLED", true)
					if outboxEnabled {
						if dbHandle == nil {
							kafkaProducer.Close()
							return nil, fmt.Errorf("policy outbox enabled but storage adapter does not expose DB handle")
						}
						outboxStore, err := policyoutbox.NewStore(dbHandle)
						if err != nil {
							kafkaProducer.Close()
							return nil, fmt.Errorf("failed to initialize policy outbox store: %w", err)
						}
						schemaCheckTimeout := cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_SCHEMA_CHECK_TIMEOUT", 10*time.Second)
						if schemaCheckTimeout <= 0 {
							schemaCheckTimeout = 10 * time.Second
						}
						schemaCtx, cancel := context.WithTimeout(context.Background(), schemaCheckTimeout)
						defer cancel()
						if err := outboxStore.EnsureSchema(schemaCtx); err != nil {
							kafkaProducer.Close()
							return nil, fmt.Errorf("policy outbox schema invariant failed: %w", err)
						}

						holderID := strings.TrimSpace(cfg.ConfigManager.GetString("NODE_ID", "backend-node"))
						safeModeRefreshInterval := cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_SAFE_MODE_REFRESH_INTERVAL", 3*time.Second)
						if safeModeRefreshInterval < 0 {
							safeModeRefreshInterval = 0
						}
						var safeModeCacheMu sync.Mutex
						safeModeCachedUntil := time.Time{}
						safeModeCachedValue := s.controlDispatchSafeMode.Load()
						outboxCfg := policyoutbox.Config{
							Enabled:             true,
							LeaseKey:            strings.TrimSpace(cfg.ConfigManager.GetString("CONTROL_POLICY_OUTBOX_LEASE_KEY", "control.policy.dispatcher")),
							PolicyTopic:         policyPublishTopic,
							ScopeRoutingEnabled: cfg.ConfigManager.GetBool("CONTROL_POLICY_SCOPE_ROUTING_ENABLED", false),
							DispatchOwnerCount:  parseOutboxOwnerCount(cfg.ConfigManager.GetString("CONSENSUS_NODES", "")),
							DispatchOwnerIndex:  parseOutboxOwnerIndex(holderID),
							DispatchSafeMode:    &s.controlDispatchSafeMode,
							RefreshSafeMode: func(ctx context.Context) (bool, error) {
								if safeModeRefreshInterval > 0 {
									now := time.Now()
									safeModeCacheMu.Lock()
									cachedValue := safeModeCachedValue
									cachedUntil := safeModeCachedUntil
									safeModeCacheMu.Unlock()
									if now.Before(cachedUntil) {
										return cachedValue, nil
									}
								}
								type dbProvider interface {
									GetDB() *sql.DB
								}
								provider, ok := cfg.DBAdapter.(dbProvider)
								if !ok || provider.GetDB() == nil {
									value := s.controlDispatchSafeMode.Load()
									if safeModeRefreshInterval > 0 {
										safeModeCacheMu.Lock()
										safeModeCachedValue = value
										safeModeCachedUntil = time.Now().Add(safeModeRefreshInterval)
										safeModeCacheMu.Unlock()
									}
									return value, nil
								}
								db := provider.GetDB()
								var enabled bool
								err := db.QueryRowContext(ctx, `
									SELECT enabled
									FROM control_runtime_state
									WHERE state_key = $1
								`, controlRuntimeStateKeyMutationsSafeMode).Scan(&enabled)
								if err == sql.ErrNoRows {
									value := s.controlDispatchSafeMode.Load()
									if safeModeRefreshInterval > 0 {
										safeModeCacheMu.Lock()
										safeModeCachedValue = value
										safeModeCachedUntil = time.Now().Add(safeModeRefreshInterval)
										safeModeCacheMu.Unlock()
									}
									return value, nil
								}
								if err != nil {
									value := s.controlDispatchSafeMode.Load()
									if safeModeRefreshInterval > 0 {
										safeModeCacheMu.Lock()
										value = safeModeCachedValue
										safeModeCachedUntil = time.Now().Add(safeModeRefreshInterval / 2)
										safeModeCacheMu.Unlock()
									}
									return value, nil
								}
								if safeModeRefreshInterval > 0 {
									safeModeCacheMu.Lock()
									safeModeCachedValue = enabled
									safeModeCachedUntil = time.Now().Add(safeModeRefreshInterval)
									safeModeCacheMu.Unlock()
								}
								return enabled, nil
							},
							ClusterShardingMode:   strings.ToLower(strings.TrimSpace(cfg.ConfigManager.GetString("CONTROL_POLICY_CLUSTER_SHARDING_MODE", "off"))),
							ClusterShardBuckets:   cfg.ConfigManager.GetInt("CONTROL_POLICY_CLUSTER_SHARD_BUCKETS", 1),
							DispatchShardCount:    cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_DISPATCH_SHARDS", 0),
							DispatchShardCompat:   cfg.ConfigManager.GetBool("CONTROL_POLICY_OUTBOX_DISPATCH_SHARD_COMPAT", true),
							MaxLeaseShards:        cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_MAX_LEASE_SHARDS", 0),
							LeaseTTL:              cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_LEASE_TTL", 10*time.Second),
							LeaseAcquireTimeout:   cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_LEASE_ACQUIRE_TIMEOUT", 0),
							ClaimTimeout:          cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_CLAIM_TIMEOUT", 0),
							ReclaimAfter:          cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_RECLAIM_AFTER", 30*time.Second),
							PollInterval:          cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_POLL_INTERVAL", 75*time.Millisecond),
							PollIntervalHot:       cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_POLL_INTERVAL_HOT", 75*time.Millisecond),
							PollIntervalHotWindow: cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_POLL_INTERVAL_HOT_WINDOW", 2*time.Second),
							DrainMaxDuration:      cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_DRAIN_MAX_DURATION", 8*time.Second),
							WakeDrainMaxDuration:  cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_WAKE_DRAIN_MAX_DURATION", 200*time.Millisecond),
							WakeDrainMaxTicks:     cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_WAKE_DRAIN_MAX_TICKS", 2),
							WakeChannelSize:       cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_WAKE_CHANNEL_SIZE", 2),
							BatchSize:             cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_BATCH_SIZE", 128),
							BatchSizeMin:          cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_BATCH_SIZE_MIN", 32),
							BatchSizeMax:          cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_BATCH_SIZE_MAX", 320),
							AdaptiveBatch:         cfg.ConfigManager.GetBool("CONTROL_POLICY_OUTBOX_ADAPTIVE_BATCH", true),
							MaxInFlight:           cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_MAX_IN_FLIGHT", 32),
							MarkWorkers:           cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_MARK_WORKERS", 24),
							InternalQueue:         cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_INTERNAL_QUEUE_SIZE", 512),
							DrainMaxBatches:       cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_DRAIN_BATCHES", 10),
							MaxRetries:            cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_MAX_RETRIES", 8),
							RetryInitialBack:      cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_RETRY_INITIAL", 250*time.Millisecond),
							RetryMaxBack:          cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_RETRY_MAX", 30*time.Second),
							RetryJitterRatio:      cfg.ConfigManager.GetFloat64("CONTROL_POLICY_OUTBOX_RETRY_JITTER_RATIO", 0.2),
							LogThrottle:           cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_LOG_THROTTLE", 5*time.Second),
						}
						dispatcher, err := policyoutbox.NewDispatcher(outboxCfg, outboxStore, pp, log, cfg.AuditLogger, holderID, s.policyTraceCollector)
						if err != nil {
							kafkaProducer.Close()
							return nil, fmt.Errorf("failed to initialize policy outbox dispatcher: %w", err)
						}
						s.policyOutboxDispatcher = dispatcher
						s.policyOutboxStore = outboxStore
						if notifierSetter, ok := cfg.DBAdapter.(interface {
							SetOutboxDurableNotifier(func(context.Context, uint64, int))
						}); ok {
							notifierSetter.SetOutboxDurableNotifier(func(_ context.Context, _ uint64, _ int) {
								s.policyOutboxDispatcher.Notify()
							})
						}

						// Outbox is the authority; disable direct policy publish paths.
						s.policyPublishOnCommit = false
						s.policyPublishOnPersistence = false
						if log != nil {
							log.Info("Control policy outbox enabled; direct policy publish disabled")
						}
					}
				}
			}
		} else if log != nil {
			log.Warn("Kafka producer disabled: no control commits or policy topic configured")
		}
	}

	// Initialize persistence worker if enabled
	if cfg.EnablePersistence && cfg.DBAdapter != nil {
		if s.policyOutboxDispatcher != nil {
			cfg.PersistenceWorker.OnPersisted = func(ctx context.Context, height uint64, txCount int) {
				// JIT wake-up: notify outbox dispatcher immediately after durable commit.
				// Poll ticker remains as fallback if the signal is dropped/coalesced.
				s.policyOutboxDispatcher.Notify()
			}
		}

		// Wire Kafka producer as onSuccess callback
		// Fix: Gap 2 - Updated to pass anomaly IDs for COMMITTED state tracking
		if kafkaProducer != nil {
			cfg.PersistenceWorker.OnSuccess = func(ctx context.Context, height uint64, hash [32]byte, stateRoot [32]byte, txCount int, ts int64, anomalyCount int, evidenceCount int, policyCount int, anomalyIDs []string, policyPayloads [][]byte) {
				// Publish commit event to Kafka after successful persistence
				if err := kafkaProducer.PublishCommit(ctx, height, hash, stateRoot, txCount, ts, anomalyCount, evidenceCount, policyCount, anomalyIDs); err != nil {
					if log != nil {
						log.ErrorContext(ctx, "Failed to publish commit to Kafka",
							utils.ZapError(err),
							utils.ZapUint64("height", height))
					}
				}
			}
		}

		worker, err := NewPersistenceWorker(
			cfg.PersistenceWorker,
			cfg.DBAdapter,
			log,
			cfg.AuditLogger,
		)
		if err != nil {
			// Cleanup Kafka producer if persistence worker creation fails
			if kafkaProducer != nil {
				kafkaProducer.Close()
			}
			return nil, fmt.Errorf("failed to create persistence worker: %w", err)
		}
		s.persistWorker = worker
		if eng != nil {
			status := eng.GetStatus()
			workerProposerOnly := s.persistCommitProposerOnly && s.persistWriterMode != "proposer_primary"
			s.persistWorker.SetWriterPolicy(workerProposerOnly, status.NodeID)
		}
		// Wire persistence into consensus engine now that worker exists
		if eng != nil {
			eng.AttachPersistence(worker)
		}
	}

	// Initialize Kafka consumer if enabled (after mempool is set)
	if cfg.EnableKafka && mp != nil {
		// Build sarama config
		saramaCfg, err := kafka.BuildSaramaConfig(context.Background(), cfg.ConfigManager, log, cfg.AuditLogger)
		if err != nil {
			// Cleanup on error
			if kafkaProducer != nil {
				kafkaProducer.Close()
			}
			return nil, fmt.Errorf("failed to build Kafka config: %w", err)
		}

		// Create consumer
		kafkaConsumer, err := kafka.NewConsumer(context.Background(), cfg.KafkaConsumerCfg, saramaCfg, mp, log, cfg.AuditLogger)
		if err != nil {
			// Cleanup on error
			if kafkaProducer != nil {
				kafkaProducer.Close()
			}
			return nil, fmt.Errorf("failed to create Kafka consumer: %w", err)
		}
		kafkaConsumer.SetTraceCollector(s.policyTraceCollector)
		kafkaConsumer.SetPreAdmitHook(func(topic string, tx state.Transaction) error {
			if s.shouldRejectReplayOnAdmit(tx, time.Now()) {
				return kafka.ErrReplayRejected
			}
			return nil
		})
		s.kafkaConsumer = kafkaConsumer
		s.kafkaConsumer.SetPostAdmitHook(func(topic string, _ state.Transaction) {
			// Wake proposer promptly after new mempool admissions.
			s.notifyProposeTrigger("kafka_admit")
		})

		// Best-effort topic validation: warn if topics missing or have no partitions
		if len(cfg.KafkaConsumerCfg.Topics) > 0 {
			if client, err := sarama.NewClient(cfg.KafkaConsumerCfg.Brokers, saramaCfg); err == nil {
				for _, t := range cfg.KafkaConsumerCfg.Topics {
					parts, perr := client.Partitions(t)
					if perr != nil {
						if log != nil {
							log.Warn("Kafka topic not accessible", utils.ZapString("topic", t), utils.ZapError(perr))
						}
						continue
					}
					if len(parts) == 0 {
						if log != nil {
							log.Warn("Kafka topic has no partitions", utils.ZapString("topic", t))
						}
					} else {
						if log != nil {
							log.Info("Kafka topic partitions", utils.ZapString("topic", t), utils.ZapInt("partition_count", len(parts)))
						}
					}
				}
				_ = client.Close()
			} else if log != nil {
				log.Warn("Kafka topic validation skipped (client create failed)", utils.ZapError(err))
			}
		}
	}

	// Initialize policy ACK consumer (optional).
	// This consumes control.enforcement_ack.v1 emitted by enforcement-agent and persists it for visibility/ops.
	if cfg.EnableKafka && cfg.ConfigManager != nil {
		ackCfg, err := policyack.LoadConfig(cfg.ConfigManager)
		if err != nil {
			if kafkaProducer != nil {
				kafkaProducer.Close()
			}
			if s.kafkaConsumer != nil {
				_ = s.kafkaConsumer.Stop()
			}
			return nil, fmt.Errorf("policy ack consumer config invalid: %w", err)
		}
		if ackCfg.Enabled {
			// Require DB access when ACK ingestion enabled (we do not silently drop acks).
			if dbHandle == nil {
				if kafkaProducer != nil {
					kafkaProducer.Close()
				}
				if s.kafkaConsumer != nil {
					_ = s.kafkaConsumer.Stop()
				}
				return nil, fmt.Errorf("policy ack consumer enabled but storage adapter does not expose DB handle")
			}

			store, err := policyack.NewStore(dbHandle)
			if err != nil {
				if kafkaProducer != nil {
					kafkaProducer.Close()
				}
				if s.kafkaConsumer != nil {
					_ = s.kafkaConsumer.Stop()
				}
				return nil, fmt.Errorf("policy ack store init failed: %w", err)
			}
			schemaCheckTimeout := cfg.ConfigManager.GetDuration("CONTROL_POLICY_ACK_SCHEMA_CHECK_TIMEOUT", 10*time.Second)
			if schemaCheckTimeout <= 0 {
				schemaCheckTimeout = 10 * time.Second
			}
			schemaCtx, cancel := context.WithTimeout(context.Background(), schemaCheckTimeout)
			defer cancel()
			if err := store.EnsureSchema(schemaCtx); err != nil {
				if kafkaProducer != nil {
					kafkaProducer.Close()
				}
				if s.kafkaConsumer != nil {
					_ = s.kafkaConsumer.Stop()
				}
				return nil, fmt.Errorf("policy ack schema check failed: %w", err)
			}

			var trust *policyack.TrustedKeys
			if ackCfg.TrustedKeysDir != "" {
				t, terr := policyack.LoadTrustedKeys(ackCfg.TrustedKeysDir)
				if terr != nil {
					if kafkaProducer != nil {
						kafkaProducer.Close()
					}
					if s.kafkaConsumer != nil {
						_ = s.kafkaConsumer.Stop()
					}
					return nil, fmt.Errorf("policy ack trust store load failed: %w", terr)
				}
				trust = t
			}

			// Reuse the backend's sarama configuration builder (TLS/SASL enforced).
			saramaCfg, err := kafka.BuildSaramaConfig(context.Background(), cfg.ConfigManager, log, cfg.AuditLogger)
			if err != nil {
				if kafkaProducer != nil {
					kafkaProducer.Close()
				}
				if s.kafkaConsumer != nil {
					_ = s.kafkaConsumer.Stop()
				}
				return nil, fmt.Errorf("policy ack consumer: build kafka config failed: %w", err)
			}

			cg, err := sarama.NewConsumerGroup(ackCfg.Brokers, ackCfg.GroupID, saramaCfg)
			if err != nil {
				if kafkaProducer != nil {
					kafkaProducer.Close()
				}
				if s.kafkaConsumer != nil {
					_ = s.kafkaConsumer.Stop()
				}
				return nil, fmt.Errorf("policy ack consumer: create consumer group failed: %w", err)
			}

			// DLQ producer is optional.
			var dlq sarama.SyncProducer
			if ackCfg.DLQ != "" {
				dlq, err = sarama.NewSyncProducer(ackCfg.Brokers, saramaCfg)
				if err != nil {
					_ = cg.Close()
					if kafkaProducer != nil {
						kafkaProducer.Close()
					}
					if s.kafkaConsumer != nil {
						_ = s.kafkaConsumer.Stop()
					}
					return nil, fmt.Errorf("policy ack consumer: create dlq producer failed: %w", err)
				}
			}

			ac, err := policyack.New(context.Background(), cg, policyack.Options{
				Config:         ackCfg,
				Store:          store,
				Trust:          trust,
				Logger:         log,
				Audit:          cfg.AuditLogger,
				DLQ:            dlq,
				TraceCollector: s.policyTraceCollector,
			})
			if err != nil {
				if dlq != nil {
					_ = dlq.Close()
				}
				_ = cg.Close()
				if kafkaProducer != nil {
					kafkaProducer.Close()
				}
				if s.kafkaConsumer != nil {
					_ = s.kafkaConsumer.Stop()
				}
				return nil, fmt.Errorf("policy ack consumer init failed: %w", err)
			}
			s.policyAckCons = ac
			if log != nil {
				log.Info("Policy ACK consumer configured",
					utils.ZapString("topic", ackCfg.Topic),
					utils.ZapString("group_id", ackCfg.GroupID),
					utils.ZapBool("signature_required", ackCfg.SigningRequired),
					utils.ZapBool("dlq_enabled", ackCfg.DLQ != ""))
			}
		}
	}

	// Initialize API server if enabled
	if cfg.EnableAPI && cfg.APIConfig != nil {
		s.controlDispatchSafeMode.Store(cfg.APIConfig.ControlMutationsSafeMode)
		apiDeps := apiserver.Dependencies{
			Config:          cfg.APIConfig,
			Logger:          log,
			AuditLogger:     cfg.AuditLogger,
			Storage:         cfg.DBAdapter,
			StateStore:      store,
			Mempool:         mp,
			Engine:          eng,
			P2PRouter:       cfg.P2PRouter,
			KafkaProd:       s.kafkaProducer,
			KafkaCons:       s.kafkaConsumer,
			OutboxStats:     s,
			TraceStats:      s,
			ControlSafeMode: &s.controlDispatchSafeMode,
			NodeAliases:     cfg.APIConfig.NodeAliasMap,
			NodeAliasList:   cfg.APIConfig.NodeAliasList,
		}

		apiSrv, err := apiserver.NewServer(apiDeps)
		if err != nil {
			// Cleanup on error
			if s.kafkaConsumer != nil {
				s.kafkaConsumer.Stop()
			}
			if kafkaProducer != nil {
				kafkaProducer.Close()
			}
			return nil, fmt.Errorf("failed to create API server: %w", err)
		}
		s.apiServer = apiSrv

		if log != nil {
			log.Info("API server initialized",
				utils.ZapString("listen_addr", cfg.APIConfig.ListenAddr),
				utils.ZapBool("tls_enabled", cfg.APIConfig.TLSEnabled))
		}
	}

	// Attach P2P consensus networking if enabled
	if cfg.EnableP2P {
		if cfg.P2PRouter == nil {
			if s.apiServer != nil {
				_ = s.apiServer.Stop()
			}
			if s.kafkaConsumer != nil {
				_ = s.kafkaConsumer.Stop()
			}
			if kafkaProducer != nil {
				kafkaProducer.Close()
			}
			return nil, fmt.Errorf("p2p enabled but router is nil")
		}

		types := []messages.MessageType{
			messages.TypeProposal,
			messages.TypeVote,
			messages.TypeViewChange,
			messages.TypeNewView,
			messages.TypeHeartbeat,
			messages.TypeEvidence,
			messages.TypeGenesisReady,
			messages.TypeGenesisCertificate,
			messages.TypeProposalIntent,
			messages.TypeReadyToVote,
		}
		topicMap := make(map[string]messages.MessageType, len(types))
		for _, mt := range types {
			topic := eng.TopicFor(mt)
			topicMap[topic] = mt
			s.log.Info("TopicMap entry", utils.ZapString("topic", topic), utils.ZapAny("msgType", mt))
		}

		if err := p2p.AttachConsensusHandlers(cfg.P2PRouter, eng, topicMap); err != nil {
			if s.apiServer != nil {
				_ = s.apiServer.Stop()
			}
			if s.kafkaConsumer != nil {
				_ = s.kafkaConsumer.Stop()
			}
			if kafkaProducer != nil {
				kafkaProducer.Close()
			}
			return nil, fmt.Errorf("failed to attach consensus handlers: %w", err)
		}

		s.router = cfg.P2PRouter
		s.eng.SetPeerObserver(cfg.P2PRouter)
	}

	initOK = true
	return s, nil
}

// GetMetrics returns a snapshot of current metrics
func (s *Service) GetMetrics() Metrics {
	return s.metrics.GetSnapshot()
}

func (s *Service) Start(ctx context.Context) error {
	// Start persistence worker if configured
	if s.persistWorker != nil {
		if err := s.persistWorker.Start(ctx); err != nil {
			return fmt.Errorf("failed to start persistence worker: %w", err)
		}
	}

	// Bootstrap the replay filter from durable storage before Kafka consumption starts.
	// This closes the restart window where already-committed transactions could be
	// re-admitted and proposed again before the periodic refresh loop runs.
	if s.replayFilterEnabled && s.replayRefreshEnabled && s.persistWorker != nil {
		bootstrapCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := s.bootstrapReplayFilterFromDB(bootstrapCtx)
		cancel()
		if err != nil {
			if s.persistWorker != nil {
				_ = s.persistWorker.Stop()
			}
			return fmt.Errorf("failed to bootstrap replay filter from storage: %w", err)
		}
		if s.log != nil {
			s.log.InfoContext(ctx, "Replay filter bootstrapped from storage",
				utils.ZapUint64("entries", uint64(s.replayFilterSize())))
		}
	}

	// Start Kafka consumer if configured
	if s.kafkaConsumer != nil {
		if err := s.kafkaConsumer.Start(); err != nil {
			// Cleanup on error
			if s.persistWorker != nil {
				s.persistWorker.Stop()
			}
			return fmt.Errorf("failed to start Kafka consumer: %w", err)
		}
		if s.log != nil {
			s.log.InfoContext(ctx, "Kafka consumer started")
		}
	}

	// Start policy ACK consumer if configured.
	if s.policyAckCons != nil {
		if err := s.policyAckCons.Start(); err != nil {
			if s.kafkaConsumer != nil {
				_ = s.kafkaConsumer.Stop()
			}
			if s.persistWorker != nil {
				_ = s.persistWorker.Stop()
			}
			return fmt.Errorf("failed to start policy ack consumer: %w", err)
		}
		if s.log != nil {
			s.log.InfoContext(ctx, "Policy ACK consumer started")
		}
	}

	// Start policy outbox dispatcher if configured.
	if s.policyOutboxDispatcher != nil {
		if err := s.policyOutboxDispatcher.Start(ctx); err != nil {
			if s.policyAckCons != nil {
				_ = s.policyAckCons.Stop()
			}
			if s.kafkaConsumer != nil {
				_ = s.kafkaConsumer.Stop()
			}
			if s.persistWorker != nil {
				_ = s.persistWorker.Stop()
			}
			return fmt.Errorf("failed to start policy outbox dispatcher: %w", err)
		}
		if s.log != nil {
			s.log.InfoContext(ctx, "Policy outbox dispatcher started")
		}
	}

	// Start API server if configured
	if s.apiServer != nil {
		if err := s.apiServer.Start(ctx); err != nil {
			// Cleanup on error
			if s.kafkaConsumer != nil {
				s.kafkaConsumer.Stop()
			}
			if s.persistWorker != nil {
				s.persistWorker.Stop()
			}
			return fmt.Errorf("failed to start API server: %w", err)
		}
		if s.log != nil {
			s.log.InfoContext(ctx, "API server started")
		}
	}

	// Register proposal callback so every validator temporarily holds tx hashes
	// from accepted proposals, preventing rapid re-proposal across view changes
	// before commit cleanup runs.
	s.eng.RegisterProposalCallback(func(pctx context.Context, proposal interface{}) error {
		s.recordObservedProposalTxHold(proposal, time.Now())
		return nil
	})
	// Register commit callback
	s.eng.RegisterCommitCallback(func(cctx context.Context, b api.Block, qc api.QC) error {
		return s.onCommit(cctx, b, qc)
	})

	// Set startTime AFTER genesis completes to ensure grace period covers cluster formation
	s.startTime = time.Now()
	s.log.InfoContext(ctx, "service start time recorded for genesis grace period",
		utils.ZapDuration("grace_period", s.genesisGracePeriod))

	// Proposer loop
	go s.runProposer(ctx)
	go s.runPersistenceBackfill(ctx)
	go s.runReplayFilterRefresh(ctx)

	return nil
}

func (s *Service) Stop() {
	s.log.Info("wiring service shutting down...")
	if s.persistDetachedCancel != nil {
		s.persistDetachedCancel()
	}

	// Signal stop to all goroutines
	close(s.stopCh)
	if s.applyBlockObserverCleanup != nil {
		s.applyBlockObserverCleanup()
		s.applyBlockObserverCleanup = nil
	}

	// Stop API server first (stop accepting new requests)
	if s.apiServer != nil {
		if err := s.apiServer.Stop(); err != nil {
			s.log.Warn("API server stop error", utils.ZapError(err))
		} else {
			s.log.Info("API server stopped")
		}
	}

	// Stop Kafka consumer second (stop ingesting new messages)
	if s.kafkaConsumer != nil {
		if err := s.kafkaConsumer.Stop(); err != nil {
			s.log.Warn("Kafka consumer stop error", utils.ZapError(err))
		} else {
			s.log.Info("Kafka consumer stopped")
		}
	}

	// Stop policy ACK consumer (stop ingesting new ACKs).
	if s.policyAckCons != nil {
		if err := s.policyAckCons.Stop(); err != nil {
			s.log.Warn("policy ack consumer stop error", utils.ZapError(err))
		} else {
			s.log.Info("policy ack consumer stopped")
		}
	}

	// Stop policy outbox dispatcher.
	if s.policyOutboxDispatcher != nil {
		s.policyOutboxDispatcher.Stop()
		if s.log != nil {
			s.log.Info("policy outbox dispatcher stopped")
		}
	}

	// Stop persistence worker (drain pending blocks)
	if s.persistWorker != nil {
		if err := s.persistWorker.Stop(); err != nil {
			s.log.Warn("persistence worker stop error", utils.ZapError(err))
		}
	}

	// Stop Kafka producer last (after all persistence completes)
	if s.kafkaProducer != nil {
		if err := s.kafkaProducer.Close(); err != nil {
			s.log.Warn("Kafka producer close error", utils.ZapError(err))
		} else {
			s.log.Info("Kafka producer closed")
		}
	}

	// Close P2P router
	if s.router != nil {
		if err := s.router.Close(); err != nil {
			s.log.Warn("p2p router close error", utils.ZapError(err))
		} else {
			s.log.Info("p2p router closed")
		}
	}

	// Note: Consensus engine and other components are stopped by caller
	// This service just needs to stop its proposer loop

	s.log.Info("wiring service stopped gracefully")
}

// PersistenceWorker returns the active persistence worker (if any).
func (s *Service) PersistenceWorker() *PersistenceWorker {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistWorker
}

// GetPolicyOutboxDispatcherStats returns dispatcher runtime stats for API metrics.
func (s *Service) GetPolicyOutboxDispatcherStats() (policyoutbox.DispatcherStats, bool) {
	s.mu.Lock()
	dispatcher := s.policyOutboxDispatcher
	s.mu.Unlock()
	if dispatcher == nil {
		return policyoutbox.DispatcherStats{}, false
	}
	return dispatcher.Stats(), true
}

func (s *Service) NotifyPolicyOutboxDispatcher() {
	s.mu.Lock()
	dispatcher := s.policyOutboxDispatcher
	s.mu.Unlock()
	if dispatcher == nil {
		return
	}
	dispatcher.Notify()
}

// GetPolicyOutboxBacklogStats returns current outbox backlog stats for API metrics.
func (s *Service) GetPolicyOutboxBacklogStats(ctx context.Context) (policyoutbox.BacklogStats, bool) {
	s.mu.Lock()
	store := s.policyOutboxStore
	s.mu.Unlock()
	if store == nil {
		return policyoutbox.BacklogStats{}, false
	}
	stats, err := store.BacklogStats(ctx)
	if err != nil {
		return policyoutbox.BacklogStats{}, false
	}
	return stats, true
}

// GetCommitPathStats returns commit-path logging/runtime stats for API metrics.
func (s *Service) GetCommitPathStats() (apiserver.CommitPathStats, bool) {
	if s == nil {
		return apiserver.CommitPathStats{}, false
	}
	metricsSnap := s.metrics.GetSnapshot()
	s.persistBackfillMu.RLock()
	backfillPending := len(s.persistBackfillPending)
	now := time.Now()
	oldestBackfillAgeMs := uint64(0)
	for _, entry := range s.persistBackfillPending {
		if entry.seenAt.IsZero() {
			continue
		}
		ageMs := uint64(now.Sub(entry.seenAt) / time.Millisecond)
		if ageMs > oldestBackfillAgeMs {
			oldestBackfillAgeMs = ageMs
		}
	}
	s.persistBackfillMu.RUnlock()
	totalBudgetExhausted := uint64(0)
	attemptTimeoutCapped := uint64(0)
	if s.persistWorker != nil {
		pExec, npExec, droppedExec := s.persistWorker.GetExecutionStats()
		atomic.StoreUint64(&s.persistExecuteProposer, pExec)
		atomic.StoreUint64(&s.persistExecuteNonProposer, npExec)
		atomic.StoreUint64(&s.persistExecuteDroppedNonOwner, droppedExec)
		totalBudgetExhausted, attemptTimeoutCapped = s.persistWorker.GetBudgetStats()
	}
	workerTiming := PersistenceWorkerTimingStats{}
	if s.persistWorker != nil {
		workerTiming = s.persistWorker.GetTimingStats()
	}
	return apiserver.CommitPathStats{
		LogSampleEvery:                       s.commitLogEveryN,
		LogsSuppressed:                       atomic.LoadUint64(&s.commitLogsSuppressed),
		BackfillPending:                      uint64(backfillPending),
		BackfillDropped:                      atomic.LoadUint64(&s.persistBackfillDropped),
		ReplayRejectedAdmit:                  atomic.LoadUint64(&s.replayRejectedAdmit),
		ReplayRejectedBuild:                  atomic.LoadUint64(&s.replayRejectedBuild),
		ReplayFilterSize:                     uint64(s.replayFilterSize()),
		ReplayFilterEvictions:                atomic.LoadUint64(&s.replayFilterEvictions),
		PersistEnqueueCommitProposer:         atomic.LoadUint64(&s.persistEnqueueCommitProposer),
		PersistEnqueueCommitNonProposer:      atomic.LoadUint64(&s.persistEnqueueCommitNonProposer),
		PersistEnqueueBackfillProposer:       atomic.LoadUint64(&s.persistEnqueueBackfillProposer),
		PersistEnqueueBackfillNonProposer:    atomic.LoadUint64(&s.persistEnqueueBackfillNonProposer),
		PersistEnqueueDroppedNonProposer:     atomic.LoadUint64(&s.persistEnqueueDroppedNonProposer),
		PersistEnqueueErrors:                 atomic.LoadUint64(&s.persistEnqueueErrors),
		PersistEnqueueTimeouts:               atomic.LoadUint64(&s.persistEnqueueTimeouts),
		PersistEnqueueTimeoutsCommit:         atomic.LoadUint64(&s.persistEnqueueTimeoutsCommit),
		PersistEnqueueTimeoutsBackfill:       atomic.LoadUint64(&s.persistEnqueueTimeoutsBackfill),
		PersistEnqueueWaitCommitSumMs:        atomic.LoadUint64(&s.persistEnqueueWaitCommitSumMs),
		PersistEnqueueWaitCommitCount:        atomic.LoadUint64(&s.persistEnqueueWaitCommitCount),
		PersistEnqueueWaitBackfillSumMs:      atomic.LoadUint64(&s.persistEnqueueWaitBackfillSumMs),
		PersistEnqueueWaitBackfillCount:      atomic.LoadUint64(&s.persistEnqueueWaitBackfillCount),
		PersistDirectFallbackAttempts:        atomic.LoadUint64(&s.persistDirectFallbackAttempts),
		PersistDirectFallbackSuccess:         atomic.LoadUint64(&s.persistDirectFallbackSuccess),
		PersistDirectFallbackFailures:        atomic.LoadUint64(&s.persistDirectFallbackFailures),
		PersistDirectFallbackThrottled:       atomic.LoadUint64(&s.persistDirectFallbackThrottled),
		PersistDirectFallbackSkippedPressure: atomic.LoadUint64(&s.persistDirectFallbackSkippedPressure),
		PersistDirectFallbackSkippedDistress: atomic.LoadUint64(&s.persistDirectFallbackSkippedDistress),
		PersistDirectFallbackInFlight:        uint64(atomic.LoadInt64(&s.persistDirectFallbackInFlight)),
		PersistWriterTakeoverActivations:     atomic.LoadUint64(&s.persistWriterTakeoverActivations),
		BackfillOldestPendingAgeMs:           oldestBackfillAgeMs,
		BackfillNonRetryQuarantined:          atomic.LoadUint64(&s.persistBackfillNonRetryQuarantined),
		PersistTotalBudgetExhausted:          totalBudgetExhausted,
		PersistAttemptTimeoutCapped:          attemptTimeoutCapped,
		PersistOnPersistedDelaySumMs:         workerTiming.OnPersistedDelaySumMs,
		PersistOnPersistedDelayCount:         workerTiming.OnPersistedDelayCount,
		PersistMetadataUpdateSumMs:           workerTiming.MetadataUpdateSumMs,
		PersistMetadataUpdateCount:           workerTiming.MetadataUpdateCount,
		PersistMetadataUpdateFailures:        workerTiming.MetadataUpdateFailures,
		PersistExecuteProposer:               atomic.LoadUint64(&s.persistExecuteProposer),
		PersistExecuteNonProposer:            atomic.LoadUint64(&s.persistExecuteNonProposer),
		PersistExecuteDroppedNonOwner:        atomic.LoadUint64(&s.persistExecuteDroppedNonOwner),
		ApplyBlockRuns:                       metricsSnap.ApplyBlockRuns,
		ApplyBlockEventTxs:                   metricsSnap.ApplyBlockEventTxs,
		ApplyBlockEvidenceTxs:                metricsSnap.ApplyBlockEvidenceTxs,
		ApplyBlockPolicyTxs:                  metricsSnap.ApplyBlockPolicyTxs,
		ApplyBlockTotalBuckets:               metricsSnap.ApplyBlockTotalBuckets,
		ApplyBlockTotalCount:                 metricsSnap.ApplyBlockTotalCount,
		ApplyBlockTotalSumMs:                 metricsSnap.ApplyBlockTotalSumMs,
		ApplyBlockTotalP95Ms:                 metricsSnap.ApplyBlockTotalP95Ms,
		ApplyBlockValidateBuckets:            metricsSnap.ApplyBlockValidateBuckets,
		ApplyBlockValidateCount:              metricsSnap.ApplyBlockValidateCount,
		ApplyBlockValidateSumMs:              metricsSnap.ApplyBlockValidateSumMs,
		ApplyBlockValidateP95Ms:              metricsSnap.ApplyBlockValidateP95Ms,
		ApplyBlockNonceCheckBuckets:          metricsSnap.ApplyBlockNonceCheckBuckets,
		ApplyBlockNonceCheckCount:            metricsSnap.ApplyBlockNonceCheckCount,
		ApplyBlockNonceCheckSumMs:            metricsSnap.ApplyBlockNonceCheckSumMs,
		ApplyBlockNonceCheckP95Ms:            metricsSnap.ApplyBlockNonceCheckP95Ms,
		ApplyBlockReducerEventBuckets:        metricsSnap.ApplyBlockReducerEventBuckets,
		ApplyBlockReducerEventCount:          metricsSnap.ApplyBlockReducerEventCount,
		ApplyBlockReducerEventSumMs:          metricsSnap.ApplyBlockReducerEventSumMs,
		ApplyBlockReducerEventP95Ms:          metricsSnap.ApplyBlockReducerEventP95Ms,
		ApplyBlockReducerEvidenceBuckets:     metricsSnap.ApplyBlockReducerEvidenceBuckets,
		ApplyBlockReducerEvidenceCount:       metricsSnap.ApplyBlockReducerEvidenceCount,
		ApplyBlockReducerEvidenceSumMs:       metricsSnap.ApplyBlockReducerEvidenceSumMs,
		ApplyBlockReducerEvidenceP95Ms:       metricsSnap.ApplyBlockReducerEvidenceP95Ms,
		ApplyBlockReducerPolicyBuckets:       metricsSnap.ApplyBlockReducerPolicyBuckets,
		ApplyBlockReducerPolicyCount:         metricsSnap.ApplyBlockReducerPolicyCount,
		ApplyBlockReducerPolicySumMs:         metricsSnap.ApplyBlockReducerPolicySumMs,
		ApplyBlockReducerPolicyP95Ms:         metricsSnap.ApplyBlockReducerPolicyP95Ms,
		ApplyBlockCommitStateBuckets:         metricsSnap.ApplyBlockCommitStateBuckets,
		ApplyBlockCommitStateCount:           metricsSnap.ApplyBlockCommitStateCount,
		ApplyBlockCommitStateSumMs:           metricsSnap.ApplyBlockCommitStateSumMs,
		ApplyBlockCommitStateP95Ms:           metricsSnap.ApplyBlockCommitStateP95Ms,
	}, true
}

// GetPolicyAckConsumerStats returns policy ACK consumer runtime stats for API metrics.
func (s *Service) GetPolicyAckConsumerStats() (apiserver.PolicyAckConsumerStats, bool) {
	s.mu.Lock()
	cons := s.policyAckCons
	s.mu.Unlock()
	if cons == nil {
		return apiserver.PolicyAckConsumerStats{}, false
	}
	stats := cons.Stats()
	return apiserver.PolicyAckConsumerStats{
		ProcessedTotal:          stats.ProcessedTotal,
		RejectedTotal:           stats.RejectedTotal,
		StoreRetryAttempts:      stats.StoreRetryAttempts,
		StoreRetryExhausted:     stats.StoreRetryExhausted,
		DLQPublishedTotal:       stats.DLQPublishedTotal,
		DLQPublishFailures:      stats.DLQPublishFailures,
		LoopErrors:              stats.LoopErrors,
		WorkQueueWaits:          stats.WorkQueueWaits,
		SoftThrottleActivations: stats.SoftThrottleActivations,
		LogsThrottled:           stats.LogsThrottled,
		PartitionAssignments:    stats.PartitionAssignments,
		PartitionRevocations:    stats.PartitionRevocations,
		PartitionsOwnedCurrent:  stats.PartitionsOwnedCurrent,
	}, true
}

// GetPolicyAckCausalStats returns causal latency/skew stats from ACK correlation store.
func (s *Service) GetPolicyAckCausalStats() (apiserver.PolicyAckCausalStats, bool) {
	s.mu.Lock()
	cons := s.policyAckCons
	s.mu.Unlock()
	if cons == nil {
		return apiserver.PolicyAckCausalStats{}, false
	}
	stats := cons.CausalStats()
	return apiserver.PolicyAckCausalStats{
		SkewCorrectionsTotal:       stats.SkewCorrectionsTotal,
		CorrelationCommand:         stats.CorrelationCommand,
		CorrelationExact:           stats.CorrelationExact,
		CorrelationFallbackHash:    stats.CorrelationFallbackHash,
		CorrelationFallbackTrace:   stats.CorrelationFallbackTrace,
		CorrelationNoMatch:         stats.CorrelationNoMatch,
		CorrelationErrors:          stats.CorrelationErrors,
		AIEventUnitCorrections:     stats.AIEventUnitCorrections,
		AIEventInvalidTotal:        stats.AIEventInvalidTotal,
		SourceEventUnitCorrections: stats.SourceEventUnitCorrections,
		SourceEventInvalidTotal:    stats.SourceEventInvalidTotal,
		AIToAckBuckets:             stats.AIToAckBuckets,
		AIToAckCount:               stats.AIToAckCount,
		AIToAckSumMs:               stats.AIToAckSumMs,
		AIToAckP95Ms:               stats.AIToAckP95Ms,
		SourceToAckBuckets:         stats.SourceToAckBuckets,
		SourceToAckCount:           stats.SourceToAckCount,
		SourceToAckSumMs:           stats.SourceToAckSumMs,
		SourceToAckP95Ms:           stats.SourceToAckP95Ms,
		PublishToAckBuckets:        stats.PublishToAckBuckets,
		PublishToAckCount:          stats.PublishToAckCount,
		PublishToAckSumMs:          stats.PublishToAckSumMs,
		PublishToAckP95Ms:          stats.PublishToAckP95Ms,
		PublishToAppliedBuckets:    stats.PublishToAppliedBuckets,
		PublishToAppliedCount:      stats.PublishToAppliedCount,
		PublishToAppliedSumMs:      stats.PublishToAppliedSumMs,
		PublishToAppliedP95Ms:      stats.PublishToAppliedP95Ms,
		AppliedToAckBuckets:        stats.AppliedToAckBuckets,
		AppliedToAckCount:          stats.AppliedToAckCount,
		AppliedToAckSumMs:          stats.AppliedToAckSumMs,
		AppliedToAckP95Ms:          stats.AppliedToAckP95Ms,
		CorrelationLatencyBuckets:  stats.CorrelationLatencyBuckets,
		CorrelationLatencyCount:    stats.CorrelationLatencyCount,
		CorrelationLatencySumMs:    stats.CorrelationLatencySumMs,
		CorrelationLatencyP95Ms:    stats.CorrelationLatencyP95Ms,
	}, true
}

// GetPolicyPublisherStats returns policy publisher dedupe/guardrail telemetry.
func (s *Service) GetPolicyPublisherStats() (apiserver.PolicyPublisherStats, bool) {
	s.mu.Lock()
	pub := s.policyPublisher
	s.mu.Unlock()
	if pub == nil {
		return apiserver.PolicyPublisherStats{}, false
	}
	stats := pub.statsSnapshot()
	return apiserver.PolicyPublisherStats{
		DedupeSuppressedByReason: stats.DedupeSuppressedByReason,
	}, true
}

func (s *Service) GetPolicyRuntimeTrace(policyID string) []policytrace.Marker {
	s.mu.Lock()
	collector := s.policyTraceCollector
	s.mu.Unlock()
	if collector == nil {
		return nil
	}
	return collector.GetPolicy(policyID)
}

func (s *Service) shouldLogCommitInfo() bool {
	every := s.commitLogEveryN
	if every <= 1 {
		return true
	}
	n := atomic.AddUint64(&s.commitCounter, 1)
	if n%every == 0 {
		return true
	}
	atomic.AddUint64(&s.commitLogsSuppressed, 1)
	return false
}

func (s *Service) shouldLogCommitWarn(lastNs *int64, throttle time.Duration) bool {
	if throttle <= 0 {
		return true
	}
	now := time.Now().UnixNano()
	prev := atomic.LoadInt64(lastNs)
	if prev == 0 || time.Duration(now-prev) >= throttle {
		if atomic.CompareAndSwapInt64(lastNs, prev, now) {
			return true
		}
	}
	atomic.AddUint64(&s.commitLogsSuppressed, 1)
	return false
}

// StopWithTimeout stops the service with a timeout for graceful shutdown
func (s *Service) StopWithTimeout(timeout time.Duration) error {
	s.log.Info("wiring service shutting down with timeout...",
		utils.ZapDuration("timeout", timeout))

	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("shutdown timeout exceeded")
	}
}
