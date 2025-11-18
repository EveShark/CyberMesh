import os
from dataclasses import dataclass, field
from .security import SecurityConfig, load_security_config
from .kafka import (
    KafkaTopicsConfig,
    KafkaProducerConfig,
    KafkaConsumerConfig,
    load_kafka_config
)


@dataclass(frozen=True)
class APIConfig:
    """Admin API configuration."""
    listen_addr: str = ":8080"
    health_path: str = "/health"
    metrics_path: str = "/metrics"
    admin_path_prefix: str = "/admin"


@dataclass(frozen=True)
class ModelConfig:
    """ML model configuration."""
    ddos_model_path: str
    malware_model_path: str
    hot_reload_enabled: bool = True
    reload_check_interval_seconds: int = 60
    
    def validate(self):
        """Validate model configuration."""
        if self.hot_reload_enabled and self.reload_check_interval_seconds < 10:
            raise ValueError("Hot reload interval must be at least 10 seconds")


@dataclass(frozen=True)
class DetectionConfig:
    """Detection pipeline configuration."""
    ddos_confidence_threshold: float = 0.85
    malware_confidence_threshold: float = 0.90
    enable_ddos_pipeline: bool = True
    enable_malware_pipeline: bool = True
    tracker_sample_window_size: int = 500
    tracker_sample_cap: int = 100
    tracker_batch_size: int = 50
    tracker_flush_interval_seconds: int = 5
    
    def validate(self):
        """Validate detection configuration."""
        if not (0.0 <= self.ddos_confidence_threshold <= 1.0):
            raise ValueError("DDoS confidence threshold must be in [0.0, 1.0]")
        if not (0.0 <= self.malware_confidence_threshold <= 1.0):
            raise ValueError("Malware confidence threshold must be in [0.0, 1.0]")


@dataclass(frozen=True)
class LimitsConfig:
    """Message size and time limits."""
    anomaly_max_size: int = 256 * 1024
    evidence_max_size: int = 2 * 1024 * 1024
    policy_max_size: int = 128 * 1024
    timestamp_skew_seconds: int = 60
    nonce_ttl_seconds: int = 900
    nonce_cleanup_interval_seconds: int = 60
    
    def validate(self):
        """Validate limits configuration."""
        if self.anomaly_max_size <= 0:
            raise ValueError("anomaly_max_size must be positive")
        if self.evidence_max_size <= 0:
            raise ValueError("evidence_max_size must be positive")
        if self.policy_max_size <= 0:
            raise ValueError("policy_max_size must be positive")
        if self.timestamp_skew_seconds <= 0:
            raise ValueError("timestamp_skew_seconds must be positive")
        if self.nonce_ttl_seconds <= 0:
            raise ValueError("nonce_ttl_seconds must be positive")
        if self.nonce_cleanup_interval_seconds <= 0:
            raise ValueError("nonce_cleanup_interval_seconds must be positive")


@dataclass(frozen=True)
class RateLimitConfig:
    """Rate limiting configuration."""
    global_capacity: int = 1000
    global_refill_rate: float = 100.0
    anomaly_capacity: int = 500
    anomaly_refill_rate: float = 50.0
    evidence_capacity: int = 200
    evidence_refill_rate: float = 20.0
    policy_capacity: int = 100
    policy_refill_rate: float = 10.0
    
    def validate(self):
        """Validate rate limit configuration."""
        if self.global_capacity <= 0:
            raise ValueError("global_capacity must be positive")
        if self.global_refill_rate <= 0:
            raise ValueError("global_refill_rate must be positive")
        if self.anomaly_capacity <= 0:
            raise ValueError("anomaly_capacity must be positive")
        if self.anomaly_refill_rate <= 0:
            raise ValueError("anomaly_refill_rate must be positive")
        if self.evidence_capacity <= 0:
            raise ValueError("evidence_capacity must be positive")
        if self.evidence_refill_rate <= 0:
            raise ValueError("evidence_refill_rate must be positive")
        if self.policy_capacity <= 0:
            raise ValueError("policy_capacity must be positive")
        if self.policy_refill_rate <= 0:
            raise ValueError("policy_refill_rate must be positive")


