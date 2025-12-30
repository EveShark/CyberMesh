package api

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/consensus/genesis"
	"backend/pkg/consensus/leader"
	"backend/pkg/consensus/messages"
	"backend/pkg/consensus/pbft"
	ctypes "backend/pkg/consensus/types"
	"backend/pkg/utils"
)

// Re-export types for convenience

// ActivationStatus summarizes readiness metrics used to activate consensus components.
type ActivationStatus struct {
	ReadyValidators int
	RequiredReady   int
	ConnectedPeers  int
	ActivePeers     int
	PeersSeen       int
	RequiredPeers   int
	HasPeerObserver bool
	HasQuorum       bool
	SeenQuorum      bool
	GenesisPhase    bool
}

// ConsensusEngine orchestrates all consensus components
type ConsensusEngine struct {
	// Core components
	encoder             *messages.Encoder
	validator           *messages.Validator
	rotation            *leader.Rotation
	pacemaker           *leader.Pacemaker
	heartbeat           *leader.HeartbeatManager
	heartbeatController *activationHeartbeatController
	quorum              *pbft.QuorumVerifier
	storage             *pbft.Storage
	hotstuff            *pbft.HotStuff
	genesis             *genesis.Coordinator

	// Infrastructure
	crypto    CryptoService
	audit     AuditLogger
	logger    Logger
	configMgr ConfigManager
	allowlist IPAllowlist

	// Configuration
	config         *EngineConfig
	validatorSet   ValidatorSet
	genesisBackend ctypes.StorageBackend

	// State
	running         bool
	commitCallbacks []CommitCallback

	// Metrics
	metrics             *EngineMetrics
	genesisReadyMetrics genesisReadyMetrics
	genesisReadyTrace   genesisReadyTrace
	genesisDiscarded    atomic.Uint64

	// Drift repair telemetry (best-effort, used for readiness diagnostics)
	driftLastRepairUnix atomic.Int64
	driftLastError      atomic.Value // string

	readyMu         sync.RWMutex
	readyValidators map[ctypes.ValidatorID]time.Time
	localReady      bool

	mu     sync.RWMutex
	stopCh chan struct{}

	consensusActive bool
	activationMu    sync.Mutex

	// Network publisher (optional)
	net NetworkPublisher

	peerObserver ctypes.PeerObserver

	// Persistence worker (optional)
	persistence PersistenceWorker

	handshake struct {
		leader   leaderHandshakeState
		follower followerHandshakeState
	}
	handshakeTimeout time.Duration

	activationCheckInterval time.Duration
	activationMaxWait       time.Duration
	activationActiveWindow  time.Duration
	activationForce         bool
	activationSeenGates     bool
}

type DriftStatus struct {
	LastRepairUnix int64  `json:"last_repair_unix"`
	LastError      string `json:"last_error"`
}

type leaderHandshakeState struct {
	mu           sync.Mutex
	active       bool
	completed    bool
	view         uint64
	height       uint64
	requiredAcks int
	nonce        [32]byte
	acks         map[ctypes.ValidatorID]struct{}
	waiters      []chan struct{}
}

type followerHandshakeState struct {
	mu         sync.Mutex
	lastView   uint64
	lastHeight uint64
	lastNonce  [32]byte
}

func (e *ConsensusEngine) computeRequiredAcks() int {
	if e.validatorSet == nil {
		return 0
	}
	count := e.validatorSet.GetValidatorCount()
	if count <= 1 {
		return 0
	}
	f := (count - 1) / 3
	quorum := 2*f + 1
	if quorum <= 1 {
		return 0
	}
	required := quorum - 1 // exclude leader
	if required < 0 {
		required = 0
	}
	return required
}

type noopAuditLogger struct{}

func (noopAuditLogger) Info(string, map[string]interface{}) error     { return nil }
func (noopAuditLogger) Warn(string, map[string]interface{}) error     { return nil }
func (noopAuditLogger) Error(string, map[string]interface{}) error    { return nil }
func (noopAuditLogger) Security(string, map[string]interface{}) error { return nil }
func (noopAuditLogger) LogContext(context.Context, string, string, map[string]interface{}) error {
	return nil
}

// EngineConfig contains consensus engine configuration
type EngineConfig struct {
	NodeID              ValidatorID
	NodeType            string
	EnableProposing     bool
	EnableVoting        bool
	BlockTimeout        time.Duration
	MetricsEnabled      bool
	HealthCheckInterval time.Duration
	Topics              map[messages.MessageType]string
}

// EngineMetrics tracks consensus performance
type EngineMetrics struct {
	ProposalsReceived              uint64
	ProposalsSent                  uint64
	VotesReceived                  uint64
	VotesSent                      uint64
	QCsFormed                      uint64
	BlocksCommitted                uint64
	InvalidBlocks                  uint64
	ProposalRejectMissingJustifyQC uint64
	BootstrapHeightQCMismatch      uint64
	ViewChanges                    uint64
	EquivocationsDetected          uint64
	ParentHashMismatches           uint64
	LastCommitTime                 time.Time
	AverageCommitLatency           time.Duration

	mu sync.RWMutex
}

type GenesisReadyMetricsSnapshot struct {
	Received      uint64
	Accepted      uint64
	Duplicate     uint64
	ReplayBlocked uint64
	Rejected      uint64
	Discarded     uint64
}

type genesisReadyMetrics struct {
	received      uint64
	accepted      uint64
	duplicate     uint64
	replayBlocked uint64
	rejected      uint64
}

func (m *genesisReadyMetrics) record(event genesis.ReadyEvent) {
	switch event {
	case genesis.ReadyEventReceived:
		atomic.AddUint64(&m.received, 1)
	case genesis.ReadyEventAccepted:
		atomic.AddUint64(&m.accepted, 1)
	case genesis.ReadyEventDuplicate:
		atomic.AddUint64(&m.duplicate, 1)
	case genesis.ReadyEventReplayBlocked:
		atomic.AddUint64(&m.replayBlocked, 1)
	case genesis.ReadyEventRejected:
		atomic.AddUint64(&m.rejected, 1)
	}
}

func (m *genesisReadyMetrics) snapshot() GenesisReadyMetricsSnapshot {
	return GenesisReadyMetricsSnapshot{
		Received:      atomic.LoadUint64(&m.received),
		Accepted:      atomic.LoadUint64(&m.accepted),
		Duplicate:     atomic.LoadUint64(&m.duplicate),
		ReplayBlocked: atomic.LoadUint64(&m.replayBlocked),
		Rejected:      atomic.LoadUint64(&m.rejected),
	}
}

type GenesisReadyTraceRecord struct {
	Timestamp time.Time
	Event     genesis.ReadyEvent
	Validator ctypes.ValidatorID
	Hash      [32]byte
	Reason    string
}

type genesisReadyTrace struct {
	mu     sync.Mutex
	limit  int
	events []GenesisReadyTraceRecord
}

func (t *genesisReadyTrace) record(record GenesisReadyTraceRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.limit <= 0 {
		t.limit = 200
	}

	t.events = append(t.events, record)
	if len(t.events) > t.limit {
		start := len(t.events) - t.limit
		trimmed := make([]GenesisReadyTraceRecord, t.limit)
		copy(trimmed, t.events[start:])
		t.events = trimmed
	}
}

func (t *genesisReadyTrace) snapshot(limit int) []GenesisReadyTraceRecord {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.events) == 0 {
		return nil
	}

	count := len(t.events)
	if limit > 0 && limit < count {
		count = limit
	}
	start := len(t.events) - count
	result := make([]GenesisReadyTraceRecord, count)
	copy(result, t.events[start:])
	return result
}

// CommitCallback is called when a block is committed
type CommitCallback func(ctx context.Context, block Block, qc QC) error

// NetworkPublisher publishes consensus messages onto the network layer.
type NetworkPublisher interface {
	Publish(ctx context.Context, topic string, data []byte) error
}

// PersistenceWorker provides access to the consensus storage backend.
type PersistenceWorker interface {
	GetStorageBackend() pbft.StorageBackend
}

func (e *ConsensusEngine) SetPersistence(worker PersistenceWorker) {
	e.persistence = worker
}

// AttachPersistence sets the persistence worker and wires its backend into the
// consensus storage if available. Intended for wiring scenarios where the
// persistence worker is constructed after the engine.
func (e *ConsensusEngine) AttachPersistence(worker PersistenceWorker) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.persistence = worker
	if e.storage != nil && worker != nil {
		backend := worker.GetStorageBackend()
		if backend != nil {
			e.storage.SetBackend(backend)
		}
	}
}

type noopNetworkPublisher struct {
	logger Logger
}

// NewNoopNetworkPublisher returns a NetworkPublisher that simply logs the publish
// attempt. It is used to keep single-node deployments running when networking is disabled.
func NewNoopNetworkPublisher(logger Logger) NetworkPublisher {
	return noopNetworkPublisher{logger: logger}
}

func (p noopNetworkPublisher) Publish(ctx context.Context, topic string, data []byte) error {
	if p.logger != nil {
		p.logger.InfoContext(ctx, "[NETWORK] skipping publish (network disabled)",
			utils.ZapString("topic", topic),
			utils.ZapInt("bytes", len(data)),
		)
	}
	return nil
}

// DefaultEngineConfig returns secure defaults
func DefaultEngineConfig() *EngineConfig {
	return &EngineConfig{
		EnableProposing:     true,
		EnableVoting:        true,
		BlockTimeout:        5 * time.Second,
		MetricsEnabled:      true,
		HealthCheckInterval: 10 * time.Second,
	}
}

