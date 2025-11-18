package kafka

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/utils"
	pb "backend/proto"
	"google.golang.org/protobuf/proto"

	"github.com/IBM/sarama"
)

// Producer handles publishing control messages to Kafka
type Producer struct {
	producer                        sarama.SyncProducer
	topics                          ProducerTopics
	logger                          *utils.Logger
	audit                           *utils.AuditLogger
	signer                          *CommitSigner
	policySigner                    *CommitSigner
	mu                              sync.RWMutex
	closed                          bool
	brokers                         []string
	config                          *sarama.Config
	publishSuccesses                atomic.Uint64
	publishFailures                 atomic.Uint64
	publishLatencyTotalMicros       atomic.Uint64
	publishLatencySamples           atomic.Uint64
	policyPublishSuccesses          atomic.Uint64
	policyPublishFailures           atomic.Uint64
	policyPublishLatencyTotalMicros atomic.Uint64
	policyPublishLatencySamples     atomic.Uint64
	lastPartition                   atomic.Int32
	lastOffset                      atomic.Int64
	lastPublishUnix                 atomic.Int64
	lastPublishErr                  atomic.Pointer[string]
	publishLatencyHist              *utils.LatencyHistogram
	policyPublishLatencyHist        *utils.LatencyHistogram
}

// ProducerTopics holds Kafka topic names for producer
type ProducerTopics struct {
	Commits    string // control.commits.v1
	Reputation string // control.reputation.v1 (future)
	Policy     string // control.policy.v1 (future)
	Evidence   string // control.evidence.v1 (future)
	PolicyDLQ  string // control.policy.dlq.v1 (optional)
}

// ProducerConfig holds configuration for creating a producer
type ProducerConfig struct {
	Brokers      []string
	Topics       ProducerTopics
	Signer       *CommitSigner
	PolicySigner *CommitSigner
}

// NewProducer creates a new Kafka producer for publishing control messages
func NewProducer(ctx context.Context, cfg ProducerConfig, saramaCfg *sarama.Config, logger *utils.Logger, audit *utils.AuditLogger) (*Producer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("kafka producer: no brokers configured")
	}
	if cfg.Topics.Commits == "" {
		return nil, fmt.Errorf("kafka producer: commits topic required")
	}
	if cfg.Signer == nil {
		return nil, fmt.Errorf("kafka producer: commit signer not configured")
	}
	if cfg.Topics.Policy != "" && cfg.PolicySigner == nil {
		if logger != nil {
			logger.Warn("Kafka policy topic configured without signer; policy publishing disabled")
		}
	}

	// Create sync producer (idempotent, exactly-once semantics)
	producer, err := sarama.NewSyncProducer(cfg.Brokers, saramaCfg)
	if err != nil {
		if audit != nil {
			_ = audit.Security("kafka_producer_creation_failed", map[string]interface{}{
				"error": err.Error(),
			})
		}
		return nil, fmt.Errorf("kafka producer: failed to create: %w", err)
	}

	p := &Producer{
		producer:     producer,
		topics:       cfg.Topics,
		logger:       logger,
		audit:        audit,
		signer:       cfg.Signer,
		policySigner: cfg.PolicySigner,
		closed:       false,
		brokers:      append([]string(nil), cfg.Brokers...),
		config:       saramaCfg,
	}
	p.publishLatencyHist = utils.NewLatencyHistogram([]float64{1, 5, 20, 100, 500, 1000, math.Inf(1)})
	p.policyPublishLatencyHist = utils.NewLatencyHistogram([]float64{1, 5, 20, 100, 500, 1000, math.Inf(1)})
	p.lastPartition.Store(-1)
	p.lastOffset.Store(-1)
	p.lastPublishUnix.Store(0)
	p.lastPublishErr.Store(nil)

	if audit != nil {
		_ = audit.Info("kafka_producer_created", map[string]interface{}{
			"brokers":          fmt.Sprintf("%d configured", len(cfg.Brokers)),
			"commits_topic":    cfg.Topics.Commits,
			"policy_topic":     cfg.Topics.Policy,
			"policy_dlq_topic": cfg.Topics.PolicyDLQ,
		})
	}

	if logger != nil {
		logger.InfoContext(ctx, "Kafka producer created",
			utils.ZapInt("brokers", len(cfg.Brokers)),
			utils.ZapString("commits_topic", cfg.Topics.Commits),
			utils.ZapString("policy_topic", cfg.Topics.Policy),
			utils.ZapString("policy_dlq_topic", cfg.Topics.PolicyDLQ))
	}

	return p, nil
}

