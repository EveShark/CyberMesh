"""
Detection pipeline orchestrator with full instrumentation.

Flow: Telemetry → Features → Engines → Ensemble → Evidence
Instrumented: Per-stage latency tracking, error handling, metrics
"""

import time
from typing import Dict, List, Optional, TYPE_CHECKING

import numpy as np

from .telemetry import TelemetrySource
from .feature_adapter import FeatureAdapter
from .interfaces import Engine
from .ensemble import EnsembleVoter
from .evidence import EvidenceGenerator
from .types import InstrumentedResult, EnsembleDecision
from ..utils.circuit_breaker import CircuitBreaker
from ..logging import get_logger

if TYPE_CHECKING:  # pragma: no cover
    from .metrics import PrometheusMetrics


class DetectionPipeline:
    """
    Main detection pipeline orchestrator.
    
    Coordinates:
    - Telemetry loading
    - Feature extraction
    - Engine inference (ML/Rules/Math)
    - Ensemble voting
    - Evidence generation
    
    Instrumentation:
    - Per-stage latency tracking (time.perf_counter)
    - Error handling with circuit breaker
    - Metrics integration
    - Graceful degradation
    """
    
    def __init__(
        self,
        telemetry_source: TelemetrySource,
        feature_extractor,
        engines: List[Engine],
        ensemble: EnsembleVoter,
        evidence_generator: EvidenceGenerator,
        circuit_breaker: CircuitBreaker,
        config: Dict,
        metrics: Optional["PrometheusMetrics"] = None,
    ):
        """
        Initialize detection pipeline.
        
        Args:
            telemetry_source: Data source (file-based or Kafka)
            feature_extractor: NetworkFlowExtractor
            engines: List of Engine instances (Rules, Math, ML)
            ensemble: EnsembleVoter
            evidence_generator: EvidenceGenerator
            circuit_breaker: CircuitBreaker from service layer
            config: Configuration dictionary
        """
        self.telemetry = telemetry_source
        self.feature_extractor = feature_extractor
        self.engines = engines
        self.ensemble = ensemble
        self.evidence_gen = evidence_generator
        self.circuit_breaker = circuit_breaker
        self.config = config
        self.logger = get_logger(__name__)
        self.metrics = metrics
        self._feature_adapter = FeatureAdapter()
        self._max_flows_per_iteration = int(config.get('MAX_FLOWS_PER_ITERATION', 100))
        self._stage_warn_threshold_ms = float(config.get('STAGE_WARN_THRESHOLD_MS', 2000))
        
        # Pipeline statistics
        self.stats = {
            'total_processed': 0,
            'total_published': 0,
            'total_abstained': 0,
            'total_errors': 0
        }
        
        self.logger.info(
            f"Initialized DetectionPipeline with {len(engines)} engines"
        )
    
    def process(self, trigger_event: Optional[Dict] = None) -> InstrumentedResult:
        """
        Run full detection pipeline.
        
        Process:
        1. Load telemetry data
        2. Extract features
        3. Run all engines
        4. Ensemble voting
        5. Generate evidence (if publishing)
        
        All stages instrumented with latency tracking.
        
        Args:
            trigger_event: Optional event that triggered pipeline (for metadata)
        
        Returns:
            InstrumentedResult with decision and latency breakdown
        """
        pipeline_start = time.perf_counter()
        latencies = {}
        error = None
        
        try:
            # Stage 1: Load telemetry
            stage_start = time.perf_counter()
            telemetry_limit = int(self.config.get('TELEMETRY_BATCH_SIZE', 1000))
            limit = max(1, min(self._max_flows_per_iteration, telemetry_limit))
            flows = self.telemetry.get_network_flows(limit=limit)
            latencies['telemetry_load'] = (time.perf_counter() - stage_start) * 1000
            self._warn_if_slow('telemetry_load', latencies['telemetry_load'], limit=limit)

            db_timings = {}
            get_timings = getattr(self.telemetry, "get_last_timings", None)
            if callable(get_timings):
                try:
                    db_timings = get_timings() or {}
                except Exception:
                    db_timings = {}

            if db_timings:
                for key, value in db_timings.items():
                    latencies[f"telemetry_{key}"] = value
                if self.metrics:
                    try:
                        self.metrics.record_db_timings(**db_timings)
                    except Exception:
                        self.logger.debug("Failed to record DB timing metrics", exc_info=True)
            
            if not flows:
                return InstrumentedResult(
                    decision=None,
                    latency_ms=latencies,
                    total_latency_ms=(time.perf_counter() - pipeline_start) * 1000,
                    error='no_telemetry_data'
                )
            
            # Stage 2: Feature extraction
            stage_start = time.perf_counter()
            try:
                # Unified 79-feature path
                features_79 = self.feature_extractor.extract_raw(flows)
                features_norm = self.feature_extractor.normalize(features_79)
                semantics_list = self._feature_adapter.derive_semantics(flows, features_79)
                feature_count = features_79.shape[0] * features_79.shape[1]
            except Exception as e:
                self.logger.error(f"Feature extraction failed: {e}", exc_info=True)
                raise
            
            latencies['feature_extraction'] = (time.perf_counter() - stage_start) * 1000
            self._warn_if_slow('feature_extraction', latencies['feature_extraction'], feature_count=feature_count)
            
            # Stage 3: Run all engines
            stage_start = time.perf_counter()
            all_candidates = []
            
            for engine in self.engines:
                if not engine.is_ready:
                    self.logger.warning(f"Engine {engine.engine_type.value} not ready, skipping")
                    continue
                
                try:
                    if engine.engine_type.value == 'ml':
                        feats = features_norm[0] if len(features_norm) > 0 else features_norm
                    else:
                        # Pass semantic dict for first flow
                        feats = semantics_list[0] if semantics_list else {}
                    candidates = engine.predict(feats)
                    all_candidates.extend(candidates)
                    
                    self.logger.debug(
                        f"Engine {engine.engine_type.value}: "
                        f"{len(candidates)} candidates"
                    )
                except Exception as e:
                    self.logger.error(
                        f"Engine {engine.engine_type.value} failed: {e}",
                        exc_info=True
                    )
                    # Continue with other engines (graceful degradation)
            
            latencies['engine_inference'] = (time.perf_counter() - stage_start) * 1000
            self._warn_if_slow('engine_inference', latencies['engine_inference'], engines=len(self.engines))
            
            # Stage 4: Ensemble voting
            stage_start = time.perf_counter()
            decision = self.ensemble.decide(all_candidates)
            # Attach minimal network context for downstream tokenization/evidence
            try:
                if flows:
                    f = flows[0] or {}
                    def _get(k: str, *alts, default=None):
                        for key in (k,)+alts:
                            if key in f and f[key] is not None:
                                return f[key]
                        return default
                    net_ctx = {
                        'src_ip': _get('src_ip', 'src', 'source'),
                        'dst_ip': _get('dst_ip', 'dst', 'destination'),
                        'src_port': _get('src_port', 'sport', 'source_port', default=0),
                        'dst_port': _get('dst_port', 'dport', 'destination_port', default=0),
                        'protocol': _get('protocol', 'proto', default=''),
                        'flow_id': _get('flow_id', 'id', default=''),
                    }
                    decision.metadata['network_context'] = net_ctx
            except Exception:
                # Best-effort only; do not fail pipeline on context extraction
                pass
            latencies['ensemble_vote'] = (time.perf_counter() - stage_start) * 1000
            self._warn_if_slow('ensemble_vote', latencies['ensemble_vote'], candidates=len(all_candidates))
            
            # Stage 5: Evidence generation (if publishing)
            if decision.should_publish:
                stage_start = time.perf_counter()
                evidence_bytes = self.evidence_gen.generate(
                    decision=decision,
                    raw_features={'flows': flows, 'semantics': semantics_list[:5]}
                )
                decision.metadata['evidence'] = evidence_bytes
                latencies['evidence_generation'] = (time.perf_counter() - stage_start) * 1000
                self._warn_if_slow('evidence_generation', latencies['evidence_generation'])
            
            # Update statistics
            self.stats['total_processed'] += 1
            if decision.should_publish:
                self.stats['total_published'] += 1
            else:
                self.stats['total_abstained'] += 1
            
            # Calculate total latency
            total_latency = (time.perf_counter() - pipeline_start) * 1000
            
            # Log if latency exceeds target
            if total_latency > 50.0:
                self.logger.warning(
                    f"Pipeline latency exceeded target: {total_latency:.2f}ms > 50ms"
                )
            
            # Circuit breaker success handled by caller
            
            return InstrumentedResult(
                decision=decision,
                latency_ms=latencies,
                total_latency_ms=total_latency,
                feature_count=feature_count,
                candidate_count=len(all_candidates),
                error=None
            )
        
        except Exception as e:
            # Record failure
            self.circuit_breaker.record_failure()
            self.stats['total_errors'] += 1
            
            error_msg = str(e)
            self.logger.error(f"Pipeline failed: {error_msg}", exc_info=True)
            
            return InstrumentedResult(
                decision=None,
                latency_ms=latencies,
                total_latency_ms=(time.perf_counter() - pipeline_start) * 1000,
                error=error_msg
            )
    
    def _warn_if_slow(self, stage: str, duration_ms: float, **kwargs) -> None:
        if duration_ms <= self._stage_warn_threshold_ms:
            return
        context = {
            "stage": stage,
            "duration_ms": round(duration_ms, 2),
        }
        for key, value in kwargs.items():
            if value is None:
                continue
            context[key] = value
        self.logger.warning("Detection pipeline stage latency warning", extra=context)

    def get_statistics(self) -> Dict:
        """
        Get pipeline statistics.
        
        Returns:
            Dictionary with processing stats
        """
        return {
            **self.stats,
            'engines_ready': sum(1 for e in self.engines if e.is_ready),
            'total_engines': len(self.engines)
        }
    
    def health_check(self) -> Dict:
        """
        Check pipeline health.
        
        Returns:
            Dictionary with health status
        """
        engines_status = {}
        for engine in self.engines:
            engines_status[engine.engine_type.value] = {
                'ready': engine.is_ready,
                'metadata': engine.get_metadata()
            }
        
        return {
            'healthy': all(e.is_ready for e in self.engines),
            'telemetry_has_data': self.telemetry.has_data(),
            'engines': engines_status,
            'statistics': self.stats,
            'circuit_breaker_state': self.circuit_breaker.state.value
        }