@dataclass(frozen=True)
class PolicyPublishingConfig:
    """Configuration controlling anomaly-to-policy conversion."""

    enabled: bool = False
    severity_threshold: int = 8
    confidence_threshold: float = 0.9
    ttl_seconds: int = 900
    scope: str = "cluster"  # cluster|namespace|node
    direction: str = "ingress"  # ingress|egress
    requires_ack: bool = True
    dry_run: bool = False
    canary_scope: bool = False
    cidr_max_prefix_len: int = 24
    max_targets: int = 32

    def validate(self):
        if not (1 <= self.severity_threshold <= 10):
            raise ValueError("severity_threshold must be between 1 and 10")
        if not (0.0 <= self.confidence_threshold <= 1.0):
            raise ValueError("confidence_threshold must be within [0, 1]")
        if self.ttl_seconds <= 0:
            raise ValueError("ttl_seconds must be positive")

        valid_scopes = {"cluster", "namespace", "node"}
        if self.scope not in valid_scopes:
            raise ValueError(f"scope must be one of {sorted(valid_scopes)}")

        valid_directions = {"ingress", "egress"}
        if self.direction not in valid_directions:
            raise ValueError(f"direction must be one of {sorted(valid_directions)}")

        if self.cidr_max_prefix_len <= 0 or self.cidr_max_prefix_len > 128:
            raise ValueError("cidr_max_prefix_len must be between 1 and 128")

        if self.max_targets <= 0:
            raise ValueError("max_targets must be positive")

@dataclass(frozen=True)
class CircuitBreakerConfig:
    """Circuit breaker configuration."""
    failure_threshold: int = 5
    timeout_seconds: int = 30
    recovery_threshold: int = 2
    
    def validate(self):
        """Validate circuit breaker configuration."""
        if self.failure_threshold <= 0:
            raise ValueError("failure_threshold must be positive")
        if self.timeout_seconds <= 0:
            raise ValueError("timeout_seconds must be positive")
        if self.recovery_threshold <= 0:
            raise ValueError("recovery_threshold must be positive")


