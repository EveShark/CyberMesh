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

	"cybermesh/telemetry-layer/stream-processor/internal/aggregate"
	"cybermesh/telemetry-layer/stream-processor/internal/codec"
	"cybermesh/telemetry-layer/stream-processor/internal/derive"
	"cybermesh/telemetry-layer/stream-processor/internal/dlq"
	"cybermesh/telemetry-layer/stream-processor/internal/kafka"
	"cybermesh/telemetry-layer/stream-processor/internal/model"
	"cybermesh/telemetry-layer/stream-processor/internal/registry"
	"cybermesh/telemetry-layer/stream-processor/internal/validate"
	"cybermesh/telemetry-layer/stream-processor/utils"
	"github.com/IBM/sarama"
)

type config struct {
	TenantID       string
	WindowSeconds  int64
	FlushInterval  time.Duration
	ConsumeBackoff time.Duration
	FlowEncoding   string
	SourceType     string
	SourceID       string
	InputTopics    []string
	DebugTrace     bool

	DeepflowEnabled  bool
	DeepflowTopic    string
	DeepflowEncoding string
	DeepflowSubject  string

	SchemaRegistryURL           string
	SchemaRegistryEnabled       bool
	SchemaRegistrySubject       string
	SchemaRegistryAllowFallback bool
	SchemaRegistryUsername      string
	SchemaRegistryPassword      string

	Kafka            kafka.Config
	OutputTopic      string
	DerivationPolicy string
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

	inputTopics := parseTopics(
		cm.GetString("KAFKA_INPUT_TOPICS", ""),
		cm.GetString("KAFKA_INPUT_TOPIC", "telemetry.flow.v1"),
	)
	if len(inputTopics) == 0 {
		return config{}, errors.New("KAFKA_INPUT_TOPIC or KAFKA_INPUT_TOPICS is required")
	}
	deepflowEnabled := cm.GetBool("DEEPFLOW_FLOW_BRIDGE_ENABLED", false)
	deepflowTopic := cm.GetString("DEEPFLOW_TOPIC", "telemetry.deepflow.v1")
	if deepflowEnabled && !containsTopic(inputTopics, deepflowTopic) {
		inputTopics = append(inputTopics, deepflowTopic)
	}

	windowSeconds := int64(cm.GetInt("AGGREGATION_WINDOW_SEC", 10))
	if windowSeconds <= 0 {
		windowSeconds = 10
	}
	flushIntervalMS := cm.GetInt("FLUSH_INTERVAL_MS", int(windowSeconds*1000))
	if flushIntervalMS < 50 {
		flushIntervalMS = 50
	}
	consumeBackoffMS := cm.GetInt("CONSUMER_RETRY_BACKOFF_MS", 500)
	if consumeBackoffMS < 50 {
		consumeBackoffMS = 50
	}

	return config{
		TenantID:                    cm.GetString("TENANT_ID", "default"),
		WindowSeconds:               windowSeconds,
		FlushInterval:               time.Duration(flushIntervalMS) * time.Millisecond,
		ConsumeBackoff:              time.Duration(consumeBackoffMS) * time.Millisecond,
		FlowEncoding:                cm.GetString("FLOW_ENCODING", "json"),
		SourceType:                  cm.GetString("SOURCE_TYPE", "processor"),
		SourceID:                    cm.GetString("SOURCE_ID", ""),
		InputTopics:                 inputTopics,
		DebugTrace:                  cm.GetBool("DEBUG_FLOW_TRACE", false),
		DeepflowEnabled:             deepflowEnabled,
		DeepflowTopic:               deepflowTopic,
		DeepflowEncoding:            cm.GetString("DEEPFLOW_ENCODING", "proto"),
		DeepflowSubject:             cm.GetString("SCHEMA_REGISTRY_SUBJECT_DEEPFLOW", "telemetry-deepflow-v1"),
		SchemaRegistryURL:           cm.GetString("SCHEMA_REGISTRY_URL", ""),
		SchemaRegistryEnabled:       cm.GetBool("SCHEMA_REGISTRY_ENABLED", false),
		SchemaRegistrySubject:       cm.GetString("SCHEMA_REGISTRY_SUBJECT_FLOW", "telemetry-flow-v1"),
		SchemaRegistryAllowFallback: cm.GetBool("SCHEMA_REGISTRY_ALLOW_FALLBACK", false),
		SchemaRegistryUsername:      cm.GetString("SCHEMA_REGISTRY_USERNAME", ""),
		SchemaRegistryPassword:      cm.GetString("SCHEMA_REGISTRY_PASSWORD", ""),
		OutputTopic:                 cm.GetString("KAFKA_OUTPUT_TOPIC", "telemetry.flow.agg.v1"),
		DerivationPolicy:            cm.GetString("DERIVATION_POLICY", "none"),
		Kafka: kafka.Config{
			Brokers:       brokers,
			Topic:         cm.GetString("KAFKA_INPUT_TOPIC", "telemetry.flow.v1"),
			DLQTopic:      cm.GetString("KAFKA_DLQ_TOPIC", "telemetry.flow.v1.dlq"),
			ClientID:      cm.GetString("KAFKA_CLIENT_ID", "telemetry-stream-processor"),
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

	producer, err := kafka.NewProducer(cfg.Kafka, logger)
	if err != nil {
		logger.Fatal("kafka producer init failed", utils.ZapError(err))
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	consumer, err := newConsumer(cfg)
	if err != nil {
		logger.Fatal("kafka consumer init failed", utils.ZapError(err))
	}
	defer consumer.Close()

	agg := aggregate.New(cfg.WindowSeconds)
	ticker := time.NewTicker(cfg.FlushInterval)
	defer ticker.Stop()

	handler := &consumerHandler{
		cfg:            cfg,
		agg:            agg,
		producer:       producer,
		registryClient: registryClient,
		logger:         logger,
	}

	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			if err := consumer.Consume(ctx, cfg.InputTopics, handler); err != nil {
				logger.Warn("kafka consume error", utils.ZapError(err))
			}
			time.Sleep(cfg.ConsumeBackoff)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			flushAggregates(logger, producer, agg, cfg, registryClient)
		}
	}
}

type consumerHandler struct {
	cfg            config
	agg            *aggregate.Aggregator
	producer       *kafka.Producer
	registryClient *registry.Client
	logger         *utils.Logger
}

func (h *consumerHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *consumerHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *consumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		payload := msg.Value
		if h.cfg.DebugTrace {
			h.logger.Info("flow message received", utils.ZapString("topic", msg.Topic), utils.ZapInt32("partition", msg.Partition))
		}
		if h.cfg.DeepflowEnabled && msg.Topic == h.cfg.DeepflowTopic {
			if err := h.handleDeepflow(session, msg, payload); err != nil {
				h.logger.Warn("deepflow message dropped", utils.ZapError(err))
			}
			continue
		}
		if err := h.handleFlowAggregate(session, msg, payload); err != nil {
			h.logger.Warn("flow message dropped", utils.ZapError(err))
		}
	}
	return nil
}

