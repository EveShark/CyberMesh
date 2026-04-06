package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/metrics"
	"github.com/CyberMesh/enforcement-agent/internal/observability"
)

// MessageHandler is invoked for each Kafka message.
type MessageHandler interface {
	HandleMessage(ctx context.Context, msg *sarama.ConsumerMessage) error
}

// MessageLaneClassifier optionally classifies messages for lane-aware admission.
type MessageLaneClassifier interface {
	MessageLane(msg *sarama.ConsumerMessage) string
}

// Consumer wraps a Sarama consumer group with graceful shutdown support.
type Consumer struct {
	group                   sarama.ConsumerGroup
	handler                 MessageHandler
	topic                   string
	workers                 int
	queueSize               int
	stallThreshold          time.Duration
	admissionPause          time.Duration
	dispatchMaxWait         time.Duration
	dispatchPause           time.Duration
	handlerTimeout          time.Duration
	stripeDistressThreshold int
	criticalMaxInFlight     int
	maintenanceMaxInFlight  int
	mu                      sync.Mutex
	closed                  bool
	metrics                 *metrics.Recorder
	errorOnce               sync.Once
	logger                  *zap.Logger
}

// Config represents the options for constructing a consumer.
type Config struct {
	Brokers []string
	GroupID string
	Topic   string
	// ProtocolVersion is a sarama.ParseKafkaVersion string (e.g. "2.1.0", "3.6.0").
	// Default: "3.6.0".
	ProtocolVersion         string
	TLS                     bool
	TLSCAPath               string
	TLSCertPath             string
	TLSKeyPath              string
	SASLEnabled             bool
	SASLMechanism           string
	SASLUsername            string
	SASLPassword            string
	HandlerWorkers          int
	HandlerQueue            int
	StallThreshold          time.Duration
	AdmissionPause          time.Duration
	DispatchMaxWait         time.Duration
	DispatchPause           time.Duration
	HandlerTimeout          time.Duration
	StripeDistressThreshold int
	CriticalMaxInFlight     int
	MaintenanceMaxInFlight  int
	Metrics                 *metrics.Recorder
	Logger                  *zap.Logger
}

// NewConsumer creates a Consumer instance.
func NewConsumer(cfg Config, handler MessageHandler) (*Consumer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("kafka consumer: brokers required")
	}
	if cfg.Topic == "" {
		return nil, fmt.Errorf("kafka consumer: topic required")
	}
	if cfg.GroupID == "" {
		return nil, fmt.Errorf("kafka consumer: group id required")
	}

	saramaCfg := sarama.NewConfig()
	versionStr := cfg.ProtocolVersion
	if versionStr == "" {
		versionStr = "3.6.0"
	}
	ver, err := sarama.ParseKafkaVersion(versionStr)
	if err != nil {
		return nil, fmt.Errorf("kafka consumer: invalid protocol version %q: %w", versionStr, err)
	}
	saramaCfg.Version = ver
	saramaCfg.Consumer.Return.Errors = true
	saramaCfg.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRange
	saramaCfg.Consumer.Offsets.Initial = sarama.OffsetNewest

	if cfg.TLS {
		tlsConfig, err := buildTLSConfig(cfg)
		if err != nil {
			return nil, err
		}
		saramaCfg.Net.TLS.Enable = true
		saramaCfg.Net.TLS.Config = tlsConfig
	}

	if cfg.SASLEnabled {
		saramaCfg.Net.SASL.Enable = true
		saramaCfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		if cfg.SASLMechanism == "SCRAM-SHA-256" {
			saramaCfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
		} else if cfg.SASLMechanism == "SCRAM-SHA-512" {
			saramaCfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
		}
		saramaCfg.Net.SASL.User = cfg.SASLUsername
		saramaCfg.Net.SASL.Password = cfg.SASLPassword
	}

	group, err := sarama.NewConsumerGroup(cfg.Brokers, cfg.GroupID, saramaCfg)
	if err != nil {
		return nil, fmt.Errorf("kafka consumer: create group: %w", err)
	}
	workers := cfg.HandlerWorkers
	if workers <= 0 {
		workers = 8
	}
	queueSize := cfg.HandlerQueue
	if queueSize <= 0 {
		queueSize = 256
	}

	return &Consumer{
		group:                   group,
		handler:                 handler,
		topic:                   cfg.Topic,
		workers:                 workers,
		queueSize:               queueSize,
		stallThreshold:          cfg.StallThreshold,
		admissionPause:          cfg.AdmissionPause,
		dispatchMaxWait:         cfg.DispatchMaxWait,
		dispatchPause:           cfg.DispatchPause,
		handlerTimeout:          cfg.HandlerTimeout,
		stripeDistressThreshold: cfg.StripeDistressThreshold,
		criticalMaxInFlight:     cfg.CriticalMaxInFlight,
		maintenanceMaxInFlight:  cfg.MaintenanceMaxInFlight,
		metrics:                 cfg.Metrics,
		logger:                  cfg.Logger,
	}, nil
}

