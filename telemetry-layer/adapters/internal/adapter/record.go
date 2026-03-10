package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"cybermesh/telemetry-layer/adapters/internal/hash"
	"cybermesh/telemetry-layer/adapters/internal/model"
)

type Record = model.Record

func FromJSONLine(line []byte) (Record, error) {
	var rec Record
	if err := json.Unmarshal(line, &rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func ToFlow(rec Record, windowSeconds int64) (model.FlowAggregate, error) {
	if rec.TenantID == "" || rec.SrcIP == "" || rec.DstIP == "" {
		return model.FlowAggregate{}, errors.New("missing required fields")
	}
	if net.ParseIP(rec.SrcIP) == nil || net.ParseIP(rec.DstIP) == nil {
		return model.FlowAggregate{}, errors.New("invalid ip")
	}
	if rec.SrcPort < 0 || rec.SrcPort > 65535 || rec.DstPort < 0 || rec.DstPort > 65535 {
		return model.FlowAggregate{}, errors.New("invalid port")
	}
	if rec.Timestamp <= 0 {
		rec.Timestamp = time.Now().Unix()
	}
	ingestTsMs := time.Now().UnixMilli()
	if windowSeconds <= 0 {
		windowSeconds = 10
	}
	sourceEventTsMs := rec.SourceEventTsMs
	if sourceEventTsMs <= 0 {
		sourceEventTsMs = rec.Timestamp * 1000
	}
	windowStart := (rec.Timestamp / windowSeconds) * windowSeconds
	flowKey := fmt.Sprintf("%s:%d-%s:%d-%d", rec.SrcIP, rec.SrcPort, rec.DstIP, rec.DstPort, rec.Proto)
	flowID := hash.FlowID(flowKey, windowStart)
	return model.FlowAggregate{
		Schema:              "flow.v1",
		Timestamp:           windowStart,
		TenantID:            rec.TenantID,
		SrcIP:               rec.SrcIP,
		DstIP:               rec.DstIP,
		SrcPort:             rec.SrcPort,
		DstPort:             rec.DstPort,
		Proto:               rec.Proto,
		FlowID:              flowID,
		TraceID:             flowID,
		SourceEventID:       flowID,
		SourceEventTsMs:     sourceEventTsMs,
		TelemetryIngestTsMs: ingestTsMs,
		BytesFwd:            rec.BytesFwd,
		BytesBwd:            rec.BytesBwd,
		PktsFwd:             rec.PktsFwd,
		PktsBwd:             rec.PktsBwd,
		DurationMS:          rec.DurationMS,
		Identity:            rec.Identity,
		Verdict:             rec.Verdict,
		MetricsKnown:        rec.MetricsKnown,
		SourceType:          rec.SourceType,
		SourceID:            rec.SourceID,
		TimingKnown:         rec.TimingKnown,
		TimingDerived:       rec.TimingDerived,
		DerivationPolicy:    rec.DerivationPolicy,
		FlagsKnown:          rec.FlagsKnown,
		FlowIatMean:         rec.FlowIatMean,
		FlowIatStd:          rec.FlowIatStd,
		FlowIatMax:          rec.FlowIatMax,
		FlowIatMin:          rec.FlowIatMin,
		FwdIatTot:           rec.FwdIatTot,
		FwdIatMean:          rec.FwdIatMean,
		FwdIatStd:           rec.FwdIatStd,
		FwdIatMax:           rec.FwdIatMax,
		FwdIatMin:           rec.FwdIatMin,
		BwdIatTot:           rec.BwdIatTot,
		BwdIatMean:          rec.BwdIatMean,
		BwdIatStd:           rec.BwdIatStd,
		BwdIatMax:           rec.BwdIatMax,
		BwdIatMin:           rec.BwdIatMin,
		ActiveMean:          rec.ActiveMean,
		ActiveStd:           rec.ActiveStd,
		ActiveMax:           rec.ActiveMax,
		ActiveMin:           rec.ActiveMin,
		IdleMean:            rec.IdleMean,
		IdleStd:             rec.IdleStd,
		IdleMax:             rec.IdleMax,
		IdleMin:             rec.IdleMin,
		FinFlagCnt:          rec.FinFlagCnt,
		SynFlagCnt:          rec.SynFlagCnt,
		RstFlagCnt:          rec.RstFlagCnt,
		PshFlagCnt:          rec.PshFlagCnt,
		AckFlagCnt:          rec.AckFlagCnt,
		UrgFlagCnt:          rec.UrgFlagCnt,
		CweFlagCount:        rec.CweFlagCount,
		EceFlagCnt:          rec.EceFlagCnt,
	}, nil
}
