"""
Service Manager - Orchestrates all AI service components.

Lifecycle:
    UNINITIALIZED -> initialize() -> INITIALIZED
    INITIALIZED -> start() -> STARTING -> RUNNING
    RUNNING -> stop() -> STOPPING -> STOPPED

Security:
    - Validates all state transitions
    - Prevents double-initialization
    - Graceful shutdown with timeout (30s max)
    - Saves crypto state on shutdown
    - Thread-safe operations
"""
import math
import os
import threading
import time
from collections import deque
from datetime import datetime
from enum import Enum
from typing import Optional, Dict, Any, Callable, List, Tuple

from ..config import Settings
from ..config.settings import FeedbackConfig
from ..logging import get_logger
from ..utils import Signer, NonceManager, CircuitBreaker
from ..utils.errors import ServiceError
from ..kafka import AIProducer, AIConsumer
from ..contracts import AnomalyMessage, EvidenceMessage, PolicyMessage
from .crypto_setup import (
    initialize_signer,
    initialize_nonce_manager,
    shutdown_nonce_manager,
)
from .handlers import MessageHandlers
from .publisher import MessagePublisher


class ServiceState(Enum):
    """Service lifecycle states."""
    UNINITIALIZED = "uninitialized"
    INITIALIZED = "initialized"
    STARTING = "starting"
    RUNNING = "running"
    STOPPING = "stopping"
    STOPPED = "stopped"
    ERROR = "error"


