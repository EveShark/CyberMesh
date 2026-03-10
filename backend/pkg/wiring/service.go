package wiring

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
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
	"backend/pkg/control/policytrace"
	"backend/pkg/ingest/kafka"
	"backend/pkg/mempool"
	"backend/pkg/p2p"
	"backend/pkg/state"
	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"
	"github.com/IBM/sarama"
)

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
	kafkaConsumer               *kafka.Consumer
	kafkaProducer               *kafka.Producer
	policyPublisher             *policyPublisher
	policyPublishOnCommit       bool
	policyPublishOnPersistence  bool
	policyCommitProposerOnly    bool
	persistCommitProposerOnly   bool
	persistBackfillEnabled      bool
	persistBackfillLagThreshold uint64
	persistBackfillStaleAfter   time.Duration
	persistBackfillPollInterval time.Duration
	persistBackfillMaxPerCycle  int
	persistBackfillPendingMax   int
	replayFilterEnabled         bool
	replayFilterTTL             time.Duration
	replayFilterMax             int
	replayRefreshEnabled        bool
	replayRefreshLeaderOnly     bool
	replayRefreshInterval       time.Duration
	replayRefreshHeightWindow   uint64
	replayRefreshMaxHeightsTick int
	policyOutboxDispatcher      *policyoutbox.Dispatcher
	policyOutboxStore           *policyoutbox.Store
	policyAckCons               *policyack.Consumer
	policyTraceCollector        *policytrace.Collector

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

	persistEnqueueCommitProposer      uint64
	persistEnqueueCommitNonProposer   uint64
	persistEnqueueBackfillProposer    uint64
	persistEnqueueBackfillNonProposer uint64
	persistEnqueueDroppedNonProposer  uint64
	persistEnqueueErrors              uint64
	persistExecuteProposer            uint64
	persistExecuteNonProposer         uint64
	persistExecuteDroppedNonOwner     uint64
	applyBlockObserverCleanup         func()

	// test hook for persistence writer ownership decision.
	persistStatusFn func() (ctypes.ValidatorID, bool)
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
		replayCommittedTx:      make(map[[32]byte]time.Time),
		policyTraceCollector:   policytrace.NewCollector(4096, 64),
	}
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
	s.persistBackfillEnabled = true
	s.persistBackfillLagThreshold = 1
	s.persistBackfillStaleAfter = 15 * time.Second
	s.persistBackfillPollInterval = 5 * time.Second
	s.persistBackfillMaxPerCycle = 4
	s.persistBackfillPendingMax = 2048
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
		persistWriterMode := strings.ToLower(strings.TrimSpace(cfg.ConfigManager.GetString("PERSIST_COMMIT_WRITER_MODE", "proposer")))
		switch persistWriterMode {
		case "all":
			s.persistCommitProposerOnly = false
		case "proposer", "":
			s.persistCommitProposerOnly = true
		default:
			s.persistCommitProposerOnly = true
			if log != nil {
				log.Warn("Invalid PERSIST_COMMIT_WRITER_MODE; defaulting to proposer (fail-closed)",
					utils.ZapString("value", persistWriterMode))
			}
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
				utils.ZapBool("persist_writer_proposer_only", s.persistCommitProposerOnly),
				utils.ZapBool("persist_backfill_enabled", s.persistBackfillEnabled),
				utils.ZapDuration("persist_backfill_stale_after", s.persistBackfillStaleAfter),
				utils.ZapDuration("persist_backfill_poll_interval", s.persistBackfillPollInterval),
				utils.ZapUint64("persist_backfill_lag_threshold", s.persistBackfillLagThreshold),
				utils.ZapInt("persist_backfill_max_per_cycle", s.persistBackfillMaxPerCycle),
				utils.ZapInt("persist_backfill_pending_max", s.persistBackfillPendingMax),
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

	var dbHandle *sql.DB
	if provider, ok := cfg.DBAdapter.(interface{ GetDB() *sql.DB }); ok {
		dbHandle = provider.GetDB()
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
		// Only set up producer when a commits topic is configured
		if cfg.KafkaProducerCfg.Topics.Commits != "" {
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
					policySignerCfg := kafka.CommitSignerConfig{
						KeyPath:    policyKeyPath,
						KeyID:      cfg.ConfigManager.GetString("CONTROL_POLICY_SIGNING_KEY_ID", signerCfg.KeyID),
						Domain:     cfg.ConfigManager.GetString("CONTROL_POLICY_SIGNING_DOMAIN", "control.policy.v1"),
						ProducerID: cfg.ConfigManager.GetString("CONTROL_POLICY_PRODUCER_ID", cfg.ConfigManager.GetString("CONTROL_PRODUCER_ID", "")),
						Logger:     log,
					}

					policySigner, err := kafka.NewCommitSigner(policySignerCfg)
					if err != nil {
						return nil, fmt.Errorf("failed to initialize policy signer: %w", err)
					}
					cfg.KafkaProducerCfg.PolicySigner = policySigner
				} else if cfg.KafkaProducerCfg.Topics.Policy != "" && log != nil {
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

				if cfg.KafkaProducerCfg.Topics.Policy != "" {
					pp, err := newPolicyPublisher(kafkaProducer, cfg.ConfigManager, cfg.KafkaProducerCfg.Topics.Policy, log, cfg.AuditLogger)
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

						outboxCfg := policyoutbox.Config{
							Enabled:             true,
							LeaseKey:            strings.TrimSpace(cfg.ConfigManager.GetString("CONTROL_POLICY_OUTBOX_LEASE_KEY", "control.policy.dispatcher")),
							PolicyTopic:         cfg.KafkaProducerCfg.Topics.Policy,
							ScopeRoutingEnabled: cfg.ConfigManager.GetBool("CONTROL_POLICY_SCOPE_ROUTING_ENABLED", false),
							ClusterShardingMode: strings.ToLower(strings.TrimSpace(cfg.ConfigManager.GetString("CONTROL_POLICY_CLUSTER_SHARDING_MODE", "off"))),
							ClusterShardBuckets: cfg.ConfigManager.GetInt("CONTROL_POLICY_CLUSTER_SHARD_BUCKETS", 1),
							LeaseTTL:            cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_LEASE_TTL", 10*time.Second),
							ReclaimAfter:        cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_RECLAIM_AFTER", 30*time.Second),
							PollInterval:        cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_POLL_INTERVAL", 500*time.Millisecond),
							DrainMaxDuration:    cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_DRAIN_MAX_DURATION", 2*time.Second),
							BatchSize:           cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_BATCH_SIZE", 100),
							BatchSizeMin:        cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_BATCH_SIZE_MIN", 25),
							BatchSizeMax:        cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_BATCH_SIZE_MAX", 400),
							AdaptiveBatch:       cfg.ConfigManager.GetBool("CONTROL_POLICY_OUTBOX_ADAPTIVE_BATCH", true),
							MaxInFlight:         cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_MAX_IN_FLIGHT", 8),
							MarkWorkers:         cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_MARK_WORKERS", 4),
							InternalQueue:       cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_INTERNAL_QUEUE_SIZE", 256),
							DrainMaxBatches:     cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_DRAIN_BATCHES", 4),
							MaxRetries:          cfg.ConfigManager.GetInt("CONTROL_POLICY_OUTBOX_MAX_RETRIES", 8),
							RetryInitialBack:    cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_RETRY_INITIAL", 250*time.Millisecond),
							RetryMaxBack:        cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_RETRY_MAX", 30*time.Second),
							RetryJitterRatio:    cfg.ConfigManager.GetFloat64("CONTROL_POLICY_OUTBOX_RETRY_JITTER_RATIO", 0.2),
							LogThrottle:         cfg.ConfigManager.GetDuration("CONTROL_POLICY_OUTBOX_LOG_THROTTLE", 5*time.Second),
						}

						holderID := strings.TrimSpace(cfg.ConfigManager.GetString("NODE_ID", "backend-node"))
						dispatcher, err := policyoutbox.NewDispatcher(outboxCfg, outboxStore, pp, log, cfg.AuditLogger, holderID, s.policyTraceCollector)
						if err != nil {
							kafkaProducer.Close()
							return nil, fmt.Errorf("failed to initialize policy outbox dispatcher: %w", err)
						}
						s.policyOutboxDispatcher = dispatcher
						s.policyOutboxStore = outboxStore

						// Outbox is the authority; disable direct policy publish paths.
						s.policyPublishOnCommit = false
						s.policyPublishOnPersistence = false
						if log != nil {
							log.Info("Control policy outbox enabled; direct policy publish disabled")
						}
					}
				}
			}
		}
	}

	// Initialize persistence worker if enabled
	if cfg.EnablePersistence && cfg.DBAdapter != nil {
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
			s.persistWorker.SetWriterPolicy(s.persistCommitProposerOnly, status.NodeID)
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
		apiDeps := apiserver.Dependencies{
			Config:        cfg.APIConfig,
			Logger:        log,
			AuditLogger:   cfg.AuditLogger,
			Storage:       cfg.DBAdapter,
			StateStore:    store,
			Mempool:       mp,
			Engine:        eng,
			P2PRouter:     cfg.P2PRouter,
			KafkaProd:     s.kafkaProducer,
			KafkaCons:     s.kafkaConsumer,
			OutboxStats:   s,
			TraceStats:    s,
			NodeAliases:   cfg.APIConfig.NodeAliasMap,
			NodeAliasList: cfg.APIConfig.NodeAliasList,
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
	s.persistBackfillMu.RUnlock()
	if s.persistWorker != nil {
		pExec, npExec, droppedExec := s.persistWorker.GetExecutionStats()
		atomic.StoreUint64(&s.persistExecuteProposer, pExec)
		atomic.StoreUint64(&s.persistExecuteNonProposer, npExec)
		atomic.StoreUint64(&s.persistExecuteDroppedNonOwner, droppedExec)
	}
	return apiserver.CommitPathStats{
		LogSampleEvery:                    s.commitLogEveryN,
		LogsSuppressed:                    atomic.LoadUint64(&s.commitLogsSuppressed),
		BackfillPending:                   uint64(backfillPending),
		BackfillDropped:                   atomic.LoadUint64(&s.persistBackfillDropped),
		ReplayRejectedAdmit:               atomic.LoadUint64(&s.replayRejectedAdmit),
		ReplayRejectedBuild:               atomic.LoadUint64(&s.replayRejectedBuild),
		ReplayFilterSize:                  uint64(s.replayFilterSize()),
		ReplayFilterEvictions:             atomic.LoadUint64(&s.replayFilterEvictions),
		PersistEnqueueCommitProposer:      atomic.LoadUint64(&s.persistEnqueueCommitProposer),
		PersistEnqueueCommitNonProposer:   atomic.LoadUint64(&s.persistEnqueueCommitNonProposer),
		PersistEnqueueBackfillProposer:    atomic.LoadUint64(&s.persistEnqueueBackfillProposer),
		PersistEnqueueBackfillNonProposer: atomic.LoadUint64(&s.persistEnqueueBackfillNonProposer),
		PersistEnqueueDroppedNonProposer:  atomic.LoadUint64(&s.persistEnqueueDroppedNonProposer),
		PersistEnqueueErrors:              atomic.LoadUint64(&s.persistEnqueueErrors),
		PersistExecuteProposer:            atomic.LoadUint64(&s.persistExecuteProposer),
		PersistExecuteNonProposer:         atomic.LoadUint64(&s.persistExecuteNonProposer),
		PersistExecuteDroppedNonOwner:     atomic.LoadUint64(&s.persistExecuteDroppedNonOwner),
		ApplyBlockRuns:                    metricsSnap.ApplyBlockRuns,
		ApplyBlockEventTxs:                metricsSnap.ApplyBlockEventTxs,
		ApplyBlockEvidenceTxs:             metricsSnap.ApplyBlockEvidenceTxs,
		ApplyBlockPolicyTxs:               metricsSnap.ApplyBlockPolicyTxs,
		ApplyBlockTotalBuckets:            metricsSnap.ApplyBlockTotalBuckets,
		ApplyBlockTotalCount:              metricsSnap.ApplyBlockTotalCount,
		ApplyBlockTotalSumMs:              metricsSnap.ApplyBlockTotalSumMs,
		ApplyBlockTotalP95Ms:              metricsSnap.ApplyBlockTotalP95Ms,
		ApplyBlockValidateBuckets:         metricsSnap.ApplyBlockValidateBuckets,
		ApplyBlockValidateCount:           metricsSnap.ApplyBlockValidateCount,
		ApplyBlockValidateSumMs:           metricsSnap.ApplyBlockValidateSumMs,
		ApplyBlockValidateP95Ms:           metricsSnap.ApplyBlockValidateP95Ms,
		ApplyBlockNonceCheckBuckets:       metricsSnap.ApplyBlockNonceCheckBuckets,
		ApplyBlockNonceCheckCount:         metricsSnap.ApplyBlockNonceCheckCount,
		ApplyBlockNonceCheckSumMs:         metricsSnap.ApplyBlockNonceCheckSumMs,
		ApplyBlockNonceCheckP95Ms:         metricsSnap.ApplyBlockNonceCheckP95Ms,
		ApplyBlockReducerEventBuckets:     metricsSnap.ApplyBlockReducerEventBuckets,
		ApplyBlockReducerEventCount:       metricsSnap.ApplyBlockReducerEventCount,
		ApplyBlockReducerEventSumMs:       metricsSnap.ApplyBlockReducerEventSumMs,
		ApplyBlockReducerEventP95Ms:       metricsSnap.ApplyBlockReducerEventP95Ms,
		ApplyBlockReducerEvidenceBuckets:  metricsSnap.ApplyBlockReducerEvidenceBuckets,
		ApplyBlockReducerEvidenceCount:    metricsSnap.ApplyBlockReducerEvidenceCount,
		ApplyBlockReducerEvidenceSumMs:    metricsSnap.ApplyBlockReducerEvidenceSumMs,
		ApplyBlockReducerEvidenceP95Ms:    metricsSnap.ApplyBlockReducerEvidenceP95Ms,
		ApplyBlockReducerPolicyBuckets:    metricsSnap.ApplyBlockReducerPolicyBuckets,
		ApplyBlockReducerPolicyCount:      metricsSnap.ApplyBlockReducerPolicyCount,
		ApplyBlockReducerPolicySumMs:      metricsSnap.ApplyBlockReducerPolicySumMs,
		ApplyBlockReducerPolicyP95Ms:      metricsSnap.ApplyBlockReducerPolicyP95Ms,
		ApplyBlockCommitStateBuckets:      metricsSnap.ApplyBlockCommitStateBuckets,
		ApplyBlockCommitStateCount:        metricsSnap.ApplyBlockCommitStateCount,
		ApplyBlockCommitStateSumMs:        metricsSnap.ApplyBlockCommitStateSumMs,
		ApplyBlockCommitStateP95Ms:        metricsSnap.ApplyBlockCommitStateP95Ms,
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
