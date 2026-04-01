package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds runtime configuration for the enforcement agent.
type Config struct {
	Kafka struct {
		Brokers                      []string
		Topic                        string
		GroupID                      string
		ConsumptionMode              string
		HandlerWorkers               int
		HandlerQueue                 int
		SkipStaleBacklogOnEmptyState bool
		SkipStaleBacklogToken        string
		ProtocolVersion              string
		TLS                          bool
		TLSCAPath                    string
		TLSCertPath                  string
		TLSKeyPath                   string
		SASLEnabled                  bool
		SASLMechanism                string
		SASLUsername                 string
		SASLPassword                 string
	}
	FastMitigation struct {
		Enabled        bool
		Topic          string
		GroupID        string
		TrustedKeysDir string
		RejectTopic    string
	}

	RateLimiter struct {
		Backend   string
		RedisAddr string
		RedisUser string
		RedisPass string
		RedisDB   int
		KeyPrefix string
	}

	Ack struct {
		Enabled        bool
		PublisherImpl  string
		Topic          string
		Brokers        []string
		ClientID       string
		Idempotent     bool
		EnqueueTimeout time.Duration
		RetryMax       int
		RetryBackoff   time.Duration
		Retrier        struct {
			MaxHeadAttempts int
		}
		QueuePath    string
		QueueMaxSize int
		Batch        struct {
			Enabled  bool
			MaxSize  int
			Interval time.Duration
		}
		Signing struct {
			Enabled bool
			KeyPath string
		}
	}

	TrustedKeysDir        string
	MetricsAddr           string
	EnforcementBackend    string
	DryRun                bool
	KillSwitchEnabled     bool
	StatePath             string
	StateHistoryRetention time.Duration
	StateLockTimeout      time.Duration
	StatePersistMinGap    time.Duration
	StateFlushInterval    time.Duration
	StateChecksum         bool
	LedgerSnapshotPath    string
	LedgerDriftGrace      time.Duration
	ReconcileInterval     time.Duration
	ReconcilerMaxBackoff  time.Duration
	ExpirationCheckFreq   time.Duration
	SchedulerMaxBackoff   time.Duration
	ShutdownTimeout       time.Duration
	IPTablesBinary        string
	NFTBinary             string
	KubeConfigPath        string
	KubeContext           string
	KubeNamespace         string
	KubeQPS               float32
	KubeBurst             int

	// Cilium backend options (ENFORCEMENT_BACKEND=cilium).
	CiliumPolicyMode      string
	CiliumPolicyNamespace string
	CiliumLabelPrefix     string

	// Gateway backend options (ENFORCEMENT_BACKEND=gateway).
	GatewayNamespace  string
	GatewayGuardrails struct {
		MaxTargetsPerPolicy int64
		MaxPortsPerPolicy   int64
		MaxActivePolicies   int64
		MaxActivePerTenant  int64
		MaxTTLSeconds       int64
		RequireTenant       bool
		EnforceTenantMatch  bool
		ApplyCooldown       time.Duration
		RequireCanary       bool
		DenyBroadCIDRs      bool
		MinIPv4Prefix       int
		MinIPv6Prefix       int
		ProtectedCIDRs      []string
		ProtectedIPs        []string
		ProtectedNamespaces []string
	}

	SelectorNamespacePrefix string
	SelectorNodePrefix      string
	FastPathEnabled         bool
	FastPathMinConfidence   float64
	FastPathSignalsRequired int64

	LogLevel string
	NodeName string
	Security struct {
		ReplayWindow          time.Duration
		ReplayFutureSkew      time.Duration
		ReplayCacheMaxEntries int
	}
}

const (
	defaultTopic              = "control.policy.v2"
	defaultGroupID            = "policy-enforcement-agent"
	defaultConsumptionMode    = consumptionModeSharedGroup
	defaultMetricsAddr        = ":9094"
	defaultReconcileInterval  = 30 * time.Second
	defaultExpirationInterval = 5 * time.Second
	defaultShutdownTimeout    = 15 * time.Second
)

const (
	consumptionModeSharedGroup   = "shared_group"
	consumptionModeFanoutPerNode = "fanout_per_node"
)