@dataclass(frozen=True)
class FeedbackConfig:
    """Feedback loop configuration for anomaly lifecycle tracking and calibration."""
    
    # Lifecycle TTL and timeouts
    lifecycle_ttl_seconds: int = 30 * 24 * 3600  # 30 days
    admit_timeout_seconds: int = 30              # PUBLISHED to TIMEOUT
    commit_timeout_seconds: int = 300            # ADMITTED to TIMEOUT (5 min consensus)
    lifecycle_batch_size: int = 1000
    
    # Metric time windows
    metric_window_realtime: int = 3600          # 1 hour
    metric_window_short: int = 86400            # 24 hours
    metric_window_medium: int = 604800          # 7 days
    metric_window_long: int = 2592000           # 30 days
    
    # Statistical minimums for confidence
    min_samples_realtime: int = 100
    min_samples_short: int = 1000
    min_samples_medium: int = 5000
    min_samples_long: int = 20000
    
    # Acceptance rate thresholds
    acceptance_rate_critical: float = 0.40      # Below this triggers alert
    acceptance_rate_low: float = 0.60           # Below this triggers calibration
    acceptance_rate_target_min: float = 0.70    # Minimum acceptable
    acceptance_rate_target_max: float = 0.85    # Maximum acceptable
    acceptance_rate_high: float = 0.90          # Above this may miss threats
    
    # Exponential decay for time-weighted metrics
    decay_half_life_seconds: int = 86400        # 24 hours
    
    # Rate limits
    rate_limit_per_anomaly: int = 10            # updates per second per anomaly
    rate_limit_per_node: int = 10000            # updates per second per node
    rate_limit_global: int = 100000             # updates per second globally
    
    # Security
    clock_skew_tolerance_seconds: int = 300     # ±5 minutes
    require_signature_for_backend: bool = True
    audit_all_state_changes: bool = True
    
    # Redis
    redis_pipeline_size: int = 1000
    redis_transaction_retry: int = 3
    
    # Calibration
    calibration_method: str = "isotonic"  # isotonic or platt
    calibration_min_samples: int = 1000   # Minimum samples before retraining
    calibration_retrain_interval: int = 3600  # Max seconds between retraining
    calibration_acceptance_threshold: float = 0.60  # Retrain if acceptance < 60%
    calibration_model_path: str = "data/models/calibration"
    calibration_save_to_redis: bool = True
    calibration_redis_key: str = "calibration:model:current"

    # Storage controls
    disable_persistence: bool = False  # If true, feedback loop keeps data in-memory only
    
    def validate(self):
        """Validate feedback configuration."""
        if self.lifecycle_ttl_seconds <= 0:
            raise ValueError("lifecycle_ttl_seconds must be positive")
        if self.admit_timeout_seconds <= 0:
            raise ValueError("admit_timeout_seconds must be positive")
        if self.commit_timeout_seconds <= 0:
            raise ValueError("commit_timeout_seconds must be positive")
        if self.lifecycle_batch_size <= 0:
            raise ValueError("lifecycle_batch_size must be positive")
        
        if not (0.0 < self.acceptance_rate_critical < 1.0):
            raise ValueError("acceptance_rate_critical must be in (0.0, 1.0)")
        if not (0.0 < self.acceptance_rate_low < 1.0):
            raise ValueError("acceptance_rate_low must be in (0.0, 1.0)")
        if not (0.0 < self.acceptance_rate_target_min < 1.0):
            raise ValueError("acceptance_rate_target_min must be in (0.0, 1.0)")
        if not (0.0 < self.acceptance_rate_target_max < 1.0):
            raise ValueError("acceptance_rate_target_max must be in (0.0, 1.0)")
        if not (0.0 < self.acceptance_rate_high < 1.0):
            raise ValueError("acceptance_rate_high must be in (0.0, 1.0)")
        
        if self.acceptance_rate_critical >= self.acceptance_rate_low:
            raise ValueError("acceptance_rate_critical must be < acceptance_rate_low")
        if self.acceptance_rate_low >= self.acceptance_rate_target_min:
            raise ValueError("acceptance_rate_low must be < acceptance_rate_target_min")
        if self.acceptance_rate_target_min >= self.acceptance_rate_target_max:
            raise ValueError("acceptance_rate_target_min must be < acceptance_rate_target_max")
        if self.acceptance_rate_target_max >= self.acceptance_rate_high:
            raise ValueError("acceptance_rate_target_max must be < acceptance_rate_high")
        
        if self.decay_half_life_seconds <= 0:
            raise ValueError("decay_half_life_seconds must be positive")
        if self.rate_limit_per_anomaly <= 0:
            raise ValueError("rate_limit_per_anomaly must be positive")
        if self.rate_limit_per_node <= 0:
            raise ValueError("rate_limit_per_node must be positive")
        if self.rate_limit_global <= 0:
            raise ValueError("rate_limit_global must be positive")
        if self.clock_skew_tolerance_seconds <= 0:
            raise ValueError("clock_skew_tolerance_seconds must be positive")
        
        if self.calibration_method not in ("isotonic", "platt"):
            raise ValueError("calibration_method must be 'isotonic' or 'platt'")
        if self.calibration_min_samples <= 0:
            raise ValueError("calibration_min_samples must be positive")
        if self.calibration_retrain_interval <= 0:
            raise ValueError("calibration_retrain_interval must be positive")
        if not (0.0 < self.calibration_acceptance_threshold < 1.0):
            raise ValueError("calibration_acceptance_threshold must be in (0.0, 1.0)")


@dataclass(frozen=True)
class Settings:
    """Complete service configuration.
    
    This is the single source of truth for all service settings.
    Loaded and validated by src/config/loader.py.
    """
    # Environment
    environment: str  # development, staging, production
    node_id: str
    
    # Kafka
    kafka_topics: KafkaTopicsConfig
    kafka_producer: KafkaProducerConfig
    kafka_consumer: KafkaConsumerConfig
    
    # Cryptographic
    signing_key_path: "Path"
    signing_key_id: str
    domain_separation: str
    nonce_state_path: "Path"
    
    # Security
    mtls_enabled: bool
    jwt_enabled: bool
    jwt_secret: str = None
    aes_key: str = None
    
    # Models (optional)
    model_ddos_path: "Path" = None
    model_malware_path: "Path" = None
    
    # Detection loop (Phase 8)
    detection_interval: int = 5                  # Seconds between detection runs
    detection_timeout: int = 30                  # Max seconds for single detection run
    telemetry_batch_size: int = 1000             # Flows per poll
    max_detections_per_second: int = 100         # Rate limit for publishing
    max_flows_per_iteration: int = 100           # Max flows per detection loop iteration
    detection_summary_interval: int = 10         # Iterations between summary logs
    max_iteration_lag_seconds: int = 20          # Acceptable delay beyond interval
    max_detection_gap_seconds: int = 600         # Warn if no detections within this window
    max_loop_latency_warning_ms: int = 3000      # Warn if loop latency exceeds this threshold
    detection_publish_flush: bool = False        # Force producer flush after publish
    timestamp_skew_tolerance_seconds: int = 600  # Future timestamp tolerance for detections
    tracker_sample_window_size: int = 500        # Detection events per sampling window
    tracker_sample_cap: int = 100                # Max events persisted per window
    tracker_batch_size: int = 50                 # Batch size for Redis writes
    tracker_flush_interval_seconds: int = 5      # Max seconds between batch flushes
    
    # ML ensemble
    min_confidence: float = 0.85                 # Ensemble minimum confidence threshold

    # Policy publishing
    policy_publishing: PolicyPublishingConfig = field(default_factory=PolicyPublishingConfig)
    
    
