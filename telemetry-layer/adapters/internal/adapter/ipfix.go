package adapter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"cybermesh/telemetry-layer/adapters/internal/codec"
	"cybermesh/telemetry-layer/adapters/internal/dlq"
	"cybermesh/telemetry-layer/adapters/internal/kafka"
	"cybermesh/telemetry-layer/adapters/internal/parser"
	"cybermesh/telemetry-layer/adapters/internal/registry"
	"cybermesh/telemetry-layer/adapters/internal/telemetrymetrics"
	"cybermesh/telemetry-layer/adapters/internal/validate"
	"cybermesh/telemetry-layer/adapters/utils"
	"github.com/IBM/sarama"
	"github.com/calmh/ipfix"
)

type ipfixProducer interface {
	Send(key string, payload []byte, headers ...sarama.RecordHeader) error
	SendDLQ(env dlq.Envelope) error
}

func runIPFIX(ctx context.Context, cfg Config, registryClient *registry.Client, producer *kafka.Producer, logger *utils.Logger) error {
	if cfg.IPFIXListenAddr == "" {
		cfg.IPFIXListenAddr = ":2055"
	}
	if cfg.IPFIXMaxPacketBytes <= 0 {
		cfg.IPFIXMaxPacketBytes = 65507
	}
	if cfg.TenantID == "" {
		return errors.New("TENANT_ID is required for ipfix_udp")
	}
	conn, err := net.ListenPacket("udp", cfg.IPFIXListenAddr)
	if err != nil {
		return err
	}
	defer conn.Close()
	logger.Info("ipfix listener started",
		utils.ZapString("listen_addr", cfg.IPFIXListenAddr),
		utils.ZapString("source_type", cfg.SourceType),
	)

	session := ipfix.NewSession()
	interpreter := ipfix.NewInterpreter(session)
	buf := make([]byte, cfg.IPFIXMaxPacketBytes)
	if cfg.IPFIXDiagSampleEvery <= 0 {
		cfg.IPFIXDiagSampleEvery = 500
	}
	var seenRecords int64
	var durationZeroRecords int64
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return err
		}
		logger.Debug("ipfix packet received",
			utils.ZapInt("bytes", n),
			utils.ZapString("remote", addr.String()),
		)
		msg, err := session.ParseBuffer(buf[:n])
		if err != nil {
			env := dlq.NewEnvelope("dlq.v1", "adapter", "IPFIX_PARSE", err.Error(), buf[:n])
			_ = producer.SendDLQ(env)
			logger.Warn("ipfix parse failed", utils.ZapError(err))
			continue
		}
		logger.Debug("ipfix message parsed",
			utils.ZapInt("data_records", len(msg.DataRecords)),
		)
		for _, record := range msg.DataRecords {
			fields := interpreter.Interpret(record)
			if len(fields) == 0 {
				env := dlq.NewEnvelope("dlq.v1", "adapter", "IPFIX_INTERPRET", "empty fields", buf[:n])
				_ = producer.SendDLQ(env)
				logger.Warn("ipfix interpret empty fields")
				continue
			}
			logger.Debug("ipfix fields interpreted",
				utils.ZapInt("field_count", len(fields)),
			)
			ok, rec := processIPFIXFields(ctx, cfg, registryClient, producer, logger, fields, buf[:n], addr.String())
			if !ok {
				continue
			}
			seenRecords++
			if rec.DurationMS <= 0 {
				durationZeroRecords++
			}
			if seenRecords%cfg.IPFIXDiagSampleEvery == 0 {
				logger.Info(
					"ipfix telemetry sample",
					utils.ZapInt64("records_seen", seenRecords),
					utils.ZapInt64("records_duration_zero", durationZeroRecords),
					utils.ZapInt64("sample_duration_ms", rec.DurationMS),
					utils.ZapBool("sample_timing_known", rec.TimingKnown),
					utils.ZapInt64("sample_pkts_fwd", rec.PktsFwd),
					utils.ZapInt64("sample_bytes_fwd", rec.BytesFwd),
					utils.ZapString("sample_src_ip", rec.SrcIP),
					utils.ZapString("sample_dst_ip", rec.DstIP),
					utils.ZapInt("sample_proto", rec.Proto),
				)
			}
		}
	}
}

