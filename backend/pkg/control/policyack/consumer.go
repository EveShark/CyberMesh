package policyack

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/control/policytrace"
	"backend/pkg/observability"
	"backend/pkg/utils"
	pb "backend/proto"
	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"
)

var legacyAckEventNamespace = uuid.MustParse("645a4f66-b019-5290-9d8d-9c44bb5f9d58")

type Consumer struct {
	cfg         Config
	store       *Store
	trust       *TrustedKeys
	log         *utils.Logger
	audit       *utils.AuditLogger
	dlqProducer sarama.SyncProducer
	trace       *policytrace.Collector
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup

	cg sarama.ConsumerGroup

	processedTotal          atomic.Uint64
	rejectedTotal           atomic.Uint64
	storeRetryAttempts      atomic.Uint64
	storeRetryExhausted     atomic.Uint64
	dlqPublishedTotal       atomic.Uint64
	dlqPublishFailures      atomic.Uint64
	loopErrors              atomic.Uint64
	workQueueWaits          atomic.Uint64
	softThrottleActivations atomic.Uint64
	transientFailureStreak  atomic.Uint64
	logsThrottled           atomic.Uint64
	lastLoopErrLogNs        int64
	lastStoreWarnLogNs      int64
	lastRejectLogNs         int64
	lastDLQErrLogNs         int64
	partitionAssignments    atomic.Uint64
	partitionRevocations    atomic.Uint64
	partitionsOwnedCurrent  atomic.Uint64
}

type ConsumerStats struct {
	ProcessedTotal          uint64
	RejectedTotal           uint64
	StoreRetryAttempts      uint64
	StoreRetryExhausted     uint64
	DLQPublishedTotal       uint64
	DLQPublishFailures      uint64
	LoopErrors              uint64
	WorkQueueWaits          uint64
	SoftThrottleActivations uint64
	LogsThrottled           uint64
	PartitionAssignments    uint64
	PartitionRevocations    uint64
	PartitionsOwnedCurrent  uint64
}

type CausalStats struct {
	SkewCorrectionsTotal       uint64
	CorrelationCommand         uint64
	CorrelationExact           uint64
	CorrelationFallbackHash    uint64
	CorrelationFallbackTrace   uint64
	CorrelationNoMatch         uint64
	CorrelationErrors          uint64
	AIEventUnitCorrections     uint64
	AIEventInvalidTotal        uint64
	SourceEventUnitCorrections uint64
	SourceEventInvalidTotal    uint64
	AIToAckBuckets             []utils.HistogramBucket
	AIToAckCount               uint64
	AIToAckSumMs               float64
	AIToAckP95Ms               float64
	SourceToAckBuckets         []utils.HistogramBucket
	SourceToAckCount           uint64
	SourceToAckSumMs           float64
	SourceToAckP95Ms           float64
	PublishToAckBuckets        []utils.HistogramBucket
	PublishToAckCount          uint64
	PublishToAckSumMs          float64
	PublishToAckP95Ms          float64
	PublishToAppliedBuckets    []utils.HistogramBucket
	PublishToAppliedCount      uint64
	PublishToAppliedSumMs      float64
	PublishToAppliedP95Ms      float64
	AppliedToAckBuckets        []utils.HistogramBucket
	AppliedToAckCount          uint64
	AppliedToAckSumMs          float64
	AppliedToAckP95Ms          float64
	CorrelationLatencyBuckets  []utils.HistogramBucket
	CorrelationLatencyCount    uint64
	CorrelationLatencySumMs    float64
	CorrelationLatencyP95Ms    float64
}

type Options struct {
	Config         Config
	Store          *Store
	Trust          *TrustedKeys
	Logger         *utils.Logger
	Audit          *utils.AuditLogger
	DLQ            sarama.SyncProducer
	TraceCollector *policytrace.Collector
}

