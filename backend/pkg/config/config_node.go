package config

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"backend/pkg/utils"
)

// Dynamic configuration constants - derived from topology
var (
	// Node boundaries (calculated from topology)
	MinNodeID  int
	MaxNodeID  int
	TotalNodes int

	// Role-based node lists (populated from RoleInstances)
	GatewayNodes    []int
	StorageNodes    []int
	ControlNodes    []int
	AINodes         []int
	ProcessingNodes []int

	// Connection defaults
	DefaultSSLMode     = "require"
	DefaultConnTimeout = 30 * time.Second
)

// Note: StorageClusterSize is declared in config_storage.go with other storage-specific variables

// Validation patterns
var (
	NodeNamePattern    = `^[a-zA-Z]+-node-\d+$`
	EnvironmentPattern = `^(development|staging|production)$`
	RegionPattern      = `^[a-zA-Z0-9\-_]+$`
	URLPattern         = regexp.MustCompile(`^https?://[^\s/$.?#].[^\s]*$`)
)

// Error codes for configuration
const (
	ErrCodeNodeNotFound     = utils.ErrorCode("NODE_NOT_FOUND")
	ErrCodeNodeOutOfRange   = utils.ErrorCode("NODE_OUT_OF_RANGE")
	ErrCodeInvalidRole      = utils.ErrorCode("INVALID_ROLE")
	ErrCodeTopologyNotReady = utils.ErrorCode("TOPOLOGY_NOT_READY")
	ErrCodeInvalidPort      = utils.ErrorCode("INVALID_PORT")
	ErrCodeInvalidAddress   = utils.ErrorCode("INVALID_ADDRESS")
)

// Initialize node constants from topology - MUST be called after InitializeTopology()
func InitializeNodeConstants() error {
	topologyMutex.RLock()
	defer topologyMutex.RUnlock()

	if !topologyInitialized {
		return utils.NewError(ErrCodeTopologyNotReady,
			"topology not initialized - call InitializeTopology() first")
	}

	TotalNodes = ClusterSize

	// Find min/max node IDs from topology
	MinNodeID = ClusterSize + 1
	MaxNodeID = 0

	for nodeID := range NodeRoles {
		if nodeID < MinNodeID {
			MinNodeID = nodeID
		}
		if nodeID > MaxNodeID {
			MaxNodeID = nodeID
		}
	}

	// Build role-based node lists from shared globals
	GatewayNodes = make([]int, len(RoleInstances["gateway"]))
	copy(GatewayNodes, RoleInstances["gateway"])

	StorageNodes = make([]int, len(RoleInstances["storage"]))
	copy(StorageNodes, RoleInstances["storage"])

	ControlNodes = make([]int, len(RoleInstances["control"]))
	copy(ControlNodes, RoleInstances["control"])

	AINodes = make([]int, len(RoleInstances["ai"]))
	copy(AINodes, RoleInstances["ai"])

	ProcessingNodes = make([]int, len(RoleInstances["processing"]))
	copy(ProcessingNodes, RoleInstances["processing"])

	StorageClusterSize = len(StorageNodes)

	return nil
}

// Dynamic Node Configuration
type NodeConfig struct {
	// Core node identification (from topology discovery)
	NodeID       int    `json:"node_id"`
	NodeType     string `json:"node_type"`
	NodeInstance int    `json:"node_instance"`
	Version      string `json:"version"`

	// Environment configuration
	Environment     string            `json:"environment"`
	Region          string            `json:"region"`
	AdvertisementIP string            `json:"advertisement_ip"`
	Capabilities    []string          `json:"capabilities"`
	Metadata        map[string]string `json:"metadata"`

	// Dynamic network configuration
	NodePort      int `json:"node_port"`
	APIPort       int `json:"api_port"`
	WebSocketPort int `json:"websocket_port"`
	MetricsPort   int `json:"metrics_port"`

	// Topology-aware mesh configuration
	MeshConfig *MeshConfig `json:"mesh_config"`

	// Security configuration
	IPAllowlist *utils.IPAllowlist `json:"-"`
	HTTPClient  *utils.HTTPClient  `json:"-"`

	// Runtime state
	storageNodes    []StorageNodeConfig
	peerConnections map[int]PeerInfo
}

