package policyack

import (
	"fmt"
	"strings"
	"time"

	"backend/pkg/utils"
)

type Config struct {
	Enabled bool

	Brokers []string
	GroupID string
	Topic   string
	DLQ     string

	SigningRequired bool
	TrustedKeysDir  string
	RequireQCRef    bool
	RequireRuleHash bool
	RequireProducer bool

	StoreRetryMax       int
	StoreRetryBackoff   time.Duration
	Workers             int
	WorkQueueSize       int
	SoftThrottleEnabled bool
	SoftThrottleSleep   time.Duration
	SoftThrottleWindow  int
	LogThrottle         time.Duration
}

func LoadConfig(cm *utils.ConfigManager) (Config, error) {
	if cm == nil {
		return Config{}, fmt.Errorf("policy ack: config manager required")
	}

	enabled := cm.GetBool("CONTROL_POLICY_ACK_CONSUME_ENABLED", false)
	if !enabled {
		return Config{Enabled: false}, nil
	}

	brokers := cm.GetStringSlice("KAFKA_BROKERS", nil)
	for i := range brokers {
		brokers[i] = strings.TrimSpace(brokers[i])
	}

	groupID := strings.TrimSpace(cm.GetString("CONTROL_POLICY_ACK_GROUP", "control-policy-acks"))
	topic := strings.TrimSpace(cm.GetString("CONTROL_POLICY_ACK_TOPIC", "control.enforcement_ack.v1"))
	dlq := strings.TrimSpace(cm.GetString("CONTROL_POLICY_ACK_DLQ_TOPIC", ""))

	env := strings.ToLower(strings.TrimSpace(cm.GetString("ENVIRONMENT", "production")))
	strictDefault := env == "production" || env == "staging"
	signingRequired := cm.GetBool("CONTROL_POLICY_ACK_SIGNING_REQUIRED", env == "production" || env == "staging")
	trustDir := strings.TrimSpace(cm.GetString("CONTROL_POLICY_ACK_TRUST_DIR", ""))
	requireQCRef := cm.GetBool("CONTROL_POLICY_ACK_REQUIRE_QC_REFERENCE", strictDefault)
	requireRuleHash := cm.GetBool("CONTROL_POLICY_ACK_REQUIRE_RULE_HASH", strictDefault)
	requireProducer := cm.GetBool("CONTROL_POLICY_ACK_REQUIRE_PRODUCER_ID", strictDefault)

	retryMax := cm.GetInt("CONTROL_POLICY_ACK_STORE_RETRY_MAX", 5)
	if retryMax <= 0 {
		retryMax = 5
	}
	backoff := cm.GetDuration("CONTROL_POLICY_ACK_STORE_RETRY_BACKOFF", 250*time.Millisecond)
	if backoff <= 0 {
		backoff = 250 * time.Millisecond
	}
	workers := cm.GetInt("CONTROL_POLICY_ACK_WORKERS", 4)
	if workers <= 0 {
		workers = 4
	}
	workQueueSize := cm.GetInt("CONTROL_POLICY_ACK_WORK_QUEUE_SIZE", 256)
	if workQueueSize <= 0 {
		workQueueSize = 256
	}
	softThrottleEnabled := cm.GetBool("CONTROL_POLICY_ACK_SOFT_THROTTLE_ENABLED", true)
	softThrottleSleep := cm.GetDuration("CONTROL_POLICY_ACK_SOFT_THROTTLE_SLEEP", 200*time.Millisecond)
	if softThrottleSleep < 0 {
		softThrottleSleep = 0
	}
	softThrottleWindow := cm.GetInt("CONTROL_POLICY_ACK_SOFT_THROTTLE_WINDOW", 3)
	if softThrottleWindow <= 0 {
		softThrottleWindow = 3
	}
	logThrottle := cm.GetDuration("CONTROL_POLICY_ACK_LOG_THROTTLE", 5*time.Second)
	if logThrottle <= 0 {
		logThrottle = 5 * time.Second
	}

	cfg := Config{
		Enabled:             enabled,
		Brokers:             brokers,
		GroupID:             groupID,
		Topic:               topic,
		DLQ:                 dlq,
		SigningRequired:     signingRequired,
		TrustedKeysDir:      trustDir,
		RequireQCRef:        requireQCRef,
		RequireRuleHash:     requireRuleHash,
		RequireProducer:     requireProducer,
		StoreRetryMax:       retryMax,
		StoreRetryBackoff:   backoff,
		Workers:             workers,
		WorkQueueSize:       workQueueSize,
		SoftThrottleEnabled: softThrottleEnabled,
		SoftThrottleSleep:   softThrottleSleep,
		SoftThrottleWindow:  softThrottleWindow,
		LogThrottle:         logThrottle,
	}

	if len(cfg.Brokers) == 0 {
		return Config{}, fmt.Errorf("policy ack: KAFKA_BROKERS required")
	}
	if cfg.Topic == "" {
		return Config{}, fmt.Errorf("policy ack: CONTROL_POLICY_ACK_TOPIC required")
	}
	if cfg.GroupID == "" {
		return Config{}, fmt.Errorf("policy ack: CONTROL_POLICY_ACK_GROUP required")
	}
	if cfg.SigningRequired && cfg.TrustedKeysDir == "" {
		return Config{}, fmt.Errorf("policy ack: signing required but CONTROL_POLICY_ACK_TRUST_DIR is empty")
	}

	return cfg, nil
}
