package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"backend/pkg/consumercontract"
	"backend/pkg/control/policytrace"
	"backend/pkg/mempool"
	"backend/pkg/observability"
	"backend/pkg/state"
	"backend/pkg/utils"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	// ErrReplayRejected indicates replay filter rejected admission before mempool.Add.
	ErrReplayRejected = errors.New("kafka: replay admission rejected")
)

// Consumer handles consuming messages from Kafka ai.* topics and submitting to mempool
type Consumer struct {
	consumerGroup sarama.ConsumerGroup
	topics        []string
	mempool       *mempool.Mempool
	verifierCfg   VerifierConfig
	logger        *utils.Logger
	audit         *utils.AuditLogger
	dlqProducer   sarama.SyncProducer // DLQ producer (optional)
	dlqTopic      string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex
	closed bool

	// Stats
	messagesConsumed    uint64
	messagesVerified    uint64
	messagesAdmitted    uint64
	messagesFailed      uint64
	messagesRetried     uint64
	transientFailures   uint64
	replayRejectedAdmit uint64
	preAdmitRejected    map[string]uint64
	classAdmitted       map[string]uint64
	classRejected       map[string]uint64
	groupID             string
	partitionOffsets    map[string]int64
	partitionLag        map[string]int64
	partitionHighwater  map[string]int64
	topicPartitions     map[string]int
	assignedParts       int
	lastMessageUnix     int64
	ingestLatencyHist   *utils.LatencyHistogram
	processLatencyHist  *utils.LatencyHistogram

	// Retry/backoff policy
	retryMax         int
	retryBackoffBase time.Duration
	retryBackoffMax  time.Duration

	// Optional callback invoked after successful mempool admission.
	onPreAdmit               func(topic string, tx state.Transaction) error
	onAdmitted               func(topic string, tx state.Transaction)
	onAdmitFailed            func(topic string, tx state.Transaction, cause error)
	trace                    *policytrace.Collector
	lifecycleMode            string
	lifecycleRollbackEnabled bool
}

// ConsumerConfig holds configuration for creating a consumer
type ConsumerConfig struct {
	Brokers     []string
	GroupID     string
	Topics      []string       // ai.anomalies.v1, ai.evidence.v1, ai.policy.v1
	DLQTopic    string         // DLQ topic for failed messages (optional)
	VerifierCfg VerifierConfig // Timestamp skew configuration
	// Retry configuration for transient processing errors
	RetryMax         int           // Maximum attempts per message (default: 5)
	RetryBackoffBase time.Duration // Initial backoff (default: 100ms)
	RetryBackoffMax  time.Duration // Max backoff cap (default: 5s)
}

// NewConsumer creates a new Kafka consumer for ai.* topics
func NewConsumer(ctx context.Context, cfg ConsumerConfig, saramaCfg *sarama.Config, mp *mempool.Mempool, logger *utils.Logger, audit *utils.AuditLogger) (*Consumer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("kafka consumer: no brokers configured")
	}
	if cfg.GroupID == "" {
		return nil, fmt.Errorf("kafka consumer: group ID required")
	}
	if len(cfg.Topics) == 0 {
		return nil, fmt.Errorf("kafka consumer: no topics configured")
	}
	if mp == nil {
		return nil, fmt.Errorf("kafka consumer: mempool required")
	}

	// Create consumer group
	consumerGroup, err := sarama.NewConsumerGroup(cfg.Brokers, cfg.GroupID, saramaCfg)
	if err != nil {
		if audit != nil {
			_ = audit.Security("kafka_consumer_creation_failed", map[string]interface{}{
				"error": err.Error(),
			})
		}
		return nil, fmt.Errorf("kafka consumer: failed to create: %w", err)
	}

	// Create DLQ producer if enabled
	var dlqProducer sarama.SyncProducer
	if cfg.DLQTopic != "" {
		dlqProducer, err = sarama.NewSyncProducer(cfg.Brokers, saramaCfg)
		if err != nil {
			consumerGroup.Close()
			return nil, fmt.Errorf("kafka consumer: failed to create DLQ producer: %w", err)
		}
	}

	consumerCtx, cancel := context.WithCancel(ctx)

	c := &Consumer{
		consumerGroup:            consumerGroup,
		topics:                   cfg.Topics,
		mempool:                  mp,
		verifierCfg:              cfg.VerifierCfg,
		logger:                   logger,
		audit:                    audit,
		dlqProducer:              dlqProducer,
		dlqTopic:                 cfg.DLQTopic,
		ctx:                      consumerCtx,
		cancel:                   cancel,
		closed:                   false,
		groupID:                  cfg.GroupID,
		partitionOffsets:         make(map[string]int64),
		partitionLag:             make(map[string]int64),
		partitionHighwater:       make(map[string]int64),
		topicPartitions:          make(map[string]int),
		preAdmitRejected:         make(map[string]uint64),
		classAdmitted:            make(map[string]uint64),
		classRejected:            make(map[string]uint64),
		lifecycleMode:            "enforce",
		lifecycleRollbackEnabled: true,
	}
	c.ingestLatencyHist = utils.NewLatencyHistogram([]float64{5, 20, 50, 100, 250, 500, 1000, math.Inf(1)})
	c.processLatencyHist = utils.NewLatencyHistogram([]float64{1, 5, 20, 50, 100, 250, 500, math.Inf(1)})

	// Configure retry/backoff with safe defaults
	c.retryMax = cfg.RetryMax
	if c.retryMax <= 0 {
		c.retryMax = 5
	}
	c.retryBackoffBase = cfg.RetryBackoffBase
	if c.retryBackoffBase <= 0 {
		c.retryBackoffBase = 100 * time.Millisecond
	}
	c.retryBackoffMax = cfg.RetryBackoffMax
	if c.retryBackoffMax <= 0 {
		c.retryBackoffMax = 5 * time.Second
	}

	if audit != nil {
		_ = audit.Info("kafka_consumer_created", map[string]interface{}{
			"group_id": cfg.GroupID,
			"topics":   fmt.Sprintf("%v", cfg.Topics),
			"dlq":      cfg.DLQTopic != "",
		})
	}

	if logger != nil {
		offsetMode := "newest"
		if saramaCfg != nil && saramaCfg.Consumer.Offsets.Initial == sarama.OffsetOldest {
			offsetMode = "earliest"
		}
		logger.InfoContext(ctx, "Kafka consumer created",
			utils.ZapString("group_id", cfg.GroupID),
			utils.ZapStringArray("topics", cfg.Topics),
			utils.ZapString("offset_initial", offsetMode),
			utils.ZapBool("dlq_enabled", cfg.DLQTopic != ""))
	}

	return c, nil
}

