package config

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/pkg/utils"
)

// SHARED TOPOLOGY GLOBALS - Single source of truth
var (
	// Topology discovery from environment (thread-safe initialization)
	NodeRoles     map[int]string   // node_id -> role
	RoleInstances map[string][]int // role -> [node_ids]
	ClusterSize   int              // Total number of nodes
	MinQuorum     int              // Minimum nodes for consensus

	// Bootstrap and consensus configuration
	BootstrapPeers []int // Bootstrap peer node IDs
	ConsensusNodes []int // Nodes participating in consensus

	// Initialization state (thread-safe)
	topologyInitialized bool
	topologyMutex       sync.RWMutex
)

// Security Constants - Non-negotiable minimums
const (
	MinPasswordLength   = 16
	MinEncryptionKeyLen = 32
	MinSaltLength       = 16
	MinSecretKeyLength  = 32
	RequiredTLSVersion  = "1.3"
	MaxConfigRetries    = 3
	DefaultPortBase     = 8000
	DefaultAPIBase      = 9000
	DefaultMetricsBase  = 10000
)

// Core Required Environment Variables - Security-first
var CoreSecurityVars = []string{
	"ENCRYPTION_KEY", // 32-byte hex string for AES-256
	"SECRET_KEY",     // 32+ character secret for signing
	"SALT",           // 16+ character salt for hashing
	"TLS_CERT_PATH",  // Path to TLS certificate
	"TLS_KEY_PATH",   // Path to TLS private key
	"P2P_ENCRYPTION", // Must be 'true'
	"AUDIT_LOG_PATH", // Security audit logs
}

// Topology Required Variables
var TopologyVars = []string{
	"NODE_ID",          // This node's unique ID
	"CLUSTER_TOPOLOGY", // "role:count,role:count" format
}

// External Service Security Variables
var ExternalServiceSecurityVars = []string{
	"AI_SERVICE_ENCRYPTION_KEY",
	"KAFKA_SASL_PASSWORD",
	"AI_SERVICE_TLS_CERT_PATH",
	"AI_RATE_LIMIT_SECRET",
	"EXTERNAL_SERVICE_ALLOWLIST",
}

// Security Configuration Errors
type SecurityError struct {
	Field   string
	Reason  string
	Context map[string]interface{}
}

func (e *SecurityError) Error() string {
	return fmt.Sprintf("SECURITY VIOLATION: %s - %s", e.Field, e.Reason)
}

// CORE TOPOLOGY INITIALIZATION - Thread-safe, single execution
func InitializeTopology() error {
	topologyMutex.Lock()
	defer topologyMutex.Unlock()

	if topologyInitialized {
		return nil // Already initialized
	}

	// Parse CLUSTER_TOPOLOGY environment variable
	topologyStr := os.Getenv("CLUSTER_TOPOLOGY")
	if topologyStr == "" {
		return &SecurityError{
			Field:  "CLUSTER_TOPOLOGY",
			Reason: "Must specify cluster topology in format 'role:count,role:count'",
		}
	}

	// Initialize global maps
	NodeRoles = make(map[int]string)
	RoleInstances = make(map[string][]int)

	// Parse topology string: "ai:2,processing:1,gateway:3,storage:2,control:1"
	pairs := strings.Split(topologyStr, ",")
	currentNodeID := 1

	for _, pair := range pairs {
		parts := strings.Split(strings.TrimSpace(pair), ":")
		if len(parts) != 2 {
			return fmt.Errorf("invalid topology format '%s', expected 'role:count'", pair)
		}

		role := strings.TrimSpace(parts[0])
		countStr := strings.TrimSpace(parts[1])

		count, err := strconv.Atoi(countStr)
		if err != nil || count < 0 {
			return fmt.Errorf("invalid count '%s' for role '%s'", countStr, role)
		}

		// Assign node IDs for this role
		instances := make([]int, count)
		for i := 0; i < count; i++ {
			NodeRoles[currentNodeID] = role
			instances[i] = currentNodeID
			currentNodeID++
		}

		RoleInstances[role] = instances
	}

	ClusterSize = currentNodeID - 1
	MinQuorum = (ClusterSize / 2) + 1

	// Allow override of quorum size
	if quorumStr := os.Getenv("QUORUM_SIZE"); quorumStr != "" {
		if q, err := strconv.Atoi(quorumStr); err == nil && q > 0 && q <= ClusterSize {
			MinQuorum = q
		}
	}

	// CRITICAL: Fail fast if cluster size is too small
	if ClusterSize < MinQuorum {
		return &SecurityError{
			Field:  "CLUSTER_SIZE",
			Reason: fmt.Sprintf("Cluster size %d is less than minimum quorum %d", ClusterSize, MinQuorum),
		}
	}

	// Calculate bootstrap and consensus peers
	BootstrapPeers = calculateBootstrapPeers()
	ConsensusNodes = calculateConsensusNodes()

	topologyInitialized = true
	return nil
}

