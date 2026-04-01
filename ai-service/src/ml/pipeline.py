"""
Detection pipeline orchestrator with full instrumentation.

Flow: Telemetry → Features → Engines → Ensemble → Evidence
Instrumented: Per-stage latency tracking, error handling, metrics
"""

from collections import defaultdict
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

    def _build_engine_metrics_snapshots(
        self,
        candidates,
        decision,
        *,
        timestamp: float,
        iteration_latency_ms: float,
    ) -> Dict[str, List[Dict[str, object]]]:
        """Build additive engine and variant snapshots from current candidates."""

        engine_entries = defaultdict(lambda: {
            "candidates": 0,
            "published": 0,
            "confidence_sum": 0.0,
            "threat_types": set(),
        })
        variant_entries = defaultdict(lambda: {
            "total": 0,
            "published": 0,
            "confidence_sum": 0.0,
            "engines": set(),
            "threat_types": set(),
        })

        published_ids = set()
        if decision and getattr(decision, "should_publish", False):
            for candidate in getattr(decision, "candidates", []) or []:
                published_ids.add(id(candidate))

        for candidate in candidates or []:
            engine_type = getattr(getattr(candidate, "engine_type", None), "value", None)
            if not engine_type:
                continue

            confidence = float(getattr(candidate, "confidence", 0.0) or 0.0)
            threat_type = str(getattr(getattr(candidate, "threat_type", None), "value", "unknown"))
            published = 1 if id(candidate) in published_ids else 0

            engine_entry = engine_entries[engine_type]
            engine_entry["candidates"] += 1
            engine_entry["published"] += published
            engine_entry["confidence_sum"] += confidence
            engine_entry["threat_types"].add(threat_type)

            metadata = getattr(candidate, "metadata", None) or {}
            variant = metadata.get("variant")
            if not isinstance(variant, str) or not variant.strip():
                continue

            variant_key = variant.strip()
            variant_entry = variant_entries[variant_key]
            variant_entry["total"] += 1
            variant_entry["published"] += published
            variant_entry["confidence_sum"] += confidence
            variant_entry["engines"].add(engine_type)
            variant_entry["threat_types"].add(threat_type)

        engine_snapshots = []
        for engine, entry in engine_entries.items():
            engine_snapshots.append({
                "engine": engine,
                "candidates": int(entry["candidates"]),
                "published": int(entry["published"]),
                "confidence_sum": float(entry["confidence_sum"]),
                "threat_types": sorted(entry["threat_types"]),
                "timestamp": timestamp,
                "iteration_latency_ms": iteration_latency_ms,
            })

        variant_snapshots = []
        for variant, entry in variant_entries.items():
            variant_snapshots.append({
                "variant": variant,
                "engines": sorted(entry["engines"]),
                "engine": sorted(entry["engines"])[0] if entry["engines"] else None,
                "total": int(entry["total"]),
                "published": int(entry["published"]),
                "confidence_sum": float(entry["confidence_sum"]),
                "threat_types": sorted(entry["threat_types"]),
                "timestamp": timestamp,
            })

        return {
            "engine_snapshots": engine_snapshots,
            "variant_snapshots": variant_snapshots,
        }
    
    def process(self, trigger_event: Optional[Dict] = None, *, telemetry_wait_policy: Optional[str] = None) -> InstrumentedResult:
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
            flows = self.telemetry.get_network_flows(limit=limit, wait_policy=telemetry_wait_policy)
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

            fetch_stats = {}
            get_fetch_stats = getattr(self.telemetry, "get_last_fetch_stats", None)
            if callable(get_fetch_stats):
                try:
                    fetch_stats = get_fetch_stats() or {}
                except Exception:
                    fetch_stats = {}
            
            if not flows:
                fetch_reason = str(fetch_stats.get("reason", "unknown"))
                error_code = f"no_telemetry_data:{fetch_reason}"
                metadata = {"telemetry_fetch": fetch_stats}
                if telemetry_wait_policy == "non_blocking":
                    metadata["abstention_reason"] = f"telemetry_unavailable:{fetch_reason}"
                return InstrumentedResult(
                    decision=None,
                    latency_ms=latencies,
                    total_latency_ms=(time.perf_counter() - pipeline_start) * 1000,
                    error=error_code,
                    metadata=metadata,
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
                        for key in (k,) + alts:
                            if key in f and f[key] is not None:
                                return f[key]
                        return default

                    # Preserve all non-empty flow metadata so downstream anomaly/policy
                    # publishers keep tenant and lineage context instead of shrinking the
                    # context down to only transport fields.
                    net_ctx = {
                        str(key): value
                        for key, value in f.items()
                        if isinstance(key, str) and value not in (None, "", [], {})
                    }
                    net_ctx.update({
                        'src_ip': _get('src_ip', 'src', 'source'),
                        'dst_ip': _get('dst_ip', 'dst', 'destination'),
                        'src_port': _get('src_port', 'sport', 'source_port', default=0),
                        'dst_port': _get('dst_port', 'dport', 'destination_port', default=0),
                        'protocol': _get('protocol', 'proto', default=''),
                        'flow_id': _get('flow_id', 'id', default=''),
                        'source_event_ts_ms': _get('source_event_ts_ms', default=0),
                        'telemetry_ingest_ts_ms': _get('telemetry_ingest_ts_ms', default=0),
                    })

                    source_event_id = _get('source_event_id', 'source_id', 'id', 'flow_id', default='')
                    if source_event_id and not net_ctx.get('source_event_id'):
                        net_ctx['source_event_id'] = source_event_id

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
            metrics_snapshots = self._build_engine_metrics_snapshots(
                all_candidates,
                decision,
                timestamp=time.time(),
                iteration_latency_ms=total_latency,
            )
            
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
                error=None,
                metadata={
                    "telemetry_fetch": fetch_stats,
                    **metrics_snapshots,
                },
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
                error=error_msg,
                metadata={"telemetry_fetch": locals().get("fetch_stats", {})},
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