// Start starts the consumer loop (blocking until Stop is called)
func (c *Consumer) Start() error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("kafka consumer: already closed")
	}
	c.mu.RUnlock()

	c.wg.Add(1)
	go c.consumeLoop()

	return nil
}

// Stop gracefully stops the consumer
func (c *Consumer) Stop() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	// Cancel context to stop consumption
	c.cancel()

	// Wait for consumer loop to finish
	c.wg.Wait()

	// Close consumer group
	if err := c.consumerGroup.Close(); err != nil {
		if c.logger != nil {
			c.logger.Error("Failed to close Kafka consumer group",
				utils.ZapError(err))
		}
		return fmt.Errorf("kafka consumer: close failed: %w", err)
	}

	// Close DLQ producer if present
	if c.dlqProducer != nil {
		if err := c.dlqProducer.Close(); err != nil {
			if c.logger != nil {
				c.logger.Error("Failed to close DLQ producer",
					utils.ZapError(err))
			}
		}
	}

	if c.logger != nil {
		c.logger.Info("Kafka consumer stopped",
			utils.ZapUint64("consumed", c.messagesConsumed),
			utils.ZapUint64("verified", c.messagesVerified),
			utils.ZapUint64("admitted", c.messagesAdmitted),
			utils.ZapUint64("failed", c.messagesFailed))
	}

	return nil
}

