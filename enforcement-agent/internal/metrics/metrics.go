package metrics

import (
	"fmt"
	"net/http"
	"strings"
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
	kafkaConsumed       *prometheus.CounterVec
	kafkaErrors         *prometheus.CounterVec
	kafkaPartitions     *prometheus.GaugeVec
	kafkaPartitionEvent *prometheus.CounterVec
	kafkaQueueSaturated prometheus.Counter
	kafkaDispatchWait   prometheus.Histogram
	publishToConsume    prometheus.Histogram
	consumeToApply      *prometheus.HistogramVec
	consumeToDone       *prometheus.HistogramVec
	applyPersist        *prometheus.HistogramVec
	applyToAckEnqueue   *prometheus.HistogramVec
	offsetMark          prometheus.Histogram
	duplicatesByScope   *prometheus.CounterVec
	ackPublish          *prometheus.CounterVec
	ackRetry            prometheus.Counter
	ackQueueDepth       prometheus.Gauge
	ackLatency          prometheus.Histogram
	ackQueueFailures    prometheus.Counter
	ackQueueBlocked     prometheus.Counter
	ackBatchSize        prometheus.Histogram
	ackBatchFlush       *prometheus.CounterVec
	gatewayTranslation  *prometheus.CounterVec
	gatewayApplyTotal   *prometheus.CounterVec
	gatewayApplyDur     *prometheus.HistogramVec
	gatewayGuardrail    *prometheus.CounterVec
	gatewayReplay       *prometheus.CounterVec
	gatewayTenantReject prometheus.Counter
	gatewayActiveRules  prometheus.Gauge
	gatewayAckPublish   *prometheus.CounterVec
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
		kafkaConsumed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_consumer_messages_total",
			Help: "Kafka policy messages consumed grouped by partition and handler result",
		}, []string{"partition", "result"}),
		kafkaErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_consumer_errors_total",
			Help: "Kafka consumer errors grouped by reason",
		}, []string{"reason"}),
		kafkaPartitions: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "policy_consumer_partition_owned",
			Help: "Whether this agent instance currently owns a Kafka partition",
		}, []string{"partition"}),
		kafkaPartitionEvent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_consumer_partition_events_total",
			Help: "Kafka partition assignment lifecycle events grouped by partition and event",
		}, []string{"partition", "event"}),
		kafkaQueueSaturated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_consumer_queue_saturated_total",
			Help: "Number of times consumer worker queues were saturated and dispatch had to wait",
		}),
		kafkaDispatchWait: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "policy_consumer_dispatch_wait_seconds",
			Help:    "Latency waiting to dispatch a consumed message to a worker",
			Buckets: prometheus.DefBuckets,
		}),
		publishToConsume: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "policy_publish_to_consume_seconds",
			Help:    "Latency from Kafka publish timestamp to enforcement consume time",
			Buckets: prometheus.DefBuckets,
		}),
		consumeToApply: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "policy_consume_to_apply_seconds",
			Help:    "Latency from enforcement consume to policy apply result",
			Buckets: prometheus.DefBuckets,
		}, []string{"result"}),
		consumeToDone: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "policy_consume_to_done_seconds",
			Help:    "Latency from enforcement consume to handler completion",
			Buckets: prometheus.DefBuckets,
		}, []string{"result"}),
		applyPersist: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "policy_apply_persist_seconds",
			Help:    "Latency from successful apply to local state persistence result",
			Buckets: prometheus.DefBuckets,
		}, []string{"result"}),
		applyToAckEnqueue: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "policy_apply_to_ack_enqueue_seconds",
			Help:    "Latency from policy apply completion to ACK enqueue result",
			Buckets: prometheus.DefBuckets,
		}, []string{"result"}),
		offsetMark: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "policy_offset_mark_seconds",
			Help:    "Latency to mark a consumed Kafka message for offset commit",
			Buckets: prometheus.DefBuckets,
		}),
		duplicatesByScope: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_duplicate_total",
			Help: "Duplicate or no-op policy deliveries grouped by scope kind",
		}, []string{"scope_kind"}),
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
		gatewayTranslation: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_translation_total",
			Help: "Gateway translation outcomes grouped by status",
		}, []string{"status"}),
		gatewayApplyTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_apply_total",
			Help: "Gateway apply outcomes grouped by reason",
		}, []string{"reason"}),
		gatewayApplyDur: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gateway_apply_duration_seconds",
			Help:    "Gateway apply latency grouped by result",
			Buckets: prometheus.DefBuckets,
		}, []string{"result"}),
		gatewayGuardrail: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_guardrail_reject_total",
			Help: "Gateway guardrail rejects grouped by reason",
		}, []string{"reason"}),
		gatewayReplay: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_replay_reject_total",
			Help: "Gateway replay rejects grouped by reason",
		}, []string{"reason"}),
		gatewayTenantReject: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "gateway_tenant_reject_total",
			Help: "Gateway tenant boundary rejects",
		}),
		gatewayActiveRules: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gateway_active_rules",
			Help: "Active gateway rules tracked by enforcer",
		}),
		gatewayAckPublish: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_ack_publish_total",
			Help: "Gateway ACK enqueue/publish outcomes grouped by status",
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
		r.kafkaConsumed,
		r.kafkaErrors,
		r.kafkaPartitions,
		r.kafkaPartitionEvent,
		r.kafkaQueueSaturated,
		r.kafkaDispatchWait,
		r.publishToConsume,
		r.consumeToApply,
		r.consumeToDone,
		r.applyPersist,
		r.applyToAckEnqueue,
		r.offsetMark,
		r.duplicatesByScope,
		r.ackPublish,
		r.ackRetry,
		r.ackQueueDepth,
		r.ackLatency,
		r.ackQueueFailures,
		r.ackQueueBlocked,
		r.ackBatchSize,
		r.ackBatchFlush,
		r.gatewayTranslation,
		r.gatewayApplyTotal,
		r.gatewayApplyDur,
		r.gatewayGuardrail,
		r.gatewayReplay,
		r.gatewayTenantReject,
		r.gatewayActiveRules,
		r.gatewayAckPublish,
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

