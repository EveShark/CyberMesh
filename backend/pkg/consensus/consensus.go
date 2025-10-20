package consensus

import (
	"context"
	"fmt"
	"time"

	"backend/pkg/consensus/api"
	"backend/pkg/consensus/leader"
	"backend/pkg/consensus/messages"
	"backend/pkg/consensus/pbft"
	"backend/pkg/consensus/types"
)

// Re-export key types for external use
type (
	// Core types
	ValidatorID   = types.ValidatorID
	ValidatorInfo = types.ValidatorInfo
	BlockHash     = types.BlockHash
	Signature     = types.Signature
	Block         = types.Block
	QC            = types.QC
	MessageType   = types.MessageType

	// Interfaces
	CryptoService  = types.CryptoService
	AuditLogger    = types.AuditLogger
	Logger         = types.Logger
	ConfigManager  = types.ConfigManager
	IPAllowlist    = types.IPAllowlist
	ValidatorSet   = types.ValidatorSet
	StorageBackend = types.StorageBackend

	// Engine
	ConsensusEngine = api.ConsensusEngine
	EngineConfig    = api.EngineConfig
	EngineStatus    = api.EngineStatus

	// Messages
	Proposal   = messages.Proposal
	Vote       = messages.Vote
	ViewChange = messages.ViewChange
	NewView    = messages.NewView
	Heartbeat  = messages.Heartbeat
	Evidence   = messages.Evidence
)

// Re-export message types
const (
	MessageTypeProposal   = types.MessageTypeProposal
	MessageTypeVote       = types.MessageTypeVote
	MessageTypeQC         = types.MessageTypeQC
	MessageTypeViewChange = types.MessageTypeViewChange
	MessageTypeNewView    = types.MessageTypeNewView
	MessageTypeHeartbeat  = types.MessageTypeHeartbeat
	MessageTypeEvidence   = types.MessageTypeEvidence
)

// Config contains all configuration for consensus
type Config struct {
	// Node configuration
	NodeID          ValidatorID
	NodeType        string
	EnableProposing bool
	EnableVoting    bool

	// Infrastructure
	CryptoService  CryptoService
	AuditLogger    AuditLogger
	Logger         Logger
	ConfigManager  ConfigManager
	IPAllowlist    IPAllowlist
	ValidatorSet   ValidatorSet
	StorageBackend StorageBackend
	Persistence    types.StorageBackend

	// Optional overrides (if nil, loaded from ConfigManager)
	EncoderConfig    *messages.EncoderConfig
	ValidationConfig *messages.ValidationConfig
	RotationConfig   *leader.RotationConfig
	PacemakerConfig  *leader.PacemakerConfig
	HeartbeatConfig  *leader.HeartbeatConfig
	QuorumConfig     *pbft.QuorumConfig
	StorageConfig    *pbft.StorageConfig
	HotStuffConfig   *pbft.HotStuffConfig
}

// Consensus provides the public API for the consensus layer
type Consensus struct {
	engine *api.ConsensusEngine
	config *Config
}

// NewConsensus creates a fully initialized consensus system
func NewConsensus(ctx context.Context, cfg *Config) (*Consensus, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	// Validate required fields
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	cfg.Logger.InfoContext(ctx, "initializing consensus system",
		"node_id", fmt.Sprintf("%x", cfg.NodeID[:8]),
		"node_type", cfg.NodeType,
	)

	// Create engine config
	engineConfig := &api.EngineConfig{
		NodeID:              cfg.NodeID,
		NodeType:            cfg.NodeType,
		EnableProposing:     cfg.EnableProposing,
		EnableVoting:        cfg.EnableVoting,
		BlockTimeout:        cfg.ConfigManager.GetDuration("CONSENSUS_BLOCK_TIMEOUT", 5*time.Second),
		MetricsEnabled:      cfg.ConfigManager.GetBool("CONSENSUS_METRICS_ENABLED", true),
		HealthCheckInterval: cfg.ConfigManager.GetDuration("CONSENSUS_HEALTH_CHECK_INTERVAL", 10*time.Second),
	}

	// Create consensus engine
	engine, err := api.NewConsensusEngine(
		cfg.CryptoService,
		cfg.AuditLogger,
		cfg.Logger,
		cfg.ConfigManager,
		cfg.IPAllowlist,
		cfg.ValidatorSet,
		engineConfig,
		cfg.Persistence,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create consensus engine: %w", err)
	}

	consensus := &Consensus{
		engine: engine,
		config: cfg,
	}

	cfg.AuditLogger.Info("consensus_initialized", map[string]interface{}{
		"node_id":         fmt.Sprintf("%x", cfg.NodeID[:]),
		"validator_count": cfg.ValidatorSet.GetValidatorCount(),
		"node_type":       cfg.NodeType,
	})

	return consensus, nil
}

