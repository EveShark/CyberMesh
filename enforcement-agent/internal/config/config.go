package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	enforcerapp "github.com/CyberMesh/enforcement-agent/internal/enforcer/app"
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
		HandlerTimeout               time.Duration
		StripeStallThreshold         time.Duration
		AdmissionPauseOnSaturation   time.Duration
		DispatchMaxWait              time.Duration
		DispatchPause                time.Duration
		StripeDistressThreshold      int
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
	CommandRejectTopic string

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
	NFTBatchAtomic        bool
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

	App struct {
		BaseURL        string
		CommandPath    string
		HealthPath     string
		Timeout        time.Duration
		BearerToken    string
		AllowedActions []string
	}

	SelectorNamespacePrefix       string
	SelectorNodePrefix            string
	ApplyTimeout                  time.Duration
	ApplyTotalBudget              time.Duration
	ApplyRetryMax                 int
	ApplyRetryBackoff             time.Duration
	ApplyRetryBaseBackoff         time.Duration
	ApplyRetryMaxBackoff          time.Duration
	ApplyRetryJitterRatio         float64
	ApplyTimeoutBreakerThreshold  int
	ApplyTimeoutBreakerCooldown   time.Duration
	ExpiredSheddingEnabled        bool
	LifecycleCompactionEnabled    bool
	LifecycleCompactionWindow     time.Duration
	CriticalLaneMaxInFlight       int
	MaintenanceLaneMaxInFlight    int
	LaneStarvationThreshold       time.Duration
	AckAcceptedEnabled            bool
	LifecycleCriticalMode         string
	LifecycleMaintenanceMode      string
	LifecyclePromotionGateEnabled bool
	LifecyclePromotionMinWindow   time.Duration
	FastPathEnabled               bool
	FastPathMinConfidence         float64
	FastPathSignalsRequired       int64

	LogLevel string
	NodeName string
	Security struct {
		ReplayWindow          time.Duration
		ReplayFutureSkew      time.Duration
		ReplayCacheMaxEntries int
	}
}

