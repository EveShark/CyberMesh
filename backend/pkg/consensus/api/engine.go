package api

import (
    "context"
    "fmt"
    "sync"
    "time"
    "strings"

    "backend/pkg/consensus/leader"
    "backend/pkg/consensus/messages"
    "backend/pkg/consensus/pbft"
)

// Re-export types for convenience

// ConsensusEngine orchestrates all consensus components
type ConsensusEngine struct {
	// Core components
	encoder   *messages.Encoder
	validator *messages.Validator
	rotation  *leader.Rotation
	pacemaker *leader.Pacemaker
	heartbeat *leader.HeartbeatManager
	quorum    *pbft.QuorumVerifier
	storage   *pbft.Storage
	hotstuff  *pbft.HotStuff

	// Infrastructure
	crypto    CryptoService
	audit     AuditLogger
	logger    Logger
	configMgr ConfigManager
	allowlist IPAllowlist

	// Configuration
	config       *EngineConfig
	validatorSet ValidatorSet

	// State
	running         bool
	commitCallbacks []CommitCallback

	// Metrics
	metrics *EngineMetrics

	mu     sync.RWMutex
	stopCh chan struct{}

	// Network publisher (optional)
	net NetworkPublisher
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
	ProposalsReceived     uint64
	ProposalsSent         uint64
	VotesReceived         uint64
	VotesSent             uint64
	QCsFormed             uint64
	BlocksCommitted       uint64
	ViewChanges           uint64
	EquivocationsDetected uint64
	LastCommitTime        time.Time
	AverageCommitLatency  time.Duration

	mu sync.RWMutex
}

// CommitCallback is called when a block is committed
type CommitCallback func(ctx context.Context, block Block, qc QC) error

