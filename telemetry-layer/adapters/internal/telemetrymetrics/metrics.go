package telemetrymetrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type histogram struct {
	buckets []float64
	counts  []uint64
	count   uint64
	sum     float64
}

func newHistogram(buckets []float64) *histogram {
	return &histogram{
		buckets: append([]float64(nil), buckets...),
		counts:  make([]uint64, len(buckets)),
	}
}

func (h *histogram) observe(seconds float64) {
	h.count++
	h.sum += seconds
	for i, bucket := range h.buckets {
		if seconds <= bucket {
			h.counts[i]++
		}
	}
}

type Collector struct {
	mu         sync.RWMutex
	operations map[string]uint64
	latencies  map[string]*histogram
	counters   map[string]uint64
}

var (
	defaultBuckets = []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0}
	globalMu       sync.RWMutex
	global         = NewCollector()
)

func NewCollector() *Collector {
	return &Collector{
		operations: make(map[string]uint64),
		latencies:  make(map[string]*histogram),
		counters:   make(map[string]uint64),
	}
}

func Global() *Collector {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return global
}

func ResetGlobal() {
	globalMu.Lock()
	defer globalMu.Unlock()
	global = NewCollector()
}

func opKey(operation, status string) string {
	return operation + "|" + status
}

func (c *Collector) RecordOperation(operation, status string, duration time.Duration) {
	if c == nil {
		return
	}
	seconds := duration.Seconds()
	key := opKey(operation, status)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.operations[key]++
	h, ok := c.latencies[key]
	if !ok {
		h = newHistogram(defaultBuckets)
		c.latencies[key] = h
	}
	h.observe(seconds)
}

func (c *Collector) IncCounter(name string) {
	if c == nil || strings.TrimSpace(name) == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counters[name]++
}

func (c *Collector) RenderPrometheus() string {
	if c == nil {
		return ""
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	var b strings.Builder
	b.WriteString("# HELP telemetry_adapter_operations_total Telemetry adapter operations by operation and status\n")
	b.WriteString("# TYPE telemetry_adapter_operations_total counter\n")

	opKeys := make([]string, 0, len(c.operations))
	for key := range c.operations {
		opKeys = append(opKeys, key)
	}
	sort.Strings(opKeys)
	for _, key := range opKeys {
		parts := strings.SplitN(key, "|", 2)
		b.WriteString(fmt.Sprintf(
			"telemetry_adapter_operations_total{operation=%q,status=%q} %d\n",
			parts[0], parts[1], c.operations[key],
		))
	}

	b.WriteString("# HELP telemetry_adapter_operation_latency_seconds Telemetry adapter operation latency\n")
	b.WriteString("# TYPE telemetry_adapter_operation_latency_seconds histogram\n")

	latencyKeys := make([]string, 0, len(c.latencies))
	for key := range c.latencies {
		latencyKeys = append(latencyKeys, key)
	}
	sort.Strings(latencyKeys)
	for _, key := range latencyKeys {
		parts := strings.SplitN(key, "|", 2)
		h := c.latencies[key]
		for i, bucket := range h.buckets {
			b.WriteString(fmt.Sprintf(
				"telemetry_adapter_operation_latency_seconds_bucket{operation=%q,status=%q,le=%q} %d\n",
				parts[0], parts[1], formatFloat(bucket), h.counts[i],
			))
		}
		b.WriteString(fmt.Sprintf(
			"telemetry_adapter_operation_latency_seconds_bucket{operation=%q,status=%q,le=\"+Inf\"} %d\n",
			parts[0], parts[1], h.count,
		))
		b.WriteString(fmt.Sprintf(
			"telemetry_adapter_operation_latency_seconds_sum{operation=%q,status=%q} %s\n",
			parts[0], parts[1], formatFloat(h.sum),
		))
		b.WriteString(fmt.Sprintf(
			"telemetry_adapter_operation_latency_seconds_count{operation=%q,status=%q} %d\n",
			parts[0], parts[1], h.count,
		))
	}

	counterHelp := map[string]string{
		"trace_id_missing_total":              "Total records missing a valid upstream trace_id at telemetry ingress",
		"source_event_id_synthesized_total":   "Total records where telemetry synthesized source_event_id",
	}
	counterNames := make([]string, 0, len(c.counters))
	for name := range c.counters {
		counterNames = append(counterNames, name)
	}
	sort.Strings(counterNames)
	for _, name := range counterNames {
		help := counterHelp[name]
		if help == "" {
			help = "Telemetry lineage counter"
		}
		b.WriteString(fmt.Sprintf("# HELP %s %s\n", name, help))
		b.WriteString(fmt.Sprintf("# TYPE %s counter\n", name))
		b.WriteString(fmt.Sprintf("%s %d\n", name, c.counters[name]))
	}

	return b.String()
}

func (c *Collector) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL != nil && r.URL.Path != "" && r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte(c.RenderPrometheus()))
	})
}

func formatFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", v), "0"), ".")
}