// consumeLoop is the main consumer loop
func (c *Consumer) consumeLoop() {
	defer c.wg.Done()

	handler := &consumerGroupHandler{consumer: c}

	for {
		// Consume will block until error or context cancellation
		if err := c.consumerGroup.Consume(c.ctx, c.topics, handler); err != nil {
			if errors.Is(err, sarama.ErrClosedConsumerGroup) {
				return
			}
			if c.logger != nil {
				c.logger.ErrorContext(c.ctx, "Kafka consumer error, retrying after backoff",
					utils.ZapError(err))
			}
			// Backoff before retry
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		// Check if context was cancelled
		if c.ctx.Err() != nil {
			return
		}
	}
}

// consumerGroupHandler implements sarama.ConsumerGroupHandler
type consumerGroupHandler struct {
	consumer *Consumer
}

// Setup is called at the beginning of a new session, before ConsumeClaim
func (h *consumerGroupHandler) Setup(session sarama.ConsumerGroupSession) error {
	h.consumer.resetPartitionTracking()
	claims := session.Claims()
	if h.consumer.logger != nil {
		totalPartitions := 0
		for topic, partitions := range claims {
			totalPartitions += len(partitions)
			ints := make([]int, len(partitions))
			for i, p := range partitions {
				ints[i] = int(p)
			}
			h.consumer.logger.Info("Kafka partitions assigned",
				utils.ZapString("topic", topic),
				utils.ZapInts("partitions", ints))
		}
		h.consumer.logger.Info("Kafka consumer session ready",
			utils.ZapInt("topics", len(claims)),
			utils.ZapInt("total_partitions", totalPartitions))
	}
	if len(claims) == 0 {
		h.consumer.setAssignedPartitions(0)
		return nil
	}

	total := 0
	for topic, partitions := range claims {
		total += len(partitions)
		h.consumer.setTopicPartitionCount(topic, len(partitions))
	}
	h.consumer.setAssignedPartitions(total)
	return nil
}

// Cleanup is called at the end of a session, once all ConsumeClaim goroutines have exited
func (h *consumerGroupHandler) Cleanup(sarama.ConsumerGroupSession) error {
	if h.consumer.logger != nil {
		h.consumer.logger.Info("Kafka consumer session closed")
	}
	return nil
}

// ConsumeClaim processes messages from a partition
func (h *consumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	if h.consumer.logger != nil {
		h.consumer.logger.Info("[DEBUG] ConsumeClaim() CALLED!",
			utils.ZapString("topic", claim.Topic()),
			utils.ZapInt("partition", int(claim.Partition())),
			utils.ZapInt64("initial_offset", claim.InitialOffset()))
	}

	ctx := session.Context()

	for {
		select {
		case <-ctx.Done():
			if h.consumer.logger != nil {
				h.consumer.logger.Info("[DEBUG] ConsumeClaim context done, exiting")
			}
			return nil

		case message := <-claim.Messages():
			if h.consumer.logger != nil {
				h.consumer.logger.Info("[DEBUG] Message received from channel!",
					utils.ZapString("topic", message.Topic),
					utils.ZapInt("partition", int(message.Partition)),
					utils.ZapInt64("offset", message.Offset))
			}
			if message == nil {
				if h.consumer.logger != nil {
					h.consumer.logger.Info("[DEBUG] Received nil message, partition closed")
				}
				return nil
			}

			if h.consumer.logger != nil {
				h.consumer.logger.Info("[DEBUG] Processing message...",
					utils.ZapInt64("offset", message.Offset))
			}

			hw := claim.HighWaterMarkOffset()
			lag := hw - message.Offset - 1
			if hw == 0 {
				lag = 0
			}
			if lag < 0 {
				lag = 0
			}
			highwater := hw
			if highwater < message.Offset {
				highwater = message.Offset
			}
			h.consumer.trackPartitionStats(message.Topic, message.Partition, message.Offset, lag, highwater)

			// Process message (with retry/backoff for transient errors)
			h.consumer.incrementConsumed()
			if !message.Timestamp.IsZero() {
				latency := time.Since(message.Timestamp)
				if latency < 0 {
					latency = 0
				}
				h.consumer.observeIngestLatency(latency)
			}

			msgCtx := observability.ExtractContextFromSaramaHeaders(ctx, message.Headers)
			backoff := h.consumer.retryBackoffBase
			var attempts int
			for attempts = 1; attempts <= h.consumer.retryMax; attempts++ {
				startProcess := time.Now()
				err := h.consumer.processMessage(msgCtx, session, message)
				h.consumer.observeProcessLatency(time.Since(startProcess))

				if err == nil {
					// Success: mark and move on
					session.MarkMessage(message, "")
					h.consumer.incrementAdmitted()
					break
				}

				if isPermanentErr(err) {
					// Permanent failure (decode/verify already DLQ'ed, or duplicate/invalid): mark and skip
					session.MarkMessage(message, "")
					h.consumer.incrementFailed()
					break
				}

				// Transient failure (e.g., mempool backpressure): backoff then retry without committing
				if h.consumer.logger != nil {
					h.consumer.logger.WarnContext(ctx, "Transient processing error; will retry",
						utils.ZapInt("attempt", attempts),
						utils.ZapDuration("backoff", backoff),
						utils.ZapError(err))
				}
				// Count retry and backoff
				h.consumer.incrementRetried()
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(backoff):
				}
				// Exponential backoff with cap
				backoff *= 2
				if backoff > h.consumer.retryBackoffMax {
					backoff = h.consumer.retryBackoffMax
				}
			}

			// If we exhausted retries on transient errors, leave uncommitted
			if attempts > h.consumer.retryMax {
				h.consumer.incrementTransientFailure()
				if h.consumer.logger != nil {
					h.consumer.logger.WarnContext(ctx, "Transient error retries exhausted; leaving offset uncommitted",
						utils.ZapInt("attempts", attempts-1))
				}
			}
		}
	}
}

// isPermanentErr classifies processing errors for offset commit policy.
// Permanent: decode/verify failures (processMessage routes to DLQ), duplicates/invalid tx.
// Transient: mempool backpressure (rate limited, full) which should be retried.
func isPermanentErr(err error) bool {
	if err == nil {
		return false
	}
	if class, ok := classifyProcessError(err); ok {
		return class == consumercontract.ErrorClassPermanent
	}
	// Transient mempool conditions
	if errors.Is(err, mempool.ErrRateLimited) || errors.Is(err, mempool.ErrMempoolFull) {
		return false
	}
	// Replay-rejected, duplicates and invalid tx are safe to skip.
	if errors.Is(err, ErrReplayRejected) || errors.Is(err, mempool.ErrDuplicate) || errors.Is(err, mempool.ErrInvalidTx) {
		return true
	}
	// Default to permanent to avoid infinite retries on unknown errors
	return true
}

func classifyProcessError(err error) (consumercontract.ErrorClass, bool) {
	if err == nil {
		return "", false
	}
	if class, ok := consumercontract.ClassifyContextError(err); ok {
		return class, true
	}
	if errors.Is(err, mempool.ErrRateLimited) || errors.Is(err, mempool.ErrMempoolFull) {
		return consumercontract.ErrorClassTransient, true
	}
	if errors.Is(err, ErrReplayRejected) || errors.Is(err, mempool.ErrDuplicate) || errors.Is(err, mempool.ErrInvalidTx) {
		return consumercontract.ErrorClassPermanent, true
	}
	return "", false
}

