package codec

import (
	"encoding/json"
	"errors"
	"strings"

	telemetrypb "cybermesh/telemetry-layer/proto/gen/go"
	"cybermesh/telemetry-layer/stream-processor/internal/model"
	"google.golang.org/protobuf/proto"
)

func DecodeDeepFlow(payload []byte, encoding string) (model.FlowEvent, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "protobuf", "proto", "pb":
		var msg telemetrypb.DeepFlowV1
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return model.FlowEvent{}, err
		}
		return deepflowToFlowEvent(&msg), nil
	default:
		return decodeDeepFlowJSON(payload)
	}
}

func deepflowToFlowEvent(msg *telemetrypb.DeepFlowV1) model.FlowEvent {
	event := model.FlowEvent{
		Timestamp:    msg.Ts,
		TenantID:     msg.TenantId,
		SrcIP:        msg.SrcIp,
		DstIP:        msg.DstIp,
		SrcPort:      int(msg.SrcPort),
		DstPort:      int(msg.DstPort),
		Proto:        int(msg.Proto),
		Bytes:        0,
		Packets:      0,
		Direction:    "egress",
		MetricsKnown: false,
		SourceType:   deepflowSourceType(msg.SourceType),
		SourceID:     msg.SensorId,
		TimingKnown:  false,
		FlagsKnown:   false,
	}
	return event
}

func decodeDeepFlowJSON(payload []byte) (model.FlowEvent, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return model.FlowEvent{}, err
	}
	event := model.FlowEvent{
		Timestamp:    readInt64(raw, "ts", "timestamp", "Timestamp"),
		TenantID:     readString(raw, "tenant_id", "tenantId", "TenantID"),
		SrcIP:        readString(raw, "src_ip", "srcIp", "SrcIP"),
		DstIP:        readString(raw, "dst_ip", "dstIp", "DstIP"),
		SrcPort:      int(readInt64(raw, "src_port", "srcPort", "SrcPort")),
		DstPort:      int(readInt64(raw, "dst_port", "dstPort", "DstPort")),
		Proto:        int(readInt64(raw, "proto", "Proto")),
		Bytes:        0,
		Packets:      0,
		Direction:    "egress",
		MetricsKnown: false,
		SourceType:   readString(raw, "source_type", "sourceType", "SourceType"),
		SourceID:     readString(raw, "sensor_id", "sensorId", "SensorID"),
		TimingKnown:  false,
		FlagsKnown:   false,
	}
	if event.TenantID == "" {
		return model.FlowEvent{}, errors.New("tenant_id required")
	}
	return event, nil
}

func readString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if val, ok := raw[key]; ok {
			switch v := val.(type) {
			case string:
				return v
			}
		}
	}
	return ""
}

func readInt64(raw map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if val, ok := raw[key]; ok {
			switch v := val.(type) {
			case float64:
				return int64(v)
			case int:
				return int64(v)
			case int64:
				return v
			case json.Number:
				if parsed, err := v.Int64(); err == nil {
					return parsed
				}
			}
		}
	}
	return 0
}

func deepflowSourceType(value telemetrypb.SourceType) string {
	switch value {
	case telemetrypb.SourceType_SOURCE_TYPE_K8S_CILIUM:
		return "k8s_cilium"
	case telemetrypb.SourceType_SOURCE_TYPE_BARE_METAL_EBPF:
		return "bare_metal"
	case telemetrypb.SourceType_SOURCE_TYPE_GATEWAY_SENSOR:
		return "gateway_sensor"
	case telemetrypb.SourceType_SOURCE_TYPE_GCP_VPC:
		return "gcp_vpc"
	case telemetrypb.SourceType_SOURCE_TYPE_AWS_VPC:
		return "aws_vpc"
	case telemetrypb.SourceType_SOURCE_TYPE_AZURE_NSG:
		return "azure_nsg"
	case telemetrypb.SourceType_SOURCE_TYPE_PCAP:
		return "pcap"
	case telemetrypb.SourceType_SOURCE_TYPE_ZEEK:
		return "zeek"
	case telemetrypb.SourceType_SOURCE_TYPE_SURICATA:
		return "suricata"
	default:
		return "unknown"
	}
}
