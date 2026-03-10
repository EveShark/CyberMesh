package kafka

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"cybermesh/telemetry-layer/adapters/internal/dlq"
	"cybermesh/telemetry-layer/adapters/internal/telemetrymetrics"
	"cybermesh/telemetry-layer/adapters/utils"
	"github.com/IBM/sarama"
)

type Config struct {
	Brokers       []string
	Topic         string
	DLQTopic      string
	ClientID      string
	RequiredAcks  string
	Compression   string
	TLSEnabled    bool
	TLSCAPath     string
	TLSCertPath   string
	TLSKeyPath    string
	SASLEnabled   bool
	SASLMechanism string
	SASLUser      string
	SASLPassword  string
}

type Producer struct {
	producer sarama.SyncProducer
	topic    string
	dlqTopic string
	logger   *utils.Logger
}

func NewProducer(cfg Config, logger *utils.Logger) (*Producer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka brokers required")
	}
	saramaConfig, err := buildSaramaConfig(cfg)
	if err != nil {
		return nil, err
	}

	producer, err := sarama.NewSyncProducer(cfg.Brokers, saramaConfig)
	if err != nil {
		return nil, fmt.Errorf("kafka producer: %w", err)
	}

	return &Producer{
		producer: producer,
		topic:    cfg.Topic,
		dlqTopic: cfg.DLQTopic,
		logger:   logger,
	}, nil
}

func (p *Producer) Send(key string, payload []byte, headers ...sarama.RecordHeader) error {
	return p.sendToTopic(p.topic, key, payload, headers...)
}

func (p *Producer) SendToTopic(topic, key string, payload []byte, headers ...sarama.RecordHeader) error {
	return p.sendToTopic(topic, key, payload, headers...)
}

func (p *Producer) SendDLQ(env dlq.Envelope) error {
	payload, err := jsonMarshal(env)
	if err != nil {
		return err
	}
	return p.sendToTopic(p.dlqTopic, env.PayloadHash, payload)
}

func (p *Producer) Close() error {
	if p == nil || p.producer == nil {
		return nil
	}
	return p.producer.Close()
}

func (p *Producer) sendToTopic(topic, key string, payload []byte, headers ...sarama.RecordHeader) error {
	start := time.Now()
	operation := "kafka_send"
	if topic == p.dlqTopic {
		operation = "dlq_send"
	}
	if topic == "" {
		telemetrymetrics.Global().RecordOperation(operation, "error", time.Since(start))
		return errors.New("kafka topic required")
	}
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(payload),
	}
	if len(headers) > 0 {
		msg.Headers = headers
	}
	_, _, err := p.producer.SendMessage(msg)
	if err != nil && p.logger != nil {
		p.logger.Warn("kafka send error", utils.ZapError(err))
	}
	status := "ok"
	if err != nil {
		status = "error"
	}
	telemetrymetrics.Global().RecordOperation(operation, status, time.Since(start))
	return err
}

func buildSaramaConfig(cfg Config) (*sarama.Config, error) {
	config := sarama.NewConfig()
	config.Version = sarama.V3_0_0_0
	config.ClientID = cfg.ClientID
	config.Producer.Return.Successes = true
	config.Producer.Return.Errors = true
	config.Producer.RequiredAcks = parseAcks(cfg.RequiredAcks)
	config.Producer.Timeout = 10 * time.Second
	config.Net.DialTimeout = 10 * time.Second
	config.Net.ReadTimeout = 10 * time.Second
	config.Net.WriteTimeout = 10 * time.Second
	config.Producer.Compression = parseCompression(cfg.Compression)

	if cfg.TLSEnabled {
		tlsConfig, err := buildTLSConfig(cfg)
		if err != nil {
			return nil, err
		}
		config.Net.TLS.Enable = true
		config.Net.TLS.Config = tlsConfig
	}
	if cfg.SASLEnabled {
		if cfg.SASLUser == "" || cfg.SASLPassword == "" {
			return nil, errors.New("kafka sasl user/password required")
		}
		config.Net.SASL.Enable = true
		config.Net.SASL.User = cfg.SASLUser
		config.Net.SASL.Password = cfg.SASLPassword
		mechanism := parseSASLMechanism(cfg.SASLMechanism)
		config.Net.SASL.Mechanism = mechanism
		if mechanism == sarama.SASLTypeSCRAMSHA256 || mechanism == sarama.SASLTypeSCRAMSHA512 {
			config.Net.SASL.SCRAMClientGeneratorFunc = scramClientGenerator(mechanism)
		}
	}
	return config, nil
}

func BuildSaramaConfig(cfg Config) (*sarama.Config, error) {
	return buildSaramaConfig(cfg)
}

func parseAcks(value string) sarama.RequiredAcks {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "all", "-1":
		return sarama.WaitForAll
	case "leader", "1":
		return sarama.WaitForLocal
	case "none", "0":
		return sarama.NoResponse
	default:
		return sarama.WaitForAll
	}
}

func parseCompression(value string) sarama.CompressionCodec {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "gzip":
		return sarama.CompressionGZIP
	case "snappy":
		return sarama.CompressionSnappy
	case "lz4":
		return sarama.CompressionLZ4
	case "zstd":
		return sarama.CompressionZSTD
	default:
		return sarama.CompressionNone
	}
}

func parseSASLMechanism(value string) sarama.SASLMechanism {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "scram-sha-256":
		return sarama.SASLTypeSCRAMSHA256
	case "scram-sha-512":
		return sarama.SASLTypeSCRAMSHA512
	default:
		return sarama.SASLTypePlaintext
	}
}

func buildTLSConfig(cfg Config) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if cfg.TLSCAPath != "" {
		caPEM, err := os.ReadFile(cfg.TLSCAPath)
		if err != nil {
			return nil, fmt.Errorf("read kafka ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("invalid kafka ca")
		}
		tlsConfig.RootCAs = pool
	}
	if cfg.TLSCertPath != "" || cfg.TLSKeyPath != "" {
		if cfg.TLSCertPath == "" || cfg.TLSKeyPath == "" {
			return nil, errors.New("both kafka cert and key are required")
		}
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertPath, cfg.TLSKeyPath)
		if err != nil {
			return nil, fmt.Errorf("load kafka cert: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return tlsConfig, nil
}

func jsonMarshal(value interface{}) ([]byte, error) {
	return json.Marshal(value)
}