const (
	defaultTopic              = "control.enforcement_command.v1"
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

	commandTopic := strings.TrimSpace(os.Getenv("CONTROL_ENFORCEMENT_COMMAND_TOPIC"))
	if commandTopic == "" {
		commandTopic = strings.TrimSpace(os.Getenv("CONTROL_POLICY_TOPIC"))
	}
	if commandTopic == "" {
		commandTopic = defaultTopic
	}
	cfg.Kafka.Topic = commandTopic
	cfg.Kafka.GroupID = envWithDefault("CONTROL_POLICY_GROUP", defaultGroupID)
	cfg.Kafka.ConsumptionMode = strings.ToLower(strings.TrimSpace(envWithDefault("CONTROL_POLICY_CONSUMPTION_MODE", defaultConsumptionMode)))
	cfg.Kafka.HandlerWorkers = int(parseIntEnv("CONTROL_POLICY_HANDLER_WORKERS", 48))
	if cfg.Kafka.HandlerWorkers <= 0 {
		cfg.Kafka.HandlerWorkers = 48
	}
	cfg.Kafka.HandlerQueue = int(parseIntEnv("CONTROL_POLICY_HANDLER_QUEUE", 4096))
	if cfg.Kafka.HandlerQueue <= 0 {
		cfg.Kafka.HandlerQueue = 4096
	}
	cfg.Kafka.HandlerTimeout = parseDurationEnv("ENFORCEMENT_HANDLER_TIMEOUT", 0)
	if cfg.Kafka.HandlerTimeout < 0 {
		cfg.Kafka.HandlerTimeout = 0
	}
	handlerTimeoutRaw, handlerTimeoutSet := os.LookupEnv("ENFORCEMENT_HANDLER_TIMEOUT")
	handlerTimeoutExplicit := handlerTimeoutSet && strings.TrimSpace(handlerTimeoutRaw) != ""
	cfg.Kafka.StripeStallThreshold = parseDurationEnv("ENFORCEMENT_STRIPE_STALL_THRESHOLD", 5*time.Second)
	if cfg.Kafka.StripeStallThreshold < 0 {
		cfg.Kafka.StripeStallThreshold = 5 * time.Second
	}
	cfg.Kafka.AdmissionPauseOnSaturation = parseDurationEnv("CONTROL_POLICY_ADMISSION_PAUSE_ON_SATURATION", 0)
	if cfg.Kafka.AdmissionPauseOnSaturation < 0 {
		cfg.Kafka.AdmissionPauseOnSaturation = 0
	}
	cfg.Kafka.DispatchMaxWait = parseDurationEnv("ENFORCEMENT_DISPATCH_MAX_WAIT", 0)
	if cfg.Kafka.DispatchMaxWait < 0 {
		cfg.Kafka.DispatchMaxWait = 0
	}
	cfg.Kafka.DispatchPause = parseDurationEnv("ENFORCEMENT_DISPATCH_PAUSE", 0)
	if cfg.Kafka.DispatchPause < 0 {
		cfg.Kafka.DispatchPause = 0
	}
	cfg.Kafka.StripeDistressThreshold = int(parseIntEnv("ENFORCEMENT_STRIPE_DISTRESS_THRESHOLD", 3))
	if cfg.Kafka.StripeDistressThreshold < 1 {
		cfg.Kafka.StripeDistressThreshold = 1
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
	cfg.CommandRejectTopic = strings.TrimSpace(envWithDefault("CONTROL_ENFORCEMENT_COMMAND_REJECT_TOPIC", "control.enforcement_command.reject.v1"))

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
	cfg.Ack.EnqueueTimeout = parseDurationEnv("ACK_ENQUEUE_TIMEOUT", time.Second)
	if cfg.Ack.EnqueueTimeout <= 0 {
		cfg.Ack.EnqueueTimeout = time.Second
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
	cfg.Ack.Batch.MaxSize = int(parseIntEnv("ACK_BATCH_MAX_SIZE", 96))
	if cfg.Ack.Batch.MaxSize <= 0 {
		cfg.Ack.Batch.MaxSize = 96
	}
	cfg.Ack.Batch.Interval = parseDurationEnv("ACK_BATCH_INTERVAL", 50*time.Millisecond)
	if cfg.Ack.Batch.Interval <= 0 {
		cfg.Ack.Batch.Interval = 50 * time.Millisecond
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
	cfg.NFTBatchAtomic = parseBoolEnv("ENFORCER_NFT_BATCH_ATOMIC", true)
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

	// App backend behavior (only used when ENFORCEMENT_BACKEND=app).
	cfg.App.BaseURL = strings.TrimSpace(os.Getenv("APP_ENFORCER_BASE_URL"))
	cfg.App.CommandPath = strings.TrimSpace(envWithDefault("APP_ENFORCER_COMMAND_PATH", "/v1/enforcement/commands"))
	cfg.App.HealthPath = strings.TrimSpace(envWithDefault("APP_ENFORCER_HEALTH_PATH", "/healthz"))
	cfg.App.Timeout = parseDurationEnv("APP_ENFORCER_TIMEOUT", 2*time.Second)
	if cfg.App.Timeout <= 0 {
		cfg.App.Timeout = 2 * time.Second
	}
	cfg.App.BearerToken = strings.TrimSpace(os.Getenv("APP_ENFORCER_BEARER_TOKEN"))
	cfg.App.AllowedActions = splitAndTrim(strings.TrimSpace(envWithDefault(
		"APP_ENFORCER_ALLOWED_ACTIONS",
		"drop,reject,remove,force_reauth,disable_export,freeze_user,freeze_tenant,throttle_action,rate_limit",
	)))

	// Preserve legacy behavior when unset/invalid/non-positive: no controller-level apply timeout.
	// Strict timeout mode is opt-in via ENFORCEMENT_APPLY_TIMEOUT > 0.
	cfg.ApplyTimeout = parseDurationEnv("ENFORCEMENT_APPLY_TIMEOUT", 0)
	if cfg.ApplyTimeout <= 0 {
		cfg.ApplyTimeout = 0
	}
	cfg.ApplyTotalBudget = parseDurationEnv("ENFORCEMENT_APPLY_TOTAL_BUDGET", 0)
	if cfg.ApplyTotalBudget <= 0 {
		cfg.ApplyTotalBudget = 0
	}
	cfg.ApplyRetryMax = int(parseIntEnv("ENFORCEMENT_APPLY_RETRY_MAX", 2))
	if cfg.ApplyRetryMax < 0 {
		cfg.ApplyRetryMax = 0
	}
	cfg.ApplyRetryBackoff = parseDurationEnv("ENFORCEMENT_APPLY_RETRY_BACKOFF", 150*time.Millisecond)
	if cfg.ApplyRetryBackoff <= 0 {
		cfg.ApplyRetryBackoff = 150 * time.Millisecond
	}
	cfg.ApplyRetryBaseBackoff = parseDurationEnv("ENFORCEMENT_APPLY_RETRY_BASE_BACKOFF", cfg.ApplyRetryBackoff)
	if cfg.ApplyRetryBaseBackoff <= 0 {
		cfg.ApplyRetryBaseBackoff = cfg.ApplyRetryBackoff
	}
	cfg.ApplyRetryMaxBackoff = parseDurationEnv("ENFORCEMENT_APPLY_RETRY_MAX_BACKOFF", time.Second)
	if cfg.ApplyRetryMaxBackoff <= 0 {
		cfg.ApplyRetryMaxBackoff = time.Second
	}
	if cfg.ApplyRetryMaxBackoff < cfg.ApplyRetryBaseBackoff {
		cfg.ApplyRetryMaxBackoff = cfg.ApplyRetryBaseBackoff
	}
	cfg.ApplyRetryJitterRatio = parseFloat64Env("ENFORCEMENT_APPLY_RETRY_JITTER_RATIO", 0)
	if cfg.ApplyRetryJitterRatio < 0 {
		cfg.ApplyRetryJitterRatio = 0
	}
	if cfg.ApplyRetryJitterRatio > 1 {
		cfg.ApplyRetryJitterRatio = 1
	}
	cfg.ApplyTimeoutBreakerThreshold = int(parseIntEnv("ENFORCEMENT_APPLY_TIMEOUT_BREAKER_THRESHOLD", 3))
	if cfg.ApplyTimeoutBreakerThreshold < 0 {
		cfg.ApplyTimeoutBreakerThreshold = 0
	}
	cfg.ApplyTimeoutBreakerCooldown = parseDurationEnv("ENFORCEMENT_APPLY_TIMEOUT_BREAKER_COOLDOWN", 2*time.Second)
	if cfg.ApplyTimeoutBreakerCooldown < 0 {
		cfg.ApplyTimeoutBreakerCooldown = 0
	}
	if cfg.ApplyTimeout > 0 {
		defaultBudget := deriveApplyRetryBudget(cfg.ApplyTimeout, cfg.ApplyRetryMax, cfg.ApplyRetryBaseBackoff, cfg.ApplyRetryMaxBackoff)
		if cfg.ApplyTotalBudget <= 0 {
			cfg.ApplyTotalBudget = defaultBudget
		}
		// Keep consumer envelope comfortably above controller retry budget.
		minHandlerTimeout := cfg.ApplyTotalBudget + 250*time.Millisecond
		if cfg.Kafka.HandlerTimeout <= 0 {
			cfg.Kafka.HandlerTimeout = minHandlerTimeout
		} else if !handlerTimeoutExplicit && cfg.Kafka.HandlerTimeout < minHandlerTimeout {
			cfg.Kafka.HandlerTimeout = minHandlerTimeout
		}
	}
	cfg.ExpiredSheddingEnabled = parseBoolEnv("ENFORCEMENT_SHEDDING_ENABLED", false)
	cfg.LifecycleCompactionEnabled = parseBoolEnv("ENFORCEMENT_LIFECYCLE_COMPACTION_ENABLED", true)
	cfg.LifecycleCompactionWindow = parseDurationEnv("ENFORCEMENT_LIFECYCLE_COMPACTION_WINDOW", 2*time.Minute)
	if cfg.LifecycleCompactionWindow <= 0 {
		cfg.LifecycleCompactionWindow = 2 * time.Minute
	}
	cfg.CriticalLaneMaxInFlight = int(parseIntEnv("ENFORCEMENT_CRITICAL_MAX_IN_FLIGHT", 32))
	if cfg.CriticalLaneMaxInFlight <= 0 {
		cfg.CriticalLaneMaxInFlight = 32
	}
	cfg.MaintenanceLaneMaxInFlight = int(parseIntEnv("ENFORCEMENT_MAINTENANCE_MAX_IN_FLIGHT", 8))
	if cfg.MaintenanceLaneMaxInFlight <= 0 {
		cfg.MaintenanceLaneMaxInFlight = 8
	}
	cfg.LaneStarvationThreshold = parseDurationEnv("ENFORCEMENT_LANE_STARVATION_THRESHOLD", 2*time.Second)
	if cfg.LaneStarvationThreshold <= 0 {
		cfg.LaneStarvationThreshold = 2 * time.Second
	}
	cfg.AckAcceptedEnabled = parseBoolEnv("ENFORCEMENT_ACK_ACCEPTED_ENABLED", false)
	cfg.LifecycleCriticalMode = strings.ToLower(strings.TrimSpace(envWithDefault("LIFECYCLE_CLASS_CRITICAL_MODE", "enforce")))
	if cfg.LifecycleCriticalMode != "enforce" && cfg.LifecycleCriticalMode != "dry_run" {
		cfg.LifecycleCriticalMode = "enforce"
	}
	cfg.LifecycleMaintenanceMode = strings.ToLower(strings.TrimSpace(envWithDefault("LIFECYCLE_CLASS_MAINTENANCE_MODE", "enforce")))
	if cfg.LifecycleMaintenanceMode != "enforce" && cfg.LifecycleMaintenanceMode != "dry_run" {
		cfg.LifecycleMaintenanceMode = "enforce"
	}
	cfg.LifecyclePromotionGateEnabled = parseBoolEnv("LIFECYCLE_PROMOTION_GATE_ENABLED", false)
	cfg.LifecyclePromotionMinWindow = parseDurationEnv("LIFECYCLE_PROMOTION_MIN_WINDOW", 0)
	if cfg.LifecyclePromotionMinWindow < 0 {
		cfg.LifecyclePromotionMinWindow = 0
	}

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
	if cfg.EnforcementBackend == "app" {
		if cfg.App.BaseURL == "" {
			return cfg, fmt.Errorf("APP_ENFORCER_BASE_URL is required when ENFORCEMENT_BACKEND=app")
		}
		parsed, err := url.Parse(cfg.App.BaseURL)
		if err != nil {
			return cfg, fmt.Errorf("invalid APP_ENFORCER_BASE_URL: %w", err)
		}
		if parsed.Scheme != "https" && parsed.Scheme != "http" {
			return cfg, fmt.Errorf("APP_ENFORCER_BASE_URL must use http or https scheme")
		}
		if len(cfg.App.AllowedActions) == 0 {
			return cfg, fmt.Errorf("APP_ENFORCER_ALLOWED_ACTIONS must include at least one action")
		}
		for _, action := range cfg.App.AllowedActions {
			if !enforcerapp.IsCanonicalAllowedAction(action) {
				return cfg, fmt.Errorf("APP_ENFORCER_ALLOWED_ACTIONS contains unsupported action %q", action)
			}
		}
	}

	return cfg, nil
}

func deriveApplyRetryBudget(applyTimeout time.Duration, retryMax int, baseBackoff time.Duration, maxBackoff time.Duration) time.Duration {
	if applyTimeout <= 0 {
		return 0
	}
	attempts := retryMax + 1
	if attempts < 1 {
		attempts = 1
	}
	if baseBackoff <= 0 {
		baseBackoff = 150 * time.Millisecond
	}
	if maxBackoff <= 0 {
		maxBackoff = time.Second
	}
	if maxBackoff < baseBackoff {
		maxBackoff = baseBackoff
	}
	applyWindow := time.Duration(attempts) * applyTimeout
	backoffSum := time.Duration(0)
	backoff := baseBackoff
	for i := 0; i < retryMax; i++ {
		backoffSum += backoff
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
	return applyWindow + backoffSum + 150*time.Millisecond
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
