package config

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"backend/pkg/utils"
)

// Dynamic storage constants - calculated from topology
var (
	// Storage node configuration (populated from RoleInstances["storage"])
	StorageNodeIDs     []int
	StorageClusterSize int
	StorageWriteQuorum int

	// Storage defaults - can be overridden by environment
	DefaultRetentionDays  = 30
	DefaultConnectionPool = 25
	DefaultDDoSThreshold  = 1000
	DefaultMalwareTimeout = 60 * time.Second
	DefaultQueryTimeout   = 30 * time.Second
)

// Storage-specific error codes
const (
	ErrCodeStorageNotInitialized = utils.ErrorCode("STORAGE_NOT_INITIALIZED")
	ErrCodeStorageMismatch       = utils.ErrorCode("STORAGE_MISMATCH")
	ErrCodeInvalidDatabaseURL    = utils.ErrorCode("INVALID_DATABASE_URL")
	ErrCodeInsecureConnection    = utils.ErrorCode("INSECURE_CONNECTION")
	ErrCodeQuorumTooLow          = utils.ErrorCode("QUORUM_TOO_LOW")
)

// Initialize storage constants from topology - MUST be called after InitializeTopology()
func InitializeStorageConstants() error {
	topologyMutex.RLock()
	defer topologyMutex.RUnlock()

	if !topologyInitialized {
		return utils.NewError(ErrCodeTopologyNotReady,
			"topology not initialized - call InitializeTopology() first")
	}

	// Get storage nodes from shared topology
	StorageNodeIDs = make([]int, len(RoleInstances["storage"]))
	copy(StorageNodeIDs, RoleInstances["storage"])

	StorageClusterSize = len(StorageNodeIDs)

	if StorageClusterSize == 0 {
		return utils.NewError(ErrCodeStorageNotInitialized,
			"no storage nodes found in topology - storage is required").
			WithDetail("topology", RoleInstances)
	}

	// Calculate write quorum: Byzantine fault tolerance
	maxFailures := (StorageClusterSize - 1) / 3
	StorageWriteQuorum = 2*maxFailures + 1

	return nil
}

// Enhanced Storage Configuration - topology-aware and security-first
type EnhancedStorageConfig struct {
	// Database connection (mandatory from environment)
	DatabaseURL  string `json:"-"`
	DatabaseHost string `json:"-"`
	DatabasePort int    `json:"-"`

	// Crypto service integration - store key version, not actual keys
	KeyVersion    uint32               `json:"key_version"`
	CryptoService *utils.CryptoService `json:"-"`

	// Connection settings
	SSLMode        string        `json:"ssl_mode"`
	ConnectionPool int           `json:"connection_pool"`
	QueryTimeout   time.Duration `json:"query_timeout"`
	MaxConnections int           `json:"max_connections"`
	IdleTimeout    time.Duration `json:"idle_timeout"`

	// Data management
	RetentionDays   int           `json:"retention_days"`
	BackupEnabled   bool          `json:"backup_enabled"`
	BackupInterval  time.Duration `json:"backup_interval"`
	BackupRetention int           `json:"backup_retention_days"`

	// Clustering and replication (topology-aware)
	StorageNodes     []int `json:"storage_nodes"`     // From RoleInstances["storage"]
	WriteQuorum      int   `json:"write_quorum"`      // Calculated from storage node count
	ReplicationNodes []int `json:"replication_nodes"` // All storage nodes participate
	ShardingEnabled  bool  `json:"sharding_enabled"`  // Enable for 4+ storage nodes

	// Security and threat detection
	EnableStaticRules  bool          `json:"enable_static_rules"`
	DDoSThreshold      int           `json:"ddos_threshold"`
	MalwareTimeout     time.Duration `json:"malware_timeout"`
	EncryptionRequired bool          `json:"encryption_required"`
	AuditLogEnabled    bool          `json:"audit_log_enabled"`

	// Performance and monitoring
	SlowQueryThreshold  time.Duration     `json:"slow_query_threshold"`
	MetricsEnabled      bool              `json:"metrics_enabled"`
	HealthCheckInterval time.Duration     `json:"health_check_interval"`
	ConnectionMetrics   map[string]string `json:"connection_metrics"`

	// Backup and disaster recovery
	DisasterRecoveryEnabled bool   `json:"disaster_recovery_enabled"`
	BackupDestination       string `json:"backup_destination"`
	PointInTimeRecovery     bool   `json:"point_in_time_recovery"`
	BackupCompressionLevel  int    `json:"backup_compression_level"`

	// Security infrastructure
	IPAllowlist *utils.IPAllowlist `json:"-"`
	HTTPClient  *utils.HTTPClient  `json:"-"`
}

