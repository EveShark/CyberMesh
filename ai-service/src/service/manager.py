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
import threading
import time
from enum import Enum
from typing import Optional, Dict, Any, Callable
from datetime import datetime

from ..config import Settings
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
                self.consumer = AIConsumer(
                    config=settings,
                    logger=self.logger
                )
                
                # Initialize message handlers (will set service_manager after ML pipeline init)
                self.logger.info("Initializing message handlers")
                self.handlers = MessageHandlers(logger=self.logger, service_manager=None)
                
                # Register handlers with consumer
                self.consumer.register_handler("commit", self.handlers.handle_commit_event)
                self.consumer.register_handler("reputation", self.handlers.handle_reputation_event)
                self.consumer.register_handler("policy_update", self.handlers.handle_policy_update_event)
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
                health["detection_loop"] = {
                    "running": self.detection_loop.is_healthy(),
                    "metrics": self.detection_loop.get_metrics()
                }
            
            # Rate limiter stats (Phase 8)
            if self.rate_limiter:
                health["rate_limiter"] = self.rate_limiter.get_stats()
            
            return health
    
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
            telemetry_source = PostgresTelemetrySource(db_config=db_config, sample_size=100)
            self.logger.info('Using PostgreSQL telemetry source')
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
        
        # 7. Prometheus metrics
        self.ml_metrics = PrometheusMetrics()
        
        # 8. Detection pipeline
        self.detection_pipeline = DetectionPipeline(
            telemetry_source=telemetry_source,
            feature_extractor=feature_extractor,
            engines=engines,
            ensemble=ensemble,
            evidence_generator=evidence_generator,
            circuit_breaker=self.circuit_breaker,
            config=ml_config
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
        
        self.feedback_service = FeedbackService(settings, self.logger)
        
        # Wire AdaptiveDetection into existing ensemble
        if self.detection_pipeline and self.detection_pipeline.ensemble:
            adaptive = AdaptiveDetection(self.feedback_service)
            self.detection_pipeline.ensemble.adaptive = adaptive
            self.logger.info("Adaptive detection wired into ensemble")
        
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
            'TELEMETRY_BATCH_SIZE': getattr(settings, 'telemetry_batch_size', 1000)
        }
        
        self.detection_loop = DetectionLoop(
            pipeline=self.detection_pipeline,
            publisher=self.publisher,
            rate_limiter=self.rate_limiter,
            config=detection_config,
            logger=self.logger
        )
        
        self.logger.info(
            f"Detection loop initialized: interval={detection_config['DETECTION_INTERVAL']}s, "
            f"rate_limit={max_detections_per_second}/s"
        )