# Legacy Settings class (keep for backward compatibility during migration)
@dataclass(frozen=True)
class LegacySettings:
    """Complete application settings."""
    node_id: str
    environment: str
    security: SecurityConfig
    kafka_topics: KafkaTopicsConfig
    kafka_producer: KafkaProducerConfig
    kafka_consumer: KafkaConsumerConfig
    api: APIConfig
    models: ModelConfig
    detection: DetectionConfig
    limits: LimitsConfig
    rate_limits: RateLimitConfig
    circuit_breaker: CircuitBreakerConfig
    feedback: FeedbackConfig
    
    def create_signer(self):
        """Create a configured Signer instance."""
        from ..utils import Signer
        return Signer(
            private_key_path=self.security.ed25519.signing_key_path,
            key_id=self.security.ed25519.signing_key_id,
            domain_separation=self.security.ed25519.domain_separation
        )
    
    def create_nonce_manager(self):
        """Create a configured NonceManager instance."""
        from ..utils import NonceManager
        return NonceManager(
            instance_id=int(self.node_id),
            ttl_seconds=self.limits.nonce_ttl_seconds
        )
    
    def create_rate_limiter(self):
        """Create a configured MultiTierRateLimiter instance."""
        from ..utils import MultiTierRateLimiter
        return MultiTierRateLimiter(
            global_capacity=self.rate_limits.global_capacity,
            global_refill_rate=self.rate_limits.global_refill_rate,
            per_type_configs={
                "anomaly": {
                    "capacity": self.rate_limits.anomaly_capacity,
                    "refill_rate": self.rate_limits.anomaly_refill_rate
                },
                "evidence": {
                    "capacity": self.rate_limits.evidence_capacity,
                    "refill_rate": self.rate_limits.evidence_refill_rate
                },
                "policy": {
                    "capacity": self.rate_limits.policy_capacity,
                    "refill_rate": self.rate_limits.policy_refill_rate
                }
            }
        )
    
    def create_circuit_breaker(self, name: str = "kafka"):
        """Create a configured CircuitBreaker instance."""
        from ..utils import CircuitBreaker
        return CircuitBreaker(
            failure_threshold=self.circuit_breaker.failure_threshold,
            timeout_seconds=self.circuit_breaker.timeout_seconds,
            recovery_threshold=self.circuit_breaker.recovery_threshold
        )
    
    def create_logger(self, name: str):
        """Create a configured SecureLogger instance."""
        from ..utils import SecureLogger
        return SecureLogger(name)
    
    def get(self, key: str, default: str = "") -> str:
        """Get string value from environment (for Kafka layer compatibility)"""
        return os.getenv(key, default)
    
    def get_bool(self, key: str, default: bool = False) -> bool:
        """Get boolean value from environment"""
        value = os.getenv(key)
        if value is None:
            return default
        return value.lower() in ("true", "1", "yes", "on")
    
    def get_int(self, key: str, default: int = 0) -> int:
        """Get integer value from environment"""
        value = os.getenv(key)
        if value is None:
            return default
        try:
            return int(value)
        except ValueError:
            return default
    
    def get_float(self, key: str, default: float = 0.0) -> float:
        """Get float value from environment"""
        value = os.getenv(key)
        if value is None:
            return default
        try:
            return float(value)
        except ValueError:
            return default