func New(ctx context.Context, group sarama.ConsumerGroup, opts Options) (*Consumer, error) {
	if group == nil {
		return nil, fmt.Errorf("policy ack consumer: consumer group required")
	}
	if !opts.Config.Enabled {
		return nil, fmt.Errorf("policy ack consumer: disabled")
	}
	if opts.Store == nil {
		return nil, fmt.Errorf("policy ack consumer: store required")
	}
	if opts.Config.SigningRequired && opts.Trust == nil {
		return nil, fmt.Errorf("policy ack consumer: signing required but trust store is nil")
	}

	cctx, cancel := context.WithCancel(ctx)
	return &Consumer{
		cfg:         opts.Config,
		store:       opts.Store,
		trust:       opts.Trust,
		log:         opts.Logger,
		audit:       opts.Audit,
		dlqProducer: opts.DLQ,
		trace:       opts.TraceCollector,
		ctx:         cctx,
		cancel:      cancel,
		cg:          group,
	}, nil
}

func (c *Consumer) Start() error {
	if c == nil {
		return fmt.Errorf("policy ack consumer: nil")
	}
	c.wg.Add(1)
	go c.loop()
	return nil
}

func (c *Consumer) Stop() error {
	if c == nil {
		return nil
	}
	c.cancel()
	c.wg.Wait()
	if c.dlqProducer != nil {
		_ = c.dlqProducer.Close()
	}
	if c.cg != nil {
		return c.cg.Close()
	}
	return nil
}

func (c *Consumer) loop() {
	defer c.wg.Done()

	handler := &handler{c: c}
	for {
		if err := c.cg.Consume(c.ctx, []string{c.cfg.Topic}, handler); err != nil {
			if errors.Is(err, sarama.ErrClosedConsumerGroup) {
				return
			}
			c.loopErrors.Add(1)
			if c.log != nil && c.shouldLog(&c.lastLoopErrLogNs) {
				c.log.ErrorContext(c.ctx, "policy ack consumer error, retrying",
					utils.ZapError(err),
					utils.ZapString("topic", c.cfg.Topic))
			}
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}
		if c.ctx.Err() != nil {
			return
		}
	}
}

type handler struct {
	c *Consumer
}

func (h *handler) Setup(sess sarama.ConsumerGroupSession) error {
	if h == nil || h.c == nil || sess == nil {
		return nil
	}
	var claims uint64
	for _, parts := range sess.Claims() {
		claims += uint64(len(parts))
	}
	h.c.partitionAssignments.Add(claims)
	h.c.partitionsOwnedCurrent.Store(claims)
	return nil
}

func (h *handler) Cleanup(sess sarama.ConsumerGroupSession) error {
	if h == nil || h.c == nil || sess == nil {
		return nil
	}
	var claims uint64
	for _, parts := range sess.Claims() {
		claims += uint64(len(parts))
	}
	h.c.partitionRevocations.Add(claims)
	h.c.partitionsOwnedCurrent.Store(0)
	return nil
}

func (h *handler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	workers := h.c.cfg.Workers
	if workers <= 0 {
		workers = 1
	}
	queueSize := h.c.cfg.WorkQueueSize
	if queueSize <= 0 {
		queueSize = workers * 64
	}

	type workItem struct {
		msg *sarama.ConsumerMessage
	}
	workCh := make(chan workItem, queueSize)
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(sess.Context())
	defer cancel()

	var wg sync.WaitGroup
	workerFn := func() {
		defer wg.Done()
		for item := range workCh {
			if item.msg == nil {
				continue
			}
			msgCtx := observability.ExtractContextFromSaramaHeaders(ctx, item.msg.Headers)
			err := h.c.process(msgCtx, item.msg)
			if err == nil {
				sess.MarkMessage(item.msg, "")
				continue
			}
			if errors.Is(err, errPermanent) {
				sess.MarkMessage(item.msg, "")
				continue
			}
			select {
			case errCh <- err:
			default:
			}
			if h.c.cfg.SoftThrottleEnabled {
				streak := h.c.transientFailureStreak.Add(1)
				if int(streak) >= h.c.cfg.SoftThrottleWindow && h.c.cfg.SoftThrottleSleep > 0 {
					h.c.softThrottleActivations.Add(1)
					select {
					case <-time.After(h.c.cfg.SoftThrottleSleep):
					case <-ctx.Done():
					}
				}
			}
			cancel()
			return
		}
	}

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go workerFn()
	}
	defer func() {
		close(workCh)
		wg.Wait()
	}()

	for {
		select {
		case <-ctx.Done():
			select {
			case err := <-errCh:
				return err
			default:
				return nil
			}
		case msg := <-claim.Messages():
			if msg == nil {
				return nil
			}
			queued := false
			select {
			case workCh <- workItem{msg: msg}:
				queued = true
			default:
				h.c.workQueueWaits.Add(1)
			}
			if queued {
				continue
			}
			select {
			case workCh <- workItem{msg: msg}:
			case err := <-errCh:
				return err
			case <-ctx.Done():
				select {
				case err := <-errCh:
					return err
				default:
					return nil
				}
			}
		}
	}
}

