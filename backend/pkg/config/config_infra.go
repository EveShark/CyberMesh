package config

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"backend/pkg/utils"
)

// Operational Configuration - Enhanced with security
type OperationalConfig struct {
	Environment        string        `json:"environment"`
	Region             string        `json:"region"`
	PeerTTL            time.Duration `json:"peer_ttl"`
	DiscoveryInterval  time.Duration `json:"discovery_interval"`
	RateLimit          int           `json:"rate_limit"`
	WebhookURL         string        `json:"webhook_url"`
	BlockchainEndpoint string        `json:"blockchain_endpoint"`
	SecurityMode       string        `json:"security_mode"` // "strict", "normal", "permissive"
	AuditLogLevel      string        `json:"audit_log_level"`
}

// Blockchain Configuration - Enhanced security
type BlockchainConfig struct {
	NetworkID          string        `json:"network_id"`
	ConsensusAlgorithm string        `json:"consensus_algorithm"`
	BlockTime          time.Duration `json:"block_time"`
	MaxBlockSize       int64         `json:"max_block_size"`
	GasLimit           uint64        `json:"gas_limit"`
	ChainDataPath      string        `json:"chain_data_path"`

	// Security settings
	RequireSignedTx  bool  `json:"require_signed_tx"`
	ValidatorNodes   []int `json:"validator_nodes"`
	MinValidators    int   `json:"min_validators"`
	EncryptedStorage bool  `json:"encrypted_storage"`
}

// Note: NodeConfig, NetworkConfig, and MonitoringConfig are now defined in config_node.go

// Load Operational Configuration using utils
func LoadOperationalConfigWithUtils(ctx context.Context, configManager *utils.ConfigManager) (*OperationalConfig, error) {
	logger := utils.GetLogger()

	environment := configManager.GetString("ENVIRONMENT", "production")
	region := configManager.GetString("REGION", "default")

	// Parse durations
	peerTTL := configManager.GetDuration("PEER_TTL", 5*time.Minute)
	discoveryInterval := configManager.GetDuration("DISCOVERY_INTERVAL", 30*time.Second)

	// Rate limiting
	rateLimit := configManager.GetIntRange("RATE_LIMIT", 1000, 1, 1000000)

	// External endpoints
	webhookURL := configManager.GetString("WEBHOOK_URL", "")
	blockchainEndpoint := configManager.GetString("BLOCKCHAIN_ENDPOINT", "")

	// Security mode
	securityMode := configManager.GetString("SECURITY_MODE", "strict")
	validModes := []string{"strict", "normal", "permissive"}
	validMode := false
	for _, mode := range validModes {
		if securityMode == mode {
			validMode = true
			break
		}
	}
	if !validMode {
		return nil, &SecurityError{
			Field:  "SECURITY_MODE",
			Reason: fmt.Sprintf("Invalid security mode '%s', must be one of: %v", securityMode, validModes),
		}
	}

	// Production must use strict mode
	if environment == "production" && securityMode != "strict" {
		return nil, &SecurityError{
			Field:  "SECURITY_MODE",
			Reason: "Production environment requires 'strict' security mode",
		}
	}

	auditLogLevel := configManager.GetString("AUDIT_LOG_LEVEL", "info")

	config := &OperationalConfig{
		Environment:        environment,
		Region:             region,
		PeerTTL:            peerTTL,
		DiscoveryInterval:  discoveryInterval,
		RateLimit:          rateLimit,
		WebhookURL:         webhookURL,
		BlockchainEndpoint: blockchainEndpoint,
		SecurityMode:       securityMode,
		AuditLogLevel:      auditLogLevel,
	}

	logger.InfoContext(ctx, "Operational configuration loaded",
		utils.ZapString("environment", config.Environment),
		utils.ZapString("region", config.Region),
		utils.ZapString("security_mode", config.SecurityMode))

	return config, nil
}

