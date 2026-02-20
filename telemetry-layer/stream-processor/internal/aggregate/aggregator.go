package aggregate

import (
	"fmt"
	"sync"
	"time"

	"cybermesh/telemetry-layer/stream-processor/internal/hash"
	"cybermesh/telemetry-layer/stream-processor/internal/model"
)

type Aggregator struct {
	windowSeconds int64
	mu            sync.Mutex
	buckets       map[bucketKey]*bucket
}

type bucketKey struct {
	key         model.FlowKey
	windowStart int64
	tenantID    string
}

type bucket struct {
	firstSeen        int64
	lastSeen         int64
	bytesFwd         int64
	bytesBwd         int64
	pktsFwd          int64
	pktsBwd          int64
	identity         model.Identity
	verdict          string
	metricsKnown     bool
	sourceType       string
	sourceID         string
	timingKnown      bool
	timingDerived    bool
	derivationPolicy string
	flagsKnown       bool
	flowIatMean      float64
	flowIatStd       float64
	flowIatMax       float64
	flowIatMin       float64
	fwdIatTot        float64
	fwdIatMean       float64
	fwdIatStd        float64
	fwdIatMax        float64
	fwdIatMin        float64
	bwdIatTot        float64
	bwdIatMean       float64
	bwdIatStd        float64
	bwdIatMax        float64
	bwdIatMin        float64
	activeMean       float64
	activeStd        float64
	activeMax        float64
	activeMin        float64
	idleMean         float64
	idleStd          float64
	idleMax          float64
	idleMin          float64
	finFlagCnt       float64
	synFlagCnt       float64
	rstFlagCnt       float64
	pshFlagCnt       float64
	ackFlagCnt       float64
	urgFlagCnt       float64
	cweFlagCount     float64
	eceFlagCnt       float64
}

func New(windowSeconds int64) *Aggregator {
	if windowSeconds <= 0 {
		windowSeconds = 10
	}
	return &Aggregator{
		windowSeconds: windowSeconds,
		buckets:       make(map[bucketKey]*bucket),
	}
}

func (a *Aggregator) Add(event model.FlowEvent) []model.FlowAggregate {
	a.mu.Lock()
	defer a.mu.Unlock()

	windowStart := (event.Timestamp / a.windowSeconds) * a.windowSeconds
	key := bucketKey{
		key: model.FlowKey{
			SrcIP:   event.SrcIP,
			DstIP:   event.DstIP,
			SrcPort: event.SrcPort,
			DstPort: event.DstPort,
			Proto:   event.Proto,
		},
		windowStart: windowStart,
		tenantID:    event.TenantID,
	}

	b, ok := a.buckets[key]
	if !ok {
		b = &bucket{
			firstSeen:        event.Timestamp,
			lastSeen:         event.Timestamp,
			identity:         event.Identity,
			verdict:          event.Verdict,
			metricsKnown:     event.MetricsKnown,
			sourceType:       event.SourceType,
			sourceID:         event.SourceID,
			timingKnown:      event.TimingKnown,
			timingDerived:    event.TimingDerived,
			derivationPolicy: event.DerivationPolicy,
			flagsKnown:       event.FlagsKnown,
			flowIatMean:      event.FlowIatMean,
			flowIatStd:       event.FlowIatStd,
			flowIatMax:       event.FlowIatMax,
			flowIatMin:       event.FlowIatMin,
			fwdIatTot:        event.FwdIatTot,
			fwdIatMean:       event.FwdIatMean,
			fwdIatStd:        event.FwdIatStd,
			fwdIatMax:        event.FwdIatMax,
			fwdIatMin:        event.FwdIatMin,
			bwdIatTot:        event.BwdIatTot,
			bwdIatMean:       event.BwdIatMean,
			bwdIatStd:        event.BwdIatStd,
			bwdIatMax:        event.BwdIatMax,
			bwdIatMin:        event.BwdIatMin,
			activeMean:       event.ActiveMean,
			activeStd:        event.ActiveStd,
			activeMax:        event.ActiveMax,
			activeMin:        event.ActiveMin,
			idleMean:         event.IdleMean,
			idleStd:          event.IdleStd,
			idleMax:          event.IdleMax,
			idleMin:          event.IdleMin,
			finFlagCnt:       event.FinFlagCnt,
			synFlagCnt:       event.SynFlagCnt,
			rstFlagCnt:       event.RstFlagCnt,
			pshFlagCnt:       event.PshFlagCnt,
			ackFlagCnt:       event.AckFlagCnt,
			urgFlagCnt:       event.UrgFlagCnt,
			cweFlagCount:     event.CweFlagCount,
			eceFlagCnt:       event.EceFlagCnt,
		}
		a.buckets[key] = b
	} else {
		if event.Timestamp < b.firstSeen {
			b.firstSeen = event.Timestamp
		}
		if event.Timestamp > b.lastSeen {
			b.lastSeen = event.Timestamp
		}
		if b.verdict == "" {
			b.verdict = event.Verdict
		}
		if event.MetricsKnown {
			b.metricsKnown = true
		}
		if b.sourceType == "" {
			b.sourceType = event.SourceType
		}
		if b.sourceID == "" {
			b.sourceID = event.SourceID
		}
		if event.TimingKnown {
			b.timingKnown = true
			b.timingDerived = event.TimingDerived
			b.derivationPolicy = event.DerivationPolicy
			b.flowIatMean = event.FlowIatMean
			b.flowIatStd = event.FlowIatStd
			b.flowIatMax = event.FlowIatMax
			b.flowIatMin = event.FlowIatMin
			b.fwdIatTot = event.FwdIatTot
			b.fwdIatMean = event.FwdIatMean
			b.fwdIatStd = event.FwdIatStd
			b.fwdIatMax = event.FwdIatMax
			b.fwdIatMin = event.FwdIatMin
			b.bwdIatTot = event.BwdIatTot
			b.bwdIatMean = event.BwdIatMean
			b.bwdIatStd = event.BwdIatStd
			b.bwdIatMax = event.BwdIatMax
			b.bwdIatMin = event.BwdIatMin
			b.activeMean = event.ActiveMean
			b.activeStd = event.ActiveStd
			b.activeMax = event.ActiveMax
			b.activeMin = event.ActiveMin
			b.idleMean = event.IdleMean
			b.idleStd = event.IdleStd
			b.idleMax = event.IdleMax
			b.idleMin = event.IdleMin
		}
		if event.FlagsKnown {
			b.flagsKnown = true
			b.finFlagCnt = event.FinFlagCnt
			b.synFlagCnt = event.SynFlagCnt
			b.rstFlagCnt = event.RstFlagCnt
			b.pshFlagCnt = event.PshFlagCnt
			b.ackFlagCnt = event.AckFlagCnt
			b.urgFlagCnt = event.UrgFlagCnt
			b.cweFlagCount = event.CweFlagCount
			b.eceFlagCnt = event.EceFlagCnt
		}
	}

	if isForward(event.Direction) {
		b.bytesFwd += event.Bytes
		b.pktsFwd += event.Packets
	} else {
		b.bytesBwd += event.Bytes
		b.pktsBwd += event.Packets
	}

	return nil
}