var errPermanent = errors.New("permanent")

func (c *Consumer) process(ctx context.Context, msg *sarama.ConsumerMessage) error {
	ctx, span := observability.Tracer("backend/policyack").Start(
		ctx,
		"backend.policyack.consume",
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination", msg.Topic),
			attribute.Int64("messaging.kafka.partition", int64(msg.Partition)),
			attribute.Int64("messaging.kafka.offset", msg.Offset),
		),
	)
	defer span.End()
	var ack pb.PolicyAckEvent
	if err := proto.Unmarshal(msg.Value, &ack); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "ack_unmarshal_failed")
		c.rejectedTotal.Add(1)
		c.sendDLQ(ctx, msg, "unmarshal_failed", err)
		return fmt.Errorf("%w: policy ack unmarshal: %v", errPermanent, err)
	}

	if strings.TrimSpace(ack.PolicyId) == "" || strings.TrimSpace(ack.ControllerInstance) == "" {
		span.SetStatus(codes.Error, "invalid_required_fields")
		c.rejectedTotal.Add(1)
		c.sendDLQ(ctx, msg, "invalid_required_fields", fmt.Errorf("policy_id/controller_instance required"))
		return fmt.Errorf("%w: invalid required fields", errPermanent)
	}
	if c.cfg.RequireQCRef && strings.TrimSpace(ack.QcReference) == "" && strings.TrimSpace(ack.TraceId) == "" {
		c.rejectedTotal.Add(1)
		c.sendDLQ(ctx, msg, "missing_trace_reference", fmt.Errorf("trace_id or qc_reference required"))
		return fmt.Errorf("%w: missing trace_id or qc_reference", errPermanent)
	}
	if c.cfg.RequireRuleHash && len(ack.RuleHash) == 0 {
		c.rejectedTotal.Add(1)
		c.sendDLQ(ctx, msg, "missing_rule_hash", fmt.Errorf("rule_hash required"))
		return fmt.Errorf("%w: missing rule_hash", errPermanent)
	}
	if len(ack.RuleHash) > 0 && len(ack.RuleHash) != 32 {
		c.rejectedTotal.Add(1)
		c.sendDLQ(ctx, msg, "invalid_rule_hash_size", fmt.Errorf("rule_hash must be 32 bytes"))
		return fmt.Errorf("%w: invalid rule_hash size", errPermanent)
	}
	if c.cfg.RequireProducer && len(ack.ProducerId) == 0 {
		c.rejectedTotal.Add(1)
		c.sendDLQ(ctx, msg, "missing_producer_id", fmt.Errorf("producer_id required"))
		return fmt.Errorf("%w: missing producer_id", errPermanent)
	}
	if len(ack.ProducerId) > 0 && len(ack.ProducerId) != 32 {
		c.rejectedTotal.Add(1)
		c.sendDLQ(ctx, msg, "invalid_producer_id_size", fmt.Errorf("producer_id must be 32 bytes"))
		return fmt.Errorf("%w: invalid producer_id size", errPermanent)
	}

	res, cls, ok := normalizeAckOutcome(strings.TrimSpace(ack.Result), strings.TrimSpace(ack.ErrorCode), strings.TrimSpace(ack.Reason))
	if !ok {
		span.SetStatus(codes.Error, "invalid_ack_result")
		c.rejectedTotal.Add(1)
		c.sendDLQ(ctx, msg, "invalid_result", fmt.Errorf("result=%q", ack.Result))
		return fmt.Errorf("%w: invalid result", errPermanent)
	}
	ack.Result = res
	span.SetAttributes(
		attribute.String("policy.id", strings.TrimSpace(ack.PolicyId)),
		attribute.String("ack.result", res),
		attribute.String("ack.class", cls),
	)
	if strings.TrimSpace(ack.ErrorCode) == "" {
		ack.ErrorCode = cls
	}
	if strings.TrimSpace(ack.AckEventId) == "" {
		ack.AckEventId = fallbackAckEventID(msg)
	}

	sig := header(msg.Headers, "ack-signature")
	algo := strings.ToLower(string(header(msg.Headers, "ack-signature-alg")))
	if len(sig) == 0 {
		if c.cfg.SigningRequired {
			c.rejectedTotal.Add(1)
			c.sendDLQ(ctx, msg, "missing_signature", fmt.Errorf("ack-signature header missing"))
			return fmt.Errorf("%w: signature required", errPermanent)
		}
	} else {
		if algo != "" && algo != "ed25519" {
			c.rejectedTotal.Add(1)
			c.sendDLQ(ctx, msg, "unsupported_signature_alg", fmt.Errorf("alg=%q", algo))
			return fmt.Errorf("%w: unsupported signature alg", errPermanent)
		}
		if c.trust == nil || !c.trust.Verify(msg.Value, sig) {
			c.rejectedTotal.Add(1)
			c.sendDLQ(ctx, msg, "signature_verify_failed", fmt.Errorf("verify failed"))
			return fmt.Errorf("%w: signature verify failed", errPermanent)
		}
	}

	// Store with bounded retry for transient DB errors.
	backoff := c.cfg.StoreRetryBackoff
	var last error
	for attempt := 1; attempt <= c.cfg.StoreRetryMax; attempt++ {
		if err := c.store.Upsert(ctx, &ack, time.Now().UTC()); err != nil {
			span.RecordError(err, trace.WithAttributes(attribute.Int("retry.attempt", attempt)))
			last = err
			c.storeRetryAttempts.Add(1)
			if c.log != nil && c.shouldLog(&c.lastStoreWarnLogNs) {
				c.log.WarnContext(ctx, "policy ack store failed; retrying",
					utils.ZapInt("attempt", attempt),
					utils.ZapDuration("backoff", backoff),
					utils.ZapString("policy_id", ack.PolicyId),
					utils.ZapError(err))
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			continue
		}
		c.processedTotal.Add(1)
		span.SetStatus(codes.Ok, "ack_persisted")
		c.transientFailureStreak.Store(0)
		if c.audit != nil {
			_ = c.audit.Info("policy_ack_persisted", map[string]interface{}{
				"policy_id":           ack.PolicyId,
				"controller_instance": ack.ControllerInstance,
				"result":              ack.Result,
				"error_code":          ack.ErrorCode,
				"fast_path":           ack.FastPath,
				"ack_topic":           c.cfg.Topic,
				"consumer_group":      c.cfg.GroupID,
				"partition":           msg.Partition,
				"offset":              msg.Offset,
				"signature_present":   len(sig) > 0,
				"signature_alg":       algo,
			})
		}
		if c.trace != nil {
			traceID := strings.TrimSpace(ack.TraceId)
			if traceID == "" {
				traceID = strings.TrimSpace(ack.QcReference)
			}
			c.trace.Record(policytrace.Marker{
				Stage:       "t_ack",
				PolicyID:    ack.PolicyId,
				TraceID:     traceID,
				TimestampMs: time.Now().UnixMilli(),
			})
		}
		return nil
	}

	// Exhausted retries: treat as transient so we don't drop data.
	c.storeRetryExhausted.Add(1)
	span.SetStatus(codes.Error, "store_retry_exhausted")
	if last != nil {
		return fmt.Errorf("policy ack store retries exhausted: %w", last)
	}
	return fmt.Errorf("policy ack store retries exhausted")
}