// StorageConfigBuilder provides fluent interface for building storage config
type StorageConfigBuilder struct {
	config    *EnhancedStorageConfig
	env       *TopologyEnvironmentProvider
	cryptoSvc *utils.CryptoService
	allowlist *utils.IPAllowlist
	err       error
}

// NewStorageConfigBuilder creates a new builder
func NewStorageConfigBuilder(env *TopologyEnvironmentProvider, cryptoSvc *utils.CryptoService) *StorageConfigBuilder {
	return &StorageConfigBuilder{
		config:    &EnhancedStorageConfig{},
		env:       env,
		cryptoSvc: cryptoSvc,
	}
}

// WithAllowlist sets the IP allowlist
func (b *StorageConfigBuilder) WithAllowlist(allowlist *utils.IPAllowlist) *StorageConfigBuilder {
	if b.err == nil {
		b.allowlist = allowlist
	}
	return b
}

// Build creates the storage configuration
func (b *StorageConfigBuilder) Build(ctx context.Context) (*EnhancedStorageConfig, error) {
	if b.err != nil {
		return nil, b.err
	}

	// Ensure storage constants are initialized
	if err := InitializeStorageConstants(); err != nil {
		return nil, err
	}

	logger := utils.GetLogger()
	logger.InfoContext(ctx, "Loading storage configuration",
		utils.ZapInt("storage_cluster_size", StorageClusterSize),
		utils.ZapInt("write_quorum", StorageWriteQuorum),
		utils.ZapInts("storage_nodes", StorageNodeIDs))

	// Load and validate database URL
	if err := b.loadDatabaseConfig(ctx); err != nil {
		return nil, err
	}

	// Setup crypto integration
	if err := b.setupCrypto(ctx); err != nil {
		return nil, err
	}

	// Load environment-driven configuration
	if err := b.loadEnvironmentConfig(ctx); err != nil {
		return nil, err
	}

	// Setup security infrastructure
	if err := b.setupSecurity(ctx); err != nil {
		return nil, err
	}

	// Validate the complete configuration
	if err := b.validate(ctx); err != nil {
		return nil, err
	}

	logger.InfoContext(ctx, "Storage configuration loaded successfully",
		utils.ZapInt("storage_nodes", len(b.config.StorageNodes)),
		utils.ZapInt("write_quorum", b.config.WriteQuorum),
		utils.ZapBool("sharding_enabled", b.config.ShardingEnabled),
		utils.ZapBool("encryption_required", b.config.EncryptionRequired),
		utils.ZapBool("backup_enabled", b.config.BackupEnabled))

	return b.config, nil
}

// Private builder methods

