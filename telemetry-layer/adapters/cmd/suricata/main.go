package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"cybermesh/telemetry-layer/adapters/internal/deepflow"
	"cybermesh/telemetry-layer/adapters/internal/kafka"
	"cybermesh/telemetry-layer/adapters/internal/telemetrymetrics"
	"cybermesh/telemetry-layer/adapters/utils"
)

func loadConfig(cm *utils.ConfigManager) (deepflow.Config, error) {
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
		return deepflow.Config{}, errors.New("KAFKA_BOOTSTRAP_SERVERS is required")
	}
	return deepflow.Config{
		InputPath:             cm.GetString("INPUT_PATH", "-"),
		RecordFormat:          cm.GetString("RECORD_FORMAT", "suricata_eve"),
		Encoding:              cm.GetString("DEEPFLOW_ENCODING", "proto"),
		TenantID:              cm.GetString("TENANT_ID", ""),
		SensorID:              cm.GetString("SENSOR_ID", ""),
		SourceType:            cm.GetString("SOURCE_TYPE", "suricata"),
		RegistryURL:           cm.GetString("SCHEMA_REGISTRY_URL", ""),
		RegistryEnabled:       cm.GetBool("SCHEMA_REGISTRY_ENABLED", false),
		RegistrySubject:       cm.GetString("SCHEMA_REGISTRY_SUBJECT_DEEPFLOW", "telemetry-deepflow-v1"),
		RegistryAllowFallback: cm.GetBool("SCHEMA_REGISTRY_ALLOW_FALLBACK", false),
		RegistryUsername:      cm.GetString("SCHEMA_REGISTRY_USERNAME", ""),
		RegistryPassword:      cm.GetString("SCHEMA_REGISTRY_PASSWORD", ""),
		Kafka: kafka.Config{
			Brokers:       brokers,
			Topic:         cm.GetString("DEEPFLOW_TOPIC", "telemetry.deepflow.v1"),
			DLQTopic:      cm.GetString("DEEPFLOW_DLQ_TOPIC", "telemetry.deepflow.v1.dlq"),
			ClientID:      cm.GetString("KAFKA_CLIENT_ID", "telemetry-suricata-adapter"),
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

	if err := deepflow.Run(ctx, cfg, logger); err != nil {
		logger.Fatal("adapter failed", utils.ZapError(err))
	}
}
