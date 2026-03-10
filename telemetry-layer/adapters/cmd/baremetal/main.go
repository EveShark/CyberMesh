package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"cybermesh/telemetry-layer/adapters/internal/adapter"
	"cybermesh/telemetry-layer/adapters/internal/kafka"
	"cybermesh/telemetry-layer/adapters/internal/telemetrymetrics"
	"cybermesh/telemetry-layer/adapters/utils"
)

func loadConfig(cm *utils.ConfigManager) (adapter.Config, error) {
	brokers := cm.GetStringSlice("KAFKA_BOOTSTRAP_SERVERS", nil)
	if len(brokers) == 0 {
		raw := cm.GetString("KAFKA_BOOTSTRAP_SERVERS", "")
		if strings.TrimSpace(raw) == "" {
			raw = cm.GetString("KAFKA_BROKERS", "")
		}
		if raw != "" {
			brokers = strings.Split(raw, ",")
		}
	}
	if len(brokers) == 0 {
		return adapter.Config{}, errors.New("KAFKA_BOOTSTRAP_SERVERS is required")
	}
	return adapter.Config{
		WindowSeconds:         int64(cm.GetInt("AGGREGATION_WINDOW_SEC", 10)),
		FlowEncoding:          cm.GetString("FLOW_ENCODING", "json"),
		SourceType:            cm.GetString("SOURCE_TYPE", "bare_metal"),
		SourceID:              cm.GetString("SOURCE_ID", ""),
		TenantID:              cm.GetString("TENANT_ID", ""),
		InputPath:             cm.GetString("INPUT_PATH", "-"),
		RecordFormat:          cm.GetString("RECORD_FORMAT", "normalized"),
		RecordMappingPath:     cm.GetString("RECORD_MAPPING_PATH", ""),
		IPFIXListenAddr:       cm.GetString("IPFIX_LISTEN_ADDR", ":2055"),
		IPFIXMaxPacketBytes:   cm.GetInt("IPFIX_MAX_PACKET_BYTES", 65507),
		RegistryURL:           cm.GetString("SCHEMA_REGISTRY_URL", ""),
		RegistryEnabled:       cm.GetBool("SCHEMA_REGISTRY_ENABLED", false),
		RegistrySubject:       cm.GetString("SCHEMA_REGISTRY_SUBJECT_FLOW", "telemetry-flow-v1"),
		RegistryAllowFallback: cm.GetBool("SCHEMA_REGISTRY_ALLOW_FALLBACK", false),
		RegistryUsername:      cm.GetString("SCHEMA_REGISTRY_USERNAME", ""),
		RegistryPassword:      cm.GetString("SCHEMA_REGISTRY_PASSWORD", ""),
		Kafka: kafka.Config{
			Brokers:       brokers,
			Topic:         cm.GetString("KAFKA_TOPIC", "telemetry.flow.v1"),
			DLQTopic:      cm.GetString("KAFKA_DLQ_TOPIC", "telemetry.flow.v1.dlq"),
			ClientID:      cm.GetString("KAFKA_CLIENT_ID", "telemetry-baremetal-adapter"),
			RequiredAcks:  cm.GetString("KAFKA_REQUIRED_ACKS", "all"),
			Compression:   cm.GetString("KAFKA_COMPRESSION", "none"),
			TLSEnabled:    cm.GetBool("KAFKA_TLS_ENABLED", false),
			TLSCAPath:     cm.GetString("KAFKA_TLS_CA_PATH", ""),
			TLSCertPath:   cm.GetString("KAFKA_TLS_CERT_PATH", ""),
			TLSKeyPath:    cm.GetString("KAFKA_TLS_KEY_PATH", ""),
			SASLEnabled:   cm.GetBool("KAFKA_SASL_ENABLED", false),
			SASLMechanism: cm.GetString("KAFKA_SASL_MECHANISM", "plain"),
			SASLUser:      pickSASLUser(cm),
			SASLPassword:  cm.GetString("KAFKA_SASL_PASSWORD", ""),
		},
	}, nil
}

func pickSASLUser(cm *utils.ConfigManager) string {
	user := cm.GetString("KAFKA_SASL_USERNAME", "")
	if strings.TrimSpace(user) != "" {
		return user
	}
	return cm.GetString("KAFKA_SASL_USER", "")
}

func main() {
	logger, _ := utils.NewLogger(utils.DefaultLogConfig())
	cm, err := utils.NewConfigManager(&utils.ConfigManagerConfig{Logger: logger})
	if err != nil {
		logger.Fatal("config init failed", utils.ZapError(err))
	}
	cfg, err := loadConfig(cm)
	if err != nil {
		logger.Fatal("config invalid", utils.ZapError(err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	telemetrymetrics.StartServer(ctx, cm.GetString("METRICS_ADDR", ":9102"), logger)

	if err := adapter.Run(ctx, cfg, logger); err != nil {
		logger.Fatal("adapter failed", utils.ZapError(err))
	}
}