class ConfigError(Exception):
    """Configuration loading or validation error."""
    pass


def load_settings() -> LegacySettings:
    """
    Load and validate complete application settings.
    
    Production gates:
    - TLS required for Kafka
    - Strong SASL mechanism (SCRAM, not PLAIN)
    - mTLS or strong JWT for admin API
    - All topics must be configured
    - Ed25519 key must exist with secure permissions
    - All secrets must meet minimum strength requirements
    
    Raises:
        ConfigError: If configuration is invalid or missing required values
    """
    # Load .env file from project root if it exists
    try:
        from pathlib import Path
        from dotenv import load_dotenv
        # Project root is two levels up from this file (ai-service/src/config/settings.py -> project root)
        project_root_env = Path(__file__).resolve().parent.parent.parent.parent / ".env"
        load_dotenv(project_root_env, override=True)
    except ImportError:
        pass  # dotenv not installed, use system environment
    
    node_id = _require_env("NODE_ID")
    environment = _get_env("ENVIRONMENT", "production")
    
    if environment not in ("development", "staging", "production"):
        raise ConfigError(
            f"ENVIRONMENT must be one of: development, staging, production; got: {environment}"
        )
    
    try:
        security = load_security_config(environment)
    except ValueError as e:
        raise ConfigError(f"Security configuration failed: {e}")
    
    try:
        kafka_topics, kafka_producer, kafka_consumer = load_kafka_config(environment)
    except ValueError as e:
        raise ConfigError(f"Kafka configuration failed: {e}")
    
    api = APIConfig(
        listen_addr=_get_env("API_LISTEN_ADDR", ":8080"),
        health_path=_get_env("API_HEALTH_PATH", "/health"),
        metrics_path=_get_env("API_METRICS_PATH", "/metrics"),
        admin_path_prefix=_get_env("API_ADMIN_PATH_PREFIX", "/admin")
    )
    
    models = ModelConfig(
        ddos_model_path=_require_env("MODEL_DDOS_PATH"),
        malware_model_path=_require_env("MODEL_MALWARE_PATH"),
        hot_reload_enabled=_get_bool_env("MODEL_HOT_RELOAD_ENABLED", True),
        reload_check_interval_seconds=_get_int_env("MODEL_RELOAD_CHECK_INTERVAL", 60)
    )
    
    detection = DetectionConfig(
        ddos_confidence_threshold=_get_float_env("DETECTION_DDOS_CONFIDENCE_THRESHOLD", 0.85),
        malware_confidence_threshold=_get_float_env("DETECTION_MALWARE_CONFIDENCE_THRESHOLD", 0.90),
        enable_ddos_pipeline=_get_bool_env("DETECTION_ENABLE_DDOS", True),
        enable_malware_pipeline=_get_bool_env("DETECTION_ENABLE_MALWARE", True),
        tracker_sample_window_size=_get_int_env("TRACKER_SAMPLE_WINDOW_SIZE", 500),
        tracker_sample_cap=_get_int_env("TRACKER_SAMPLE_CAP", 100),
        tracker_batch_size=_get_int_env("TRACKER_BATCH_SIZE", 50),
        tracker_flush_interval_seconds=_get_int_env("TRACKER_FLUSH_INTERVAL_SECONDS", 5),
    )
    
    limits = LimitsConfig(
        anomaly_max_size=_get_int_env("LIMITS_ANOMALY_MAX_SIZE", 256 * 1024),
        evidence_max_size=_get_int_env("LIMITS_EVIDENCE_MAX_SIZE", 2 * 1024 * 1024),
        policy_max_size=_get_int_env("LIMITS_POLICY_MAX_SIZE", 128 * 1024),
        timestamp_skew_seconds=_get_int_env("LIMITS_TIMESTAMP_SKEW_SECONDS", 60),
        nonce_ttl_seconds=_get_int_env("LIMITS_NONCE_TTL_SECONDS", 900),
        nonce_cleanup_interval_seconds=_get_int_env("LIMITS_NONCE_CLEANUP_INTERVAL_SECONDS", 60)
    )
    
    rate_limits = RateLimitConfig(
        global_capacity=_get_int_env("RATE_LIMIT_GLOBAL_CAPACITY", 1000),
        global_refill_rate=_get_float_env("RATE_LIMIT_GLOBAL_REFILL_RATE", 100.0),
        anomaly_capacity=_get_int_env("RATE_LIMIT_ANOMALY_CAPACITY", 500),
        anomaly_refill_rate=_get_float_env("RATE_LIMIT_ANOMALY_REFILL_RATE", 50.0),
        evidence_capacity=_get_int_env("RATE_LIMIT_EVIDENCE_CAPACITY", 200),
        evidence_refill_rate=_get_float_env("RATE_LIMIT_EVIDENCE_REFILL_RATE", 20.0),
        policy_capacity=_get_int_env("RATE_LIMIT_POLICY_CAPACITY", 100),
        policy_refill_rate=_get_float_env("RATE_LIMIT_POLICY_REFILL_RATE", 10.0)
    )
    
    circuit_breaker = CircuitBreakerConfig(
        failure_threshold=_get_int_env("CIRCUIT_BREAKER_FAILURE_THRESHOLD", 5),
        timeout_seconds=_get_int_env("CIRCUIT_BREAKER_TIMEOUT_SECONDS", 30),
        recovery_threshold=_get_int_env("CIRCUIT_BREAKER_RECOVERY_THRESHOLD", 2)
    )
    
    feedback = FeedbackConfig(
        lifecycle_ttl_seconds=_get_int_env("FEEDBACK_LIFECYCLE_TTL_SECONDS", 30 * 24 * 3600),
        admit_timeout_seconds=_get_int_env("FEEDBACK_ADMIT_TIMEOUT_SECONDS", 30),
        commit_timeout_seconds=_get_int_env("FEEDBACK_COMMIT_TIMEOUT_SECONDS", 300),
        lifecycle_batch_size=_get_int_env("FEEDBACK_LIFECYCLE_BATCH_SIZE", 1000),
        metric_window_realtime=_get_int_env("FEEDBACK_METRIC_WINDOW_REALTIME", 3600),
        metric_window_short=_get_int_env("FEEDBACK_METRIC_WINDOW_SHORT", 86400),
        metric_window_medium=_get_int_env("FEEDBACK_METRIC_WINDOW_MEDIUM", 604800),
        metric_window_long=_get_int_env("FEEDBACK_METRIC_WINDOW_LONG", 2592000),
        min_samples_realtime=_get_int_env("FEEDBACK_MIN_SAMPLES_REALTIME", 100),
        min_samples_short=_get_int_env("FEEDBACK_MIN_SAMPLES_SHORT", 1000),
        min_samples_medium=_get_int_env("FEEDBACK_MIN_SAMPLES_MEDIUM", 5000),
        min_samples_long=_get_int_env("FEEDBACK_MIN_SAMPLES_LONG", 20000),
        acceptance_rate_critical=_get_float_env("FEEDBACK_ACCEPTANCE_RATE_CRITICAL", 0.40),
        acceptance_rate_low=_get_float_env("FEEDBACK_ACCEPTANCE_RATE_LOW", 0.60),
        acceptance_rate_target_min=_get_float_env("FEEDBACK_ACCEPTANCE_RATE_TARGET_MIN", 0.70),
        acceptance_rate_target_max=_get_float_env("FEEDBACK_ACCEPTANCE_RATE_TARGET_MAX", 0.85),
        acceptance_rate_high=_get_float_env("FEEDBACK_ACCEPTANCE_RATE_HIGH", 0.90),
        decay_half_life_seconds=_get_int_env("FEEDBACK_DECAY_HALF_LIFE_SECONDS", 86400),
        rate_limit_per_anomaly=_get_int_env("FEEDBACK_RATE_LIMIT_PER_ANOMALY", 10),
        rate_limit_per_node=_get_int_env("FEEDBACK_RATE_LIMIT_PER_NODE", 10000),
        rate_limit_global=_get_int_env("FEEDBACK_RATE_LIMIT_GLOBAL", 100000),
        clock_skew_tolerance_seconds=_get_int_env("FEEDBACK_CLOCK_SKEW_TOLERANCE_SECONDS", 300),
        require_signature_for_backend=_get_bool_env("FEEDBACK_REQUIRE_SIGNATURE_FOR_BACKEND", True),
        audit_all_state_changes=_get_bool_env("FEEDBACK_AUDIT_ALL_STATE_CHANGES", True),
        redis_pipeline_size=_get_int_env("FEEDBACK_REDIS_PIPELINE_SIZE", 1000),
        redis_transaction_retry=_get_int_env("FEEDBACK_REDIS_TRANSACTION_RETRY", 3),
        calibration_method=_get_env("FEEDBACK_CALIBRATION_METHOD", "isotonic"),
        calibration_min_samples=_get_int_env("FEEDBACK_CALIBRATION_MIN_SAMPLES", 1000),
        calibration_retrain_interval=_get_int_env("FEEDBACK_CALIBRATION_RETRAIN_INTERVAL", 3600),
        calibration_acceptance_threshold=_get_float_env("FEEDBACK_CALIBRATION_ACCEPTANCE_THRESHOLD", 0.60),
        calibration_model_path=_get_env("FEEDBACK_CALIBRATION_MODEL_PATH", "data/models/calibration"),
        calibration_save_to_redis=_get_bool_env("FEEDBACK_CALIBRATION_SAVE_TO_REDIS", True),
        calibration_redis_key=_get_env("FEEDBACK_CALIBRATION_REDIS_KEY", "calibration:model:current"),
        disable_persistence=_get_bool_env("FEEDBACK_DISABLE_PERSISTENCE", False)
    )
    
    try:
        models.validate()
        detection.validate()
        limits.validate()
        rate_limits.validate()
        circuit_breaker.validate()
        feedback.validate()
    except ValueError as e:
        raise ConfigError(f"Configuration validation failed: {e}")
    
    _validate_production_gates(environment, security, kafka_producer, kafka_consumer)
    
    return LegacySettings(
        node_id=node_id,
        environment=environment,
        security=security,
        kafka_topics=kafka_topics,
        kafka_producer=kafka_producer,
        kafka_consumer=kafka_consumer,
        api=api,
        models=models,
        detection=detection,
        limits=limits,
        rate_limits=rate_limits,
        circuit_breaker=circuit_breaker,
        feedback=feedback
    )


