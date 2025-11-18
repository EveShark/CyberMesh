package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Recorder exposes Prometheus metrics for the agent.
type Recorder struct {
	ingested            prometheus.Counter
	validated           prometheus.Counter
	rejected            prometheus.Counter
	applied             prometheus.Counter
	removed             prometheus.Counter
	applyDuration       *prometheus.HistogramVec
	reconciled          prometheus.Counter
	reconcileErrs       prometheus.Counter
	reconcileDur        *prometheus.HistogramVec
	guardrail           *prometheus.CounterVec
	fastPathEligibility *prometheus.CounterVec
	backendApplied      *prometheus.CounterVec
	activeGauge         prometheus.Gauge
	killSwitch          prometheus.Gauge
	backoffGauge        *prometheus.GaugeVec
	ledgerDrift         *prometheus.CounterVec
	kafkaLag            *prometheus.GaugeVec
	kafkaErrors         *prometheus.CounterVec
	ackPublish          *prometheus.CounterVec
	ackRetry            prometheus.Counter
	ackQueueDepth       prometheus.Gauge
	ackLatency          prometheus.Histogram
	ackQueueFailures    prometheus.Counter
	ackQueueBlocked     prometheus.Counter
	ackBatchSize        prometheus.Histogram
	ackBatchFlush       *prometheus.CounterVec
	backend             string
}

// NewRecorder registers metrics with provided registry.
func NewRecorder(reg prometheus.Registerer) *Recorder {
	r := &Recorder{
		ingested: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_ingested_total",
			Help: "Total number of policy events ingested",
		}),
		validated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_validated_total",
			Help: "Total number of policy events successfully validated",
		}),
		rejected: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_rejected_total",
			Help: "Total number of policy events rejected by guardrails",
		}),
		applied: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_applied_total",
			Help: "Total number of policies applied to enforcement backend",
		}),
		removed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_removed_total",
			Help: "Total number of policies removed from backend",
		}),
		applyDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "policy_apply_duration_seconds",
			Help:    "Latency of applying policies",
			Buckets: prometheus.DefBuckets,
		}, []string{"result"}),
		reconciled: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_reconciled_total",
			Help: "Total number of reconciliation apply attempts",
		}),
		reconcileErrs: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_reconcile_errors_total",
			Help: "Total reconciliation failures",
		}),
		reconcileDur: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "policy_reconcile_duration_seconds",
			Help:    "Latency of reconciliation apply operations",
			Buckets: prometheus.DefBuckets,
		}, []string{"result"}),
		guardrail: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_guardrail_violations_total",
			Help: "Total guardrail rejections grouped by reason",
		}, []string{"reason"}),
		fastPathEligibility: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "fast_path_eligibility_total",
			Help: "Fast-path eligibility decisions grouped by result",
		}, []string{"result"}),
		backendApplied: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_backend_applied_total",
			Help: "Total policies applied grouped by backend",
		}, []string{"backend"}),
		activeGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "policy_active_total",
			Help: "Number of active policies tracked by the agent",
		}),
		killSwitch: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "policy_kill_switch",
			Help: "Kill-switch state (1=enforcement disabled)",
		}),
		backoffGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "policy_backoff_seconds",
			Help: "Current backoff duration per component",
		}, []string{"component"}),
		ledgerDrift: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_ledger_drift_total",
			Help: "Ledger reconciliation drift adjustments",
		}, []string{"direction"}),
		kafkaLag: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "policy_consumer_lag",
			Help: "Kafka consumer lag by partition",
		}, []string{"partition"}),
		kafkaErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_consumer_errors_total",
			Help: "Kafka consumer errors grouped by reason",
		}, []string{"reason"}),
		ackPublish: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_ack_publish_total",
			Help: "ACK publish outcomes grouped by status",
		}, []string{"status"}),
		ackRetry: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_ack_retry_total",
			Help: "Total ACK publish retries",
		}),
		ackQueueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "policy_ack_queue_depth",
			Help: "Number of queued ACK payloads awaiting publish",
		}),
		ackLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "policy_ack_latency_seconds",
			Help:    "Latency between policy apply completion and ACK emission",
			Buckets: prometheus.DefBuckets,
		}),
		ackQueueFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_ack_queue_failures_total",
			Help: "Failures encountered when processing ACK queue",
		}),
		ackQueueBlocked: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_ack_queue_blocked_total",
			Help: "Number of times ACK queue rejected enqueue due to capacity",
		}),
		ackBatchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "policy_ack_batch_size",
			Help:    "Number of ACK payloads flushed per batch",
			Buckets: []float64{1, 5, 10, 20, 50, 100},
		}),
		ackBatchFlush: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_ack_batch_flush_total",
			Help: "ACK batch flush outcomes grouped by status",
		}, []string{"status"}),
	}

	reg.MustRegister(
		r.ingested,
		r.validated,
		r.rejected,
		r.applied,
		r.removed,
		r.applyDuration,
		r.reconciled,
		r.reconcileErrs,
		r.reconcileDur,
		r.guardrail,
		r.fastPathEligibility,
		r.backendApplied,
		r.activeGauge,
		r.killSwitch,
		r.backoffGauge,
		r.ledgerDrift,
		r.kafkaLag,
		r.kafkaErrors,
		r.ackPublish,
		r.ackRetry,
		r.ackQueueDepth,
		r.ackLatency,
		r.ackQueueFailures,
		r.ackQueueBlocked,
		r.ackBatchSize,
		r.ackBatchFlush,
	)
	return r
}

