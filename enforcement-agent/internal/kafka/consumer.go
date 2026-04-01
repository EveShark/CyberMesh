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
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/metrics"
)

// MessageHandler is invoked for each Kafka message.
type MessageHandler interface {
	HandleMessage(ctx context.Context, msg *sarama.ConsumerMessage) error
}

// Consumer wraps a Sarama consumer group with graceful shutdown support.
type Consumer struct {
	group     sarama.ConsumerGroup
	handler   MessageHandler
	topic     string
	workers   int
	queueSize int
	mu        sync.Mutex
	closed    bool
	metrics   *metrics.Recorder
	errorOnce sync.Once
	logger    *zap.Logger
}

// Config represents the options for constructing a consumer.
type Config struct {
	Brokers []string
	GroupID string
	Topic   string
	// ProtocolVersion is a sarama.ParseKafkaVersion string (e.g. "2.1.0", "3.6.0").
	// Default: "3.6.0".
	ProtocolVersion string
	TLS             bool
	TLSCAPath       string
	TLSCertPath     string
	TLSKeyPath      string
	SASLEnabled     bool
	SASLMechanism   string
	SASLUsername    string
	SASLPassword    string
	HandlerWorkers  int
	HandlerQueue    int
	Metrics         *metrics.Recorder
	Logger          *zap.Logger
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
		group:     group,
		handler:   handler,
		topic:     cfg.Topic,
		workers:   workers,
		queueSize: queueSize,
		metrics:   cfg.Metrics,
		logger:    cfg.Logger,
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
		handler:   c.handler,
		metrics:   c.metrics,
		logger:    c.logger,
		workers:   c.workers,
		queueSize: c.queueSize,
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
	handler   MessageHandler
	metrics   *metrics.Recorder
	logger    *zap.Logger
	workers   int
	queueSize int
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
}

type consumerResult struct {
	msg      *sarama.ConsumerMessage
	start    time.Time
	done     time.Time
	err      error
	canceled bool
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

	var workerWG sync.WaitGroup
	for i := 0; i < workers; i++ {
		workerWG.Add(1)
		go func(ch <-chan consumerTask) {
			defer workerWG.Done()
			for task := range ch {
				err := g.handler.HandleMessage(session.Context(), task.msg)
				done := time.Now()
				canceled := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
				res := consumerResult{
					msg:      task.msg,
					start:    task.start,
					done:     done,
					err:      err,
					canceled: canceled,
				}
				select {
				case results <- res:
				case <-session.Context().Done():
					return
				}
			}
		}(workerQueues[i])
	}

	defer func() {
		for _, ch := range workerQueues {
			close(ch)
		}
		workerWG.Wait()
		close(results)
	}()

	var (
		inflight      int
		nextOffset    int64
		nextOffsetSet bool
		pending       = make(map[int64]consumerResult)
		readOpen      = true
		msgPending    *sarama.ConsumerMessage
	)

	for {
		if session.Context().Err() != nil {
			return nil
		}
		if !readOpen && inflight == 0 && msgPending == nil {
			return nil
		}

		if msgPending == nil && readOpen {
			select {
			case <-session.Context().Done():
				return nil
			case res := <-results:
				if res.msg == nil {
					continue
				}
				inflight--
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
			task := consumerTask{msg: msgPending, start: time.Now()}
			stripe := workerStripeForMessage(msgPending, workers)
			dispatchStart := time.Now()
			select {
			case workerQueues[stripe] <- task:
				if g.metrics != nil {
					g.metrics.ObserveKafkaDispatchWait(time.Since(dispatchStart))
				}
				inflight++
				msgPending = nil
			default:
				if g.metrics != nil {
					g.metrics.ObserveKafkaQueueSaturated()
				}
				select {
				case <-session.Context().Done():
					return nil
				case res := <-results:
					if res.msg == nil {
						continue
					}
					inflight--
					pending[res.msg.Offset] = res
					drainMarkableResults(g, session, pending, &nextOffset, &nextOffsetSet)
				case workerQueues[stripe] <- task:
					if g.metrics != nil {
						g.metrics.ObserveKafkaDispatchWait(time.Since(dispatchStart))
					}
					inflight++
					msgPending = nil
				}
			}
			continue
		}

		select {
		case <-session.Context().Done():
			return nil
		case res := <-results:
			if res.msg == nil {
				continue
			}
			inflight--
			pending[res.msg.Offset] = res
			drainMarkableResults(g, session, pending, &nextOffset, &nextOffsetSet)
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