func (h *consumerHandler) handleDeepflow(session sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage, payload []byte) error {
	if h.cfg.SchemaRegistryEnabled && h.cfg.DeepflowEncoding != "json" {
		schemaID := extractSchemaID(msg.Headers)
		if schemaID == 0 && !h.cfg.SchemaRegistryAllowFallback {
			env := dlq.NewEnvelope("dlq.v1", "stream-processor", "DEEPFLOW_SCHEMA_MISSING", "schema_id missing", payload)
			_ = h.producer.SendDLQ(env)
			session.MarkMessage(msg, "")
			return errors.New("deepflow schema_id missing")
		}
		if schemaID != 0 {
			latestID, err := h.registryClient.SubjectLatestID(context.Background(), h.cfg.DeepflowSubject)
			if err != nil || schemaID != latestID {
				env := dlq.NewEnvelope("dlq.v1", "stream-processor", "DEEPFLOW_SCHEMA_MISMATCH", "schema_id mismatch", payload)
				_ = h.producer.SendDLQ(env)
				session.MarkMessage(msg, "")
				return errors.New("deepflow schema_id mismatch")
			}
		}
	}

	event, err := codec.DecodeDeepFlow(payload, h.cfg.DeepflowEncoding)
	if err != nil {
		env := dlq.NewEnvelope("dlq.v1", "stream-processor", "DEEPFLOW_DECODE", err.Error(), payload)
		_ = h.producer.SendDLQ(env)
		session.MarkMessage(msg, "")
		return err
	}
	if err := validateFlowEvent(event); err != nil {
		env := dlq.NewEnvelope("dlq.v1", "stream-processor", "DEEPFLOW_INVALID", err.Error(), payload)
		_ = h.producer.SendDLQ(env)
		session.MarkMessage(msg, "")
		return err
	}
	h.agg.Add(event)
	session.MarkMessage(msg, "")
	return nil
}