func (b *StorageConfigBuilder) loadDatabaseConfig(ctx context.Context) error {
	// Database URL - MANDATORY from environment
	databaseURL := b.env.Get("DATABASE_URL")
	if databaseURL == "" {
		return utils.NewValidationError(
			"DATABASE_URL is required - no hardcoded fallbacks allowed for security").
			WithDetail("environment", b.env.Get("ENVIRONMENT"))
	}

	// Parse and validate database URL
	u, err := url.Parse(databaseURL)
	if err != nil {
		return utils.WrapError(err, ErrCodeInvalidDatabaseURL, "invalid database URL format")
	}

	// Validate scheme
	allowedSchemes := map[string]bool{
		"postgresql": true,
		"postgres":   true,
	}
	if !allowedSchemes[u.Scheme] {
		return utils.NewError(ErrCodeInvalidDatabaseURL,
			fmt.Sprintf("invalid database scheme '%s'", u.Scheme)).
			WithDetail("allowed_schemes", []string{"postgresql", "postgres"})
	}

	// Parse host and port using utils
	host, portStr, err := utils.SplitHostPortLoose(u.Host)
	if err != nil {
		return utils.WrapError(err, ErrCodeInvalidDatabaseURL, "invalid database host")
	}

	port := 5432 // PostgreSQL default
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	// Validate port
	if !utils.IsPortValid(port) {
		return utils.NewError(ErrCodeInvalidDatabaseURL,
			fmt.Sprintf("invalid database port: %d", port))
	}

	// Validate host with allowlist if available
	if b.allowlist != nil {
		if err := b.allowlist.IsAddrAllowed(host); err != nil {
			return utils.WrapError(err, utils.CodeIPNotAllowed,
				"database host not allowed by IP allowlist")
		}
	}

	// Validate SSL mode
	query := u.Query()
	sslMode := query.Get("sslmode")
	if sslMode == "" {
		return utils.NewError(ErrCodeInsecureConnection,
			"sslmode must be specified in database URL")
	}

	secureSSLModes := map[string]bool{
		"require":     true,
		"verify-ca":   true,
		"verify-full": true,
	}
	if !secureSSLModes[sslMode] {
		return utils.NewError(ErrCodeInsecureConnection,
			fmt.Sprintf("SSL mode '%s' is not secure", sslMode)).
			WithDetail("allowed_modes", []string{"require", "verify-ca", "verify-full"})
	}

	b.config.DatabaseURL = databaseURL
	b.config.DatabaseHost = host
	b.config.DatabasePort = port
	b.config.SSLMode = sslMode

	return nil
}

func (b *StorageConfigBuilder) setupCrypto(ctx context.Context) error {
	if b.cryptoSvc == nil {
		return utils.NewError(utils.CodeInternal, "crypto service is required")
	}

	// Store crypto service reference
	b.config.CryptoService = b.cryptoSvc

	// Get current key version (don't store actual keys)
	keyVersion, err := b.cryptoSvc.GetKeyVersion()
	if err != nil {
		return utils.WrapError(err, utils.CodeInternal, "failed to get key version")
	}
	b.config.KeyVersion = keyVersion

	return nil
}

func (b *StorageConfigBuilder) loadEnvironmentConfig(ctx context.Context) error {
	environment := b.env.Get("ENVIRONMENT")
	if environment == "" {
		environment = "production"
	}

	isProduction := environment == "production"
	isSecureEnvironment := isProduction || environment == "staging"

	// Connection settings
	b.config.ConnectionPool = getIntFromEnv(b.env, "DB_CONNECTION_POOL", DefaultConnectionPool)
	b.config.QueryTimeout = getDurationFromEnv(b.env, "DB_QUERY_TIMEOUT", DefaultQueryTimeout)
	b.config.MaxConnections = getIntFromEnv(b.env, "DB_MAX_CONNECTIONS", 100)
	b.config.IdleTimeout = getDurationFromEnv(b.env, "DB_IDLE_TIMEOUT", 5*time.Minute)

	// Data management
	b.config.RetentionDays = getIntFromEnv(b.env, "STORAGE_RETENTION_DAYS", DefaultRetentionDays)
	b.config.BackupEnabled = true // Always enable backups
	b.config.BackupInterval = getDurationFromEnv(b.env, "BACKUP_INTERVAL", 6*time.Hour)
	b.config.BackupRetention = getIntFromEnv(b.env, "BACKUP_RETENTION_DAYS", 90)

	// Topology-aware clustering
	b.config.StorageNodes = make([]int, len(StorageNodeIDs))
	copy(b.config.StorageNodes, StorageNodeIDs)
	b.config.WriteQuorum = StorageWriteQuorum
	b.config.ReplicationNodes = make([]int, len(StorageNodeIDs))
	copy(b.config.ReplicationNodes, StorageNodeIDs)
	b.config.ShardingEnabled = StorageClusterSize > 3

	// Security settings
	b.config.EnableStaticRules = true
	b.config.DDoSThreshold = getIntFromEnv(b.env, "DDOS_THRESHOLD", DefaultDDoSThreshold)
	b.config.MalwareTimeout = getDurationFromEnv(b.env, "MALWARE_TIMEOUT", DefaultMalwareTimeout)
	b.config.EncryptionRequired = isSecureEnvironment
	b.config.AuditLogEnabled = isSecureEnvironment

	// Performance monitoring
	b.config.SlowQueryThreshold = getDurationFromEnv(b.env, "SLOW_QUERY_THRESHOLD", 5*time.Second)
	b.config.MetricsEnabled = true
	b.config.HealthCheckInterval = getDurationFromEnv(b.env, "HEALTH_CHECK_INTERVAL", 30*time.Second)
	b.config.ConnectionMetrics = map[string]string{
		"pool_size":    strconv.Itoa(b.config.ConnectionPool),
		"max_idle":     "10",
		"max_lifetime": "1h",
	}

	// Disaster recovery
	b.config.DisasterRecoveryEnabled = isProduction
	b.config.BackupDestination = b.env.Get("BACKUP_DESTINATION")
	b.config.PointInTimeRecovery = isProduction
	b.config.BackupCompressionLevel = getIntFromEnv(b.env, "BACKUP_COMPRESSION_LEVEL", 6)

	return nil
}