// SecureTokenManager wraps utils.CryptoService for token management
type SecureTokenManager struct {
	cryptoService *utils.CryptoService
	auditLogger   *utils.AuditLogger
	configManager *utils.ConfigManager
	logger        *utils.Logger
}

// NewSecureTokenManager creates a token manager using utils.CryptoService
func NewSecureTokenManager(ctx context.Context) (*SecureTokenManager, error) {
	logger := utils.GetLogger()

	// Create config manager for secure environment access
	configManager, err := utils.NewConfigManager(&utils.ConfigManagerConfig{
		Logger:        logger,
		SensitiveKeys: []string{"encryption_key", "secret_key", "salt", "password", "token"},
		RedactMode:    utils.RedactFull,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create config manager: %w", err)
	}

	// Setup audit logging
	auditPath := configManager.GetString("AUDIT_LOG_PATH", "/var/log/cybermesh/audit.log")
	auditConfig := &utils.AuditConfig{
		FilePath:       auditPath,
		EnableRotation: true,
		MaxSize:        100,
		MaxBackups:     30,
		MaxAge:         90,
		Compress:       true,
		EnableSigning:  true,
		NodeID:         configManager.GetString("NODE_ID", ""),
		Component:      "config-manager",
	}

	// Get signing key for audit log
	signingKey := configManager.GetString("AUDIT_SIGNING_KEY", "")
	if signingKey != "" {
		auditConfig.SigningKey = []byte(signingKey)
	}

	auditLogger, err := utils.NewAuditLogger(auditConfig)
	if err != nil {
		logger.Warn("Failed to initialize audit logger, continuing without audit",
			utils.ZapError(err))
	}

	// Create crypto service for key management
	cryptoConfig := &utils.CryptoConfig{
		KeyTTL:                 24 * time.Hour,
		MaxKeyHistory:          10,
		AutoRotate:             false,
		EnableReplayProtection: true,
		ReplayWindowSize:       10000,
		MaxSignatureAge:        24 * time.Hour,
		EnableAuditLog:         auditLogger != nil,
	}

	cryptoService, err := utils.NewCryptoService(cryptoConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize crypto service: %w", err)
	}

	tm := &SecureTokenManager{
		cryptoService: cryptoService,
		auditLogger:   auditLogger,
		configManager: configManager,
		logger:        logger,
	}

	// Log initialization
	if auditLogger != nil {
		auditLogger.Security("token_manager_initialized", map[string]interface{}{
			"node_id":   configManager.GetString("NODE_ID", ""),
			"component": "SecureTokenManager",
		})
	}

	return tm, nil
}

// GetOrGenerateToken gets or generates a secure token using CryptoService
func (tm *SecureTokenManager) GetOrGenerateToken(ctx context.Context, tokenName string, length int) (string, error) {
	// Try to load from environment first
	envKey := strings.ToUpper(tokenName)
	if existingToken, err := tm.configManager.GetSecret(envKey); err == nil && existingToken != "" {
		tm.logger.InfoContext(ctx, "Loaded existing token from environment",
			utils.ZapString("token_name", tokenName))

		if tm.auditLogger != nil {
			tm.auditLogger.Info("token_loaded", map[string]interface{}{
				"token_name": tokenName,
				"source":     "environment",
			})
		}

		return existingToken, nil
	}

	// Generate new token using crypto/rand (FIXED)
	tokenBytes := make([]byte, length/2) // hex encoding doubles size
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}

	token := hex.EncodeToString(tokenBytes)

	tm.logger.InfoContext(ctx, "Generated new token",
		utils.ZapString("token_name", tokenName),
		utils.ZapInt("length", length))

	if tm.auditLogger != nil {
		tm.auditLogger.Security("token_generated", map[string]interface{}{
			"token_name": tokenName,
			"length":     length,
		})
	}

	return token, nil
}

