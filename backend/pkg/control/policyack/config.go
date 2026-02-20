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

	StoreRetryMax     int
	StoreRetryBackoff time.Duration
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
	signingRequired := cm.GetBool("CONTROL_POLICY_ACK_SIGNING_REQUIRED", env == "production" || env == "staging")
	trustDir := strings.TrimSpace(cm.GetString("CONTROL_POLICY_ACK_TRUST_DIR", ""))

	retryMax := cm.GetInt("CONTROL_POLICY_ACK_STORE_RETRY_MAX", 5)
	if retryMax <= 0 {
		retryMax = 5
	}
	backoff := cm.GetDuration("CONTROL_POLICY_ACK_STORE_RETRY_BACKOFF", 250*time.Millisecond)
	if backoff <= 0 {
		backoff = 250 * time.Millisecond
	}

	cfg := Config{
		Enabled:           enabled,
		Brokers:           brokers,
		GroupID:           groupID,
		Topic:             topic,
		DLQ:               dlq,
		SigningRequired:   signingRequired,
		TrustedKeysDir:    trustDir,
		StoreRetryMax:     retryMax,
		StoreRetryBackoff: backoff,
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
