package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
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

	return &Consumer{
		group:   group,
		handler: handler,
		topic:   cfg.Topic,
		metrics: cfg.Metrics,
		logger:  cfg.Logger,
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
	consumer := &groupHandler{handler: c.handler, metrics: c.metrics, logger: c.logger}

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
	handler MessageHandler
	metrics *metrics.Recorder
	logger  *zap.Logger
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

	for {
		select {
		case <-session.Context().Done():
			return nil
		case message, ok := <-claim.Messages():
			if !ok {
				return nil
			}
			if g.metrics != nil {
				lag := claim.HighWaterMarkOffset() - message.Offset - 1
				if lag < 0 {
					lag = 0
				}
				g.metrics.ObserveKafkaLag(message.Partition, lag)
				if !message.Timestamp.IsZero() {
					publishToConsume := time.Since(message.Timestamp)
					if publishToConsume >= 0 {
						g.metrics.ObservePublishToConsume(publishToConsume.Seconds())
					}
				}
			}
			if err := g.handler.HandleMessage(session.Context(), message); err != nil {
				if g.metrics != nil {
					g.metrics.ObserveKafkaError("handler")
					g.metrics.ObserveKafkaConsumed(message.Partition, "error")
				}
				return err
			}
			if g.metrics != nil {
				g.metrics.ObserveKafkaConsumed(message.Partition, "success")
			}
			markStart := time.Now()
			session.MarkMessage(message, "")
			if g.metrics != nil {
				g.metrics.ObserveOffsetMark(time.Since(markStart))
			}
		}
	}
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