// Shutdown cleans up resources
func (tm *SecureTokenManager) Shutdown() error {
	if tm.cryptoService != nil {
		if err := tm.cryptoService.Shutdown(); err != nil {
			return err
		}
	}

	if tm.auditLogger != nil {
		if err := tm.auditLogger.Close(); err != nil {
			return err
		}
	}

	return nil
}

// Flexible Configuration Structure - Complete integration
type Config struct {
	Node        *NodeConfig        `json:"node_config"`
	Topology    *TopologyConfig    `json:"topology_config"`
	Security    *SecurityConfig    `json:"security_config"`
	Network     *NetworkConfig     `json:"network_config"`
	Services    *ServicesConfig    `json:"services_config"`
	Storage     *StorageConfig     `json:"storage_config"`
	Monitoring  *MonitoringConfig  `json:"monitoring_config"`
	AI          *AIConfig          `json:"ai_config"`
	Kafka       *KafkaConfig       `json:"kafka_config"`
	Consensus   *ConsensusConfig   `json:"consensus_config"`
	Blockchain  *BlockchainConfig  `json:"blockchain_config"`
	Operational *OperationalConfig `json:"operational_config"`
}

// Dynamic Topology Configuration
type TopologyConfig struct {
	ClusterSize    int              `json:"cluster_size"`
	QuorumSize     int              `json:"quorum_size"`
	RoleMapping    map[string][]int `json:"role_mapping"`
	NodeMapping    map[int]string   `json:"node_mapping"`
	ServicePeers   map[string][]int `json:"service_peers"`
	BootstrapPeers []int            `json:"bootstrap_peers"`
	ConsensusNodes []int            `json:"consensus_nodes"`
}

// Services Configuration
type ServicesConfig struct {
	Endpoints map[string]*ServiceEndpoint `json:"endpoints"`
}

// Service Endpoint Configuration
type ServiceEndpoint struct {
	URL           string            `json:"url"`
	AuthToken     string            `json:"-"`
	TLSEnabled    bool              `json:"tls_enabled"`
	Timeout       time.Duration     `json:"timeout"`
	RetryAttempts int               `json:"retry_attempts"`
	HealthCheck   string            `json:"health_check"`
	Headers       map[string]string `json:"headers"`
}

// AI Service Configuration
type AIConfig struct {
	Services       []ExternalAIService `json:"services"`
	DefaultService string              `json:"default_service"`
	Timeout        time.Duration       `json:"timeout"`
	MaxRetries     int                 `json:"max_retries"`
}

// External AI Service Configuration
type ExternalAIService struct {
	Name         string   `json:"name"`
	Endpoint     string   `json:"endpoint"`
	AuthToken    string   `json:"-"`
	ModelType    string   `json:"model_type"`
	AllowedCIDRs []string `json:"allowed_cidrs"`
	RateLimitRPS int      `json:"rate_limit_rps"`
	TLSVerify    bool     `json:"tls_verify"`
}