// HealthCheck validates broker connectivity by opening a lightweight client.
func (p *Producer) HealthCheck(ctx context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return fmt.Errorf("kafka producer: closed")
	}

	if len(p.brokers) == 0 || p.config == nil {
		return fmt.Errorf("kafka producer: configuration incomplete")
	}

	client, err := sarama.NewClient(p.brokers, p.config)
	if err != nil {
		return fmt.Errorf("kafka producer: client create failed: %w", err)
	}
	defer client.Close() //nolint:errcheck // close best-effort

	// Perform a lightweight metadata request by listing brokers.
	brokers := client.Brokers()
	if len(brokers) == 0 {
		return fmt.Errorf("kafka producer: no brokers returned")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// PublishCommit publishes a block commit event to control.commits.v1.
func (p *Producer) PublishCommit(ctx context.Context, height uint64, hash [32]byte, stateRoot [32]byte, txCount int, ts int64, anomalyCount int, evidenceCount int, policyCount int, anomalyIDs []string) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return fmt.Errorf("kafka producer: already closed")
	}
	p.mu.RUnlock()

	if p.signer == nil {
		return fmt.Errorf("kafka producer: signer unavailable")
	}

	blockHash := make([]byte, len(hash))
	copy(blockHash, hash[:])
	stateRootCopy := make([]byte, len(stateRoot))
	copy(stateRootCopy, stateRoot[:])

	// Build protobuf CommitEvent
	evt := &pb.CommitEvent{
		Height:        int64(height),
		BlockHash:     blockHash,
		StateRoot:     stateRootCopy,
		TxCount:       uint32(txCount),
		AnomalyCount:  uint32(anomalyCount),
		EvidenceCount: uint32(evidenceCount),
		PolicyCount:   uint32(policyCount),
		AnomalyIds:    anomalyIDs,
		Timestamp:     ts,
	}

	if err := p.signer.Sign(evt); err != nil {
		if p.logger != nil {
			p.logger.ErrorContext(ctx, "Failed to sign commit event",
				utils.ZapError(err),
				utils.ZapUint64("height", height))
		}
		return fmt.Errorf("kafka producer: sign failed: %w", err)
	}

	msg, err := proto.Marshal(evt)
	if err != nil {
		return fmt.Errorf("kafka producer: protobuf marshal failed: %w", err)
	}

	// Create Kafka message
	kafkaMsg := &sarama.ProducerMessage{
		Topic: p.topics.Commits,
		Key:   sarama.ByteEncoder(encodeUint64(height)), // Partition by height
		Value: sarama.ByteEncoder(msg),
		Headers: []sarama.RecordHeader{
			{Key: []byte("version"), Value: []byte("1")},
			{Key: []byte("type"), Value: []byte("commit")},
		},
	}

	// Send message (blocking with idempotent producer)
	start := time.Now()
	partition, offset, err := p.producer.SendMessage(kafkaMsg)
	if err != nil {
		p.publishFailures.Add(1)
		errStr := err.Error()
		p.lastPublishErr.Store(&errStr)
		if p.audit != nil {
			_ = p.audit.Error("kafka_publish_commit_failed", map[string]interface{}{
				"height": height,
				"error":  err.Error(),
			})
		}
		if p.logger != nil {
			p.logger.ErrorContext(ctx, "Failed to publish commit to Kafka",
				utils.ZapError(err),
				utils.ZapUint64("height", height))
		}
		return fmt.Errorf("kafka producer: publish failed: %w", err)
	}

	p.publishSuccesses.Add(1)
	elapsed := time.Since(start)
	if elapsed > 0 {
		p.publishLatencyTotalMicros.Add(uint64(elapsed.Microseconds()))
		p.publishLatencySamples.Add(1)
		p.publishLatencyHist.Observe(float64(elapsed) / float64(time.Millisecond))
	}
	p.lastPartition.Store(partition)
	p.lastOffset.Store(offset)
	p.lastPublishUnix.Store(time.Now().Unix())
	p.lastPublishErr.Store(nil)

	// Success
	if p.audit != nil {
		_ = p.audit.Info("kafka_commit_published", map[string]interface{}{
			"height":        height,
			"hash":          fmt.Sprintf("%x", hash[:8]),
			"partition":     partition,
			"offset":        offset,
			"anomaly_count": len(anomalyIDs),
		})
	}

	if p.logger != nil {
		p.logger.InfoContext(ctx, "Commit published to Kafka",
			utils.ZapUint64("height", height),
			utils.ZapString("hash", fmt.Sprintf("%x", hash[:8])),
			utils.ZapInt32("partition", partition),
			utils.ZapInt64("offset", offset),
			utils.ZapInt("anomaly_ids_tracked", len(anomalyIDs)))
	}

	return nil
}