// NewConsensusEngine creates a new consensus engine
func NewConsensusEngine(
	crypto CryptoService,
	audit AuditLogger,
	logger Logger,
	configMgr ConfigManager,
	allowlist IPAllowlist,
	validatorSet ValidatorSet,
	config *EngineConfig,
	genesisBackend ctypes.StorageBackend,
) (*ConsensusEngine, error) {
	if config == nil {
		config = DefaultEngineConfig()
	}

	if audit == nil {
		audit = noopAuditLogger{}
	}

	engine := &ConsensusEngine{
		crypto:            crypto,
		audit:             audit,
		logger:            logger,
		configMgr:         configMgr,
		allowlist:         allowlist,
		validatorSet:      validatorSet,
		config:            config,
		commitCallbacks:   make([]CommitCallback, 0),
		metrics:           &EngineMetrics{},
		genesisReadyTrace: genesisReadyTrace{limit: 200},
		stopCh:            make(chan struct{}),
		readyValidators:   make(map[ctypes.ValidatorID]time.Time),
		genesisBackend:    genesisBackend,
	}

	// Default to a no-op network publisher so single-node deployments continue running
	// even when P2P networking is disabled. Multi-node wiring overrides this via SetNetwork.
	engine.net = NewNoopNetworkPublisher(logger)

	// Initialize components
	if err := engine.initializeComponents(); err != nil {
		return nil, fmt.Errorf("failed to initialize components: %w", err)
	}

	return engine, nil
}

// initializeComponents initializes all consensus components
func (e *ConsensusEngine) initializeComponents() error {
	// 1. Initialize message encoder
	encoderConfig := &messages.EncoderConfig{
		MaxProposalSize:           e.configMgr.GetInt("CONSENSUS_MAX_PROPOSAL_SIZE", 1<<20),
		MaxVoteSize:               e.configMgr.GetInt("CONSENSUS_MAX_VOTE_SIZE", 10<<10),
		MaxQCSize:                 e.configMgr.GetInt("CONSENSUS_MAX_QC_SIZE", 100<<10),
		MaxViewChangeSize:         e.configMgr.GetInt("CONSENSUS_MAX_VIEWCHANGE_SIZE", 50<<10),
		MaxNewViewSize:            e.configMgr.GetInt("CONSENSUS_MAX_NEWVIEW_SIZE", 500<<10),
		MaxHeartbeatSize:          e.configMgr.GetInt("CONSENSUS_MAX_HEARTBEAT_SIZE", 5<<10),
		MaxEvidenceSize:           e.configMgr.GetInt("CONSENSUS_MAX_EVIDENCE_SIZE", 200<<10),
		MaxGenesisReadySize:       e.configMgr.GetInt("CONSENSUS_MAX_GENESIS_READY_SIZE", 16<<10),
		MaxGenesisCertificateSize: e.configMgr.GetInt("CONSENSUS_MAX_GENESIS_CERT_SIZE", 64<<10),
		MaxProposalIntentSize:     e.configMgr.GetInt("CONSENSUS_MAX_PROPOSAL_INTENT_SIZE", 4<<10),
		MaxReadyToVoteSize:        e.configMgr.GetInt("CONSENSUS_MAX_READY_TO_VOTE_SIZE", 4<<10),
		ClockSkewTolerance:        e.configMgr.GetDuration("CONSENSUS_CLOCK_SKEW_TOLERANCE", 10*time.Second),
		GenesisClockSkewTolerance: e.configMgr.GetDuration("CONSENSUS_GENESIS_CLOCK_SKEW_TOLERANCE", 24*time.Hour),
		VerifyCacheSize:           e.configMgr.GetInt("CONSENSUS_VERIFY_CACHE_SIZE", 10000),
		VerifyCacheTTL:            e.configMgr.GetDuration("CONSENSUS_VERIFY_CACHE_TTL", 5*time.Minute),
		RejectFutureMessages:      e.configMgr.GetBool("CONSENSUS_REJECT_FUTURE_MESSAGES", true),
	}

	var err error
	e.encoder, err = messages.NewEncoder(e.crypto, encoderConfig)
	if err != nil {
		return fmt.Errorf("failed to create encoder: %w", err)
	}

	// 2. Initialize leader rotation (created before validator to support GetLeaderForView)
	rotationConfig := &leader.RotationConfig{
		MinReputation:       e.configMgr.GetFloat64("CONSENSUS_MIN_LEADER_REPUTATION", 0.7),
		EnableQuarantine:    e.configMgr.GetBool("CONSENSUS_ENABLE_QUARANTINE", true),
		EnableReputation:    e.configMgr.GetBool("CONSENSUS_ENABLE_REPUTATION", true),
		SelectionSeed:       uint64(e.configMgr.GetInt("CONSENSUS_SELECTION_SEED", 0)),
		FallbackToAll:       e.configMgr.GetBool("CONSENSUS_FALLBACK_TO_ALL", true),
		MaxQuarantinedRatio: e.configMgr.GetFloat64("CONSENSUS_MAX_QUARANTINED_RATIO", 0.33),
	}
	e.rotation = leader.NewRotation(e.validatorSet, nil, e.audit, e.logger, rotationConfig)
	e.rotation.SetReadinessOracle(e)

	// 3. Initialize storage
	storageConfig := &pbft.StorageConfig{
		ReplayWindowSize:  e.configMgr.GetInt("CONSENSUS_REPLAY_WINDOW_SIZE", 100),
		MaxProposals:      e.configMgr.GetInt("CONSENSUS_MAX_PROPOSALS", 1000),
		MaxVotesPerView:   e.configMgr.GetInt("CONSENSUS_MAX_VOTES_PER_VIEW", 10000),
		MaxQCs:            e.configMgr.GetInt("CONSENSUS_MAX_QCS", 1000),
		MaxEvidence:       e.configMgr.GetInt("CONSENSUS_MAX_EVIDENCE", 10000),
		EnablePersistence: e.configMgr.GetBool("CONSENSUS_ENABLE_PERSISTENCE", true),
		PersistInterval:   e.configMgr.GetDuration("CONSENSUS_PERSIST_INTERVAL", 10*time.Second),
		AutoPrune:         e.configMgr.GetBool("CONSENSUS_AUTO_PRUNE", true),
		MinRetainBlocks:   e.configMgr.GetInt("CONSENSUS_MIN_RETAIN_BLOCKS", 10),
	}
	var storageBackend pbft.StorageBackend
	if e.genesisBackend != nil {
		storageBackend = e.genesisBackend
	}
	if storageBackend == nil && e.persistence != nil {
		storageBackend = e.persistence.GetStorageBackend()
	}
	e.storage = pbft.NewStorage(storageBackend, e.audit, e.logger, storageConfig)

	// 4. Initialize message validator (requires consensus state and validator-set adapter)
	// HotStuff requires leaders to vote their own proposals; force-enable self voting regardless of env
	_ = e.configMgr.GetBool("CONSENSUS_ALLOW_SELF_VOTING", true) // read for completeness
	validationConfig := &messages.ValidationConfig{
		MaxViewJump:         uint64(e.configMgr.GetInt("CONSENSUS_MAX_VIEW_JUMP", 100)),
		MaxHeightJump:       uint64(e.configMgr.GetInt("CONSENSUS_MAX_HEIGHT_JUMP", 10)),
		RequireJustifyQC:    e.configMgr.GetBool("CONSENSUS_REQUIRE_JUSTIFY_QC", true),
		StrictMonotonicity:  e.configMgr.GetBool("CONSENSUS_STRICT_MONOTONICITY", true),
		MinQuorumSize:       e.configMgr.GetInt("CONSENSUS_MIN_QUORUM_SIZE", 1),
		MaxQuorumSize:       e.configMgr.GetInt("CONSENSUS_MAX_QUORUM_SIZE", 10000),
		AllowSelfVoting:     true,
		MaxViewChangeProofs: e.configMgr.GetInt("CONSENSUS_MAX_VIEWCHANGE_PROOFS", 1000),
		MaxEvidenceAge:      e.configMgr.GetDuration("CONSENSUS_MAX_EVIDENCE_AGE", 24*time.Hour),
	}
	vsAdapter := newValidatorSetAdapter(e.validatorSet, e.rotation)
	e.encoder.SetValidatorSet(vsAdapter)
	state := newEngineState(e)
	e.validator = messages.NewValidator(vsAdapter, state, e.encoder, validationConfig, e.logger)

	// 5. Initialize quorum verifier
	quorumConfig := &pbft.QuorumConfig{
		EnableCache:         e.configMgr.GetBool("CONSENSUS_ENABLE_QC_CACHE", true),
		CacheSize:           e.configMgr.GetInt("CONSENSUS_QC_CACHE_SIZE", 10000),
		CacheTTL:            int64(e.configMgr.GetInt("CONSENSUS_QC_CACHE_TTL", 300)),
		StrictValidation:    e.configMgr.GetBool("CONSENSUS_STRICT_VALIDATION", true),
		RequireUniqueVoters: e.configMgr.GetBool("CONSENSUS_REQUIRE_UNIQUE_VOTERS", true),
		AllowSelfVoting:     true,
	}

	e.quorum = pbft.NewQuorumVerifier(e.validatorSet, newEncoderAdapter(e.encoder), e.audit, e.logger, quorumConfig)

	// 6. Initialize pacemaker
	pacemakerConfig := &leader.PacemakerConfig{
		BaseTimeout:           e.configMgr.GetDuration("CONSENSUS_BASE_TIMEOUT", 10*time.Second), // Increased from 2s for container startup variance
		MaxTimeout:            e.configMgr.GetDuration("CONSENSUS_MAX_TIMEOUT", 60*time.Second),
		MinTimeout:            e.configMgr.GetDuration("CONSENSUS_MIN_TIMEOUT", 5*time.Second), // Increased from 1s
		TimeoutIncreaseFactor: e.configMgr.GetFloat64("CONSENSUS_TIMEOUT_INCREASE_FACTOR", 1.5),
		TimeoutDecreaseDelta:  e.configMgr.GetDuration("CONSENSUS_TIMEOUT_DECREASE_DELTA", 200*time.Millisecond),
		JitterPercent:         e.configMgr.GetFloat64("CONSENSUS_JITTER_PERCENT", 0.1),
		ViewChangeTimeout:     e.configMgr.GetDuration("CONSENSUS_VIEWCHANGE_TIMEOUT", 30*time.Second),
		EnableAIMD:            e.configMgr.GetBool("CONSENSUS_ENABLE_AIMD", true),
		RequireQuorum:         e.configMgr.GetBool("CONSENSUS_REQUIRE_QUORUM", true),
	}

	e.pacemaker = leader.NewPacemaker(e.rotation, e.validatorSet, e.crypto, e.audit, e.logger, pacemakerConfig, e, newPacemakerPublisherAdapter(e))

	// 7. Initialize heartbeat manager
	heartbeatConfig := &leader.HeartbeatConfig{
		SendInterval:     e.configMgr.GetDuration("CONSENSUS_HEARTBEAT_INTERVAL", 500*time.Millisecond),
		MaxIdleTime:      e.configMgr.GetDuration("CONSENSUS_MAX_IDLE_TIME", 3*time.Second),
		MissedBeforeFail: e.configMgr.GetInt("CONSENSUS_MISSED_HEARTBEATS", 6),
		EnableSending:    e.config.EnableProposing,
		EnableChecking:   true,
		JitterPercent:    e.configMgr.GetFloat64("CONSENSUS_HEARTBEAT_JITTER", 0.05),
	}

	e.heartbeatController = &activationHeartbeatController{}
	e.heartbeat = leader.NewHeartbeatManager(e.rotation, e.pacemaker, e.crypto, e.audit, e.logger, heartbeatConfig, e, newHeartbeatPublisherAdapter(e), e.heartbeatController)

	// 8. Initialize HotStuff engine
	hotstuffConfig := &pbft.HotStuffConfig{
		ValidatorID:            e.config.NodeID,
		EnableVoting:           e.config.EnableVoting,
		EnableProposing:        e.config.EnableProposing,
		MaxPendingVotes:        e.configMgr.GetInt("CONSENSUS_MAX_PENDING_VOTES", 1000),
		VoteTimeout:            e.configMgr.GetDuration("CONSENSUS_VOTE_TIMEOUT", 5*time.Second),
		SafetyCheckStrict:      e.configMgr.GetBool("CONSENSUS_SAFETY_CHECK_STRICT", true),
		AutoCleanupOldVotes:    e.configMgr.GetBool("CONSENSUS_AUTO_CLEANUP_VOTES", true),
		MaxConcurrentProposals: e.configMgr.GetInt("CONSENSUS_MAX_CONCURRENT_PROPOSALS", 10),
	}

	e.hotstuff = pbft.NewHotStuff(
		e.validatorSet,
		e.rotation,
		e.pacemaker,
		e.quorum,
		e.storage,
		newEncoderAdapter(e.encoder),
		newValidatorAdapter(e.validator),
		e.crypto,
		e.audit,
		e.logger,
		hotstuffConfig,
		e,
	)

	e.handshakeTimeout = e.configMgr.GetDuration("CONSENSUS_HANDSHAKE_TIMEOUT", 5*time.Second)

	e.activationCheckInterval = e.configMgr.GetDuration("CONSENSUS_ACTIVATION_CHECK_INTERVAL", 1*time.Second)
	if e.activationCheckInterval <= 0 {
		e.activationCheckInterval = time.Second
	}
	forceActivation := e.configMgr.GetBool("CONSENSUS_FORCE_ACTIVATION", false)
	forceActivationConfigured := forceActivation
	legacyForce := e.configMgr.GetBool("CONSENSUS_ACTIVATION_FORCE", false)
	maxWait := e.configMgr.GetDuration("CONSENSUS_ACTIVATION_MAX_WAIT", 0)
	if maxWait < 0 {
		maxWait = 0
	}
	if !forceActivation && legacyForce {
		forceActivation = true
	}
	if !forceActivation {
		maxWait = 0
	}
	e.activationForce = forceActivation
	e.activationMaxWait = maxWait
	e.activationActiveWindow = e.configMgr.GetDuration("CONSENSUS_ACTIVATION_ACTIVE_WINDOW", 20*time.Second)
	if e.activationActiveWindow <= 0 {
		e.activationActiveWindow = 20 * time.Second
	}

	if legacyForce && !forceActivationConfigured {
		e.logger.WarnContext(context.Background(), "CONSENSUS_ACTIVATION_FORCE is deprecated; treating as CONSENSUS_FORCE_ACTIVATION",
			"max_wait", maxWait)
		if e.audit != nil {
			_ = e.audit.Warn("consensus_force_activation_override_legacy", map[string]interface{}{
				"max_wait": maxWait.String(),
			})
		}
	}

	if forceActivation {
		e.logger.WarnContext(context.Background(), "CONSENSUS_FORCE_ACTIVATION enabled; consensus may start without genesis certificate",
			"max_wait", maxWait)
		if e.audit != nil {
			_ = e.audit.Warn("consensus_force_activation_override", map[string]interface{}{
				"max_wait": maxWait.String(),
			})
		}
	}

	return nil
}