// processMessage handles a single Kafka message: decode → verify → mempool.Admit
func (c *Consumer) processMessage(ctx context.Context, session sarama.ConsumerGroupSession, message *sarama.ConsumerMessage) error {
	ctx, span := observability.Tracer("backend/ingest-kafka").Start(
		ctx,
		"backend.ingest.kafka.process",
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination", message.Topic),
			attribute.Int64("messaging.kafka.partition", int64(message.Partition)),
			attribute.Int64("messaging.kafka.offset", message.Offset),
		),
	)
	defer span.End()

	if c.logger != nil {
		c.logger.Info("[DEBUG] processMessage() CALLED",
			utils.ZapString("topic", message.Topic),
			utils.ZapInt64("offset", message.Offset))
	}

	// Decode message based on topic
	var tx state.Transaction
	var meta mempool.AdmissionMeta
	var err error

	if c.logger != nil {
		c.logger.Info("[DEBUG] Routing to handler by topic",
			utils.ZapString("topic", message.Topic))
	}

	switch message.Topic {
	case "ai.anomalies.v1":
		if c.logger != nil {
			c.logger.Info("[DEBUG] Calling processAnomalyMessage()")
		}
		tx, meta, err = c.processAnomalyMessage(message)
		if c.logger != nil {
			if err != nil {
				c.logger.Info("[DEBUG] processAnomalyMessage() returned ERROR",
					utils.ZapError(err))
			} else {
				c.logger.Info("[DEBUG] processAnomalyMessage() returned SUCCESS")
			}
		}
	case "ai.evidence.v1":
		if c.logger != nil {
			c.logger.Info("[DEBUG] Calling processEvidenceMessage()")
		}
		tx, meta, err = c.processEvidenceMessage(message)
		if c.logger != nil {
			if err != nil {
				c.logger.Info("[DEBUG] processEvidenceMessage() returned ERROR",
					utils.ZapError(err))
			} else {
				c.logger.Info("[DEBUG] processEvidenceMessage() returned SUCCESS")
			}
		}
	case "ai.policy.v1":
		if c.logger != nil {
			c.logger.Info("[DEBUG] Calling processPolicyMessage()")
		}
		tx, meta, err = c.processPolicyMessage(message)
		if c.logger != nil {
			if err != nil {
				c.logger.Info("[DEBUG] processPolicyMessage() returned ERROR",
					utils.ZapError(err))
			} else {
				c.logger.Info("[DEBUG] processPolicyMessage() returned SUCCESS")
			}
		}
	default:
		// Unknown topic
		if c.logger != nil {
			c.logger.WarnContext(ctx, "Unknown Kafka topic",
				utils.ZapString("topic", message.Topic))
		}
		span.SetStatus(codes.Error, "unknown_topic")
		return fmt.Errorf("unknown topic: %s", message.Topic)
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "decode_or_verify_failed")
		if c.logger != nil {
			c.logger.Info("[DEBUG] Handler returned error, routing to DLQ",
				utils.ZapError(err))
		}
		// Decode/verify failed - route to DLQ
		c.routeToDLQ(ctx, message, err)
		return err
	}

	if c.logger != nil {
		c.logger.Info("[DEBUG] Message decoded and verified successfully")
	}

	c.incrementVerified()
	c.setLastMessageTime(time.Now())

	// Submit to mempool
	if c.logger != nil {
		c.logger.Info("[DEBUG] Calling mempool.Add()")
	}
	now := time.Now()
	if err := c.emitPreAdmit(message.Topic, tx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "preadmit_rejected")
		if reason, ok := PreAdmitReason(err); ok {
			c.incrementPreAdmitRejected(reason)
		} else {
			c.incrementPreAdmitRejected("other")
		}
		if errors.Is(err, ErrReplayRejected) {
			c.incrementReplayRejectedAdmit()
		}
		if c.audit != nil {
			_ = c.audit.Warn("pre_admit_rejected", map[string]interface{}{
				"error":  err.Error(),
				"topic":  message.Topic,
				"offset": message.Offset,
			})
		}
		if c.logger != nil {
			c.logger.WarnContext(ctx, "Pre-admit hook rejected transaction",
				utils.ZapError(err),
				utils.ZapString("topic", message.Topic))
		}
		return err
	}
	if err := c.mempool.Add(tx, meta, now); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "mempool_add_failed")
		c.emitAdmitFailed(message.Topic, tx, err)
		if class, ok := mempool.ClassFullReason(err); ok {
			c.incrementClassRejected(class)
		}
		if c.logger != nil {
			c.logger.Info("[DEBUG] mempool.Add() returned ERROR",
				utils.ZapError(err))
		}
		// Mempool admission failed (rate limit, full, duplicate, etc.)
		if c.audit != nil {
			_ = c.audit.Warn("mempool_admit_failed", map[string]interface{}{
				"error":  err.Error(),
				"topic":  message.Topic,
				"offset": message.Offset,
			})
		}
		if c.logger != nil {
			c.logger.WarnContext(ctx, "Mempool admission failed",
				utils.ZapError(err),
				utils.ZapString("topic", message.Topic))
		}

		// Transient error - DO NOT commit offset, retry on rebalance
		// But also don't send to DLQ (it's not a message error)
		return err
	}
	c.incrementClassAdmitted(meta.PriorityClass)
	span.SetAttributes(attribute.String("mempool.priority_class", meta.PriorityClass))

	if message.Topic == "ai.policy.v1" {
		if ptx, ok := tx.(*state.PolicyTx); ok {
			policyID, traceID := extractPolicyStageIdentity(ptx.Data)
			if policyID != "" {
				c.recordPolicyStage("t_mempool_enqueued", policyID, traceID, now.UnixMilli())
			}
		}
	}
	c.emitAdmitted(message.Topic, tx)

	if c.logger != nil {
		c.logger.Info("[DEBUG] mempool.Add() SUCCESS - message fully processed")
	}

	// Success
	span.SetStatus(codes.Ok, "admitted")
	return nil
}

