package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cybermesh/telemetry-layer/adapters/internal/kafka"
	"cybermesh/telemetry-layer/adapters/internal/observability"
	"cybermesh/telemetry-layer/adapters/internal/spectra"
	"cybermesh/telemetry-layer/adapters/utils"
)

func main() {
	logger, _ := utils.NewLogger(utils.DefaultLogConfig())
	cm, err := utils.NewConfigManager(&utils.ConfigManagerConfig{Logger: logger})
	if err != nil {
		logger.Fatal("config init failed", utils.ZapError(err))
	}

	shutdownOTel, otelErr := observability.InitFromEnv(context.Background(), "cybermesh-telemetry-app-events-adapter")
	if otelErr != nil {
		logger.Warn("OpenTelemetry tracing disabled due to init error", utils.ZapError(otelErr))
	}
	defer func() {
		if shutdownOTel != nil {
			_ = shutdownOTel(context.Background())
		}
	}()

	kcfg, scfg, err := loadConfig(cm)
	if err != nil {
		logger.Fatal("config invalid", utils.ZapError(err))
	}
	producer, err := kafka.NewProducer(kcfg, logger)
	if err != nil {
		logger.Fatal("kafka producer init failed", utils.ZapError(err))
	}
	defer producer.Close()

	svc := spectra.NewService(scfg, producer, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := svc.Run(ctx); err != nil {
		logger.Fatal("spectra adapter failed", utils.ZapError(err))
	}
}

func loadConfig(cm *utils.ConfigManager) (kafka.Config, spectra.Config, error) {
	brokers := cm.GetStringSlice("KAFKA_BOOTSTRAP_SERVERS", nil)
	if len(brokers) == 0 {
		raw := cm.GetString("KAFKA_BOOTSTRAP_SERVERS", "")
		if strings.TrimSpace(raw) == "" {
			raw = cm.GetString("KAFKA_BROKERS", "")
		}
		if strings.TrimSpace(raw) != "" {
			brokers = strings.Split(raw, ",")
		}
	}
	if len(brokers) == 0 {
		return kafka.Config{}, spectra.Config{}, errors.New("KAFKA_BOOTSTRAP_SERVERS is required")
	}

	kcfg := kafka.Config{
		Brokers:       brokers,
		Topic:         cm.GetString("KAFKA_TOPIC", "telemetry.flow.v1"),
		DLQTopic:      cm.GetString("KAFKA_DLQ_TOPIC", "telemetry.flow.v1.dlq"),
		ClientID:      cm.GetString("KAFKA_CLIENT_ID", "telemetry-app-events-adapter"),
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
	}

	scfg := spectra.Config{
		ListenAddr:               getStringCompat(cm, "APP_EVENTS_LISTEN_ADDR", "SPECTRA_LISTEN_ADDR", ":8087"),
		WebhookPath:              getStringCompat(cm, "APP_EVENTS_WEBHOOK_PATH", "SPECTRA_WEBHOOK_PATH", "/ingest/app-events"),
		WebhookSecret:            getStringCompat(cm, "APP_EVENTS_WEBHOOK_SECRET", "SPECTRA_WEBHOOK_SECRET", ""),
		WebhookRequireSignature:  getBoolCompat(cm, "APP_EVENTS_WEBHOOK_REQUIRE_SIGNATURE", "SPECTRA_WEBHOOK_REQUIRE_SIGNATURE", true),
		SignatureTimestampHeader: getStringCompat(cm, "APP_EVENTS_SIGNATURE_TS_HEADER", "SPECTRA_SIGNATURE_TS_HEADER", "X-CM-Timestamp"),
		SignatureHeader:          getStringCompat(cm, "APP_EVENTS_SIGNATURE_HEADER", "SPECTRA_SIGNATURE_HEADER", "X-CM-Signature"),
		ReplayWindow:             getDurationCompat(cm, "APP_EVENTS_REPLAY_WINDOW", "SPECTRA_REPLAY_WINDOW", 5*time.Minute),
		DedupeTTL:                getDurationCompat(cm, "APP_EVENTS_DEDUPE_TTL", "SPECTRA_DEDUPE_TTL", 10*time.Minute),
		FlowEncoding:             getStringCompat(cm, "APP_EVENTS_FLOW_ENCODING", "SPECTRA_FLOW_ENCODING", "json"),
		DeepFlowEncoding:         getStringCompat(cm, "APP_EVENTS_DEEPFLOW_ENCODING", "SPECTRA_DEEPFLOW_ENCODING", "json"),
		FlowTopic:                getStringCompat(cm, "APP_EVENTS_FLOW_TOPIC", "SPECTRA_FLOW_TOPIC", "telemetry.flow.v1"),
		DeepFlowTopic:            getStringCompat(cm, "APP_EVENTS_DEEPFLOW_TOPIC", "SPECTRA_DEEPFLOW_TOPIC", "telemetry.deepflow.v1"),
		RequestMirrorDeepFlow:    getBoolCompat(cm, "APP_EVENTS_REQUEST_MIRROR_DEEPFLOW", "SPECTRA_REQUEST_MIRROR_DEEPFLOW", true),
		PollURL:                  getStringCompat(cm, "APP_EVENTS_POLL_URL", "SPECTRA_POLL_URL", ""),
		PollInterval:             getDurationCompat(cm, "APP_EVENTS_POLL_INTERVAL", "SPECTRA_POLL_INTERVAL", 30*time.Second),
		PollAuthBearer:           getStringCompat(cm, "APP_EVENTS_POLL_BEARER", "SPECTRA_POLL_BEARER", ""),
		AppIDDefault:             getStringCompat(cm, "APP_EVENTS_APP_ID_DEFAULT", "SPECTRA_APP_ID_DEFAULT", "spectra-productos"),
		SourceIDDefault:          getStringCompat(cm, "APP_EVENTS_SOURCE_ID_DEFAULT", "SPECTRA_SOURCE_ID_DEFAULT", "spectra"),
	}
	return kcfg, scfg, nil
}

func pickSASLUser(cm *utils.ConfigManager) string {
	user := cm.GetString("KAFKA_SASL_USERNAME", "")
	if strings.TrimSpace(user) != "" {
		return user
	}
	return cm.GetString("KAFKA_SASL_USER", "")
}

func getStringCompat(cm *utils.ConfigManager, primary, legacy, def string) string {
	if v, ok := os.LookupEnv(primary); ok && strings.TrimSpace(v) != "" {
		return v
	}
	if v, ok := os.LookupEnv(legacy); ok && strings.TrimSpace(v) != "" {
		return v
	}
	if v := cm.GetString(primary, ""); strings.TrimSpace(v) != "" {
		return v
	}
	return cm.GetString(legacy, def)
}

func getBoolCompat(cm *utils.ConfigManager, primary, legacy string, def bool) bool {
	if v, ok := os.LookupEnv(primary); ok && strings.TrimSpace(v) != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			return parsed
		}
	}
	if v, ok := os.LookupEnv(legacy); ok && strings.TrimSpace(v) != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			return parsed
		}
	}
	if _, ok := os.LookupEnv(primary); ok {
		return cm.GetBool(primary, def)
	}
	if _, ok := os.LookupEnv(legacy); ok {
		return cm.GetBool(legacy, def)
	}
	return cm.GetBool(primary, cm.GetBool(legacy, def))
}

func getDurationCompat(cm *utils.ConfigManager, primary, legacy string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(primary); ok && strings.TrimSpace(v) != "" {
		if parsed, err := time.ParseDuration(v); err == nil {
			return parsed
		}
	}
	if v, ok := os.LookupEnv(legacy); ok && strings.TrimSpace(v) != "" {
		if parsed, err := time.ParseDuration(v); err == nil {
			return parsed
		}
	}
	if _, ok := os.LookupEnv(primary); ok {
		return cm.GetDuration(primary, def)
	}
	if _, ok := os.LookupEnv(legacy); ok {
		return cm.GetDuration(legacy, def)
	}
	return cm.GetDuration(primary, cm.GetDuration(legacy, def))
}
