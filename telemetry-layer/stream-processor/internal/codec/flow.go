package codec

import (
	"encoding/json"
	"strings"

	telemetrypb "cybermesh/telemetry-layer/proto/gen/go"
	"cybermesh/telemetry-layer/stream-processor/internal/model"
	"google.golang.org/protobuf/proto"
)

func EncodeFlowAggregate(agg model.FlowAggregate, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "protobuf", "proto", "pb":
		msg := toProtoFlow(agg)
		return proto.Marshal(msg)
	default:
		return json.Marshal(agg)
	}
}

func DecodeFlowAggregate(payload []byte, encoding string) (model.FlowAggregate, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "protobuf", "proto", "pb":
		var msg telemetrypb.FlowV1
		if err := proto.Unmarshal(payload, &msg); err != nil {
			return model.FlowAggregate{}, err
		}
		return fromProtoFlow(&msg), nil
	default:
		var out model.FlowAggregate
		err := json.Unmarshal(payload, &out)
		return out, err
	}
}

func toProtoFlow(agg model.FlowAggregate) *telemetrypb.FlowV1 {
	msg := &telemetrypb.FlowV1{
		Schema:              agg.Schema,
		Ts:                  agg.Timestamp,
		TenantId:            agg.TenantID,
		FlowId:              agg.FlowID,
		SrcIp:               agg.SrcIP,
		DstIp:               agg.DstIP,
		SrcPort:             uint32(agg.SrcPort),
		DstPort:             uint32(agg.DstPort),
		Proto:               uint32(agg.Proto),
		BytesFwd:            agg.BytesFwd,
		BytesBwd:            agg.BytesBwd,
		PktsFwd:             agg.PktsFwd,
		PktsBwd:             agg.PktsBwd,
		DurationMs:          agg.DurationMS,
		Verdict:             agg.Verdict,
		MetricsKnown:        agg.MetricsKnown,
		SourceType:          mapSourceType(agg.SourceType),
		SourceId:            agg.SourceID,
		TimingKnown:         agg.TimingKnown,
		TimingDerived:       agg.TimingDerived,
		DerivationPolicy:    agg.DerivationPolicy,
		SourceEventTsMs:     agg.SourceEventTsMs,
		TelemetryIngestTsMs: agg.TelemetryIngestTsMs,
		FlowIatMean:         agg.FlowIatMean,
		FlowIatStd:          agg.FlowIatStd,
		FlowIatMax:          agg.FlowIatMax,
		FlowIatMin:          agg.FlowIatMin,
		FwdIatTot:           agg.FwdIatTot,
		FwdIatMean:          agg.FwdIatMean,
		FwdIatStd:           agg.FwdIatStd,
		FwdIatMax:           agg.FwdIatMax,
		FwdIatMin:           agg.FwdIatMin,
		BwdIatTot:           agg.BwdIatTot,
		BwdIatMean:          agg.BwdIatMean,
		BwdIatStd:           agg.BwdIatStd,
		BwdIatMax:           agg.BwdIatMax,
		BwdIatMin:           agg.BwdIatMin,
		ActiveMean:          agg.ActiveMean,
		ActiveStd:           agg.ActiveStd,
		ActiveMax:           agg.ActiveMax,
		ActiveMin:           agg.ActiveMin,
		IdleMean:            agg.IdleMean,
		IdleStd:             agg.IdleStd,
		IdleMax:             agg.IdleMax,
		IdleMin:             agg.IdleMin,
		FlagsKnown:          agg.FlagsKnown,
		FinFlagCnt:          agg.FinFlagCnt,
		SynFlagCnt:          agg.SynFlagCnt,
		RstFlagCnt:          agg.RstFlagCnt,
		PshFlagCnt:          agg.PshFlagCnt,
		AckFlagCnt:          agg.AckFlagCnt,
		UrgFlagCnt:          agg.UrgFlagCnt,
		CweFlagCount:        agg.CweFlagCount,
		EceFlagCnt:          agg.EceFlagCnt,
	}
	if agg.Identity.Namespace != "" || agg.Identity.Pod != "" || agg.Identity.Node != "" {
		msg.Identity = &telemetrypb.Identity{
			Namespace: agg.Identity.Namespace,
			Pod:       agg.Identity.Pod,
			Node:      agg.Identity.Node,
		}
	}
	return msg
}