// Load Blockchain Configuration using utils
func LoadBlockchainConfigWithUtils(ctx context.Context, configManager *utils.ConfigManager) (*BlockchainConfig, error) {
	logger := utils.GetLogger()

	networkID := configManager.GetString("BLOCKCHAIN_NETWORK_ID", "cybermesh-mainnet")
	consensusAlgorithm := configManager.GetString("CONSENSUS_ALGORITHM", "pbft")

	// Validate consensus algorithm
	validAlgorithms := []string{"pbft", "raft", "tendermint"}
	validAlgo := false
	for _, algo := range validAlgorithms {
		if consensusAlgorithm == algo {
			validAlgo = true
			break
		}
	}
	if !validAlgo {
		return nil, &SecurityError{
			Field:  "CONSENSUS_ALGORITHM",
			Reason: fmt.Sprintf("Invalid consensus algorithm '%s', must be one of: %v", consensusAlgorithm, validAlgorithms),
		}
	}

	blockTime := configManager.GetDuration("BLOCK_TIME", 5*time.Second)
	maxBlockSize := int64(configManager.GetInt("MAX_BLOCK_SIZE", 1024*1024)) // 1MB default
	gasLimit := uint64(configManager.GetInt("GAS_LIMIT", 10000000))

	chainDataPath := configManager.GetString("CHAIN_DATA_PATH", "/var/lib/cybermesh/chain")

	environment := configManager.GetString("ENVIRONMENT", "production")
	isProduction := environment == "production"

	// Get validator nodes from consensus nodes
	validatorNodes := ConsensusNodes
	if len(validatorNodes) == 0 {
		return nil, &SecurityError{
			Field:  "ValidatorNodes",
			Reason: "No consensus nodes available for blockchain validators",
		}
	}

	minValidators := (len(validatorNodes) / 2) + 1

	config := &BlockchainConfig{
		NetworkID:          networkID,
		ConsensusAlgorithm: consensusAlgorithm,
		BlockTime:          blockTime,
		MaxBlockSize:       maxBlockSize,
		GasLimit:           gasLimit,
		ChainDataPath:      chainDataPath,
		RequireSignedTx:    isProduction,
		ValidatorNodes:     validatorNodes,
		MinValidators:      minValidators,
		EncryptedStorage:   isProduction,
	}

	logger.InfoContext(ctx, "Blockchain configuration loaded",
		utils.ZapString("network_id", config.NetworkID),
		utils.ZapString("consensus", config.ConsensusAlgorithm),
		utils.ZapInt("validators", len(config.ValidatorNodes)))

	return config, nil
}

// Load Node Configuration using utils
func LoadNodeConfigWithUtils(ctx context.Context, nodeID int, configManager *utils.ConfigManager) (*NodeConfig, error) {
	logger := utils.GetLogger()

	// Ensure node constants are initialized
	if err := InitializeNodeConstants(); err != nil {
		return nil, fmt.Errorf("failed to initialize node constants: %w", err)
	}

	// Validate node ID
	nodeType, exists := NodeRoles[nodeID]
	if !exists {
		return nil, &SecurityError{
			Field:  "NodeID",
			Reason: fmt.Sprintf("Node ID %d not found in cluster topology", nodeID),
		}
	}

	instance := getInstanceNumberForNode(nodeID, nodeType)

	version := configManager.GetString("NODE_VERSION", "1.0.0")
	environment := configManager.GetString("ENVIRONMENT", "production")
	region := configManager.GetString("REGION", "default")

	advertisementIP := configManager.GetString("NODE_ADVERTISEMENT_IP", "")
	if advertisementIP == "" {
		// Detect from environment
		if ip := configManager.GetString("EXTERNAL_IP", ""); ip != "" {
			advertisementIP = ip
		} else if ip := configManager.GetString("POD_IP", ""); ip != "" {
			advertisementIP = ip
		} else {
			advertisementIP = "localhost"
		}
	}

	// Get capabilities for this role
	capabilities := getCapabilitiesForRole(nodeType)

	config := &NodeConfig{
		NodeID:          nodeID,
		NodeType:        nodeType,
		NodeInstance:    instance,
		Version:         version,
		Environment:     environment,
		Region:          region,
		AdvertisementIP: advertisementIP,
		Capabilities:    capabilities,
		Metadata:        make(map[string]string),
	}

	logger.InfoContext(ctx, "Node configuration loaded",
		utils.ZapInt("node_id", config.NodeID),
		utils.ZapString("node_type", config.NodeType),
		utils.ZapInt("instance", config.NodeInstance))

	return config, nil
}