// Close shuts down the consumer group.
func (c *Consumer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.group.Close()
}

// Run starts consuming messages until the context is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	consumer := &groupHandler{
		handler:                 c.handler,
		metrics:                 c.metrics,
		logger:                  c.logger,
		workers:                 c.workers,
		queueSize:               c.queueSize,
		stallThreshold:          c.stallThreshold,
		admissionPause:          c.admissionPause,
		dispatchMaxWait:         c.dispatchMaxWait,
		dispatchPause:           c.dispatchPause,
		handlerTimeout:          c.handlerTimeout,
		stripeDistressThreshold: c.stripeDistressThreshold,
		criticalMaxInFlight:     c.criticalMaxInFlight,
		maintenanceMaxInFlight:  c.maintenanceMaxInFlight,
	}

	c.errorOnce.Do(func() {
		go c.observeErrors(ctx)
	})

	for {
		if err := c.group.Consume(ctx, []string{c.topic}, consumer); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

func (c *Consumer) observeErrors(ctx context.Context) {
	if c.metrics == nil {
		return
	}
	errs := c.group.Errors()
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-errs:
			if !ok {
				return
			}
			if err != nil {
				c.metrics.ObserveKafkaError(err.Error())
			}
		}
	}
}

type groupHandler struct {
	handler                 MessageHandler
	laneClassifier          MessageLaneClassifier
	metrics                 *metrics.Recorder
	logger                  *zap.Logger
	workers                 int
	queueSize               int
	stallThreshold          time.Duration
	admissionPause          time.Duration
	dispatchMaxWait         time.Duration
	dispatchPause           time.Duration
	handlerTimeout          time.Duration
	stripeDistressThreshold int
	criticalMaxInFlight     int
	maintenanceMaxInFlight  int
}

func (g *groupHandler) Setup(session sarama.ConsumerGroupSession) error {
	if session == nil {
		return nil
	}
	for topic, partitions := range session.Claims() {
		for _, partition := range partitions {
			if g.metrics != nil {
				g.metrics.ObservePartitionAssigned(partition)
			}
			if g.logger != nil {
				g.logger.Info("policy consumer partition assigned",
					zap.String("topic", topic),
					zap.Int32("partition", partition),
					zap.String("member_id", session.MemberID()),
					zap.Int32("generation", session.GenerationID()))
			}
		}
	}
	return nil
}

func (g *groupHandler) Cleanup(session sarama.ConsumerGroupSession) error {
	if session == nil {
		return nil
	}
	for topic, partitions := range session.Claims() {
		for _, partition := range partitions {
			if g.metrics != nil {
				g.metrics.ObservePartitionRevoked(partition)
			}
			if g.logger != nil {
				g.logger.Info("policy consumer partition revoked",
					zap.String("topic", topic),
					zap.Int32("partition", partition),
					zap.String("member_id", session.MemberID()),
					zap.Int32("generation", session.GenerationID()))
			}
		}
	}
	return nil
}

func (g *groupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	if classifier, ok := g.handler.(MessageLaneClassifier); ok {
		g.laneClassifier = classifier
	}
	if g.logger != nil {
		g.logger.Info("policy consumer claim started",
			zap.String("topic", claim.Topic()),
			zap.Int32("partition", claim.Partition()),
			zap.Int64("initial_offset", claim.InitialOffset()),
			zap.Int64("high_watermark", claim.HighWaterMarkOffset()))
	}
	defer func() {
		if g.logger != nil {
			g.logger.Info("policy consumer claim stopped",
				zap.String("topic", claim.Topic()),
				zap.Int32("partition", claim.Partition()))
		}
	}()

	workers := g.workers
	if workers <= 0 {
		workers = 8
	}
	queueSize := g.queueSize
	if queueSize <= 0 {
		queueSize = 256
	}
	return g.consumeClaimParallel(session, claim, workers, queueSize)
}

