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
	ingested                     prometheus.Counter
	validated                    prometheus.Counter
	rejected                     prometheus.Counter
	applied                      prometheus.Counter
	removed                      prometheus.Counter
	applyDuration                *prometheus.HistogramVec
	reconciled                   prometheus.Counter
	reconcileErrs                prometheus.Counter
	reconcileDur                 *prometheus.HistogramVec
	guardrail                    *prometheus.CounterVec
	fastPathEligibility          *prometheus.CounterVec
	backendApplied               *prometheus.CounterVec
	activeGauge                  prometheus.Gauge
	killSwitch                   prometheus.Gauge
	backoffGauge                 *prometheus.GaugeVec
	ledgerDrift                  *prometheus.CounterVec
	kafkaLag                     *prometheus.GaugeVec
	kafkaConsumed                *prometheus.CounterVec
	consumeTotal                 *prometheus.CounterVec
	kafkaErrors                  *prometheus.CounterVec
	kafkaPartitions              *prometheus.GaugeVec
	kafkaPartitionEvent          *prometheus.CounterVec
	kafkaRebalance               *prometheus.CounterVec
	rebalanceTotal               prometheus.Counter
	kafkaQueueSaturated          prometheus.Counter
	queueSaturatedTotal          prometheus.Counter
	kafkaDispatchWait            prometheus.Histogram
	dispatchWaitMs               prometheus.Histogram
	kafkaDispatchBudgetExhausted prometheus.Counter
	kafkaStripeStall             *prometheus.CounterVec
	kafkaStripeDistressOpen      *prometheus.CounterVec
	stripeDistressOpenTotal      *prometheus.CounterVec
	kafkaStripeDistressActive    *prometheus.GaugeVec
	stripeDistressActive         *prometheus.GaugeVec
	kafkaStripeInflight          *prometheus.GaugeVec
	kafkaStripeOldest            *prometheus.GaugeVec
	publishToConsume             prometheus.Histogram
	consumeToApply               *prometheus.HistogramVec
	consumeToDone                *prometheus.HistogramVec
	applyPersist                 *prometheus.HistogramVec
	applyToAckEnqueue            *prometheus.HistogramVec
	applyRetry                   *prometheus.CounterVec
	retryTotal                   *prometheus.CounterVec
	timeoutTotal                 prometheus.Counter
	applyTerminal                *prometheus.CounterVec
	applyRetryExhausted          prometheus.Counter
	applyTimeoutBudgetExceeded   prometheus.Counter
	applyTimeoutBreakerOpen      prometheus.Counter
	applyTimeoutBreakerSkip      prometheus.Counter
	commandRejectPublish         *prometheus.CounterVec
	rejectPublishFail            prometheus.Counter
	policyShedding               *prometheus.CounterVec
	lifecycleCompaction          *prometheus.CounterVec
	kafkaAdmissionPause          prometheus.Counter
	laneQueueDepth               *prometheus.GaugeVec
	laneDispatchWait             *prometheus.HistogramVec
	laneStarvation               *prometheus.CounterVec
	offsetMark                   prometheus.Histogram
	duplicatesByScope            *prometheus.CounterVec
	ackPublish                   *prometheus.CounterVec
	ackRetry                     prometheus.Counter
	ackQueueDepth                prometheus.Gauge
	ackLatency                   prometheus.Histogram
	ackQueueFailures             prometheus.Counter
	ackQueueBlocked              prometheus.Counter
	ackBatchSize                 prometheus.Histogram
	ackBatchFlush                *prometheus.CounterVec
	gatewayTranslation           *prometheus.CounterVec
	gatewayApplyTotal            *prometheus.CounterVec
	gatewayApplyDur              *prometheus.HistogramVec
	gatewayGuardrail             *prometheus.CounterVec
	gatewayReplay                *prometheus.CounterVec
	gatewayTenantReject          prometheus.Counter
	gatewayActiveRules           prometheus.Gauge
	gatewayAckPublish            *prometheus.CounterVec
	backend                      string
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
		consumeTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "consume_total",
			Help: "Parity alias: total consumed messages grouped by result",
		}, []string{"result"}),
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
		kafkaRebalance: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_consumer_partition_rebalance_events_total",
			Help: "Kafka consumer partition assignment/revocation events grouped by event",
		}, []string{"event"}),
		rebalanceTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "rebalance_total",
			Help: "Parity alias: total partition assignment/revocation events",
		}),
		kafkaQueueSaturated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_consumer_queue_saturated_total",
			Help: "Number of times consumer worker queues were saturated and dispatch had to wait",
		}),
		queueSaturatedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "queue_saturated_total",
			Help: "Parity alias: total queue saturation events",
		}),
		kafkaDispatchWait: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "policy_consumer_dispatch_wait_seconds",
			Help:    "Latency waiting to dispatch a consumed message to a worker",
			Buckets: prometheus.DefBuckets,
		}),
		dispatchWaitMs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "dispatch_wait_ms",
			Help:    "Parity alias: dispatch wait latency in milliseconds",
			Buckets: []float64{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000},
		}),
		kafkaDispatchBudgetExhausted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_consumer_dispatch_budget_exhausted_total",
			Help: "Number of times dispatch max-wait budget was exhausted while queue remained saturated",
		}),
		kafkaStripeStall: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_consumer_stripe_stall_total",
			Help: "Number of stall detections where a stripe in-flight message exceeded stall threshold",
		}, []string{"stripe"}),
		kafkaStripeDistressOpen: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_consumer_stripe_distress_open_total",
			Help: "Number of times a stripe entered distress due to sustained saturation",
		}, []string{"stripe"}),
		stripeDistressOpenTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "stripe_distress_open_total",
			Help: "Parity alias: stripe distress openings grouped by stripe",
		}, []string{"stripe"}),
		kafkaStripeDistressActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "policy_consumer_stripe_distress_active",
			Help: "Whether a stripe is currently in distress state (1 active, 0 inactive)",
		}, []string{"stripe"}),
		stripeDistressActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "stripe_distress_active",
			Help: "Parity alias: stripe distress state grouped by stripe (1 active, 0 inactive)",
		}, []string{"stripe"}),
		kafkaStripeInflight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "policy_consumer_stripe_inflight",
			Help: "Current in-flight message count per stripe (0/1 for striped worker model)",
		}, []string{"stripe"}),
		kafkaStripeOldest: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "policy_consumer_stripe_oldest_inflight_seconds",
			Help: "Age in seconds of the oldest in-flight message per stripe",
		}, []string{"stripe"}),
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
		applyRetry: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_apply_retry_total",
			Help: "Apply retry attempts grouped by reason class",
		}, []string{"reason"}),
		retryTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "retry_total",
			Help: "Parity alias: total retries grouped by reason",
		}, []string{"reason"}),
		timeoutTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "timeout_total",
			Help: "Parity alias: total timeout events observed",
		}),
		applyTerminal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_apply_terminal_failure_total",
			Help: "Terminal apply failures grouped by reason",
		}, []string{"reason"}),
		applyRetryExhausted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_apply_retry_exhausted_total",
			Help: "Total number of apply operations that exhausted retries",
		}),
		applyTimeoutBudgetExceeded: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_apply_timeout_budget_exceeded_total",
			Help: "Total number of apply attempts that exceeded per-attempt timeout budget",
		}),
		applyTimeoutBreakerOpen: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_apply_timeout_breaker_open_total",
			Help: "Total number of times apply timeout breaker opened",
		}),
		applyTimeoutBreakerSkip: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_apply_timeout_breaker_skip_total",
			Help: "Total number of apply attempts skipped due to open timeout breaker",
		}),
		commandRejectPublish: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_command_reject_publish_total",
			Help: "Command reject artifact publish outcomes grouped by result",
		}, []string{"result"}),
		rejectPublishFail: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_reject_publish_fail_total",
			Help: "Total number of terminal paths where reject artifact publish failed",
		}),
		policyShedding: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_shedding_total",
			Help: "Alert-level counter for policy shedding paths grouped by reason",
		}, []string{"reason"}),
		lifecycleCompaction: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "policy_lifecycle_compaction_total",
			Help: "Lifecycle compaction outcomes grouped by result",
		}, []string{"result"}),
		kafkaAdmissionPause: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "policy_consumer_admission_pause_total",
			Help: "Number of admission-shaping pauses applied in consumer read path",
		}),
		laneQueueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lane_queue_depth",
			Help: "Current queued requests waiting for execution slots by lane",
		}, []string{"lane"}),
		laneDispatchWait: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lane_dispatch_wait_ms",
			Help:    "Time spent waiting for lane execution slot in milliseconds",
			Buckets: []float64{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000},
		}, []string{"lane"}),
		laneStarvation: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lane_starvation_total",
			Help: "Number of lane wait events that exceeded starvation threshold",
		}, []string{"lane"}),
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
		r.consumeTotal,
		r.kafkaErrors,
		r.kafkaPartitions,
		r.kafkaPartitionEvent,
		r.kafkaRebalance,
		r.rebalanceTotal,
		r.kafkaQueueSaturated,
		r.queueSaturatedTotal,
		r.kafkaDispatchWait,
		r.dispatchWaitMs,
		r.kafkaDispatchBudgetExhausted,
		r.kafkaStripeStall,
		r.kafkaStripeDistressOpen,
		r.stripeDistressOpenTotal,
		r.kafkaStripeDistressActive,
		r.stripeDistressActive,
		r.kafkaStripeInflight,
		r.kafkaStripeOldest,
		r.publishToConsume,
		r.consumeToApply,
		r.consumeToDone,
		r.applyPersist,
		r.applyToAckEnqueue,
		r.applyRetry,
		r.retryTotal,
		r.timeoutTotal,
		r.applyTerminal,
		r.applyRetryExhausted,
		r.applyTimeoutBudgetExceeded,
		r.applyTimeoutBreakerOpen,
		r.applyTimeoutBreakerSkip,
		r.commandRejectPublish,
		r.rejectPublishFail,
		r.policyShedding,
		r.lifecycleCompaction,
		r.kafkaAdmissionPause,
		r.laneQueueDepth,
		r.laneDispatchWait,
		r.laneStarvation,
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
	if r.consumeTotal != nil {
		r.consumeTotal.WithLabelValues(result).Inc()
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
	if r.kafkaRebalance != nil {
		r.kafkaRebalance.WithLabelValues("assigned").Inc()
	}
	if r.rebalanceTotal != nil {
		r.rebalanceTotal.Inc()
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
	if r.kafkaRebalance != nil {
		r.kafkaRebalance.WithLabelValues("revoked").Inc()
	}
	if r.rebalanceTotal != nil {
		r.rebalanceTotal.Inc()
	}
}

// ObserveKafkaQueueSaturated increments consumer dispatch saturation counter.
func (r *Recorder) ObserveKafkaQueueSaturated() {
	if r == nil || r.kafkaQueueSaturated == nil {
		return
	}
	r.kafkaQueueSaturated.Inc()
	if r.queueSaturatedTotal != nil {
		r.queueSaturatedTotal.Inc()
	}
}

// ObserveKafkaDispatchWait records dispatch wait latency before worker enqueue.
func (r *Recorder) ObserveKafkaDispatchWait(d time.Duration) {
	if r == nil || r.kafkaDispatchWait == nil || d < 0 {
		return
	}
	r.kafkaDispatchWait.Observe(d.Seconds())
	if r.dispatchWaitMs != nil {
		r.dispatchWaitMs.Observe(float64(d) / float64(time.Millisecond))
	}
}

// ObserveKafkaDispatchBudgetExhausted increments dispatch budget exhaustion counter.
func (r *Recorder) ObserveKafkaDispatchBudgetExhausted() {
	if r == nil || r.kafkaDispatchBudgetExhausted == nil {
		return
	}
	r.kafkaDispatchBudgetExhausted.Inc()
}

// ObserveKafkaAdmissionPause increments admission shaping pause counter.
func (r *Recorder) ObserveKafkaAdmissionPause() {
	if r == nil || r.kafkaAdmissionPause == nil {
		return
	}
	r.kafkaAdmissionPause.Inc()
}

// SetLaneQueueDepth sets queued depth for execution lane.
func (r *Recorder) SetLaneQueueDepth(lane string, depth int64) {
	if r == nil || r.laneQueueDepth == nil {
		return
	}
	if lane == "" {
		lane = "unknown"
	}
	if depth < 0 {
		depth = 0
	}
	r.laneQueueDepth.WithLabelValues(lane).Set(float64(depth))
}

// ObserveLaneDispatchWait records lane slot wait duration in milliseconds.
func (r *Recorder) ObserveLaneDispatchWait(lane string, d time.Duration) {
	if r == nil || r.laneDispatchWait == nil || d < 0 {
		return
	}
	if lane == "" {
		lane = "unknown"
	}
	r.laneDispatchWait.WithLabelValues(lane).Observe(float64(d) / float64(time.Millisecond))
}

// ObserveLaneStarvation increments starvation counter for lane.
func (r *Recorder) ObserveLaneStarvation(lane string) {
	if r == nil || r.laneStarvation == nil {
		return
	}
	if lane == "" {
		lane = "unknown"
	}
	r.laneStarvation.WithLabelValues(lane).Inc()
}

// ObserveKafkaStripeStall increments stripe stall detection counter.
func (r *Recorder) ObserveKafkaStripeStall(stripe int) {
	if r == nil || r.kafkaStripeStall == nil {
		return
	}
	r.kafkaStripeStall.WithLabelValues(fmt.Sprintf("%d", stripe)).Inc()
}

// ObserveKafkaStripeDistressOpen increments distress-open counter for a stripe.
func (r *Recorder) ObserveKafkaStripeDistressOpen(stripe int) {
	if r == nil || r.kafkaStripeDistressOpen == nil {
		return
	}
	label := fmt.Sprintf("%d", stripe)
	r.kafkaStripeDistressOpen.WithLabelValues(label).Inc()
	if r.stripeDistressOpenTotal != nil {
		r.stripeDistressOpenTotal.WithLabelValues(label).Inc()
	}
}

// SetKafkaStripeDistressActive sets distress-active state for a stripe.
func (r *Recorder) SetKafkaStripeDistressActive(stripe int, active bool) {
	if r == nil || r.kafkaStripeDistressActive == nil {
		return
	}
	label := fmt.Sprintf("%d", stripe)
	v := 0.0
	if active {
		v = 1
	}
	r.kafkaStripeDistressActive.WithLabelValues(label).Set(v)
	if r.stripeDistressActive != nil {
		r.stripeDistressActive.WithLabelValues(label).Set(v)
	}
}

// ObserveKafkaStripeInflight sets per-stripe in-flight gauge.
func (r *Recorder) ObserveKafkaStripeInflight(stripe int, inflight int) {
	if r == nil || r.kafkaStripeInflight == nil {
		return
	}
	r.kafkaStripeInflight.WithLabelValues(fmt.Sprintf("%d", stripe)).Set(float64(inflight))
}

// ObserveKafkaStripeOldestInflight sets per-stripe oldest in-flight age gauge.
func (r *Recorder) ObserveKafkaStripeOldestInflight(stripe int, age time.Duration) {
	if r == nil || r.kafkaStripeOldest == nil || age < 0 {
		return
	}
	r.kafkaStripeOldest.WithLabelValues(fmt.Sprintf("%d", stripe)).Set(age.Seconds())
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

// ObserveApplyRetry increments apply retry counter grouped by reason.
func (r *Recorder) ObserveApplyRetry(reason string) {
	if r == nil || r.applyRetry == nil {
		return
	}
	if reason == "" {
		reason = "unknown"
	}
	r.applyRetry.WithLabelValues(reason).Inc()
	if r.retryTotal != nil {
		r.retryTotal.WithLabelValues(reason).Inc()
	}
	if reason == "timeout" && r.timeoutTotal != nil {
		r.timeoutTotal.Inc()
	}
}

// ObserveApplyTerminalFailure increments terminal apply failure counter.
func (r *Recorder) ObserveApplyTerminalFailure(reason string) {
	if r == nil || r.applyTerminal == nil {
		return
	}
	if reason == "" {
		reason = "unknown"
	}
	r.applyTerminal.WithLabelValues(reason).Inc()
}

// ObserveApplyRetryExhausted increments retry exhausted counter.
func (r *Recorder) ObserveApplyRetryExhausted() {
	if r == nil || r.applyRetryExhausted == nil {
		return
	}
	r.applyRetryExhausted.Inc()
}

// ObserveApplyTimeoutBudgetExceeded increments timeout budget exceeded counter.
func (r *Recorder) ObserveApplyTimeoutBudgetExceeded() {
	if r == nil || r.applyTimeoutBudgetExceeded == nil {
		return
	}
	r.applyTimeoutBudgetExceeded.Inc()
}

// ObserveApplyTimeoutBreakerOpen increments breaker-open counter.
func (r *Recorder) ObserveApplyTimeoutBreakerOpen() {
	if r == nil || r.applyTimeoutBreakerOpen == nil {
		return
	}
	r.applyTimeoutBreakerOpen.Inc()
}

// ObserveApplyTimeoutBreakerSkip increments breaker-skip counter.
func (r *Recorder) ObserveApplyTimeoutBreakerSkip() {
	if r == nil || r.applyTimeoutBreakerSkip == nil {
		return
	}
	r.applyTimeoutBreakerSkip.Inc()
}

// ObserveCommandRejectPublish records command reject artifact publish result.
func (r *Recorder) ObserveCommandRejectPublish(result string) {
	if r == nil || r.commandRejectPublish == nil {
		return
	}
	if result == "" {
		result = "unknown"
	}
	r.commandRejectPublish.WithLabelValues(result).Inc()
}

// ObserveRejectPublishFail increments reject publish failure counter.
func (r *Recorder) ObserveRejectPublishFail() {
	if r == nil || r.rejectPublishFail == nil {
		return
	}
	r.rejectPublishFail.Inc()
}

// ObservePolicyShedding increments alert-level shedding counter.
func (r *Recorder) ObservePolicyShedding(reason string) {
	if r == nil || r.policyShedding == nil {
		return
	}
	if reason == "" {
		reason = "unknown"
	}
	r.policyShedding.WithLabelValues(reason).Inc()
}

// ObserveLifecycleCompaction increments lifecycle compaction counters.
func (r *Recorder) ObserveLifecycleCompaction(result string) {
	if r == nil || r.lifecycleCompaction == nil {
		return
	}
	if result == "" {
		result = "unknown"
	}
	r.lifecycleCompaction.WithLabelValues(result).Inc()
}

// ObserveTimeout increments timeout parity counter.
func (r *Recorder) ObserveTimeout() {
	if r == nil || r.timeoutTotal == nil {
		return
	}
	r.timeoutTotal.Inc()
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

// KafkaRebalanceCounter exposes low-cardinality rebalance counters for tests/alerts.
func (r *Recorder) KafkaRebalanceCounter() *prometheus.CounterVec { return r.kafkaRebalance }

// KafkaQueueSaturatedCounter exposes queue saturation counter for tests.
func (r *Recorder) KafkaQueueSaturatedCounter() prometheus.Counter { return r.kafkaQueueSaturated }

// KafkaAdmissionPauseCounter exposes admission pause counter for tests.
func (r *Recorder) KafkaAdmissionPauseCounter() prometheus.Counter { return r.kafkaAdmissionPause }

// KafkaDispatchBudgetExhaustedCounter exposes dispatch budget exhaustion counter for tests.
func (r *Recorder) KafkaDispatchBudgetExhaustedCounter() prometheus.Counter {
	return r.kafkaDispatchBudgetExhausted
}

// KafkaStripeStallCounter exposes stripe stall counter for tests.
func (r *Recorder) KafkaStripeStallCounter() *prometheus.CounterVec { return r.kafkaStripeStall }

// KafkaStripeDistressOpenCounter exposes distress-open counters for tests.
func (r *Recorder) KafkaStripeDistressOpenCounter() *prometheus.CounterVec {
	return r.kafkaStripeDistressOpen
}

// KafkaStripeDistressActiveGauge exposes distress-active gauges for tests.
func (r *Recorder) KafkaStripeDistressActiveGauge() *prometheus.GaugeVec {
	return r.kafkaStripeDistressActive
}

// KafkaStripeInflightGauge exposes per-stripe inflight gauges for tests.
func (r *Recorder) KafkaStripeInflightGauge() *prometheus.GaugeVec { return r.kafkaStripeInflight }

// KafkaStripeOldestInflightGauge exposes per-stripe oldest-inflight gauges for tests.
func (r *Recorder) KafkaStripeOldestInflightGauge() *prometheus.GaugeVec { return r.kafkaStripeOldest }

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