// Load Network Configuration using utils
func LoadNetworkConfigWithUtils(ctx context.Context, nodeID int, configManager *utils.ConfigManager) (*NetworkConfig, error) {
	logger := utils.GetLogger()

	// Get node type
	nodeType, exists := NodeRoles[nodeID]
	if !exists {
		return nil, &SecurityError{
			Field:  "NodeID",
			Reason: fmt.Sprintf("Node ID %d not found in topology", nodeID),
		}
	}

	instance := getInstanceNumberForNode(nodeID, nodeType)

	// Port calculation
	portBase := configManager.GetInt("PORT_RANGE_START", DefaultPortBase)
	portRangeSize := configManager.GetInt("PORT_RANGE_SIZE", 100)

	// Port randomization for security
	var portSalt int
	if configManager.GetBool("PORT_RANDOMIZE", false) {
		hashInput := fmt.Sprintf("%s-%d-%s", nodeType, instance, configManager.GetString("ENVIRONMENT", "production"))
		portSalt = int(hashString(hashInput) % 1000)
	}

	roleOffset := getRoleOffset(nodeType)
	instanceOffset := instance - 1

	nodePort := portBase + portSalt + (roleOffset * portRangeSize) + instanceOffset
	apiPort := (portBase + 1000) + portSalt + (roleOffset * portRangeSize) + instanceOffset
	metricsPort := (portBase + 2000) + portSalt + (roleOffset * portRangeSize) + instanceOffset

	// Get advertisement IP
	advertisementIP := configManager.GetString("NODE_ADVERTISEMENT_IP", "localhost")

	// Build port allocation
	portAllocation := map[string]int{
		"node":    nodePort,
		"api":     apiPort,
		"metrics": metricsPort,
		"health":  nodePort + 100,
	}

	config := &NetworkConfig{
		NodePort:        nodePort,
		APIPort:         apiPort,
		MetricsPort:     metricsPort,
		AdvertisementIP: advertisementIP,
		PortAllocation:  portAllocation,
	}

	logger.InfoContext(ctx, "Network configuration loaded",
		utils.ZapInt("node_id", nodeID),
		utils.ZapInt("node_port", config.NodePort),
		utils.ZapInt("api_port", config.APIPort))

	return config, nil
}

// Load Monitoring Configuration using utils
func LoadMonitoringConfigWithUtils(ctx context.Context, configManager *utils.ConfigManager, cryptoService *utils.CryptoService) (*MonitoringConfig, error) {
	logger := utils.GetLogger()

	metricsPort := configManager.GetInt("METRICS_PORT", DefaultMetricsBase)
	metricsPath := configManager.GetString("METRICS_PATH", "/metrics")
	logLevel := configManager.GetString("LOG_LEVEL", "info")

	environment := configManager.GetString("ENVIRONMENT", "production")
	isProduction := environment == "production"

	auditEnabled := configManager.GetBool("AUDIT_ENABLED", isProduction)
	alertsEnabled := configManager.GetBool("ALERTS_ENABLED", isProduction)

	// Generate auth token for metrics endpoint using crypto/rand (FIXED)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate metrics auth token: %w", err)
	}
	authToken := hex.EncodeToString(tokenBytes)

	// Get collector nodes (typically control and monitoring nodes)
	collectorNodes := make([]int, 0)
	for role, instances := range RoleInstances {
		if role == "control" || role == "monitoring" {
			collectorNodes = append(collectorNodes, instances...)
		}
	}

	if len(collectorNodes) == 0 {
		// Fallback to all nodes
		for nodeID := range NodeRoles {
			collectorNodes = append(collectorNodes, nodeID)
		}
	}

	config := &MonitoringConfig{
		MetricsPort:    metricsPort,
		MetricsPath:    metricsPath,
		AuthToken:      authToken,
		LogLevel:       logLevel,
		AuditEnabled:   auditEnabled,
		AlertsEnabled:  alertsEnabled,
		CollectorNodes: collectorNodes,
		SecurityMetrics: map[string]string{
			"auth_failures":     "security_auth_failures_total",
			"rate_limit_hits":   "security_rate_limit_hits_total",
			"invalid_requests":  "security_invalid_requests_total",
			"crypto_operations": "security_crypto_operations_total",
		},
	}

	logger.InfoContext(ctx, "Monitoring configuration loaded",
		utils.ZapInt("metrics_port", config.MetricsPort),
		utils.ZapString("log_level", config.LogLevel),
		utils.ZapInt("collectors", len(config.CollectorNodes)))

	return config, nil
}

