package metrics

import "github.com/prometheus/client_golang/prometheus"

// AckQueueMetrics exposes specialized ACK observability hooks.
type AckQueueMetrics struct {
	depth    prometheus.Gauge
	latency  prometheus.Histogram
	failures prometheus.Counter
	blocked  prometheus.Counter
}

// NewAckQueueMetrics registers queue metrics to registry.
func NewAckQueueMetrics(reg prometheus.Registerer) *AckQueueMetrics {
	m := &AckQueueMetrics{
		depth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "policy_ack_queue_depth",
			Help: "Number of ACK payloads queued for retry",
		}),
		latency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "policy_ack_queue_latency_seconds",
			Help:    "Latency from dequeue to publish",
			Buckets: prometheus.DefBuckets,
		}),
		failures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_ack_queue_failures_total",
			Help: "Queue dequeue/publish failures",
		}),
		blocked: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_ack_queue_blocked_total",
			Help: "Attempts to enqueue when queue is full",
		}),
	}
	reg.MustRegister(m.depth, m.latency, m.failures, m.blocked)
	return m
}

// ObserveDepth records queue size.
func (m *AckQueueMetrics) ObserveDepth(size int) {
	if m == nil {
		return
	}
	m.depth.Set(float64(size))
}

// ObserveLatency records publish latency.
func (m *AckQueueMetrics) ObserveLatency(seconds float64) {
	if m == nil {
		return
	}
	m.latency.Observe(seconds)
}

// ObserveFailure increments failure counter.
func (m *AckQueueMetrics) ObserveFailure() {
	if m == nil {
		return
	}
	m.failures.Inc()
}

// ObserveBlocked increments blocked counter.
func (m *AckQueueMetrics) ObserveBlocked() {
	if m == nil {
		return
	}
	m.blocked.Inc()
}