// Kafka Configuration - Military-grade security for Confluent Cloud
type KafkaConfig struct {
	// Connection
	Brokers []string      `json:"brokers"` // Comma-separated Kafka brokers
	Timeout time.Duration `json:"timeout"` // Connection timeout (default: 30s)

	// Security (TLS/SASL)
	TLSEnabled    bool   `json:"tls_enabled"`    // Enable TLS (required in production)
	SASLMechanism string `json:"sasl_mechanism"` // SCRAM-SHA-256, SCRAM-SHA-512, PLAIN
	SASLUsername  string `json:"sasl_username"`  // SASL username
	SASLPassword  string `json:"-"`              // SASL password (never logged)

	// Topics - Input (AI → Backend)
	InputTopics []string `json:"input_topics"` // ai.anomalies.v1, ai.evidence.v1, ai.policy.v1

	// Topics - Output (Backend → AI)
	OutputTopicCommits    string `json:"output_topic_commits"`    // control.commits.v1
	OutputTopicReputation string `json:"output_topic_reputation"` // control.reputation.v1
	OutputTopicPolicy     string `json:"output_topic_policy"`     // control.policy.v1
	OutputTopicEvidence   string `json:"output_topic_evidence"`   // control.evidence.v1

	// Consumer Settings
	ConsumerGroupID        string        `json:"consumer_group_id"`         // Consumer group (e.g., "backend-validators")
	ConsumerOffsetInitial  string        `json:"consumer_offset_initial"`   // "latest" or "earliest"
	ConsumerMaxPollRecords int           `json:"consumer_max_poll_records"` // Max records per poll (default: 500)
	ConsumerSessionTimeout time.Duration `json:"consumer_session_timeout"`  // Session timeout (default: 10s)
	ConsumerHeartbeat      time.Duration `json:"consumer_heartbeat"`        // Heartbeat interval (default: 3s)

	// Producer Settings
	ProducerIdempotent   bool   `json:"producer_idempotent"`    // Idempotent producer (default: true)
	ProducerRequiredAcks int16  `json:"producer_required_acks"` // Required acks: -1 (all), 1 (leader), 0 (none)
	ProducerRetries      int    `json:"producer_retries"`       // Max retries (default: 3)
	ProducerCompression  string `json:"producer_compression"`   // none, gzip, snappy, lz4, zstd

	// Dead Letter Queue
	DLQEnabled     bool   `json:"dlq_enabled"`      // Enable DLQ routing
	DLQTopicSuffix string `json:"dlq_topic_suffix"` // DLQ suffix (default: ".dlq")

	// Limits (DoS Protection)
	MaxMessageSize int `json:"max_message_size"` // Max message size in bytes (default: 1MB)
	MaxPayloadSize int `json:"max_payload_size"` // Max payload size (default: 512KB)
}

// TopologyEnvironmentProvider provides access to environment variables with validation
type TopologyEnvironmentProvider struct {
	configManager *utils.ConfigManager
}

// NewTopologyEnvironmentProvider creates a new environment provider
func NewTopologyEnvironmentProvider(configManager *utils.ConfigManager) *TopologyEnvironmentProvider {
	return &TopologyEnvironmentProvider{
		configManager: configManager,
	}
}

// Get retrieves an environment variable value
func (t *TopologyEnvironmentProvider) Get(key string) string {
	return t.configManager.GetString(key, "")
}

// FlexibleConfigLoader provides flexible configuration loading
type FlexibleConfigLoader struct {
	env           *TopologyEnvironmentProvider
	configManager *utils.ConfigManager
	cryptoService *utils.CryptoService
	logger        *utils.Logger
}

