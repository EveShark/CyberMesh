package controller

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
	xdgscram "github.com/xdg-go/scram"
	"go.uber.org/zap"
)

const (
	defaultFastRejectQueueSize = 256
	defaultFastRejectWorkers   = 1
)

type FastRejectSink interface {
	PublishReject(ctx context.Context, msg *sarama.ConsumerMessage, reason error) error
	Close() error
}

type noopFastRejectSink struct{}

func (noopFastRejectSink) PublishReject(context.Context, *sarama.ConsumerMessage, error) error {
	return nil
}
func (noopFastRejectSink) Close() error { return nil }

type fastRejectRecord struct {
	SchemaVersion   string `json:"schema_version"`
	ConsumerTopic   string `json:"consumer_topic"`
	ConsumerGroup   string `json:"consumer_group,omitempty"`
	Partition       int32  `json:"partition"`
	Offset          int64  `json:"offset"`
	MessageKeyB64   string `json:"message_key_b64,omitempty"`
	MessageValueB64 string `json:"message_value_b64"`
	MessageTimeUnix int64  `json:"message_time_unix,omitempty"`
	RejectedAtUnix  int64  `json:"rejected_at_unix"`
	Reason          string `json:"reason"`
}

type syncProducer interface {
	SendMessage(*sarama.ProducerMessage) (partition int32, offset int64, err error)
	Close() error
}

type queuedReject struct {
	record []byte
}

type kafkaFastRejectSink struct {
	producer syncProducer
	topic    string
	groupID  string
	logger   *zap.Logger

	queue    chan queuedReject
	closing  chan struct{}
	wg       sync.WaitGroup
	closed   atomic.Bool
	rejected atomic.Int64
}

type xdgSCRAMClient struct {
	hashGen func() xdgscram.HashGeneratorFcn
	client  *xdgscram.Client
	conv    *xdgscram.ClientConversation
}

func (x *xdgSCRAMClient) Begin(userName, password, authzID string) error {
	client, err := x.hashGen().NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	x.client = client
	x.conv = client.NewConversation()
	return nil
}

func (x *xdgSCRAMClient) Step(challenge string) (string, error) {
	return x.conv.Step(challenge)
}

func (x *xdgSCRAMClient) Done() bool {
	return x.conv != nil && x.conv.Done()
}

func NewKafkaFastRejectSink(cfg sarama.Config, brokers []string, topic, groupID string, logger *zap.Logger) (FastRejectSink, error) {
	if topic == "" {
		return noopFastRejectSink{}, nil
	}
	cfg.Producer.Return.Successes = true
	cfg.Producer.Return.Errors = true
	cfg.Producer.RequiredAcks = sarama.WaitForLocal
	cfg.Producer.Retry.Max = 0
	cfg.Producer.Timeout = 3 * time.Second
	cfg.Net.DialTimeout = 3 * time.Second
	cfg.Net.ReadTimeout = 3 * time.Second
	cfg.Net.WriteTimeout = 3 * time.Second

	producer, err := sarama.NewSyncProducer(brokers, &cfg)
	if err != nil {
		return nil, fmt.Errorf("create fast reject producer: %w", err)
	}
	sink := &kafkaFastRejectSink{
		producer: producer,
		topic:    topic,
		groupID:  groupID,
		logger:   logger,
		queue:    make(chan queuedReject, defaultFastRejectQueueSize),
		closing:  make(chan struct{}),
	}
	for i := 0; i < defaultFastRejectWorkers; i++ {
		sink.wg.Add(1)
		go sink.worker()
	}
	return sink, nil
}

func (s *kafkaFastRejectSink) PublishReject(ctx context.Context, msg *sarama.ConsumerMessage, reason error) error {
	if s == nil || s.producer == nil || msg == nil || reason == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.closed.Load() {
		return fmt.Errorf("fast reject sink closed")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	payload, err := s.buildRecord(msg, reason)
	if err != nil {
		return err
	}
	item := queuedReject{record: payload}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closing:
		return fmt.Errorf("fast reject sink closing")
	case s.queue <- item:
		return nil
	default:
		s.rejected.Add(1)
		return fmt.Errorf("fast reject queue full")
	}
}