type consumerTask struct {
	msg   *sarama.ConsumerMessage
	start time.Time
	lane  string
}

type consumerResult struct {
	msg      *sarama.ConsumerMessage
	start    time.Time
	done     time.Time
	err      error
	canceled bool
	lane     string
}

func (g *groupHandler) consumeClaimParallel(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim, workers int, queueSize int) error {
	if workers < 1 {
		workers = 1
	}
	if queueSize < workers {
		queueSize = workers
	}
	if queueSize < 1 {
		queueSize = 1
	}

	workerQueues := make([]chan consumerTask, workers)
	for i := 0; i < workers; i++ {
		workerQueues[i] = make(chan consumerTask, queueSize)
	}
	results := make(chan consumerResult, queueSize*2)
	stripeStartedAtNs := make([]int64, workers)
	stripeStallLatched := make([]uint32, workers)
	for i := 0; i < workers; i++ {
		if g.metrics != nil {
			g.metrics.ObserveKafkaStripeInflight(i, 0)
			g.metrics.ObserveKafkaStripeOldestInflight(i, 0)
			g.metrics.SetKafkaStripeDistressActive(i, false)
		}
	}

	var stallTicker *time.Ticker
	if g.stallThreshold > 0 {
		interval := g.stallThreshold / 2
		if interval < 100*time.Millisecond {
			interval = 100 * time.Millisecond
		}
		stallTicker = time.NewTicker(interval)
		defer stallTicker.Stop()
	}

	var workerWG sync.WaitGroup
	for i := 0; i < workers; i++ {
		workerWG.Add(1)
		go func(idx int, ch <-chan consumerTask) {
			defer workerWG.Done()
			for task := range ch {
				nowNs := time.Now().UnixNano()
				atomic.StoreInt64(&stripeStartedAtNs[idx], nowNs)
				atomic.StoreUint32(&stripeStallLatched[idx], 0)
				if g.metrics != nil {
					g.metrics.ObserveKafkaStripeInflight(idx, 1)
				}
				handlerCtx := observability.ExtractContextFromSaramaHeaders(session.Context(), task.msg.Headers)
				cancel := func() {}
				if g.handlerTimeout > 0 {
					handlerCtx, cancel = context.WithTimeout(handlerCtx, g.handlerTimeout)
				}
				err := g.handler.HandleMessage(handlerCtx, task.msg)
				cancel()
				done := time.Now()
				atomic.StoreInt64(&stripeStartedAtNs[idx], 0)
				atomic.StoreUint32(&stripeStallLatched[idx], 0)
				if g.metrics != nil {
					g.metrics.ObserveKafkaStripeInflight(idx, 0)
					g.metrics.ObserveKafkaStripeOldestInflight(idx, 0)
				}
				canceled := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
				res := consumerResult{
					msg:      task.msg,
					start:    task.start,
					done:     done,
					err:      err,
					canceled: canceled,
					lane:     task.lane,
				}
				select {
				case results <- res:
				case <-session.Context().Done():
					return
				}
			}
		}(i, workerQueues[i])
	}

	defer func() {
		if g.metrics != nil {
			for i := 0; i < workers; i++ {
				g.metrics.SetKafkaStripeDistressActive(i, false)
			}
		}
		for _, ch := range workerQueues {
			close(ch)
		}
		workerWG.Wait()
		close(results)
	}()

	var (
		inflight             int
		criticalInFlight     int
		maintenanceInFlight  int
		nextOffset           int64
		nextOffsetSet        bool
		pending              = make(map[int64]consumerResult)
		readOpen             = true
		msgPending           *sarama.ConsumerMessage
		saturationByStripe   = make([]int, workers)
		distressActive       = make([]bool, workers)
		admissionPausedUntil time.Time
	)

	for {
		if session.Context().Err() != nil {
			return nil
		}
		if !readOpen && inflight == 0 && msgPending == nil {
			return nil
		}

		if msgPending == nil && readOpen {
			if g.admissionPause > 0 && time.Now().Before(admissionPausedUntil) {
				waitFor := time.Until(admissionPausedUntil)
				if waitFor < 0 {
					waitFor = 0
				}
				select {
				case <-session.Context().Done():
					return nil
				case <-stallTick(stallTicker):
					g.observeStripeStall(stripeStartedAtNs, stripeStallLatched)
				case res := <-results:
					if res.msg == nil {
						continue
					}
					inflight--
					if res.lane == "maintenance" {
						maintenanceInFlight--
					} else {
						criticalInFlight--
					}
					pending[res.msg.Offset] = res
					drainMarkableResults(g, session, pending, &nextOffset, &nextOffsetSet)
				case <-time.After(waitFor):
				}
				continue
			}
			select {
			case <-session.Context().Done():
				return nil
			case <-stallTick(stallTicker):
				g.observeStripeStall(stripeStartedAtNs, stripeStallLatched)
			case res := <-results:
				if res.msg == nil {
					continue
				}
				inflight--
				if res.lane == "maintenance" {
					maintenanceInFlight--
				} else {
					criticalInFlight--
				}
				pending[res.msg.Offset] = res
				drainMarkableResults(g, session, pending, &nextOffset, &nextOffsetSet)
			case msg, ok := <-claim.Messages():
				if !ok {
					readOpen = false
					continue
				}
				msgPending = msg
			}
			continue
		}

		if msgPending != nil {
			if g.metrics != nil {
				lag := claim.HighWaterMarkOffset() - msgPending.Offset - 1
				if lag < 0 {
					lag = 0
				}
				g.metrics.ObserveKafkaLag(msgPending.Partition, lag)
				if !msgPending.Timestamp.IsZero() {
					publishToConsume := time.Since(msgPending.Timestamp)
					if publishToConsume >= 0 {
						g.metrics.ObservePublishToConsume(publishToConsume.Seconds())
					}
				}
			}
			if !nextOffsetSet {
				nextOffset = msgPending.Offset
				nextOffsetSet = true
			}
			lane := g.classifyLane(msgPending)
			if !g.laneAvailable(lane, criticalInFlight, maintenanceInFlight) {
				select {
				case <-session.Context().Done():
					return nil
				case res := <-results:
					if res.msg == nil {
						continue
					}
					inflight--
					if res.lane == "maintenance" {
						maintenanceInFlight--
					} else {
						criticalInFlight--
					}
					pending[res.msg.Offset] = res
					drainMarkableResults(g, session, pending, &nextOffset, &nextOffsetSet)
				case <-time.After(5 * time.Millisecond):
				}
				continue
			}
			task := consumerTask{msg: msgPending, start: time.Now(), lane: lane}
			stripe := workerStripeForMessage(msgPending, workers)
			dispatchStart := time.Now()
			dispatchCtx := observability.ExtractContextFromSaramaHeaders(session.Context(), msgPending.Headers)
			_, dispatchSpan := observability.Tracer("enforcement-agent/kafka-consumer").Start(
				dispatchCtx,
				"enforcement.kafka.dispatch",
				trace.WithAttributes(
					attribute.String("messaging.system", "kafka"),
					attribute.String("messaging.destination", msgPending.Topic),
					attribute.Int64("messaging.kafka.partition", int64(msgPending.Partition)),
					attribute.Int64("messaging.kafka.offset", msgPending.Offset),
					attribute.Int("dispatch.stripe", stripe),
					attribute.String("dispatch.lane", lane),
				),
			)
			select {
			case workerQueues[stripe] <- task:
				dispatchSpan.SetAttributes(attribute.Int64("dispatch.wait_ms", time.Since(dispatchStart).Milliseconds()))
				dispatchSpan.SetStatus(codes.Ok, "queued")
				dispatchSpan.End()
				if g.metrics != nil {
					g.metrics.ObserveKafkaDispatchWait(time.Since(dispatchStart))
				}
				saturationByStripe[stripe] = 0
				if distressActive[stripe] {
					distressActive[stripe] = false
					if g.metrics != nil {
						g.metrics.SetKafkaStripeDistressActive(stripe, false)
					}
				}
				inflight++
				if lane == "maintenance" {
					maintenanceInFlight++
				} else {
					criticalInFlight++
				}
				msgPending = nil
			default:
				dispatchSpan.SetAttributes(attribute.Bool("dispatch.queue_saturated", true))
				if g.metrics != nil {
					g.metrics.ObserveKafkaQueueSaturated()
				}
				saturationByStripe[stripe]++
				threshold := g.stripeDistressThreshold
				if threshold < 1 {
					threshold = 3
				}
				if !distressActive[stripe] && saturationByStripe[stripe] >= threshold {
					distressActive[stripe] = true
					if g.metrics != nil {
						g.metrics.ObserveKafkaStripeDistressOpen(stripe)
						g.metrics.SetKafkaStripeDistressActive(stripe, true)
					}
				}
				if g.admissionPause > 0 {
					admissionPausedUntil = time.Now().Add(g.admissionPause)
					if g.metrics != nil {
						g.metrics.ObserveKafkaAdmissionPause()
					}
				}
				if g.dispatchPause > 0 {
					pauseUntil := time.Now().Add(g.dispatchPause)
					if pauseUntil.After(admissionPausedUntil) {
						admissionPausedUntil = pauseUntil
					}
					if g.metrics != nil {
						g.metrics.ObserveKafkaAdmissionPause()
					}
				}
				var waitBudget <-chan time.Time
				if g.dispatchMaxWait > 0 {
					waitBudget = time.After(g.dispatchMaxWait)
				}
				select {
				case <-session.Context().Done():
					dispatchSpan.SetStatus(codes.Error, "session_done")
					dispatchSpan.End()
					return nil
				case <-stallTick(stallTicker):
					dispatchSpan.SetStatus(codes.Error, "stall_tick")
					dispatchSpan.End()
					g.observeStripeStall(stripeStartedAtNs, stripeStallLatched)
				case <-waitBudget:
					dispatchSpan.SetStatus(codes.Error, "dispatch_budget_exhausted")
					dispatchSpan.End()
					if g.metrics != nil {
						g.metrics.ObserveKafkaDispatchBudgetExhausted()
					}
					// Keep message pending and return to loop to apply pause/read shaping.
					continue
				case res := <-results:
					dispatchSpan.SetStatus(codes.Error, "wait_for_slot")
					dispatchSpan.End()
					if res.msg == nil {
						continue
					}
					inflight--
					if res.lane == "maintenance" {
						maintenanceInFlight--
					} else {
						criticalInFlight--
					}
					pending[res.msg.Offset] = res
					drainMarkableResults(g, session, pending, &nextOffset, &nextOffsetSet)
				case workerQueues[stripe] <- task:
					dispatchSpan.SetAttributes(attribute.Int64("dispatch.wait_ms", time.Since(dispatchStart).Milliseconds()))
					dispatchSpan.SetStatus(codes.Ok, "queued_after_wait")
					dispatchSpan.End()
					if g.metrics != nil {
						g.metrics.ObserveKafkaDispatchWait(time.Since(dispatchStart))
					}
					saturationByStripe[stripe] = 0
					if distressActive[stripe] {
						distressActive[stripe] = false
						if g.metrics != nil {
							g.metrics.SetKafkaStripeDistressActive(stripe, false)
						}
					}
					inflight++
					if lane == "maintenance" {
						maintenanceInFlight++
					} else {
						criticalInFlight++
					}
					msgPending = nil
				}
			}
			continue
		}

		select {
		case <-session.Context().Done():
			return nil
		case <-stallTick(stallTicker):
			g.observeStripeStall(stripeStartedAtNs, stripeStallLatched)
		case res := <-results:
			if res.msg == nil {
				continue
			}
			inflight--
			if res.lane == "maintenance" {
				maintenanceInFlight--
			} else {
				criticalInFlight--
			}
			pending[res.msg.Offset] = res
			drainMarkableResults(g, session, pending, &nextOffset, &nextOffsetSet)
		}
	}
}