// SetPreAdmitHook installs a callback invoked before mempool admission.
func (c *Consumer) SetPreAdmitHook(hook func(topic string, tx state.Transaction) error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.onPreAdmit = hook
	c.mu.Unlock()
}

// SetPostAdmitHook installs a callback invoked after successful mempool admission.
func (c *Consumer) SetPostAdmitHook(hook func(topic string, tx state.Transaction)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.onAdmitted = hook
	c.mu.Unlock()
}

// SetAdmitFailedHook installs a callback invoked when mempool admission fails.
func (c *Consumer) SetAdmitFailedHook(hook func(topic string, tx state.Transaction, cause error)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.onAdmitFailed = hook
	c.mu.Unlock()
}

func (c *Consumer) SetTraceCollector(trace *policytrace.Collector) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.trace = trace
	c.mu.Unlock()
}

func (c *Consumer) SetLifecycleAdmissionMode(mode string, rollbackEnabled bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.lifecycleMode = strings.ToLower(strings.TrimSpace(mode))
	if c.lifecycleMode == "" {
		c.lifecycleMode = "enforce"
	}
	c.lifecycleRollbackEnabled = rollbackEnabled
	c.mu.Unlock()
}

func (c *Consumer) recordPolicyStage(stage, policyID, traceID string, tsMs int64) {
	if c == nil || stage == "" || policyID == "" || tsMs <= 0 {
		return
	}
	if c.logger != nil {
		c.logger.Info("policy stage marker",
			utils.ZapString("stage", stage),
			utils.ZapString("policy_id", policyID),
			utils.ZapString("trace_id", traceID),
			utils.ZapInt64("t_ms", tsMs))
	}
	c.mu.RLock()
	trace := c.trace
	c.mu.RUnlock()
	if trace != nil {
		trace.Record(policytrace.Marker{
			Stage:       stage,
			PolicyID:    policyID,
			TraceID:     traceID,
			TimestampMs: tsMs,
		})
	}
}

func (c *Consumer) emitPreAdmit(topic string, tx state.Transaction) (err error) {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	hook := c.onPreAdmit
	c.mu.RUnlock()
	if hook == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			panicErr := fmt.Errorf("pre-admit hook panic: %v", r)
			if c.logger != nil {
				c.logger.Warn("pre-admit hook panicked",
					utils.ZapString("topic", topic),
					utils.ZapString("panic", fmt.Sprintf("%v", r)))
			}
			// Fail closed: if guard hook panics, reject admission.
			err = panicErr
		}
	}()
	return hook(topic, tx)
}

func (c *Consumer) emitAdmitted(topic string, tx state.Transaction) {
	if c == nil {
		return
	}
	c.mu.RLock()
	hook := c.onAdmitted
	c.mu.RUnlock()
	if hook == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil && c.logger != nil {
			c.logger.Warn("post-admit hook panicked",
				utils.ZapString("topic", topic),
				utils.ZapString("panic", fmt.Sprintf("%v", r)))
		}
	}()
	hook(topic, tx)
}

func (c *Consumer) emitAdmitFailed(topic string, tx state.Transaction, cause error) {
	if c == nil {
		return
	}
	c.mu.RLock()
	hook := c.onAdmitFailed
	c.mu.RUnlock()
	if hook == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil && c.logger != nil {
			c.logger.Warn("admit-failed hook panicked",
				utils.ZapString("topic", topic),
				utils.ZapString("panic", fmt.Sprintf("%v", r)))
		}
	}()
	hook(topic, tx, cause)
}

// processAnomalyMessage decodes and verifies ai.anomalies.v1 message
func (c *Consumer) processAnomalyMessage(message *sarama.ConsumerMessage) (state.Transaction, mempool.AdmissionMeta, error) {
	if c.logger != nil {
		c.logger.Info("[DEBUG] processAnomalyMessage() STARTED",
			utils.ZapInt64("offset", message.Offset),
			utils.ZapInt("payload_bytes", len(message.Value)))
	}

	// Decode
	if c.logger != nil {
		c.logger.Info("[DEBUG] Calling DecodeAnomalyMsg()")
	}
	msg, err := DecodeAnomalyMsg(message.Value)
	if c.logger != nil {
		if err != nil {
			c.logger.Info("[DEBUG] DecodeAnomalyMsg() FAILED",
				utils.ZapError(err))
		} else {
			c.logger.Info("[DEBUG] DecodeAnomalyMsg() SUCCESS")
		}
	}
	if err != nil {
		if c.logger != nil {
			c.logger.WarnContext(c.ctx, "Failed to decode anomaly message",
				utils.ZapError(err),
				utils.ZapInt64("offset", message.Offset))
		}
		return nil, mempool.AdmissionMeta{}, fmt.Errorf("decode failed: %w", err)
	}

	// Verify signature and convert to state.EventTx
	if c.logger != nil {
		c.logger.Info("[DEBUG] Calling VerifyAnomalyMsg()")
	}
	tx, err := VerifyAnomalyMsg(msg, c.verifierCfg, c.logger)
	if c.logger != nil {
		if err != nil {
			c.logger.Info("[DEBUG] VerifyAnomalyMsg() FAILED",
				utils.ZapError(err))
		} else {
			c.logger.Info("[DEBUG] VerifyAnomalyMsg() SUCCESS")
		}
	}
	if err != nil {
		if c.audit != nil {
			_ = c.audit.Security("kafka_message_verification_failed", map[string]interface{}{
				"error":  err.Error(),
				"topic":  message.Topic,
				"offset": message.Offset,
			})
		}
		if c.logger != nil {
			c.logger.WarnContext(c.ctx, "Anomaly message verification failed",
				utils.ZapError(err),
				utils.ZapInt64("offset", message.Offset))
		}
		return nil, mempool.AdmissionMeta{}, fmt.Errorf("verification failed: %w", err)
	}

	// Extract metadata for mempool prioritization
	meta := mempool.AdmissionMeta{
		Severity:      msg.Severity,
		Confidence:    msg.Confidence,
		PriorityClass: "p2",
	}

	return tx, mempool.ComputeMeta(tx, meta), nil
}