// Start initializes and starts the consensus engine
func (e *ConsensusEngine) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("consensus engine already running")
	}

	e.logger.InfoContext(ctx, "starting consensus engine",
		"node_id", fmt.Sprintf("%x", e.config.NodeID[:8]),
		"node_type", e.config.NodeType,
		"enable_proposing", e.config.EnableProposing,
		"enable_voting", e.config.EnableVoting,
	)

	validatorCount := 0
	if e.validatorSet != nil {
		validatorCount = e.validatorSet.GetValidatorCount()
	}
	if e.net == nil && validatorCount > 1 {
		e.mu.Unlock()
		return fmt.Errorf("network publisher not configured for multi-node consensus: validators=%d (enable P2P or call SetNetwork)", validatorCount)
	}

	// Reset activation gates
	e.consensusActive = false
	e.handshake.leader = leaderHandshakeState{}
	e.handshake.follower = followerHandshakeState{}
	e.activationSeenGates = false
	if e.heartbeatController != nil {
		e.heartbeatController.Reset()
	}
	e.readyMu.Lock()
	e.readyValidators = make(map[ctypes.ValidatorID]time.Time)
	e.localReady = false
	e.readyMu.Unlock()
	e.mu.Unlock()

	if err := e.storage.Start(ctx); err != nil {
		return fmt.Errorf("failed to start storage: %w", err)
	}
	if err := e.storage.RepairLastCommittedMetadata(ctx); err != nil {
		e.driftLastError.Store(err.Error())
		e.driftLastRepairUnix.Store(time.Now().Unix())
		return fmt.Errorf("failed to repair consensus metadata: %w", err)
	}
	e.driftLastError.Store("")
	e.driftLastRepairUnix.Store(time.Now().Unix())

	if err := e.hotstuff.Start(ctx); err != nil {
		return fmt.Errorf("failed to start HotStuff: %w", err)
	}

	e.syncPacemakerWithHotStuff(ctx, "pre_pacemaker_start")

	if e.genesis == nil {
		e.MarkValidatorReady(e.config.NodeID)
		if err := e.ActivateConsensus(ctx); err != nil {
			return err
		}
	} else {
		e.logger.InfoContext(ctx, "genesis coordinator present; delaying pacemaker activation until certificate is received")
	}

	e.mu.Lock()
	e.running = true
	e.mu.Unlock()

	if e.config.HealthCheckInterval > 0 {
		go e.healthCheckLoop(ctx)
	}
	go e.driftRepairLoop(ctx)

	if e.config.MetricsEnabled {
		go e.metricsLoop(ctx)
	}

	e.audit.Info("consensus_engine_started", map[string]interface{}{
		"node_id":         fmt.Sprintf("%x", e.config.NodeID[:]),
		"validator_count": e.validatorSet.GetValidatorCount(),
		"current_view":    e.pacemaker.GetCurrentView(),
		"current_height":  e.pacemaker.GetCurrentHeight(),
	})

	return nil
}

// Stop gracefully shuts down the consensus engine
func (e *ConsensusEngine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	e.logger.InfoContext(context.Background(), "stopping consensus engine")

	// Signal stop
	close(e.stopCh)

	// Stop components in reverse order
	if err := e.heartbeat.Stop(); err != nil {
		e.logger.ErrorContext(context.Background(), "failed to stop heartbeat", "error", err)
	}
	if e.heartbeatController != nil {
		e.heartbeatController.Reset()
	}

	if err := e.pacemaker.Stop(); err != nil {
		e.logger.ErrorContext(context.Background(), "failed to stop pacemaker", "error", err)
	}

	if err := e.hotstuff.Stop(); err != nil {
		e.logger.ErrorContext(context.Background(), "failed to stop HotStuff", "error", err)
	}

	if err := e.storage.Stop(); err != nil {
		e.logger.ErrorContext(context.Background(), "failed to stop storage", "error", err)
	}

	e.running = false
	e.consensusActive = false
	e.handshake.leader = leaderHandshakeState{}
	e.handshake.follower = followerHandshakeState{}
	e.readyMu.Lock()
	e.readyValidators = make(map[ctypes.ValidatorID]time.Time)
	e.localReady = false
	e.readyMu.Unlock()

	e.audit.Info("consensus_engine_stopped", map[string]interface{}{
		"node_id":          fmt.Sprintf("%x", e.config.NodeID[:]),
		"final_view":       e.pacemaker.GetCurrentView(),
		"final_height":     e.pacemaker.GetCurrentHeight(),
		"blocks_committed": e.metrics.BlocksCommitted,
	})

	return nil
}