// NetworkPublisher publishes consensus messages onto the network layer.
type NetworkPublisher interface {
	Publish(ctx context.Context, topic string, data []byte) error
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
) (*ConsensusEngine, error) {
	if config == nil {
		config = DefaultEngineConfig()
	}

	if audit == nil {
		audit = noopAuditLogger{}
	}

	engine := &ConsensusEngine{
		crypto:          crypto,
		audit:           audit,
		logger:          logger,
		configMgr:       configMgr,
		allowlist:       allowlist,
		validatorSet:    validatorSet,
		config:          config,
		commitCallbacks: make([]CommitCallback, 0),
		metrics:         &EngineMetrics{},
		stopCh:          make(chan struct{}),
	}

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
		MaxProposalSize:      e.configMgr.GetInt("CONSENSUS_MAX_PROPOSAL_SIZE", 1<<20),
		MaxVoteSize:          e.configMgr.GetInt("CONSENSUS_MAX_VOTE_SIZE", 10<<10),
		MaxQCSize:            e.configMgr.GetInt("CONSENSUS_MAX_QC_SIZE", 100<<10),
		MaxViewChangeSize:    e.configMgr.GetInt("CONSENSUS_MAX_VIEWCHANGE_SIZE", 50<<10),
		MaxNewViewSize:       e.configMgr.GetInt("CONSENSUS_MAX_NEWVIEW_SIZE", 500<<10),
		MaxHeartbeatSize:     e.configMgr.GetInt("CONSENSUS_MAX_HEARTBEAT_SIZE", 5<<10),
		MaxEvidenceSize:      e.configMgr.GetInt("CONSENSUS_MAX_EVIDENCE_SIZE", 200<<10),
		ClockSkewTolerance:   e.configMgr.GetDuration("CONSENSUS_CLOCK_SKEW_TOLERANCE", 5*time.Second),
		VerifyCacheSize:      e.configMgr.GetInt("CONSENSUS_VERIFY_CACHE_SIZE", 10000),
		VerifyCacheTTL:       e.configMgr.GetDuration("CONSENSUS_VERIFY_CACHE_TTL", 5*time.Minute),
		RejectFutureMessages: e.configMgr.GetBool("CONSENSUS_REJECT_FUTURE_MESSAGES", true),
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
	e.storage = pbft.NewStorage(nil, e.audit, e.logger, storageConfig)

	// 4. Initialize message validator (requires consensus state and validator-set adapter)
	validationConfig := &messages.ValidationConfig{
		MaxViewJump:         uint64(e.configMgr.GetInt("CONSENSUS_MAX_VIEW_JUMP", 100)),
		MaxHeightJump:       uint64(e.configMgr.GetInt("CONSENSUS_MAX_HEIGHT_JUMP", 10)),
		RequireJustifyQC:    e.configMgr.GetBool("CONSENSUS_REQUIRE_JUSTIFY_QC", true),
		StrictMonotonicity:  e.configMgr.GetBool("CONSENSUS_STRICT_MONOTONICITY", true),
		MinQuorumSize:       e.configMgr.GetInt("CONSENSUS_MIN_QUORUM_SIZE", 1),
		MaxQuorumSize:       e.configMgr.GetInt("CONSENSUS_MAX_QUORUM_SIZE", 10000),
		AllowSelfVoting:     e.configMgr.GetBool("CONSENSUS_ALLOW_SELF_VOTING", true), // MUST be true for HotStuff
		MaxViewChangeProofs: e.configMgr.GetInt("CONSENSUS_MAX_VIEWCHANGE_PROOFS", 1000),
		MaxEvidenceAge:      e.configMgr.GetDuration("CONSENSUS_MAX_EVIDENCE_AGE", 24*time.Hour),
	}
	vsAdapter := newValidatorSetAdapter(e.validatorSet, e.rotation)
	state := newEngineState(e)
	e.validator = messages.NewValidator(vsAdapter, state, e.encoder, validationConfig)

	// 5. Initialize quorum verifier
	quorumConfig := &pbft.QuorumConfig{
		EnableCache:         e.configMgr.GetBool("CONSENSUS_ENABLE_QC_CACHE", true),
		CacheSize:           e.configMgr.GetInt("CONSENSUS_QC_CACHE_SIZE", 10000),
		CacheTTL:            int64(e.configMgr.GetInt("CONSENSUS_QC_CACHE_TTL", 300)),
		StrictValidation:    e.configMgr.GetBool("CONSENSUS_STRICT_VALIDATION", true),
		RequireUniqueVoters: e.configMgr.GetBool("CONSENSUS_REQUIRE_UNIQUE_VOTERS", true),
		AllowSelfVoting:     e.configMgr.GetBool("CONSENSUS_ALLOW_SELF_VOTING", true), // MUST be true for HotStuff
	}

	e.quorum = pbft.NewQuorumVerifier(e.validatorSet, newEncoderAdapter(e.encoder), e.audit, e.logger, quorumConfig)

	// 6. Initialize pacemaker
	pacemakerConfig := &leader.PacemakerConfig{
		BaseTimeout:           e.configMgr.GetDuration("CONSENSUS_BASE_TIMEOUT", 2*time.Second),
		MaxTimeout:            e.configMgr.GetDuration("CONSENSUS_MAX_TIMEOUT", 60*time.Second),
		MinTimeout:            e.configMgr.GetDuration("CONSENSUS_MIN_TIMEOUT", 1*time.Second),
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

	e.heartbeat = leader.NewHeartbeatManager(e.rotation, e.pacemaker, e.crypto, e.audit, e.logger, heartbeatConfig, e, newHeartbeatPublisherAdapter(e))

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

	return nil
}

// Start initializes and starts the consensus engine
func (e *ConsensusEngine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("consensus engine already running")
	}

	e.logger.InfoContext(ctx, "starting consensus engine",
		"node_id", fmt.Sprintf("%x", e.config.NodeID[:8]),
		"node_type", e.config.NodeType,
		"enable_proposing", e.config.EnableProposing,
		"enable_voting", e.config.EnableVoting,
	)

	// Start storage
	if err := e.storage.Start(ctx); err != nil {
		return fmt.Errorf("failed to start storage: %w", err)
	}

	// Start HotStuff
	if err := e.hotstuff.Start(ctx); err != nil {
		return fmt.Errorf("failed to start HotStuff: %w", err)
	}

	// Start pacemaker
	if err := e.pacemaker.Start(ctx); err != nil {
		return fmt.Errorf("failed to start pacemaker: %w", err)
	}

	// Start heartbeat manager
	if err := e.heartbeat.Start(ctx); err != nil {
		return fmt.Errorf("failed to start heartbeat: %w", err)
	}

	e.running = true

	// Start health check
	if e.config.HealthCheckInterval > 0 {
		go e.healthCheckLoop(ctx)
	}

	// Start metrics collection
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

	// Validate we're the leader
	currentView := e.pacemaker.GetCurrentView()
	isLeader, err := e.rotation.IsLeader(ctx, e.config.NodeID, currentView)
	if err != nil {
		return fmt.Errorf("failed to check leader status: %w", err)
	}

	if !isLeader {
		return fmt.Errorf("not the leader for view %d", currentView)
	}

	if !e.config.EnableProposing {
		return fmt.Errorf("proposing disabled for this node")
	}

	h := block.GetHash()
	e.logger.InfoContext(ctx, "submitting block for consensus",
		"height", block.GetHeight(),
		"hash", fmt.Sprintf("%x", h[:8]),
		"view", currentView,
	)

	// Create proposal
	proposal, err := e.hotstuff.CreateProposal(ctx, block)
	if err != nil {
		return fmt.Errorf("failed to create proposal: %w", err)
	}

	e.metrics.incrementProposalsSent()

	// Process our own proposal
	if err := e.hotstuff.OnProposal(ctx, proposal); err != nil {
		return fmt.Errorf("failed to process proposal: %w", err)
	}

	// Broadcast would happen here via network layer
	// For now, just audit the event
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
	if !e.running {
		e.mu.RUnlock()
		e.logger.WarnContext(ctx, "OnMessageReceived - engine not running!")
		return fmt.Errorf("consensus engine not running")
	}
	e.mu.RUnlock()

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

	// Route to appropriate handler
	switch msgType {
	case messages.TypeProposal:
		e.metrics.incrementProposalsReceived()
		proposal := msg.(*messages.Proposal)
		return e.hotstuff.OnProposal(ctx, proposal)

	case messages.TypeVote:
		e.metrics.incrementVotesReceived()
		vote := msg.(*messages.Vote)
		return e.hotstuff.OnVote(ctx, vote)

	case messages.TypeViewChange:
		e.metrics.incrementViewChanges()
		vc := msg.(*messages.ViewChange)
		lvc := &leader.ViewChangeMsg{
			OldView:   vc.OldView,
			NewView:   vc.NewView,
			HighestQC: vc.HighestQC,
			SenderID:  vc.SenderID,
			Timestamp: vc.Timestamp,
		}
		if vc.Signature.Bytes != nil {
			lvc.Signature = vc.Signature.Bytes
		}
		return e.pacemaker.OnViewChange(ctx, lvc)

	case messages.TypeNewView:
		nv := msg.(*messages.NewView)
		// Convert ViewChanges
		vcl := make([]*leader.ViewChangeMsg, 0, len(nv.ViewChanges))
		for i := range nv.ViewChanges {
			vc := nv.ViewChanges[i]
			lvc := &leader.ViewChangeMsg{
				OldView:   vc.OldView,
				NewView:   vc.NewView,
				HighestQC: vc.HighestQC,
				SenderID:  vc.SenderID,
				Timestamp: vc.Timestamp,
			}
			if vc.Signature.Bytes != nil {
				lvc.Signature = vc.Signature.Bytes
			}
			vcl = append(vcl, lvc)
		}
		lnv := &leader.NewViewMsg{
			View:        nv.View,
			ViewChanges: vcl,
			HighestQC:   nv.HighestQC,
			LeaderID:    nv.LeaderID,
			Timestamp:   nv.Timestamp,
		}
		if nv.Signature.Bytes != nil {
			lnv.Signature = nv.Signature.Bytes
		}
		return e.pacemaker.OnNewView(ctx, lnv)

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
		err := e.heartbeat.OnHeartbeat(ctx, lhb)
		if err != nil {
			e.logger.ErrorContext(ctx, "Heartbeat.OnHeartbeat FAILED", "error", err)
		} else {
			e.logger.InfoContext(ctx, "Heartbeat processed successfully")
		}
		return err

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
	e.logger.InfoContext(ctx, "block committed",
		"height", block.GetHeight(),
		"hash", fmt.Sprintf("%x", h3[:8]),
		"view", qc.GetView(),
	)

	e.metrics.incrementBlocksCommitted()
	e.metrics.updateLastCommitTime()

	// Notify heartbeat of proposal
	e.heartbeat.OnProposal(ctx)

	// Execute callbacks
	e.mu.RLock()
	callbacks := make([]CommitCallback, len(e.commitCallbacks))
	copy(callbacks, e.commitCallbacks)
	e.mu.RUnlock()

	for _, cb := range callbacks {
		if err := cb(ctx, block, qc); err != nil {
			e.logger.ErrorContext(ctx, "commit callback failed", "error", err)
		}
	}

	return nil
}

// OnProposal is called when a proposal is created/received
func (e *ConsensusEngine) OnProposal(proposal interface{}) error {
	ctx := context.Background()
	e.heartbeat.OnProposal(ctx)
	if e.net != nil {
		if p, ok := proposal.(*messages.Proposal); ok {
			if data, err := e.encoder.Encode(p); err == nil {
				_ = e.net.Publish(ctx, e.topicFor(messages.TypeProposal), data)
				e.metrics.incrementProposalsSent()
			}
		}
	}
	return nil
}

// OnVote is called when a vote is cast
func (e *ConsensusEngine) OnVote(v interface{}) error {
	ctx := context.Background()
	if e.net != nil {
		if vote, ok := v.(*messages.Vote); ok {
			if data, err := e.encoder.Encode(vote); err == nil {
				_ = e.net.Publish(ctx, e.topicFor(messages.TypeVote), data)
			}
		}
	}
	e.metrics.incrementVotesSent()
	return nil
}

// OnQCFormed is called when a QC is formed
func (e *ConsensusEngine) OnQCFormed(qc QC) error {
	e.metrics.incrementQCsFormed()
	return nil
}

// Pacemaker callback implementations

// OnViewChange is called when view changes
func (e *ConsensusEngine) OnViewChange(newView uint64, highestQC QC) error {
	e.metrics.incrementViewChanges()
	e.heartbeat.ResetLiveness()
	return nil
}

// OnNewView is called when new view message is received
func (e *ConsensusEngine) OnNewView(newView uint64, newViewMsg interface{}) error {
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
	return nil
}

// OnLeaderAlive is called when leader heartbeat received
func (e *ConsensusEngine) OnLeaderAlive(view uint64, leaderID ValidatorID) error {
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

// GetCurrentLeader returns the current leader
func (e *ConsensusEngine) GetCurrentLeader(ctx context.Context) (ValidatorID, error) {
	return e.rotation.GetLeaderForView(ctx, e.GetCurrentView())
}

// IsLeader checks if this node is the current leader
func (e *ConsensusEngine) IsLeader(ctx context.Context) (bool, error) {
	return e.rotation.IsLeader(ctx, e.config.NodeID, e.GetCurrentView())
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
		"view_changes", snapshot.ViewChanges,
	)
}

// SetNetwork configures the outbound publisher.
func (e *ConsensusEngine) SetNetwork(p NetworkPublisher) { e.net = p }

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

func (m *EngineMetrics) incrementViewChanges() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ViewChanges++
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
		ProposalsReceived:     m.ProposalsReceived,
		ProposalsSent:         m.ProposalsSent,
		VotesReceived:         m.VotesReceived,
		VotesSent:             m.VotesSent,
		QCsFormed:             m.QCsFormed,
		BlocksCommitted:       m.BlocksCommitted,
		ViewChanges:           m.ViewChanges,
		EquivocationsDetected: m.EquivocationsDetected,
		LastCommitTime:        m.LastCommitTime,
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
	ProposalsReceived     uint64
	ProposalsSent         uint64
	VotesReceived         uint64
	VotesSent             uint64
	QCsFormed             uint64
	BlocksCommitted       uint64
	ViewChanges           uint64
	EquivocationsDetected uint64
	LastCommitTime        time.Time
}

// Message types (simplified - should import from messages package)
// All message type definitions are sourced from the messages package.
