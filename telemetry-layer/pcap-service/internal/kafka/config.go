package kafka

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/IBM/sarama"
)

type Config struct {
	Brokers       []string
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

func BuildSaramaConfig(cfg Config) (*sarama.Config, error) {
	config := sarama.NewConfig()
	config.Version = sarama.V3_0_0_0
	config.ClientID = cfg.ClientID
	config.Producer.Return.Successes = true
	config.Producer.Return.Errors = true
	// Gate runs use short-lived consumer groups and isolated topics; consume from oldest
	// to avoid races where requests are produced before the group fully joins.
	config.Consumer.Offsets.Initial = sarama.OffsetOldest
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