// Handler returns HTTP handler serving /metrics.
func Handler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg})
}

// ObserveIngest increments ingestion counter.
func (r *Recorder) ObserveIngest() { r.ingested.Inc() }

// ObserveValidated increments validated counter.
func (r *Recorder) ObserveValidated() { r.validated.Inc() }

// ObserveRejected increments rejected counter.
func (r *Recorder) ObserveRejected() { r.rejected.Inc() }

// ObserveApplied records apply success.
func (r *Recorder) ObserveApplied(d time.Duration) {
	r.applied.Inc()
	r.applyDuration.WithLabelValues("success").Observe(d.Seconds())
	if r.backendApplied != nil {
		backend := r.backend
		if backend == "" {
			backend = "unknown"
		}
		r.backendApplied.WithLabelValues(backend).Inc()
	}
}

// ObserveApplyError records apply failure.
func (r *Recorder) ObserveApplyError(d time.Duration) {
	r.applyDuration.WithLabelValues("error").Observe(d.Seconds())
}

// ObserveRemoved increments remove counter.
func (r *Recorder) ObserveRemoved() { r.removed.Inc() }

// ObserveReconciled records a successful reconciliation apply.
func (r *Recorder) ObserveReconciled(d time.Duration) {
	r.reconciled.Inc()
	r.reconcileDur.WithLabelValues("success").Observe(d.Seconds())
}

// ObserveReconcileError records a reconciliation error.
func (r *Recorder) ObserveReconcileError(d time.Duration) {
	r.reconcileErrs.Inc()
	r.reconcileDur.WithLabelValues("error").Observe(d.Seconds())
}

// ObserveGuardrailViolation increments guardrail violation counter with reason label.
func (r *Recorder) ObserveGuardrailViolation(reason string) {
	if reason == "" {
		reason = "unknown"
	}
	r.guardrail.WithLabelValues(reason).Inc()
}

// ObserveFastPathEligibility records fast-path eligibility decisions.
func (r *Recorder) ObserveFastPathEligibility(result string) {
	if result == "" {
		result = "unknown"
	}
	r.fastPathEligibility.WithLabelValues(result).Inc()
}

// SetBackend identifies the enforcement backend for backend-labelled counters.
func (r *Recorder) SetBackend(backend string) {
	r.backend = backend
}

// SetActivePolicies records the number of active policies.
func (r *Recorder) SetActivePolicies(count int) {
	if r.activeGauge != nil {
		r.activeGauge.Set(float64(count))
	}
}

// SetKillSwitch toggles the kill-switch gauge (1 means enforcement disabled).
func (r *Recorder) SetKillSwitch(enabled bool) {
	if r.killSwitch != nil {
		if enabled {
			r.killSwitch.Set(1)
		} else {
			r.killSwitch.Set(0)
		}
	}
}