type MeshConfig struct {
	MeshNodes      []string         `json:"mesh_nodes"`
	BootstrapNodes []string         `json:"bootstrap_nodes"`
	PeerNodes      []string         `json:"peer_nodes"`
	RoleTopology   map[string][]int `json:"role_topology"`
	ServicePeers   map[string][]int `json:"service_peers"`
}

type StorageNodeConfig struct {
	ID       int    `json:"id"`
	Role     string `json:"role"`
	Instance int    `json:"instance"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Secure   bool   `json:"secure"`
}

type PeerInfo struct {
	NodeID  int    `json:"node_id"`
	Role    string `json:"role"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Secure  bool   `json:"secure"`
}

type NetworkConfig struct {
	NodePort        int            `json:"node_port"`
	APIPort         int            `json:"api_port"`
	MetricsPort     int            `json:"metrics_port"`
	AdvertisementIP string         `json:"advertisement_ip"`
	PeerNodes       []PeerInfo     `json:"peer_nodes"`
	BootstrapPeers  []PeerInfo     `json:"bootstrap_peers"`
	PortAllocation  map[string]int `json:"port_allocation"`
}

type StorageConfig struct {
	DatabaseURL      string        `json:"-"`
	EncryptionKey    string        `json:"-"`
	SSLMode          string        `json:"ssl_mode"`
	ConnectionPool   int           `json:"connection_pool"`
	QueryTimeout     time.Duration `json:"query_timeout"`
	RetentionDays    int           `json:"retention_days"`
	BackupEnabled    bool          `json:"backup_enabled"`
	ReplicationNodes []int         `json:"replication_nodes"`
	ShardingEnabled  bool          `json:"sharding_enabled"`
	StorageNodes     []int         `json:"storage_nodes"`
	DDoSThreshold    int           `json:"ddos_threshold"`
	MalwareTimeout   time.Duration `json:"malware_timeout"`
}

type MonitoringConfig struct {
	MetricsPort     int               `json:"metrics_port"`
	MetricsPath     string            `json:"metrics_path"`
	AuthToken       string            `json:"-"`
	LogLevel        string            `json:"log_level"`
	AuditEnabled    bool              `json:"audit_enabled"`
	AlertsEnabled   bool              `json:"alerts_enabled"`
	CollectorNodes  []int             `json:"collector_nodes"`
	SecurityMetrics map[string]string `json:"security_metrics"`
}

// Network Config Builder - topology-aware with security
type TopologyAwareNetworkBuilder struct {
	env       *TopologyEnvironmentProvider
	allowlist *utils.IPAllowlist
}

func NewTopologyAwareNetworkBuilder(env *TopologyEnvironmentProvider) (*TopologyAwareNetworkBuilder, error) {
	// Initialize IP allowlist with secure defaults
	allowlistConfig := utils.DefaultIPAllowlistConfig()

	// Parse allowed CIDRs from environment
	if cidrs := env.Get("ALLOWED_CIDRS"); cidrs != "" {
		allowlistConfig.AllowedCIDRs = strings.Split(cidrs, ",")
	}

	// Configure based on environment
	if env.Get("ENVIRONMENT") == "development" {
		allowlistConfig.AllowPrivate = true
		allowlistConfig.AllowLoopback = true
	}

	allowlist, err := utils.NewIPAllowlist(allowlistConfig)
	if err != nil {
		return nil, utils.WrapError(err, utils.CodeConfigInvalid, "failed to create IP allowlist")
	}

	return &TopologyAwareNetworkBuilder{
		env:       env,
		allowlist: allowlist,
	}, nil
}

func (builder *TopologyAwareNetworkBuilder) BuildMeshNodes() []string {
	nodes := make([]string, 0, TotalNodes)

	for nodeID, role := range NodeRoles {
		instance := getInstanceNumberForNode(nodeID, role)
		nodeName := fmt.Sprintf("%s-node-%d", role, instance)
		nodes = append(nodes, nodeName)
	}

	return nodes
}

