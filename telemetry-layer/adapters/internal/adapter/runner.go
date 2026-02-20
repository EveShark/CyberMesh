package adapter

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
	"cybermesh/telemetry-layer/adapters/internal/registry"
	"cybermesh/telemetry-layer/adapters/internal/validate"
	"cybermesh/telemetry-layer/adapters/utils"
	"github.com/IBM/sarama"
)

type Config struct {
	WindowSeconds         int64
	FlowEncoding          string
	SourceType            string
	SourceID              string
	TenantID              string
	InputPath             string
	RecordFormat          string
	RecordMappingPath     string
	IPFIXListenAddr       string
	IPFIXMaxPacketBytes   int
	IPFIXDiagSampleEvery  int64
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

	format := strings.TrimSpace(cfg.RecordFormat)
	if format == "" {
		format = "normalized"
	}
	if format == "ipfix_udp" {
		return runIPFIX(ctx, cfg, registryClient, producer, logger)
	}

	reader, err := inputReader(cfg.InputPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	var mappingSpec MappingSpec
	if format == "mapped_json" {
		if cfg.RecordMappingPath == "" {
			return errors.New("RECORD_MAPPING_PATH is required for mapped_json format")
		}
		spec, err := LoadMapping(cfg.RecordMappingPath)
		if err != nil {
			return err
		}
		mappingSpec = spec
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
		var records []Record
		var err error
		switch format {
		case "normalized":
			var rec Record
			rec, err = FromJSONLine([]byte(line))
			if err == nil {
				records = []Record{rec}
			}
		case "mapped_json":
			var rec Record
			rec, err = RecordFromMappedJSON([]byte(line), mappingSpec)
			if err == nil {
				records = []Record{rec}
			}
		case "gcp_vpc_raw":
			records, err = parseGCP(line)
		case "aws_vpc_raw":
			records, err = parseAWS(line)
		case "azure_nsg_raw":
			records, err = parseAzure(line)
		default:
			err = fmt.Errorf("unsupported RECORD_FORMAT %q", format)
		}
		if err != nil {
			env := dlq.NewEnvelope("dlq.v1", "adapter", "RECORD_PARSE", err.Error(), []byte(line))
			_ = producer.SendDLQ(env)
			continue
		}
		for _, rec := range records {
			if rec.SourceType == "" {
				rec.SourceType = cfg.SourceType
			}
			if rec.SourceID == "" {
				rec.SourceID = cfg.SourceID
			}
			flow, err := ToFlow(rec, cfg.WindowSeconds)
			if err != nil {
				env := dlq.NewEnvelope("dlq.v1", "adapter", "FLOW_MAP", err.Error(), []byte(line))
				_ = producer.SendDLQ(env)
				continue
			}
			if err := validate.FlowAggregate(flow); err != nil {
				env := dlq.NewEnvelope("dlq.v1", "adapter", "FLOW_INVALID", err.Error(), []byte(flow.FlowID))
				_ = producer.SendDLQ(env)
				continue
			}
			payload, err := codec.EncodeFlowAggregate(flow, cfg.FlowEncoding)
			if err != nil {
				env := dlq.NewEnvelope("dlq.v1", "adapter", "FLOW_ENCODE", err.Error(), []byte(flow.FlowID))
				_ = producer.SendDLQ(env)
				continue
			}
			var headers []sarama.RecordHeader
			if registryClient.Enabled() && cfg.FlowEncoding != "json" {
				schemaID, err := registryClient.SubjectLatestID(ctx, cfg.RegistrySubject)
				if err != nil {
					env := dlq.NewEnvelope("dlq.v1", "adapter", "SCHEMA_REGISTRY", err.Error(), []byte(flow.FlowID))
					_ = producer.SendDLQ(env)
					if !cfg.RegistryAllowFallback {
						continue
					}
				} else {
					headers = append(headers, sarama.RecordHeader{Key: []byte("schema_id"), Value: []byte(fmt.Sprintf("%d", schemaID))})
				}
			}
			if err := producer.Send(flow.FlowID, payload, headers...); err != nil {
				env := dlq.NewEnvelope("dlq.v1", "adapter", "KAFKA_SEND", err.Error(), []byte(flow.FlowID))
				_ = producer.SendDLQ(env)
				logger.Warn("kafka send failed", utils.ZapError(err))
			}
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