func processIPFIXFields(
	ctx context.Context,
	cfg Config,
	registryClient *registry.Client,
	producer ipfixProducer,
	logger *utils.Logger,
	fields []ipfix.InterpretedField,
	rawPacket []byte,
	sourceID string,
) (bool, Record) {
	recordStart := time.Now()
	parseStart := time.Now()
	rec, err := parser.RecordFromIPFIX(fields)
	isDurationInvalid := false
	if err == nil {
		err = validateIPFIXTelemetryRecord(rec)
		isDurationInvalid = err != nil
	}
	telemetrymetrics.Global().RecordOperation("record_parse", statusFromError(err), time.Since(parseStart))
	if err != nil {
		code := "IPFIX_MAP"
		if isDurationInvalid {
			code = "IPFIX_DURATION_INVALID"
		}
		reason := err.Error()
		if code == "IPFIX_DURATION_INVALID" {
			reason = fmt.Sprintf(
				"ipfix telemetry gate failed: %v (duration_ms=%d timing_known=%t src=%s dst=%s proto=%d)",
				err, rec.DurationMS, rec.TimingKnown, rec.SrcIP, rec.DstIP, rec.Proto,
			)
		}
		_ = producer.SendDLQ(dlq.NewEnvelope("dlq.v1", "adapter", code, reason, rawPacket))
		if logger != nil {
			logger.Warn("ipfix record rejected", utils.ZapError(err))
		}
		telemetrymetrics.Global().RecordOperation("record_total", "error", time.Since(recordStart))
		return false, Record{}
	}

	rec.SourceType = "ipfix"
	rec.SourceID = sourceID
	if rec.TenantID == "" {
		rec.TenantID = cfg.TenantID
	}
	if cfg.SourceType != "" {
		rec.SourceType = cfg.SourceType
	}
	if cfg.SourceID != "" {
		rec.SourceID = cfg.SourceID
	}

	mapStart := time.Now()
	flow, err := ToFlow(rec, cfg.WindowSeconds)
	telemetrymetrics.Global().RecordOperation("flow_map", statusFromError(err), time.Since(mapStart))
	if err != nil {
		_ = producer.SendDLQ(dlq.NewEnvelope("dlq.v1", "adapter", "FLOW_MAP", err.Error(), []byte(sourceID)))
		telemetrymetrics.Global().RecordOperation("record_total", "error", time.Since(recordStart))
		return false, Record{}
	}

	validateStart := time.Now()
	err = validate.FlowAggregate(flow)
	telemetrymetrics.Global().RecordOperation("flow_validate", statusFromError(err), time.Since(validateStart))
	if err != nil {
		_ = producer.SendDLQ(dlq.NewEnvelope("dlq.v1", "adapter", "FLOW_INVALID", err.Error(), []byte(flow.FlowID)))
		telemetrymetrics.Global().RecordOperation("record_total", "error", time.Since(recordStart))
		return false, Record{}
	}

	encodeStart := time.Now()
	payload, err := codec.EncodeFlowAggregate(flow, cfg.FlowEncoding)
	telemetrymetrics.Global().RecordOperation("flow_encode", statusFromError(err), time.Since(encodeStart))
	if err != nil {
		_ = producer.SendDLQ(dlq.NewEnvelope("dlq.v1", "adapter", "FLOW_ENCODE", err.Error(), []byte(flow.FlowID)))
		telemetrymetrics.Global().RecordOperation("record_total", "error", time.Since(recordStart))
		return false, Record{}
	}

	var headers []sarama.RecordHeader
	if registryClient != nil && registryClient.Enabled() && cfg.FlowEncoding != "json" {
		schemaID, schemaErr := registryClient.SubjectLatestID(ctx, cfg.RegistrySubject)
		if schemaErr != nil {
			_ = producer.SendDLQ(dlq.NewEnvelope("dlq.v1", "adapter", "SCHEMA_REGISTRY", schemaErr.Error(), []byte(flow.FlowID)))
			if !cfg.RegistryAllowFallback {
				telemetrymetrics.Global().RecordOperation("record_total", "error", time.Since(recordStart))
				return false, Record{}
			}
		} else {
			headers = append(headers, sarama.RecordHeader{Key: []byte("schema_id"), Value: []byte(fmt.Sprintf("%d", schemaID))})
		}
	}
	if err := producer.Send(flow.FlowID, payload, headers...); err != nil {
		_ = producer.SendDLQ(dlq.NewEnvelope("dlq.v1", "adapter", "KAFKA_SEND", err.Error(), []byte(flow.FlowID)))
		if logger != nil {
			logger.Warn("kafka send failed", utils.ZapError(err))
		}
		telemetrymetrics.Global().RecordOperation("record_total", "error", time.Since(recordStart))
		return false, Record{}
	}

	telemetrymetrics.Global().RecordOperation("record_total", "ok", time.Since(recordStart))
	return true, rec
}

func validateIPFIXTelemetryRecord(rec Record) error {
	if !rec.TimingKnown {
		return errors.New("timing_known=false")
	}
	if rec.DurationMS <= 0 {
		return errors.New("duration_ms<=0")
	}
	return nil
}