func (builder *TopologyAwareNetworkBuilder) BuildBootstrapNodes() []string {
	bootstrapAddresses := make([]string, 0)

	for _, nodeID := range BootstrapPeers {
		if role, exists := NodeRoles[nodeID]; exists {
			instance := getInstanceNumberForNode(nodeID, role)
			serviceName := fmt.Sprintf("%s-node-%d", role, instance)
			namespace := getNamespaceFromEnv()
			fqdn := fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace)
			bootstrapAddresses = append(bootstrapAddresses, fqdn)
		}
	}

	return bootstrapAddresses
}

func (builder *TopologyAwareNetworkBuilder) BuildServiceEndpoints() map[string]string {
	endpoints := make(map[string]string)

	for role, instances := range RoleInstances {
		if len(instances) == 0 {
			continue
		}

		primaryNodeID := instances[0]
		instance := getInstanceNumberForNode(primaryNodeID, role)

		envKey := strings.ToUpper(role) + "_SERVICE_URL"
		if serviceURL := builder.env.Get(envKey); serviceURL != "" {
			endpoints[role] = serviceURL
		} else {
			serviceName := fmt.Sprintf("%s-service-%d", role, instance)
			namespace := getNamespaceFromEnv()
			defaultURL := fmt.Sprintf("https://%s.%s.svc.cluster.local", serviceName, namespace)
			endpoints[role] = defaultURL
		}
	}

	return endpoints
}

func (builder *TopologyAwareNetworkBuilder) BuildStorageNodes() ([]StorageNodeConfig, error) {
	storageNodes := make([]StorageNodeConfig, 0, len(StorageNodes))

	// Check for unified database URL first
	databaseURL := builder.env.Get("DATABASE_URL")
	if databaseURL != "" {
		host, port, secure, err := parseUnifiedDatabaseURL(databaseURL)
		if err != nil {
			return nil, utils.WrapError(err, ErrCodeInvalidAddress, "invalid database URL")
		}

		// Validate host address
		if err := validateHostAddress(host, builder.allowlist); err != nil {
			return nil, utils.WrapError(err, ErrCodeInvalidAddress, "database host not allowed")
		}

		// Validate port
		if !utils.IsPortValid(port) {
			return nil, utils.NewError(ErrCodeInvalidPort,
				fmt.Sprintf("invalid database port: %d", port))
		}

		for _, nodeID := range StorageNodes {
			instance := getInstanceNumberForNode(nodeID, "storage")

			storageNodes = append(storageNodes, StorageNodeConfig{
				ID:       nodeID,
				Role:     "storage",
				Instance: instance,
				Host:     host,
				Port:     port,
				Secure:   secure,
			})
		}

		return storageNodes, nil
	}

	// Fallback: individual storage node configuration
	for _, nodeID := range StorageNodes {
		instance := getInstanceNumberForNode(nodeID, "storage")

		hostKey := fmt.Sprintf("STORAGE_NODE_%d_HOST", nodeID)
		portKey := fmt.Sprintf("STORAGE_NODE_%d_PORT", nodeID)

		host := builder.env.Get(hostKey)
		if host == "" {
			host = fmt.Sprintf("storage-node-%d.%s.svc.cluster.local",
				instance, getNamespaceFromEnv())
		}

		// Validate host
		if err := validateHostAddress(host, builder.allowlist); err != nil {
			return nil, utils.WrapErrorf(err, ErrCodeInvalidAddress,
				"storage node %d host not allowed", nodeID)
		}

		port := 5432
		if portStr := builder.env.Get(portKey); portStr != "" {
			if p, err := strconv.Atoi(portStr); err == nil {
				port = p
			}
		}

		if !utils.IsPortValid(port) {
			return nil, utils.NewError(ErrCodeInvalidPort,
				fmt.Sprintf("invalid port for storage node %d: %d", nodeID, port))
		}

		storageNodes = append(storageNodes, StorageNodeConfig{
			ID:       nodeID,
			Role:     "storage",
			Instance: instance,
			Host:     host,
			Port:     port,
			Secure:   true,
		})
	}

	return storageNodes, nil
}

