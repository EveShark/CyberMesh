package aggregate

import (
	"fmt"
	"sync"
	"time"

	"cybermesh/telemetry-layer/ingest-bridge/internal/hash"
	"cybermesh/telemetry-layer/ingest-bridge/internal/model"
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
	firstSeen    int64
	lastSeen     int64
	bytesFwd     int64
	bytesBwd     int64
	pktsFwd      int64
	pktsBwd      int64
	identity     model.Identity
	verdict      string
	metricsKnown bool
	sourceType   string
	sourceID     string
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
			firstSeen:    event.Timestamp,
			lastSeen:     event.Timestamp,
			identity:     event.Identity,
			verdict:      event.Verdict,
			metricsKnown: event.MetricsKnown,
			sourceType:   event.SourceType,
			sourceID:     event.SourceID,
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
			Schema:       "flow.v1",
			Timestamp:    key.windowStart,
			TenantID:     key.tenantID,
			SrcIP:        key.key.SrcIP,
			DstIP:        key.key.DstIP,
			SrcPort:      key.key.SrcPort,
			DstPort:      key.key.DstPort,
			Proto:        key.key.Proto,
			FlowID:       flowID,
			BytesFwd:     b.bytesFwd,
			BytesBwd:     b.bytesBwd,
			PktsFwd:      b.pktsFwd,
			PktsBwd:      b.pktsBwd,
			DurationMS:   durationMS,
			Identity:     b.identity,
			Verdict:      b.verdict,
			MetricsKnown: b.metricsKnown,
			SourceType:   b.sourceType,
			SourceID:     b.sourceID,
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