// SubmitBlock submits a block for consensus
func (e *ConsensusEngine) SubmitBlock(ctx context.Context, block Block) error {
	e.mu.RLock()
	if !e.running {
		e.mu.RUnlock()
		return fmt.Errorf("consensus engine not running")
	}
	e.mu.RUnlock()

	if !e.IsConsensusActive() {
		return fmt.Errorf("consensus not active")
	}

	// Validate we're the leader
	currentView := e.pacemaker.GetCurrentView()
	isLeader, err := e.rotation.IsLeader(ctx, e.config.NodeID, currentView)
	if err != nil {
		return fmt.Errorf("failed to check leader status: %w", err)
	}

	if !isLeader {
		return fmt.Errorf("not the leader for view %d", currentView)
	}
	if eligible, reason := e.IsLocalLeaderEligible(ctx, currentView); !eligible {
		if reason == "" {
			reason = "leader ineligible"
		}
		e.logger.WarnContext(ctx, "submit block aborted: leader ineligible",
			"view", currentView,
			"reason", reason,
		)
		return fmt.Errorf("leader ineligible to propose in view %d: %s", currentView, reason)
	}

	if !e.config.EnableProposing {
		return fmt.Errorf("proposing disabled for this node")
	}

	h := block.GetHash()
	blockHeight := block.GetHeight()
	pacemakerHeight := e.pacemaker.GetCurrentHeight()
	e.logger.InfoContext(ctx, "[SUBMIT_BLOCK] starting block submission",
		"block_height", blockHeight,
		"pacemaker_height", pacemakerHeight,
		"hash", fmt.Sprintf("%x", h[:8]),
		"view", currentView,
	)

	// Create proposal
	e.logger.InfoContext(ctx, "[SUBMIT_BLOCK] calling HotStuff.CreateProposal")
	proposal, err := e.hotstuff.CreateProposal(ctx, block)
	if err != nil {
		e.logger.ErrorContext(ctx, "[SUBMIT_BLOCK] CreateProposal FAILED",
			"error", err,
			"block_height", blockHeight,
			"pacemaker_height", pacemakerHeight)
		return fmt.Errorf("failed to create proposal: %w", err)
	}

	e.logger.InfoContext(ctx, "[SUBMIT_BLOCK] CreateProposal SUCCESS",
		"proposal_height", proposal.Height,
		"proposal_view", proposal.View)

	e.metrics.incrementProposalsSent()

	// Process our own proposal
	if err := e.hotstuff.OnProposal(ctx, proposal); err != nil {
		return fmt.Errorf("failed to process proposal: %w", err)
	}

	// Broadcast proposal to peers so validators can vote (no-op publisher handles disabled networking)
	if err := e.publishMessage(ctx, proposal); err != nil {
		e.logger.ErrorContext(ctx, "[SUBMIT_BLOCK] proposal broadcast failed",
			"error", err,
			"height", proposal.Height,
			"view", proposal.View,
		)
		return fmt.Errorf("failed to broadcast proposal: %w", err)
	}

	h2 := block.GetHash()
	e.audit.Info("block_submitted", map[string]interface{}{
		"height": block.GetHeight(),
		"hash":   fmt.Sprintf("%x", h2[:]),
		"view":   currentView,
	})

	return nil
}

// OnMessageReceived processes messages from the network
func (e *ConsensusEngine) OnMessageReceived(ctx context.Context, peerAddr string, msgType messages.MessageType, data []byte) error {
	e.logger.InfoContext(ctx, "OnMessageReceived CALLED", "peer", peerAddr, "type", msgType, "bytes", len(data))
	e.mu.RLock()
	running := e.running
	active := e.consensusActive
	e.mu.RUnlock()

	if !running {
		e.logger.WarnContext(ctx, "OnMessageReceived - engine not running!")
		return fmt.Errorf("consensus engine not running")
	}

	// Validate peer address only when it looks like an IP/host. P2P passes libp2p peer IDs here,
	// and transport/IP-level gating is already enforced by the router/conn gater.
	if e.allowlist != nil && (strings.Contains(peerAddr, ".") || strings.Contains(peerAddr, ":")) {
		if err := e.allowlist.ValidateBeforeConnect(ctx, peerAddr); err != nil {
			e.audit.Security("message_from_unauthorized_peer", map[string]interface{}{
				"peer": peerAddr,
			})
			return fmt.Errorf("peer not allowed: %w", err)
		}
	}

	// Decode and verify message
	e.logger.InfoContext(ctx, "Decoding message", "type", msgType)
	msg, err := e.encoder.VerifyAndDecode(ctx, data, msgType)
	if err != nil {
		e.logger.ErrorContext(ctx, "DECODE FAILED", "type", msgType, "error", err)
		e.audit.Warn("message_decode_failed", map[string]interface{}{
			"peer":  peerAddr,
			"type":  msgType,
			"error": err.Error(),
		})
		return fmt.Errorf("failed to decode message: %w", err)
	}
	e.logger.InfoContext(ctx, "Decode SUCCESS, routing to handler", "type", msgType)

	if !active {
		switch msgType {
		case messages.TypeGenesisReady, messages.TypeGenesisCertificate,
			messages.TypeProposalIntent, messages.TypeReadyToVote:
			// allowed during activation
		default:
			e.logger.WarnContext(ctx, "dropping message before consensus activation",
				"type", msgType,
				"peer", peerAddr)
			return nil
		}
	}

	// Route to appropriate handler
	switch msgType {
	case messages.TypeProposal:
		e.metrics.incrementProposalsReceived()
		proposal := msg.(*messages.Proposal)
		e.markValidatorActive(proposal.ProposerID, time.Now())
		if err := e.hotstuff.OnProposal(ctx, proposal); err != nil {
			if e.metrics != nil && errors.Is(err, messages.ErrMissingJustifyQC) {
				e.metrics.incrementProposalRejectMissingJustifyQC()
			}
			e.logger.WarnContext(ctx, "proposal message rejected by HotStuff",
				"peer", peerAddr,
				"view", proposal.View,
				"height", proposal.Height,
				"block_hash", fmt.Sprintf("%x", proposal.BlockHash[:8]),
				"proposer", fmt.Sprintf("%x", proposal.ProposerID[:8]),
				"error", err.Error(),
			)
			return err
		}
		return nil

	case messages.TypeVote:
		e.metrics.incrementVotesReceived()
		vote := msg.(*messages.Vote)
		e.markValidatorActive(vote.VoterID, time.Now())
		if err := e.hotstuff.OnVote(ctx, vote); err != nil {
			e.logger.WarnContext(ctx, "vote message rejected by HotStuff",
				"peer", peerAddr,
				"view", vote.View,
				"height", vote.Height,
				"block_hash", fmt.Sprintf("%x", vote.BlockHash[:8]),
				"voter", fmt.Sprintf("%x", vote.VoterID[:8]),
				"error", err.Error(),
			)
			return err
		}
		return nil

	case messages.TypeViewChange:
		e.metrics.incrementViewChanges()
		vc := msg.(*messages.ViewChange)
		e.markValidatorActive(vc.SenderID, time.Now())
		// Avoid typed-nil QC in interface: only assign when non-nil
		var hqc QC
		if vc.HighestQC != nil {
			hqc = vc.HighestQC
		} else {
			hqc = nil
		}
		lvc := &leader.ViewChangeMsg{
			OldView:   vc.OldView,
			NewView:   vc.NewView,
			HighestQC: hqc,
			SenderID:  vc.SenderID,
			Timestamp: vc.Timestamp,
		}
		if vc.Signature.Bytes != nil {
			lvc.Signature = vc.Signature.Bytes
		}
		return e.pacemaker.OnViewChange(ctx, lvc)

	case messages.TypeNewView:
		e.logger.InfoContext(ctx, "received newview message", "peer", peerAddr)
		nv := msg.(*messages.NewView)
		e.logger.InfoContext(ctx, "newview decoded", "view", nv.View, "leader", nv.LeaderID)
		e.markValidatorActive(nv.LeaderID, nv.Timestamp)

		// Convert ViewChanges
		vcl := make([]*leader.ViewChangeMsg, 0, len(nv.ViewChanges))
		for i := range nv.ViewChanges {
			vc := nv.ViewChanges[i]
			// Avoid typed-nil QC in interface: only assign when non-nil
			var vhqc QC
			if vc.HighestQC != nil {
				vhqc = vc.HighestQC
			} else {
				vhqc = nil
			}
			lvc := &leader.ViewChangeMsg{
				OldView:   vc.OldView,
				NewView:   vc.NewView,
				HighestQC: vhqc,
				SenderID:  vc.SenderID,
				Timestamp: vc.Timestamp,
			}
			if vc.Signature.Bytes != nil {
				lvc.Signature = vc.Signature.Bytes
			}
			vcl = append(vcl, lvc)
		}
		// Avoid typed-nil QC for NewView as well
		var nhqc QC
		if nv.HighestQC != nil {
			nhqc = nv.HighestQC
		} else {
			nhqc = nil
		}
		lnv := &leader.NewViewMsg{
			View:        nv.View,
			ViewChanges: vcl,
			HighestQC:   nhqc,
			LeaderID:    nv.LeaderID,
			Timestamp:   nv.Timestamp,
		}
		if nv.Signature.Bytes != nil {
			lnv.Signature = nv.Signature.Bytes
		}

		e.logger.InfoContext(ctx, "calling pacemaker OnNewView", "view", lnv.View)
		err := e.pacemaker.OnNewView(ctx, lnv)
		if err != nil {
			e.logger.WarnContext(ctx, "pacemaker OnNewView failed", "error", err)
		}
		return err

	case messages.TypeHeartbeat:
		hb := msg.(*messages.Heartbeat)
		e.logger.InfoContext(ctx, "Heartbeat decoded", "view", hb.View, "height", hb.Height, "leader", hb.LeaderID)
		lhb := &leader.HeartbeatMsg{
			View:      hb.View,
			Height:    hb.Height,
			LeaderID:  hb.LeaderID,
			Timestamp: hb.Timestamp,
		}
		if hb.Signature.Bytes != nil {
			lhb.Signature = hb.Signature.Bytes
		}
		e.markValidatorActive(hb.LeaderID, hb.Timestamp)
		err := e.heartbeat.OnHeartbeat(ctx, lhb)
		if err != nil {
			e.logger.ErrorContext(ctx, "Heartbeat.OnHeartbeat FAILED", "error", err)
		} else {
			e.logger.InfoContext(ctx, "Heartbeat processed successfully")
		}
		return err

	case messages.TypeEvidence:
		ev := msg.(*messages.Evidence)
		return e.validator.ValidateEvidence(ctx, ev)

	case messages.TypeGenesisReady:
		if e.genesis != nil {
			ready := msg.(*messages.GenesisReady)
			e.genesis.OnGenesisReady(ctx, ready)
		} else {
			e.logger.WarnContext(ctx, "received genesis ready without coordinator", "peer", peerAddr)
		}
		return nil

	case messages.TypeGenesisCertificate:
		cert := msg.(*messages.GenesisCertificate)
		if e.genesis != nil {
			e.genesis.OnGenesisCertificate(ctx, cert)
		} else {
			e.logger.InfoContext(ctx, "activating consensus from external genesis certificate")
			if err := e.ActivateConsensus(ctx); err != nil {
				e.logger.ErrorContext(ctx, "failed to activate consensus", "error", err)
				return err
			}
		}
		return nil

	case messages.TypeProposalIntent:
		pi := msg.(*messages.ProposalIntent)
		e.markValidatorActive(pi.LeaderID, pi.Timestamp)
		e.handleProposalIntent(ctx, pi)
		return nil

	case messages.TypeReadyToVote:
		rtv := msg.(*messages.ReadyToVote)
		e.markValidatorActive(rtv.ValidatorID, rtv.Timestamp)
		e.recordReadyToVote(rtv)
		return nil

	default:
		return fmt.Errorf("unknown message type: %v", msgType)
	}
}