func (builder *TopologyAwareNetworkBuilder) BuildPeerConnections(nodeID int) (map[int]PeerInfo, error) {
	connections := make(map[int]PeerInfo)

	portBase, _ := strconv.Atoi(builder.env.Get("PORT_RANGE_START"))
	if portBase == 0 {
		portBase = 8000
	}

	portRangeSize, _ := strconv.Atoi(builder.env.Get("PORT_RANGE_SIZE"))
	if portRangeSize == 0 {
		portRangeSize = 100
	}

	for peerID, peerRole := range NodeRoles {
		if peerID == nodeID {
			continue
		}

		peerInstance := getInstanceNumberForNode(peerID, peerRole)

		// Calculate peer port using utils
		roleOffset := getRoleOffset(peerRole)
		instanceOffset := peerInstance - 1

		peerPort, err := utils.PortFromRange(portBase, portRangeSize,
			roleOffset*portRangeSize+instanceOffset)
		if err != nil {
			return nil, utils.WrapErrorf(err, ErrCodeInvalidPort,
				"failed to calculate port for peer %d", peerID)
		}

		connections[peerID] = PeerInfo{
			NodeID:  peerID,
			Role:    peerRole,
			Address: fmt.Sprintf("%s-node-%d", peerRole, peerInstance),
			Port:    peerPort,
			Secure:  true,
		}
	}

	return connections, nil
}

// Load node configuration using shared topology
func (loader *FlexibleConfigLoader) LoadNodeConfig(ctx context.Context, nodeID int) (*NodeConfig, error) {
	// Ensure node constants are initialized
	if err := InitializeNodeConstants(); err != nil {
		return nil, err
	}

	// Validate node ID against topology
	if err := validateNodeID(nodeID); err != nil {
		return nil, err
	}

	nodeType, exists := NodeRoles[nodeID]
	if !exists {
		return nil, utils.NewError(ErrCodeNodeNotFound,
			fmt.Sprintf("node ID %d not found in topology", nodeID)).
			WithDetail("node_id", nodeID)
	}

	instance := getInstanceNumberForNode(nodeID, nodeType)

	environment := loader.env.Get("ENVIRONMENT")
	if environment == "" {
		environment = "production"
	}

	region := loader.env.Get("REGION")
	if region == "" {
		region = "default"
	}

	// Build network configuration with security
	networkBuilder, err := NewTopologyAwareNetworkBuilder(loader.env)
	if err != nil {
		return nil, err
	}

	advertisementIP := loader.env.Get("NODE_ADVERTISEMENT_IP")
	if advertisementIP == "" {
		advertisementIP = detectAdvertisementIP()
	}

	// Validate advertisement IP
	if err := validateHostAddress(advertisementIP, networkBuilder.allowlist); err != nil {
		return nil, utils.WrapError(err, ErrCodeInvalidAddress,
			"advertisement IP not allowed")
	}

	capabilities := getCapabilitiesForRole(nodeType)

	// Build storage nodes with validation
	storageNodes, err := networkBuilder.BuildStorageNodes()
	if err != nil {
		return nil, err
	}

	// Build peer connections with port validation
	peerConnections, err := networkBuilder.BuildPeerConnections(nodeID)
	if err != nil {
		return nil, err
	}

	// Create HTTP client with secure defaults
	httpClient, err := utils.NewHTTPClientBuilder().
		WithTimeout(DefaultConnTimeout).
		WithTLSConfig(utils.DefaultHTTPClientConfig().TLSMinVersion,
			utils.DefaultHTTPClientConfig().TLSMaxVersion).
		Build()
	if err != nil {
		return nil, utils.WrapError(err, utils.CodeInternal,
			"failed to create HTTP client")
	}

	nodeConfig := &NodeConfig{
		NodeID:          nodeID,
		NodeType:        nodeType,
		NodeInstance:    instance,
		Version:         loader.env.Get("NODE_VERSION"),
		Environment:     environment,
		Region:          region,
		AdvertisementIP: advertisementIP,
		Capabilities:    capabilities,
		Metadata:        make(map[string]string),
		IPAllowlist:     networkBuilder.allowlist,
		HTTPClient:      httpClient,
		storageNodes:    storageNodes,
		peerConnections: peerConnections,
	}

	meshConfig := &MeshConfig{
		MeshNodes:      networkBuilder.BuildMeshNodes(),
		BootstrapNodes: networkBuilder.BuildBootstrapNodes(),
		PeerNodes:      nodeConfig.GetPeerNodes(),
		RoleTopology:   RoleInstances,
		ServicePeers:   make(map[string][]int),
	}

	nodeConfig.MeshConfig = meshConfig

	return nodeConfig, nil
}