// processEvidenceMessage decodes and verifies ai.evidence.v1 message
func (c *Consumer) processEvidenceMessage(message *sarama.ConsumerMessage) (state.Transaction, mempool.AdmissionMeta, error) {
	msg, err := DecodeEvidenceMsg(message.Value)
	if err != nil {
		if c.logger != nil {
			c.logger.WarnContext(c.ctx, "Failed to decode evidence message",
				utils.ZapError(err),
				utils.ZapInt64("offset", message.Offset))
		}
		return nil, mempool.AdmissionMeta{}, fmt.Errorf("decode failed: %w", err)
	}

	tx, err := VerifyEvidenceMsg(msg, c.verifierCfg, c.logger)
	if err != nil {
		if c.audit != nil {
			_ = c.audit.Security("kafka_message_verification_failed", map[string]interface{}{
				"error":  err.Error(),
				"topic":  message.Topic,
				"offset": message.Offset,
			})
		}
		if c.logger != nil {
			c.logger.WarnContext(c.ctx, "Evidence message verification failed",
				utils.ZapError(err),
				utils.ZapInt64("offset", message.Offset))
		}
		return nil, mempool.AdmissionMeta{}, fmt.Errorf("verification failed: %w", err)
	}

	// Evidence has no severity/confidence - use default
	meta := mempool.AdmissionMeta{
		Severity:      5,   // Medium severity
		Confidence:    1.0, // Full confidence (cryptographically verified)
		PriorityClass: "p2",
	}

	return tx, mempool.ComputeMeta(tx, meta), nil
}

// processPolicyMessage decodes and verifies ai.policy.v1 message
func (c *Consumer) processPolicyMessage(message *sarama.ConsumerMessage) (state.Transaction, mempool.AdmissionMeta, error) {
	consumeMs := time.Now().UnixMilli()
	msg, err := DecodePolicyMsg(message.Value)
	if err != nil {
		if c.logger != nil {
			c.logger.WarnContext(c.ctx, "Failed to decode policy message",
				utils.ZapError(err),
				utils.ZapInt64("offset", message.Offset))
		}
		return nil, mempool.AdmissionMeta{}, fmt.Errorf("decode failed: %w", err)
	}
	policyID, traceID := extractPolicyStageIdentity(msg.Params)
	if policyID != "" {
		c.recordPolicyStage("t_policy_handler_enter", policyID, traceID, consumeMs)
		c.recordPolicyStage("t_decode_done", policyID, traceID, time.Now().UnixMilli())
		c.recordPolicyStage("t_backend_consume", policyID, traceID, consumeMs)
	}

	tx, err := VerifyPolicyMsg(msg, c.verifierCfg, c.logger)
	if err != nil {
		if c.audit != nil {
			_ = c.audit.Security("kafka_message_verification_failed", map[string]interface{}{
				"error":  err.Error(),
				"topic":  message.Topic,
				"offset": message.Offset,
			})
		}
		if c.logger != nil {
			c.logger.WarnContext(c.ctx, "Policy message verification failed",
				utils.ZapError(err),
				utils.ZapInt64("offset", message.Offset))
		}
		return nil, mempool.AdmissionMeta{}, fmt.Errorf("verification failed: %w", err)
	}
	if policyID != "" {
		c.recordPolicyStage("t_backend_verified_done", policyID, traceID, time.Now().UnixMilli())
	}

	// Policy has high priority
	meta := mempool.AdmissionMeta{
		Severity:      8,   // High severity (policy changes are critical)
		Confidence:    1.0, // Full confidence (cryptographically verified)
		PriorityClass: classifyPolicyPriorityClass(msg.Action, msg.Params),
	}

	return tx, mempool.ComputeMeta(tx, meta), nil
}

func extractPolicyStageIdentity(payload []byte) (string, string) {
	if len(payload) == 0 {
		return "", ""
	}
	var root map[string]interface{}
	if err := json.Unmarshal(payload, &root); err != nil {
		return "", ""
	}
	policyID := strings.TrimSpace(asString(root["policy_id"]))
	traceID := strings.TrimSpace(asString(root["trace_id"]))
	if traceID == "" {
		if metadata, ok := root["metadata"].(map[string]interface{}); ok {
			traceID = strings.TrimSpace(asString(metadata["trace_id"]))
		}
	}
	if traceID == "" {
		if trace, ok := root["trace"].(map[string]interface{}); ok {
			traceID = strings.TrimSpace(asString(trace["id"]))
		}
	}
	if nested, ok := root["params"].(map[string]interface{}); ok {
		if policyID == "" {
			policyID = strings.TrimSpace(asString(nested["policy_id"]))
		}
		if traceID == "" {
			traceID = strings.TrimSpace(asString(nested["trace_id"]))
		}
		if traceID == "" {
			if metadata, ok := nested["metadata"].(map[string]interface{}); ok {
				traceID = strings.TrimSpace(asString(metadata["trace_id"]))
			}
		}
		if traceID == "" {
			if trace, ok := nested["trace"].(map[string]interface{}); ok {
				traceID = strings.TrimSpace(asString(trace["id"]))
			}
		}
	}
	return policyID, traceID
}