func (b *StorageConfigBuilder) setupSecurity(ctx context.Context) error {
	// Setup IP allowlist if not provided
	if b.allowlist == nil {
		allowlistConfig := utils.DefaultIPAllowlistConfig()

		// Configure based on environment
		if b.env.Get("ENVIRONMENT") == "development" {
			allowlistConfig.AllowPrivate = true
			allowlistConfig.AllowLoopback = true
		}

		// Parse allowed CIDRs from environment
		if cidrs := b.env.Get("ALLOWED_CIDRS"); cidrs != "" {
			allowlistConfig.AllowedCIDRs = strings.Split(cidrs, ",")
		}

		allowlist, err := utils.NewIPAllowlist(allowlistConfig)
		if err != nil {
			return utils.WrapError(err, utils.CodeConfigInvalid,
				"failed to create IP allowlist")
		}
		b.allowlist = allowlist
	}

	b.config.IPAllowlist = b.allowlist

	// Create HTTP client for storage operations
	httpClient, err := utils.NewHTTPClientBuilder().
		WithTimeout(b.config.QueryTimeout).
		WithTLSConfig(utils.DefaultHTTPClientConfig().TLSMinVersion,
			utils.DefaultHTTPClientConfig().TLSMaxVersion).
		Build()
	if err != nil {
		return utils.WrapError(err, utils.CodeInternal,
			"failed to create HTTP client")
	}

	b.config.HTTPClient = httpClient

	return nil
}