// NewFlexibleConfigLoader creates a new flexible config loader
func NewFlexibleConfigLoader(configManager *utils.ConfigManager) (*FlexibleConfigLoader, error) {
	env := NewTopologyEnvironmentProvider(configManager)

	// Create crypto service
	cryptoConfig := utils.DefaultCryptoConfig()
	cryptoService, err := utils.NewCryptoService(cryptoConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create crypto service: %w", err)
	}

	return &FlexibleConfigLoader{
		env:           env,
		configManager: configManager,
		cryptoService: cryptoService,
		logger:        utils.GetLogger(),
	}, nil
}

// Enhanced Configuration Loader using utils
type EnhancedConfigLoader struct {
	configManager *utils.ConfigManager
	tokenManager  *SecureTokenManager
	cryptoService *utils.CryptoService
	auditLogger   *utils.AuditLogger
	logger        *utils.Logger
}

func NewEnhancedConfigLoader(ctx context.Context) (*EnhancedConfigLoader, error) {
	logger := utils.GetLogger()

	// Create token manager (includes crypto service and audit logger)
	tokenManager, err := NewSecureTokenManager(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create token manager: %w", err)
	}

	return &EnhancedConfigLoader{
		configManager: tokenManager.configManager,
		tokenManager:  tokenManager,
		cryptoService: tokenManager.cryptoService,
		auditLogger:   tokenManager.auditLogger,
		logger:        logger,
	}, nil
}

func (loader *EnhancedConfigLoader) LoadConfig(ctx context.Context) (*Config, error) {
	// Validate environment and ensure topology is initialized
	if err := loader.validateRequiredEnvironment(); err != nil {
		return nil, fmt.Errorf("environment validation failed: %w", err)
	}

	loader.logger.InfoContext(ctx, "Topology discovered",
		utils.ZapInt("cluster_size", ClusterSize),
		utils.ZapInt("quorum_size", MinQuorum),
		utils.ZapAny("role_mapping", RoleInstances))

	if loader.auditLogger != nil {
		loader.auditLogger.Info("config_load_started", map[string]interface{}{
			"cluster_size": ClusterSize,
			"quorum_size":  MinQuorum,
		})
	}

	// Load configurations in strict dependency order
	securityConfig, err := loader.loadEnhancedSecurityConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load security config: %w", err)
	}

	topologyConfig := &TopologyConfig{
		ClusterSize:    ClusterSize,
		QuorumSize:     MinQuorum,
		RoleMapping:    RoleInstances,
		NodeMapping:    NodeRoles,
		ServicePeers:   make(map[string][]int),
		BootstrapPeers: BootstrapPeers,
		ConsensusNodes: ConsensusNodes,
	}

	// Load remaining configurations
	consensusConfig, err := loader.loadConsensusConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load consensus config: %w", err)
	}

	storageConfig, err := loader.loadStorageConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load storage config: %w", err)
	}

	blockchainConfig, err := loader.loadBlockchainConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load blockchain config: %w", err)
	}

	operationalConfig, err := loader.loadOperationalConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load operational config: %w", err)
	}

	monitoringConfig, err := loader.loadMonitoringConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load monitoring config: %w", err)
	}

	config := &Config{
		Topology:    topologyConfig,
		Security:    securityConfig,
		Consensus:   consensusConfig,
		Storage:     storageConfig,
		Blockchain:  blockchainConfig,
		Operational: operationalConfig,
		Monitoring:  monitoringConfig,
	}

	// Final system-wide security validation
	if err := ValidateSystemSecurity(ctx, config); err != nil {
		return nil, fmt.Errorf("system security validation failed: %w", err)
	}

	loader.logger.InfoContext(ctx, "Complete configuration loaded and validated successfully",
		utils.ZapInt("cluster_size", config.Topology.ClusterSize),
		utils.ZapInt("consensus_nodes", len(config.Topology.ConsensusNodes)),
		utils.ZapString("environment", loader.configManager.GetString("ENVIRONMENT", "production")))

	if loader.auditLogger != nil {
		loader.auditLogger.Security("config_loaded_successfully", map[string]interface{}{
			"cluster_size":    config.Topology.ClusterSize,
			"consensus_nodes": len(config.Topology.ConsensusNodes),
		})
	}

	return config, nil
}