def _validate_production_gates(
    environment: str,
    security: SecurityConfig,
    kafka_producer: KafkaProducerConfig,
    kafka_consumer: KafkaConsumerConfig
):
    """
    Enforce production security requirements.
    
    Fail-closed: Any violation raises ConfigError.
    """
    if environment != "production":
        return
    
    if not kafka_producer.security.tls_enabled:
        raise ConfigError("Production gate: Kafka TLS is required")
    
    if kafka_producer.security.sasl_mechanism == "PLAIN":
        raise ConfigError("Production gate: PLAIN SASL not allowed, use SCRAM-SHA-256 or SCRAM-SHA-512")
    
    if kafka_producer.acks not in ("all", "-1"):
        raise ConfigError("Production gate: Kafka producer acks must be 'all'")
    
    if not kafka_producer.idempotence_enabled:
        raise ConfigError("Production gate: Kafka producer idempotence must be enabled")
    
    if kafka_consumer.enable_auto_commit:
        raise ConfigError("Production gate: Kafka consumer auto-commit must be disabled")
    
    if not security.mtls.enabled and len(security.jwt.secret) < 128:
        raise ConfigError("Production gate: Must enable mTLS or use JWT secret >= 128 chars")
    
    if len(security.ed25519.signing_key_id) == 0:
        raise ConfigError("Production gate: Ed25519 signing key ID must be set")


def _require_env(key: str) -> str:
    value = os.getenv(key)
    if not value:
        raise ConfigError(f"Required environment variable {key} is not set")
    return value


def _get_env(key: str, default: str = "") -> str:
    return os.getenv(key, default)


def _get_bool_env(key: str, default: bool = False) -> bool:
    value = os.getenv(key)
    if value is None:
        return default
    return value.lower() in ("true", "1", "yes", "on")


def _get_int_env(key: str, default: int) -> int:
    value = os.getenv(key)
    if value is None:
        return default
    try:
        return int(value)
    except ValueError:
        raise ConfigError(f"Environment variable {key} must be an integer, got: {value}")


def _get_float_env(key: str, default: float) -> float:
    value = os.getenv(key)
    if value is None:
        return default
    try:
        return float(value)
    except ValueError:
        raise ConfigError(f"Environment variable {key} must be a float, got: {value}")
