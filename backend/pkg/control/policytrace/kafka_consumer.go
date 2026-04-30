package policytrace

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/observability"
	"backend/pkg/utils"
	"github.com/IBM/sarama"
)

type KafkaIngressConfig struct {
	Enabled     bool
	Brokers     []string
	Topic       string
	GroupID     string
	LogThrottle time.Duration
}

func LoadKafkaIngressConfig(cm *utils.ConfigManager) (KafkaIngressConfig, error) {
	if cm == nil {
		return KafkaIngressConfig{}, fmt.Errorf("policy trace ingress: config manager required")
	}
	cfg := KafkaIngressConfig{
		Enabled:     cm.GetBool("KAFKA_TRACE_EVENTS_ENABLED", false),
		Brokers:     cm.GetStringSlice("KAFKA_BROKERS", nil),
		Topic:       strings.TrimSpace(cm.GetString("TOPIC_CONTROL_TRACE_EVENTS", "control.trace.events.v1")),
		GroupID:     strings.TrimSpace(cm.GetString("KAFKA_TRACE_EVENTS_GROUP_ID", "backend-control-trace-events")),
		LogThrottle: cm.GetDuration("KAFKA_TRACE_EVENTS_LOG_THROTTLE", 5*time.Second),
	}
	if !cfg.Enabled {
		return cfg, nil
	}
	if len(cfg.Brokers) == 0 {
		return KafkaIngressConfig{}, fmt.Errorf("policy trace ingress: KAFKA_BROKERS required")
	}
	if cfg.Topic == "" {
		return KafkaIngressConfig{}, fmt.Errorf("policy trace ingress: TOPIC_CONTROL_TRACE_EVENTS required")
	}
	if cfg.GroupID == "" {
		return KafkaIngressConfig{}, fmt.Errorf("policy trace ingress: KAFKA_TRACE_EVENTS_GROUP_ID required")
	}
	return cfg, nil
}

type KafkaIngressConsumer struct {
	cfg   KafkaIngressConfig
	store *Store
	cg    sarama.ConsumerGroup
	log   *utils.Logger
	ctx   context.Context
	stop  context.CancelFunc
	wg    sync.WaitGroup

	processed     atomic.Uint64
	rejected      atomic.Uint64
	loopErrors    atomic.Uint64
	logsThrottled atomic.Uint64
	lastLoopLogNs int64
	lastRejectNs  int64
}