class ServiceManager:
    """
    Orchestrates AI service lifecycle and components.
    
    Manages:
        - Configuration loading
        - Cryptographic components (Signer, NonceManager)
        - Kafka producer/consumer
        - Service lifecycle (initialize, start, stop)
        - Health checks and metrics
        
    Thread Safety:
        - All public methods are thread-safe
        - Uses lock for state transitions
        - Safe to call from multiple threads
        
    Example:
        manager = ServiceManager()
        manager.initialize()
        manager.start()
        
        # Send messages
        manager.send_anomaly(anomaly_msg)
        
        # Graceful shutdown
        manager.stop()
    """
    
    # Shutdown timeout in seconds
    SHUTDOWN_TIMEOUT = 30
    
    def __init__(self):
        """
        Create service manager.
        
        Note: Manager starts in UNINITIALIZED state.
        Call initialize() to load configuration and create components.
        """
        self._state = ServiceState.UNINITIALIZED
        self._state_lock = threading.RLock()
        
        # Components (initialized in initialize())
        self.settings: Optional[Settings] = None
        self.logger = None
        self.signer: Optional[Signer] = None
        self.nonce_manager: Optional[NonceManager] = None
        self.circuit_breaker: Optional[CircuitBreaker] = None
        self.producer: Optional[AIProducer] = None
        self.consumer: Optional[AIConsumer] = None
        self.handlers: Optional[MessageHandlers] = None
        self.publisher: Optional[MessagePublisher] = None
        
        # ML pipeline (Phase 6.1)
        self.detection_pipeline = None
        self.ml_metrics = None
        
        # Feedback loop (Phase 7)
        self.feedback_service = None
        
        # Real-time detection (Phase 8)
        self.detection_loop = None
        self.rate_limiter = None
        
        # State tracking
        self._start_time: Optional[datetime] = None
        self._error: Optional[str] = None
        self._running = False
        
        # Metrics
        self._metrics = {
            "messages_sent": 0,
            "messages_received": 0,
            "messages_failed": 0,
            "errors": 0,
        }
        self._metrics_lock = threading.Lock()

        # Cached detection metrics for API exposure
        self._cached_detection_metrics = {
            "running": False,
            "metrics": {
                "detections_total": 0,
                "detections_published": 0,
                "detections_rate_limited": 0,
                "errors": 0,
                "last_detection_time": None,
                "last_iteration_time": None,
                "loop_iterations": 0,
                "avg_latency_ms": 0.0,
                "last_latency_ms": 0.0,
            },
            "last_updated": None,
        }

        self._cached_detection_snapshot = {
            "running": False,
            "status": "unknown",
            "healthy": False,
            "blocking": False,
            "issues": [],
            "message": "",
            "metrics": self._cached_detection_metrics["metrics"],
            "last_updated": None,
            "history_staleness_seconds": None,
        }
        self._last_loop_status = "unknown"
        self._last_loop_blocking = False

        # Detection history (for API exposure)

        self._history_staleness_seconds = int(os.getenv("DETECTION_HISTORY_STALE_SECONDS", "900"))
        self._history_lock = threading.Lock()
        self._detection_history = deque(maxlen=500)

        # Aggregated engine analytics
        self._engine_metrics_lock = threading.RLock()
        self._engine_metrics = {}
        self._variant_metrics_lock = threading.RLock()
        self._variant_metrics = {}
        self._timestamp_skew_tolerance = 600
        self._timestamp_backlog_tolerance = 3600
    
    @property
    def state(self) -> ServiceState:
        """Get current service state (thread-safe)."""
        with self._state_lock:
            return self._state
    
    def _set_state(self, new_state: ServiceState):
        """Set service state (internal use only)."""
        with self._state_lock:
            old_state = self._state
            self._state = new_state
            
            if self.logger:
                self.logger.info(
                    "State transition",
                    extra={"from": old_state.value, "to": new_state.value}
                )
    
    def initialize(self, settings: Settings) -> None:
        """
        Initialize service manager and all components.
        
        Args:
            settings: Service configuration
            
        Raises:
            ServiceError: If already initialized or initialization fails
            
        Security:
            - Validates configuration
            - Initializes crypto components with security checks
            - Sets up Kafka with TLS/SASL
            - Fails fast on any error
        """
        with self._state_lock:
            # Guard: Prevent double-initialization
            if self._state != ServiceState.UNINITIALIZED:
                raise ServiceError(
                    f"Cannot initialize: already in state {self._state.value}. "
                    f"Expected: {ServiceState.UNINITIALIZED.value}"
                )
            
            try:
                # Store settings
                self.settings = settings
                
                # Initialize logger first (needed for all subsequent steps)
                self.logger = get_logger("service_manager")
                self.logger.info(
                    "Initializing service manager",
                    extra={
                        "environment": settings.environment,
                        "node_id": settings.node_id,
                    }
                )
                
                # Initialize cryptographic components
                self.logger.info("Initializing cryptographic components")
                self.signer = initialize_signer(settings, self.logger)
                self.nonce_manager = initialize_nonce_manager(settings, self.logger)
                
                # Initialize circuit breaker
                self.logger.info("Initializing circuit breaker")
                self.circuit_breaker = CircuitBreaker(
                    failure_threshold=5,
                    timeout_seconds=30,
                    recovery_threshold=2
                )
                
                # Initialize Kafka producer
                self.logger.info("Initializing Kafka producer")
                self.producer = AIProducer(
                    config=settings,
                    circuit_breaker=self.circuit_breaker
                )
                
                # Initialize backend validator trust store (for signature verification)
                self.logger.info("Initializing backend validator trust store")
                from ..contracts.commit import CommitEvent
                from ..contracts.policy import PolicyUpdateEvent
                keys_dir = settings.backend_validators_keys_dir if hasattr(settings, 'backend_validators_keys_dir') else None
                CommitEvent.initialize_trust_store(keys_dir)
                PolicyUpdateEvent.initialize_trust_store(keys_dir)
                self.logger.info("Trust store initialized for commit and policy verification")
                
                # Initialize Kafka consumer
                self.logger.info("Initializing Kafka consumer")
                consumer_topics = [
                    settings.kafka_topics.control_reputation,
                    settings.kafka_topics.control_policy,
                    settings.kafka_topics.control_policy_ack,
                    settings.kafka_topics.control_evidence,
                ]

                self.consumer = AIConsumer(
                    config=settings,
                    logger=self.logger,
                    topics=consumer_topics,
                )
                
                # Initialize message handlers (will set service_manager after ML pipeline init)
                self.logger.info("Initializing message handlers")
                self.handlers = MessageHandlers(logger=self.logger, service_manager=None)
                
                # Register handlers with consumer
                self.consumer.register_handler("reputation", self.handlers.handle_reputation_event)
                self.consumer.register_handler("policy_update", self.handlers.handle_policy_update_event)
                self.consumer.register_handler("policy_ack", self.handlers.handle_policy_ack_event)
                self.consumer.register_handler("evidence_request", self.handlers.handle_evidence_request_event)
                
                self.logger.info("All message handlers registered")
                
                # Initialize message publisher
                self.logger.info("Initializing message publisher")
                self.publisher = MessagePublisher(
                    producer=self.producer,
                    signer=self.signer,
                    nonce_manager=self.nonce_manager,
                    circuit_breaker=self.circuit_breaker,
                    logger=self.logger
                )
                
                self.logger.info("Message publisher initialized")
                
                # Initialize ML detection pipeline
                self.logger.info("Initializing ML detection pipeline")
                self._initialize_ml_pipeline(settings)
                self.logger.info("ML detection pipeline initialized")
                
                # Initialize feedback loop (Phase 7)
                self.logger.info("Initializing feedback service")
                self._initialize_feedback_service(settings)
                self.logger.info("Feedback service initialized")
                
                # Wire handlers to service_manager (circular dependency resolved)
                self.handlers.service_manager = self
                
                # Mark as initialized
                self._set_state(ServiceState.INITIALIZED)
                self.logger.info("Service manager initialized successfully")
                
            except Exception as e:
                self._set_state(ServiceState.ERROR)
                self._error = str(e)
                self.logger.error(
                    "Service initialization failed",
                    exc_info=True,
                    extra={"error": str(e)}
                )
                raise ServiceError(f"Initialization failed: {e}")
    
    def start(self) -> None:
        """
        Start service (start Kafka consumer, enable message sending).
        
        Raises:
            ServiceError: If not initialized or already running
            
        Thread Safety:
            - Safe to call from any thread
            - Consumer runs in background daemon thread
            - Main thread remains responsive
        """
        with self._state_lock:
            # Guard: Must be initialized first
            if self._state != ServiceState.INITIALIZED:
                raise ServiceError(
                    f"Cannot start: current state is {self._state.value}. "
                    f"Must call initialize() first."
                )
            
            try:
                self._set_state(ServiceState.STARTING)
                self.logger.info("Starting service")
                
                # Start Kafka consumer
                self.logger.info("Starting Kafka consumer")
                self.consumer.start()
                
                # Start feedback service (Phase 7)
                if self.feedback_service:
                    self.logger.info("Starting feedback service")
                    self.feedback_service.start()
                
                # Start detection loop (Phase 8)
                if self.detection_loop:
                    self.logger.info("Starting detection loop")
                    self.detection_loop.start()
                
                # Mark as running
                self._running = True
                self._start_time = datetime.utcnow()
                self._set_state(ServiceState.RUNNING)
                
                self.logger.info(
                    "Service started successfully",
                    extra={"start_time": self._start_time.isoformat()}
                )
                
            except Exception as e:
                self._set_state(ServiceState.ERROR)
                self._error = str(e)
                self.logger.error(
                    "Service start failed",
                    exc_info=True,
                    extra={"error": str(e)}
                )
                raise ServiceError(f"Start failed: {e}")
    
    def stop(self, timeout: Optional[int] = None) -> None:
        """
        Stop service gracefully.
        
        Args:
            timeout: Max seconds to wait for graceful shutdown (default: 30)
            
        Shutdown Steps:
            1. Set running flag to False
            2. Stop Kafka consumer (waits for in-flight messages)
            3. Flush Kafka producer (send buffered messages)
            4. Save nonce manager state
            5. Close all connections
            
        Thread Safety:
            - Idempotent (safe to call multiple times)
            - Waits for background threads to finish
            - Force-stops after timeout
        """
        with self._state_lock:
            # Idempotent: Already stopped
            if self._state in (ServiceState.STOPPED, ServiceState.STOPPING):
                self.logger.info("Service already stopped or stopping")
                return
            
            # Can stop from any state except UNINITIALIZED
            if self._state == ServiceState.UNINITIALIZED:
                raise ServiceError("Cannot stop: service not initialized")
            
            try:
                self._set_state(ServiceState.STOPPING)
                self.logger.info(
                    "Stopping service",
                    extra={"timeout": timeout or self.SHUTDOWN_TIMEOUT}
                )
                
                shutdown_timeout = timeout or self.SHUTDOWN_TIMEOUT
                shutdown_start = time.time()
                
                # Stop accepting new messages
                self._running = False
                
                # Stop detection loop (Phase 8)
                if self.detection_loop:
                    self.logger.info("Stopping detection loop")
                    try:
                        self.detection_loop.stop(timeout=10)
                    except Exception as e:
                        self.logger.error(
                            "Error stopping detection loop",
                            exc_info=True,
                            extra={"error": str(e)}
                        )
                
                # Stop feedback service (Phase 7)
                if self.feedback_service:
                    self.logger.info("Stopping feedback service")
                    try:
                        self.feedback_service.stop()
                    except Exception as e:
                        self.logger.error(
                            "Error stopping feedback service",
                            exc_info=True,
                            extra={"error": str(e)}
                        )
                
                # Stop Kafka consumer
                if self.consumer:
                    self.logger.info("Stopping Kafka consumer")
                    try:
                        self.consumer.stop()
                    except Exception as e:
                        self.logger.error(
                            "Error stopping consumer",
                            exc_info=True,
                            extra={"error": str(e)}
                        )
                
                # Flush producer (send buffered messages)
                if self.producer:
                    self.logger.info("Flushing Kafka producer")
                    try:
                        self.producer.flush()
                    except Exception as e:
                        self.logger.error(
                            "Error flushing producer",
                            exc_info=True,
                            extra={"error": str(e)}
                        )
                
                # Save nonce state
                if self.nonce_manager and self.settings:
                    self.logger.info("Saving nonce state")
                    try:
                        shutdown_nonce_manager(
                            self.nonce_manager,
                            self.settings.nonce_state_path,
                            int(self.settings.node_id),
                            self.logger
                        )
                    except Exception as e:
                        self.logger.error(
                            "Error saving nonce state",
                            exc_info=True,
                            extra={"error": str(e)}
                        )
                
                # Close producer
                if self.producer:
                    self.logger.info("Closing Kafka producer")
                    try:
                        self.producer.close()
                    except Exception as e:
                        self.logger.error(
                            "Error closing producer",
                            exc_info=True,
                            extra={"error": str(e)}
                        )

                # Close telemetry resources
                if self.detection_pipeline:
                    telemetry = getattr(self.detection_pipeline, "telemetry", None)
                    if telemetry and hasattr(telemetry, "close"):
                        try:
                            telemetry.close()
                        except Exception as e:
                            self.logger.debug(
                                "Error closing telemetry source",
                                exc_info=True,
                                extra={"error": str(e)}
                            )
                
                # Check if we exceeded timeout
                elapsed = time.time() - shutdown_start
                if elapsed > shutdown_timeout:
                    self.logger.warning(
                        "Graceful shutdown exceeded timeout",
                        extra={
                            "elapsed": elapsed,
                            "timeout": shutdown_timeout
                        }
                    )
                
                # Mark as stopped
                self._set_state(ServiceState.STOPPED)
                self.logger.info(
                    "Service stopped",
                    extra={"shutdown_duration": elapsed}
                )
                
            except Exception as e:
                self._set_state(ServiceState.ERROR)
                self._error = str(e)
                self.logger.error(
                    "Error during shutdown",
                    exc_info=True,
                    extra={"error": str(e)}
                )
                raise ServiceError(f"Shutdown failed: {e}")
    
    def send_anomaly(self, message: AnomalyMessage) -> bool:
        """
        Send anomaly message to backend.
        
        Args:
            message: Signed anomaly message
            
        Returns:
            True if sent successfully
            
        Raises:
            ServiceError: If service not running
        """
        if not self._running:
            raise ServiceError("Cannot send: service not running")
        
        try:
            result = self.producer.send_anomaly(message)
            
            with self._metrics_lock:
                if result:
                    self._metrics["messages_sent"] += 1
                else:
                    self._metrics["messages_failed"] += 1
            
            return result
            
        except Exception as e:
            with self._metrics_lock:
                self._metrics["messages_failed"] += 1
                self._metrics["errors"] += 1
            
            self.logger.error(
                "Failed to send anomaly",
                exc_info=True,
                extra={"error": str(e)}
            )
            raise
    
    def send_evidence(self, message: EvidenceMessage) -> bool:
        """Send evidence message to backend."""
        if not self._running:
            raise ServiceError("Cannot send: service not running")
        
        try:
            result = self.producer.send_evidence(message)
            
            with self._metrics_lock:
                if result:
                    self._metrics["messages_sent"] += 1
                else:
                    self._metrics["messages_failed"] += 1
            
            return result
            
        except Exception as e:
            with self._metrics_lock:
                self._metrics["messages_failed"] += 1
                self._metrics["errors"] += 1
            
            self.logger.error(
                "Failed to send evidence",
                exc_info=True,
                extra={"error": str(e)}
            )
            raise
    
    def send_policy(self, message: PolicyMessage) -> bool:
        """Send policy message to backend."""
        if not self._running:
            raise ServiceError("Cannot send: service not running")
        
        try:
            result = self.producer.send_policy(message)
            
            with self._metrics_lock:
                if result:
                    self._metrics["messages_sent"] += 1
                else:
                    self._metrics["messages_failed"] += 1
            
            return result
            
        except Exception as e:
            with self._metrics_lock:
                self._metrics["messages_failed"] += 1
                self._metrics["errors"] += 1
            
            self.logger.error(
                "Failed to send policy",
                exc_info=True,
                extra={"error": str(e)}
            )
            raise
    
    def register_handler(self, message_type: str, handler: Callable) -> None:
        """
        Register handler for control messages from backend.
        
        Args:
            message_type: Type of message ("commit", "reputation", "policy_update", "evidence_request")
            handler: Callable that takes message object
            
        Example:
            def on_commit(commit_event):
                logger.info(f"Block committed: {commit_event.height}")
            
            manager.register_handler("commit", on_commit)
        """
        if not self.consumer:
            raise ServiceError("Cannot register handler: consumer not initialized")
        
        self.consumer.register_handler(message_type, handler)
        self.logger.info(
            "Handler registered",
            extra={"message_type": message_type}
        )
    
    def health_check(self) -> Dict[str, Any]:
        """
        Get service health status.
        
        Returns:
            Dictionary with detailed health information
            
        Format:
            {
                "status": "running" | "error" | "stopped",
                "state": "running",
                "uptime_seconds": 123.45,
                "start_time": "2025-01-03T12:00:00Z",
                "metrics": {
                    "messages_sent": 100,
                    "messages_received": 50,
                    "messages_failed": 2,
                    "errors": 1
                },
                "circuit_breaker": "closed" | "open" | "half_open",
                "error": "error message" (if any)
            }
        """
        with self._state_lock:
            health = {
                "status": "unknown",
                "state": self._state.value,
                "metrics": {},
            }
            
            # Overall status
            if self._state == ServiceState.RUNNING:
                health["status"] = "running"
            elif self._state == ServiceState.ERROR:
                health["status"] = "error"
                health["error"] = self._error
            elif self._state == ServiceState.STOPPED:
                health["status"] = "stopped"
            else:
                health["status"] = self._state.value
            
            # Uptime
            if self._start_time:
                uptime = (datetime.utcnow() - self._start_time).total_seconds()
                health["uptime_seconds"] = uptime
                health["start_time"] = self._start_time.isoformat()
            
            # Metrics
            with self._metrics_lock:
                health["metrics"] = dict(self._metrics)
            
            # Circuit breaker state
            if self.circuit_breaker:
                cb_state = self.circuit_breaker.state
                health["circuit_breaker"] = cb_state.value if hasattr(cb_state, 'value') else str(cb_state)
            
            # Component status
            health["components"] = {
                "signer": self.signer is not None,
                "nonce_manager": self.nonce_manager is not None,
                "producer": self.producer is not None,
                "consumer": self.consumer is not None,
                "handlers": self.handlers is not None,
                "publisher": self.publisher is not None,
            }
            
            # Handler metrics
            if self.handlers:
                health["handler_metrics"] = self.handlers.get_metrics()
            
            # Publisher metrics
            if self.publisher:
                health["publisher_metrics"] = self.publisher.get_metrics()
                health["pending_deliveries"] = self.publisher.get_pending_count()
            
            # Detection loop metrics (Phase 8)
            if self.detection_loop:
                snapshot = self.detection_loop.get_health_snapshot()

                metrics_copy = dict(snapshot.get("metrics", {}))
                issues_copy = list(snapshot.get("issues", []))

                cache_entry = {
                    "running": snapshot.get("running", False),
                    "metrics": metrics_copy,
                    "last_updated": snapshot.get("last_updated"),
                }
                self._cached_detection_metrics = cache_entry

                snapshot_copy = {
                    "running": snapshot.get("running", False),
                    "status": snapshot.get("status", "unknown"),
                    "healthy": snapshot.get("healthy", False),
                    "blocking": snapshot.get("blocking", False),
                    "issues": issues_copy,
                    "message": snapshot.get("message", ""),
                    "metrics": metrics_copy,
                    "last_updated": snapshot.get("last_updated"),
                    "history_staleness_seconds": None,
                    "seconds_since_last_iteration": snapshot.get("seconds_since_last_iteration"),
                    "seconds_since_last_detection": snapshot.get("seconds_since_last_detection"),
                }

                history_issue, history_staleness = self._evaluate_history_health(metrics_copy)
                snapshot_copy["history_staleness_seconds"] = history_staleness
                if history_issue:
                    if history_issue not in issues_copy:
                        issues_copy.append(history_issue)
                    if snapshot_copy["status"] == "ok":
                        snapshot_copy["status"] = "degraded"
                    if snapshot_copy["message"]:
                        snapshot_copy["message"] = f"{snapshot_copy['message']}; {history_issue}"
                    else:
                        snapshot_copy["message"] = history_issue

                self._cached_detection_snapshot = snapshot_copy

                if self.logger and (
                    snapshot_copy["status"] != self._last_loop_status
                    or snapshot_copy["blocking"] != self._last_loop_blocking
                ):
                    log_extra = {
                        "status": snapshot_copy["status"],
                        "issues": issues_copy,
                        "blocking": snapshot_copy["blocking"],
                    }
                    if snapshot_copy["status"] in ("critical", "stopped") or snapshot_copy["blocking"]:
                        self.logger.warning("Detection loop status changed", extra=log_extra)
                    else:
                        self.logger.info("Detection loop status changed", extra=log_extra)
                self._last_loop_status = snapshot_copy["status"]
                self._last_loop_blocking = snapshot_copy["blocking"]

                health["detection_loop"] = snapshot_copy
            else:
                # Serve cached snapshot when loop not initialized or stopped
                cached_snapshot = {
                    "running": self._cached_detection_snapshot.get("running", False),
                    "status": self._cached_detection_snapshot.get("status", "unknown"),
                    "healthy": self._cached_detection_snapshot.get("healthy", False),
                    "blocking": self._cached_detection_snapshot.get("blocking", False),
                    "issues": list(self._cached_detection_snapshot.get("issues", [])),
                    "message": self._cached_detection_snapshot.get("message", ""),
                    "metrics": dict(self._cached_detection_metrics.get("metrics", {})),
                    "last_updated": self._cached_detection_snapshot.get("last_updated"),
                    "history_staleness_seconds": self._cached_detection_snapshot.get("history_staleness_seconds"),
                    "seconds_since_last_iteration": self._cached_detection_snapshot.get("seconds_since_last_iteration"),
                    "seconds_since_last_detection": self._cached_detection_snapshot.get("seconds_since_last_detection"),
                }
                health["detection_loop"] = cached_snapshot
                self._last_loop_status = cached_snapshot["status"]
                self._last_loop_blocking = cached_snapshot["blocking"]
            
            # Rate limiter stats (Phase 8)
            if self.rate_limiter:
                health["rate_limiter"] = self.rate_limiter.get_stats()

            if self.producer:
                health["kafka_producer"] = self.producer.get_metrics()

            if self.consumer:
                health["kafka_consumer"] = self.consumer.get_metrics()
            
            return health
    
    def get_detection_metrics(self) -> Dict[str, Any]:
        """Return latest detection loop metrics snapshot."""
        with self._state_lock:
            return {
                "running": self._cached_detection_snapshot.get("running", False),
                "status": self._cached_detection_snapshot.get("status", "unknown"),
                "healthy": self._cached_detection_snapshot.get("healthy", False),
                "blocking": self._cached_detection_snapshot.get("blocking", False),
                "issues": list(self._cached_detection_snapshot.get("issues", [])),
                "message": self._cached_detection_snapshot.get("message", ""),
                "metrics": dict(self._cached_detection_metrics.get("metrics", {})),
                "last_updated": self._cached_detection_snapshot.get("last_updated"),
                "history_staleness_seconds": self._cached_detection_snapshot.get("history_staleness_seconds"),
                "seconds_since_last_iteration": self._cached_detection_snapshot.get("seconds_since_last_iteration"),
                "seconds_since_last_detection": self._cached_detection_snapshot.get("seconds_since_last_detection"),
            }

    def _evaluate_history_health(self, loop_metrics: Dict[str, Any]) -> Tuple[Optional[str], Optional[float]]:
        now = time.time()
        with self._history_lock:
            if not self._detection_history:
                iterations = loop_metrics.get("loop_iterations", 0) or 0
                published = loop_metrics.get("detections_published", 0) or 0
                if iterations >= 5 or published > 0:
                    return ("detection history empty (no events persisted)", None)
                return (None, None)

            latest_event = self._detection_history[0]
            timestamp = latest_event.get("timestamp")

        try:
            last_ts = float(timestamp)
        except (TypeError, ValueError):
            return ("detection history entry missing timestamp", None)

        staleness = max(0.0, now - last_ts)
        if staleness > self._history_staleness_seconds:
            message = (
                f"detection history stale ({staleness:.0f}s > {self._history_staleness_seconds}s)"
            )
            return (message, staleness)

        return (None, staleness)

    def get_handler_metrics(self) -> Dict[str, Any]:
        """
        Get message handler metrics.
        
        Returns:
            Dictionary with processed/failed counts per handler type
            
        Raises:
            ServiceError: If handlers not initialized
        """
        if not self.handlers:
            raise ServiceError("Handlers not initialized")
        
        return self.handlers.get_metrics()
    
    def get_publisher_metrics(self) -> Dict[str, Any]:
        """
        Get message publisher metrics.
        
        Returns:
            Dictionary with sent/failed counts per message type
            
        Raises:
            ServiceError: If publisher not initialized
        """
        if not self.publisher:
            raise ServiceError("Publisher not initialized")
        
        return self.publisher.get_metrics()
    
    def _initialize_ml_pipeline(self, settings: Settings) -> None:
        """
        Initialize ML detection pipeline (Phase 6.2).
        
        Components:
        - Telemetry source (file-based)
        - Feature extractor (network flows)
        - Engines: Rules + Math + ML (3 trained models)
        - Ensemble voter (weighted voting across 3 engines)
        - Evidence generator (Ed25519 signing)
        - Pipeline orchestrator (latency tracking)
        
        Args:
            settings: Service configuration
        """
        from ..ml.telemetry import FileTelemetrySource
        from ..ml.telemetry_postgres import PostgresTelemetrySource
        from ..ml.features_flow import FlowFeatureExtractor
        from ..ml.detectors import RulesEngine, MathEngine, MLEngine
        from ..ml.ensemble import EnsembleVoter
        from ..ml.evidence import EvidenceGenerator
        from ..ml.pipeline import DetectionPipeline
        from ..ml.metrics import PrometheusMetrics
        from ..ml.serving import ModelRegistry
        
        # Helper to get config values with defaults
        import os
        def get_config(key: str, default):
            # Try environment variable first (for new fields not in Settings)
            env_val = os.getenv(key)
            if env_val is not None:
                return env_val
            # Fall back to settings attribute
            try:
                return getattr(settings, key.lower(), default)
            except:
                return default
        
        # Configuration dictionary for ML components
        ml_config = {
            'TELEMETRY_FLOWS_PATH': get_config('TELEMETRY_FLOWS_PATH', 'data/telemetry/flows'),
            'TELEMETRY_FILES_PATH': get_config('TELEMETRY_FILES_PATH', 'data/telemetry/files'),
            'MODELS_PATH': get_config('MODELS_PATH', 'data/models'),
            'TELEMETRY_BATCH_SIZE': int(get_config('TELEMETRY_BATCH_SIZE', getattr(settings, 'telemetry_batch_size', 1000))),
            'ML_WEIGHT': float(get_config('ML_WEIGHT', 0.5)),
            'RULES_WEIGHT': float(get_config('RULES_WEIGHT', 0.3)),
            'MATH_WEIGHT': float(get_config('MATH_WEIGHT', 0.2)),
            'DDOS_THRESHOLD': float(get_config('DDOS_THRESHOLD', 0.7)),
            'MALWARE_THRESHOLD': float(get_config('MALWARE_THRESHOLD', 0.85)),
            'ANOMALY_THRESHOLD': float(get_config('ANOMALY_THRESHOLD', 0.75)),
            'MIN_CONFIDENCE': float(get_config('MIN_CONFIDENCE', 0.85)),
            'DDOS_PPS_THRESHOLD': float(get_config('DDOS_PPS_THRESHOLD', 100000)),
            'PORT_SCAN_THRESHOLD': int(get_config('PORT_SCAN_THRESHOLD', 500)),
            'MALWARE_ENTROPY_THRESHOLD': float(get_config('MALWARE_ENTROPY_THRESHOLD', 7.5)),
            'BASELINE_STATS_PATH': get_config('BASELINE_STATS_PATH', 'data/models/baseline_stats.json'),
        }
        ml_config['MAX_FLOWS_PER_ITERATION'] = int(get_config('MAX_FLOWS_PER_ITERATION', getattr(settings, 'max_flows_per_iteration', 100)))
        ml_config['STAGE_WARN_THRESHOLD_MS'] = float(get_config('STAGE_WARN_THRESHOLD_MS', getattr(settings, 'max_loop_latency_warning_ms', 3000)))
        
        # 1. Telemetry source - use PostgreSQL if configured
        telemetry_source_type = get_config('TELEMETRY_SOURCE_TYPE', 'file')
        if telemetry_source_type == 'postgres':
            db_config = {
                'host': get_config('DB_HOST', 'localhost'),
                'port': int(get_config('DB_PORT', 5432)),
                'dbname': get_config('DB_NAME', 'cybermesh'),
                'user': get_config('DB_USER', 'postgres'),
                'password': get_config('DB_PASSWORD', 'postgres'),
            }
            sslmode = get_config('DB_SSLMODE', None)
            if sslmode:
                db_config['sslmode'] = sslmode
            sslrootcert = get_config('DB_SSLROOTCERT', None)
            if sslrootcert:
                db_config['sslrootcert'] = sslrootcert
            sslcert = get_config('DB_SSLCERT', None)
            if sslcert:
                db_config['sslcert'] = sslcert
            sslkey = get_config('DB_SSLKEY', None)
            if sslkey:
                db_config['sslkey'] = sslkey
            table_name = get_config('DB_TABLE', 'test_ddos_binary')
            schema = 'curated' if 'test' in table_name else 'public'
            sample_table = get_config('DB_SAMPLE_TABLE', None)
            sample_strategy = get_config('DB_SAMPLE_STRATEGY', 'tablesample')
            sample_percent = float(get_config('DB_SAMPLE_PERCENT', 5.0))
            pool_min = int(get_config('DB_POOL_MIN_CONN', 1))
            pool_max = int(get_config('DB_POOL_MAX_CONN', 4))
            prefetch_enabled = str(get_config('DB_PREFETCH_ENABLED', 'false')).lower() in ('1', 'true', 'yes', 'on')
            telemetry_source = PostgresTelemetrySource(
                db_config=db_config, 
                sample_size=100,
                table_name=table_name,
                schema=schema,
                sample_table=sample_table if sample_table else None,
                sample_strategy=sample_strategy,
                sample_percent=sample_percent,
                pool_min=pool_min,
                pool_max=pool_max,
                prefetch_enabled=prefetch_enabled,
            )
            self.logger.info(
                'Using PostgreSQL telemetry source',
                extra={
                    'table': f"{schema}.{table_name}",
                    'sample_strategy': sample_strategy,
                    'sample_table': sample_table,
                    'sample_percent': sample_percent,
                    'pool_min': pool_min,
                    'pool_max': pool_max,
                    'prefetch_enabled': prefetch_enabled,
                }
            )
        else:
            telemetry_source = FileTelemetrySource(
                flows_path=ml_config['TELEMETRY_FLOWS_PATH'],
                files_path=ml_config['TELEMETRY_FILES_PATH']
            )
            self.logger.info('Using file telemetry source')
        
        # 2. Feature extractor: single 79-feature path
        feature_extractor = FlowFeatureExtractor()
        
        # 3. Model registry (loads 3 ML models)
        model_registry = ModelRegistry(
            models_path=ml_config['MODELS_PATH'],
            signer=self.signer
        )
        
        # 4. Detection engines (Rules + Math + ML)
        # DLQ emitter for ML validation faults (uses producer to send to DLQ topic)
        def _ml_dlq_emit(payload: dict):
            try:
                from confluent_kafka import Producer
                import json as _json, hashlib as _hashlib
                topic = settings.kafka_topics.dlq
                data = _json.dumps({
                    "component": payload.get("component", "ml_engine"),
                    "reason": payload.get("reason", "unknown"),
                    "variant": payload.get("variant"),
                    "error": payload.get("error"),
                    "expected": payload.get("expected"),
                    "got": payload.get("got"),
                    "timestamp": int(time.time()),
                    "node_id": settings.node_id,
                }, separators=(",", ":")).encode()
                key = _hashlib.sha256(data).digest()
                # Reuse existing producer if available
                if self.producer and getattr(self.producer, 'producer', None):
                    self.producer.producer.produce(topic, value=data, key=key)
                    self.producer.producer.flush(3)
            except Exception:
                # Metrics-only fallback
                if self.logger:
                    self.logger.warning("ML DLQ emit failed; logged only")

        # 7. Prometheus metrics
        self.ml_metrics = PrometheusMetrics()

        engines = [
            RulesEngine(config=ml_config),
            MathEngine(config=ml_config, baseline_path=ml_config['BASELINE_STATS_PATH']),
            MLEngine(registry=model_registry, config=ml_config, dlq_callback=_ml_dlq_emit, metrics=self.ml_metrics)
        ]
        
        # Log engine status
        for engine in engines:
            status = "READY" if engine.is_ready else "NOT READY"
            self.logger.info(f"Engine {engine.engine_type.value}: {status}")
        
        # 5. Ensemble voter
        ensemble = EnsembleVoter(config=ml_config)
        
        # 6. Evidence generator
        evidence_generator = EvidenceGenerator(
            signer=self.signer,
            nonce_manager=self.nonce_manager,
            node_id=settings.node_id
        )
        
        # 8. Detection pipeline
        self.detection_pipeline = DetectionPipeline(
            telemetry_source=telemetry_source,
            feature_extractor=feature_extractor,
            engines=engines,
            ensemble=ensemble,
            evidence_generator=evidence_generator,
            circuit_breaker=self.circuit_breaker,
            config=ml_config,
            metrics=self.ml_metrics,
        )
        
        # Record engine status in metrics
        for engine in engines:
            self.ml_metrics.record_engine_status(
                engine_type=engine.engine_type.value,
                is_ready=engine.is_ready
            )
        
        # 9. Initialize detection loop (Phase 8)
        self._initialize_detection_loop(settings, ml_config)
        
        self.logger.info("ML detection pipeline components initialized")
    
    def _initialize_feedback_service(self, settings: Settings) -> None:
        """
        Initialize feedback service (Phase 7).
        
        Components:
        - FeedbackService (orchestrator)
        - AdaptiveDetection (wired into ensemble)
        - Starts AIConsumer for backend messages
        
        Args:
            settings: Service configuration
        """
        from ..feedback.service import FeedbackService
        from ..ml.adaptive import AdaptiveDetection
        
        self.feedback_service = FeedbackService(
            settings,
            self.logger,
            extra_commit_handler=self.handlers.handle_commit_event,
        )
        
        # Wire AdaptiveDetection into existing ensemble
        if self.detection_pipeline and self.detection_pipeline.ensemble:
            adaptive = AdaptiveDetection(self.feedback_service)
            self.detection_pipeline.ensemble.adaptive = adaptive
            self.logger.info("Adaptive detection wired into ensemble")
        
        if self.detection_loop:
            try:
                self.detection_loop.set_tracker(self.feedback_service.tracker)
            except Exception as err:
                self.logger.warning(
                    "Failed to attach tracker to detection loop",
                    extra={"error": str(err)}
                )

        self.logger.info("Feedback service components initialized")
    
    def _initialize_detection_loop(self, settings: Settings, ml_config: Dict) -> None:
        """
        Initialize real-time detection loop (Phase 8).
        
        Components:
        - RateLimiter (token bucket for publish rate limiting)
        - DetectionLoop (continuous detection in background thread)
        
        Args:
            settings: Service configuration
            ml_config: ML pipeline configuration
        """
        from ..service.rate_limiter import RateLimiter
        from ..service.detection_loop import DetectionLoop
        
        # Create rate limiter
        max_detections_per_second = getattr(
            settings,
            'max_detections_per_second',
            100
        )
        self.rate_limiter = RateLimiter(max_per_second=max_detections_per_second)
        
        # Create detection loop
        detection_config = {
            'DETECTION_INTERVAL': getattr(settings, 'detection_interval', 5),
            'DETECTION_TIMEOUT': getattr(settings, 'detection_timeout', 30),
            'TELEMETRY_BATCH_SIZE': getattr(settings, 'telemetry_batch_size', 1000),
            'MAX_FLOWS_PER_ITERATION': getattr(settings, 'max_flows_per_iteration', 100),
            'DETECTION_SUMMARY_INTERVAL': getattr(settings, 'detection_summary_interval', 10),
            'MAX_ITERATION_LAG_SECONDS': getattr(settings, 'max_iteration_lag_seconds', 20),
            'MAX_DETECTION_GAP_SECONDS': getattr(settings, 'max_detection_gap_seconds', 600),
            'MAX_LOOP_LATENCY_WARNING_MS': getattr(settings, 'max_loop_latency_warning_ms', 3000),
            'DETECTION_PUBLISH_FLUSH': getattr(settings, 'detection_publish_flush', False),
            'TRACKER_SAMPLE_WINDOW_SIZE': getattr(settings, 'tracker_sample_window_size', 500),
            'TRACKER_SAMPLE_CAP': getattr(settings, 'tracker_sample_cap', 100),
            'TRACKER_BATCH_SIZE': getattr(settings, 'tracker_batch_size', 50),
            'TRACKER_FLUSH_INTERVAL_SECONDS': getattr(settings, 'tracker_flush_interval_seconds', 5),
        }

        policy_cfg = getattr(settings, "policy_publishing", None)
        if policy_cfg is not None:
            detection_config['POLICY_PUBLISHING'] = policy_cfg

        # Align sampling configuration with calibration requirements to avoid
        # starving the feedback loop of training samples. When persistence is
        # disabled we can safely capture every event in-memory; otherwise ensure
        # the sampler can gather at least the minimum needed for calibration.
        feedback_cfg = getattr(settings, "feedback", None)
        if feedback_cfg is None:
            env_flag = os.getenv("FEEDBACK_DISABLE_PERSISTENCE")
            disable_persistence = env_flag.lower() in ("true", "1", "yes", "on") if env_flag else False
            feedback_cfg = FeedbackConfig(disable_persistence=disable_persistence)
        else:
            disable_persistence = getattr(feedback_cfg, "disable_persistence", False)

        sample_window = int(detection_config['TRACKER_SAMPLE_WINDOW_SIZE'])
        sample_cap = int(detection_config['TRACKER_SAMPLE_CAP'])
        calibration_min = max(1, getattr(feedback_cfg, 'calibration_min_samples', 1000))

        effective_window = sample_window
        effective_cap = sample_cap

        if disable_persistence:
            effective_window = max(sample_window, calibration_min)
            effective_cap = effective_window
        else:
            if sample_window < calibration_min:
                effective_window = calibration_min
            required_cap = min(effective_window, calibration_min)
            if sample_cap < required_cap:
                effective_cap = required_cap

        detection_config['TRACKER_SAMPLE_WINDOW_SIZE'] = int(effective_window)
        detection_config['TRACKER_SAMPLE_CAP'] = int(effective_cap)

        if effective_window != sample_window or effective_cap != sample_cap:
            self.logger.info(
                "Adjusted tracker sampling parameters",
                extra={
                    "disable_persistence": disable_persistence,
                    "calibration_min_samples": calibration_min,
                    "sample_window_original": sample_window,
                    "sample_cap_original": sample_cap,
                    "sample_window_effective": effective_window,
                    "sample_cap_effective": effective_cap,
                },
            )

        self._timestamp_skew_tolerance = getattr(settings, 'timestamp_skew_tolerance_seconds', 600)
        self._timestamp_backlog_tolerance = max(self._timestamp_skew_tolerance * 6, 3600)
        
        self.detection_loop = DetectionLoop(
            pipeline=self.detection_pipeline,
            publisher=self.publisher,
            rate_limiter=self.rate_limiter,
            config=detection_config,
            logger=self.logger,
            metrics=self.ml_metrics,
            event_recorder=self.record_detection_event,
            engine_metrics_callback=self.record_engine_metrics,
        )
        
        self.logger.info(
            f"Detection loop initialized: interval={detection_config['DETECTION_INTERVAL']}s, "
            f"rate_limit={max_detections_per_second}/s"
        )

    def record_engine_metrics(self, engines: List[Dict[str, Any]], variants: List[Dict[str, Any]]) -> None:
        """Record per-iteration engine and variant analytics."""

        now = time.time()

        if engines:
            with self._engine_metrics_lock:
                for snapshot in engines:
                    engine = snapshot.get("engine")
                    if not engine:
                        continue

                    entry = self._engine_metrics.setdefault(
                        engine,
                        {
                            "engine": engine,
                            "candidates": 0,
                            "published": 0,
                            "confidence_sum": 0.0,
                            "confidence_samples": 0,
                            "first_seen": now,
                            "last_updated": now,
                            "last_latency_ms": None,
                            "threat_types": set(),
                        },
                    )

                    candidates = int(snapshot.get("candidates", 0))
                    published = int(snapshot.get("published", 0))
                    confidence_sum = float(snapshot.get("confidence_sum", 0.0))
                    threat_types = snapshot.get("threat_types", [])

                    entry["candidates"] += candidates
                    entry["published"] += published
                    entry["confidence_sum"] += confidence_sum
                    entry["confidence_samples"] += max(0, candidates)
                    entry["last_updated"] = snapshot.get("timestamp", now)
                    entry["last_latency_ms"] = snapshot.get("iteration_latency_ms")
                    entry["threat_types"].update(threat_types)

        if variants:
            with self._variant_metrics_lock:
                for snapshot in variants:
                    variant = snapshot.get("variant")
                    if not variant:
                        continue

                    entry = self._variant_metrics.setdefault(
                        variant,
                        {
                            "variant": variant,
                            "total": 0,
                            "published": 0,
                            "confidence_sum": 0.0,
                            "confidence_samples": 0,
                            "engines": set(),
                            "threat_types": set(),
                            "first_seen": now,
                            "last_updated": now,
                        },
                    )

                    total = int(snapshot.get("total", 0))
                    published = int(snapshot.get("published", 0))
                    confidence_sum = float(snapshot.get("confidence_sum", 0.0))

                    entry["total"] += total
                    entry["published"] += published
                    entry["confidence_sum"] += confidence_sum
                    entry["confidence_samples"] += max(0, total)
                    entry["last_updated"] = snapshot.get("timestamp", now)
                    if snapshot.get("engine"):
                        entry["engines"].add(snapshot["engine"])
                    entry["threat_types"].update(snapshot.get("threat_types", []))

    def get_engine_metrics_summary(self) -> List[Dict[str, Any]]:
        """Return aggregated per-engine metrics for API payloads."""

        now = time.time()
        summary: List[Dict[str, Any]] = []
        with self._engine_metrics_lock:
            for engine, data in self._engine_metrics.items():
                candidates = max(0, int(data.get("candidates", 0)))
                published = max(0, int(data.get("published", 0)))
                confidence_samples = max(0, int(data.get("confidence_samples", 0)))
                confidence_sum = float(data.get("confidence_sum", 0.0))
                first_seen = float(data.get("first_seen", now))
                elapsed_minutes = max((now - first_seen) / 60.0, 0.0001)

                avg_confidence = None
                if confidence_samples > 0:
                    avg_confidence = confidence_sum / confidence_samples

                throughput = candidates / elapsed_minutes if candidates > 0 else 0.0
                publish_ratio = float(published) / candidates if candidates > 0 else 0.0

                summary.append(
                    {
                        "engine": engine,
                        "ready": self._is_engine_ready(engine),
                        "candidates": candidates,
                        "published": published,
                        "publish_ratio": publish_ratio,
                        "throughput_per_minute": throughput,
                        "avg_confidence": avg_confidence,
                        "threat_types": sorted(data.get("threat_types", set())),
                        "last_latency_ms": data.get("last_latency_ms"),
                        "last_updated": data.get("last_updated"),
                    }
                )

        summary.sort(key=lambda item: item.get("throughput_per_minute", 0.0), reverse=True)
        return summary

    def get_variant_metrics_summary(self, limit: int = 20) -> List[Dict[str, Any]]:
        """Return aggregated variant metrics sorted by total activity."""

        now = time.time()
        limit = max(1, min(limit, 100))
        items: List[Dict[str, Any]] = []
        with self._variant_metrics_lock:
            for variant, data in self._variant_metrics.items():
                total = max(0, int(data.get("total", 0)))
                published = max(0, int(data.get("published", 0)))
                confidence_samples = max(0, int(data.get("confidence_samples", 0)))
                confidence_sum = float(data.get("confidence_sum", 0.0))
                first_seen = float(data.get("first_seen", now))
                elapsed_minutes = max((now - first_seen) / 60.0, 0.0001)

                avg_confidence = None
                if confidence_samples > 0:
                    avg_confidence = confidence_sum / confidence_samples

                publish_ratio = float(published) / total if total > 0 else 0.0
                throughput = total / elapsed_minutes if total > 0 else 0.0

                items.append(
                    {
                        "variant": variant,
                        "engines": sorted(data.get("engines", set())),
                        "total": total,
                        "published": published,
                        "publish_ratio": publish_ratio,
                        "throughput_per_minute": throughput,
                        "avg_confidence": avg_confidence,
                        "threat_types": sorted(data.get("threat_types", set())),
                        "last_updated": data.get("last_updated"),
                    }
                )

        items.sort(key=lambda item: item.get("total", 0), reverse=True)
        return items[:limit]

    def _is_engine_ready(self, engine: str) -> bool:
        if not self.detection_pipeline:
            return False
        try:
            engines = getattr(self.detection_pipeline, "engines", [])
        except Exception:
            return False

        for detector in engines:
            try:
                engine_type = getattr(detector, "engine_type")
                if engine_type and engine_type.value == engine:
                    return bool(getattr(detector, "is_ready", False))
            except Exception:
                continue
        return False

    # ------------------------------------------------------------------
    # Detection history helpers (AI API exposure)
    # ------------------------------------------------------------------

    def record_detection_event(
        self,
        *,
        source: str,
        validator_id: Optional[str],
        threat_type: str,
        severity: int,
        confidence: float,
        final_score: float,
        should_publish: bool,
        metadata: Optional[Dict[str, Any]] = None,
    ) -> None:
        """Persist sanitized detection event for API queries."""

        if validator_id is not None and isinstance(validator_id, bytes):
            validator_id = validator_id.hex()

        now = time.time()
        event_timestamp = self._resolve_event_timestamp(metadata)

        event = {
            "timestamp": event_timestamp if event_timestamp is not None else now,
            "source": source,
            "validator_id": validator_id,
            "threat_type": threat_type,
            "severity": int(max(0, severity)),
            "confidence": float(max(0.0, min(confidence, 1.0))),
            "final_score": float(max(0.0, min(final_score, 1.0))),
            "should_publish": bool(should_publish),
            "metadata": self._sanitize_event_metadata(metadata or {}),
        }

        with self._history_lock:
            self._detection_history.append(event)

    def get_detection_history(
        self,
        limit: int = 50,
        since: Optional[float] = None,
        validator_id: Optional[str] = None,
    ) -> list:
        """Return recent detection events (newest first)."""

        limit = max(1, min(limit, 200))
        with self._history_lock:
            events = list(self._detection_history)

        if since is not None:
            events = [evt for evt in events if evt["timestamp"] >= since]

        if validator_id:
            validator_id = validator_id.lower()
            events = [evt for evt in events if (evt.get("validator_id") or "").lower() == validator_id]

        events.sort(key=lambda evt: evt["timestamp"], reverse=True)

        return [self._format_detection_entry(evt) for evt in events[:limit]]

    def _resolve_event_timestamp(self, metadata: Optional[Dict[str, Any]]) -> Optional[float]:
        """Normalize event timestamps to guard against clock skew."""

        if not metadata:
            return None

        candidate = metadata.get("timestamp") or metadata.get("detection_timestamp") or metadata.get("commit_timestamp")
        if candidate is None and isinstance(metadata.get("latency_ms"), (int, float)):
            try:
                base = metadata.get("commit_height_timestamp")
                if isinstance(base, (int, float)):
                    candidate = float(base)
            except Exception:
                candidate = None

        ts: Optional[float]
        if isinstance(candidate, (int, float)):
            ts = float(candidate)
        elif isinstance(candidate, str):
            try:
                ts = float(candidate)
            except ValueError:
                ts = None
        else:
            ts = None

        if ts is None:
            return None

        now = time.time()
        # Clamp timestamps that are wildly out of range (beyond +-10 minutes)
        future_tolerance = float(getattr(self, "_timestamp_skew_tolerance", 600))
        past_tolerance = float(getattr(self, "_timestamp_backlog_tolerance", 3600))
        if ts > now + future_tolerance or ts < now - past_tolerance:
            return now
        return ts

    def get_ai_suspicious_nodes(self, limit: int = 10) -> list:
        """Compute AI-driven suspicious validator scores."""

        limit = max(1, min(limit, 50))
        now = time.time()

        with self._history_lock:
            events = list(self._detection_history)

        aggregates: Dict[str, Dict[str, Any]] = {}
        for event in events:
            if not event.get("should_publish"):
                continue

            validator_id = event.get("validator_id")
            if not validator_id:
                continue

            age_seconds = now - event["timestamp"]
            decay = math.exp(-age_seconds / 900.0) if age_seconds > 0 else 1.0
            decay = max(decay, 0.05)

            severity_component = float(event["severity"]) * 8.0
            confidence_component = float(event["confidence"]) * 20.0
            score = (severity_component + confidence_component) * decay

            entry = aggregates.setdefault(
                validator_id,
                {
                    "score": 0.0,
                    "event_count": 0,
                    "last_seen": event["timestamp"],
                    "max_severity": 0,
                    "max_confidence": 0.0,
                    "threat_types": set(),
                },
            )

            entry["score"] += score
            entry["event_count"] += 1
            entry["last_seen"] = max(entry["last_seen"], event["timestamp"])
            entry["max_severity"] = max(entry["max_severity"], event["severity"])
            entry["max_confidence"] = max(entry["max_confidence"], event["confidence"])
            entry["threat_types"].add(event.get("threat_type", "unknown"))

        results = []
        for validator_id, data in aggregates.items():
            suspicion = min(100.0, round(data["score"], 2))

            if suspicion < 20.0:
                # Ignore noise-level scores
                continue

            if suspicion >= 75.0:
                status = "critical"
            elif suspicion >= 40.0:
                status = "warning"
            else:
                status = "healthy"

            last_seen_iso = datetime.utcfromtimestamp(data["last_seen"]).isoformat() + "Z"
            reason = (
                f"events={data['event_count']};"
                f"max_severity={data['max_severity']};"
                f"last_seen={last_seen_iso}"
            )

            results.append(
                {
                    "id": validator_id,
                    "status": status,
                    "uptime": max(0.0, 100.0 - suspicion),
                    "suspicion_score": suspicion,
                    "reason": reason,
                    "event_count": data["event_count"],
                    "last_seen": last_seen_iso,
                    "threat_types": sorted(data["threat_types"]),
                }
            )

        results.sort(key=lambda item: item["suspicion_score"], reverse=True)
        return results[:limit]

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _sanitize_event_metadata(self, metadata: Dict[str, Any]) -> Dict[str, Any]:
        """Keep only primitive metadata fields (no PII, no nested objects)."""

        sanitized = {}
        for key, value in metadata.items():
            if len(sanitized) >= 8:
                break
            if isinstance(value, (str, int, float, bool)) and len(str(value)) <= 256:
                sanitized[str(key)[:64]] = value
        return sanitized

    def _format_detection_entry(self, event: Dict[str, Any]) -> Dict[str, Any]:
        formatted = {
            "timestamp": datetime.utcfromtimestamp(event["timestamp"]).isoformat() + "Z",
            "source": event["source"],
            "validator_id": event.get("validator_id"),
            "threat_type": event.get("threat_type"),
            "severity": event.get("severity"),
            "confidence": round(float(event.get("confidence", 0.0)), 4),
            "final_score": round(float(event.get("final_score", 0.0)), 4),
            "should_publish": event.get("should_publish", False),
        }

        metadata = event.get("metadata")
        if metadata:
            formatted["metadata"] = metadata

        return formatted

