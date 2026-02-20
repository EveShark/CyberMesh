package deepflow

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"cybermesh/telemetry-layer/adapters/internal/codec"
	"cybermesh/telemetry-layer/adapters/internal/dlq"
	"cybermesh/telemetry-layer/adapters/internal/kafka"
	"cybermesh/telemetry-layer/adapters/internal/model"
	"cybermesh/telemetry-layer/adapters/internal/parser"
	"cybermesh/telemetry-layer/adapters/internal/registry"
	"cybermesh/telemetry-layer/adapters/internal/validate"
	"cybermesh/telemetry-layer/adapters/utils"
	"github.com/IBM/sarama"
)

type Config struct {
	InputPath             string
	RecordFormat          string
	Encoding              string
	TenantID              string
	SensorID              string
	SourceType            string
	RegistryURL           string
	RegistryEnabled       bool
	RegistrySubject       string
	RegistryAllowFallback bool
	RegistryUsername      string
	RegistryPassword      string
	Kafka                 kafka.Config
}

func Run(ctx context.Context, cfg Config, logger *utils.Logger) error {
	if logger == nil {
		return errors.New("logger required")
	}
	if cfg.TenantID == "" {
		return errors.New("TENANT_ID is required")
	}
	if cfg.SensorID == "" {
		return errors.New("SENSOR_ID is required")
	}

	producer, err := kafka.NewProducer(cfg.Kafka, logger)
	if err != nil {
		return err
	}
	defer producer.Close()

	registryClient := registry.New(registry.Config{
		Enabled:  cfg.RegistryEnabled,
		URL:      cfg.RegistryURL,
		Username: cfg.RegistryUsername,
		Password: cfg.RegistryPassword,
		Timeout:  5 * time.Second,
		CacheTTL: 5 * time.Minute,
	})

	reader, err := inputReader(cfg.InputPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	format := strings.TrimSpace(cfg.RecordFormat)
	if format == "" {
		format = "suricata_eve"
	}

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event model.DeepFlowEvent
		switch format {
		case "zeek_json":
			event, err = parser.ParseZeekJSON([]byte(line), cfg.TenantID, cfg.SensorID, cfg.SourceType)
		case "suricata_eve":
			event, err = parser.ParseSuricataEVE([]byte(line), cfg.TenantID, cfg.SensorID, cfg.SourceType)
		default:
			err = fmt.Errorf("unsupported RECORD_FORMAT %q", format)
		}
		if err != nil {
			env := dlq.NewEnvelope("dlq.v1", "deepflow-adapter", "RECORD_PARSE", err.Error(), []byte(line))
			_ = producer.SendDLQ(env)
			continue
		}

		if err := validate.DeepFlow(event); err != nil {
			env := dlq.NewEnvelope("dlq.v1", "deepflow-adapter", "RECORD_INVALID", err.Error(), []byte(line))
			_ = producer.SendDLQ(env)
			continue
		}

		payload, err := codec.EncodeDeepFlow(event, cfg.Encoding)
		if err != nil {
			env := dlq.NewEnvelope("dlq.v1", "deepflow-adapter", "ENCODE", err.Error(), []byte(line))
			_ = producer.SendDLQ(env)
			continue
		}

		var headers []sarama.RecordHeader
		if registryClient.Enabled() && cfg.Encoding != "json" {
			schemaID, err := registryClient.SubjectLatestID(ctx, cfg.RegistrySubject)
			if err != nil {
				env := dlq.NewEnvelope("dlq.v1", "deepflow-adapter", "SCHEMA_REGISTRY", err.Error(), []byte(line))
				_ = producer.SendDLQ(env)
				if !cfg.RegistryAllowFallback {
					continue
				}
			} else {
				headers = append(headers, sarama.RecordHeader{Key: []byte("schema_id"), Value: []byte(fmt.Sprintf("%d", schemaID))})
			}
		}

		if err := producer.Send(event.FlowID, payload, headers...); err != nil {
			env := dlq.NewEnvelope("dlq.v1", "deepflow-adapter", "KAFKA_SEND", err.Error(), []byte(line))
			_ = producer.SendDLQ(env)
			logger.Warn("kafka send failed", utils.ZapError(err))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

type readCloser interface {
	Read(p []byte) (int, error)
	Close() error
}

func inputReader(path string) (readCloser, error) {
	if path == "" || path == "-" {
		return os.Stdin, nil
	}
	return os.Open(path)
}
