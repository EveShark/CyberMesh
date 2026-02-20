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
	"cybermesh/telemetry-layer/adapters/internal/validate"
	"cybermesh/telemetry-layer/adapters/utils"
	"github.com/IBM/sarama"
	"github.com/calmh/ipfix"
)

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
			rec, err := parser.RecordFromIPFIX(fields)
			if err != nil {
				env := dlq.NewEnvelope("dlq.v1", "adapter", "IPFIX_MAP", err.Error(), buf[:n])
				_ = producer.SendDLQ(env)
				logger.Warn("ipfix map failed", utils.ZapError(err))
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
			if err := validateIPFIXTelemetryRecord(rec); err != nil {
				reason := fmt.Sprintf(
					"ipfix telemetry gate failed: %v (duration_ms=%d timing_known=%t src=%s dst=%s proto=%d)",
					err, rec.DurationMS, rec.TimingKnown, rec.SrcIP, rec.DstIP, rec.Proto,
				)
				env := dlq.NewEnvelope("dlq.v1", "adapter", "IPFIX_DURATION_INVALID", reason, buf[:n])
				_ = producer.SendDLQ(env)
				logger.Warn("ipfix telemetry gate failed", utils.ZapError(err))
				continue
			}
			rec.SourceType = "ipfix"
			rec.SourceID = addr.String()
			if rec.TenantID == "" {
				rec.TenantID = cfg.TenantID
			}
			if cfg.SourceType != "" {
				rec.SourceType = cfg.SourceType
			}
			if cfg.SourceID != "" {
				rec.SourceID = cfg.SourceID
			}
			flow, err := ToFlow(rec, cfg.WindowSeconds)
			if err != nil {
				env := dlq.NewEnvelope("dlq.v1", "adapter", "FLOW_MAP", err.Error(), []byte(addr.String()))
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