// Load reads configuration from environment variables with sensible defaults.
func Load() (Config, error) {
	var cfg Config
	cfg.NodeName = strings.TrimSpace(os.Getenv("NODE_NAME"))

	brokers := strings.TrimSpace(os.Getenv("CONTROL_POLICY_BROKERS"))
	if brokers == "" {
		return cfg, fmt.Errorf("CONTROL_POLICY_BROKERS is required")
	}
	cfg.Kafka.Brokers = splitAndTrim(brokers)

	cfg.Kafka.Topic = envWithDefault("CONTROL_POLICY_TOPIC", defaultTopic)
	cfg.Kafka.GroupID = envWithDefault("CONTROL_POLICY_GROUP", defaultGroupID)
	cfg.Kafka.ConsumptionMode = strings.ToLower(strings.TrimSpace(envWithDefault("CONTROL_POLICY_CONSUMPTION_MODE", defaultConsumptionMode)))
	cfg.Kafka.HandlerWorkers = int(parseIntEnv("CONTROL_POLICY_HANDLER_WORKERS", 8))
	if cfg.Kafka.HandlerWorkers <= 0 {
		cfg.Kafka.HandlerWorkers = 8
	}
	cfg.Kafka.HandlerQueue = int(parseIntEnv("CONTROL_POLICY_HANDLER_QUEUE", 256))
	if cfg.Kafka.HandlerQueue <= 0 {
		cfg.Kafka.HandlerQueue = 256
	}
	cfg.Kafka.SkipStaleBacklogOnEmptyState = parseBoolEnv("CONTROL_POLICY_SKIP_STALE_BACKLOG_ON_EMPTY_STATE", false)
	cfg.Kafka.SkipStaleBacklogToken = strings.TrimSpace(os.Getenv("CONTROL_POLICY_SKIP_STALE_BACKLOG_TOKEN"))
	switch cfg.Kafka.ConsumptionMode {
	case consumptionModeSharedGroup:
	case consumptionModeFanoutPerNode:
		if cfg.NodeName == "" {
			return cfg, fmt.Errorf("NODE_NAME is required when CONTROL_POLICY_CONSUMPTION_MODE=%s", consumptionModeFanoutPerNode)
		}
		if cfg.Kafka.GroupID == defaultGroupID || !strings.Contains(cfg.Kafka.GroupID, cfg.NodeName) {
			return cfg, fmt.Errorf("CONTROL_POLICY_GROUP must include NODE_NAME when CONTROL_POLICY_CONSUMPTION_MODE=%s", consumptionModeFanoutPerNode)
		}
	default:
		return cfg, fmt.Errorf("unsupported CONTROL_POLICY_CONSUMPTION_MODE %q", cfg.Kafka.ConsumptionMode)
	}
	cfg.Kafka.ProtocolVersion = envWithDefault("KAFKA_PROTOCOL_VERSION", "3.6.0")
	cfg.Kafka.TLS = parseBool(os.Getenv("CONTROL_POLICY_TLS"))
	cfg.Kafka.TLSCAPath = strings.TrimSpace(os.Getenv("CONTROL_POLICY_TLS_CA"))
	cfg.Kafka.TLSCertPath = strings.TrimSpace(os.Getenv("CONTROL_POLICY_TLS_CERT"))
	cfg.Kafka.TLSKeyPath = strings.TrimSpace(os.Getenv("CONTROL_POLICY_TLS_KEY"))
	cfg.Kafka.SASLEnabled = parseBoolEnv("KAFKA_SASL_ENABLED", parseBool(os.Getenv("KAFKA_TLS_ENABLED")))
	cfg.Kafka.SASLMechanism = strings.TrimSpace(os.Getenv("KAFKA_SASL_MECHANISM"))
	cfg.Kafka.SASLUsername = strings.TrimSpace(os.Getenv("KAFKA_SASL_USERNAME"))
	cfg.Kafka.SASLPassword = strings.TrimSpace(os.Getenv("KAFKA_SASL_PASSWORD"))

	cfg.FastMitigation.Enabled = parseBoolEnv("FAST_MITIGATION_ENABLED", false)
	cfg.FastMitigation.Topic = envWithDefault("FAST_MITIGATION_TOPIC", "control.fast_mitigation.v1")
	cfg.FastMitigation.GroupID = envWithDefault("FAST_MITIGATION_GROUP", cfg.Kafka.GroupID+"-fast")
	cfg.FastMitigation.TrustedKeysDir = strings.TrimSpace(os.Getenv("FAST_MITIGATION_TRUSTED_KEYS"))
	cfg.FastMitigation.RejectTopic = strings.TrimSpace(envWithDefault("FAST_MITIGATION_REJECT_TOPIC", "control.fast_mitigation.reject.v1"))

	// State path is needed early so other subsystems (e.g. ACK queue) can derive defaults.
	cfg.StatePath = strings.TrimSpace(os.Getenv("ENFORCEMENT_STATE_PATH"))
	if cfg.StatePath != "" {
		abs, err := filepath.Abs(cfg.StatePath)
		if err != nil {
			return cfg, fmt.Errorf("resolve ENFORCEMENT_STATE_PATH: %w", err)
		}
		cfg.StatePath = abs
	}

	cfg.RateLimiter.Backend = strings.ToLower(envWithDefault("RATE_LIMIT_COORDINATOR", "local"))
	cfg.RateLimiter.RedisAddr = strings.TrimSpace(os.Getenv("RATE_LIMIT_REDIS_ADDR"))
	cfg.RateLimiter.RedisUser = strings.TrimSpace(os.Getenv("RATE_LIMIT_REDIS_USERNAME"))
	cfg.RateLimiter.RedisPass = strings.TrimSpace(os.Getenv("RATE_LIMIT_REDIS_PASSWORD"))
	cfg.RateLimiter.RedisDB = int(parseIntEnv("RATE_LIMIT_REDIS_DB", 0))
	cfg.RateLimiter.KeyPrefix = strings.TrimSpace(os.Getenv("RATE_LIMIT_KEY_PREFIX"))

	cfg.Ack.Enabled = parseBoolEnv("ACK_ENABLED", false)
	cfg.Ack.PublisherImpl = strings.ToLower(envWithDefault("ACK_PUBLISHER_IMPL", "kafkago"))
	ackTopic := strings.TrimSpace(os.Getenv("ACK_TOPIC"))
	if ackTopic == "" {
		ackTopic = "control.enforcement_ack.v1"
	}
	cfg.Ack.Topic = ackTopic
	ackBrokers := strings.TrimSpace(os.Getenv("ACK_BROKERS"))
	if ackBrokers == "" {
		ackBrokers = brokers
	}
	cfg.Ack.Brokers = splitAndTrim(ackBrokers)
	if len(cfg.Ack.Brokers) == 0 {
		cfg.Ack.Brokers = cfg.Kafka.Brokers
	}
	cfg.Ack.ClientID = strings.TrimSpace(os.Getenv("ACK_CLIENT_ID"))
	if cfg.Ack.ClientID == "" {
		cfg.Ack.ClientID = "policy-ack-publisher"
	}
	cfg.Ack.Idempotent = parseBoolEnv("ACK_IDEMPOTENT", true)
	cfg.Ack.EnqueueTimeout = parseDurationEnv("ACK_ENQUEUE_TIMEOUT", 500*time.Millisecond)
	if cfg.Ack.EnqueueTimeout <= 0 {
		cfg.Ack.EnqueueTimeout = 500 * time.Millisecond
	}
	cfg.Ack.RetryMax = int(parseIntEnv("ACK_RETRY_MAX", 5))
	if cfg.Ack.RetryMax <= 0 {
		cfg.Ack.RetryMax = 5
	}
	backoff := parseDurationEnv("ACK_RETRY_BACKOFF", 500*time.Millisecond)
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}
	cfg.Ack.RetryBackoff = backoff
	// Keep head-of-line retries low by default; the durable queue retrier will
	// revisit failed records and should not pin the queue head for long.
	cfg.Ack.Retrier.MaxHeadAttempts = int(parseIntEnv("ACK_RETRIER_MAX_HEAD_ATTEMPTS", 1))
	if cfg.Ack.Retrier.MaxHeadAttempts <= 0 {
		cfg.Ack.Retrier.MaxHeadAttempts = 1
	}
	cfg.Ack.QueuePath = strings.TrimSpace(os.Getenv("ACK_QUEUE_PATH"))
	if cfg.Ack.QueuePath == "" && cfg.StatePath != "" {
		cfg.Ack.QueuePath = filepath.Join(filepath.Dir(cfg.StatePath), "ack-queue.db")
	}
	cfg.Ack.QueueMaxSize = int(parseIntEnv("ACK_QUEUE_MAX_SIZE", 10000))
	if cfg.Ack.QueueMaxSize <= 0 {
		cfg.Ack.QueueMaxSize = 10000
	}
	cfg.Ack.Batch.Enabled = parseBoolEnv("ACK_BATCH_ENABLED", false)
	cfg.Ack.Batch.MaxSize = int(parseIntEnv("ACK_BATCH_MAX_SIZE", 50))
	if cfg.Ack.Batch.MaxSize <= 0 {
		cfg.Ack.Batch.MaxSize = 50
	}
	cfg.Ack.Batch.Interval = parseDurationEnv("ACK_BATCH_INTERVAL", 250*time.Millisecond)
	if cfg.Ack.Batch.Interval <= 0 {
		cfg.Ack.Batch.Interval = 250 * time.Millisecond
	}
	cfg.Ack.Signing.Enabled = parseBoolEnv("ACK_SIGNING_ENABLED", false)
	cfg.Ack.Signing.KeyPath = strings.TrimSpace(os.Getenv("ACK_SIGNING_KEY_PATH"))

	cfg.TrustedKeysDir = strings.TrimSpace(os.Getenv("CONTROL_POLICY_TRUSTED_KEYS"))
	if cfg.TrustedKeysDir == "" {
		return cfg, fmt.Errorf("CONTROL_POLICY_TRUSTED_KEYS is required")
	}
	if cfg.FastMitigation.Enabled && cfg.FastMitigation.TrustedKeysDir == "" {
		cfg.FastMitigation.TrustedKeysDir = cfg.TrustedKeysDir
	}
	if cfg.FastMitigation.Enabled && cfg.FastMitigation.TrustedKeysDir == "" {
		return cfg, fmt.Errorf("FAST_MITIGATION_TRUSTED_KEYS is required when FAST_MITIGATION_ENABLED=true")
	}
	if cfg.FastMitigation.Enabled && cfg.Kafka.ConsumptionMode == consumptionModeFanoutPerNode {
		if cfg.FastMitigation.GroupID == "" || !strings.Contains(cfg.FastMitigation.GroupID, cfg.NodeName) {
			return cfg, fmt.Errorf("FAST_MITIGATION_GROUP must include NODE_NAME when CONTROL_POLICY_CONSUMPTION_MODE=%s", consumptionModeFanoutPerNode)
		}
	}

	cfg.EnforcementBackend = strings.ToLower(envWithDefault("ENFORCEMENT_BACKEND", "iptables"))
	cfg.DryRun = parseBool(os.Getenv("ENFORCEMENT_DRY_RUN"))
	cfg.KillSwitchEnabled = parseBoolEnv("ENFORCEMENT_KILL_SWITCH_ENABLED", false)
	if cfg.StatePath == "" {
		return cfg, fmt.Errorf("ENFORCEMENT_STATE_PATH is required")
	}
	if cfg.Kafka.SkipStaleBacklogOnEmptyState {
		if cfg.Kafka.ConsumptionMode != consumptionModeFanoutPerNode {
			return cfg, fmt.Errorf("CONTROL_POLICY_SKIP_STALE_BACKLOG_ON_EMPTY_STATE requires CONTROL_POLICY_CONSUMPTION_MODE=%s", consumptionModeFanoutPerNode)
		}
		if cfg.Kafka.SkipStaleBacklogToken == "" {
			return cfg, fmt.Errorf("CONTROL_POLICY_SKIP_STALE_BACKLOG_TOKEN is required when CONTROL_POLICY_SKIP_STALE_BACKLOG_ON_EMPTY_STATE=true")
		}
	}
	cfg.IPTablesBinary = envWithDefault("ENFORCER_IPTABLES_BIN", "iptables")
	cfg.NFTBinary = envWithDefault("ENFORCER_NFT_BIN", "nft")
	cfg.SelectorNamespacePrefix = strings.TrimSpace(os.Getenv("ENFORCER_SELECTOR_NAMESPACE_PREFIX"))
	cfg.SelectorNodePrefix = strings.TrimSpace(os.Getenv("ENFORCER_SELECTOR_NODE_PREFIX"))
	cfg.KubeConfigPath = strings.TrimSpace(os.Getenv("ENFORCER_KUBE_CONFIG"))
	cfg.KubeContext = strings.TrimSpace(os.Getenv("ENFORCER_KUBE_CONTEXT"))
	cfg.KubeNamespace = envWithDefault("ENFORCER_KUBE_NAMESPACE", "default")
	cfg.KubeQPS = parseFloatEnv("ENFORCER_KUBE_QPS", 5.0)
	cfg.KubeBurst = int(parseIntEnv("ENFORCER_KUBE_BURST", 10))

	// Cilium backend behavior (only used when ENFORCEMENT_BACKEND=cilium).
	cfg.CiliumPolicyMode = strings.ToLower(envWithDefault("CILIUM_POLICY_MODE", "namespaced"))
	cfg.CiliumPolicyNamespace = strings.TrimSpace(os.Getenv("CILIUM_POLICY_NAMESPACE"))
	cfg.CiliumLabelPrefix = envWithDefault("CILIUM_LABEL_PREFIX", "k8s:")

	// Gateway backend behavior (only used when ENFORCEMENT_BACKEND=gateway).
	// Keep it flexible: support both names so we can share env across backend/enforcement during testing.
	cfg.GatewayNamespace = strings.TrimSpace(os.Getenv("GATEWAY_NAMESPACE"))
	if cfg.GatewayNamespace == "" {
		cfg.GatewayNamespace = strings.TrimSpace(os.Getenv("POLICY_GATEWAY_NAMESPACE"))
	}
	cfg.GatewayNamespace = strings.ToLower(strings.TrimSpace(cfg.GatewayNamespace))
	cfg.GatewayGuardrails.MaxTargetsPerPolicy = parseIntEnv("GATEWAY_MAX_TARGETS_PER_POLICY", 256)
	cfg.GatewayGuardrails.MaxPortsPerPolicy = parseIntEnv("GATEWAY_MAX_PORTS_PER_POLICY", 128)
	cfg.GatewayGuardrails.MaxActivePolicies = parseIntEnv("GATEWAY_MAX_ACTIVE_POLICIES", 4096)
	cfg.GatewayGuardrails.MaxActivePerTenant = parseIntEnv("GATEWAY_MAX_ACTIVE_POLICIES_PER_TENANT", 0)
	cfg.GatewayGuardrails.MaxTTLSeconds = parseIntEnv("GATEWAY_MAX_TTL_SECONDS", 3600)
	cfg.GatewayGuardrails.RequireTenant = parseBoolEnv("GATEWAY_REQUIRE_TENANT", false)
	cfg.GatewayGuardrails.EnforceTenantMatch = parseBoolEnv("GATEWAY_ENFORCE_TENANT_MATCH", true)
	cfg.GatewayGuardrails.ApplyCooldown = parseDurationEnv("GATEWAY_APPLY_COOLDOWN", 0)
	cfg.GatewayGuardrails.RequireCanary = parseBoolEnv("GATEWAY_REQUIRE_CANARY", false)
	cfg.GatewayGuardrails.DenyBroadCIDRs = parseBoolEnv("GATEWAY_DENY_BROAD_CIDRS", true)
	cfg.GatewayGuardrails.MinIPv4Prefix = int(parseIntEnv("GATEWAY_MIN_IPV4_PREFIX", 8))
	cfg.GatewayGuardrails.MinIPv6Prefix = int(parseIntEnv("GATEWAY_MIN_IPV6_PREFIX", 32))
	cfg.GatewayGuardrails.ProtectedCIDRs = splitAndTrim(strings.TrimSpace(os.Getenv("GATEWAY_PROTECTED_CIDRS")))
	cfg.GatewayGuardrails.ProtectedIPs = splitAndTrim(strings.TrimSpace(os.Getenv("GATEWAY_PROTECTED_IPS")))
	cfg.GatewayGuardrails.ProtectedNamespaces = splitAndTrim(strings.TrimSpace(os.Getenv("GATEWAY_PROTECTED_NAMESPACES")))

	cfg.FastPathEnabled = parseBoolEnv("FAST_PATH_ENABLED", false)
	cfg.FastPathMinConfidence = parseFloat64Env("FAST_PATH_MIN_CONFIDENCE", 0.9)
	cfg.FastPathSignalsRequired = parseIntEnv("FAST_PATH_SIGNALS_REQUIRED", 2)

	cfg.MetricsAddr = envWithDefault("METRICS_ADDR", defaultMetricsAddr)
	cfg.LogLevel = strings.ToLower(envWithDefault("LOG_LEVEL", "info"))
	cfg.Security.ReplayWindow = parseDurationEnv("SECURITY_REPLAY_WINDOW", 10*time.Minute)
	cfg.Security.ReplayFutureSkew = parseDurationEnv("SECURITY_REPLAY_FUTURE_SKEW", 30*time.Second)
	cfg.Security.ReplayCacheMaxEntries = int(parseIntEnv("SECURITY_REPLAY_CACHE_MAX_ENTRIES", 20000))

	cfg.StateHistoryRetention = parseDurationEnv("ENFORCEMENT_STATE_HISTORY_RETENTION", 10*time.Minute)
	cfg.StateLockTimeout = parseDurationEnv("ENFORCEMENT_STATE_LOCK_TIMEOUT", 3*time.Second)
	cfg.StatePersistMinGap = parseDurationEnv("ENFORCEMENT_STATE_PERSIST_MIN_GAP", 0)
	cfg.StateFlushInterval = parseDurationEnv("ENFORCEMENT_STATE_FLUSH_INTERVAL", 500*time.Millisecond)
	if cfg.StatePersistMinGap < 0 {
		cfg.StatePersistMinGap = 0
	}
	if cfg.StateFlushInterval < 0 {
		cfg.StateFlushInterval = 0
	}
	cfg.StateChecksum = parseBoolEnv("ENFORCEMENT_STATE_CHECKSUM", true)
	cfg.LedgerSnapshotPath = strings.TrimSpace(os.Getenv("LEDGER_SNAPSHOT_PATH"))
	if cfg.LedgerSnapshotPath != "" {
		abs, err := filepath.Abs(cfg.LedgerSnapshotPath)
		if err != nil {
			return cfg, fmt.Errorf("resolve LEDGER_SNAPSHOT_PATH: %w", err)
		}
		cfg.LedgerSnapshotPath = abs
	}
	cfg.LedgerDriftGrace = parseDurationEnv("LEDGER_DRIFT_GRACE", 0)

	cfg.ReconcileInterval = parseDurationEnv("ENFORCEMENT_RECONCILE_INTERVAL", defaultReconcileInterval)
	cfg.ReconcilerMaxBackoff = parseDurationEnv("ENFORCEMENT_RECONCILER_MAX_BACKOFF", cfg.ReconcileInterval*4)
	if cfg.ReconcilerMaxBackoff <= 0 {
		cfg.ReconcilerMaxBackoff = cfg.ReconcileInterval * 4
	}
	cfg.ExpirationCheckFreq = parseDurationEnv("ENFORCEMENT_EXPIRATION_INTERVAL", defaultExpirationInterval)
	cfg.SchedulerMaxBackoff = parseDurationEnv("ENFORCEMENT_SCHEDULER_MAX_BACKOFF", cfg.ExpirationCheckFreq*4)
	if cfg.SchedulerMaxBackoff <= 0 {
		cfg.SchedulerMaxBackoff = cfg.ExpirationCheckFreq * 4
	}
	cfg.ShutdownTimeout = parseDurationEnv("SHUTDOWN_TIMEOUT", defaultShutdownTimeout)

	// Backend-specific required fields.
	if cfg.EnforcementBackend == "gateway" && cfg.GatewayNamespace == "" {
		return cfg, fmt.Errorf("GATEWAY_NAMESPACE is required when ENFORCEMENT_BACKEND=gateway")
	}

	return cfg, nil
}

func splitAndTrim(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func envWithDefault(key, def string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return def
}

func parseBool(val string) bool {
	res, err := strconv.ParseBool(strings.TrimSpace(val))
	if err != nil {
		return false
	}
	return res
}

func parseBoolEnv(key string, def bool) bool {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	parsed, err := strconv.ParseBool(val)
	if err != nil {
		return def
	}
	return parsed
}

func parseDurationEnv(key string, def time.Duration) time.Duration {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		return def
	}
	return d
}

func parseFloatEnv(key string, def float32) float32 {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	f, err := strconv.ParseFloat(val, 32)
	if err != nil {
		return def
	}
	return float32(f)
}

func parseFloat64Env(key string, def float64) float64 {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return def
	}
	return f
}

func parseIntEnv(key string, def int64) int64 {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	i, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return def
	}
	return i
}