func (loader *EnhancedConfigLoader) validateRequiredEnvironment() error {
	// Ensure topology is initialized first
	if err := InitializeTopology(); err != nil {
		return fmt.Errorf("topology initialization failed: %w", err)
	}

	// Validate core security vars using ConfigManager
	missing := make([]string, 0)

	for _, envVar := range CoreSecurityVars {
		if loader.configManager.GetString(envVar, "") == "" {
			missing = append(missing, envVar)
		}
	}

	for _, envVar := range TopologyVars {
		if loader.configManager.GetString(envVar, "") == "" {
			missing = append(missing, envVar)
		}
	}

	if len(missing) > 0 {
		return &SecurityError{
			Field:  "Environment",
			Reason: fmt.Sprintf("Missing required variables: %s", strings.Join(missing, ", ")),
		}
	}

	return nil
}

// Load enhanced security configuration using integrated builder
func (loader *EnhancedConfigLoader) loadEnhancedSecurityConfig(ctx context.Context) (*SecurityConfig, error) {
	// Use the security builder from config_security.go
	return BuildSecurityConfig(ctx, loader.configManager)
}

// Placeholder loading methods - will be implemented in subsequent refactoring
func (loader *EnhancedConfigLoader) loadConsensusConfig(ctx context.Context) (*ConsensusConfig, error) {
	nodeID := loader.configManager.GetInt("NODE_ID", 1)
	return BuildConsensusConfigForNode(ctx, nodeID, loader.configManager, loader.cryptoService)
}

func (loader *EnhancedConfigLoader) loadStorageConfig(ctx context.Context) (*StorageConfig, error) {
	env := NewTopologyEnvironmentProvider(loader.configManager)
	enhancedStorage, err := LoadStorageConfigForEnvironment(ctx, env, loader.cryptoService)
	if err != nil {
		return nil, err
	}

	// Convert EnhancedStorageConfig to StorageConfig for backwards compatibility
	return &StorageConfig{
		DatabaseURL:      enhancedStorage.DatabaseURL,
		EncryptionKey:    "", // Managed by crypto service
		SSLMode:          enhancedStorage.SSLMode,
		ConnectionPool:   enhancedStorage.ConnectionPool,
		QueryTimeout:     enhancedStorage.QueryTimeout,
		RetentionDays:    enhancedStorage.RetentionDays,
		BackupEnabled:    enhancedStorage.BackupEnabled,
		ReplicationNodes: enhancedStorage.ReplicationNodes,
		ShardingEnabled:  enhancedStorage.ShardingEnabled,
		StorageNodes:     enhancedStorage.StorageNodes,
		DDoSThreshold:    enhancedStorage.DDoSThreshold,
		MalwareTimeout:   enhancedStorage.MalwareTimeout,
	}, nil
}

func (loader *EnhancedConfigLoader) loadBlockchainConfig(ctx context.Context) (*BlockchainConfig, error) {
	// Implementation will be added
	return &BlockchainConfig{}, nil
}

func (loader *EnhancedConfigLoader) loadOperationalConfig(ctx context.Context) (*OperationalConfig, error) {
	// Implementation will be added
	return &OperationalConfig{}, nil
}

func (loader *EnhancedConfigLoader) loadMonitoringConfig(ctx context.Context) (*MonitoringConfig, error) {
	// Implementation will be added
	return &MonitoringConfig{}, nil
}

// Cleanup resources
func (loader *EnhancedConfigLoader) Shutdown() error {
	if loader.tokenManager != nil {
		return loader.tokenManager.Shutdown()
	}
	return nil
}