func normalizeAckOutcome(resultRaw, errorCodeRaw, reasonRaw string) (string, string, bool) {
	result := strings.ToLower(strings.TrimSpace(resultRaw))
	errorCode := strings.ToLower(strings.TrimSpace(errorCodeRaw))
	reason := strings.ToLower(strings.TrimSpace(reasonRaw))

	timeoutLike := strings.Contains(errorCode, "timeout") || strings.Contains(errorCode, "deadline")
	if !timeoutLike {
		timeoutLike = strings.Contains(reason, "timeout") || strings.Contains(reason, "deadline")
	}
	retryExhaustedLike := strings.Contains(errorCode, "retry_exhausted") || strings.Contains(errorCode, "retries_exhausted")
	if !retryExhaustedLike {
		retryExhaustedLike = strings.Contains(reason, "retry_exhausted") || strings.Contains(reason, "retries_exhausted")
	}
	noopLike := strings.Contains(errorCode, "noop") || strings.Contains(reason, "noop") || strings.Contains(errorCode, "duplicate")

	switch result {
	case "accepted":
		return "accepted", "accepted", true
	case "applied":
		if noopLike {
			return "noop", "noop", true
		}
		return "applied", "applied", true
	case "failed":
		if timeoutLike {
			return "timeout", "timeout", true
		}
		if retryExhaustedLike {
			return "retry_exhausted", "retry_exhausted", true
		}
		return "rejected", "rejected", true
	case "noop":
		return "noop", "noop", true
	case "rejected":
		return "rejected", "rejected", true
	case "timeout":
		return "timeout", "timeout", true
	case "retry_exhausted":
		return "retry_exhausted", "retry_exhausted", true
	default:
		return "", "", false
	}
}

