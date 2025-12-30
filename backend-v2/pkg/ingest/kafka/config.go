package kafka

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"backend/pkg/utils"

	"github.com/IBM/sarama"
)

// Environment indicates the deployment environment
type Environment string

const (
	EnvProduction  Environment = "production"
	EnvStaging     Environment = "staging"
	EnvDevelopment Environment = "development"
	EnvTest        Environment = "test"
)

// validateKafkaConfig performs security-critical validation
func validateKafkaConfig(ctx context.Context, cm *utils.ConfigManager, audit *utils.AuditLogger) error {
	// Validate brokers
	brokers := cm.GetStringSlice("KAFKA_BROKERS", nil)
	if len(brokers) == 0 {
		if audit != nil {
			_ = audit.Security("kafka_config_invalid", map[string]interface{}{
				"error": "KAFKA_BROKERS required",
			})
		}
		return fmt.Errorf("kafka: KAFKA_BROKERS required (comma-separated list)")
	}

	// Validate input topics
	inputTopics := cm.GetStringSlice("KAFKA_INPUT_TOPICS", nil)
	if len(inputTopics) == 0 {
		if audit != nil {
			_ = audit.Security("kafka_config_invalid", map[string]interface{}{
				"error": "KAFKA_INPUT_TOPICS required",
			})
		}
		return fmt.Errorf("kafka: KAFKA_INPUT_TOPICS required (comma-separated list)")
	}

	// Validate SASL credentials present
	if _, err := cm.GetStringRequired("KAFKA_SASL_USERNAME"); err != nil {
		if audit != nil {
			_ = audit.Security("kafka_config_invalid", map[string]interface{}{
				"error": "KAFKA_SASL_USERNAME required",
			})
		}
		return fmt.Errorf("kafka: KAFKA_SASL_USERNAME required: %w", err)
	}
	if _, err := cm.GetSecret("KAFKA_SASL_PASSWORD"); err != nil {
		if audit != nil {
			_ = audit.Security("kafka_config_invalid", map[string]interface{}{
				"error": "KAFKA_SASL_PASSWORD required",
			})
		}
		return fmt.Errorf("kafka: KAFKA_SASL_PASSWORD required: %w", err)
	}

	// Validate consumer group ID
	groupID := cm.GetString("KAFKA_CONSUMER_GROUP_ID", "")
	if groupID == "" {
		if audit != nil {
			_ = audit.Security("kafka_config_invalid", map[string]interface{}{
				"error": "KAFKA_CONSUMER_GROUP_ID required",
			})
		}
		return fmt.Errorf("kafka: KAFKA_CONSUMER_GROUP_ID required")
	}

	return nil
}