func (b *StorageConfigBuilder) validate(ctx context.Context) error {
	if b.config == nil {
		return utils.NewValidationError("storage configuration cannot be nil")
	}

	// Ensure storage constants are initialized
	if StorageClusterSize == 0 {
		if err := InitializeStorageConstants(); err != nil {
			return utils.WrapError(err, ErrCodeStorageNotInitialized,
				"storage constants not initialized")
		}
	}

	// Validate topology consistency
	if len(b.config.StorageNodes) != StorageClusterSize {
		return utils.NewError(ErrCodeStorageMismatch,
			fmt.Sprintf("storage node count mismatch: config has %d, topology has %d",
				len(b.config.StorageNodes), StorageClusterSize)).
			WithDetail("config_nodes", b.config.StorageNodes).
			WithDetail("topology_nodes", StorageNodeIDs)
	}

	// Validate storage node IDs match topology
	for i, nodeID := range b.config.StorageNodes {
		if i >= len(StorageNodeIDs) || nodeID != StorageNodeIDs[i] {
			return utils.NewError(ErrCodeStorageMismatch,
				fmt.Sprintf("storage node ID mismatch at index %d: expected %d, got %d",
					i, StorageNodeIDs[i], nodeID))
		}
	}

	// Validate write quorum
	if b.config.WriteQuorum != StorageWriteQuorum {
		return utils.NewError(ErrCodeQuorumTooLow,
			fmt.Sprintf("write quorum must be %d for %d storage nodes",
				StorageWriteQuorum, StorageClusterSize))
	}

	// Validate database URL
	if b.config.DatabaseURL == "" {
		return utils.NewValidationError("database URL cannot be empty")
	}

	// Environment-specific security validations
	environment := b.env.Get("ENVIRONMENT")
	if environment == "production" {
		if !b.config.EncryptionRequired {
			return utils.NewError(utils.CodeConfigInvalid,
				"encryption is mandatory in production environment")
		}

		if !b.config.AuditLogEnabled {
			return utils.NewError(utils.CodeConfigInvalid,
				"audit logging is mandatory in production environment")
		}

		if !b.config.BackupEnabled {
			return utils.NewError(utils.CodeConfigInvalid,
				"backups are mandatory in production environment")
		}

		if !b.config.DisasterRecoveryEnabled {
			return utils.NewError(utils.CodeConfigInvalid,
				"disaster recovery is mandatory in production environment")
		}

		if b.config.SSLMode != "require" && b.config.SSLMode != "verify-full" {
			return utils.NewError(ErrCodeInsecureConnection,
				"SSL mode must be 'require' or 'verify-full' in production")
		}
	}

	// Range validations
	if b.config.RetentionDays <= 0 {
		return utils.NewValidationError("retention days must be greater than 0")
	}

	if b.config.DDoSThreshold <= 0 {
		return utils.NewValidationError("DDoS threshold must be greater than 0")
	}

	if b.config.ConnectionPool <= 0 {
		return utils.NewValidationError("connection pool size must be greater than 0")
	}

	if b.config.MaxConnections < b.config.ConnectionPool {
		return utils.NewValidationError(
			"max connections must be >= connection pool size")
	}

	// Timeout validations
	if b.config.QueryTimeout <= 0 {
		return utils.NewValidationError("query timeout must be greater than 0")
	}

	if b.config.MalwareTimeout <= 0 {
		return utils.NewValidationError("malware timeout must be greater than 0")
	}

	return nil
}

// Public API functions

// LoadStorageConfigForEnvironment loads storage configuration with full validation
func LoadStorageConfigForEnvironment(ctx context.Context, env *TopologyEnvironmentProvider, cryptoSvc *utils.CryptoService) (*EnhancedStorageConfig, error) {
	builder := NewStorageConfigBuilder(env, cryptoSvc)
	return builder.Build(ctx)
}

// LoadStorageConfigWithAllowlist loads storage config with custom IP allowlist
func LoadStorageConfigWithAllowlist(ctx context.Context, env *TopologyEnvironmentProvider, cryptoSvc *utils.CryptoService, allowlist *utils.IPAllowlist) (*EnhancedStorageConfig, error) {
	builder := NewStorageConfigBuilder(env, cryptoSvc).
		WithAllowlist(allowlist)
	return builder.Build(ctx)
}

// EncryptData encrypts data using the storage configuration's crypto service
func (c *EnhancedStorageConfig) EncryptData(ctx context.Context, plaintext []byte) ([]byte, error) {
	if c.CryptoService == nil {
		return nil, utils.NewError(utils.CodeInternal, "crypto service not configured")
	}
	return c.CryptoService.EncryptWithContext(ctx, plaintext)
}

// DecryptData decrypts data using the storage configuration's crypto service
func (c *EnhancedStorageConfig) DecryptData(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if c.CryptoService == nil {
		return nil, utils.NewError(utils.CodeInternal, "crypto service not configured")
	}
	return c.CryptoService.DecryptWithContext(ctx, ciphertext)
}