// RegisterCommitCallback registers a callback for block commits
func (e *ConsensusEngine) RegisterCommitCallback(callback CommitCallback) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.commitCallbacks = append(e.commitCallbacks, callback)
}

// Consensus callback implementations

// OnCommit is called when a block is committed
func (e *ConsensusEngine) OnCommit(block Block, qc QC) error {
	ctx := context.Background()

	h3 := block.GetHash()
	e.logger.InfoContext(ctx, "commit received",
		"height", block.GetHeight(),
		"hash", fmt.Sprintf("%x", h3[:8]),
		"view", qc.GetView(),
	)

	// Execute callbacks
	e.mu.RLock()
	callbacks := make([]CommitCallback, len(e.commitCallbacks))
	copy(callbacks, e.commitCallbacks)
	e.mu.RUnlock()

	var cbErr error
	for _, cb := range callbacks {
		if err := cb(ctx, block, qc); err != nil {
			if cbErr == nil {
				cbErr = err
			}
			e.logger.ErrorContext(ctx, "commit callback failed", "error", err)
			if e.audit != nil {
				e.audit.Security("commit_callback_failed", map[string]interface{}{
					"height": block.GetHeight(),
					"hash":   fmt.Sprintf("%x", h3[:8]),
					"view":   qc.GetView(),
					"error":  err.Error(),
				})
			}
		}
	}
	if cbErr != nil {
		if IsInvalidBlock(cbErr) {
			if e.metrics != nil {
				e.metrics.incrementInvalidBlocks()
			}
			e.syncPacemakerWithHotStuff(ctx, "invalid_block_commit")
			if err := e.TriggerViewChange(ctx); err != nil {
				e.logger.WarnContext(ctx, "failed to trigger view change after invalid block",
					"error", err,
				)
			}
			return cbErr
		}
		go func() { _ = e.Stop() }()
		return cbErr
	}

	e.metrics.incrementBlocksCommitted()
	e.metrics.updateLastCommitTime()

	// Notify heartbeat of proposal
	e.heartbeat.OnProposal(ctx)

	return nil
}

// OnProposal is called when a proposal is created/received
func (e *ConsensusEngine) OnProposal(proposal interface{}) error {
	ctx := context.Background()
	e.heartbeat.OnProposal(ctx)
	if e.net != nil {
		var p *messages.Proposal
		switch v := proposal.(type) {
		case *messages.Proposal:
			p = v
		case messages.Proposal:
			p = &v
		}
		if p != nil {
			if data, err := e.encoder.Encode(p); err == nil {
				pubTopic := e.topicFor(messages.TypeProposal)
				if perr := e.net.Publish(ctx, pubTopic, data); perr != nil {
					e.logger.WarnContext(ctx, "proposal publish failed", "topic", pubTopic, "bytes", len(data), "error", perr)
				} else {
					e.logger.InfoContext(ctx, "proposal published", "topic", pubTopic, "bytes", len(data))
				}
				e.metrics.incrementProposalsSent()
			} else {
				e.logger.ErrorContext(ctx, "proposal encode failed", "error", err)
			}
		}
	}
	return nil
}

// OnVote is called when a vote is cast
func (e *ConsensusEngine) OnVote(v interface{}) error {
	ctx := context.Background()
	if e.net != nil {
		var vote *messages.Vote
		switch x := v.(type) {
		case *messages.Vote:
			vote = x
		case messages.Vote:
			vote = &x
		}
		if vote != nil {
			e.logger.InfoContext(ctx, "publishing vote",
				"view", vote.View,
				"height", vote.Height,
				"block_hash", fmt.Sprintf("%x", vote.BlockHash[:8]),
				"voter", fmt.Sprintf("%x", vote.VoterID[:8]),
			)
			if data, err := e.encoder.Encode(vote); err == nil {
				pubTopic := e.topicFor(messages.TypeVote)
				if perr := e.net.Publish(ctx, pubTopic, data); perr != nil {
					e.logger.WarnContext(ctx, "vote publish failed", "topic", pubTopic, "bytes", len(data), "error", perr)
				} else {
					e.logger.InfoContext(ctx, "vote published", "topic", pubTopic, "bytes", len(data))
				}
			} else {
				e.logger.ErrorContext(ctx, "vote encode failed", "error", err)
			}
		}
	}
	e.metrics.incrementVotesSent()
	return nil
}

// OnQCFormed is called when a QC is formed
func (e *ConsensusEngine) OnQCFormed(qc QC) error {
	e.metrics.incrementQCsFormed()
	// Advance pacemaker to next view when QC formed so all validators stay synchronized
	e.pacemaker.OnQC(context.Background(), qc)
	return nil
}

// Pacemaker callback implementations

// OnViewChange is called when view changes
func (e *ConsensusEngine) OnViewChange(newView uint64, highestQC QC) error {
	e.metrics.incrementViewChanges()
	e.heartbeat.ResetLiveness()

	// Advance HotStuff consensus to the new view
	if err := e.hotstuff.AdvanceView(context.Background(), newView, highestQC); err != nil {
		e.logger.ErrorContext(context.Background(), "failed to advance view in HotStuff",
			"new_view", newView,
			"error", err,
		)
		return fmt.Errorf("failed to advance view: %w", err)
	}

	return nil
}

// OnNewView is called when new view message is received
func (e *ConsensusEngine) OnNewView(newView uint64, newViewMsg interface{}) error {
	e.metrics.incrementViewChanges()
	e.heartbeat.ResetLiveness()

	// Extract highestQC from the NewView message if available
	var highestQC QC
	if nv, ok := newViewMsg.(*leader.NewViewMsg); ok && nv != nil && nv.HighestQC != nil {
		highestQC = nv.HighestQC
	}

	// Advance HotStuff consensus to the new view (same as OnViewChange)
	// This ensures followers update their engine view when receiving NewView from a leader
	if err := e.hotstuff.AdvanceView(context.Background(), newView, highestQC); err != nil {
		e.logger.ErrorContext(context.Background(), "failed to advance view in HotStuff via OnNewView",
			"new_view", newView,
			"error", err,
		)
		return fmt.Errorf("failed to advance view: %w", err)
	}

	e.logger.InfoContext(context.Background(), "view advanced via OnNewView",
		"new_view", newView,
		"has_qc", highestQC != nil,
	)

	return nil
}

// OnTimeout is called when view times out
func (e *ConsensusEngine) OnTimeout(view uint64) error {
	return nil
}

// Heartbeat callback implementations

// OnHeartbeatTimeout is called when heartbeat times out
func (e *ConsensusEngine) OnHeartbeatTimeout(view uint64, lastSeen time.Time) error {
	e.logger.WarnContext(context.Background(), "leader heartbeat timeout",
		"view", view,
		"last_seen", lastSeen,
	)
	if e.rotation != nil {
		if leaderID, err := e.rotation.GetLeaderForView(context.Background(), view); err == nil {
			e.markValidatorInactive(leaderID, time.Now())
		}
	}
	return nil
}

// OnLeaderAlive is called when leader heartbeat received
func (e *ConsensusEngine) OnLeaderAlive(view uint64, leaderID ValidatorID) error {
	e.markValidatorActive(leaderID, time.Now())
	return nil
}

// Status and query methods

// GetCurrentView returns the current consensus view
func (e *ConsensusEngine) GetCurrentView() uint64 {
	return e.pacemaker.GetCurrentView()
}

// GetCurrentHeight returns the current block height
func (e *ConsensusEngine) GetCurrentHeight() uint64 {
	return e.pacemaker.GetCurrentHeight()
}

func (e *ConsensusEngine) GetHighestQC() QC {
	if e == nil || e.pacemaker == nil {
		return nil
	}
	return e.pacemaker.GetHighestQC()
}

func (e *ConsensusEngine) SetBlockValidationFunc(f func(ctx context.Context, block Block) error) {
	if e == nil || e.hotstuff == nil {
		return
	}
	e.hotstuff.SetBlockValidationFunc(f)
}

func (e *ConsensusEngine) TriggerViewChange(ctx context.Context) error {
	e.mu.RLock()
	running := e.running
	active := e.consensusActive
	e.mu.RUnlock()
	if !running {
		return fmt.Errorf("consensus engine not running")
	}
	if !active {
		return fmt.Errorf("consensus not active")
	}
	if e.pacemaker == nil {
		return fmt.Errorf("pacemaker not configured")
	}
	return e.pacemaker.TriggerViewChange(ctx)
}

// GetCurrentLeader returns the current leader
func (e *ConsensusEngine) GetCurrentLeader(ctx context.Context) (ValidatorID, error) {
	return e.rotation.GetLeaderForView(ctx, e.GetCurrentView())
}

// IsLeader checks if this node is the current leader
func (e *ConsensusEngine) IsLeader(ctx context.Context) (bool, error) {
	return e.rotation.IsLeader(ctx, e.config.NodeID, e.GetCurrentView())
}

// IsLocalLeaderEligible reports whether the local validator passes eligibility checks for the given view.
func (e *ConsensusEngine) IsLocalLeaderEligible(ctx context.Context, view uint64) (bool, string) {
	if e.rotation == nil {
		return false, "leader rotation unavailable"
	}
	return e.rotation.IsLeaderEligibleToPropose(ctx, e.config.NodeID, view)
}

// GetStatus returns current consensus status
func (e *ConsensusEngine) GetStatus() EngineStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ctx := context.Background()
	currentLeader, _ := e.GetCurrentLeader(ctx)
	isLeader, _ := e.IsLeader(ctx)

	return EngineStatus{
		Running:       e.running,
		View:          e.GetCurrentView(),
		Height:        e.GetCurrentHeight(),
		NodeID:        e.config.NodeID,
		IsLeader:      isLeader,
		CurrentLeader: currentLeader,
		Metrics:       e.metrics.snapshot(),
	}
}

