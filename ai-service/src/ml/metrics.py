"""
Prometheus metrics for ML detection pipeline.

Tracks:
- Latency per stage (histograms)
- Detection counts (counters)
- Confidence/LLR distributions (histograms)
- Engine health (gauges)

No secrets in metrics - security-first.
"""

from prometheus_client import Histogram, Counter, Gauge
from typing import Dict
from .types import EnsembleDecision


class PrometheusMetrics:
    """
    Prometheus metrics collection for ML pipeline.
    
    All metrics prefixed with 'ml_' to avoid collisions.
    """
    
    def __init__(self):
        """Initialize all Prometheus metrics."""
        
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