// ObserveKafkaConsumed increments consumer message counters by partition and result.
func (r *Recorder) ObserveKafkaConsumed(partition int32, result string) {
	if r == nil || r.kafkaConsumed == nil {
		return
	}
	if result == "" {
		result = "unknown"
	}
	r.kafkaConsumed.WithLabelValues(fmt.Sprintf("%d", partition), result).Inc()
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

// ObservePartitionAssigned marks a Kafka partition as owned by this process.
func (r *Recorder) ObservePartitionAssigned(partition int32) {
	if r == nil {
		return
	}
	label := fmt.Sprintf("%d", partition)
	if r.kafkaPartitions != nil {
		r.kafkaPartitions.WithLabelValues(label).Set(1)
	}
	if r.kafkaPartitionEvent != nil {
		r.kafkaPartitionEvent.WithLabelValues(label, "assigned").Inc()
	}
}

// ObservePartitionRevoked marks a Kafka partition as no longer owned by this process.
func (r *Recorder) ObservePartitionRevoked(partition int32) {
	if r == nil {
		return
	}
	label := fmt.Sprintf("%d", partition)
	if r.kafkaPartitions != nil {
		r.kafkaPartitions.WithLabelValues(label).Set(0)
	}
	if r.kafkaPartitionEvent != nil {
		r.kafkaPartitionEvent.WithLabelValues(label, "revoked").Inc()
	}
}

// ObserveKafkaQueueSaturated increments consumer dispatch saturation counter.
func (r *Recorder) ObserveKafkaQueueSaturated() {
	if r == nil || r.kafkaQueueSaturated == nil {
		return
	}
	r.kafkaQueueSaturated.Inc()
}

// ObserveKafkaDispatchWait records dispatch wait latency before worker enqueue.
func (r *Recorder) ObserveKafkaDispatchWait(d time.Duration) {
	if r == nil || r.kafkaDispatchWait == nil || d < 0 {
		return
	}
	r.kafkaDispatchWait.Observe(d.Seconds())
}

// ObservePublishToConsume records Kafka publish-to-consume latency.
func (r *Recorder) ObservePublishToConsume(seconds float64) {
	if r == nil || r.publishToConsume == nil || seconds < 0 {
		return
	}
	r.publishToConsume.Observe(seconds)
}

// ObserveConsumeToApply records enforcement consume-to-apply latency.
func (r *Recorder) ObserveConsumeToApply(result string, d time.Duration) {
	if r == nil || r.consumeToApply == nil || d < 0 {
		return
	}
	if result == "" {
		result = "unknown"
	}
	r.consumeToApply.WithLabelValues(result).Observe(d.Seconds())
}

// ObserveConsumeToDone records end-to-end handler latency from consume to return.
func (r *Recorder) ObserveConsumeToDone(result string, d time.Duration) {
	if r == nil || r.consumeToDone == nil || d < 0 {
		return
	}
	if result == "" {
		result = "unknown"
	}
	r.consumeToDone.WithLabelValues(result).Observe(d.Seconds())
}

// ObserveApplyPersist records local state persistence latency after apply.
func (r *Recorder) ObserveApplyPersist(result string, d time.Duration) {
	if r == nil || r.applyPersist == nil || d < 0 {
		return
	}
	if result == "" {
		result = "unknown"
	}
	r.applyPersist.WithLabelValues(result).Observe(d.Seconds())
}

// ObserveApplyToAckEnqueue records latency from apply completion to ACK enqueue result.
func (r *Recorder) ObserveApplyToAckEnqueue(result string, d time.Duration) {
	if r == nil || r.applyToAckEnqueue == nil || d < 0 {
		return
	}
	if result == "" {
		result = "unknown"
	}
	r.applyToAckEnqueue.WithLabelValues(result).Observe(d.Seconds())
}

// ObserveOffsetMark records latency to mark a consumed message for commit.
func (r *Recorder) ObserveOffsetMark(d time.Duration) {
	if r == nil || r.offsetMark == nil || d < 0 {
		return
	}
	r.offsetMark.Observe(d.Seconds())
}

// ObserveDuplicateScope records duplicate/no-op deliveries by scope kind.
func (r *Recorder) ObserveDuplicateScope(scopeKind string) {
	if r == nil || r.duplicatesByScope == nil {
		return
	}
	if scopeKind == "" {
		scopeKind = "unknown"
	}
	r.duplicatesByScope.WithLabelValues(scopeKind).Inc()
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

func (r *Recorder) isGatewayBackend() bool {
	return strings.EqualFold(strings.TrimSpace(r.backend), "gateway")
}

// ObserveGatewayTranslation records gateway translation outcomes.
func (r *Recorder) ObserveGatewayTranslation(status string) {
	if r == nil || r.gatewayTranslation == nil || !r.isGatewayBackend() {
		return
	}
	if status == "" {
		status = "unknown"
	}
	r.gatewayTranslation.WithLabelValues(status).Inc()
}

// ObserveGatewayApplySuccess records successful gateway apply operations.
func (r *Recorder) ObserveGatewayApplySuccess(d time.Duration) {
	if r == nil || !r.isGatewayBackend() {
		return
	}
	if r.gatewayApplyTotal != nil {
		r.gatewayApplyTotal.WithLabelValues("ok").Inc()
	}
	if r.gatewayApplyDur != nil {
		r.gatewayApplyDur.WithLabelValues("success").Observe(d.Seconds())
	}
}

// ObserveGatewayApplyFailure records failed gateway apply operations.
func (r *Recorder) ObserveGatewayApplyFailure(reason string, d time.Duration) {
	if r == nil || !r.isGatewayBackend() {
		return
	}
	if reason == "" {
		reason = "unknown"
	}
	if r.gatewayApplyTotal != nil {
		r.gatewayApplyTotal.WithLabelValues(reason).Inc()
	}
	if r.gatewayApplyDur != nil {
		r.gatewayApplyDur.WithLabelValues("error").Observe(d.Seconds())
	}
}

// ObserveGatewayGuardrailReject records guardrail rejections in gateway backend.
func (r *Recorder) ObserveGatewayGuardrailReject(reason string) {
	if r == nil || r.gatewayGuardrail == nil || !r.isGatewayBackend() {
		return
	}
	if reason == "" {
		reason = "unknown"
	}
	r.gatewayGuardrail.WithLabelValues(reason).Inc()
}

// ObserveGatewayReplayReject records replay-security rejects.
func (r *Recorder) ObserveGatewayReplayReject(reason string) {
	if r == nil || r.gatewayReplay == nil || !r.isGatewayBackend() {
		return
	}
	if reason == "" {
		reason = "unknown"
	}
	r.gatewayReplay.WithLabelValues(reason).Inc()
}

// ObserveGatewayTenantReject increments tenant-boundary reject counter.
func (r *Recorder) ObserveGatewayTenantReject() {
	if r == nil || r.gatewayTenantReject == nil || !r.isGatewayBackend() {
		return
	}
	r.gatewayTenantReject.Inc()
}

// SetGatewayActiveRules sets active gateway rule gauge.
func (r *Recorder) SetGatewayActiveRules(count int) {
	if r == nil || r.gatewayActiveRules == nil || !r.isGatewayBackend() {
		return
	}
	r.gatewayActiveRules.Set(float64(count))
}

// ObserveGatewayAckPublish records gateway ACK path outcomes.
func (r *Recorder) ObserveGatewayAckPublish(status string) {
	if r == nil || r.gatewayAckPublish == nil || !r.isGatewayBackend() {
		return
	}
	if status == "" {
		status = "unknown"
	}
	r.gatewayAckPublish.WithLabelValues(status).Inc()
}

// KafkaLagGauge exposes the consumer lag gauge (used in tests).
func (r *Recorder) KafkaLagGauge() *prometheus.GaugeVec { return r.kafkaLag }

// KafkaConsumedCounter exposes Kafka consumed counters for tests.
func (r *Recorder) KafkaConsumedCounter() *prometheus.CounterVec { return r.kafkaConsumed }

// KafkaPartitionGauge exposes partition ownership gauge (used in tests).
func (r *Recorder) KafkaPartitionGauge() *prometheus.GaugeVec { return r.kafkaPartitions }

// KafkaPartitionEventCounter exposes partition lifecycle counters (used in tests).
func (r *Recorder) KafkaPartitionEventCounter() *prometheus.CounterVec { return r.kafkaPartitionEvent }

// KafkaQueueSaturatedCounter exposes queue saturation counter for tests.
func (r *Recorder) KafkaQueueSaturatedCounter() prometheus.Counter { return r.kafkaQueueSaturated }

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

// GatewayGuardrailCounter exposes gateway guardrail counters for tests.
func (r *Recorder) GatewayGuardrailCounter() *prometheus.CounterVec { return r.gatewayGuardrail }

// GatewayReplayCounter exposes gateway replay counters for tests.
func (r *Recorder) GatewayReplayCounter() *prometheus.CounterVec { return r.gatewayReplay }

// GatewayApplyCounter exposes gateway apply counters for tests.
func (r *Recorder) GatewayApplyCounter() *prometheus.CounterVec { return r.gatewayApplyTotal }

// GatewayTranslationCounter exposes gateway translation counters for tests.
func (r *Recorder) GatewayTranslationCounter() *prometheus.CounterVec { return r.gatewayTranslation }

// GatewayAckCounter exposes gateway ACK counters for tests.
func (r *Recorder) GatewayAckCounter() *prometheus.CounterVec { return r.gatewayAckPublish }
