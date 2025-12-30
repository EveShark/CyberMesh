package config

import (
	"context"
	"fmt"
	"time"

	"backend/pkg/utils"
)

// Dynamic consensus constants - calculated from topology
var (
	CalculatedMinConsensus int
	CalculatedMaxFailures  int
	CalculatedWriteQuorum  int

	DefaultConsensusTimeout  = 30 * time.Second
	DefaultHeartbeatInterval = 5 * time.Second
)

// Consensus-specific error codes
const (
	ErrCodeConsensusNotInitialized    = utils.ErrorCode("CONSENSUS_NOT_INITIALIZED")
	ErrCodeInsufficientConsensusNodes = utils.ErrorCode("INSUFFICIENT_CONSENSUS_NODES")
	ErrCodeInvalidQuorum              = utils.ErrorCode("INVALID_QUORUM")
	ErrCodeInvalidWeights             = utils.ErrorCode("INVALID_WEIGHTS")
	ErrCodeNodeNotInConsensus         = utils.ErrorCode("NODE_NOT_IN_CONSENSUS")
)

// Initialize consensus constants from topology
func InitializeConsensusConstants() error {
	topologyMutex.RLock()
	defer topologyMutex.RUnlock()

	if !topologyInitialized {
		return utils.NewError(ErrCodeTopologyNotReady,
			"topology not initialized - call InitializeTopology() first")
	}

	consensusNodeCount := len(ConsensusNodes)
	if consensusNodeCount < 4 {
		return utils.NewError(ErrCodeInsufficientConsensusNodes,
			fmt.Sprintf("need at least 4 consensus nodes for Byzantine fault tolerance, got %d", consensusNodeCount)).
			WithDetail("consensus_nodes", ConsensusNodes).
			WithDetail("cluster_size", ClusterSize)
	}

	// Byzantine fault tolerance: need 2f+1 nodes for BFT quorum
	CalculatedMaxFailures = (consensusNodeCount - 1) / 3
	CalculatedMinConsensus = 2*CalculatedMaxFailures + 1
	CalculatedWriteQuorum = (consensusNodeCount / 2) + 1

	return nil
}