// BuildSaramaConfig creates a military-grade sarama.Config from utils.ConfigManager
// Enforces TLS/SASL authentication, idempotent producer, and fail-closed security
func BuildSaramaConfig(ctx context.Context, cm *utils.ConfigManager, log *utils.Logger, audit *utils.AuditLogger) (*sarama.Config, error) {
	// Validate configuration first (security-critical)
	if err := validateKafkaConfig(ctx, cm, audit); err != nil {
		return nil, err
	}

	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_9_0_0 // Latest stable Kafka protocol version

	// Connection timeouts
	timeout := cm.GetDuration("KAFKA_TIMEOUT", 30*time.Second)
	cfg.Net.DialTimeout = timeout
	cfg.Net.ReadTimeout = timeout
	cfg.Net.WriteTimeout = timeout

	// Environment detection
	envStr := cm.GetString("ENVIRONMENT", "production")
	env := Environment(envStr)

	// TLS Configuration (REQUIRED in production/staging)
	tlsEnabled := cm.GetBool("KAFKA_TLS_ENABLED", true)

	// Enforce TLS in production/staging (fail-closed)
	if (env == EnvProduction || env == EnvStaging) && !tlsEnabled {
		if audit != nil {
			_ = audit.Security("kafka_tls_required", map[string]interface{}{
				"environment": string(env),
				"tls_enabled": tlsEnabled,
			})
		}
		return nil, fmt.Errorf("kafka: TLS is REQUIRED in %s environment (fail-closed)", env)
	}

	if !tlsEnabled {
		log.WarnContext(ctx, "Kafka TLS DISABLED - this is INSECURE and should only be used in dev",
			utils.ZapString("environment", string(env)))
	}
	cfg.Net.TLS.Enable = tlsEnabled
	if tlsEnabled {
		cfg.Net.TLS.Config = &tls.Config{
			MinVersion: tls.VersionTLS12, // Allow TLS 1.2+ for cluster compatibility
			MaxVersion: tls.VersionTLS13, // Prefer TLS 1.3
		}
	}

	// SASL Authentication (REQUIRED in production)
	saslMechanism := cm.GetString("KAFKA_SASL_MECHANISM", "SCRAM-SHA-512")
	saslUsername, err := cm.GetStringRequired("KAFKA_SASL_USERNAME")
	if err != nil {
		return nil, fmt.Errorf("KAFKA_SASL_USERNAME required: %w", err)
	}
	saslPassword, err := cm.GetSecret("KAFKA_SASL_PASSWORD")
	if err != nil {
		return nil, fmt.Errorf("KAFKA_SASL_PASSWORD required: %w", err)
	}

	cfg.Net.SASL.Enable = true
	cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
	cfg.Net.SASL.User = saslUsername
	cfg.Net.SASL.Password = saslPassword

	switch saslMechanism {
	case "SCRAM-SHA-512":
		cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
		cfg.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &XDGSCRAMClient{HashGeneratorFcn: SHA512}
		}
	case "SCRAM-SHA-256":
		cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
		cfg.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &XDGSCRAMClient{HashGeneratorFcn: SHA256}
		}
	case "PLAIN":
		if !tlsEnabled {
			return nil, fmt.Errorf("SASL PLAIN without TLS is FORBIDDEN (credentials would be cleartext)")
		}
		cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		log.WarnContext(ctx, "SASL PLAIN enabled - use SCRAM for better security")
	default:
		return nil, fmt.Errorf("unsupported SASL mechanism: %s (use SCRAM-SHA-256, SCRAM-SHA-512, or PLAIN)", saslMechanism)
	}

	// Consumer Configuration
	cfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	cfg.Consumer.Offsets.Initial = sarama.OffsetNewest // Start from latest on first run
	offsetInitial := strings.ToLower(strings.TrimSpace(cm.GetString("KAFKA_CONSUMER_OFFSET_INITIAL", "")))
	if offsetInitial == "" {
		offsetInitial = strings.ToLower(strings.TrimSpace(cm.GetString("KAFKA_CONSUMER_AUTO_OFFSET_RESET", "")))
	}
	if offsetInitial == "" {
		offsetInitial = "latest"
	}
	// Acceptable values: earliest/oldest/beginning → OffsetOldest, anything else → OffsetNewest
	switch offsetInitial {
	case "earliest", "oldest", "beginning":
		cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	default:
		cfg.Consumer.Offsets.Initial = sarama.OffsetNewest
	}
	cfg.Consumer.MaxProcessingTime = cm.GetDuration("KAFKA_CONSUMER_MAX_PROCESSING_TIME", 30*time.Second)
	cfg.Consumer.Group.Session.Timeout = cm.GetDuration("KAFKA_CONSUMER_SESSION_TIMEOUT", 10*time.Second)
	cfg.Consumer.Group.Heartbeat.Interval = cm.GetDuration("KAFKA_CONSUMER_HEARTBEAT", 3*time.Second)
	cfg.Consumer.Return.Errors = true

	// Producer Configuration (Idempotent + Exactly-Once)
	cfg.Producer.Idempotent = cm.GetBool("KAFKA_PRODUCER_IDEMPOTENT", true)
	cfg.Producer.RequiredAcks = sarama.WaitForAll // -1 = all in-sync replicas must ack
	requiredAcks := cm.GetInt("KAFKA_PRODUCER_REQUIRED_ACKS", -1)
	cfg.Producer.RequiredAcks = sarama.RequiredAcks(requiredAcks)
	cfg.Producer.Retry.Max = cm.GetInt("KAFKA_PRODUCER_RETRIES", 3)
	cfg.Producer.Return.Successes = true
	cfg.Producer.Return.Errors = true

	// Idempotent producer requires MaxOpenRequests = 1
	if cfg.Producer.Idempotent {
		cfg.Net.MaxOpenRequests = 1
	}

	// Compression (optional for security, mandatory for performance)
	compression := cm.GetString("KAFKA_PRODUCER_COMPRESSION", "snappy")
	switch compression {
	case "gzip":
		cfg.Producer.Compression = sarama.CompressionGZIP
	case "snappy":
		cfg.Producer.Compression = sarama.CompressionSnappy
	case "lz4":
		cfg.Producer.Compression = sarama.CompressionLZ4
	case "zstd":
		cfg.Producer.Compression = sarama.CompressionZSTD
	case "none":
		cfg.Producer.Compression = sarama.CompressionNone
	default:
		return nil, fmt.Errorf("unsupported compression: %s (use gzip, snappy, lz4, zstd, none)", compression)
	}

	// Message Size Limits (DoS Protection)
	maxMessageSize := cm.GetInt("KAFKA_MAX_MESSAGE_SIZE", 1*1024*1024) // 1MB default
	cfg.Producer.MaxMessageBytes = maxMessageSize
	cfg.Consumer.Fetch.Max = int32(maxMessageSize)

	// Metadata refresh (detect broker changes)
	cfg.Metadata.Retry.Max = 3
	cfg.Metadata.Retry.Backoff = 250 * time.Millisecond
	cfg.Metadata.RefreshFrequency = 10 * time.Minute

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		if audit != nil {
			_ = audit.Security("kafka_sarama_config_invalid", map[string]interface{}{
				"error": err.Error(),
			})
		}
		return nil, fmt.Errorf("invalid sarama config: %w", err)
	}

	// Audit successful configuration
	if audit != nil {
		_ = audit.Info("kafka_config_built", map[string]interface{}{
			"environment":    string(env),
			"sasl_mechanism": saslMechanism,
			"tls_enabled":    tlsEnabled,
			"compression":    compression,
			"idempotent":     cfg.Producer.Idempotent,
		})
	}

	log.InfoContext(ctx, "Sarama config built",
		utils.ZapString("environment", string(env)),
		utils.ZapString("sasl_mechanism", saslMechanism),
		utils.ZapBool("tls_enabled", tlsEnabled),
		utils.ZapString("compression", compression),
		utils.ZapBool("idempotent", cfg.Producer.Idempotent))

	return cfg, nil
}