func fromProtoFlow(msg *telemetrypb.FlowV1) model.FlowAggregate {
	identity := model.Identity{}
	if msg.Identity != nil {
		identity = model.Identity{
			Namespace: msg.Identity.Namespace,
			Pod:       msg.Identity.Pod,
			Node:      msg.Identity.Node,
		}
	}
	return model.FlowAggregate{
		Schema:              msg.Schema,
		Timestamp:           msg.Ts,
		TenantID:            msg.TenantId,
		FlowID:              msg.FlowId,
		SrcIP:               msg.SrcIp,
		DstIP:               msg.DstIp,
		SrcPort:             int(msg.SrcPort),
		DstPort:             int(msg.DstPort),
		Proto:               int(msg.Proto),
		BytesFwd:            msg.BytesFwd,
		BytesBwd:            msg.BytesBwd,
		PktsFwd:             msg.PktsFwd,
		PktsBwd:             msg.PktsBwd,
		DurationMS:          msg.DurationMs,
		Identity:            identity,
		Verdict:             msg.Verdict,
		MetricsKnown:        msg.MetricsKnown,
		SourceType:          reverseSourceType(msg.SourceType),
		SourceID:            msg.SourceId,
		TimingKnown:         msg.TimingKnown,
		TimingDerived:       msg.TimingDerived,
		DerivationPolicy:    msg.DerivationPolicy,
		SourceEventTsMs:     msg.SourceEventTsMs,
		TelemetryIngestTsMs: msg.TelemetryIngestTsMs,
		FlowIatMean:         msg.FlowIatMean,
		FlowIatStd:          msg.FlowIatStd,
		FlowIatMax:          msg.FlowIatMax,
		FlowIatMin:          msg.FlowIatMin,
		FwdIatTot:           msg.FwdIatTot,
		FwdIatMean:          msg.FwdIatMean,
		FwdIatStd:           msg.FwdIatStd,
		FwdIatMax:           msg.FwdIatMax,
		FwdIatMin:           msg.FwdIatMin,
		BwdIatTot:           msg.BwdIatTot,
		BwdIatMean:          msg.BwdIatMean,
		BwdIatStd:           msg.BwdIatStd,
		BwdIatMax:           msg.BwdIatMax,
		BwdIatMin:           msg.BwdIatMin,
		ActiveMean:          msg.ActiveMean,
		ActiveStd:           msg.ActiveStd,
		ActiveMax:           msg.ActiveMax,
		ActiveMin:           msg.ActiveMin,
		IdleMean:            msg.IdleMean,
		IdleStd:             msg.IdleStd,
		IdleMax:             msg.IdleMax,
		IdleMin:             msg.IdleMin,
		FlagsKnown:          msg.FlagsKnown,
		FinFlagCnt:          msg.FinFlagCnt,
		SynFlagCnt:          msg.SynFlagCnt,
		RstFlagCnt:          msg.RstFlagCnt,
		PshFlagCnt:          msg.PshFlagCnt,
		AckFlagCnt:          msg.AckFlagCnt,
		UrgFlagCnt:          msg.UrgFlagCnt,
		CweFlagCount:        msg.CweFlagCount,
		EceFlagCnt:          msg.EceFlagCnt,
	}
}

func mapSourceType(value string) telemetrypb.SourceType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "k8s", "k8s_cilium", "cilium", "k8s-cilium":
		return telemetrypb.SourceType_SOURCE_TYPE_K8S_CILIUM
	case "bare_metal", "bare-metal", "ebpf":
		return telemetrypb.SourceType_SOURCE_TYPE_BARE_METAL_EBPF
	case "gateway", "gateway_sensor", "gateway-sensor":
		return telemetrypb.SourceType_SOURCE_TYPE_GATEWAY_SENSOR
	case "gcp", "gcp_vpc", "gcp-vpc":
		return telemetrypb.SourceType_SOURCE_TYPE_GCP_VPC
	case "aws", "aws_vpc", "aws-vpc":
		return telemetrypb.SourceType_SOURCE_TYPE_AWS_VPC
	case "azure", "azure_nsg", "azure-nsg":
		return telemetrypb.SourceType_SOURCE_TYPE_AZURE_NSG
	case "pcap":
		return telemetrypb.SourceType_SOURCE_TYPE_PCAP
	case "zeek":
		return telemetrypb.SourceType_SOURCE_TYPE_ZEEK
	case "suricata":
		return telemetrypb.SourceType_SOURCE_TYPE_SURICATA
	default:
		return telemetrypb.SourceType_SOURCE_TYPE_UNKNOWN
	}
}

func reverseSourceType(value telemetrypb.SourceType) string {
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