// GetMetrics returns current metrics
func (e *ConsensusEngine) GetMetrics() MetricsSnapshot {
	return e.metrics.snapshot()
}

func (e *ConsensusEngine) GetPBFTStorageStats() pbft.StorageStats {
	if e == nil || e.storage == nil {
		return pbft.StorageStats{}
	}
	return e.storage.GetStats()
}

// RecordParentHashMismatch increments the metric tracking parent-hash validation mismatches.
func (e *ConsensusEngine) RecordParentHashMismatch() {
	if e == nil || e.metrics == nil {
		return
	}
	e.metrics.incrementParentHashMismatches()
}

// GetGenesisReadyMetrics returns aggregated genesis readiness telemetry counters.
func (e *ConsensusEngine) GetGenesisReadyMetrics() GenesisReadyMetricsSnapshot {
	snapshot := e.genesisReadyMetrics.snapshot()
	snapshot.Discarded = e.genesisDiscarded.Load()
	return snapshot
}

// GetGenesisReadyTrace returns the most recent genesis readiness telemetry records (up to limit; zero for all).
func (e *ConsensusEngine) GetGenesisReadyTrace(limit int) []GenesisReadyTraceRecord {
	return e.genesisReadyTrace.snapshot(limit)
}

// Validator set accessors
// ListValidators returns a snapshot of the current validator set.
func (e *ConsensusEngine) ListValidators() []ValidatorInfo {
	if e.validatorSet == nil {
		return nil
	}
	vals := e.validatorSet.GetValidators()
	out := make([]ValidatorInfo, len(vals))
	for i := range vals {
		out[i] = vals[i]
	}
	return out
}

// ValidatorInfo returns metadata for a specific validator.
func (e *ConsensusEngine) ValidatorInfo(id ValidatorID) (*ValidatorInfo, error) {
	if e.validatorSet == nil {
		return nil, fmt.Errorf("validator set not configured")
	}
	info, err := e.validatorSet.GetValidator(id)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}
	v := *info
	return &v, nil
}

// Health check and metrics

func (e *ConsensusEngine) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(e.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.performHealthCheck(ctx)
		}
	}
}

func (e *ConsensusEngine) driftRepairLoop(ctx context.Context) {
	interval := 10 * time.Second
	if e.config != nil && e.config.HealthCheckInterval > 0 {
		interval = e.config.HealthCheckInterval
		if interval < 2*time.Second {
			interval = 2 * time.Second
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			if err := e.storage.RepairLastCommittedMetadata(ctx); err != nil {
				e.driftLastError.Store(err.Error())
				e.driftLastRepairUnix.Store(time.Now().Unix())
				e.logger.ErrorContext(ctx, "consensus metadata repair failed", "error", err)
				if e.audit != nil {
					_ = e.audit.Security("consensus_metadata_repair_failed", map[string]interface{}{
						"error": err.Error(),
					})
				}
				continue
			}
			e.driftLastError.Store("")
			e.driftLastRepairUnix.Store(time.Now().Unix())
		}
	}
}

func (e *ConsensusEngine) performHealthCheck(ctx context.Context) {
	// Check if components are responsive
	// Log warnings if issues detected

	stats := e.storage.GetStats()
	if stats.ProposalCount > e.configMgr.GetInt("CONSENSUS_MAX_PROPOSALS", 1000) {
		e.logger.WarnContext(ctx, "high proposal count", "count", stats.ProposalCount)
	}
}

func (e *ConsensusEngine) metricsLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.logMetrics(ctx)
		}
	}
}

func (e *ConsensusEngine) logMetrics(ctx context.Context) {
	snapshot := e.metrics.snapshot()

	e.logger.InfoContext(ctx, "consensus metrics",
		"proposals_received", snapshot.ProposalsReceived,
		"proposals_sent", snapshot.ProposalsSent,
		"votes_received", snapshot.VotesReceived,
		"votes_sent", snapshot.VotesSent,
		"qcs_formed", snapshot.QCsFormed,
		"blocks_committed", snapshot.BlocksCommitted,
		"invalid_blocks", snapshot.InvalidBlocks,
		"proposal_reject_missing_justify_qc", snapshot.ProposalRejectMissingJustifyQC,
		"bootstrap_height_qc_mismatch", snapshot.BootstrapHeightQCMismatch,
		"view_changes", snapshot.ViewChanges,
	)
}

// SetNetwork configures the outbound publisher.
func (e *ConsensusEngine) SetNetwork(p NetworkPublisher) { e.net = p }

func (e *ConsensusEngine) syncPacemakerWithHotStuff(ctx context.Context, stage string) {
	if e.pacemaker == nil || e.hotstuff == nil {
		return
	}

	hotstuffHeight := e.hotstuff.GetCurrentHeight()
	hotstuffView := e.hotstuff.GetCurrentView()
	e.pacemaker.SetCurrentHeight(hotstuffHeight)
	e.pacemaker.SetCurrentView(hotstuffView)

	if lockedQC := e.hotstuff.GetLockedQC(); lockedQC != nil {
		e.pacemaker.SetHighestQC(lockedQC)
		if mqc, ok := lockedQC.(*messages.QC); ok {
			e.encoder.CacheQCSignatures(mqc)
			for _, sig := range mqc.Signatures {
				e.markValidatorActive(ctypes.ValidatorID(sig.KeyID), time.Now())
			}
		}
	}

	e.logger.InfoContext(ctx, "pacemaker synchronized with HotStuff",
		"stage", stage,
		"height", hotstuffHeight,
		"view", hotstuffView,
	)
}

// SetGenesisCoordinator wires the genesis ceremony coordinator into the engine.
func (e *ConsensusEngine) SetGenesisCoordinator(coord *genesis.Coordinator) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.genesis = coord
}

// LeaderRotation exposes the deterministic leader rotation strategy (read-only).
func (e *ConsensusEngine) LeaderRotation() ctypes.LeaderRotation {
	return e.rotation
}

func (e *ConsensusEngine) startConsensusComponents(ctx context.Context) error {
	e.syncPacemakerWithHotStuff(ctx, "pre_pacemaker_run")

	if e.configMgr.GetBool("CONSENSUS_REQUIRE_JUSTIFY_QC", true) {
		height := e.pacemaker.GetCurrentHeight()
		highestQC := e.pacemaker.GetHighestQC()
		if height > 1 && highestQC == nil {
			if e.metrics != nil {
				e.metrics.incrementBootstrapHeightQCMismatch()
			}
			e.logger.ErrorContext(ctx, "bootstrap aborted: height set but no highest QC; refusing to start",
				"height", height,
				"view", e.pacemaker.GetCurrentView(),
				"last_committed", e.storage.GetLastCommittedHeight(),
				"hotstuff_height", e.hotstuff.GetCurrentHeight(),
				"hotstuff_view", e.hotstuff.GetCurrentView(),
			)
			return fmt.Errorf("bootstrap aborted: height=%d but no highest QC", height)
		}
	}

	if err := e.pacemaker.Start(ctx); err != nil {
		return fmt.Errorf("failed to start pacemaker: %w", err)
	}

	e.syncPacemakerWithHotStuff(ctx, "post_pacemaker_start")
	if err := e.heartbeat.Start(ctx); err != nil {
		_ = e.pacemaker.Stop()
		return fmt.Errorf("failed to start heartbeat: %w", err)
	}
	return nil
}

// SetPeerObserver wires connectivity metrics from the network layer.
func (e *ConsensusEngine) SetPeerObserver(observer ctypes.PeerObserver) {
	e.mu.Lock()
	e.peerObserver = observer
	e.activationSeenGates = false
	e.mu.Unlock()
}

// MarkValidatorReady records that a validator has completed readiness checks.
func (e *ConsensusEngine) MarkValidatorReady(id ctypes.ValidatorID) {
	if e.validatorSet != nil && !e.validatorSet.IsValidator(id) {
		return
	}
	now := time.Now()
	e.readyMu.Lock()
	_, existed := e.readyValidators[id]
	if !existed {
		e.readyValidators[id] = now
		if id == e.config.NodeID {
			e.localReady = true
		}
	}
	total := len(e.readyValidators)
	e.readyMu.Unlock()
	if !existed {
		e.logger.InfoContext(context.Background(), "validator marked ready",
			"validator", fmt.Sprintf("%x", id[:8]),
			"ready_total", total)
	}
}

// ReadyValidatorCount returns the number of validators marked ready.
func (e *ConsensusEngine) ReadyValidatorCount() int {
	e.readyMu.RLock()
	defer e.readyMu.RUnlock()
	return len(e.readyValidators)
}

// IsValidatorReady reports whether the specified validator has been marked ready.
func (e *ConsensusEngine) IsValidatorReady(id ctypes.ValidatorID) bool {
	e.readyMu.RLock()
	if len(e.readyValidators) == 0 {
		e.readyMu.RUnlock()
		return true
	}
	_, ok := e.readyValidators[id]
	e.readyMu.RUnlock()
	return ok
}

// IsLocalValidatorReady reports whether the local validator has completed readiness.
func (e *ConsensusEngine) IsLocalValidatorReady() bool {
	return e.IsValidatorReady(e.config.NodeID)
}

// GetActivationStatus returns the current activation metrics used for consensus gating.
func (e *ConsensusEngine) GetActivationStatus() ActivationStatus {
	status := ActivationStatus{}
	status.RequiredReady = e.quorumThreshold()
	e.readyMu.RLock()
	status.ReadyValidators = len(e.readyValidators)
	e.readyMu.RUnlock()

	if status.RequiredReady > 0 {
		status.RequiredPeers = status.RequiredReady - 1
		if status.RequiredPeers < 0 {
			status.RequiredPeers = 0
		}
	}

	e.mu.RLock()
	observer := e.peerObserver
	seenQuorum := e.activationSeenGates
	e.mu.RUnlock()

	if seenQuorum {
		status.SeenQuorum = true
	}

	if observer != nil {
		status.HasPeerObserver = true
		window := e.activationActiveWindow
		if window <= 0 {
			window = 20 * time.Second
		}
		status.ConnectedPeers = observer.GetConnectedPeerCount()
		status.ActivePeers = observer.GetActivePeerCount(window)
		status.PeersSeen = observer.GetPeersSeenSinceStartup()
		if status.PeersSeen >= status.RequiredPeers && !seenQuorum {
			e.mu.Lock()
			if !e.activationSeenGates {
				e.activationSeenGates = true
				e.logger.InfoContext(context.Background(), "seen quorum reached, disabling active-peer timing gate",
					"peers_seen", status.PeersSeen,
					"required", status.RequiredPeers)
			}
			e.mu.Unlock()
			seenQuorum = true
		}
	}
	status.SeenQuorum = seenQuorum

	status.GenesisPhase = !e.IsConsensusActive()
	status.HasQuorum = e.activationQuorumSatisfied(status)
	return status
}

