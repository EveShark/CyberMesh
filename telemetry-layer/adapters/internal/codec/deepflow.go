package codec

import (
	"encoding/json"
	"strings"

	"cybermesh/telemetry-layer/adapters/internal/model"
	telemetrypb "cybermesh/telemetry-layer/proto/gen/go"
	"google.golang.org/protobuf/proto"
)

func EncodeDeepFlow(event model.DeepFlowEvent, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "protobuf", "proto", "pb":
		msg := toProtoDeepFlow(event)
		return proto.Marshal(msg)
	default:
		return json.Marshal(event)
	}
}

func toProtoDeepFlow(event model.DeepFlowEvent) *telemetrypb.DeepFlowV1 {
	msg := &telemetrypb.DeepFlowV1{
		Schema:        event.Schema,
		Ts:            event.Timestamp,
		TenantId:      event.TenantID,
		SensorId:      event.SensorID,
		SourceType:    mapSourceType(event.SourceType),
		SrcIp:         event.SrcIP,
		DstIp:         event.DstIP,
		SrcPort:       uint32(event.SrcPort),
		DstPort:       uint32(event.DstPort),
		Proto:         uint32(event.Proto),
		AlertType:     event.AlertType,
		AlertCategory: event.AlertCategory,
		Severity:      event.Severity,
		Signature:     event.Signature,
		SignatureId:   event.SignatureID,
		Metadata:      event.Metadata,
		FlowId:        event.FlowID,
	}
	return msg
}