// ObserveBackoff records the current backoff duration for a component.
func (r *Recorder) ObserveBackoff(component string, duration time.Duration) {
	if r.backoffGauge != nil {
		r.backoffGauge.WithLabelValues(component).Set(duration.Seconds())
	}
}

// ObserveLedgerDrift records ledger reconciliation adjustments.
func (r *Recorder) ObserveLedgerDrift(removed, added int) {
	if r.ledgerDrift == nil {
		return
	}
	if removed > 0 {
		r.ledgerDrift.WithLabelValues("removed").Add(float64(removed))
	}
	if added > 0 {
		r.ledgerDrift.WithLabelValues("added").Add(float64(added))
	}
}

// ObserveKafkaLag reports consumer lag per partition.
func (r *Recorder) ObserveKafkaLag(partition int32, lag int64) {
	if r.kafkaLag != nil {
		r.kafkaLag.WithLabelValues(fmt.Sprintf("%d", partition)).Set(float64(lag))
	}
}

// ObserveKafkaError increments consumer error counters.
func (r *Recorder) ObserveKafkaError(reason string) {
	if reason == "" {
		reason = "unknown"
	}
	if r.kafkaErrors != nil {
		r.kafkaErrors.WithLabelValues(reason).Inc()
	}
}

// ObserveAckPublish records ACK publish outcome.
func (r *Recorder) ObserveAckPublish(status string) {
	if status == "" {
		status = "unknown"
	}
	if r.ackPublish != nil {
		r.ackPublish.WithLabelValues(status).Inc()
	}
}

// ObserveAckRetry increments retry counter.
func (r *Recorder) ObserveAckRetry() {
	if r.ackRetry != nil {
		r.ackRetry.Inc()
	}
}

// ObserveAckQueueDepth records queue depth.
func (r *Recorder) ObserveAckQueueDepth(size int) {
	if r.ackQueueDepth != nil {
		r.ackQueueDepth.Set(float64(size))
	}
}

// ObserveAckLatency records ack publish latency in seconds.
func (r *Recorder) ObserveAckLatency(seconds float64) {
	if r.ackLatency != nil {
		r.ackLatency.Observe(seconds)
	}
}

// ObserveAckQueueFailure increments queue failure counter.
func (r *Recorder) ObserveAckQueueFailure() {
	if r.ackQueueFailures != nil {
		r.ackQueueFailures.Inc()
	}
}

// ObserveAckQueueBlocked increments blocked counter.
func (r *Recorder) ObserveAckQueueBlocked() {
	if r.ackQueueBlocked != nil {
		r.ackQueueBlocked.Inc()
	}
}

// ObserveAckBatchSize records batch size histogram.
func (r *Recorder) ObserveAckBatchSize(size int) {
	if r.ackBatchSize != nil && size > 0 {
		r.ackBatchSize.Observe(float64(size))
	}
}

// ObserveAckBatchFlush records batch flush outcomes.
func (r *Recorder) ObserveAckBatchFlush(status string) {
	if status == "" {
		status = "unknown"
	}
	if r.ackBatchFlush != nil {
		r.ackBatchFlush.WithLabelValues(status).Inc()
	}
}

// KafkaLagGauge exposes the consumer lag gauge (used in tests).
func (r *Recorder) KafkaLagGauge() *prometheus.GaugeVec { return r.kafkaLag }

// AppliedCounter exposes the total applied counter (used in tests).
func (r *Recorder) AppliedCounter() prometheus.Counter { return r.applied }

// GuardrailCounter exposes guardrail violation counters.
func (r *Recorder) GuardrailCounter() *prometheus.CounterVec { return r.guardrail }

// RejectedCounter exposes the rejected counter.
func (r *Recorder) RejectedCounter() prometheus.Counter { return r.rejected }

// ValidatedCounter exposes the validated counter.
func (r *Recorder) ValidatedCounter() prometheus.Counter { return r.validated }

// KafkaErrorCounter exposes the consumer error counter (used in tests).
func (r *Recorder) KafkaErrorCounter() *prometheus.CounterVec { return r.kafkaErrors }