// Consensus Configuration with full utils integration
type ConsensusConfig struct {
	// Topology-based settings
	MinConsensusNodes int           `json:"min_consensus_nodes"`
	MaxFailureNodes   int           `json:"max_failure_nodes"`
	ConsensusTimeout  time.Duration `json:"consensus_timeout"`
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`

	// Heartbeat security
	HeartbeatCryptoValidation      bool `json:"heartbeat_crypto_validation"`
	HeartbeatReplayWindowSeconds   int  `json:"heartbeat_replay_window_seconds"`
	CrossNodeValidationRequired    bool `json:"cross_node_validation_required"`
	HeartbeatCrossValidationQuorum int  `json:"heartbeat_cross_validation_quorum"`

	// Evidence pipeline
	ByzantineDetectionEnabled bool `json:"byzantine_detection_enabled"`
	EvidenceGossipEnabled     bool `json:"evidence_gossip_enabled"`
	EvidenceMinAttachments    int  `json:"evidence_min_attachments"`
	QuarantineAutoPropose     bool `json:"quarantine_auto_propose"`

	// Byzantine detection
	ByzantineScoreThreshold           int     `json:"byzantine_score_threshold"`
	ByzantineQuarantineDurationHours  int     `json:"byzantine_quarantine_duration_hours"`
	NodeReputationMinScore            float64 `json:"node_reputation_min_score"`
	NodeReputationDecayRate           float64 `json:"node_reputation_decay_rate"`
	LeaderRotationReputationThreshold float64 `json:"leader_rotation_reputation_threshold"`
	EmergencyLeaderRotationEnabled    bool    `json:"emergency_leader_rotation_enabled"`
	EmergencyQuorumMinNodes           int     `json:"emergency_quorum_min_nodes"`
	NetworkPartitionDetectionEnabled  bool    `json:"network_partition_detection_enabled"`
	RedundantBootstrapPeers           int     `json:"redundant_bootstrap_peers"`
	PeerReputationFiltering           bool    `json:"peer_reputation_filtering"`
	DynamicPeerDiscovery              bool    `json:"dynamic_peer_discovery"`

	// Adaptive timeouts
	AdaptiveTimeoutEnabled          bool    `json:"adaptive_timeout_enabled"`
	AdaptiveTimeoutMinMs            int     `json:"adaptive_timeout_min_ms"`
	AdaptiveTimeoutMaxMs            int     `json:"adaptive_timeout_max_ms"`
	PartitionDetectionWindowMs      int     `json:"partition_detection_window_ms"`
	ReputationUpdateIntervalSeconds int     `json:"reputation_update_interval_seconds"`
	ReputationDecayIntervalSeconds  int     `json:"reputation_decay_interval_seconds"`
	MultiFailoverSuccessionCount    int     `json:"multi_failover_succession_count"`
	ViewChangeTimeoutMultiplier     float64 `json:"view_change_timeout_multiplier"`

	// P2P hardening
	MinHealthyPeers                int  `json:"min_healthy_peers"`
	PeerScoringEnabled             bool `json:"peer_scoring_enabled"`
	CircuitBreakerEnabled          bool `json:"circuit_breaker_enabled"`
	CircuitBreakerFailureThreshold int  `json:"circuit_breaker_failure_threshold"`
	CircuitBreakerCooldownSeconds  int  `json:"circuit_breaker_cooldown_seconds"`
	PeerDiscoveryIntervalSeconds   int  `json:"peer_discovery_interval_seconds"`
	BootstrapDialJitterMs          int  `json:"bootstrap_dial_jitter_ms"`

	// Scoring weights
	PeerReputationWeight  float64 `json:"peer_reputation_weight"`
	ByzantineScoreWeight  float64 `json:"byzantine_score_weight"`
	SecurityPenaltyWeight float64 `json:"security_penalty_weight"`
	StabilityScoreWeight  float64 `json:"stability_score_weight"`

	// Emergency settings
	AllowDegradedBootstrap    bool `json:"allow_degraded_bootstrap"`
	DegradedBootstrapMinPeers int  `json:"degraded_bootstrap_min_peers"`

	// Security infrastructure
	CryptoService *utils.CryptoService `json:"-"`
	IPAllowlist   *utils.IPAllowlist   `json:"-"`
	HTTPClient    *utils.HTTPClient    `json:"-"`
	AuditLogger   *utils.AuditLogger   `json:"-"`

	// Metrics
	MetricsEnabled bool `json:"metrics_enabled"`
}

// ConsensusConfigBuilder provides fluent interface for building consensus config
type ConsensusConfigBuilder struct {
	config      *ConsensusConfig
	nodeID      int
	configMgr   *utils.ConfigManager
	cryptoSvc   *utils.CryptoService
	allowlist   *utils.IPAllowlist
	auditLogger *utils.AuditLogger
	err         error
}

// NewConsensusConfigBuilder creates a new builder
func NewConsensusConfigBuilder(nodeID int, configMgr *utils.ConfigManager, cryptoSvc *utils.CryptoService) *ConsensusConfigBuilder {
	return &ConsensusConfigBuilder{
		config:    &ConsensusConfig{},
		nodeID:    nodeID,
		configMgr: configMgr,
		cryptoSvc: cryptoSvc,
	}
}

// WithAllowlist sets the IP allowlist
func (b *ConsensusConfigBuilder) WithAllowlist(allowlist *utils.IPAllowlist) *ConsensusConfigBuilder {
	if b.err == nil {
		b.allowlist = allowlist
	}
	return b
}

// WithAuditLogger sets the audit logger
func (b *ConsensusConfigBuilder) WithAuditLogger(auditLogger *utils.AuditLogger) *ConsensusConfigBuilder {
	if b.err == nil {
		b.auditLogger = auditLogger
	}
	return b
}

// Build creates the consensus configuration
func (b *ConsensusConfigBuilder) Build(ctx context.Context) (*ConsensusConfig, error) {
	if b.err != nil {
		return nil, b.err
	}

	// Initialize consensus constants
	if err := InitializeConsensusConstants(); err != nil {
		return nil, err
	}

	// Validate node participates in consensus
	if err := b.validateNodeEligibility(); err != nil {
		return nil, err
	}

	// Load configuration
	if err := b.loadConsensusSettings(ctx); err != nil {
		return nil, err
	}

	// Setup security infrastructure
	if err := b.setupSecurity(ctx); err != nil {
		return nil, err
	}

	// Validate complete configuration
	if err := b.validate(ctx); err != nil {
		return nil, err
	}

	logger := utils.GetLogger()
	logger.InfoContext(ctx, "Built consensus configuration",
		utils.ZapInt("node_id", b.nodeID),
		utils.ZapInt("min_consensus", b.config.MinConsensusNodes),
		utils.ZapInt("max_failures", b.config.MaxFailureNodes),
		utils.ZapBool("byzantine_detection", b.config.ByzantineDetectionEnabled))

	return b.config, nil
}

// Private builder methods

func (b *ConsensusConfigBuilder) validateNodeEligibility() error {
	participatesInConsensus := false
	for _, consensusNodeID := range ConsensusNodes {
		if consensusNodeID == b.nodeID {
			participatesInConsensus = true
			break
		}
	}

	if !participatesInConsensus {
		return utils.NewError(ErrCodeNodeNotInConsensus,
			fmt.Sprintf("node %d does not participate in consensus", b.nodeID)).
			WithDetail("node_id", b.nodeID).
			WithDetail("consensus_nodes", ConsensusNodes)
	}

	return nil
}

func (b *ConsensusConfigBuilder) loadConsensusSettings(ctx context.Context) error {
	logger := utils.GetLogger()
	logger.InfoContext(ctx, "Loading consensus settings",
		utils.ZapInt("node_id", b.nodeID),
		utils.ZapInt("min_consensus", CalculatedMinConsensus),
		utils.ZapInt("max_failures", CalculatedMaxFailures),
		utils.ZapInts("consensus_nodes", ConsensusNodes))

	environment := b.configMgr.GetString("ENVIRONMENT", "production")
	isProduction := environment == "production"
	isSecureEnvironment := isProduction || environment == "staging"

	// Core settings
	b.config.MinConsensusNodes = CalculatedMinConsensus
	b.config.MaxFailureNodes = CalculatedMaxFailures
	b.config.ConsensusTimeout = b.configMgr.GetDuration("CONSENSUS_TIMEOUT", DefaultConsensusTimeout)
	b.config.HeartbeatInterval = b.configMgr.GetDuration("HEARTBEAT_INTERVAL", DefaultHeartbeatInterval)

	// Heartbeat security
	b.config.HeartbeatCryptoValidation = isSecureEnvironment
	b.config.HeartbeatReplayWindowSeconds = b.configMgr.GetIntRange("HEARTBEAT_REPLAY_WINDOW", 300, 60, 3600)
	b.config.CrossNodeValidationRequired = isSecureEnvironment
	b.config.HeartbeatCrossValidationQuorum = CalculatedMinConsensus

	// Byzantine detection
	b.config.ByzantineDetectionEnabled = true
	b.config.EvidenceGossipEnabled = true
	b.config.EvidenceMinAttachments = b.configMgr.GetIntRange("EVIDENCE_MIN_ATTACHMENTS", 2, 1, 10)
	b.config.QuarantineAutoPropose = isSecureEnvironment

	// Byzantine scoring
	b.config.ByzantineScoreThreshold = b.configMgr.GetIntRange("BYZANTINE_SCORE_THRESHOLD", 80, 0, 100)
	b.config.ByzantineQuarantineDurationHours = b.configMgr.GetIntRange("BYZANTINE_QUARANTINE_HOURS", 24, 1, 168)

	// Reputation system
	b.config.NodeReputationMinScore = b.configMgr.GetFloat64("NODE_REPUTATION_MIN_SCORE", 0.6)
	b.config.NodeReputationDecayRate = b.configMgr.GetFloat64("NODE_REPUTATION_DECAY_RATE", 0.01)
	b.config.LeaderRotationReputationThreshold = b.configMgr.GetFloat64("LEADER_ROTATION_THRESHOLD", 0.7)
	b.config.EmergencyLeaderRotationEnabled = true
	b.config.EmergencyQuorumMinNodes = CalculatedMinConsensus

	// Network resilience
	b.config.NetworkPartitionDetectionEnabled = true
	b.config.RedundantBootstrapPeers = len(BootstrapPeers)
	b.config.PeerReputationFiltering = isSecureEnvironment
	b.config.DynamicPeerDiscovery = b.configMgr.GetBool("DYNAMIC_PEER_DISCOVERY", true)

	// Adaptive timeouts
	b.config.AdaptiveTimeoutEnabled = b.configMgr.GetBool("ADAPTIVE_TIMEOUT_ENABLED", true)
	b.config.AdaptiveTimeoutMinMs = b.configMgr.GetIntRange("ADAPTIVE_TIMEOUT_MIN_MS", 1000, 100, 10000)
	b.config.AdaptiveTimeoutMaxMs = b.configMgr.GetIntRange("ADAPTIVE_TIMEOUT_MAX_MS", 30000, 1000, 60000)
	b.config.PartitionDetectionWindowMs = b.configMgr.GetIntRange("PARTITION_DETECTION_WINDOW_MS", 10000, 1000, 30000)

	// Reputation intervals
	b.config.ReputationUpdateIntervalSeconds = b.configMgr.GetIntRange("REPUTATION_UPDATE_INTERVAL", 60, 10, 300)
	b.config.ReputationDecayIntervalSeconds = b.configMgr.GetIntRange("REPUTATION_DECAY_INTERVAL", 300, 60, 3600)
	b.config.MultiFailoverSuccessionCount = b.configMgr.GetIntRange("MULTI_FAILOVER_COUNT", 3, 1, 5)
	b.config.ViewChangeTimeoutMultiplier = b.configMgr.GetFloat64("VIEW_CHANGE_TIMEOUT_MULTIPLIER", 2.0)

	// P2P hardening
	b.config.MinHealthyPeers = b.configMgr.GetIntRange("MIN_HEALTHY_PEERS", 3, 2, 10)
	b.config.PeerScoringEnabled = true
	b.config.CircuitBreakerEnabled = true
	b.config.CircuitBreakerFailureThreshold = b.configMgr.GetIntRange("CIRCUIT_BREAKER_THRESHOLD", 5, 1, 20)
	b.config.CircuitBreakerCooldownSeconds = b.configMgr.GetIntRange("CIRCUIT_BREAKER_COOLDOWN", 60, 30, 300)
	b.config.PeerDiscoveryIntervalSeconds = b.configMgr.GetIntRange("PEER_DISCOVERY_INTERVAL", 300, 60, 1800)
	b.config.BootstrapDialJitterMs = b.configMgr.GetIntRange("BOOTSTRAP_DIAL_JITTER_MS", 1000, 100, 5000)

	// Scoring weights
	b.config.PeerReputationWeight = b.configMgr.GetFloat64("PEER_REPUTATION_WEIGHT", 0.3)
	b.config.ByzantineScoreWeight = b.configMgr.GetFloat64("BYZANTINE_SCORE_WEIGHT", 0.4)
	b.config.SecurityPenaltyWeight = b.configMgr.GetFloat64("SECURITY_PENALTY_WEIGHT", 0.2)
	b.config.StabilityScoreWeight = b.configMgr.GetFloat64("STABILITY_SCORE_WEIGHT", 0.1)

	// Emergency settings
	b.config.AllowDegradedBootstrap = !isProduction && b.configMgr.GetBool("ALLOW_DEGRADED_BOOTSTRAP", false)
	b.config.DegradedBootstrapMinPeers = b.configMgr.GetIntRange("DEGRADED_BOOTSTRAP_MIN_PEERS", 2, 1, 5)

	// Metrics
	b.config.MetricsEnabled = true

	return nil
}

func (b *ConsensusConfigBuilder) setupSecurity(ctx context.Context) error {
	// Validate crypto service
	if b.cryptoSvc == nil {
		return utils.NewError(utils.CodeInternal, "crypto service is required for consensus")
	}
	b.config.CryptoService = b.cryptoSvc

	// Setup IP allowlist
	if b.allowlist == nil {
		allowlistConfig := utils.DefaultIPAllowlistConfig()

		if b.configMgr.GetString("ENVIRONMENT", "production") == "development" {
			allowlistConfig.AllowPrivate = true
			allowlistConfig.AllowLoopback = true
		}

		// Parse allowed CIDRs for consensus peers
		if cidrs := b.configMgr.GetString("CONSENSUS_ALLOWED_CIDRS", ""); cidrs != "" {
			allowlistConfig.AllowedCIDRs = b.configMgr.GetStringSlice("CONSENSUS_ALLOWED_CIDRS", nil)
		}

		allowlist, err := utils.NewIPAllowlist(allowlistConfig)
		if err != nil {
			return utils.WrapError(err, utils.CodeConfigInvalid,
				"failed to create consensus IP allowlist")
		}
		b.allowlist = allowlist
	}
	b.config.IPAllowlist = b.allowlist

	// Create HTTP client for peer communication
	httpClient, err := utils.NewHTTPClientBuilder().
		WithTimeout(b.config.ConsensusTimeout).
		WithTLSConfig(utils.DefaultHTTPClientConfig().TLSMinVersion,
			utils.DefaultHTTPClientConfig().TLSMaxVersion).
		WithRetry(3, 1*time.Second, 5*time.Second).
		Build()
	if err != nil {
		return utils.WrapError(err, utils.CodeInternal,
			"failed to create consensus HTTP client")
	}
	b.config.HTTPClient = httpClient

	// Setup audit logger
	if b.auditLogger == nil && b.config.ByzantineDetectionEnabled {
		auditConfig := utils.DefaultAuditConfig()
		auditConfig.FilePath = fmt.Sprintf("/var/log/consensus/node-%d-consensus.log", b.nodeID)
		auditConfig.NodeID = fmt.Sprintf("node-%d", b.nodeID)
		auditConfig.Component = "consensus"
		auditConfig.EnableSigning = true

		// Get signing key for audit log
		auditKey, err := b.cryptoSvc.GetKeyStore().DeriveAuditKey()
		if err != nil {
			return utils.WrapError(err, utils.CodeInternal, "failed to derive audit key")
		}
		auditConfig.SigningKey = auditKey

		auditLogger, err := utils.NewAuditLogger(auditConfig)
		if err != nil {
			return utils.WrapError(err, utils.CodeInternal,
				"failed to create consensus audit logger")
		}
		b.auditLogger = auditLogger
	}
	b.config.AuditLogger = b.auditLogger

	return nil
}

func (b *ConsensusConfigBuilder) validate(ctx context.Context) error {
	if b.config == nil {
		return utils.NewValidationError("consensus configuration cannot be nil")
	}

	// Validate topology consistency
	if b.config.MinConsensusNodes != CalculatedMinConsensus {
		return utils.NewError(ErrCodeInvalidQuorum,
			fmt.Sprintf("must be %d for current topology (consensus nodes: %d)",
				CalculatedMinConsensus, len(ConsensusNodes))).
			WithDetail("expected", CalculatedMinConsensus).
			WithDetail("actual", b.config.MinConsensusNodes).
			WithDetail("consensus_nodes", len(ConsensusNodes))
	}

	if b.config.MaxFailureNodes != CalculatedMaxFailures {
		return utils.NewError(ErrCodeInvalidQuorum,
			fmt.Sprintf("must be %d for Byzantine fault tolerance with %d consensus nodes",
				CalculatedMaxFailures, len(ConsensusNodes))).
			WithDetail("expected", CalculatedMaxFailures).
			WithDetail("actual", b.config.MaxFailureNodes)
	}

	// Timeout validations
	if b.config.ConsensusTimeout <= 0 {
		return utils.NewValidationError("consensus timeout must be greater than 0")
	}

	if b.config.HeartbeatInterval <= 0 {
		return utils.NewValidationError("heartbeat interval must be greater than 0")
	}

	// Quorum validations
	if b.config.HeartbeatCrossValidationQuorum > len(ConsensusNodes) {
		return utils.NewError(ErrCodeInvalidQuorum,
			fmt.Sprintf("heartbeat quorum cannot exceed consensus node count (%d)", len(ConsensusNodes)))
	}

	if b.config.EmergencyQuorumMinNodes > len(ConsensusNodes) {
		return utils.NewError(ErrCodeInvalidQuorum,
			fmt.Sprintf("emergency quorum cannot exceed consensus node count (%d)", len(ConsensusNodes)))
	}

	// Bootstrap peer validation
	if b.config.RedundantBootstrapPeers > len(BootstrapPeers) {
		return utils.NewValidationError(
			fmt.Sprintf("redundant bootstrap peers (%d) cannot exceed available peers (%d)",
				b.config.RedundantBootstrapPeers, len(BootstrapPeers)))
	}

	// Weight validation with proper float handling
	weights := []float64{
		b.config.PeerReputationWeight,
		b.config.ByzantineScoreWeight,
		b.config.SecurityPenaltyWeight,
		b.config.StabilityScoreWeight,
	}

	// Validate individual weights
	for i, weight := range weights {
		if weight < 0.0 || weight > 1.0 {
			names := []string{"PeerReputationWeight", "ByzantineScoreWeight",
				"SecurityPenaltyWeight", "StabilityScoreWeight"}
			return utils.NewError(ErrCodeInvalidWeights,
				fmt.Sprintf("%s must be between 0.0 and 1.0, got %.2f", names[i], weight))
		}
	}

	// Validate weight sum
	totalWeight := 0.0
	for _, w := range weights {
		totalWeight += w
	}

	if totalWeight < 0.99 || totalWeight > 1.01 {
		return utils.NewError(ErrCodeInvalidWeights,
			fmt.Sprintf("weight sum should be 1.0, got %.2f", totalWeight)).
			WithDetail("peer_reputation", b.config.PeerReputationWeight).
			WithDetail("byzantine_score", b.config.ByzantineScoreWeight).
			WithDetail("security_penalty", b.config.SecurityPenaltyWeight).
			WithDetail("stability_score", b.config.StabilityScoreWeight)
	}

	// Validate reputation thresholds
	if b.config.NodeReputationMinScore < 0.0 || b.config.NodeReputationMinScore > 1.0 {
		return utils.NewValidationError("node reputation min score must be between 0.0 and 1.0")
	}

	if b.config.NodeReputationDecayRate < 0.0 || b.config.NodeReputationDecayRate > 1.0 {
		return utils.NewValidationError("node reputation decay rate must be between 0.0 and 1.0")
	}

	if b.config.LeaderRotationReputationThreshold < 0.0 || b.config.LeaderRotationReputationThreshold > 1.0 {
		return utils.NewValidationError("leader rotation threshold must be between 0.0 and 1.0")
	}

	// Validate adaptive timeout ranges
	if b.config.AdaptiveTimeoutMinMs > b.config.AdaptiveTimeoutMaxMs {
		return utils.NewValidationError("adaptive timeout min must be <= max")
	}

	return nil
}

// Public API

// BuildConsensusConfigForNode builds consensus configuration for a node
func BuildConsensusConfigForNode(ctx context.Context, nodeID int, configMgr *utils.ConfigManager, cryptoSvc *utils.CryptoService) (*ConsensusConfig, error) {
	builder := NewConsensusConfigBuilder(nodeID, configMgr, cryptoSvc)
	return builder.Build(ctx)
}

// BuildConsensusConfigWithSecurity builds config with custom security components
func BuildConsensusConfigWithSecurity(
	ctx context.Context,
	nodeID int,
	configMgr *utils.ConfigManager,
	cryptoSvc *utils.CryptoService,
	allowlist *utils.IPAllowlist,
	auditLogger *utils.AuditLogger,
) (*ConsensusConfig, error) {
	builder := NewConsensusConfigBuilder(nodeID, configMgr, cryptoSvc).
		WithAllowlist(allowlist).
		WithAuditLogger(auditLogger)
	return builder.Build(ctx)
}

// Methods for consensus operations

// SignHeartbeat signs a heartbeat message
func (c *ConsensusConfig) SignHeartbeat(ctx context.Context, heartbeatData []byte) ([]byte, error) {
	if c.CryptoService == nil {
		return nil, utils.NewError(utils.CodeInternal, "crypto service not configured")
	}
	return c.CryptoService.SignWithContext(ctx, heartbeatData)
}

// VerifyHeartbeat verifies a heartbeat signature
func (c *ConsensusConfig) VerifyHeartbeat(ctx context.Context, heartbeatData, signature []byte, peerPubKey []byte) error {
	if c.CryptoService == nil {
		return utils.NewError(utils.CodeInternal, "crypto service not configured")
	}
	return c.CryptoService.VerifyWithContext(ctx, heartbeatData, signature, peerPubKey, false)
}

// AuditByzantineDetection logs Byzantine behavior detection
func (c *ConsensusConfig) AuditByzantineDetection(ctx context.Context, nodeID int, reason string, evidence map[string]interface{}) error {
	if c.AuditLogger == nil {
		return nil // Audit logging not enabled
	}

	fields := map[string]interface{}{
		"byzantine_node_id": nodeID,
		"detection_reason":  reason,
	}
	for k, v := range evidence {
		fields[k] = v
	}

	return c.AuditLogger.Security("byzantine_behavior_detected", fields)
}

// AuditLeaderChange logs leader rotation events
func (c *ConsensusConfig) AuditLeaderChange(ctx context.Context, oldLeader, newLeader int, reason string) error {
	if c.AuditLogger == nil {
		return nil
	}

	return c.AuditLogger.Info("leader_rotation", map[string]interface{}{
		"old_leader": oldLeader,
		"new_leader": newLeader,
		"reason":     reason,
	})
}

// ValidatePeerAddress validates a peer address against allowlist
func (c *ConsensusConfig) ValidatePeerAddress(ctx context.Context, address string) error {
	if c.IPAllowlist == nil {
		return nil // Allowlist not configured
	}
	return c.IPAllowlist.ValidateBeforeConnect(ctx, address)
}

// Close closes resources
func (c *ConsensusConfig) Close() error {
	if c.HTTPClient != nil {
		c.HTTPClient.Close()
	}
	if c.AuditLogger != nil {
		c.AuditLogger.Close()
	}
	return nil
}

// GetConsensusConfigSummary returns configuration summary
func GetConsensusConfigSummary() map[string]interface{} {
	return map[string]interface{}{
		"cluster_size":         ClusterSize,
		"consensus_nodes":      len(ConsensusNodes),
		"min_consensus_nodes":  CalculatedMinConsensus,
		"max_failure_nodes":    CalculatedMaxFailures,
		"write_quorum":         CalculatedWriteQuorum,
		"bootstrap_peers":      len(BootstrapPeers),
		"topology_initialized": topologyInitialized,
	}
}