type kafkaIngressMessage struct {
	PolicyID        string            `json:"policy_id"`
	TraceID         string            `json:"trace_id"`
	Stage           string            `json:"stage"`
	StageClass      string            `json:"stage_class"`
	StageSource     string            `json:"stage_source"`
	TimestampMs     int64             `json:"timestamp_ms"`
	RequestID       string            `json:"request_id,omitempty"`
	CommandID       string            `json:"command_id,omitempty"`
	WorkflowID      string            `json:"workflow_id,omitempty"`
	SourceEventID   string            `json:"source_event_id,omitempty"`
	SentinelEventID string            `json:"sentinel_event_id,omitempty"`
	OutboxID        string            `json:"outbox_id,omitempty"`
	AckEventID      string            `json:"ack_event_id,omitempty"`
	RuleHash        string            `json:"rule_hash,omitempty"`
	ScopeIdentifier string            `json:"scope_identifier,omitempty"`
	Tenant          string            `json:"tenant,omitempty"`
	Region          string            `json:"region,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	Height          uint64            `json:"height,omitempty"`
	TxIndex         int               `json:"tx_index,omitempty"`
	View            uint64            `json:"view,omitempty"`
	QCTsMs          int64             `json:"qc_ts_ms,omitempty"`
	Partition       int32             `json:"partition,omitempty"`
	Offset          int64             `json:"offset,omitempty"`
	AnomalyCode     string            `json:"anomaly_code,omitempty"`
	Details         map[string]string `json:"details,omitempty"`
}

func NewKafkaIngressConsumer(
	ctx context.Context,
	cfg KafkaIngressConfig,
	group sarama.ConsumerGroup,
	store *Store,
	log *utils.Logger,
) (*KafkaIngressConsumer, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("policy trace ingress: disabled")
	}
	if group == nil {
		return nil, fmt.Errorf("policy trace ingress: consumer group required")
	}
	if store == nil {
		return nil, fmt.Errorf("policy trace ingress: store required")
	}
	if cfg.LogThrottle <= 0 {
		cfg.LogThrottle = 5 * time.Second
	}
	cctx, cancel := context.WithCancel(ctx)
	return &KafkaIngressConsumer{
		cfg:   cfg,
		store: store,
		cg:    group,
		log:   log,
		ctx:   cctx,
		stop:  cancel,
	}, nil
}

func (c *KafkaIngressConsumer) Start() error {
	if c == nil {
		return fmt.Errorf("policy trace ingress: nil")
	}
	c.wg.Add(1)
	go c.loop()
	return nil
}

func (c *KafkaIngressConsumer) Stop() error {
	if c == nil {
		return nil
	}
	c.stop()
	c.wg.Wait()
	if c.cg != nil {
		return c.cg.Close()
	}
	return nil
}

func (c *KafkaIngressConsumer) loop() {
	defer c.wg.Done()
	handler := &kafkaIngressHandler{c: c}
	for {
		if err := c.cg.Consume(c.ctx, []string{c.cfg.Topic}, handler); err != nil {
			if errors.Is(err, sarama.ErrClosedConsumerGroup) {
				return
			}
			c.loopErrors.Add(1)
			if c.log != nil && c.shouldLog(&c.lastLoopLogNs) {
				c.log.ErrorContext(c.ctx, "policy trace ingress consumer error",
					utils.ZapError(err),
					utils.ZapString("topic", c.cfg.Topic))
			}
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
		if c.ctx.Err() != nil {
			return
		}
	}
}

type kafkaIngressHandler struct {
	c *KafkaIngressConsumer
}

func (h *kafkaIngressHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *kafkaIngressHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *kafkaIngressHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case <-sess.Context().Done():
			return nil
		case msg := <-claim.Messages():
			if msg == nil {
				return nil
			}
			ctx := observability.ExtractContextFromSaramaHeaders(sess.Context(), msg.Headers)
			if err := h.c.process(ctx, msg); err != nil {
				h.c.rejected.Add(1)
				if h.c.log != nil && h.c.shouldLog(&h.c.lastRejectNs) {
					h.c.log.WarnContext(ctx, "policy trace ingress rejected message",
						utils.ZapError(err),
						utils.ZapString("topic", msg.Topic),
						utils.ZapInt32("partition", msg.Partition),
						utils.ZapInt64("offset", msg.Offset))
				}
			} else {
				h.c.processed.Add(1)
			}
			sess.MarkMessage(msg, "")
		}
	}
}

func (c *KafkaIngressConsumer) process(ctx context.Context, msg *sarama.ConsumerMessage) error {
	var raw kafkaIngressMessage
	if err := json.Unmarshal(msg.Value, &raw); err != nil {
		return fmt.Errorf("unmarshal trace event: %w", err)
	}
	event, err := raw.toTraceEvent()
	if err != nil {
		return err
	}
	if event.Partition == 0 && msg.Partition != 0 {
		event.Partition = msg.Partition
	}
	if event.Offset == 0 && msg.Offset != 0 {
		event.Offset = msg.Offset
	}
	return c.store.Append(ctx, event)
}

func (m kafkaIngressMessage) toTraceEvent() (TraceEvent, error) {
	var ruleHash []byte
	if raw := strings.TrimSpace(m.RuleHash); raw != "" {
		decoded, err := hex.DecodeString(raw)
		if err != nil {
			return TraceEvent{}, fmt.Errorf("decode rule_hash: %w", err)
		}
		ruleHash = decoded
	}
	event := TraceEvent{
		PolicyID:        strings.TrimSpace(m.PolicyID),
		TraceID:         strings.TrimSpace(m.TraceID),
		Stage:           strings.TrimSpace(m.Stage),
		StageClass:      StageClass(strings.TrimSpace(m.StageClass)),
		StageSource:     strings.TrimSpace(m.StageSource),
		TimestampMs:     m.TimestampMs,
		RequestID:       strings.TrimSpace(m.RequestID),
		CommandID:       strings.TrimSpace(m.CommandID),
		WorkflowID:      strings.TrimSpace(m.WorkflowID),
		SourceEventID:   strings.TrimSpace(m.SourceEventID),
		SentinelEventID: strings.TrimSpace(m.SentinelEventID),
		OutboxID:        strings.TrimSpace(m.OutboxID),
		AckEventID:      strings.TrimSpace(m.AckEventID),
		RuleHash:        ruleHash,
		ScopeIdentifier: strings.TrimSpace(m.ScopeIdentifier),
		Tenant:          strings.TrimSpace(m.Tenant),
		Region:          strings.TrimSpace(m.Region),
		Reason:          strings.TrimSpace(m.Reason),
		Height:          m.Height,
		TxIndex:         m.TxIndex,
		View:            m.View,
		QCTsMs:          m.QCTsMs,
		Partition:       m.Partition,
		Offset:          m.Offset,
		AnomalyCode:     strings.TrimSpace(m.AnomalyCode),
		Details:         m.Details,
		RuntimeMarkerRef: fmt.Sprintf("kafka:%s:%s",
			strings.TrimSpace(m.PolicyID),
			strings.TrimSpace(m.Stage),
		),
	}
	if event.StageClass == "" {
		event.StageClass = ClassifyStage(event.Stage)
	}
	return event, event.Normalize()
}

func (c *KafkaIngressConsumer) shouldLog(lastNs *int64) bool {
	now := time.Now().UnixNano()
	prev := atomic.LoadInt64(lastNs)
	if prev == 0 || time.Duration(now-prev) >= c.cfg.LogThrottle {
		if atomic.CompareAndSwapInt64(lastNs, prev, now) {
			return true
		}
	}
	c.logsThrottled.Add(1)
	return false
}