func (h *consumerHandler) handleFlowAggregate(session sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage, payload []byte) error {
	flow, err := codec.DecodeFlowAggregate(payload, h.cfg.FlowEncoding)
	if err != nil {
		env := dlq.NewEnvelope("dlq.v1", "stream-processor", "FLOW_DECODE", err.Error(), payload)
		_ = h.producer.SendDLQ(env)
		session.MarkMessage(msg, "")
		return err
	}
	if h.cfg.SchemaRegistryEnabled && h.cfg.FlowEncoding != "json" {
		schemaID := extractSchemaID(msg.Headers)
		if schemaID == 0 && !h.cfg.SchemaRegistryAllowFallback {
			env := dlq.NewEnvelope("dlq.v1", "stream-processor", "SCHEMA_MISSING", "schema_id missing", payload)
			_ = h.producer.SendDLQ(env)
			session.MarkMessage(msg, "")
			return errors.New("flow schema_id missing")
		}
		if schemaID != 0 {
			latestID, err := h.registryClient.SubjectLatestID(context.Background(), h.cfg.SchemaRegistrySubject)
			if err != nil || schemaID != latestID {
				env := dlq.NewEnvelope("dlq.v1", "stream-processor", "SCHEMA_MISMATCH", "schema_id mismatch", payload)
				_ = h.producer.SendDLQ(env)
				session.MarkMessage(msg, "")
				return errors.New("flow schema_id mismatch")
			}
		}
	}
	if h.cfg.SourceType != "" {
		flow.SourceType = h.cfg.SourceType
	}
	if h.cfg.SourceID != "" {
		flow.SourceID = h.cfg.SourceID
	}
	h.agg.Add(model.FlowEvent{
		Timestamp:    flow.Timestamp,
		TenantID:     flow.TenantID,
		SrcIP:        flow.SrcIP,
		DstIP:        flow.DstIP,
		SrcPort:      flow.SrcPort,
		DstPort:      flow.DstPort,
		Proto:        flow.Proto,
		Bytes:        flow.BytesFwd + flow.BytesBwd,
		Packets:      flow.PktsFwd + flow.PktsBwd,
		Direction:    "egress",
		Identity:     flow.Identity,
		Verdict:      flow.Verdict,
		MetricsKnown: flow.MetricsKnown,
		SourceType:   flow.SourceType,
		SourceID:     flow.SourceID,
		TimingKnown:  flow.TimingKnown,
		FlagsKnown:   flow.FlagsKnown,
		FlowIatMean:  flow.FlowIatMean,
		FlowIatStd:   flow.FlowIatStd,
		FlowIatMax:   flow.FlowIatMax,
		FlowIatMin:   flow.FlowIatMin,
		FwdIatTot:    flow.FwdIatTot,
		FwdIatMean:   flow.FwdIatMean,
		FwdIatStd:    flow.FwdIatStd,
		FwdIatMax:    flow.FwdIatMax,
		FwdIatMin:    flow.FwdIatMin,
		BwdIatTot:    flow.BwdIatTot,
		BwdIatMean:   flow.BwdIatMean,
		BwdIatStd:    flow.BwdIatStd,
		BwdIatMax:    flow.BwdIatMax,
		BwdIatMin:    flow.BwdIatMin,
		ActiveMean:   flow.ActiveMean,
		ActiveStd:    flow.ActiveStd,
		ActiveMax:    flow.ActiveMax,
		ActiveMin:    flow.ActiveMin,
		IdleMean:     flow.IdleMean,
		IdleStd:      flow.IdleStd,
		IdleMax:      flow.IdleMax,
		IdleMin:      flow.IdleMin,
		FinFlagCnt:   flow.FinFlagCnt,
		SynFlagCnt:   flow.SynFlagCnt,
		RstFlagCnt:   flow.RstFlagCnt,
		PshFlagCnt:   flow.PshFlagCnt,
		AckFlagCnt:   flow.AckFlagCnt,
		UrgFlagCnt:   flow.UrgFlagCnt,
		CweFlagCount: flow.CweFlagCount,
		EceFlagCnt:   flow.EceFlagCnt,
	})
	session.MarkMessage(msg, "")
	return nil
}