func fallbackAckEventID(msg *sarama.ConsumerMessage) string {
	if msg == nil {
		return uuid.NewSHA1(legacyAckEventNamespace, []byte("ack:nil")).String()
	}
	if msg.Topic != "" || msg.Partition != 0 || msg.Offset != 0 {
		seed := fmt.Sprintf("ack:%s:%d:%d", msg.Topic, msg.Partition, msg.Offset)
		return uuid.NewSHA1(legacyAckEventNamespace, []byte(seed)).String()
	}
	sum := sha256.Sum256(msg.Value)
	seed := fmt.Sprintf("ack:raw:%x", sum[:])
	return uuid.NewSHA1(legacyAckEventNamespace, []byte(seed)).String()
}

func header(headers []*sarama.RecordHeader, key string) []byte {
	for _, h := range headers {
		if h == nil || len(h.Key) == 0 {
			continue
		}
		if string(h.Key) == key {
			return h.Value
		}
	}
	return nil
}

func (c *Consumer) sendDLQ(ctx context.Context, msg *sarama.ConsumerMessage, reason string, err error) {
	if c.audit != nil {
		_ = c.audit.Warn("policy_ack_rejected", map[string]interface{}{
			"reason":    reason,
			"error":     errString(err),
			"topic":     msg.Topic,
			"partition": msg.Partition,
			"offset":    msg.Offset,
		})
	}

	if c.dlqProducer == nil || c.cfg.DLQ == "" {
		if c.log != nil && c.shouldLog(&c.lastRejectLogNs) {
			c.log.WarnContext(ctx, "policy ack rejected (no dlq configured)",
				utils.ZapString("reason", reason),
				utils.ZapError(err))
		}
		return
	}

	headers := []sarama.RecordHeader{
		{Key: []byte("type"), Value: []byte("policy_ack")},
		{Key: []byte("reason"), Value: []byte(reason)},
		{Key: []byte("original_topic"), Value: []byte(msg.Topic)},
		{Key: []byte("original_partition"), Value: []byte(fmt.Sprintf("%d", msg.Partition))},
		{Key: []byte("original_offset"), Value: []byte(fmt.Sprintf("%d", msg.Offset))},
	}
	if err != nil {
		headers = append(headers, sarama.RecordHeader{Key: []byte("error"), Value: []byte(err.Error())})
	}

	key := sarama.StringEncoder(fmt.Sprintf("ack:%s:%d:%d", msg.Topic, msg.Partition, msg.Offset))
	dlqMsg := &sarama.ProducerMessage{
		Topic:   c.cfg.DLQ,
		Key:     key,
		Value:   sarama.ByteEncoder(msg.Value),
		Headers: headers,
	}
	dlqMsg.Headers = observability.InjectContextToSaramaHeaders(ctx, dlqMsg.Headers)
	if _, _, perr := c.dlqProducer.SendMessage(dlqMsg); perr != nil {
		c.dlqPublishFailures.Add(1)
		if c.log != nil && c.shouldLog(&c.lastDLQErrLogNs) {
			c.log.WarnContext(ctx, "failed to publish policy ack dlq",
				utils.ZapString("dlq_topic", c.cfg.DLQ),
				utils.ZapError(perr))
		}
		return
	}
	c.dlqPublishedTotal.Add(1)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (c *Consumer) shouldLog(lastNs *int64) bool {
	throttle := c.cfg.LogThrottle
	if throttle <= 0 {
		throttle = 5 * time.Second
	}
	now := time.Now().UnixNano()
	prev := atomic.LoadInt64(lastNs)
	if prev == 0 || time.Duration(now-prev) >= throttle {
		if atomic.CompareAndSwapInt64(lastNs, prev, now) {
			return true
		}
	}
	c.logsThrottled.Add(1)
	return false
}

// Stats returns runtime counters for the ACK consumer.
func (c *Consumer) Stats() ConsumerStats {
	if c == nil {
		return ConsumerStats{}
	}
	return ConsumerStats{
		ProcessedTotal:          c.processedTotal.Load(),
		RejectedTotal:           c.rejectedTotal.Load(),
		StoreRetryAttempts:      c.storeRetryAttempts.Load(),
		StoreRetryExhausted:     c.storeRetryExhausted.Load(),
		DLQPublishedTotal:       c.dlqPublishedTotal.Load(),
		DLQPublishFailures:      c.dlqPublishFailures.Load(),
		LoopErrors:              c.loopErrors.Load(),
		WorkQueueWaits:          c.workQueueWaits.Load(),
		SoftThrottleActivations: c.softThrottleActivations.Load(),
		LogsThrottled:           c.logsThrottled.Load(),
		PartitionAssignments:    c.partitionAssignments.Load(),
		PartitionRevocations:    c.partitionRevocations.Load(),
		PartitionsOwnedCurrent:  c.partitionsOwnedCurrent.Load(),
	}
}

func (c *Consumer) CausalStats() CausalStats {
	if c == nil || c.store == nil {
		return CausalStats{}
	}
	s := c.store.Stats()
	return CausalStats{
		SkewCorrectionsTotal:       s.SkewCorrectionsTotal,
		CorrelationCommand:         s.CorrelationCommand,
		CorrelationExact:           s.CorrelationExact,
		CorrelationFallbackHash:    s.CorrelationFallbackHash,
		CorrelationFallbackTrace:   s.CorrelationFallbackTrace,
		CorrelationNoMatch:         s.CorrelationNoMatch,
		CorrelationErrors:          s.CorrelationErrors,
		AIEventUnitCorrections:     s.AIEventUnitCorrections,
		AIEventInvalidTotal:        s.AIEventInvalidTotal,
		SourceEventUnitCorrections: s.SourceEventUnitCorrections,
		SourceEventInvalidTotal:    s.SourceEventInvalidTotal,
		AIToAckBuckets:             s.AIToAckBuckets,
		AIToAckCount:               s.AIToAckCount,
		AIToAckSumMs:               s.AIToAckSumMs,
		AIToAckP95Ms:               s.AIToAckP95Ms,
		SourceToAckBuckets:         s.SourceToAckBuckets,
		SourceToAckCount:           s.SourceToAckCount,
		SourceToAckSumMs:           s.SourceToAckSumMs,
		SourceToAckP95Ms:           s.SourceToAckP95Ms,
		PublishToAckBuckets:        s.PublishToAckBuckets,
		PublishToAckCount:          s.PublishToAckCount,
		PublishToAckSumMs:          s.PublishToAckSumMs,
		PublishToAckP95Ms:          s.PublishToAckP95Ms,
		PublishToAppliedBuckets:    s.PublishToAppliedBuckets,
		PublishToAppliedCount:      s.PublishToAppliedCount,
		PublishToAppliedSumMs:      s.PublishToAppliedSumMs,
		PublishToAppliedP95Ms:      s.PublishToAppliedP95Ms,
		AppliedToAckBuckets:        s.AppliedToAckBuckets,
		AppliedToAckCount:          s.AppliedToAckCount,
		AppliedToAckSumMs:          s.AppliedToAckSumMs,
		AppliedToAckP95Ms:          s.AppliedToAckP95Ms,
		CorrelationLatencyBuckets:  s.CorrelationLatencyBuckets,
		CorrelationLatencyCount:    s.CorrelationLatencyCount,
		CorrelationLatencySumMs:    s.CorrelationLatencySumMs,
		CorrelationLatencyP95Ms:    s.CorrelationLatencyP95Ms,
	}
}