func (s *kafkaFastRejectSink) Close() error {
	if s == nil || s.producer == nil {
		return nil
	}
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(s.closing)
	s.wg.Wait()
	return s.producer.Close()
}

func (s *kafkaFastRejectSink) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.closing:
			for {
				select {
				case item := <-s.queue:
					if err := s.publish(item.record); err != nil && s.logger != nil {
						s.logger.Warn("fast mitigation reject publish failed", zap.Error(err), zap.String("topic", s.topic))
					}
				default:
					return
				}
			}
		case item := <-s.queue:
			if err := s.publish(item.record); err != nil && s.logger != nil {
				s.logger.Warn("fast mitigation reject publish failed", zap.Error(err), zap.String("topic", s.topic))
			}
		}
	}
}

func (s *kafkaFastRejectSink) publish(payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	_, _, err := s.producer.SendMessage(&sarama.ProducerMessage{
		Topic: s.topic,
		Key:   sarama.StringEncoder(s.groupID),
		Value: sarama.ByteEncoder(payload),
	})
	return err
}

func (s *kafkaFastRejectSink) buildRecord(msg *sarama.ConsumerMessage, reason error) ([]byte, error) {
	record := fastRejectRecord{
		SchemaVersion:   "control.fast_mitigation.reject.v1",
		ConsumerTopic:   msg.Topic,
		ConsumerGroup:   s.groupID,
		Partition:       msg.Partition,
		Offset:          msg.Offset,
		MessageValueB64: base64.StdEncoding.EncodeToString(msg.Value),
		RejectedAtUnix:  time.Now().UTC().Unix(),
		Reason:          reason.Error(),
	}
	if len(msg.Key) > 0 {
		record.MessageKeyB64 = base64.StdEncoding.EncodeToString(msg.Key)
	}
	if !msg.Timestamp.IsZero() {
		record.MessageTimeUnix = msg.Timestamp.UTC().Unix()
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("marshal fast reject record: %w", err)
	}
	return payload, nil
}

func BuildFastRejectSaramaConfig(version string, tlsEnabled bool, tlsCAPath, tlsCertPath, tlsKeyPath string, saslEnabled bool, saslMechanism, saslUsername, saslPassword string) (sarama.Config, error) {
	cfg := *sarama.NewConfig()
	if version == "" {
		version = "3.6.0"
	}
	ver, err := sarama.ParseKafkaVersion(version)
	if err != nil {
		return cfg, fmt.Errorf("invalid kafka version %q: %w", version, err)
	}
	cfg.Version = ver
	if tlsEnabled {
		tlsCfg, err := buildTLSConfig(tlsCAPath, tlsCertPath, tlsKeyPath)
		if err != nil {
			return cfg, err
		}
		cfg.Net.TLS.Enable = true
		cfg.Net.TLS.Config = tlsCfg
	}
	if saslEnabled {
		cfg.Net.SASL.Enable = true
		cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		mech := strings.ToUpper(strings.TrimSpace(saslMechanism))
		switch mech {
		case "", "PLAIN", "PLAINTEXT":
			cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		case "SCRAM-SHA-256":
			cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
			cfg.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &xdgSCRAMClient{hashGen: func() xdgscram.HashGeneratorFcn { return xdgscram.SHA256 }}
			}
		case "SCRAM-SHA-512":
			cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
			cfg.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &xdgSCRAMClient{hashGen: func() xdgscram.HashGeneratorFcn { return xdgscram.SHA512 }}
			}
		default:
			return cfg, fmt.Errorf("unsupported kafka sasl mechanism %q", saslMechanism)
		}
		cfg.Net.SASL.User = saslUsername
		cfg.Net.SASL.Password = saslPassword
	}
	return cfg, nil
}

func buildTLSConfig(caPath, certPath, keyPath string) (*tls.Config, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caPath != "" {
		ca, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read tls ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("parse tls ca: no certificates found")
		}
		tlsConfig.RootCAs = pool
	}
	if certPath != "" || keyPath != "" {
		if certPath == "" || keyPath == "" {
			return nil, fmt.Errorf("both tls cert and key are required")
		}
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("load tls cert/key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return tlsConfig, nil
}