// Load network configuration with proper port validation
func (loader *FlexibleConfigLoader) LoadNetworkConfig(ctx context.Context, nodeConfig *NodeConfig) (*NetworkConfig, error) {
	portBase, _ := strconv.Atoi(loader.env.Get("PORT_RANGE_START"))
	if portBase == 0 {
		portBase = 8000
	}

	portRangeSize, _ := strconv.Atoi(loader.env.Get("PORT_RANGE_SIZE"))
	if portRangeSize == 0 {
		portRangeSize = 100
	}

	// Port randomization for security
	var portSalt int
	if loader.env.Get("PORT_RANDOMIZE") == "true" {
		hashInput := fmt.Sprintf("%s-%d-%s", nodeConfig.NodeType,
			nodeConfig.NodeInstance, nodeConfig.Environment)
		portSalt = int(hashString(hashInput) % 1000)
	}

	roleOffset := getRoleOffset(nodeConfig.NodeType)
	instanceOffset := nodeConfig.NodeInstance - 1

	// Calculate ports using utils for validation
	nodePort, err := utils.PortFromRange(portBase+portSalt, portRangeSize,
		roleOffset*portRangeSize+instanceOffset)
	if err != nil {
		return nil, utils.WrapError(err, ErrCodeInvalidPort, "invalid node port")
	}

	apiPort, err := utils.PortFromRange(portBase+1000+portSalt, portRangeSize,
		roleOffset*portRangeSize+instanceOffset)
	if err != nil {
		return nil, utils.WrapError(err, ErrCodeInvalidPort, "invalid API port")
	}

	metricsPort, err := utils.PortFromRange(portBase+2000+portSalt, portRangeSize,
		roleOffset*portRangeSize+instanceOffset)
	if err != nil {
		return nil, utils.WrapError(err, ErrCodeInvalidPort, "invalid metrics port")
	}

	// Build peer information using shared topology
	peerNodes := make([]PeerInfo, 0)
	bootstrapPeers := make([]PeerInfo, 0)

	for peerID, peerRole := range NodeRoles {
		if peerID == nodeConfig.NodeID {
			continue
		}

		peerInstance := getInstanceNumberForNode(peerID, peerRole)
		peerRoleOffset := getRoleOffset(peerRole)

		peerPort, err := utils.PortFromRange(portBase+portSalt, portRangeSize,
			peerRoleOffset*portRangeSize+(peerInstance-1))
		if err != nil {
			return nil, utils.WrapErrorf(err, ErrCodeInvalidPort,
				"invalid port for peer %d", peerID)
		}

		peerInfo := PeerInfo{
			NodeID:  peerID,
			Role:    peerRole,
			Address: fmt.Sprintf("%s-node-%d", peerRole, peerInstance),
			Port:    peerPort,
			Secure:  true,
		}

		peerNodes = append(peerNodes, peerInfo)

		for _, bootstrapID := range BootstrapPeers {
			if bootstrapID == peerID {
				bootstrapPeers = append(bootstrapPeers, peerInfo)
				break
			}
		}
	}

	return &NetworkConfig{
		NodePort:        nodePort,
		APIPort:         apiPort,
		MetricsPort:     metricsPort,
		AdvertisementIP: nodeConfig.AdvertisementIP,
		PeerNodes:       peerNodes,
		BootstrapPeers:  bootstrapPeers,
		PortAllocation:  buildPortAllocation(nodeConfig, portBase+portSalt, portRangeSize),
	}, nil
}