// PublishPolicy publishes a policy update event to control.policy.v1.
func (p *Producer) PublishPolicy(ctx context.Context, evt *pb.PolicyUpdateEvent) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return fmt.Errorf("kafka producer: already closed")
	}
	p.mu.RUnlock()

	if p.topics.Policy == "" {
		return fmt.Errorf("kafka producer: policy topic not configured")
	}
	if p.policySigner == nil {
		return fmt.Errorf("kafka producer: policy signer unavailable")
	}
	if evt == nil {
		return fmt.Errorf("kafka producer: policy event is nil")
	}

	if err := p.policySigner.SignPolicy(evt); err != nil {
		if p.logger != nil {
			p.logger.ErrorContext(ctx, "Failed to sign policy event",
				utils.ZapError(err),
				utils.ZapString("policy_id", evt.GetPolicyId()))
		}
		return fmt.Errorf("kafka producer: policy sign failed: %w", err)
	}

	msg, err := proto.Marshal(evt)
	if err != nil {
		return fmt.Errorf("kafka producer: policy marshal failed: %w", err)
	}

	var key sarama.Encoder
	if len(evt.RuleHash) > 0 {
		key = sarama.ByteEncoder(evt.RuleHash)
	} else if evt.PolicyId != "" {
		key = sarama.StringEncoder(evt.PolicyId)
	} else {
		key = sarama.StringEncoder(fmt.Sprintf("policy-%d", time.Now().UnixNano()))
	}

	kafkaMsg := &sarama.ProducerMessage{
		Topic: p.topics.Policy,
		Key:   key,
		Value: sarama.ByteEncoder(msg),
		Headers: []sarama.RecordHeader{
			{Key: []byte("version"), Value: []byte("1")},
			{Key: []byte("type"), Value: []byte("policy")},
		},
	}

	start := time.Now()
	partition, offset, err := p.producer.SendMessage(kafkaMsg)
	if err != nil {
		p.policyPublishFailures.Add(1)
		errStr := err.Error()
		p.lastPublishErr.Store(&errStr)
		if p.audit != nil {
			_ = p.audit.Error("kafka_publish_policy_failed", map[string]interface{}{
				"policy_id": evt.GetPolicyId(),
				"error":     err.Error(),
			})
		}
		if p.logger != nil {
			p.logger.ErrorContext(ctx, "Failed to publish policy to Kafka",
				utils.ZapError(err),
				utils.ZapString("policy_id", evt.GetPolicyId()))
		}
		return fmt.Errorf("kafka producer: publish policy failed: %w", err)
	}

	p.policyPublishSuccesses.Add(1)
	elapsed := time.Since(start)
	if elapsed > 0 {
		p.policyPublishLatencyTotalMicros.Add(uint64(elapsed.Microseconds()))
		p.policyPublishLatencySamples.Add(1)
		p.policyPublishLatencyHist.Observe(float64(elapsed) / float64(time.Millisecond))
	}
	p.lastPartition.Store(partition)
	p.lastOffset.Store(offset)
	p.lastPublishUnix.Store(time.Now().Unix())
	p.lastPublishErr.Store(nil)

	if p.audit != nil {
		_ = p.audit.Info("kafka_policy_published", map[string]interface{}{
			"policy_id": evt.GetPolicyId(),
			"partition": partition,
			"offset":    offset,
		})
	}
	if p.logger != nil {
		p.logger.InfoContext(ctx, "Policy published to Kafka",
			utils.ZapString("policy_id", evt.GetPolicyId()),
			utils.ZapInt32("partition", partition),
			utils.ZapInt64("offset", offset),
			utils.ZapInt64("expiration_height", evt.GetExpirationHeight()))
	}

	return nil
}

// PublishDLQ publishes a raw payload to the configured topic (used for guardrail rejects).
func (p *Producer) PublishDLQ(ctx context.Context, topic string, key sarama.Encoder, payload []byte, headers []sarama.RecordHeader) (int32, int64, error) {
	if topic == "" {
		return 0, 0, fmt.Errorf("kafka producer: dlq topic not configured")
	}

	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return 0, 0, fmt.Errorf("kafka producer: already closed")
	}
	p.mu.RUnlock()

	msg := &sarama.ProducerMessage{
		Topic:   topic,
		Key:     key,
		Value:   sarama.ByteEncoder(payload),
		Headers: headers,
	}

	if msg.Key == nil {
		msg.Key = sarama.StringEncoder(fmt.Sprintf("dlq-%d", time.Now().UnixNano()))
	}

	start := time.Now()
	partition, offset, err := p.producer.SendMessage(msg)
	if err != nil {
		if p.logger != nil {
			p.logger.ErrorContext(ctx, "Failed to publish DLQ payload",
				utils.ZapError(err),
				utils.ZapString("topic", topic))
		}
		if p.audit != nil {
			_ = p.audit.Error("kafka_policy_dlq_failed", map[string]interface{}{
				"topic": topic,
				"error": err.Error(),
			})
		}
		return partition, offset, fmt.Errorf("kafka producer: publish dlq failed: %w", err)
	}

	if p.audit != nil {
		_ = p.audit.Info("kafka_policy_dlq_published", map[string]interface{}{
			"topic":      topic,
			"partition":  partition,
			"offset":     offset,
			"latency_ms": time.Since(start).Milliseconds(),
		})
	}

	if p.logger != nil {
		p.logger.DebugContext(ctx, "DLQ payload published",
			utils.ZapString("topic", topic),
			utils.ZapInt32("partition", partition),
			utils.ZapInt64("offset", offset))
	}

	return partition, offset, nil
}

