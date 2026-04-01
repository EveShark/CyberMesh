package codec

import (
	"encoding/json"
	"strings"

	"cybermesh/telemetry-layer/adapters/internal/model"
	telemetrypb "cybermesh/telemetry-layer/proto/gen/go"
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
		TraceId:             agg.TraceID,
		SourceEventId:       agg.SourceEventID,
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