// PrivateGetActivationStatus exposes activation metrics to external packages that
// cannot import the full consensus API due to dependency cycles. It intentionally
// returns a copy to preserve encapsulation.
func PrivateGetActivationStatus(engine interface{}) ActivationStatus {
	if ce, ok := engine.(*ConsensusEngine); ok && ce != nil {
		return ce.GetActivationStatus()
	}
	return ActivationStatus{}
}

func (e *ConsensusEngine) GetDriftStatus() DriftStatus {
	status := DriftStatus{LastRepairUnix: e.driftLastRepairUnix.Load()}
	if v := e.driftLastError.Load(); v != nil {
		if s, ok := v.(string); ok {
			status.LastError = s
		}
	}
	return status
}

// PrivateGetDriftStatus exposes drift-repair telemetry to packages that should not depend on
// internal consensus engine types.
func PrivateGetDriftStatus(engine interface{}) DriftStatus {
	if ce, ok := engine.(*ConsensusEngine); ok && ce != nil {
		return ce.GetDriftStatus()
	}
	return DriftStatus{}
}

// HasActivationQuorum reports whether activation requirements are currently satisfied.
func (e *ConsensusEngine) HasActivationQuorum() bool {
	status := e.GetActivationStatus()
	return e.activationQuorumSatisfied(status)
}

func (e *ConsensusEngine) activationQuorumSatisfied(status ActivationStatus) bool {
	if status.RequiredReady <= 1 {
		return status.ReadyValidators >= status.RequiredReady
	}
	if status.ReadyValidators < status.RequiredReady {
		return false
	}
	if status.HasPeerObserver && status.RequiredPeers > 0 {
		if status.ConnectedPeers < status.RequiredPeers {
			return false
		}
		// Two-phase gate: require active peers until we've seen all required peers at least once.
		// After that, trust P2P connectivity checks alone. Avoids blocking on message timing at restart.
		if !status.SeenQuorum {
			if status.PeersSeen < status.RequiredPeers {
				return false
			}
			if status.ActivePeers < status.RequiredPeers {
				return false
			}
		}
	}
	return true
}

func (e *ConsensusEngine) quorumThreshold() int {
	if e.validatorSet == nil {
		return 0
	}
	count := e.validatorSet.GetValidatorCount()
	if count <= 0 {
		return 0
	}
	f := (count - 1) / 3
	threshold := 2*f + 1
	if threshold < 1 {
		threshold = 1
	}
	if threshold > count {
		threshold = count
	}
	return threshold
}

func (e *ConsensusEngine) shouldAwaitActivation() bool {
	if e.validatorSet == nil {
		return false
	}
	return e.validatorSet.GetValidatorCount() > 1
}

func (e *ConsensusEngine) waitForActivationQuorum(ctx context.Context) error {
	if !e.shouldAwaitActivation() {
		return nil
	}

	var deadline time.Time
	if e.activationForce && e.activationMaxWait > 0 {
		deadline = time.Now().Add(e.activationMaxWait)
	}
	interval := e.activationCheckInterval
	if interval <= 0 {
		interval = time.Second
	}

	status := e.GetActivationStatus()
	e.logger.InfoContext(ctx, "waiting for consensus activation quorum",
		"ready", status.ReadyValidators,
		"required_ready", status.RequiredReady,
		"connected_peers", status.ConnectedPeers,
		"required_peers", status.RequiredPeers,
		"force_activation", e.activationForce,
		"max_wait", e.activationMaxWait)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if e.activationQuorumSatisfied(status) {
			e.logger.InfoContext(ctx, "activation quorum satisfied",
				"ready", status.ReadyValidators,
				"required_ready", status.RequiredReady,
				"connected_peers", status.ConnectedPeers,
				"required_peers", status.RequiredPeers,
				"active_peers", status.ActivePeers)
			return nil
		}

		if !deadline.IsZero() && time.Now().After(deadline) {
			err := fmt.Errorf("activation quorum not reached: ready=%d/%d connected_peers=%d/%d active_peers=%d",
				status.ReadyValidators, status.RequiredReady,
				status.ConnectedPeers, status.RequiredPeers,
				status.ActivePeers)
			e.logger.ErrorContext(ctx, "activation quorum wait failed", utils.ZapError(err))
			return err
		}

		select {
		case <-ctx.Done():
			err := ctx.Err()
			e.logger.WarnContext(context.Background(), "activation quorum wait canceled", utils.ZapError(err))
			return err
		case <-ticker.C:
			status = e.GetActivationStatus()
		}
	}
}

// PublishGenesisMessage satisfies genesis.Host by routing genesis messages to the network layer.
func (e *ConsensusEngine) PublishGenesisMessage(ctx context.Context, msg ctypes.Message) error {
	return e.publishMessage(ctx, msg)
}

// RecordGenesisReady captures genesis readiness telemetry for instrumentation.
func (e *ConsensusEngine) RecordGenesisReady(ctx context.Context, event genesis.ReadyEvent, id ctypes.ValidatorID, hash [32]byte, reason string) {
	e.genesisReadyMetrics.record(event)
	e.genesisReadyTrace.record(GenesisReadyTraceRecord{
		Timestamp: time.Now().UTC(),
		Event:     event,
		Validator: id,
		Hash:      hash,
		Reason:    reason,
	})

	if e.logger == nil {
		return
	}

	fields := []interface{}{
		"event", string(event),
		"validator", fmt.Sprintf("%x", id[:4]),
	}
	var zeroHash [32]byte
	if hash != zeroHash {
		fields = append(fields, "hash", fmt.Sprintf("%x", hash[:4]))
	}
	if reason != "" {
		fields = append(fields, "reason", reason)
	}

	e.logger.DebugContext(ctx, "genesis ready event observed", fields...)
}

// RecordGenesisCertificateDiscard increments the discard counter for persisted genesis certificates.
func (e *ConsensusEngine) RecordGenesisCertificateDiscard(ctx context.Context) {
	e.genesisDiscarded.Add(1)
	if e.logger != nil {
		e.logger.WarnContext(ctx, "persisted genesis certificate discarded")
	}
}

// OnGenesisCertificate is invoked when the genesis ceremony completes.
func (e *ConsensusEngine) OnGenesisCertificate(ctx context.Context, cert *messages.GenesisCertificate) error {
	for i := range cert.Attestations {
		e.MarkValidatorReady(cert.Attestations[i].ValidatorID)
	}
	if err := e.ActivateConsensus(ctx); err != nil {
		return err
	}
	e.logger.InfoContext(ctx, "genesis ceremony complete; consensus activated",
		"attestations", len(cert.Attestations),
		"aggregator", fmt.Sprintf("%x", cert.Aggregator[:4]))
	return nil
}

