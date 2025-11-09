"""
Prometheus metrics for ML detection pipeline.

Tracks:
- Latency per stage (histograms)
- Detection counts (counters)
- Confidence/LLR distributions (histograms)
- Engine health (gauges)

No secrets in metrics - security-first.
"""

import time

from prometheus_client import Histogram, Counter, Gauge
from typing import Dict, Optional
from .types import EnsembleDecision


class PrometheusMetrics:
    """
    Prometheus metrics collection for ML pipeline.
    
    All metrics prefixed with 'ml_' to avoid collisions.
    """

    _shared_metrics: Dict[str, object] = {}
    _initialized: bool = False
    
    def __init__(self):
        """Initialize all Prometheus metrics."""

        if PrometheusMetrics._initialized:
            for name, metric in PrometheusMetrics._shared_metrics.items():
                setattr(self, name, metric)
            return
        
        # Latency histograms (milliseconds)
        self.telemetry_latency = Histogram(
            'ml_telemetry_load_ms',
            'Telemetry loading latency in milliseconds',
            buckets=[1, 5, 10, 25, 50, 100, 250]
        )
        
        self.feature_latency = Histogram(
            'ml_feature_extraction_ms',
            'Feature extraction latency in milliseconds',
            buckets=[1, 5, 10, 25, 50, 100, 250]
        )
        
        self.engine_latency = Histogram(
            'ml_engine_inference_ms',
            'Engine inference latency in milliseconds',
            buckets=[1, 5, 10, 25, 50, 100, 250]
        )
        
        self.ensemble_latency = Histogram(
            'ml_ensemble_vote_ms',
            'Ensemble voting latency in milliseconds',
            buckets=[1, 2, 5, 10, 25, 50]
        )
        
        self.evidence_latency = Histogram(
            'ml_evidence_generation_ms',
            'Evidence generation latency in milliseconds',
            buckets=[1, 2, 5, 10, 25, 50]
        )
        
        self.total_latency = Histogram(
            'ml_pipeline_total_ms',
            'Total pipeline latency in milliseconds',
            buckets=[10, 25, 50, 100, 200, 500, 1000]
        )
        
        # Detection counters
        self.detections = Counter(
            'ml_detections_total',
            'Total detections by threat type and decision',
            ['threat_type', 'decision']
        )
        
        self.abstentions = Counter(
            'ml_abstentions_total',
            'Abstentions by reason',
            ['reason']
        )
        
        self.pipeline_runs = Counter(
            'ml_pipeline_runs_total',
            'Total pipeline runs',
            ['status']  # 'success', 'error'
        )

        # Detection loop metrics
        self.detection_loop_running = Gauge(
            'ml_detection_loop_running',
            'Detection loop running status (1=running, 0=stopped)'
        )

        self.detection_loop_last_iteration = Gauge(
            'ml_detection_loop_last_iteration_timestamp',
            'Unix timestamp of the last detection loop iteration'
        )

        self.detection_loop_last_detection = Gauge(
            'ml_detection_loop_last_detection_timestamp',
            'Unix timestamp of the last published detection'
        )

        self.detection_iteration_latency = Histogram(
            'ml_detection_iteration_latency_ms',
            'Detection loop iteration latency in milliseconds',
            buckets=[5, 10, 25, 50, 100, 250, 500, 1000]
        )

        self.detection_decisions = Counter(
            'ml_detection_decisions_total',
            'Detection loop decisions by outcome',
            ['outcome']
        )

        self.detection_errors = Counter(
            'ml_detection_errors_total',
            'Detection loop errors'
        )

        self.telemetry_connect_latency = Histogram(
            'ml_telemetry_connect_ms',
            'PostgreSQL connect latency in milliseconds',
            buckets=[1, 5, 10, 25, 50, 100, 250, 500, 1000]
        )

        self.telemetry_query_latency = Histogram(
            'ml_telemetry_query_ms',
            'PostgreSQL query latency in milliseconds',
            buckets=[1, 5, 10, 25, 50, 100, 250, 500, 1000]
        )

        self.telemetry_fetch_latency = Histogram(
            'ml_telemetry_fetch_ms',
            'PostgreSQL fetch latency in milliseconds',
            buckets=[1, 5, 10, 25, 50, 100, 250, 500, 1000]
        )

        self.telemetry_convert_latency = Histogram(
            'ml_telemetry_convert_ms',
            'Telemetry conversion latency in milliseconds',
            buckets=[1, 5, 10, 25, 50, 100, 250, 500, 1000]
        )

        self.telemetry_total_latency = Histogram(
            'ml_telemetry_total_ms',
            'Total telemetry pipeline latency in milliseconds',
            buckets=[5, 10, 25, 50, 100, 250, 500, 1000, 2000]
        )

        self.engine_candidates = Counter(
            'ml_engine_candidates_total',
            'Detection candidates produced per engine',
            ['engine_type']
        )

        self.engine_published = Counter(
            'ml_engine_published_total',
            'Published decisions where engine contributed',
            ['engine_type']
        )

        self.engine_confidence_sum = Counter(
            'ml_engine_confidence_sum',
            'Aggregate confidence sum per engine',
            ['engine_type']
        )

        self.engine_confidence_count = Counter(
            'ml_engine_confidence_count',
            'Number of confidence samples per engine',
            ['engine_type']
        )
        
        # Quality metrics
        self.confidence_dist = Histogram(
            'ml_confidence_distribution',
            'Detection confidence distribution',
            buckets=[0.5, 0.6, 0.7, 0.75, 0.8, 0.85, 0.9, 0.95, 0.99]
        )
        
        self.llr_dist = Histogram(
            'ml_llr_distribution',
            'Log-likelihood ratio distribution',
            buckets=[-10, -5, -2, -1, 0, 1, 2, 5, 10, 20]
        )
        
        self.quality_score_dist = Histogram(
            'ml_evidence_quality_distribution',
            'Evidence quality score distribution',
            buckets=[0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0]
        )
        
        # Engine health gauges
        self.engine_ready = Gauge(
            'ml_engine_ready',
            'Engine ready status (1=ready, 0=not ready)',
            ['engine_type']
        )
        
        self.candidate_count = Histogram(
            'ml_candidates_per_run',
            'Number of detection candidates per pipeline run',
            buckets=[0, 1, 2, 3, 5, 10, 20, 50]
        )
        
        self.feature_count = Histogram(
            'ml_features_extracted',
            'Number of features extracted per run',
            buckets=[10, 30, 50, 100, 300, 500, 1000]
        )

        # Variant-level metrics (Phase C)
        self.variant_inference_latency = Histogram(
            'ml_variant_inference_ms',
            'Inference latency per malware variant',
            ['variant'],
            buckets=[0.5, 1, 2, 5, 10, 25, 50, 100]
        )

        self.variant_inferences = Counter(
            'ml_variant_inferences_total',
            'Total variant inferences by outcome',
            ['variant', 'outcome']  # outcome: ok|threshold|dlq|error
        )

        self.variant_dlq = Counter(
            'ml_variant_dlq_total',
            'DLQ actions for variant validation failures',
            ['variant', 'reason']
        )

        PrometheusMetrics._shared_metrics = {
            'telemetry_latency': self.telemetry_latency,
            'feature_latency': self.feature_latency,
            'engine_latency': self.engine_latency,
            'ensemble_latency': self.ensemble_latency,
            'evidence_latency': self.evidence_latency,
            'total_latency': self.total_latency,
            'detections': self.detections,
            'abstentions': self.abstentions,
            'pipeline_runs': self.pipeline_runs,
            'detection_loop_running': self.detection_loop_running,
            'detection_loop_last_iteration': self.detection_loop_last_iteration,
            'detection_loop_last_detection': self.detection_loop_last_detection,
            'detection_iteration_latency': self.detection_iteration_latency,
            'detection_decisions': self.detection_decisions,
            'detection_errors': self.detection_errors,
            'telemetry_connect_latency': self.telemetry_connect_latency,
            'telemetry_query_latency': self.telemetry_query_latency,
            'telemetry_fetch_latency': self.telemetry_fetch_latency,
            'telemetry_convert_latency': self.telemetry_convert_latency,
            'telemetry_total_latency': self.telemetry_total_latency,
            'engine_candidates': self.engine_candidates,
            'engine_published': self.engine_published,
            'engine_confidence_sum': self.engine_confidence_sum,
            'engine_confidence_count': self.engine_confidence_count,
            'confidence_dist': self.confidence_dist,
            'llr_dist': self.llr_dist,
            'quality_score_dist': self.quality_score_dist,
            'engine_ready': self.engine_ready,
            'candidate_count': self.candidate_count,
            'feature_count': self.feature_count,
            'variant_inference_latency': self.variant_inference_latency,
            'variant_inferences': self.variant_inferences,
            'variant_dlq': self.variant_dlq,
        }

        PrometheusMetrics._initialized = True
    
    def record_pipeline_result(
        self,
        decision: EnsembleDecision,
        latencies: Dict[str, float],
        total_latency: float,
        feature_count: int = 0,
        candidate_count: int = 0
    ):
        """
        Record full pipeline execution metrics.
        
        Args:
            decision: Ensemble decision (or None if error)
            latencies: Dictionary of per-stage latencies
            total_latency: Total pipeline latency in ms
            feature_count: Number of features extracted
            candidate_count: Number of detection candidates
        """
        # Record latencies
        if 'telemetry_load' in latencies:
            self.telemetry_latency.observe(latencies['telemetry_load'])
        
        if 'feature_extraction' in latencies:
            self.feature_latency.observe(latencies['feature_extraction'])
        
        if 'engine_inference' in latencies:
            self.engine_latency.observe(latencies['engine_inference'])
        
        if 'ensemble_vote' in latencies:
            self.ensemble_latency.observe(latencies['ensemble_vote'])
        
        if 'evidence_generation' in latencies:
            self.evidence_latency.observe(latencies['evidence_generation'])
        
        self.total_latency.observe(total_latency)
        
        # Record decision
        if decision:
            if decision.should_publish:
                self.detections.labels(
                    threat_type=decision.threat_type.value,
                    decision='publish'
                ).inc()
                
                # Record quality metrics
                self.confidence_dist.observe(decision.confidence)
                self.llr_dist.observe(decision.llr)
            else:
                # Parse abstention reason to get category
                reason_category = decision.abstention_reason.split('_')[0] if decision.abstention_reason else 'unknown'
                self.abstentions.labels(reason=reason_category).inc()
        
        # Record counts
        if feature_count > 0:
            self.feature_count.observe(feature_count)
        
        if candidate_count > 0:
            self.candidate_count.observe(candidate_count)
        
        # Record pipeline run
        status = 'success' if decision else 'error'
        self.pipeline_runs.labels(status=status).inc()
    
    def record_engine_status(self, engine_type: str, is_ready: bool):
        """
        Record engine health status.
        
        Args:
            engine_type: Engine type ('ml', 'rules', 'math')
            is_ready: Whether engine is ready
        """
        self.engine_ready.labels(engine_type=engine_type).set(1 if is_ready else 0)
    
    def record_evidence_quality(self, quality_score: float):
        """
        Record evidence quality score.
        
        Args:
            quality_score: Quality score [0,1]
        """
        self.quality_score_dist.observe(quality_score)

    # Variant helpers
    def record_variant_latency(self, variant: str, ms: float):
        try:
            self.variant_inference_latency.labels(variant=variant).observe(ms)
        except Exception:
            pass

    def inc_variant(self, variant: str, outcome: str):
        try:
            self.variant_inferences.labels(variant=variant, outcome=outcome).inc()
        except Exception:
            pass

    def inc_variant_dlq(self, variant: str, reason: str):
        try:
            self.variant_dlq.labels(variant=variant, reason=reason).inc()
        except Exception:
            pass

    # Detection loop helpers
    def set_detection_loop_running(self, running: bool):
        try:
            self.detection_loop_running.set(1 if running else 0)
        except Exception:
            pass

    def record_detection_iteration(self, *, latency_ms: float, published: bool, rate_limited: bool):
        try:
            self.detection_iteration_latency.observe(latency_ms)
            self.detection_loop_last_iteration.set(time.time())

            if published:
                outcome = 'published'
                self.detection_loop_last_detection.set(time.time())
            elif rate_limited:
                outcome = 'rate_limited'
            else:
                outcome = 'no_publish'

            self.detection_decisions.labels(outcome=outcome).inc()
        except Exception:
            pass

    def record_detection_error(self):
        try:
            self.detection_errors.inc()
        except Exception:
            pass

    def record_engine_iteration(self, *, engine_type: str, candidates: int, published: int, confidence_sum: float):
        try:
            if candidates > 0:
                self.engine_candidates.labels(engine_type=engine_type).inc(candidates)
                self.engine_confidence_sum.labels(engine_type=engine_type).inc(confidence_sum)
                self.engine_confidence_count.labels(engine_type=engine_type).inc(candidates)
            if published > 0:
                self.engine_published.labels(engine_type=engine_type).inc(published)
        except Exception:
            pass

    def record_db_timings(
        self,
        *,
        connect_ms: Optional[float] = None,
        query_ms: Optional[float] = None,
        fetch_ms: Optional[float] = None,
        convert_ms: Optional[float] = None,
        total_ms: Optional[float] = None,
    ) -> None:
        try:
            if connect_ms is not None:
                self.telemetry_connect_latency.observe(connect_ms)
            if query_ms is not None:
                self.telemetry_query_latency.observe(query_ms)
            if fetch_ms is not None:
                self.telemetry_fetch_latency.observe(fetch_ms)
            if convert_ms is not None:
                self.telemetry_convert_latency.observe(convert_ms)
            if total_ms is not None:
                self.telemetry_total_latency.observe(total_ms)
        except Exception:
            pass
