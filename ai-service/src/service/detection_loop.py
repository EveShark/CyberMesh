"""
Real-time detection loop - continuously polls telemetry and detects anomalies.

Runs in background thread, publishes detections to Kafka.
"""

import logging
import random
import threading
import time
from typing import Optional, Dict, Any, Callable, List, TYPE_CHECKING

if TYPE_CHECKING:  # pragma: no cover - avoid runtime dependency cycle
    from ..feedback.tracker import AnomalyLifecycleTracker


def _extract_raw_score(decision) -> Optional[float]:
    """Best-effort extraction of the ensemble's underlying raw score."""

    if not decision or not getattr(decision, "candidates", None):
        return None

    try:
        from ..ml.types import DetectionCandidate  # local import to prevent cycle

        typed_candidates = [
            c for c in decision.candidates
            if isinstance(c, DetectionCandidate) and c.threat_type == decision.threat_type
        ]
        target = typed_candidates[0] if typed_candidates else decision.candidates[0]
        raw = getattr(target, "raw_score", None)
        if raw is None:
            return None
        return float(raw)
    except Exception:
        return None


class DetectionLoop:
    """
    Continuous detection loop that polls telemetry and publishes anomalies.
    
    Architecture:
    - Runs in background daemon thread
    - Polls telemetry source every N seconds
    - Runs detection pipeline on new data
    - Publishes anomalies (with rate limiting)
    - Tracks metrics (detections/sec, latency, errors)
    - Graceful shutdown
    
    Thread Safety:
    - Safe to start/stop from any thread
    - Uses threading primitives for state management
    - Daemon thread (dies with main process)
    """
    
    def __init__(
        self,
        pipeline,
        publisher,
        rate_limiter,
        config: Dict[str, Any],
        logger: Optional[logging.Logger] = None,
        metrics=None,
        event_recorder: Optional[Callable[..., None]] = None,
        engine_metrics_callback: Optional[Callable[[List[Dict[str, Any]], List[Dict[str, Any]]], None]] = None,
    ):
        """
        Initialize detection loop.
        
        Args:
            pipeline: DetectionPipeline instance (with internal telemetry source)
            publisher: MessagePublisher instance
            rate_limiter: RateLimiter instance
            config: Configuration dict with detection_interval, etc.
            logger: Optional logger (creates one if not provided)
        """
        self.pipeline = pipeline
        self.publisher = publisher
        self.rate_limiter = rate_limiter
        self.config = config
        self.logger = logger or logging.getLogger(__name__)
        self._metrics_collector = metrics
        self._event_recorder = event_recorder
        self._engine_metrics_callback = engine_metrics_callback
        self._tracker: Optional["AnomalyLifecycleTracker"] = None
        
        # Thread control
        self._running = False
        self._thread: Optional[threading.Thread] = None
        self._lock = threading.RLock()
        
        # Configuration
        self._interval = config.get('DETECTION_INTERVAL', 5)
        self._timeout = config.get('DETECTION_TIMEOUT', 30)
        
        # Metrics
        self._metrics = {
            "detections_total": 0,
            "detections_published": 0,
            "detections_rate_limited": 0,
            "errors": 0,
            "last_detection_time": None,
            "last_iteration_time": None,
            "loop_iterations": 0,
            "avg_latency_ms": 0.0,
            "last_latency_ms": 0.0,
        }
        self._metrics_lock = threading.Lock()
        self._summary_interval = max(1, int(config.get('DETECTION_SUMMARY_INTERVAL', 10)))
        self._max_flows_per_iteration = int(config.get('MAX_FLOWS_PER_ITERATION', 100))
        self._max_iteration_lag = max(0, float(config.get('MAX_ITERATION_LAG_SECONDS', 20)))
        self._max_detection_gap = max(0, float(config.get('MAX_DETECTION_GAP_SECONDS', 600)))
        self._max_loop_latency_ms = max(0, float(config.get('MAX_LOOP_LATENCY_WARNING_MS', 3000)))
        self._publish_flush = bool(config.get('DETECTION_PUBLISH_FLUSH', False))
        self._summary_state = {
            "count": 0,
            "latency_total": 0.0,
            "total_latency_total": 0.0,
            "published": 0,
            "rate_limited": 0,
            "errors": 0,
        }
        self._tracker_sample_window_size = max(1, int(config.get('TRACKER_SAMPLE_WINDOW_SIZE', 500)))
        self._tracker_sample_cap = max(0, min(self._tracker_sample_window_size, int(config.get('TRACKER_SAMPLE_CAP', 100))))
        self._tracker_batch_size = max(1, int(config.get('TRACKER_BATCH_SIZE', 50)))
        self._tracker_flush_interval = max(1.0, float(config.get('TRACKER_FLUSH_INTERVAL_SECONDS', 5)))
        self._tracker_buffer: List[Dict[str, Any]] = []
        self._last_tracker_flush = time.time()
        self._sample_window_total = 0
        self._sample_window_taken = 0
        self._rng = random.Random()
        
        self.logger.info(
            f"DetectionLoop initialized: interval={self._interval}s, "
            f"timeout={self._timeout}s"
        )
    
    def set_tracker(self, tracker: Optional["AnomalyLifecycleTracker"]) -> None:
        """Attach lifecycle tracker for anomaly persistence."""

        if tracker is self._tracker:
            return

        if tracker is None:
            self._maybe_flush_tracker_buffer(force=True)
            self._tracker = None
            self.logger.info("Detection loop tracker detached")
            return

        self._tracker = tracker
        self.logger.info("Detection loop tracker attached")
        self._maybe_flush_tracker_buffer(force=True)

    def _record_tracker_events(
        self,
        *,
        anomaly_id: str,
        anomaly_type: str,
        severity: int,
        confidence: float,
        raw_score: Optional[float],
        timestamp: float,
    ) -> None:
        if not self._should_sample_tracker_event():
            return

        self._tracker_buffer.append(
            {
                "anomaly_id": anomaly_id,
                "anomaly_type": anomaly_type,
                "severity": severity,
                "confidence": float(confidence),
                "raw_score": None if raw_score is None else float(raw_score),
                "timestamp": float(timestamp),
            }
        )

        if len(self._tracker_buffer) >= self._tracker_batch_size:
            self._maybe_flush_tracker_buffer()

    def _should_sample_tracker_event(self) -> bool:
        if self._tracker_sample_cap <= 0:
            self._sample_window_total += 1
            if self._sample_window_total >= self._tracker_sample_window_size:
                self._sample_window_total = 0
                self._sample_window_taken = 0
            return False

        if self._sample_window_total >= self._tracker_sample_window_size:
            self._sample_window_total = 0
            self._sample_window_taken = 0

        remaining_events = self._tracker_sample_window_size - self._sample_window_total
        remaining_slots = self._tracker_sample_cap - self._sample_window_taken

        if remaining_slots <= 0:
            self._sample_window_total += 1
            if self._sample_window_total >= self._tracker_sample_window_size:
                self._sample_window_total = 0
                self._sample_window_taken = 0
            return False

        probability = min(1.0, max(0.0, remaining_slots / float(max(1, remaining_events))))
        keep = self._rng.random() < probability

        self._sample_window_total += 1
        if keep:
            self._sample_window_taken += 1

        if self._sample_window_total >= self._tracker_sample_window_size:
            self._sample_window_total = 0
            self._sample_window_taken = 0

        return keep

    def _maybe_flush_tracker_buffer(self, force: bool = False) -> None:
        if not self._tracker_buffer:
            if force:
                self._last_tracker_flush = time.time()
            return

        now = time.time()
        if not force:
            if len(self._tracker_buffer) < self._tracker_batch_size and (now - self._last_tracker_flush) < self._tracker_flush_interval:
                return

        tracker = self._tracker
        if tracker is None:
            if force and self._tracker_buffer:
                self.logger.warning(
                    "Dropping tracker batch because tracker unavailable",
                    extra={"count": len(self._tracker_buffer)}
                )
                self._tracker_buffer.clear()
            self._last_tracker_flush = now
            return

        try:
            tracker.record_batch(list(self._tracker_buffer))
        except Exception as err:
            self.logger.error(
                "Failed to flush tracker batch",
                extra={
                    "count": len(self._tracker_buffer),
                    "error": str(err),
                },
                exc_info=True,
            )
            self._last_tracker_flush = now
            return

        self._tracker_buffer.clear()
        self._last_tracker_flush = now

    def start(self) -> None:
        """
        Start detection loop in background thread.
        
        Thread Safety:
            - Idempotent (safe to call multiple times)
            - Only one loop runs at a time
        
        Raises:
            RuntimeError: If loop already running
        """
        with self._lock:
            if self._running:
                self.logger.warning("Detection loop already running")
                return
            
            self._running = True
            self._thread = threading.Thread(
                target=self._run_loop,
                name='DetectionLoop',
                daemon=True
            )
            self._thread.start()
            
            if self._metrics_collector is not None:
                try:
                    self._metrics_collector.set_detection_loop_running(True)
                except Exception:
                    self.logger.debug("Failed to set detection loop running metric", exc_info=True)

            self.logger.info("Detection loop started")
    
    def stop(self, timeout: int = 30) -> None:
        """
        Stop detection loop gracefully.
        
        Args:
            timeout: Max seconds to wait for loop to finish (default: 30)
        
        Thread Safety:
            - Idempotent (safe to call multiple times)
            - Waits for current iteration to complete
        """
        with self._lock:
            if not self._running:
                self.logger.info("Detection loop not running")
                return
            
            self.logger.info("Stopping detection loop")
            self._running = False
            
            # Wait for thread to finish
            if self._thread and self._thread.is_alive():
                self._thread.join(timeout)
                
                if self._thread.is_alive():
                    self.logger.warning(
                        f"Detection loop did not stop within {timeout}s"
                    )
                else:
                    self.logger.info("Detection loop stopped")

            self._maybe_flush_tracker_buffer(force=True)

            if self._metrics_collector is not None:
                try:
                    self._metrics_collector.set_detection_loop_running(False)
                except Exception:
                    self.logger.debug("Failed to clear detection loop running metric", exc_info=True)
    
    def _run_loop(self) -> None:
        """
        Main detection loop (runs in background thread).
        
        Process:
        1. Run detection pipeline (pipeline polls telemetry internally)
        2. If should_publish, publish anomaly (with rate limiting)
        3. Update metrics
        4. Sleep until next interval
        5. Repeat until stopped
        """
        self.logger.info(f"Detection loop running (interval={self._interval}s)")
        
        while self._running:
            iteration_start = time.time()
            rate_limited = False
            
            try:
                # 1. Run detection pipeline
                detection_start = time.time()
                result = self.pipeline.process()
                detection_latency = (time.time() - detection_start) * 1000  # ms
                
                # Record pipeline metrics if available
                if self._metrics_collector is not None:
                    try:
                        self._metrics_collector.record_pipeline_result(
                            decision=result.decision,
                            latencies=result.latency_ms,
                            total_latency=result.total_latency_ms,
                            feature_count=result.feature_count,
                            candidate_count=result.candidate_count,
                        )
                    except Exception as metrics_err:
                        self.logger.debug(
                            "Failed to record pipeline metrics",
                            exc_info=True,
                            extra={"error": str(metrics_err)},
                        )

                # 2. Check if we should publish
                decision = result.decision

                if decision and self._event_recorder is not None:
                    try:
                        validator_id = None
                        if decision.metadata:
                            validator_id = decision.metadata.get("validator_id")

                        severity = max(1, min(10, int(decision.final_score * 10)))

                        self._event_recorder(
                            source="detection_loop",
                            validator_id=validator_id,
                            threat_type=decision.threat_type.value if decision.threat_type else "unknown",
                            severity=severity,
                            confidence=float(decision.confidence),
                            final_score=float(decision.final_score),
                            should_publish=decision.should_publish,
                            metadata={
                                "candidate_count": result.candidate_count,
                                "latency_ms": round(result.total_latency_ms, 2),
                                "abstention_reason": decision.abstention_reason,
                            },
                        )
                    except Exception as record_err:
                        self.logger.debug(
                            "Failed to record detection event",
                            exc_info=True,
                            extra={"error": str(record_err)},
                        )

                if decision and decision.should_publish:
                    self._increment_metric("detections_total")
                    
                    # Check rate limit
                    if self.rate_limiter.acquire():
                        # Publish anomaly
                        import uuid
                        import json
                        
                        anomaly_id = str(uuid.uuid4())
                        anomaly_type = decision.threat_type.value
                        severity = max(1, min(10, int(decision.final_score * 10)))
                        confidence = decision.confidence
                        detection_ts = time.time()
                        raw_score = _extract_raw_score(decision)
                        
                        # Build JSON payload with FULL security context
                        
                        # Extract network context from metadata/candidates
                        network_context = {}
                        if decision.candidates:
                            # Get features from first candidate
                            features = decision.candidates[0].features
                            network_context = {
                                'src_ip': features.get('src_ip', 'unknown'),
                                'dst_ip': features.get('dst_ip', 'unknown'),
                                'src_port': features.get('src_port', 0),
                                'dst_port': features.get('dst_port', 0),
                                'protocol': features.get('protocol', 0),
                                'flow_id': features.get('flow_id', 'unknown'),
                                'flow_duration': features.get('flow_duration', 0),
                                'total_packets': features.get('tot_fwd_pkts', 0) + features.get('tot_bwd_pkts', 0),
                                'total_bytes': features.get('totlen_fwd_pkts', 0) + features.get('totlen_bwd_pkts', 0),
                            }
                        
                        payload_obj = {
                            # Detection metadata
                            'detection_timestamp': detection_ts,
                            'anomaly_id': anomaly_id,
                            'severity': severity,
                            'confidence': float(confidence),
                            'threat_type': anomaly_type,
                            'final_score': float(decision.final_score),
                            'llr': float(decision.llr),
                            # Network context
                            **network_context,
                            # Model info
                            'model_version': 'v1.0.0',
                            'contributing_engines': [c.engine_name for c in decision.candidates[:3]],
                        }
                        payload_bytes = json.dumps(payload_obj, separators=(",", ":")).encode('utf-8')
                        
                        published = False
                        try:
                            self.publisher.publish_anomaly(
                                anomaly_id=anomaly_id,
                                anomaly_type=anomaly_type,
                                source='detection_loop',
                                severity=severity,
                                confidence=confidence,
                                payload=payload_bytes,
                                model_version='v1.0.0'
                            )
                            published = True
                        finally:
                            if published:
                                self._increment_metric("detections_published")

                        if published:
                            self.logger.info(
                                f"Published anomaly: {anomaly_type} "
                                f"(confidence={confidence:.2f}, severity={severity})"
                            )

                            self._record_tracker_events(
                                anomaly_id=anomaly_id,
                                anomaly_type=anomaly_type,
                                severity=severity,
                                confidence=float(confidence),
                                raw_score=raw_score,
                                timestamp=detection_ts,
                            )
                        
                        if published:
                            # Publish supporting evidence (Fix: Gap 1)
                            self.logger.info(f"[EVIDENCE_DEBUG] Starting evidence publishing attempt for anomaly {anomaly_id}")
                            try:
                                ev_bytes = decision.metadata.get('evidence')
                                self.logger.info(f"[EVIDENCE_DEBUG] ev_bytes={'PRESENT' if ev_bytes else 'MISSING'}, metadata_keys={list(decision.metadata.keys())}")
                                if ev_bytes:
                                    # Evidence is already a signed, serialized protobuf EvidenceMessage
                                    # Send it directly to Kafka
                                    topic = self.publisher.producer.topics.ai_evidence
                                    self.publisher.producer.producer.produce(
                                        topic,
                                        value=ev_bytes,
                                        callback=self.publisher.producer._delivery_callback
                                    )
                                    self.publisher.producer.producer.poll(0)
                                    if self._publish_flush:
                                        self.publisher.producer.producer.flush(timeout=5)

                                    self.logger.info(
                                        f"Published evidence for anomaly {anomaly_id}"
                                    )
                            except Exception as e:
                                # Don't fail detection on evidence publication errors
                                self.logger.error(
                                    f"Failed to publish evidence: {e}",
                                    exc_info=True
                                )
                    else:
                        self._increment_metric("detections_rate_limited")
                        rate_limited = True
                        self.logger.warning("Rate limited detection")
                    
                    # Update last detection time
                    with self._metrics_lock:
                        self._metrics["last_detection_time"] = time.time()
                elif result.error:
                    # Promote pipeline errors to INFO for visibility at default log level
                    self.logger.info(
                        "Pipeline returned error",
                        extra={"error": result.error, "latency_ms": round(detection_latency, 2)}
                    )
                
                # 3. Update metrics
                self._increment_metric("loop_iterations")
                self._update_avg_latency(detection_latency)
                with self._metrics_lock:
                    self._metrics["last_iteration_time"] = time.time()
                    self._metrics["last_latency_ms"] = detection_latency
                
                self._update_summary(
                    iteration_latency=detection_latency,
                    total_latency=result.total_latency_ms,
                    published=bool(result.decision and result.decision.should_publish),
                    rate_limited=rate_limited,
                    errored=False,
                )

                # INFO-level iteration summary to make pipeline activity visible without DEBUG
                self.logger.info(
                    "Detection iteration",
                    extra={
                        "published": bool(result.decision and result.decision.should_publish),
                        "abstention_reason": (
                            result.decision.abstention_reason
                            if (result.decision and not result.decision.should_publish)
                            else None
                        ),
                        "feature_count": result.feature_count,
                        "candidate_count": result.candidate_count,
                        "latency_ms": round(detection_latency, 2),
                        "total_latency_ms": round(result.total_latency_ms, 2),
                    },
                )

                self.logger.debug(
                    f"Detection iteration complete: "
                    f"published={result.decision.should_publish if result.decision else False}, "
                    f"{detection_latency:.1f}ms"
                )

                if self._metrics_collector is not None:
                    try:
                        self._metrics_collector.record_detection_iteration(
                            latency_ms=detection_latency,
                            published=bool(result.decision and result.decision.should_publish),
                            rate_limited=rate_limited,
                        )
                    except Exception:
                        self.logger.debug("Failed to record detection iteration metrics", exc_info=True)

                # Record engine-level metrics
                engine_snapshots: List[Dict[str, Any]] = []
                variant_snapshots: List[Dict[str, Any]] = []
                if result.decision:
                    now = time.time()
                    engine_aggregate: Dict[str, Dict[str, Any]] = {}
                    variant_aggregate: Dict[str, Dict[str, Any]] = {}

                    for candidate in result.decision.candidates:
                        engine_key = candidate.engine_type.value
                        engine_entry = engine_aggregate.setdefault(
                            engine_key,
                            {
                                "engine": engine_key,
                                "candidates": 0,
                                "published": 0,
                                "confidence_sum": 0.0,
                                "threat_types": set(),
                            },
                        )
                        engine_entry["candidates"] += 1
                        engine_entry["confidence_sum"] += float(candidate.confidence)
                        engine_entry["threat_types"].add(candidate.threat_type.value)
                        if result.decision.should_publish:
                            engine_entry["published"] += 1

                        variant = candidate.metadata.get("variant") if candidate.metadata else None
                        if variant:
                            variant_entry = variant_aggregate.setdefault(
                                variant,
                                {
                                    "variant": variant,
                                    "engine": engine_key,
                                    "total": 0,
                                    "published": 0,
                                    "confidence_sum": 0.0,
                                    "threat_types": set(),
                                },
                            )
                            variant_entry["total"] += 1
                            variant_entry["confidence_sum"] += float(candidate.confidence)
                            variant_entry["threat_types"].add(candidate.threat_type.value)
                            if result.decision.should_publish:
                                variant_entry["published"] += 1

                    for engine_key, data in engine_aggregate.items():
                        snapshot = {
                            "engine": engine_key,
                            "candidates": data["candidates"],
                            "published": data["published"],
                            "confidence_sum": data["confidence_sum"],
                            "threat_types": sorted(data["threat_types"]),
                            "timestamp": now,
                            "iteration_latency_ms": detection_latency,
                        }
                        engine_snapshots.append(snapshot)
                        if self._metrics_collector is not None:
                            try:
                                self._metrics_collector.record_engine_iteration(
                                    engine_type=engine_key,
                                    candidates=data["candidates"],
                                    published=data["published"],
                                    confidence_sum=data["confidence_sum"],
                                )
                            except Exception:
                                self.logger.debug("Failed to record engine metrics", exc_info=True)

                    for variant_key, data in variant_aggregate.items():
                        variant_snapshots.append(
                            {
                                "variant": variant_key,
                                "engine": data["engine"],
                                "total": data["total"],
                                "published": data["published"],
                                "confidence_sum": data["confidence_sum"],
                                "threat_types": sorted(data["threat_types"]),
                                "timestamp": now,
                            }
                        )

                if self._engine_metrics_callback and (engine_snapshots or variant_snapshots):
                    try:
                        self._engine_metrics_callback(engine_snapshots, variant_snapshots)
                    except Exception:
                        self.logger.debug("Failed to record engine analytics", exc_info=True)
                
            except Exception as e:
                self._increment_metric("errors")
                self.logger.error(
                    f"Detection loop error: {e}",
                    exc_info=True,
                    extra={"error": str(e)}
                )
                if self._metrics_collector is not None:
                    try:
                        self._metrics_collector.record_detection_error()
                    except Exception:
                        self.logger.debug("Failed to record detection error metric", exc_info=True)

                self._update_summary(
                    iteration_latency=0.0,
                    total_latency=0.0,
                    published=False,
                    rate_limited=False,
                    errored=True,
                )
            
            # Flush tracker buffer if interval elapsed
            self._maybe_flush_tracker_buffer()

            # 5. Sleep until next interval
            iteration_time = time.time() - iteration_start
            sleep_time = max(0, self._interval - iteration_time)
            
            if iteration_time > self._interval + self._max_iteration_lag:
                self.logger.warning(
                    "Detection loop lagging",
                    extra={
                        "iteration_time_s": round(iteration_time, 2),
                        "interval_s": self._interval,
                    },
                )

            if sleep_time > 0:
                time.sleep(sleep_time)
        
        self.logger.info("Detection loop exited")
        self._maybe_flush_tracker_buffer(force=True)
        if self._metrics_collector is not None:
            try:
                self._metrics_collector.set_detection_loop_running(False)
            except Exception:
                self.logger.debug("Failed to clear detection loop running metric", exc_info=True)
    
    def _increment_metric(self, key: str, value: int = 1) -> None:
        """Increment metric counter (thread-safe)."""
        with self._metrics_lock:
            self._metrics[key] = self._metrics.get(key, 0) + value
    
    def _update_avg_latency(self, latency_ms: float) -> None:
        """Update average latency using exponential moving average."""
        with self._metrics_lock:
            current_avg = self._metrics.get("avg_latency_ms", 0.0)
            # EMA with alpha=0.1 (smooth over ~10 samples)
            self._metrics["avg_latency_ms"] = current_avg * 0.9 + latency_ms * 0.1
    
    def get_metrics(self) -> Dict[str, Any]:
        """
        Get detection loop metrics.
        
        Returns:
            Dictionary with metrics:
            - detections_total: Total anomalies detected
            - detections_published: Anomalies published to Kafka
            - detections_rate_limited: Anomalies dropped due to rate limit
            - errors: Error count
            - last_detection_time: Timestamp of last detection
            - loop_iterations: Number of loop iterations
            - avg_latency_ms: Average detection latency
        
        Thread Safety:
            - Returns copy of metrics (safe to read while loop runs)
        """
        with self._metrics_lock:
            return self._metrics.copy()
    
    def is_running(self) -> bool:
        """Return True when the background loop thread is active."""
        with self._lock:
            return self._running

    def get_health_snapshot(self) -> Dict[str, Any]:
        """Evaluate current loop health using configured thresholds."""

        now = time.time()
        with self._metrics_lock:
            metrics = self._metrics.copy()

        running = self.is_running()
        issues: List[str] = []
        blocking = False

        last_iteration_raw = metrics.get("last_iteration_time")
        last_detection_raw = metrics.get("last_detection_time")
        last_latency = metrics.get("last_latency_ms")
        iterations = metrics.get("loop_iterations", 0) or 0
        errors = metrics.get("errors", 0) or 0

        iteration_gap: Optional[float] = None
        detection_gap: Optional[float] = None

        if isinstance(last_iteration_raw, (int, float)):
            iteration_gap = max(0.0, now - float(last_iteration_raw))

        if isinstance(last_detection_raw, (int, float)):
            detection_gap = max(0.0, now - float(last_detection_raw))

        if not running:
            blocking = True
            issues.append("detection loop not running")
        else:
            # Evaluate iteration cadence
            iteration_threshold = max(self._interval * 3, self._max_iteration_lag + self._interval)
            if iteration_gap is None:
                # After a few iterations we expect cadence timestamps
                if iterations >= max(6, int(self._summary_interval)):
                    issues.append("no iteration cadence recorded")
            elif self._max_iteration_lag > 0 and iteration_gap > iteration_threshold:
                issues.append(f"{iteration_gap:.1f}s since last iteration")
                if iteration_gap > iteration_threshold*2:
                    blocking = True

            # Evaluate detection freshness
            if self._max_detection_gap > 0:
                if detection_gap is None:
                    if iterations >= max(12, int(self._summary_interval)*2):
                        issues.append("no detections recorded yet")
                elif detection_gap > self._max_detection_gap:
                    issues.append(f"{detection_gap:.1f}s since last detection")
                    if detection_gap > self._max_detection_gap*2:
                        blocking = True

            # Evaluate latency spikes
            if isinstance(last_latency, (int, float)) and self._max_loop_latency_ms > 0:
                if last_latency > self._max_loop_latency_ms:
                    issues.append(f"iteration latency {last_latency:.0f}ms")

            # Evaluate error rate
            if iterations > 0 and errors > 0:
                error_rate = errors / max(1, iterations)
                if error_rate > 0.25:
                    issues.append(f"error rate {error_rate*100:.1f}%")
                    if error_rate > 0.5:
                        blocking = True

        status = "ok"
        if not running:
            status = "stopped"
        elif blocking:
            status = "critical"
        elif issues:
            status = "degraded"

        message = "; ".join(issues)

        snapshot = {
            "running": running,
            "status": status,
            "healthy": status == "ok",
            "blocking": blocking,
            "issues": issues,
            "message": message,
            "metrics": metrics,
            "last_updated": now,
            "seconds_since_last_iteration": iteration_gap,
            "seconds_since_last_detection": detection_gap,
        }

        return snapshot

    def is_healthy(self) -> bool:
        """
        Check if detection loop is healthy.
        
        Health criteria:
        - Loop is running
        - No errors in last iteration (or errors < 10% of iterations)
        - Not stalled (detected something in last 5 minutes, or just started)
        
        Returns:
            True if healthy, False otherwise
        """
        if not self._running:
            return False
        
        with self._metrics_lock:
            # Check error rate
            iterations = self._metrics.get("loop_iterations", 0)
            errors = self._metrics.get("errors", 0)
            
            if iterations > 0:
                error_rate = errors / iterations
                if error_rate > 0.1:  # More than 10% errors
                    return False
            
            # Check if stalled (no detections in 5 minutes)
            last_detection = self._metrics.get("last_detection_time")
            if last_detection:
                stalled = time.time() - last_detection > 300
                if stalled and iterations > 60:  # Only flag if loop has run for a while
                    return False
        
        return True

    def _update_summary(self, *, iteration_latency: float, total_latency: float, published: bool, rate_limited: bool, errored: bool) -> None:
        if self._summary_interval <= 0:
            return

        self._summary_state["count"] += 1
        self._summary_state["latency_total"] += iteration_latency
        self._summary_state["total_latency_total"] += total_latency
        if published:
            self._summary_state["published"] += 1
        if rate_limited:
            self._summary_state["rate_limited"] += 1
        if errored:
            self._summary_state["errors"] += 1

        if iteration_latency > self._max_loop_latency_ms > 0:
            self.logger.warning(
                "Detection iteration latency exceeded threshold",
                extra={
                    "latency_ms": round(iteration_latency, 2),
                    "threshold_ms": self._max_loop_latency_ms,
                },
            )

        if self._summary_state["count"] >= self._summary_interval:
            count = self._summary_state["count"]
            avg_iteration = self._summary_state["latency_total"] / max(1, count)
            avg_pipeline = self._summary_state["total_latency_total"] / max(1, count)
            self.logger.info(
                "Detection loop summary",
                extra={
                    "iterations": count,
                    "avg_iteration_ms": round(avg_iteration, 2),
                    "avg_pipeline_ms": round(avg_pipeline, 2),
                    "published": self._summary_state["published"],
                    "rate_limited": self._summary_state["rate_limited"],
                    "errors": self._summary_state["errors"],
                },
            )
            if self._max_detection_gap > 0:
                with self._metrics_lock:
                    last_detection = self._metrics.get("last_detection_time")
                if isinstance(last_detection, (int, float)) and last_detection > 0:
                    gap = time.time() - last_detection
                    if gap > self._max_detection_gap:
                        self.logger.warning(
                            "Detection gap exceeds threshold",
                            extra={
                                "gap_seconds": round(gap, 2),
                                "threshold_seconds": self._max_detection_gap,
                            },
                        )

            self._summary_state = {
                "count": 0,
                "latency_total": 0.0,
                "total_latency_total": 0.0,
                "published": 0,
                "rate_limited": 0,
                "errors": 0,
            }
