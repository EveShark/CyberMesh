"""
Real-time detection loop - continuously polls telemetry and detects anomalies.

Runs in background thread, publishes detections to Kafka.
"""

import threading
import time
import logging
from typing import Optional, Dict, Any


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
        logger: Optional[logging.Logger] = None
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
            "loop_iterations": 0,
            "avg_latency_ms": 0.0
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
            
            try:
                # 1. Run detection pipeline
                detection_start = time.time()
                result = self.pipeline.process()
                detection_latency = (time.time() - detection_start) * 1000  # ms
                
                # 2. Check if we should publish
                if result.decision and result.decision.should_publish:
                    self._increment_metric("detections_total")
                    
                    # Check rate limit
                    if self.rate_limiter.acquire():
                        # Publish anomaly
                        import uuid
                        
                        anomaly_id = str(uuid.uuid4())
                        anomaly_type = result.decision.threat_type.value
                        severity = max(1, min(10, int(result.decision.final_score * 10)))
                        confidence = result.decision.confidence
                        evidence = result.decision.metadata.get('evidence', b'')
                        
                        self.publisher.publish_anomaly(
                            anomaly_id=anomaly_id,
                            anomaly_type=anomaly_type,
                            source='detection_loop',
                            severity=severity,
                            confidence=confidence,
                            payload=evidence,
                            model_version='v1.0.0'
                        )
                        
                        self._increment_metric("detections_published")
                        
                        self.logger.info(
                            f"Published anomaly: {anomaly_type} "
                            f"(confidence={confidence:.2f}, severity={severity})"
                        )
                    else:
                        self._increment_metric("detections_rate_limited")
                        self.logger.warning("Rate limited detection")
                    
                    # Update last detection time
                    with self._metrics_lock:
                        self._metrics["last_detection_time"] = time.time()
                elif result.error:
                    self.logger.debug(f"Pipeline returned error: {result.error}")
                
                # 3. Update metrics
                self._increment_metric("loop_iterations")
                self._update_avg_latency(detection_latency)
                
                self.logger.debug(
                    f"Detection iteration complete: "
                    f"published={result.decision.should_publish if result.decision else False}, "
                    f"{detection_latency:.1f}ms"
                )
                
            except Exception as e:
                self._increment_metric("errors")
                self.logger.error(
                    f"Detection loop error: {e}",
                    exc_info=True,
                    extra={"error": str(e)}
                )
            
            # 5. Sleep until next interval
            iteration_time = time.time() - iteration_start
            sleep_time = max(0, self._interval - iteration_time)
            
            if sleep_time > 0:
                time.sleep(sleep_time)
        
        self.logger.info("Detection loop exited")
    
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