func (a *Aggregator) Flush(now time.Time) []model.FlowAggregate {
	a.mu.Lock()
	defer a.mu.Unlock()

	cutoff := now.Unix() - a.windowSeconds
	var out []model.FlowAggregate

	for key, b := range a.buckets {
		if key.windowStart > cutoff {
			continue
		}
		durationMS := (b.lastSeen - b.firstSeen) * 1000
		if durationMS < 0 {
			durationMS = 0
		}
		flowKey := fmt.Sprintf("%s:%d-%s:%d-%d", key.key.SrcIP, key.key.SrcPort, key.key.DstIP, key.key.DstPort, key.key.Proto)
		flowID := hash.FlowID(flowKey, key.windowStart)
		out = append(out, model.FlowAggregate{
			Schema:           "flow.v1",
			Timestamp:        key.windowStart,
			TenantID:         key.tenantID,
			SrcIP:            key.key.SrcIP,
			DstIP:            key.key.DstIP,
			SrcPort:          key.key.SrcPort,
			DstPort:          key.key.DstPort,
			Proto:            key.key.Proto,
			FlowID:           flowID,
			BytesFwd:         b.bytesFwd,
			BytesBwd:         b.bytesBwd,
			PktsFwd:          b.pktsFwd,
			PktsBwd:          b.pktsBwd,
			DurationMS:       durationMS,
			Identity:         b.identity,
			Verdict:          b.verdict,
			MetricsKnown:     b.metricsKnown,
			SourceType:       b.sourceType,
			SourceID:         b.sourceID,
			TimingKnown:      b.timingKnown,
			TimingDerived:    b.timingDerived,
			DerivationPolicy: b.derivationPolicy,
			FlagsKnown:       b.flagsKnown,
			FlowIatMean:      b.flowIatMean,
			FlowIatStd:       b.flowIatStd,
			FlowIatMax:       b.flowIatMax,
			FlowIatMin:       b.flowIatMin,
			FwdIatTot:        b.fwdIatTot,
			FwdIatMean:       b.fwdIatMean,
			FwdIatStd:        b.fwdIatStd,
			FwdIatMax:        b.fwdIatMax,
			FwdIatMin:        b.fwdIatMin,
			BwdIatTot:        b.bwdIatTot,
			BwdIatMean:       b.bwdIatMean,
			BwdIatStd:        b.bwdIatStd,
			BwdIatMax:        b.bwdIatMax,
			BwdIatMin:        b.bwdIatMin,
			ActiveMean:       b.activeMean,
			ActiveStd:        b.activeStd,
			ActiveMax:        b.activeMax,
			ActiveMin:        b.activeMin,
			IdleMean:         b.idleMean,
			IdleStd:          b.idleStd,
			IdleMax:          b.idleMax,
			IdleMin:          b.idleMin,
			FinFlagCnt:       b.finFlagCnt,
			SynFlagCnt:       b.synFlagCnt,
			RstFlagCnt:       b.rstFlagCnt,
			PshFlagCnt:       b.pshFlagCnt,
			AckFlagCnt:       b.ackFlagCnt,
			UrgFlagCnt:       b.urgFlagCnt,
			CweFlagCount:     b.cweFlagCount,
			EceFlagCnt:       b.eceFlagCnt,
		})
		delete(a.buckets, key)
	}

	return out
}

func isForward(direction string) bool {
	switch direction {
	case "egress", "outbound", "forward", "fwd":
		return true
	case "ingress", "inbound", "reverse", "bwd":
		return false
	default:
		return true
	}
}