func stallTick(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

func (g *groupHandler) observeStripeStall(startedAtNs []int64, latched []uint32) {
	if g == nil || g.stallThreshold <= 0 {
		return
	}
	now := time.Now()
	threshold := g.stallThreshold
	for i := 0; i < len(startedAtNs); i++ {
		started := atomic.LoadInt64(&startedAtNs[i])
		if started <= 0 {
			continue
		}
		age := now.Sub(time.Unix(0, started))
		if g.metrics != nil {
			g.metrics.ObserveKafkaStripeOldestInflight(i, age)
		}
		if age < threshold {
			continue
		}
		if atomic.CompareAndSwapUint32(&latched[i], 0, 1) {
			if g.metrics != nil {
				g.metrics.ObserveKafkaStripeStall(i)
			}
		}
	}
}

func drainMarkableResults(g *groupHandler, session sarama.ConsumerGroupSession, pending map[int64]consumerResult, nextOffset *int64, nextOffsetSet *bool) {
	if !*nextOffsetSet {
		return
	}
	for {
		res, ok := pending[*nextOffset]
		if !ok {
			return
		}
		delete(pending, *nextOffset)

		if res.err != nil {
			if !(res.canceled && session.Context().Err() != nil) {
				if g.metrics != nil {
					g.metrics.ObserveKafkaError("handler")
					g.metrics.ObserveKafkaConsumed(res.msg.Partition, "error")
					g.metrics.ObserveConsumeToDone("error", res.done.Sub(res.start))
				}
				if g.logger != nil {
					g.logger.Warn("policy consumer handler error; marking message and continuing",
						zap.String("topic", res.msg.Topic),
						zap.Int32("partition", res.msg.Partition),
						zap.Int64("offset", res.msg.Offset),
						zap.Error(res.err))
				}
			}
		} else if g.metrics != nil {
			g.metrics.ObserveKafkaConsumed(res.msg.Partition, "success")
			g.metrics.ObserveConsumeToDone("success", res.done.Sub(res.start))
		}

		markStart := time.Now()
		session.MarkMessage(res.msg, "")
		if g.metrics != nil {
			g.metrics.ObserveOffsetMark(time.Since(markStart))
		}
		*nextOffset = *nextOffset + 1
	}
}

func workerStripeForMessage(msg *sarama.ConsumerMessage, workers int) int {
	if workers <= 1 {
		return 0
	}
	key := strings.TrimSpace(policyIDHeader(msg))
	if key == "" {
		key = strings.TrimSpace(string(msg.Key))
	}
	if key == "" {
		key = fmt.Sprintf("partition:%d", msg.Partition)
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(workers))
}

func (g *groupHandler) classifyLane(msg *sarama.ConsumerMessage) string {
	if g != nil && g.laneClassifier != nil {
		lane := strings.ToLower(strings.TrimSpace(g.laneClassifier.MessageLane(msg)))
		if lane == "maintenance" {
			return "maintenance"
		}
	}
	return "critical"
}

func (g *groupHandler) laneAvailable(lane string, criticalInFlight, maintenanceInFlight int) bool {
	if lane == "maintenance" {
		if g.maintenanceMaxInFlight > 0 && maintenanceInFlight >= g.maintenanceMaxInFlight {
			return false
		}
		return true
	}
	if g.criticalMaxInFlight > 0 && criticalInFlight >= g.criticalMaxInFlight {
		return false
	}
	return true
}

func policyIDHeader(msg *sarama.ConsumerMessage) string {
	if msg == nil || len(msg.Headers) == 0 {
		return ""
	}
	for _, h := range msg.Headers {
		if strings.EqualFold(strings.TrimSpace(string(h.Key)), "policy_id") {
			return strings.TrimSpace(string(h.Value))
		}
	}
	return ""
}

func buildTLSConfig(cfg Config) (*tls.Config, error) {
	var caCertPool *x509.CertPool
	var err error

	if cfg.TLSCAPath != "" {
		// Custom CA provided - create empty pool and load it
		caCertPool = x509.NewCertPool()
		caBytes, readErr := ioutil.ReadFile(cfg.TLSCAPath)
		if readErr != nil {
			return nil, fmt.Errorf("kafka consumer: read ca: %w", readErr)
		}
		if ok := caCertPool.AppendCertsFromPEM(caBytes); !ok {
			return nil, fmt.Errorf("kafka consumer: invalid ca cert")
		}
	} else {
		// No custom CA - use system CA certs for Confluent Cloud
		caCertPool, err = x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("kafka consumer: load system ca: %w", err)
		}
	}

	var certs []tls.Certificate
	if cfg.TLSCertPath != "" && cfg.TLSKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertPath, cfg.TLSKeyPath)
		if err != nil {
			return nil, fmt.Errorf("kafka consumer: load client cert: %w", err)
		}
		certs = append(certs, cert)
	}

	return &tls.Config{
		RootCAs:      caCertPool,
		Certificates: certs,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// Health checks rely on consumer errors during runtime in this MVP.
