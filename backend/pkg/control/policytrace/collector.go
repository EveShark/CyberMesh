package policytrace

import (
	"sort"
	"sync"
)

// Marker captures a single runtime stage transition for a policy workflow.
type Marker struct {
	Stage       string
	PolicyID    string
	TraceID     string
	Reason      string
	TimestampMs int64
	Height      uint64
	View        uint64
	QCTsMs      int64
	OutboxID    string
	Partition   int32
	Offset      int64
}

// Collector keeps a bounded in-memory runtime trace lane keyed by policy ID.
type Collector struct {
	mu                  sync.RWMutex
	byPolicy            map[string][]Marker
	order               []string
	maxPolicies         int
	maxMarkersPerPolicy int
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
	}
}

func (c *Collector) Record(marker Marker) {
	if c == nil || marker.PolicyID == "" || marker.Stage == "" || marker.TimestampMs <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

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