// ProducerStats captures operational telemetry for the Kafka producer.
type ProducerStats struct {
	BrokerCount            int
	PublishSuccesses       uint64
	PublishFailures        uint64
	PublishLatencyMs       float64
	PolicyPublishSuccesses uint64
	PolicyPublishFailures  uint64
	PolicyPublishLatencyMs float64
	LastPartition          *int32
	LastOffset             *int64
	LastPublishUnix        *int64
	LastError              string
	LatencyBuckets         []utils.HistogramBucket
	LatencyP50Ms           float64
	LatencyP95Ms           float64
	LatencyCount           uint64
	LatencySumMs           float64
	PolicyLatencyBuckets   []utils.HistogramBucket
	PolicyLatencyP50Ms     float64
	PolicyLatencyP95Ms     float64
	PolicyLatencyCount     uint64
	PolicyLatencySumMs     float64
}

// Stats returns a snapshot of producer telemetry counters.
func (p *Producer) Stats() ProducerStats {
	var stats ProducerStats
	p.mu.RLock()
	stats.BrokerCount = len(p.brokers)
	p.mu.RUnlock()
	stats.PublishSuccesses = p.publishSuccesses.Load()
	stats.PublishFailures = p.publishFailures.Load()
	stats.PolicyPublishSuccesses = p.policyPublishSuccesses.Load()
	stats.PolicyPublishFailures = p.policyPublishFailures.Load()
	samples := p.publishLatencySamples.Load()
	if samples > 0 {
		totalMicros := p.publishLatencyTotalMicros.Load()
		stats.PublishLatencyMs = (float64(totalMicros) / float64(samples)) / 1000.0
	}
	policySamples := p.policyPublishLatencySamples.Load()
	if policySamples > 0 {
		totalMicros := p.policyPublishLatencyTotalMicros.Load()
		stats.PolicyPublishLatencyMs = (float64(totalMicros) / float64(policySamples)) / 1000.0
	}
	if partition := p.lastPartition.Load(); partition >= 0 {
		val := partition
		stats.LastPartition = &val
	}
	if offset := p.lastOffset.Load(); offset >= 0 {
		val := offset
		stats.LastOffset = &val
	}
	if unix := p.lastPublishUnix.Load(); unix > 0 {
		val := unix
		stats.LastPublishUnix = &val
	}
	if ptr := p.lastPublishErr.Load(); ptr != nil {
		stats.LastError = *ptr
	}
	buckets, total, sum := p.publishLatencyHist.Snapshot()
	if total > 0 {
		stats.LatencyBuckets = buckets
		stats.LatencyP50Ms = p.publishLatencyHist.Quantile(0.50)
		stats.LatencyP95Ms = p.publishLatencyHist.Quantile(0.95)
		stats.LatencyCount = total
		stats.LatencySumMs = sum
	}
	policyBuckets, policyTotal, policySum := p.policyPublishLatencyHist.Snapshot()
	if policyTotal > 0 {
		stats.PolicyLatencyBuckets = policyBuckets
		stats.PolicyLatencyP50Ms = p.policyPublishLatencyHist.Quantile(0.50)
		stats.PolicyLatencyP95Ms = p.policyPublishLatencyHist.Quantile(0.95)
		stats.PolicyLatencyCount = policyTotal
		stats.PolicyLatencySumMs = policySum
	}
	return stats
}

// Close gracefully closes the producer
func (p *Producer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	p.closed = true

	if err := p.producer.Close(); err != nil {
		if p.logger != nil {
			p.logger.Error("Failed to close Kafka producer",
				utils.ZapError(err))
		}
		return fmt.Errorf("kafka producer: close failed: %w", err)
	}

	if p.logger != nil {
		p.logger.Info("Kafka producer closed")
	}

	return nil
}

// encodeUint64 encodes uint64 as big-endian bytes for Kafka key
func encodeUint64(n uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, n)
	return buf
}
