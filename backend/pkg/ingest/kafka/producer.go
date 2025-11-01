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
	producer                  sarama.SyncProducer
	topics                    ProducerTopics
	logger                    *utils.Logger
	audit                     *utils.AuditLogger
	signer                    *CommitSigner
	mu                        sync.RWMutex
	closed                    bool
	brokers                   []string
	config                    *sarama.Config
	publishSuccesses          atomic.Uint64
	publishFailures           atomic.Uint64
	publishLatencyTotalMicros atomic.Uint64
	publishLatencySamples     atomic.Uint64
	lastPartition             atomic.Int32
	lastOffset                atomic.Int64
	lastPublishUnix           atomic.Int64
	lastPublishErr            atomic.Pointer[string]
	publishLatencyHist        *utils.LatencyHistogram
}

// ProducerTopics holds Kafka topic names for producer
type ProducerTopics struct {
	Commits    string // control.commits.v1
	Reputation string // control.reputation.v1 (future)
	Policy     string // control.policy.v1 (future)
	Evidence   string // control.evidence.v1 (future)
}

// ProducerConfig holds configuration for creating a producer
type ProducerConfig struct {
	Brokers []string
	Topics  ProducerTopics
	Signer  *CommitSigner
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
		producer: producer,
		topics:   cfg.Topics,
		logger:   logger,
		audit:    audit,
		signer:   cfg.Signer,
		closed:   false,
		brokers:  append([]string(nil), cfg.Brokers...),
		config:   saramaCfg,
	}
	p.publishLatencyHist = utils.NewLatencyHistogram([]float64{1, 5, 20, 100, 500, 1000, math.Inf(1)})
	p.lastPartition.Store(-1)
	p.lastOffset.Store(-1)
	p.lastPublishUnix.Store(0)
	p.lastPublishErr.Store(nil)

	if audit != nil {
		_ = audit.Info("kafka_producer_created", map[string]interface{}{
			"brokers":       fmt.Sprintf("%d configured", len(cfg.Brokers)),
			"commits_topic": cfg.Topics.Commits,
		})
	}

	if logger != nil {
		logger.InfoContext(ctx, "Kafka producer created",
			utils.ZapInt("brokers", len(cfg.Brokers)),
			utils.ZapString("commits_topic", cfg.Topics.Commits))
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

// PublishCommit publishes a block commit event to control.commits.v1
// Called after durable persistence completes
// Fix: Gap 2 - Added anomalyIDs parameter to enable COMMITTED state tracking
func (p *Producer) PublishCommit(ctx context.Context, height uint64, hash [32]byte, stateRoot [32]byte, txCount int, ts int64, anomalyIDs []string) error {
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
	// Fix: Gap 2 - Include anomaly IDs for individual tracking
	evt := &pb.CommitEvent{
		Height:     int64(height),
		BlockHash:  blockHash,
		StateRoot:  stateRootCopy,
		TxCount:    uint32(txCount),
		AnomalyIds: anomalyIDs, // New field
		Timestamp:  ts,
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

// ProducerStats captures operational telemetry for the Kafka producer.
type ProducerStats struct {
	BrokerCount      int
	PublishSuccesses uint64
	PublishFailures  uint64
	PublishLatencyMs float64
	LastPartition    *int32
	LastOffset       *int64
	LastPublishUnix  *int64
	LastError        string
	LatencyBuckets   []utils.HistogramBucket
	LatencyP50Ms     float64
	LatencyP95Ms     float64
	LatencyCount     uint64
	LatencySumMs     float64
}

// Stats returns a snapshot of producer telemetry counters.
func (p *Producer) Stats() ProducerStats {
	var stats ProducerStats
	p.mu.RLock()
	stats.BrokerCount = len(p.brokers)
	p.mu.RUnlock()
	stats.PublishSuccesses = p.publishSuccesses.Load()
	stats.PublishFailures = p.publishFailures.Load()
	samples := p.publishLatencySamples.Load()
	if samples > 0 {
		totalMicros := p.publishLatencyTotalMicros.Load()
		stats.PublishLatencyMs = (float64(totalMicros) / float64(samples)) / 1000.0
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