// EnsureProposalQuorum performs the proof-of-quorum handshake before proposing.
// NOTE: The handshake is intentionally scoped to the genesis proposal (view 0 / height 0)
// because subsequent proposals rely on normal HotStuff voting semantics.
func (e *ConsensusEngine) EnsureProposalQuorum(ctx context.Context, view, height uint64) error {
	// Only enforce handshake for the genesis proposal (view 0 / height 0)
	if view != 0 || height != 0 {
		return nil
	}

	required := e.computeRequiredAcks()
	if required == 0 {
		return nil
	}

	leader := &e.handshake.leader
	leader.mu.Lock()
	if leader.completed && leader.view == view && leader.height == height {
		leader.mu.Unlock()
		return nil
	}

	if leader.active && leader.view == view && leader.height == height {
		waitCh := make(chan struct{})
		leader.waiters = append(leader.waiters, waitCh)
		leader.mu.Unlock()
		select {
		case <-waitCh:
			return nil
		case <-time.After(e.handshakeTimeout):
			return fmt.Errorf("proposal quorum handshake timed out")
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	leader.active = true
	leader.completed = false
	leader.view = view
	leader.height = height
	leader.requiredAcks = required
	leader.acks = make(map[ctypes.ValidatorID]struct{}, required)
	nonce := generateNonce()
	leader.nonce = nonce
	waitCh := make(chan struct{})
	leader.waiters = append(leader.waiters, waitCh)
	leader.mu.Unlock()

	if err := e.sendProposalIntent(ctx, view, height, nonce); err != nil {
		leader.mu.Lock()
		leader.active = false
		leader.acks = nil
		leader.waiters = nil
		leader.mu.Unlock()
		return err
	}

	select {
	case <-waitCh:
		return nil
	case <-time.After(e.handshakeTimeout):
		leader.mu.Lock()
		leader.active = false
		leader.acks = nil
		leader.waiters = nil
		leader.mu.Unlock()
		return fmt.Errorf("proposal quorum handshake timed out")
	case <-ctx.Done():
		leader.mu.Lock()
		leader.active = false
		leader.acks = nil
		leader.waiters = nil
		leader.mu.Unlock()
		return ctx.Err()
	}
}

// ActivateConsensus enables pacemaker/heartbeat once readiness gates succeed.
func (e *ConsensusEngine) ActivateConsensus(ctx context.Context) error {
	e.activationMu.Lock()
	defer e.activationMu.Unlock()

	e.mu.RLock()
	alreadyActive := e.consensusActive
	e.mu.RUnlock()
	if alreadyActive {
		return nil
	}

	if err := e.waitForActivationQuorum(ctx); err != nil {
		return err
	}

	if err := e.startConsensusComponents(ctx); err != nil {
		e.logger.ErrorContext(ctx, "consensus component start failed", utils.ZapError(err))
		return err
	}

	e.mu.Lock()
	e.consensusActive = true
	if e.heartbeatController != nil {
		e.heartbeatController.EnableSender()
	}
	e.mu.Unlock()

	e.logger.InfoContext(ctx, "consensus activation complete")

	return nil
}

// IsConsensusActive reports whether pacemaker/heartbeat are running.
func (e *ConsensusEngine) IsConsensusActive() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.consensusActive
}

// IsRunning reports whether the consensus engine main loop is running.
func (e *ConsensusEngine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

func (e *ConsensusEngine) sendProposalIntent(ctx context.Context, view, height uint64, nonce [32]byte) error {
	msg := &messages.ProposalIntent{
		View:      view,
		Height:    height,
		LeaderID:  e.config.NodeID,
		Nonce:     nonce,
		Timestamp: time.Now().UTC(),
	}
	sig, err := e.crypto.SignWithContext(ctx, msg.SignBytes())
	if err != nil {
		return fmt.Errorf("sign proposal intent: %w", err)
	}
	msg.Signature = messages.Signature{
		Bytes:     sig,
		KeyID:     e.config.NodeID,
		Timestamp: msg.Timestamp,
	}
	return e.publishMessage(ctx, msg)
}

func (e *ConsensusEngine) handleProposalIntent(ctx context.Context, pi *messages.ProposalIntent) {
	// Ignore intents not targeting the current leader or outside genesis proposal
	if pi.View != 0 || pi.Height != 0 {
		return
	}
	if pi.LeaderID == e.config.NodeID {
		return // our own broadcast
	}
	if e.rotation == nil {
		return
	}
	isLeader, err := e.rotation.IsLeader(ctx, pi.LeaderID, pi.View)
	if err != nil || !isLeader {
		return
	}

	fState := &e.handshake.follower
	fState.mu.Lock()
	if fState.lastView == pi.View && fState.lastHeight == pi.Height && fState.lastNonce == pi.Nonce {
		fState.mu.Unlock()
		return
	}
	fState.lastView = pi.View
	fState.lastHeight = pi.Height
	fState.lastNonce = pi.Nonce
	fState.mu.Unlock()

	if err := e.sendReadyToVote(ctx, pi); err != nil {
		e.logger.ErrorContext(ctx, "failed to send ready_to_vote", "error", err)
	}
}

func (e *ConsensusEngine) sendReadyToVote(ctx context.Context, pi *messages.ProposalIntent) error {
	msg := &messages.ReadyToVote{
		View:        pi.View,
		Height:      pi.Height,
		ValidatorID: e.config.NodeID,
		LeaderID:    pi.LeaderID,
		Nonce:       pi.Nonce,
		Timestamp:   time.Now().UTC(),
	}
	sig, err := e.crypto.SignWithContext(ctx, msg.SignBytes())
	if err != nil {
		return fmt.Errorf("sign ready_to_vote: %w", err)
	}
	msg.Signature = messages.Signature{
		Bytes:     sig,
		KeyID:     e.config.NodeID,
		Timestamp: msg.Timestamp,
	}
	return e.publishMessage(ctx, msg)
}

func (e *ConsensusEngine) recordReadyToVote(rtv *messages.ReadyToVote) {
	leader := &e.handshake.leader
	leader.mu.Lock()
	if !leader.active || rtv.View != leader.view || rtv.Height != leader.height || rtv.Nonce != leader.nonce {
		leader.mu.Unlock()
		return
	}
	if _, exists := leader.acks[rtv.ValidatorID]; exists {
		leader.mu.Unlock()
		return
	}
	leader.acks[rtv.ValidatorID] = struct{}{}
	if len(leader.acks) >= leader.requiredAcks {
		leader.completed = true
		leader.active = false
		leader.acks = nil
		waiters := leader.waiters
		leader.waiters = nil
		leader.mu.Unlock()
		for _, ch := range waiters {
			close(ch)
		}
		return
	}
	leader.mu.Unlock()
}

func generateNonce() [32]byte {
	var out [32]byte
	if _, err := rand.Read(out[:]); err != nil {
		// Extremely unlikely; fall back to timestamp-based nonce
		unix := time.Now().UnixNano()
		for i := 0; i < 32; i += 8 {
			val := uint64(unix) + uint64(i)
			out[i+0] = byte(val >> 56)
			out[i+1] = byte(val >> 48)
			out[i+2] = byte(val >> 40)
			out[i+3] = byte(val >> 32)
			out[i+4] = byte(val >> 24)
			out[i+5] = byte(val >> 16)
			out[i+6] = byte(val >> 8)
			out[i+7] = byte(val)
		}
	}
	return out
}

func (e *ConsensusEngine) publishMessage(ctx context.Context, msg ctypes.Message) error {
	if e.net == nil {
		return fmt.Errorf("network publisher not configured")
	}
	data, err := e.encoder.Encode(msg)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	topic := e.topicFor(messages.MessageType(msg.Type()))
	if err := e.net.Publish(ctx, topic, data); err != nil {
		return fmt.Errorf("publish to topic %s: %w", topic, err)
	}
	return nil
}

func (e *ConsensusEngine) markValidatorActive(id ctypes.ValidatorID, when time.Time) {
	if e.validatorSet == nil {
		return
	}
	var zero ctypes.ValidatorID
	if id == zero {
		return
	}
	if tracker, ok := e.validatorSet.(interface {
		MarkActive(ctypes.ValidatorID, time.Time)
	}); ok {
		tracker.MarkActive(id, when)
	}
}

func (e *ConsensusEngine) markValidatorInactive(id ctypes.ValidatorID, when time.Time) {
	if e.validatorSet == nil {
		return
	}
	var zero ctypes.ValidatorID
	if id == zero {
		return
	}
	if tracker, ok := e.validatorSet.(interface {
		MarkInactive(ctypes.ValidatorID, time.Time)
	}); ok {
		tracker.MarkInactive(id, when)
	}
}

func (e *ConsensusEngine) topicFor(t messages.MessageType) string {
	if e.config != nil && e.config.Topics != nil {
		if s, ok := e.config.Topics[t]; ok && s != "" {
			// Ignore flat "consensus" override to prevent collapsing all subtopics
			if strings.EqualFold(strings.TrimSpace(s), "consensus") {
				// fall through to defaults below
			} else {
				return s
			}
		}
	}
	switch t {
	case messages.TypeProposal:
		return "consensus/proposal"
	case messages.TypeVote:
		return "consensus/vote"
	case messages.TypeViewChange:
		return "consensus/viewchange"
	case messages.TypeNewView:
		return "consensus/newview"
	case messages.TypeHeartbeat:
		return "consensus/heartbeat"
	case messages.TypeEvidence:
		return "consensus/evidence"
	case messages.TypeGenesisReady:
		return "consensus/genesis/ready"
	case messages.TypeGenesisCertificate:
		return "consensus/genesis/certificate"
	case messages.TypeProposalIntent:
		return "consensus/proposal_intent"
	case messages.TypeReadyToVote:
		return "consensus/ready_to_vote"
	default:
		return "consensus/unknown"
	}
}

// TopicFor exposes the configured publish topic for the provided message type.
func (e *ConsensusEngine) TopicFor(t messages.MessageType) string {
	return e.topicFor(t)
}

// Metrics methods

func (m *EngineMetrics) incrementProposalsReceived() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ProposalsReceived++
}

func (m *EngineMetrics) incrementProposalsSent() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ProposalsSent++
}

func (m *EngineMetrics) incrementVotesReceived() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.VotesReceived++
}

func (m *EngineMetrics) incrementVotesSent() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.VotesSent++
}

func (m *EngineMetrics) incrementQCsFormed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.QCsFormed++
}

func (m *EngineMetrics) incrementBlocksCommitted() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BlocksCommitted++
}

func (m *EngineMetrics) incrementInvalidBlocks() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.InvalidBlocks++
}

func (m *EngineMetrics) incrementProposalRejectMissingJustifyQC() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ProposalRejectMissingJustifyQC++
}

func (m *EngineMetrics) incrementBootstrapHeightQCMismatch() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BootstrapHeightQCMismatch++
}

func (m *EngineMetrics) incrementViewChanges() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ViewChanges++
}

func (m *EngineMetrics) incrementParentHashMismatches() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ParentHashMismatches++
}

func (m *EngineMetrics) updateLastCommitTime() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastCommitTime = time.Now()
}

func (m *EngineMetrics) snapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return MetricsSnapshot{
		ProposalsReceived:              m.ProposalsReceived,
		ProposalsSent:                  m.ProposalsSent,
		VotesReceived:                  m.VotesReceived,
		VotesSent:                      m.VotesSent,
		QCsFormed:                      m.QCsFormed,
		BlocksCommitted:                m.BlocksCommitted,
		InvalidBlocks:                  m.InvalidBlocks,
		ProposalRejectMissingJustifyQC: m.ProposalRejectMissingJustifyQC,
		BootstrapHeightQCMismatch:      m.BootstrapHeightQCMismatch,
		ViewChanges:                    m.ViewChanges,
		EquivocationsDetected:          m.EquivocationsDetected,
		ParentHashMismatches:           m.ParentHashMismatches,
		LastCommitTime:                 m.LastCommitTime,
	}
}

// Types

// EngineStatus contains current engine status
type EngineStatus struct {
	Running       bool
	View          uint64
	Height        uint64
	NodeID        ValidatorID
	IsLeader      bool
	CurrentLeader ValidatorID
	Metrics       MetricsSnapshot
}

// MetricsSnapshot is a point-in-time metrics view
type MetricsSnapshot struct {
	ProposalsReceived              uint64
	ProposalsSent                  uint64
	VotesReceived                  uint64
	VotesSent                      uint64
	QCsFormed                      uint64
	BlocksCommitted                uint64
	InvalidBlocks                  uint64
	ProposalRejectMissingJustifyQC uint64
	BootstrapHeightQCMismatch      uint64
	ViewChanges                    uint64
	EquivocationsDetected          uint64
	ParentHashMismatches           uint64
	LastCommitTime                 time.Time
}

// Message types (simplified - should import from messages package)
// All message type definitions are sourced from the messages package.