func classifyPolicyPriorityClass(action string, params []byte) string {
	act := strings.ToLower(strings.TrimSpace(action))
	controlAction := ""
	if len(params) > 0 {
		var root map[string]interface{}
		if err := json.Unmarshal(params, &root); err == nil {
			controlAction = strings.ToLower(strings.TrimSpace(asString(root["control_action"])))
			if controlAction == "" {
				controlAction = strings.ToLower(strings.TrimSpace(asString(root["action"])))
			}
			if controlAction == "" {
				if nested, ok := root["params"].(map[string]interface{}); ok {
					controlAction = strings.ToLower(strings.TrimSpace(asString(nested["control_action"])))
					if controlAction == "" {
						controlAction = strings.ToLower(strings.TrimSpace(asString(nested["action"])))
					}
				}
			}
		}
	}
	if isCriticalPolicyAction(act, controlAction) {
		return "p0"
	}
	if isMaintenancePolicyAction(act, controlAction) {
		return "p2"
	}
	if act == "" {
		return "p1"
	}
	return "p1"
}

func isCriticalPolicyAction(action, controlAction string) bool {
	switch strings.ToLower(strings.TrimSpace(controlAction)) {
	case "drop", "reject", "freeze", "block", "remove":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "remove", "block", "drop", "reject", "freeze":
		return true
	default:
		return false
	}
}

func isMaintenancePolicyAction(action, controlAction string) bool {
	switch strings.ToLower(strings.TrimSpace(controlAction)) {
	case "refresh", "suppress", "bypass", "noop", "reconcile", "ttl":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "refresh", "suppress", "bypass", "noop", "reconcile", "ttl":
		return true
	default:
		return false
	}
}

func asString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// routeToDLQ sends failed messages to Dead Letter Queue
func (c *Consumer) routeToDLQ(ctx context.Context, message *sarama.ConsumerMessage, originalErr error) {
	if c.dlqProducer == nil || c.dlqTopic == "" {
		// DLQ not configured - log only
		return
	}

	dlqMessage := &sarama.ProducerMessage{
		Topic: c.dlqTopic,
		Key:   sarama.ByteEncoder(message.Key),
		Value: sarama.ByteEncoder(message.Value),
		Headers: []sarama.RecordHeader{
			{Key: []byte("original_topic"), Value: []byte(message.Topic)},
			{Key: []byte("original_partition"), Value: []byte(fmt.Sprintf("%d", message.Partition))},
			{Key: []byte("original_offset"), Value: []byte(fmt.Sprintf("%d", message.Offset))},
			{Key: []byte("error"), Value: []byte(originalErr.Error())},
		},
	}

	if _, _, err := c.dlqProducer.SendMessage(dlqMessage); err != nil {
		if c.audit != nil {
			_ = c.audit.Error("dlq_routing_failed", map[string]interface{}{
				"error":           err.Error(),
				"original_topic":  message.Topic,
				"original_offset": message.Offset,
			})
		}
		if c.logger != nil {
			c.logger.ErrorContext(ctx, "Failed to route message to DLQ",
				utils.ZapError(err),
				utils.ZapString("topic", message.Topic),
				utils.ZapInt64("offset", message.Offset))
		}
	} else {
		if c.audit != nil {
			_ = c.audit.Warn("message_routed_to_dlq", map[string]interface{}{
				"original_topic":  message.Topic,
				"original_offset": message.Offset,
				"original_error":  originalErr.Error(),
			})
		}
	}
}

// Stats tracking
func (c *Consumer) incrementConsumed() {
	c.mu.Lock()
	c.messagesConsumed++
	c.mu.Unlock()
}

func (c *Consumer) incrementVerified() {
	c.mu.Lock()
	c.messagesVerified++
	c.mu.Unlock()
}

func (c *Consumer) incrementAdmitted() {
	c.mu.Lock()
	c.messagesAdmitted++
	c.mu.Unlock()
}

func (c *Consumer) incrementFailed() {
	c.mu.Lock()
	c.messagesFailed++
	c.mu.Unlock()
}

func (c *Consumer) incrementRetried() {
	c.mu.Lock()
	c.messagesRetried++
	c.mu.Unlock()
}

func (c *Consumer) incrementTransientFailure() {
	c.mu.Lock()
	c.transientFailures++
	c.mu.Unlock()
}

func (c *Consumer) incrementReplayRejectedAdmit() {
	c.mu.Lock()
	c.replayRejectedAdmit++
	c.mu.Unlock()
}

func (c *Consumer) incrementPreAdmitRejected(reason string) {
	c.mu.Lock()
	c.preAdmitRejected[sanitizeReasonKey(reason)]++
	c.mu.Unlock()
}

func (c *Consumer) incrementClassAdmitted(class string) {
	c.mu.Lock()
	c.classAdmitted[normalizePriorityClass(class)]++
	c.mu.Unlock()
}

