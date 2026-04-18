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
import sys
import threading
import time
from collections import deque
from datetime import datetime
from enum import Enum
from pathlib import Path
from typing import Optional, Dict, Any, Callable, List, Tuple
import ipaddress

from ..config import Settings
from ..config.settings import FeedbackConfig
from ..logging import get_logger
from ..utils import Signer, NonceManager, CircuitBreaker
from ..utils.errors import ServiceError
from ..kafka import AIProducer, AIConsumer
from ..contracts import AnomalyMessage, EvidenceMessage, PolicyMessage
from ..utils.id_gen import generate_uuid7
from .crypto_setup import (
    initialize_signer,
    initialize_nonce_manager,
    shutdown_nonce_manager,
)
from .handlers import MessageHandlers
from .publisher import MessagePublisher
from .policy_aggregation import PolicyAggregationManager
from ..utils.validators import validate_uuid


def _load_pcap_request_pb2():
    proto_path = os.getenv("TELEMETRY_PROTO_PATH", "").strip()
    candidates = []
    if proto_path:
        candidates.append(Path(proto_path))
    candidates.append(Path(__file__).resolve().parents[3] / "telemetry-layer" / "proto" / "gen" / "python")
    for path in candidates:
        if path.exists():
            path_str = str(path)
            if path_str not in sys.path:
                sys.path.insert(0, path_str)
            break
    from telemetry_pcap_request_v1_pb2 import PcapRequestV1  # type: ignore
    return PcapRequestV1


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
        self.sentinel_adapter = None
        self.policy_aggregator = None
        
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
        self._activity_stale_seconds = int(os.getenv("DETECTION_ACTIVITY_STALE_SECONDS", "180"))
        self._history_lock = threading.Lock()
        self._detection_history = deque(maxlen=500)
        self._detection_source_metrics_lock = threading.Lock()
        self._detection_source_metrics = {}

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

                # Initialize Sentinel adapter (Kafka integration path)
                self.logger.info("Initializing Sentinel adapter")
                self._initialize_sentinel_adapter(settings)
                self.logger.info("Sentinel adapter initialized")
                
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

                telemetry_source = getattr(self.detection_pipeline, "telemetry_source", None)
                if telemetry_source is None:
                    telemetry_source = getattr(self.detection_pipeline, "telemetry", None)
                if telemetry_source is not None and hasattr(telemetry_source, "prime_assignment"):
                    self.logger.info("Priming telemetry consumer assignment before detection loop start")
                    try:
                        assignment_status = telemetry_source.prime_assignment()
                        self.logger.info("Telemetry consumer prime complete", extra=assignment_status)
                    except Exception as exc:
                        self.logger.warning(
                            "Telemetry consumer prime failed",
                            extra={"error": str(exc)},
                            exc_info=True,
                        )
                
                # Start detection loop (Phase 8)
                if self.detection_loop:
                    self.logger.info("Starting detection loop")
                    self.detection_loop.start()

                # Start Sentinel adapter loop
                if self.sentinel_adapter:
                    self.logger.info("Starting Sentinel adapter")
                    self.sentinel_adapter.start()
                
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

                # Stop Sentinel adapter before producer shutdown
                if self.sentinel_adapter:
                    self.logger.info("Stopping Sentinel adapter")
                    try:
                        self.sentinel_adapter.stop(timeout=10)
                    except Exception as e:
                        self.logger.error(
                            "Error stopping Sentinel adapter",
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

    def request_policy_revoke(
        self,
        policy_id: str,
        *,
        reason_code: Optional[str] = None,
        reason_text: Optional[str] = None,
        requested_by: Optional[str] = None,
        requires_ack: bool = True,
        rollback_policy_id: Optional[str] = None,
    ) -> Dict[str, Any]:
        """Publish a revoke/remove request for an existing enforcement policy."""
        if not self._running:
            raise ServiceError("Cannot revoke policy: service not running")
        if self.publisher is None:
            raise ServiceError("Cannot revoke policy: publisher not initialized")

        validate_uuid(policy_id, "policy_id")
        if rollback_policy_id:
            validate_uuid(rollback_policy_id, "rollback_policy_id")

        payload: Dict[str, Any] = {
            "schema_version": 1,
            "policy_id": policy_id,
            "rule_type": "block",
            "action": "remove",
            "guardrails": {
                "requires_ack": bool(requires_ack),
            },
            "metadata": {
                "source_service": "ai-service",
                "requested_at_ms": int(time.time() * 1000),
            },
        }

        audit: Dict[str, Any] = {}
        if reason_code:
            audit["reason_code"] = str(reason_code).strip()
        if reason_text:
            audit["reason_text"] = str(reason_text).strip()
        if requested_by:
            requested_by = str(requested_by).strip()
            if requested_by:
                audit["requested_by"] = requested_by
                payload["metadata"]["requested_by"] = requested_by
        if audit:
            payload["audit"] = audit
        if rollback_policy_id:
            payload["rollback_policy_id"] = rollback_policy_id

        self.publisher.publish_policy_violation(
            policy_id=policy_id,
            rule_type="block",
            enforcement_action="remove",
            payload=payload,
        )

        return {
            "accepted": True,
            "policy_id": policy_id,
            "action": "remove",
            "requires_ack": bool(requires_ack),
        }

    def request_packet_capture(
        self,
        *,
        tenant_id: str,
        sensor_id: str,
        requester: Optional[str] = None,
        reason: Optional[str] = None,
        src_ip: Optional[str] = None,
        dst_ip: Optional[str] = None,
        src_port: Optional[int] = None,
        dst_port: Optional[int] = None,
        proto: Optional[int] = None,
        duration_ms: Optional[int] = None,
        max_bytes: Optional[int] = None,
        request_id: Optional[str] = None,
    ) -> Dict[str, Any]:
        """Publish a typed PCAP request to telemetry."""
        if not self._running:
            raise ServiceError("Cannot request pcap: service not running")
        if self.producer is None:
            raise ServiceError("Cannot request pcap: producer not initialized")

        tenant_id = str(tenant_id or "").strip()
        sensor_id = str(sensor_id or "").strip()
        requester = str(requester or "ai-service-ops").strip()
        reason = str(reason or "manual_capture").strip()
        if not tenant_id:
            raise ValueError("tenant_id required")
        if not sensor_id:
            raise ValueError("sensor_id required")
        if not requester:
            raise ValueError("requester required")

        req_id = str(request_id or generate_uuid7()).strip()
        validate_uuid(req_id, "request_id")

        duration_val = int(duration_ms if duration_ms is not None else os.getenv("AI_PCAP_DEFAULT_DURATION_MS", "5000"))
        max_duration = int(os.getenv("AI_PCAP_MAX_DURATION_MS", "30000"))
        if duration_val <= 0:
            raise ValueError("duration_ms must be positive")
        if max_duration > 0 and duration_val > max_duration:
            raise ValueError("duration_ms exceeds max")

        max_bytes_val = int(max_bytes if max_bytes is not None else os.getenv("AI_PCAP_DEFAULT_MAX_BYTES", "1048576"))
        cap_bytes = int(os.getenv("AI_PCAP_MAX_BYTES", "10485760"))
        if max_bytes_val < 0:
            raise ValueError("max_bytes must be non-negative")
        if cap_bytes > 0 and max_bytes_val > cap_bytes:
            raise ValueError("max_bytes exceeds max")

        src_ip = str(src_ip or "").strip()
        dst_ip = str(dst_ip or "").strip()
        if src_ip:
            ipaddress.ip_address(src_ip)
        if dst_ip:
            ipaddress.ip_address(dst_ip)

        src_port_val = int(src_port or 0)
        dst_port_val = int(dst_port or 0)
        for name, value in (("src_port", src_port_val), ("dst_port", dst_port_val)):
            if value < 0 or value > 65535:
                raise ValueError(f"{name} must be between 0 and 65535")

        proto_val = int(proto or 0)
        if proto_val not in (0, 6, 17):
            raise ValueError("proto must be one of 0, 6, 17")

        if not any((src_ip, dst_ip, src_port_val, dst_port_val, proto_val)):
            raise ValueError("at least one packet selector is required")

        PcapRequestV1 = _load_pcap_request_pb2()
        msg = PcapRequestV1(
            schema="pcap.request.v1",
            ts=int(time.time()),
            tenant_id=tenant_id,
            request_id=req_id,
            requester=requester,
            reason=reason,
            src_ip=src_ip,
            dst_ip=dst_ip,
            src_port=src_port_val,
            dst_port=dst_port_val,
            proto=proto_val,
            duration_ms=duration_val,
            max_bytes=max_bytes_val,
            sensor_id=sensor_id,
        )

        self.producer.send_pcap_request(msg.SerializeToString(), key=req_id)
        return {
            "accepted": True,
            "request_id": req_id,
            "topic": self.settings.kafka_topics.pcap_request if self.settings else "pcap.request.v1",
            "tenant_id": tenant_id,
            "sensor_id": sensor_id,
            "duration_ms": duration_val,
            "max_bytes": max_bytes_val,
        }
    
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
                aggregate_metrics = self._build_aggregate_detection_metrics(loop_metrics=metrics_copy)

                cache_entry = {
                    "running": snapshot.get("running", False),
                    "metrics": aggregate_metrics,
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
                    "metrics": aggregate_metrics,
                    "last_updated": snapshot.get("last_updated"),
                    "history_staleness_seconds": None,
                    "seconds_since_last_iteration": snapshot.get("seconds_since_last_iteration"),
                    "seconds_since_last_detection": self._seconds_since_detection(aggregate_metrics) or snapshot.get("seconds_since_last_detection"),
                    "sources": self._get_detection_source_metrics(),
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

                self._reconcile_recent_detection_activity(snapshot_copy)

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
                    "sources": self._get_detection_source_metrics(),
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
            aggregate_metrics = self._build_aggregate_detection_metrics()
            return {
                "running": self._cached_detection_snapshot.get("running", False),
                "status": self._cached_detection_snapshot.get("status", "unknown"),
                "healthy": self._cached_detection_snapshot.get("healthy", False),
                "blocking": self._cached_detection_snapshot.get("blocking", False),
                "issues": list(self._cached_detection_snapshot.get("issues", [])),
                "message": self._cached_detection_snapshot.get("message", ""),
                "metrics": aggregate_metrics,
                "last_updated": self._cached_detection_snapshot.get("last_updated"),
                "history_staleness_seconds": self._cached_detection_snapshot.get("history_staleness_seconds"),
                "seconds_since_last_iteration": self._cached_detection_snapshot.get("seconds_since_last_iteration"),
                "seconds_since_last_detection": self._cached_detection_snapshot.get("seconds_since_last_detection"),
                "sources": self._get_detection_source_metrics(),
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

    def _seconds_since_detection(self, metrics: Dict[str, Any]) -> Optional[float]:
        candidate = metrics.get("last_detection_time")
        try:
            if candidate is None:
                return None
            return max(0.0, time.time() - float(candidate))
        except (TypeError, ValueError):
            return None

    def _reconcile_recent_detection_activity(self, snapshot: Dict[str, Any]) -> None:
        if not snapshot.get("running", False):
            return

        activity_age = snapshot.get("seconds_since_last_detection")
        if not isinstance(activity_age, (int, float)):
            return
        if activity_age > max(1, self._activity_stale_seconds):
            return

        stale_prefixes = (
            "no detections recorded yet",
            "detection history empty",
        )
        stale_substrings = (
            "since last iteration",
            "since last detection",
        )

        original_issues = list(snapshot.get("issues", []))
        filtered_issues = []
        removed_issue = False
        for issue in original_issues:
            issue_text = str(issue)
            if issue_text.startswith(stale_prefixes) or any(token in issue_text for token in stale_substrings):
                removed_issue = True
                continue
            filtered_issues.append(issue_text)

        if not removed_issue:
            return

        snapshot["issues"] = filtered_issues
        snapshot["message"] = "; ".join(filtered_issues)
        if not filtered_issues:
            snapshot["blocking"] = False
            snapshot["status"] = "ok"
            snapshot["healthy"] = True
            return

        snapshot["blocking"] = False
        snapshot["status"] = "degraded"
        snapshot["healthy"] = False

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

    def get_kafka_consumer_status(self) -> Dict[str, Any]:
        """Return Kafka consumer runtime status for ops endpoints."""
        if not self.consumer:
            return {"available": False, "reason": "consumer_not_initialized"}
        status = self.consumer.get_runtime_status()
        status["available"] = True
        return status

    def get_kafka_consumer_offsets(self, topic: Optional[str] = None) -> Dict[str, Any]:
        """Return assigned partition offsets/lag for ops endpoints."""
        if not self.consumer:
            return {"available": False, "reason": "consumer_not_initialized", "offsets": [], "errors": []}
        data = self.consumer.get_offsets(topic=topic)
        data["available"] = True
        if topic:
            data["topic_filter"] = topic
        return data

    def get_recent_poison_messages(self, limit: int = 50) -> Dict[str, Any]:
        """Return recent poison/DLQ records captured by the consumer."""
        if not self.consumer:
            return {"available": False, "reason": "consumer_not_initialized", "messages": []}
        messages = self.consumer.get_recent_poison_messages(limit=limit)
        return {
            "available": True,
            "count": len(messages),
            "messages": messages,
        }

    def get_policy_emitter_status(self) -> Dict[str, Any]:
        """Return policy emitter runtime health/metrics."""
        status: Dict[str, Any] = {
            "available": False,
            "state": self.state.value,
        }
        if self.producer is None:
            status["reason"] = "producer_not_initialized"
            return status

        status["available"] = True
        status["producer_metrics"] = self.producer.get_metrics()
        status["pending_deliveries"] = self.publisher.get_pending_count() if self.publisher else None
        status["publisher_metrics"] = self.publisher.get_metrics() if self.publisher else {}
        status["circuit_breaker"] = None
        if self.circuit_breaker:
            cb_state = self.circuit_breaker.state
            status["circuit_breaker"] = cb_state.value if hasattr(cb_state, "value") else str(cb_state)
        if self.sentinel_adapter is not None:
            try:
                status["sentinel_adapter"] = self.sentinel_adapter.get_metrics()
            except Exception:
                status["sentinel_adapter"] = {"available": False, "reason": "metrics_unavailable"}
        return status
    
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
        from ..ml.telemetry_live import LiveTelemetrySource
        from ..ml.telemetry_kafka import KafkaTelemetrySource
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
        
        # 1. Telemetry source - use PostgreSQL or Kafka if configured
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
            telemetry_mode = get_config('TELEMETRY_MODE', 'train')
            feature_mask_enabled = str(get_config('FEATURE_MASK_ENABLED', 'true')).lower() in ('1', 'true', 'yes', 'on')
            min_feature_coverage = float(get_config('FEATURE_MIN_COVERAGE', getattr(settings, 'min_feature_coverage', 0.45)))
            if telemetry_mode == 'live':
                telemetry_source = LiveTelemetrySource(
                    db_config=db_config,
                    sample_size=100,
                    table_name=table_name,
                    schema=schema,
                    pool_min=pool_min,
                    pool_max=pool_max,
                    prefetch_enabled=prefetch_enabled,
                    feature_mask_enabled=feature_mask_enabled,
                    min_feature_coverage=min_feature_coverage,
                )
                self.logger.info(
                    'Using LiveTelemetrySource',
                    extra={
                        'table': f"{schema}.{table_name}",
                        'pool_min': pool_min,
                        'pool_max': pool_max,
                        'prefetch_enabled': prefetch_enabled,
                        'feature_mask_enabled': feature_mask_enabled,
                        'min_feature_coverage': min_feature_coverage,
                    }
                )
            else:
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
        elif telemetry_source_type == 'kafka':
            telemetry_topic = get_config('TELEMETRY_FEATURES_TOPIC', 'telemetry.features.v1')
            telemetry_dlq = get_config('TELEMETRY_FEATURES_DLQ_TOPIC', 'telemetry.features.v1.dlq')
            consumer_group = get_config('TELEMETRY_CONSUMER_GROUP', f"{settings.node_id}-telemetry")
            poll_timeout = float(get_config('TELEMETRY_POLL_TIMEOUT_SEC', 1.0))
            feature_mask_enabled = str(get_config('FEATURE_MASK_ENABLED', 'true')).lower() in ('1', 'true', 'yes', 'on')
            telemetry_source = KafkaTelemetrySource(
                settings=settings,
                topic=telemetry_topic,
                dlq_topic=telemetry_dlq,
                consumer_group=consumer_group,
                poll_timeout=poll_timeout,
                feature_mask_enabled=feature_mask_enabled,
            )
            self.logger.info(
                'Using Kafka telemetry source',
                extra={
                    'topic': telemetry_topic,
                    'dlq_topic': telemetry_dlq,
                    'consumer_group': consumer_group,
                    'poll_timeout': poll_timeout,
                    'feature_mask_enabled': feature_mask_enabled,
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
            # Diagnostics-only mode (explicit opt-in): when enabled the loop may publish a minimal
            # anomaly even if the pipeline abstains, so we can validate Kafka wiring end-to-end.
            'DIAGNOSTICS_FORCE_PUBLISH': str(os.getenv('DIAGNOSTICS_FORCE_PUBLISH', 'false')).lower() in ('1', 'true', 'yes', 'on'),
        }

        policy_cfg = getattr(settings, "policy_publishing", None)
        if policy_cfg is not None:
            detection_config['POLICY_PUBLISHING'] = policy_cfg
            self.policy_aggregator = PolicyAggregationManager(policy_cfg)
        else:
            self.policy_aggregator = None

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
            policy_aggregator=self.policy_aggregator,
        )
        
        self.logger.info(
            f"Detection loop initialized: interval={detection_config['DETECTION_INTERVAL']}s, "
            f"rate_limit={max_detections_per_second}/s"
        )

    def _initialize_sentinel_adapter(self, settings: Settings) -> None:
        """
        Initialize Sentinel Kafka adapter for integrated mode.

        This path is transport-based (Kafka), not in-process wrapper mode.
        """
        if os.getenv("SENTINEL_ADAPTER_ENABLED", "false").lower() not in ("1", "true", "yes", "on"):
            self.sentinel_adapter = None
            self.logger.info("Sentinel adapter disabled")
            return

        from .sentinel_adapter import SentinelKafkaAdapter

        tracker = None
        if self.feedback_service:
            tracker = getattr(self.feedback_service, "tracker", None)

        self.sentinel_adapter = SentinelKafkaAdapter(
            settings=settings,
            publisher=self.publisher,
            logger=self.logger,
            tracker=tracker,
            event_recorder=self.record_detection_event,
            policy_aggregator=self.policy_aggregator,
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

    def get_sentinel_metrics_summary(self) -> Dict[str, Any]:
        """Return a client-friendly Sentinel pipeline summary."""

        source_metrics = self._get_detection_source_metrics().get("sentinel", self._new_detection_source_metrics())
        events_total = int(source_metrics.get("events_total", 0) or 0)
        detections_total = int(source_metrics.get("detections_total", 0) or 0)
        publish_ratio = float(detections_total) / events_total if events_total > 0 else 0.0
        last_detection_time = source_metrics.get("last_detection_time")
        last_entity_id = source_metrics.get("last_entity_id")
        last_entity_type = source_metrics.get("last_entity_type")

        threat_counts: Dict[str, int] = {}
        latest_history_ts = None
        with self._history_lock:
            events = list(self._detection_history)

        for event in events:
            if event.get("source") != "sentinel":
                continue
            threat_type = str(event.get("threat_type") or "unknown")
            threat_counts[threat_type] = threat_counts.get(threat_type, 0) + 1
            timestamp = event.get("timestamp")
            try:
                ts_value = float(timestamp)
            except (TypeError, ValueError):
                continue
            if latest_history_ts is None or ts_value > latest_history_ts:
                latest_history_ts = ts_value

        top_threat_type = None
        if threat_counts:
            top_threat_type = sorted(
                threat_counts.items(),
                key=lambda item: (-item[1], item[0]),
            )[0][0]

        if events_total <= 0 and detections_total <= 0:
            status = "idle"
        elif detections_total > 0:
            status = "active"
        else:
            status = "observing"

        return {
            "status": status,
            "events_total": events_total,
            "detections_total": detections_total,
            "publish_ratio": publish_ratio,
            "last_detection_time": last_detection_time,
            "last_history_event_time": latest_history_ts,
            "top_threat_type": top_threat_type,
            "last_entity_id": last_entity_id,
            "last_entity_type": last_entity_type,
        }

    def get_detection_interval_seconds(self) -> Optional[float]:
        """Return configured detection interval for UI display."""

        if self.detection_loop is not None:
            try:
                return float(getattr(self.detection_loop, "_interval"))
            except Exception:
                pass

        if self.settings is not None:
            try:
                return float(getattr(self.settings, "detection_interval"))
            except Exception:
                return None
        return None

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
        self._record_detection_source_event(
            source=source,
            timestamp=event["timestamp"],
            should_publish=bool(should_publish),
            validator_id=validator_id,
            metadata=event["metadata"],
        )

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

    def get_ai_suspicious_nodes(self, limit: int = 10, entity_type: Optional[str] = None) -> list:
        """Compute AI-driven suspicious entities from recent detection history."""

        limit = max(1, min(limit, 50))
        requested_entity_type = None
        if entity_type:
            requested_entity_type = str(entity_type).strip().lower()
            if requested_entity_type in ("all", "*"):
                requested_entity_type = None
        now = time.time()

        with self._history_lock:
            events = list(self._detection_history)

        aggregates: Dict[str, Dict[str, Any]] = {}
        for event in events:
            if not event.get("should_publish"):
                continue

            resolved_entity_id, resolved_entity_type = self._resolve_suspicious_entity(event)
            if not resolved_entity_id:
                continue

            age_seconds = now - event["timestamp"]
            decay = math.exp(-age_seconds / 900.0) if age_seconds > 0 else 1.0
            decay = max(decay, 0.05)

            severity_component = float(event["severity"]) * 8.0
            confidence_component = float(event["confidence"]) * 20.0
            score = (severity_component + confidence_component) * decay

            entry = aggregates.setdefault(
                resolved_entity_id,
                {
                    "entity_type": resolved_entity_type,
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
        for entity_id, data in aggregates.items():
            if requested_entity_type and str(data.get("entity_type", "")).lower() != requested_entity_type:
                continue
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
                    "id": entity_id,
                    "entity_type": data.get("entity_type", "validator"),
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

    def _new_detection_source_metrics(self) -> Dict[str, Any]:
        return {
            "events_total": 0,
            "detections_total": 0,
            "last_detection_time": None,
            "last_validator_id": None,
            "last_entity_id": None,
            "last_entity_type": None,
        }

    def _record_detection_source_event(
        self,
        *,
        source: str,
        timestamp: float,
        should_publish: bool,
        validator_id: Optional[str],
        metadata: Optional[Dict[str, Any]],
    ) -> None:
        source_key = str(source or "unknown")[:64]
        entity_id, entity_type = self._resolve_suspicious_entity({
            "validator_id": validator_id,
            "metadata": metadata or {},
        })
        with self._detection_source_metrics_lock:
            entry = self._detection_source_metrics.setdefault(source_key, self._new_detection_source_metrics())
            entry["events_total"] += 1
            if should_publish:
                entry["detections_total"] += 1
                entry["last_detection_time"] = timestamp
            if validator_id:
                entry["last_validator_id"] = validator_id
            if entity_id:
                entry["last_entity_id"] = entity_id
                entry["last_entity_type"] = entity_type

    def _get_detection_source_metrics(self) -> Dict[str, Dict[str, Any]]:
        with self._detection_source_metrics_lock:
            return {
                source: dict(metrics)
                for source, metrics in self._detection_source_metrics.items()
            }

    def _build_aggregate_detection_metrics(self, loop_metrics: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
        loop_metrics = dict(loop_metrics if loop_metrics is not None else self._cached_detection_metrics.get("metrics", {}))
        source_metrics = self._get_detection_source_metrics()
        publisher_metrics = self.publisher.get_metrics() if self.publisher else {}

        aggregate = dict(loop_metrics)
        aggregate["detections_total"] = int(
            sum(int(metrics.get("detections_total", 0) or 0) for metrics in source_metrics.values())
        )
        aggregate["detections_published"] = int(
            publisher_metrics.get("anomalies_sent", loop_metrics.get("detections_published", 0) or 0) or 0
        )
        aggregate["events_total"] = int(
            sum(int(metrics.get("events_total", 0) or 0) for metrics in source_metrics.values())
        )
        aggregate["sources"] = source_metrics

        latest_detection = None
        for metrics in source_metrics.values():
            ts = metrics.get("last_detection_time")
            if ts is None:
                continue
            try:
                ts_val = float(ts)
            except (TypeError, ValueError):
                continue
            if latest_detection is None or ts_val > latest_detection:
                latest_detection = ts_val
        if latest_detection is not None:
            aggregate["last_detection_time"] = latest_detection

        return aggregate

    def _resolve_suspicious_entity(self, event: Dict[str, Any]) -> Tuple[Optional[str], str]:
        validator_id = event.get("validator_id")
        if isinstance(validator_id, bytes):
            validator_id = validator_id.hex()
        if validator_id:
            return (str(validator_id), "validator")

        metadata = event.get("metadata") or {}
        if not isinstance(metadata, dict):
            return (None, "unknown")

        for key in ("src_ip", "source_ip"):
            candidate = metadata.get(key)
            if isinstance(candidate, str) and candidate.strip():
                return (f"network:{candidate.strip()}", "network_source")

        for key in ("dst_ip", "destination_ip"):
            candidate = metadata.get(key)
            if isinstance(candidate, str) and candidate.strip():
                return (f"target:{candidate.strip()}", "network_target")

        return (None, "unknown")

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

