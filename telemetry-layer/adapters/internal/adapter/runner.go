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
	"cybermesh/telemetry-layer/adapters/internal/telemetrymetrics"
	"cybermesh/telemetry-layer/adapters/internal/validate"
	"cybermesh/telemetry-layer/adapters/utils"
	"github.com/IBM/sarama"
)

func stageMarkersEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("TELEMETRY_STAGE_MARKERS_ENABLED")), "true") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("TELEMETRY_STAGE_MARKERS_ENABLED")), "1") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("TELEMETRY_STAGE_MARKERS_ENABLED")), "yes") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("TELEMETRY_STAGE_MARKERS_ENABLED")), "on")
}

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
		lineStart := time.Now()
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
			parseStart := time.Now()
			rec, err = FromJSONLine([]byte(line))
			telemetrymetrics.Global().RecordOperation("record_parse", statusFromError(err), time.Since(parseStart))
			if err == nil {
				records = []Record{rec}
			}
		case "mapped_json":
			var rec Record
			parseStart := time.Now()
			rec, err = RecordFromMappedJSON([]byte(line), mappingSpec)
			telemetrymetrics.Global().RecordOperation("record_parse", statusFromError(err), time.Since(parseStart))
			if err == nil {
				records = []Record{rec}
			}
		case "gcp_vpc_raw":
			parseStart := time.Now()
			records, err = parseGCP(line)
			telemetrymetrics.Global().RecordOperation("record_parse", statusFromError(err), time.Since(parseStart))
		case "aws_vpc_raw":
			parseStart := time.Now()
			records, err = parseAWS(line)
			telemetrymetrics.Global().RecordOperation("record_parse", statusFromError(err), time.Since(parseStart))
		case "azure_nsg_raw":
			parseStart := time.Now()
			records, err = parseAzure(line)
			telemetrymetrics.Global().RecordOperation("record_parse", statusFromError(err), time.Since(parseStart))
		default:
			err = fmt.Errorf("unsupported RECORD_FORMAT %q", format)
			telemetrymetrics.Global().RecordOperation("record_parse", "error", 0)
		}
		if err != nil {
			env := dlq.NewEnvelope("dlq.v1", "adapter", "RECORD_PARSE", err.Error(), []byte(line))
			_ = producer.SendDLQ(env)
			telemetrymetrics.Global().RecordOperation("record_total", "error", time.Since(lineStart))
			continue
		}
		for _, rec := range records {
			recordStart := time.Now()
			if rec.SourceType == "" {
				rec.SourceType = cfg.SourceType
			}
			if rec.SourceID == "" {
				rec.SourceID = cfg.SourceID
			}
			mapStart := time.Now()
			flow, err := ToFlow(rec, cfg.WindowSeconds)
			telemetrymetrics.Global().RecordOperation("flow_map", statusFromError(err), time.Since(mapStart))
			if err != nil {
				env := dlq.NewEnvelope("dlq.v1", "adapter", "FLOW_MAP", err.Error(), []byte(line))
				_ = producer.SendDLQ(env)
				telemetrymetrics.Global().RecordOperation("record_total", "error", time.Since(recordStart))
				continue
			}
			validateStart := time.Now()
			if err := validate.FlowAggregate(flow); err != nil {
				telemetrymetrics.Global().RecordOperation("flow_validate", "error", time.Since(validateStart))
				env := dlq.NewEnvelope("dlq.v1", "adapter", "FLOW_INVALID", err.Error(), []byte(flow.FlowID))
				_ = producer.SendDLQ(env)
				telemetrymetrics.Global().RecordOperation("record_total", "error", time.Since(recordStart))
				continue
			}
			telemetrymetrics.Global().RecordOperation("flow_validate", "ok", time.Since(validateStart))
			encodeStart := time.Now()
			payload, err := codec.EncodeFlowAggregate(flow, cfg.FlowEncoding)
			telemetrymetrics.Global().RecordOperation("flow_encode", statusFromError(err), time.Since(encodeStart))
			if err != nil {
				env := dlq.NewEnvelope("dlq.v1", "adapter", "FLOW_ENCODE", err.Error(), []byte(flow.FlowID))
				_ = producer.SendDLQ(env)
				telemetrymetrics.Global().RecordOperation("record_total", "error", time.Since(recordStart))
				continue
			}
			var headers []sarama.RecordHeader
			if registryClient.Enabled() && cfg.FlowEncoding != "json" {
				schemaID, err := registryClient.SubjectLatestID(ctx, cfg.RegistrySubject)
				if err != nil {
					env := dlq.NewEnvelope("dlq.v1", "adapter", "SCHEMA_REGISTRY", err.Error(), []byte(flow.FlowID))
					_ = producer.SendDLQ(env)
					if !cfg.RegistryAllowFallback {
						telemetrymetrics.Global().RecordOperation("record_total", "error", time.Since(recordStart))
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
				telemetrymetrics.Global().RecordOperation("record_total", "error", time.Since(recordStart))
				continue
			}
			if stageMarkersEnabled() {
				logger.Info("runtime stage marker",
					utils.ZapString("stage", "t_telemetry_publish_ack"),
					utils.ZapString("trace_id", flow.FlowID),
					utils.ZapString("event_id", flow.FlowID),
					utils.ZapString("tenant_id", flow.TenantID),
					utils.ZapInt64("t_ms", time.Now().UnixMilli()),
					utils.ZapString("source", "telemetry.adapter"),
				)
			}
			telemetrymetrics.Global().RecordOperation("record_total", "ok", time.Since(recordStart))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func statusFromError(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
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