func (c *Consumer) incrementClassRejected(class string) {
	c.mu.Lock()
	c.classRejected[normalizePriorityClass(class)]++
	c.mu.Unlock()
}

func (c *Consumer) observeIngestLatency(d time.Duration) {
	if c.ingestLatencyHist == nil {
		return
	}
	c.ingestLatencyHist.Observe(float64(d) / float64(time.Millisecond))
}

func (c *Consumer) observeProcessLatency(d time.Duration) {
	if c.processLatencyHist == nil {
		return
	}
	c.processLatencyHist.Observe(float64(d) / float64(time.Millisecond))
}

func (c *Consumer) Stats() ConsumerStats {
	stats := ConsumerStats{}
	c.mu.RLock()
	stats.GroupID = c.groupID
	stats.Topics = append([]string(nil), c.topics...)
	stats.TopicPartitions = copyIntMap(c.topicPartitions)
	stats.PartitionOffsets = copyInt64Map(c.partitionOffsets)
	stats.PartitionLag = copyInt64Map(c.partitionLag)
	stats.PartitionHighwater = copyInt64Map(c.partitionHighwater)
	stats.AssignedPartitions = c.assignedParts
	stats.MessagesConsumed = c.messagesConsumed
	stats.MessagesVerified = c.messagesVerified
	stats.MessagesAdmitted = c.messagesAdmitted
	stats.MessagesFailed = c.messagesFailed
	stats.MessagesRetried = c.messagesRetried
	stats.TransientFailures = c.transientFailures
	stats.ReplayRejectedAdmit = c.replayRejectedAdmit
	stats.PreAdmitRejected = copyCounterMap(c.preAdmitRejected)
	stats.ClassAdmitted = copyCounterMap(c.classAdmitted)
	stats.ClassRejected = copyCounterMap(c.classRejected)
	stats.LifecycleMode = c.lifecycleMode
	stats.LifecycleRollbackEnabled = c.lifecycleRollbackEnabled
	stats.LastMessageUnix = c.lastMessageUnix
	c.mu.RUnlock()
	buckets, total, sum := c.ingestLatencyHist.Snapshot()
	if total > 0 {
		stats.IngestLatencyBuckets = buckets
		stats.IngestLatencyP95Ms = c.ingestLatencyHist.Quantile(0.95)
		stats.IngestLatencyCount = total
		stats.IngestLatencySumMs = sum
	}
	procBuckets, procTotal, procSum := c.processLatencyHist.Snapshot()
	if procTotal > 0 {
		stats.ProcessLatencyBuckets = procBuckets
		stats.ProcessLatencyP95Ms = c.processLatencyHist.Quantile(0.95)
		stats.ProcessLatencyCount = procTotal
		stats.ProcessLatencySumMs = procSum
	}
	return stats
}

func (c *Consumer) resetPartitionTracking() {
	c.mu.Lock()
	c.partitionOffsets = make(map[string]int64)
	c.partitionLag = make(map[string]int64)
	c.partitionHighwater = make(map[string]int64)
	c.topicPartitions = make(map[string]int)
	c.mu.Unlock()
}

func (c *Consumer) setAssignedPartitions(n int) {
	c.mu.Lock()
	c.assignedParts = n
	c.mu.Unlock()
}

func (c *Consumer) trackPartitionStats(topic string, partition int32, offset int64, lag int64, highwater int64) {
	c.mu.Lock()
	key := fmt.Sprintf("%s-%d", topic, partition)
	c.partitionOffsets[key] = offset
	c.partitionLag[key] = lag
	c.partitionHighwater[key] = highwater
	c.mu.Unlock()
}

func (c *Consumer) setTopicPartitionCount(topic string, count int) {
	c.mu.Lock()
	c.topicPartitions[topic] = count
	c.mu.Unlock()
}

func (c *Consumer) setLastMessageTime(t time.Time) {
	c.mu.Lock()
	c.lastMessageUnix = t.Unix()
	c.mu.Unlock()
}

func copyInt64Map(src map[string]int64) map[string]int64 {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]int64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyIntMap(src map[string]int) map[string]int {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]int, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

type ConsumerStats struct {
	GroupID                  string
	Topics                   []string
	TopicPartitions          map[string]int
	PartitionOffsets         map[string]int64
	PartitionLag             map[string]int64
	PartitionHighwater       map[string]int64
	AssignedPartitions       int
	MessagesConsumed         uint64
	MessagesVerified         uint64
	MessagesAdmitted         uint64
	MessagesFailed           uint64
	MessagesRetried          uint64
	TransientFailures        uint64
	ReplayRejectedAdmit      uint64
	PreAdmitRejected         map[string]uint64
	ClassAdmitted            map[string]uint64
	ClassRejected            map[string]uint64
	LifecycleMode            string
	LifecycleRollbackEnabled bool
	LastMessageUnix          int64
	IngestLatencyBuckets     []utils.HistogramBucket
	IngestLatencyP95Ms       float64
	ProcessLatencyBuckets    []utils.HistogramBucket
	ProcessLatencyP95Ms      float64
	IngestLatencyCount       uint64
	IngestLatencySumMs       float64
	ProcessLatencyCount      uint64
	ProcessLatencySumMs      float64
}

func copyCounterMap(src map[string]uint64) map[string]uint64 {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]uint64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