// Validate External Service Security using utils
func ValidateExternalServiceSecurityWithUtils(ctx context.Context, service ExternalAIService, configManager *utils.ConfigManager) error {
	environment := configManager.GetString("ENVIRONMENT", "production")

	// HTTPS mandatory in production
	if environment == "production" && !strings.HasPrefix(service.Endpoint, "https://") {
		return &SecurityError{
			Field:  "ExternalAIService.Endpoint",
			Reason: "HTTPS required for external AI services in production",
			Context: map[string]interface{}{
				"service":  service.Name,
				"endpoint": service.Endpoint,
			},
		}
	}

	// Validate allowed CIDR ranges
	if len(service.AllowedCIDRs) == 0 {
		return &SecurityError{
			Field:  "ExternalAIService.AllowedCIDRs",
			Reason: "Network ACLs required for external services",
		}
	}

	for _, cidr := range service.AllowedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return &SecurityError{
				Field:  "ExternalAIService.AllowedCIDRs",
				Reason: fmt.Sprintf("Invalid CIDR format: %s", cidr),
			}
		}
	}

	// Rate limiting mandatory
	if service.RateLimitRPS <= 0 {
		return &SecurityError{
			Field:  "ExternalAIService.RateLimitRPS",
			Reason: "Rate limiting required for external services",
		}
	}

	// Auth token validation
	if len(service.AuthToken) < 32 {
		return &SecurityError{
			Field:  "ExternalAIService.AuthToken",
			Reason: "Auth token must be at least 32 characters for external services",
		}
	}

	return nil
}

// Build Service Endpoints from Topology using utils
func BuildTopologyServiceEndpointsWithUtils(ctx context.Context, configManager *utils.ConfigManager, tokenManager *SecureTokenManager) (map[string]*ServiceEndpoint, error) {
	endpoints := make(map[string]*ServiceEndpoint)

	environment := configManager.GetString("ENVIRONMENT", "production")

	// Build endpoints for each service role using shared RoleInstances
	for role, instances := range RoleInstances {
		if len(instances) == 0 {
			continue
		}

		// Use first instance as primary endpoint
		primaryNodeID := instances[0]
		instance := getInstanceNumberForNode(primaryNodeID, role)

		// Check for explicit service URL in environment
		envKey := strings.ToUpper(role) + "_SERVICE_URL"
		serviceURL := configManager.GetString(envKey, "")

		if serviceURL == "" {
			// Generate default internal endpoint
			namespace := configManager.GetString("KUBERNETES_NAMESPACE", "cybermesh-system")
			serviceURL = fmt.Sprintf("https://%s-service-%d.%s.svc.cluster.local", role, instance, namespace)
		}

		// Generate auth token for this service
		authToken, err := tokenManager.GetOrGenerateToken(ctx, fmt.Sprintf("service_%s", role), 64)
		if err != nil {
			return nil, fmt.Errorf("failed to generate token for service %s: %w", role, err)
		}

		endpoints[role] = &ServiceEndpoint{
			URL:           serviceURL,
			AuthToken:     authToken,
			TLSEnabled:    environment != "development",
			Timeout:       30 * time.Second,
			RetryAttempts: 3,
			HealthCheck:   fmt.Sprintf("%s/health", serviceURL),
			Headers: map[string]string{
				"X-Service-Role": role,
				"X-Node-ID":      strconv.Itoa(primaryNodeID),
				"X-Auth-Token":   authToken,
			},
		}
	}

	return endpoints, nil
}

// Utility functions

// Note: getRoleOffset, getCapabilitiesForRole, and hashString are now defined in config_node.go