// Start begins consensus operations
func (c *Consensus) Start(ctx context.Context) error {
	c.config.Logger.InfoContext(ctx, "starting consensus system")

	if err := c.engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start consensus engine: %w", err)
	}

	c.config.AuditLogger.Info("consensus_started", map[string]interface{}{
		"node_id": fmt.Sprintf("%x", c.config.NodeID[:]),
	})

	return nil
}

// Stop gracefully shuts down consensus
func (c *Consensus) Stop(ctx context.Context) error {
	c.config.Logger.InfoContext(ctx, "stopping consensus system")

	if err := c.engine.Stop(); err != nil {
		return fmt.Errorf("failed to stop consensus engine: %w", err)
	}

	c.config.AuditLogger.Info("consensus_stopped", map[string]interface{}{
		"node_id": fmt.Sprintf("%x", c.config.NodeID[:]),
	})

	return nil
}

// SubmitBlock submits a block for consensus (leader only)
func (c *Consensus) SubmitBlock(ctx context.Context, block Block) error {
	return c.engine.SubmitBlock(ctx, block)
}

// OnMessageReceived processes consensus messages from the network
func (c *Consensus) OnMessageReceived(ctx context.Context, peerAddr string, msgType MessageType, data []byte) error {
	return c.engine.OnMessageReceived(ctx, peerAddr, msgType, data)
}

// RegisterCommitCallback registers a callback for block commits
func (c *Consensus) RegisterCommitCallback(callback func(ctx context.Context, block Block, qc QC) error) {
	c.engine.RegisterCommitCallback(callback)
}

// GetStatus returns current consensus status
func (c *Consensus) GetStatus() EngineStatus {
	return c.engine.GetStatus()
}

// GetCurrentView returns the current consensus view
func (c *Consensus) GetCurrentView() uint64 {
	return c.engine.GetCurrentView()
}

// GetCurrentHeight returns the current block height
func (c *Consensus) GetCurrentHeight() uint64 {
	return c.engine.GetCurrentHeight()
}

// GetCurrentLeader returns the current leader validator ID
func (c *Consensus) GetCurrentLeader(ctx context.Context) (ValidatorID, error) {
	return c.engine.GetCurrentLeader(ctx)
}

// IsLeader checks if this node is the current leader
func (c *Consensus) IsLeader(ctx context.Context) (bool, error) {
	return c.engine.IsLeader(ctx)
}

// GetMetrics returns current consensus metrics
func (c *Consensus) GetMetrics() interface{} {
	return c.engine.GetMetrics()
}

// validateConfig validates the consensus configuration
func validateConfig(cfg *Config) error {
	if cfg.CryptoService == nil {
		return fmt.Errorf("CryptoService is required")
	}
	if cfg.AuditLogger == nil {
		return fmt.Errorf("AuditLogger is required")
	}
	if cfg.Logger == nil {
		return fmt.Errorf("Logger is required")
	}
	if cfg.ConfigManager == nil {
		return fmt.Errorf("ConfigManager is required")
	}
	if cfg.IPAllowlist == nil {
		return fmt.Errorf("IPAllowlist is required")
	}
	if cfg.ValidatorSet == nil {
		return fmt.Errorf("ValidatorSet is required")
	}

	// Validate node ID is not zero
	zeroID := ValidatorID{}
	if cfg.NodeID == zeroID {
		return fmt.Errorf("NodeID cannot be zero")
	}

	// Validate validator set has enough validators
	validatorCount := cfg.ValidatorSet.GetValidatorCount()
	if validatorCount < 4 {
		return fmt.Errorf("insufficient validators for BFT: need at least 4, have %d", validatorCount)
	}

	// Validate this node is in the validator set
	if !cfg.ValidatorSet.IsValidator(cfg.NodeID) {
		return fmt.Errorf("node %x is not in validator set", cfg.NodeID[:8])
	}

	return nil
}

// Helper functions for creating configurations

// DefaultConfig returns a config template with required fields
func DefaultConfig() *Config {
	return &Config{
		EnableProposing: true,
		EnableVoting:    true,
		NodeType:        "validator",
	}
}

// NewConfigFromEnv creates a config using environment-based ConfigManager
func NewConfigFromEnv(
	nodeID ValidatorID,
	crypto CryptoService,
	audit AuditLogger,
	logger Logger,
	configMgr ConfigManager,
	allowlist IPAllowlist,
	validatorSet ValidatorSet,
) *Config {
	return &Config{
		NodeID:          nodeID,
		NodeType:        configMgr.GetString("NODE_TYPE", "validator"),
		EnableProposing: configMgr.GetBool("CONSENSUS_ENABLE_PROPOSING", true),
		EnableVoting:    configMgr.GetBool("CONSENSUS_ENABLE_VOTING", true),
		CryptoService:   crypto,
		AuditLogger:     audit,
		Logger:          logger,
		ConfigManager:   configMgr,
		IPAllowlist:     allowlist,
		ValidatorSet:    validatorSet,
	}
}