// ValidateConnection attempts to validate the database connection
func (c *EnhancedStorageConfig) ValidateConnection(ctx context.Context) error {
	if c.IPAllowlist != nil {
		if err := c.IPAllowlist.ValidateBeforeConnect(ctx, c.DatabaseHost); err != nil {
			return utils.WrapError(err, utils.CodeIPNotAllowed,
				"database host failed validation")
		}
	}
	return nil
}

// Close closes resources
func (c *EnhancedStorageConfig) Close() error {
	if c.HTTPClient != nil {
		c.HTTPClient.Close()
	}
	return nil
}

// Build storage service endpoints from topology
func BuildStorageServiceEndpoints(env *TopologyEnvironmentProvider) (map[string]string, error) {
	if len(StorageNodeIDs) == 0 {
		if err := InitializeStorageConstants(); err != nil {
			return nil, err
		}
	}

	endpoints := make(map[string]string)
	namespace := getNamespaceFromEnv()

	// Build endpoints for each storage node
	for i, nodeID := range StorageNodeIDs {
		instance := i + 1

		envKey := fmt.Sprintf("STORAGE_NODE_%d_URL", nodeID)
		if nodeURL := env.Get(envKey); nodeURL != "" {
			endpoints[fmt.Sprintf("storage_node_%d", nodeID)] = nodeURL
		} else {
			defaultURL := fmt.Sprintf("https://storage-node-%d.%s.svc.cluster.local",
				instance, namespace)
			endpoints[fmt.Sprintf("storage_node_%d", nodeID)] = defaultURL
		}
	}

	// Primary storage endpoint
	if primaryURL := env.Get("STORAGE_PRIMARY_URL"); primaryURL != "" {
		endpoints["storage_primary"] = primaryURL
	} else {
		endpoints["storage_primary"] = fmt.Sprintf("https://storage-service.%s.svc.cluster.local",
			namespace)
	}

	return endpoints, nil
}

// Utility functions

func getIntFromEnv(env *TopologyEnvironmentProvider, key string, defaultValue int) int {
	if valueStr := env.Get(key); valueStr != "" {
		if value, err := strconv.Atoi(valueStr); err == nil && value > 0 {
			return value
		}
	}
	return defaultValue
}

func getDurationFromEnv(env *TopologyEnvironmentProvider, key string, defaultValue time.Duration) time.Duration {
	if valueStr := env.Get(key); valueStr != "" {
		if value, err := time.ParseDuration(valueStr); err == nil {
			return value
		}
	}
	return defaultValue
}

// GetStorageConfigSummary returns storage configuration summary for monitoring
func GetStorageConfigSummary() map[string]interface{} {
	return map[string]interface{}{
		"storage_cluster_size":  StorageClusterSize,
		"storage_nodes":         StorageNodeIDs,
		"write_quorum":          StorageWriteQuorum,
		"constants_initialized": StorageClusterSize > 0,
	}
}

// Legacy compatibility - DEPRECATED
// Use LoadStorageConfigForEnvironment instead
func LoadStorageConfig(ctx context.Context, env *TopologyEnvironmentProvider) (*StorageConfig, error) {
	// Ensure constants are initialized
	if err := InitializeStorageConstants(); err != nil {
		return nil, err
	}

	// Basic config without crypto service (legacy mode)
	databaseURL := env.Get("DATABASE_URL")
	if databaseURL == "" {
		return nil, utils.NewValidationError("DATABASE_URL is required")
	}

	return &StorageConfig{
		DatabaseURL:      databaseURL,
		EncryptionKey:    "", // Managed by crypto service
		SSLMode:          "require",
		ConnectionPool:   DefaultConnectionPool,
		QueryTimeout:     DefaultQueryTimeout,
		RetentionDays:    DefaultRetentionDays,
		BackupEnabled:    true,
		ReplicationNodes: StorageNodeIDs,
		ShardingEnabled:  len(StorageNodeIDs) > 3,
		StorageNodes:     StorageNodeIDs,
		DDoSThreshold:    DefaultDDoSThreshold,
		MalwareTimeout:   DefaultMalwareTimeout,
	}, nil
}