// Utility functions
func calculateBootstrapPeers() []int {
	majorRoles := []string{"gateway", "storage", "control"}
	peers := make([]int, 0)
	rolesCovered := make(map[string]bool)

	for _, role := range majorRoles {
		if instances, exists := RoleInstances[role]; exists && len(instances) > 0 {
			peers = append(peers, instances[0])
			rolesCovered[role] = true
		}
	}

	for role, instances := range RoleInstances {
		if len(peers) >= MinQuorum {
			break
		}

		if !rolesCovered[role] && len(instances) > 0 {
			peers = append(peers, instances[0])
			rolesCovered[role] = true
		}
	}

	if len(peers) < 2 {
		return []int{1, 2}
	}

	return peers
}

func calculateConsensusNodes() []int {
	consensusStr := os.Getenv("CONSENSUS_NODES")
	if consensusStr != "" {
		if nodes, err := parseIntList(consensusStr, 1, ClusterSize); err == nil {
			return uniqueSortedInts(nodes)
		}
	}

	consensusRoles := []string{"storage", "control", "gateway"}
	nodes := make([]int, 0)

	for _, role := range consensusRoles {
		if instances, exists := RoleInstances[role]; exists {
			nodes = append(nodes, instances...)
		}
	}

	if len(nodes) == 0 {
		for nodeID := range NodeRoles {
			nodes = append(nodes, nodeID)
		}
	}

	return uniqueSortedInts(nodes)
}

func parseIntList(input string, min, max int) ([]int, error) {
	if input == "" {
		return nil, fmt.Errorf("empty input")
	}

	parts := strings.Split(input, ",")
	result := make([]int, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		num, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid number '%s': %w", part, err)
		}

		if num < min || num > max {
			return nil, fmt.Errorf("number %d out of range [%d-%d]", num, min, max)
		}

		result = append(result, num)
	}

	return result, nil
}

func uniqueSortedInts(values []int) []int {
	if len(values) == 0 {
		return values
	}

	sort.Ints(values)

	out := values[:1]
	last := values[0]
	for _, v := range values[1:] {
		if v == last {
			continue
		}
		out = append(out, v)
		last = v
	}

	return out
}

// Main entry point for loading complete integrated configuration
func LoadCompleteConfig(nodeID int) (*Config, error) {
	ctx := utils.ContextWithOperation(context.Background(), "load_complete_config")
	ctx = utils.ContextWithNodeID(ctx, strconv.Itoa(nodeID))

	// Set NODE_ID for validation
	os.Setenv("NODE_ID", strconv.Itoa(nodeID))

	loader, err := NewEnhancedConfigLoader(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create config loader: %w", err)
	}
	defer loader.Shutdown()

	return loader.LoadConfig(ctx)
}

// Initialize the complete configuration system - must be called first
func InitializeConfigurationSystem() error {
	if err := InitializeTopology(); err != nil {
		return fmt.Errorf("failed to initialize topology: %w", err)
	}

	if err := InitializeNodeConstants(); err != nil {
		return fmt.Errorf("failed to initialize node constants: %w", err)
	}

	if err := InitializeConsensusConstants(); err != nil {
		return fmt.Errorf("failed to initialize consensus constants: %w", err)
	}

	if err := InitializeStorageConstants(); err != nil {
		return fmt.Errorf("failed to initialize storage constants: %w", err)
	}

	return nil
}

// Configuration system status for monitoring
func GetConfigurationSystemStatus() map[string]interface{} {
	return map[string]interface{}{
		"topology_initialized": topologyInitialized,
		"cluster_size":         ClusterSize,
		"min_quorum":           MinQuorum,
		"bootstrap_peers":      len(BootstrapPeers),
		"consensus_nodes":      len(ConsensusNodes),
		"role_mapping":         RoleInstances,
		"node_mapping":         NodeRoles,
		"consensus_summary":    GetConsensusConfigSummary(),
		"storage_summary":      GetStorageConfigSummary(),
	}
}
