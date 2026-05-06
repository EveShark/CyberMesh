package policytrace

import (
	"context"
	"sort"
	"sync"
	"time"
)

// RuntimeSink observes runtime markers emitted through the in-memory collector.
// Phase 1 keeps the collector behavior unchanged unless a sink is explicitly attached.
type RuntimeSink interface {
	RecordRuntimeMarker(marker Marker) error
}

// BatchRuntimeSink allows the collector to persist hot-path markers in batches.
type BatchRuntimeSink interface {
	RecordRuntimeMarkers(ctx context.Context, markers []Marker) error
}

// Marker captures a single runtime stage transition for a policy workflow.
type Marker struct {
	Stage           string
	PolicyID        string
	TraceID         string
	RequestID       string
	CommandID       string
	WorkflowID      string
	SourceEventID   string
	SentinelEventID string
	ScopeIdentifier string
	Tenant          string
	Region          string
	Reason          string
	TimestampMs     int64
	Height          uint64
	TxIndex         int
	View            uint64
	QCTsMs          int64
	OutboxID        string
	AckEventID      string
	Partition       int32
	Offset          int64
	RuleHash        []byte
}

// Collector keeps a bounded in-memory runtime trace lane keyed by policy ID.
type Collector struct {
	mu                  sync.RWMutex
	byPolicy            map[string][]Marker
	order               []string
	maxPolicies         int
	maxMarkersPerPolicy int
	sinks               []RuntimeSink
	sinkCh              chan Marker
	sinkOnce            sync.Once
}

func NewCollector(maxPolicies, maxMarkersPerPolicy int) *Collector {
	if maxPolicies <= 0 {
		maxPolicies = 4096
	}
	if maxMarkersPerPolicy <= 0 {
		maxMarkersPerPolicy = 64
	}
	return &Collector{
		byPolicy:            make(map[string][]Marker),
		maxPolicies:         maxPolicies,
		maxMarkersPerPolicy: maxMarkersPerPolicy,
		sinkCh:              make(chan Marker, 65536),
	}
}

func (c *Collector) Record(marker Marker) {
	if c == nil || marker.PolicyID == "" || marker.Stage == "" || marker.TimestampMs <= 0 {
		return
	}

	c.mu.Lock()

	if _, ok := c.byPolicy[marker.PolicyID]; !ok {
		if len(c.byPolicy) >= c.maxPolicies && len(c.order) > 0 {
			evict := c.order[0]
			c.order = c.order[1:]
			delete(c.byPolicy, evict)
		}
		c.order = append(c.order, marker.PolicyID)
	}

	entries := append(c.byPolicy[marker.PolicyID], marker)
	if len(entries) > c.maxMarkersPerPolicy {
		entries = append([]Marker(nil), entries[len(entries)-c.maxMarkersPerPolicy:]...)
	}
	c.byPolicy[marker.PolicyID] = entries
	c.mu.Unlock()

	if !c.hasSinks() {
		return
	}
	select {
	case c.sinkCh <- marker:
	default:
		// Trace persistence must not run on the consensus hot path. If the queue
		// is saturated, spill to a goroutine and preserve the source timestamp.
		go c.dispatchRuntimeMarkers([]Marker{marker})
	}
}

func (c *Collector) GetPolicy(policyID string) []Marker {
	if c == nil || policyID == "" {
		return nil
	}
	c.mu.RLock()
	entries := append([]Marker(nil), c.byPolicy[policyID]...)
	c.mu.RUnlock()
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].TimestampMs == entries[j].TimestampMs {
			return entries[i].Stage < entries[j].Stage
		}
		return entries[i].TimestampMs < entries[j].TimestampMs
	})
	return entries
}

func (c *Collector) AddSink(sink RuntimeSink) {
	if c == nil || sink == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sinks = append(c.sinks, sink)
	c.sinkOnce.Do(func() {
		go c.runSinkDispatcher()
	})
}

func (c *Collector) hasSinks() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.sinks) > 0
}

func (c *Collector) runSinkDispatcher() {
	const maxBatch = 256
	timer := time.NewTimer(50 * time.Millisecond)
	defer timer.Stop()
	batch := make([]Marker, 0, maxBatch)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		out := append([]Marker(nil), batch...)
		batch = batch[:0]
		c.dispatchRuntimeMarkers(out)
	}
	for {
		select {
		case marker := <-c.sinkCh:
			batch = append(batch, marker)
			if len(batch) >= maxBatch {
				flush()
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(50 * time.Millisecond)
			}
		case <-timer.C:
			flush()
			timer.Reset(50 * time.Millisecond)
		}
	}
}

func (c *Collector) dispatchRuntimeMarkers(markers []Marker) {
	if c == nil || len(markers) == 0 {
		return
	}
	c.mu.RLock()
	sinks := append([]RuntimeSink(nil), c.sinks...)
	c.mu.RUnlock()
	for _, sink := range sinks {
		if sink == nil {
			continue
		}
		if batchSink, ok := sink.(BatchRuntimeSink); ok {
			_ = batchSink.RecordRuntimeMarkers(context.Background(), markers)
			continue
		}
		for _, marker := range markers {
			_ = sink.RecordRuntimeMarker(marker)
		}
	}
}