// NODE CONFIG CONVENIENCE METHODS WITH SECURITY

func (c *NodeConfig) ConnectToPeer(ctx context.Context, peerID int) (net.Conn, error) {
	peer, exists := c.peerConnections[peerID]
	if !exists {
		return nil, utils.NewError(ErrCodeNodeNotFound,
			fmt.Sprintf("peer %d not found", peerID))
	}

	address := fmt.Sprintf("%s:%d", peer.Address, peer.Port)
	return utils.DialContextWithTimeout(ctx, "tcp", address,
		DefaultConnTimeout, c.IPAllowlist)
}

func (c *NodeConfig) GetServiceEndpoint(serviceName string) string {
	if instances, exists := RoleInstances[serviceName]; exists && len(instances) > 0 {
		nodeID := instances[0]
		instance := getInstanceNumberForNode(nodeID, serviceName)
		namespace := getNamespaceFromEnv()
		return fmt.Sprintf("https://%s-service-%d.%s.svc.cluster.local",
			serviceName, instance, namespace)
	}
	return ""
}

func (c *NodeConfig) GetNodeType() string {
	return c.NodeType
}

func (c *NodeConfig) IsAINode() bool {
	return c.NodeType == "ai"
}

func (c *NodeConfig) IsProcessingNode() bool {
	return c.NodeType == "processing"
}

func (c *NodeConfig) IsGatewayNode() bool {
	return c.NodeType == "gateway"
}

func (c *NodeConfig) IsStorageNode() bool {
	return c.NodeType == "storage"
}

func (c *NodeConfig) IsControlNode() bool {
	return c.NodeType == "control"
}

func (c *NodeConfig) GetPeerNodes() []string {
	if c.MeshConfig == nil {
		return []string{}
	}
	return c.MeshConfig.PeerNodes
}

func (c *NodeConfig) GetStorageNodes() []StorageNodeConfig {
	cp := make([]StorageNodeConfig, len(c.storageNodes))
	copy(cp, c.storageNodes)
	return cp
}

func (c *NodeConfig) GetRolePeers(role string) []int {
	if instances, exists := RoleInstances[role]; exists {
		peers := make([]int, 0, len(instances))
		for _, nodeID := range instances {
			if nodeID != c.NodeID {
				peers = append(peers, nodeID)
			}
		}
		return peers
	}
	return []int{}
}

func (c *NodeConfig) Close() {
	if c.HTTPClient != nil {
		c.HTTPClient.Close()
	}
}

// VALIDATION FUNCTIONS

func validateNodeID(nodeID int) error {
	if nodeID < MinNodeID || nodeID > MaxNodeID {
		return utils.NewError(ErrCodeNodeOutOfRange,
			fmt.Sprintf("node ID %d out of range [%d-%d]", nodeID, MinNodeID, MaxNodeID)).
			WithDetail("cluster_size", TotalNodes).
			WithDetail("min_id", MinNodeID).
			WithDetail("max_id", MaxNodeID)
	}

	if _, exists := NodeRoles[nodeID]; !exists {
		return utils.NewError(ErrCodeNodeNotFound,
			fmt.Sprintf("node ID %d not found in cluster topology", nodeID)).
			WithDetail("node_id", nodeID)
	}

	return nil
}

func validateHostAddress(host string, allowlist *utils.IPAllowlist) error {
	// Try parsing as IP first
	if ip := net.ParseIP(host); ip != nil {
		if allowlist != nil && !allowlist.IsAllowed(ip) {
			return utils.NewError(utils.CodeIPNotAllowed,
				fmt.Sprintf("IP address not allowed: %s", host))
		}
		return nil
	}

	// It's a hostname - validate if allowlist requires it
	if allowlist != nil {
		if err := allowlist.IsAddrAllowed(host); err != nil {
			return err
		}
	}

	return nil
}

// UTILITY FUNCTIONS

