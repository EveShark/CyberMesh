package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cybermesh/telemetry-layer/ingest-bridge/internal/aggregate"
	"cybermesh/telemetry-layer/ingest-bridge/internal/codec"
	"cybermesh/telemetry-layer/ingest-bridge/internal/dlq"
	"cybermesh/telemetry-layer/ingest-bridge/internal/hubble"
	"cybermesh/telemetry-layer/ingest-bridge/internal/kafka"
	"cybermesh/telemetry-layer/ingest-bridge/internal/metrics"
	"cybermesh/telemetry-layer/ingest-bridge/internal/model"
	"cybermesh/telemetry-layer/ingest-bridge/internal/registry"
	"cybermesh/telemetry-layer/ingest-bridge/internal/validate"
	"cybermesh/telemetry-layer/ingest-bridge/utils"
	"github.com/IBM/sarama"
)

type config struct {
	TenantID               string
	WindowSeconds          int64
	MetricsAddr            string
	FlowEncoding           string
	SourceType             string
	SourceID               string
	SchemaRegistryURL      string
	SchemaRegistryEnabled  bool
	SchemaRegistrySubject  string
	SchemaRegistryUsername string
	SchemaRegistryPassword string
	Hubble                 hubble.Config
	Kafka                  kafka.Config
}

func loadConfig(cm *utils.ConfigManager) (config, error) {
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
		return config{}, errors.New("KAFKA_BOOTSTRAP_SERVERS is required")
	}

	return config{
		TenantID:               cm.GetString("TENANT_ID", "default"),
		WindowSeconds:          int64(cm.GetInt("AGGREGATION_WINDOW_SEC", 10)),
		MetricsAddr:            cm.GetString("METRICS_ADDR", ":9107"),
		FlowEncoding:           cm.GetString("FLOW_ENCODING", "json"),
		SourceType:             cm.GetString("SOURCE_TYPE", "k8s_cilium"),
		SourceID:               cm.GetString("SOURCE_ID", ""),
		SchemaRegistryURL:      cm.GetString("SCHEMA_REGISTRY_URL", ""),
		SchemaRegistryEnabled:  cm.GetBool("SCHEMA_REGISTRY_ENABLED", false),
		SchemaRegistrySubject:  cm.GetString("SCHEMA_REGISTRY_SUBJECT_FLOW", "telemetry-flow-v1"),
		SchemaRegistryUsername: cm.GetString("SCHEMA_REGISTRY_USERNAME", ""),
		SchemaRegistryPassword: cm.GetString("SCHEMA_REGISTRY_PASSWORD", ""),
		Hubble: hubble.Config{
			Address:         cm.GetString("HUBBLE_ADDRESS", "127.0.0.1:4245"),
			TLSEnabled:      cm.GetBool("HUBBLE_TLS_ENABLED", false),
			TLSCAPath:       cm.GetString("HUBBLE_TLS_CA_PATH", ""),
			TLSCertPath:     cm.GetString("HUBBLE_TLS_CERT_PATH", ""),
			TLSKeyPath:      cm.GetString("HUBBLE_TLS_KEY_PATH", ""),
			TLSServerName:   cm.GetString("HUBBLE_TLS_SERVER_NAME", ""),
			Follow:          cm.GetBool("HUBBLE_FOLLOW", true),
			SinceSeconds:    int64(cm.GetInt("HUBBLE_SINCE_SECONDS", 0)),
			MaxFlowsPerRecv: cm.GetUint64("HUBBLE_MAX_FLOWS_PER_RECV", 0),
		},
		Kafka: kafka.Config{
			Brokers:       brokers,
			Topic:         cm.GetString("KAFKA_TOPIC", "telemetry.flow.v1"),
			DLQTopic:      cm.GetString("KAFKA_DLQ_TOPIC", "telemetry.flow.v1.dlq"),
			ClientID:      cm.GetString("KAFKA_CLIENT_ID", "telemetry-ingest-bridge"),
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

	m := metrics.New()
	m.Register()
	go metrics.StartServer(cfg.MetricsAddr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	producer, err := kafka.NewProducer(cfg.Kafka, logger)
	if err != nil {
		logger.Fatal("kafka init failed", utils.ZapError(err))
	}
	defer producer.Close()

	registryClient := registry.New(registry.Config{
		Enabled:  cfg.SchemaRegistryEnabled,
		URL:      cfg.SchemaRegistryURL,
		Username: cfg.SchemaRegistryUsername,
		Password: cfg.SchemaRegistryPassword,
		Timeout:  5 * time.Second,
		CacheTTL: 5 * time.Minute,
	})

	hubbleClient, err := hubble.Dial(ctx, cfg.Hubble)
	if err != nil {
		logger.Fatal("hubble dial failed", utils.ZapError(err))
	}
	defer hubbleClient.Close()

	agg := aggregate.New(cfg.WindowSeconds)
	ticker := time.NewTicker(time.Duration(cfg.WindowSeconds) * time.Second)
	defer ticker.Stop()

	events := make(chan model.FlowEvent, 1024)
	go readFlows(ctx, logger, hubbleClient, cfg, producer, events, m)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			flushAggregates(logger, producer, agg, cfg.FlowEncoding, cfg, registryClient, m)
		case ev := <-events:
			agg.Add(ev)
		}
	}
}