func newConsumer(cfg config) (sarama.ConsumerGroup, error) {
	saramaConfig, err := kafka.BuildSaramaConfig(cfg.Kafka)
	if err != nil {
		return nil, err
	}
	saramaConfig.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRange
	saramaConfig.Consumer.Offsets.Initial = sarama.OffsetNewest
	groupID := "telemetry-stream-processor"
	if cfg.Kafka.ClientID != "" {
		groupID = cfg.Kafka.ClientID
	}
	return sarama.NewConsumerGroup(cfg.Kafka.Brokers, groupID, saramaConfig)
}

func extractSchemaID(headers []*sarama.RecordHeader) int {
	for _, h := range headers {
		if string(h.Key) == "schema_id" {
			var id int
			_, _ = fmt.Sscanf(string(h.Value), "%d", &id)
			return id
		}
	}
	return 0
}

func validateFlowEvent(event model.FlowEvent) error {
	if event.TenantID == "" {
		return errors.New("tenant_id required")
	}
	if event.SrcIP == "" || event.DstIP == "" {
		return errors.New("src_ip and dst_ip required")
	}
	if event.SrcPort == 0 || event.DstPort == 0 {
		return errors.New("src_port and dst_port required")
	}
	if event.Proto == 0 {
		return errors.New("proto required")
	}
	return nil
}

func parseTopics(primary string, fallback string) []string {
	raw := strings.TrimSpace(primary)
	if raw == "" {
		raw = strings.TrimSpace(fallback)
	}
	if raw == "" {
		return nil
	}
	var topics []string
	for _, part := range strings.Split(raw, ",") {
		topic := strings.TrimSpace(part)
		if topic == "" {
			continue
		}
		if !containsTopic(topics, topic) {
			topics = append(topics, topic)
		}
	}
	return topics
}

func containsTopic(topics []string, candidate string) bool {
	for _, topic := range topics {
		if topic == candidate {
			return true
		}
	}
	return false
}

func flushAggregates(logger *utils.Logger, producer *kafka.Producer, agg *aggregate.Aggregator, cfg config, registryClient *registry.Client) {
	flushed := agg.Flush(time.Now())
	if cfg.DebugTrace && len(flushed) > 0 {
		logger.Info("flow aggregates flushed", utils.ZapInt("count", len(flushed)))
	}
	for _, out := range flushed {
		if err := validate.FlowAggregate(out); err != nil {
			env := dlq.NewEnvelope("dlq.v1", "stream-processor", "FLOW_INVALID", err.Error(), []byte(out.FlowID))
			_ = producer.SendDLQ(env)
			continue
		}
		if cfg.DerivationPolicy != "" && cfg.DerivationPolicy != "none" {
			_ = derive.ApplyTimingDerivation(&out, cfg.DerivationPolicy)
		}
		payload, err := codec.EncodeFlowAggregate(out, cfg.FlowEncoding)
		if err != nil {
			env := dlq.NewEnvelope("dlq.v1", "stream-processor", "FLOW_MARSHAL", err.Error(), []byte(out.FlowID))
			_ = producer.SendDLQ(env)
			continue
		}
		var headers []sarama.RecordHeader
		if registryClient != nil && registryClient.Enabled() && cfg.FlowEncoding != "json" {
			schemaID, err := registryClient.SubjectLatestID(context.Background(), cfg.SchemaRegistrySubject)
			if err != nil {
				env := dlq.NewEnvelope("dlq.v1", "stream-processor", "SCHEMA_REGISTRY", err.Error(), []byte(out.FlowID))
				_ = producer.SendDLQ(env)
				continue
			}
			headers = append(headers, sarama.RecordHeader{Key: []byte("schema_id"), Value: []byte(fmt.Sprintf("%d", schemaID))})
		}
		if err := producer.SendToTopic(cfg.OutputTopic, out.FlowID, payload, headers...); err != nil {
			env := dlq.NewEnvelope("dlq.v1", "stream-processor", "KAFKA_SEND", err.Error(), []byte(out.FlowID))
			_ = producer.SendDLQ(env)
			logger.Warn("kafka send failed", utils.ZapError(err))
		}
	}
}