func getInstanceNumberForNode(nodeID int, role string) int {
	instances, exists := RoleInstances[role]
	if !exists {
		return 1
	}

	for i, id := range instances {
		if id == nodeID {
			return i + 1
		}
	}

	return 1
}

func getRoleOffset(role string) int {
	offsets := map[string]int{
		"ai":         0,
		"processing": 1,
		"gateway":    2,
		"storage":    3,
		"control":    4,
	}

	if offset, exists := offsets[role]; exists {
		return offset
	}

	return len(offsets)
}

func getCapabilitiesForRole(role string) []string {
	capabilities := map[string][]string{
		"ai":         {"inference", "training", "ml_pipeline"},
		"processing": {"data_processing", "analytics", "transformation"},
		"gateway":    {"routing", "load_balancing", "api_gateway"},
		"storage":    {"persistence", "backup", "replication"},
		"control":    {"monitoring", "orchestration", "ui"},
	}

	if caps, exists := capabilities[role]; exists {
		return caps
	}

	return []string{"generic"}
}

func parseUnifiedDatabaseURL(databaseURL string) (host string, port int, secure bool, err error) {
	u, parseErr := url.Parse(databaseURL)
	if parseErr != nil {
		return "", 0, false, parseErr
	}

	// Use utils for proper host/port splitting
	host, portStr, splitErr := utils.SplitHostPortLoose(u.Host)
	if splitErr != nil {
		return "", 0, false, splitErr
	}

	port = 5432 // PostgreSQL default
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	// Validate port
	if !utils.IsPortValid(port) {
		return "", 0, false, fmt.Errorf("invalid port in database URL: %d", port)
	}

	secure = strings.Contains(databaseURL, "sslmode=require") ||
		strings.Contains(databaseURL, "sslmode=verify-full") ||
		u.Scheme == "postgresql+ssl"

	return host, port, secure, nil
}

func getNamespaceFromEnv() string {
	if ns := os.Getenv("KUBERNETES_NAMESPACE"); ns != "" {
		return ns
	}
	if ns := os.Getenv("NAMESPACE"); ns != "" {
		return ns
	}
	return "cybermesh-system"
}

func detectAdvertisementIP() string {
	if ip := os.Getenv("EXTERNAL_IP"); ip != "" {
		return ip
	}
	if ip := os.Getenv("POD_IP"); ip != "" {
		return ip
	}
	return "127.0.0.1"
}

func buildPortAllocation(nodeConfig *NodeConfig, portBase, portRangeSize int) map[string]int {
	allocation := make(map[string]int)
	roleOffset := getRoleOffset(nodeConfig.NodeType)
	instanceOffset := nodeConfig.NodeInstance - 1

	basePort, err := utils.PortFromRange(portBase, portRangeSize,
		roleOffset*portRangeSize+instanceOffset)
	if err != nil {
		// Fallback to basic calculation if range fails
		basePort = portBase + (roleOffset * portRangeSize) + instanceOffset
	}

	// Standard service ports
	allocation["node"] = basePort
	allocation["api"] = basePort + 1000
	allocation["metrics"] = basePort + 2000
	allocation["health"] = basePort + 3000

	// Role-specific ports
	switch nodeConfig.NodeType {
	case "ai":
		allocation["inference"] = basePort + 10
		allocation["training"] = basePort + 11
	case "processing":
		allocation["processor"] = basePort + 10
		allocation["analytics"] = basePort + 11
	case "gateway":
		allocation["external"] = basePort + 10
		allocation["internal"] = basePort + 11
	case "storage":
		allocation["database"] = basePort + 10
		allocation["backup"] = basePort + 11
	case "control":
		allocation["dashboard"] = basePort + 10
		allocation["admin"] = basePort + 11
	}

	return allocation
}

// Helper function for string hashing (used in port randomization)
func hashString(s string) uint32 {
	h := uint32(0)
	for i := 0; i < len(s); i++ {
		h = h*31 + uint32(s[i])
	}
	return h
}

// isPortAvailable checks if a port is available for binding
func isPortAvailable(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}