func readFlows(ctx context.Context, logger *utils.Logger, client *hubble.Client, cfg config, producer *kafka.Producer, events chan<- model.FlowEvent, m *metrics.Metrics) {
	for {
		if ctx.Err() != nil {
			return
		}
		stream, err := client.Stream(ctx, cfg.Hubble)
		if err != nil {
			logger.Error("hubble stream failed", utils.ZapError(err))
			time.Sleep(2 * time.Second)
			continue
		}
		for {
			resp, err := stream.Recv()
			if err != nil {
				logger.Error("hubble recv failed", utils.ZapError(err))
				if m != nil {
					m.EventsInvalid.Inc()
				}
				time.Sleep(2 * time.Second)
				break
			}
			ev, err := hubble.ToFlowEvent(resp.GetFlow(), cfg.TenantID)
			if err != nil {
				env := dlq.NewEnvelope("dlq.v1", "ingest-bridge", "FLOW_PARSE", err.Error(), []byte(err.Error()))
				_ = producer.SendDLQ(env)
				if m != nil {
					m.EventsInvalid.Inc()
					m.DLQTotal.Inc()
				}
				continue
			}
			ev.SourceType = cfg.SourceType
			ev.SourceID = cfg.SourceID
			if ev.Bytes > 0 || ev.Packets > 0 {
				ev.MetricsKnown = true
			}
			if m != nil {
				m.EventsTotal.Inc()
			}
			select {
			case events <- ev:
			case <-ctx.Done():
				return
			}
		}
	}
}

func flushAggregates(logger *utils.Logger, producer *kafka.Producer, agg *aggregate.Aggregator, encoding string, cfg config, registryClient *registry.Client, m *metrics.Metrics) {
	for _, out := range agg.Flush(time.Now()) {
		if err := validate.FlowAggregate(out); err != nil {
			env := dlq.NewEnvelope("dlq.v1", "ingest-bridge", "FLOW_INVALID", err.Error(), []byte(out.FlowID))
			_ = producer.SendDLQ(env)
			if m != nil {
				m.EventsInvalid.Inc()
				m.DLQTotal.Inc()
			}
			continue
		}
		payload, err := codec.EncodeFlowAggregate(out, encoding)
		if err != nil {
			env := dlq.NewEnvelope("dlq.v1", "ingest-bridge", "FLOW_MARSHAL", err.Error(), []byte(out.FlowID))
			_ = producer.SendDLQ(env)
			if m != nil {
				m.EventsInvalid.Inc()
				m.DLQTotal.Inc()
			}
			continue
		}
		var headers []sarama.RecordHeader
		if registryClient != nil && registryClient.Enabled() {
			schemaID, err := registryClient.SubjectLatestID(context.Background(), cfg.SchemaRegistrySubject)
			if err != nil {
				env := dlq.NewEnvelope("dlq.v1", "ingest-bridge", "SCHEMA_REGISTRY", err.Error(), []byte(out.FlowID))
				_ = producer.SendDLQ(env)
				if m != nil {
					m.EventsInvalid.Inc()
					m.DLQTotal.Inc()
				}
				continue
			}
			headers = append(headers, sarama.RecordHeader{Key: []byte("schema_id"), Value: []byte(fmt.Sprintf("%d", schemaID))})
		}
		if err := producer.Send(out.FlowID, payload, headers...); err != nil {
			env := dlq.NewEnvelope("dlq.v1", "ingest-bridge", "KAFKA_SEND", err.Error(), []byte(out.FlowID))
			_ = producer.SendDLQ(env)
			logger.Warn("kafka send failed", utils.ZapError(err))
			if m != nil {
				m.DLQTotal.Inc()
			}
		} else if m != nil {
			m.AggregatesTotal.Inc()
		}
	}
}
