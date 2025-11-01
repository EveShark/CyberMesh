"""
Real-time detection loop - continuously polls telemetry and detects anomalies.

Runs in background thread, publishes detections to Kafka.
"""

import logging
import threading
import time
from typing import Optional, Dict, Any, Callable, List


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
        
        self.logger.info(
            f"DetectionLoop initialized: interval={self._interval}s, "
            f"timeout={self._timeout}s"
        )
    
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
                            'detection_timestamp': time.time(),
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
                        
                        self.publisher.publish_anomaly(
                            anomaly_id=anomaly_id,
                            anomaly_type=anomaly_type,
                            source='detection_loop',
                            severity=severity,
                            confidence=confidence,
                            payload=payload_bytes,
                            model_version='v1.0.0'
                        )
                        
                        self._increment_metric("detections_published")
                        
                        self.logger.info(
                            f"Published anomaly: {anomaly_type} "
                            f"(confidence={confidence:.2f}, severity={severity})"
                        )
                        
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
                                self.publisher.producer.producer.flush(timeout=10)
                                
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
            
            # 5. Sleep until next interval
            iteration_time = time.time() - iteration_start
            sleep_time = max(0, self._interval - iteration_time)
            
            if sleep_time > 0:
                time.sleep(sleep_time)
        
        self.logger.info("Detection loop exited")
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
