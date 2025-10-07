"""
Configuration loader for AI service.

Loads and validates all configuration from environment variables.
Security-first: Fail fast on missing/invalid config in production.
"""
import os
from pathlib import Path
from typing import Optional
from .settings import Settings
from .kafka import KafkaTopicsConfig, KafkaProducerConfig, KafkaConsumerConfig, KafkaSecurityConfig
from ..utils.errors import ConfigError


def load_settings(env_file: Optional[str] = None) -> Settings:
    """
    Load and validate service configuration from environment.
    
    Args:
        env_file: Path to .env file (default: .env in current directory)
        
    Returns:
        Validated Settings object
        
    Raises:
        ConfigError: If configuration is invalid or incomplete
        
    Security:
        - Production mode requires all security configs
        - Development mode has relaxed requirements
        - All secrets validated for minimum length
        - TLS enforced in production
    """
    # Load .env file if specified
    if env_file:
        _load_env_file(env_file)
    
    # Determine environment
    environment = _get_env("ENVIRONMENT", "development")
    if environment not in ("development", "staging", "production"):
        raise ConfigError(
            f"Invalid ENVIRONMENT: {environment}. "
            f"Must be: development, staging, or production"
        )
    
    is_production = environment == "production"
    
    # Core configuration
    node_id = _require_env("NODE_ID", "Node ID is required")
    
    # Kafka configuration
    kafka_topics = _load_kafka_topics()
    kafka_security = _load_kafka_security(is_production)
    kafka_producer = _load_kafka_producer(kafka_security, is_production)
    kafka_consumer = _load_kafka_consumer(kafka_security, is_production)
    
    # Cryptographic configuration
    signing_key_path = _require_env("ED25519_SIGNING_KEY_PATH", "Signing key path required")
    signing_key_id = _require_env("ED25519_SIGNING_KEY_ID", "Signing key ID required")
    domain_separation = _get_env("ED25519_DOMAIN_SEPARATION", "ai.v1")
    
    # Validate key path
    _validate_key_path(signing_key_path, is_production)
    
    # Nonce manager configuration
    nonce_state_path = _get_env("NONCE_STATE_PATH", "./data/nonce_state.json")
    
    # Security configuration
    mtls_enabled = _get_bool_env("MTLS_ENABLED", is_production)
    jwt_enabled = _get_bool_env("JWT_ENABLED", False)
    jwt_secret = None
    if jwt_enabled:
        jwt_secret = _require_env("JWT_SECRET", "JWT secret required when JWT enabled")
        _validate_secret_strength(jwt_secret, "JWT_SECRET", min_length=128)
    
    # AES encryption key (for sensitive data at rest)
    aes_key = _get_env("AES_KEY")
    if aes_key:
        _validate_hex_key(aes_key, "AES_KEY", expected_length=64)  # 32 bytes = 64 hex chars
    
    # Model paths (optional, for AI core layer)
    model_ddos_path = _get_env("MODEL_DDOS_PATH")
    model_malware_path = _get_env("MODEL_MALWARE_PATH")
    
    # Build Settings object
    settings = Settings(
        environment=environment,
        node_id=node_id,
        kafka_topics=kafka_topics,
        kafka_producer=kafka_producer,
        kafka_consumer=kafka_consumer,
        signing_key_path=Path(signing_key_path),
        signing_key_id=signing_key_id,
        domain_separation=domain_separation,
        nonce_state_path=Path(nonce_state_path),
        mtls_enabled=mtls_enabled,
        jwt_enabled=jwt_enabled,
        jwt_secret=jwt_secret,
        aes_key=aes_key,
        model_ddos_path=Path(model_ddos_path) if model_ddos_path else None,
        model_malware_path=Path(model_malware_path) if model_malware_path else None,
    )
    
    # Final validation
    kafka_topics.validate()
    kafka_producer.validate(environment)
    kafka_consumer.validate(environment)
    
    return settings


def _load_env_file(env_file: str):
    """Load environment variables from .env file."""
    if not os.path.exists(env_file):
        raise ConfigError(f".env file not found: {env_file}")
    
    try:
        from dotenv import load_dotenv
        load_dotenv(env_file, override=True)
    except ImportError:
        raise ConfigError(
            "python-dotenv not installed. "
            "Install with: pip install python-dotenv"
        )


def _load_kafka_topics() -> KafkaTopicsConfig:
    """Load Kafka topic configuration."""
    return KafkaTopicsConfig(
        ai_anomalies=_get_env("TOPIC_AI_ANOMALIES", "ai.anomalies.v1"),
        ai_evidence=_get_env("TOPIC_AI_EVIDENCE", "ai.evidence.v1"),
        ai_policy=_get_env("TOPIC_AI_POLICY", "ai.policy.v1"),
        control_commits=_get_env("TOPIC_CONTROL_COMMITS", "control.commits.v1"),
        control_reputation=_get_env("TOPIC_CONTROL_REPUTATION", "control.reputation.v1"),
        control_policy=_get_env("TOPIC_CONTROL_POLICY", "control.policy.v1"),
        control_evidence=_get_env("TOPIC_CONTROL_EVIDENCE", "control.evidence.v1"),
        dlq=_get_env("TOPIC_DLQ", "ai.dlq.v1"),
    )


def _load_kafka_security(is_production: bool) -> KafkaSecurityConfig:
    """Load Kafka security configuration."""
    tls_enabled = _get_bool_env("KAFKA_TLS_ENABLED", True)
    sasl_mechanism = _get_env("KAFKA_SASL_MECHANISM", "SCRAM-SHA-256" if is_production else "PLAIN")
    
    # SASL credentials
    sasl_username = None
    sasl_password = None
    if sasl_mechanism != "NONE":
        sasl_username = _require_env("KAFKA_SASL_USERNAME", "SASL username required")
        sasl_password = _require_env("KAFKA_SASL_PASSWORD", "SASL password required")
        
        # Validate password strength
        min_length = 32 if is_production else 16
        if len(sasl_password) < min_length:
            raise ConfigError(
                f"KAFKA_SASL_PASSWORD too weak: {len(sasl_password)} chars "
                f"(minimum {min_length} required in {'production' if is_production else 'development'})"
            )
    
    # TLS certificates
    ca_cert_path = _get_env("KAFKA_CA_CERT_PATH")
    client_cert_path = _get_env("KAFKA_CLIENT_CERT_PATH")
    client_key_path = _get_env("KAFKA_CLIENT_KEY_PATH")
    
    # Production TLS validation (allow missing CA cert if using cloud Kafka like Confluent)
    # CA cert is optional if SASL is configured (cloud providers handle TLS internally)
    if is_production and tls_enabled and not ca_cert_path and sasl_mechanism == "NONE":
        raise ConfigError(
            "KAFKA_CA_CERT_PATH required in production with TLS enabled and no SASL"
        )
    
    return KafkaSecurityConfig(
        tls_enabled=tls_enabled,
        sasl_mechanism=sasl_mechanism,
        sasl_username=sasl_username,
        sasl_password=sasl_password,
        ca_cert_path=ca_cert_path,
        client_cert_path=client_cert_path,
        client_key_path=client_key_path,
    )


def _load_kafka_producer(security: KafkaSecurityConfig, is_production: bool) -> KafkaProducerConfig:
    """Load Kafka producer configuration."""
    # Support both KAFKA_BOOTSTRAP_SERVERS and KAFKA_BROKERS (backend uses KAFKA_BROKERS)
    bootstrap_servers = _get_env("KAFKA_BOOTSTRAP_SERVERS") or _get_env("KAFKA_BROKERS")
    if not bootstrap_servers:
        raise ConfigError("Kafka bootstrap servers required: KAFKA_BOOTSTRAP_SERVERS or KAFKA_BROKERS not set")
    
    return KafkaProducerConfig(
        bootstrap_servers=bootstrap_servers,
        security=security,
        acks=_get_env("KAFKA_PRODUCER_ACKS", "all"),
        retries=_get_int_env("KAFKA_PRODUCER_RETRIES", 10),
        max_in_flight_requests=_get_int_env("KAFKA_PRODUCER_MAX_IN_FLIGHT", 5),
        idempotence_enabled=_get_bool_env("KAFKA_PRODUCER_IDEMPOTENCE", True),
        compression_type=_get_env("KAFKA_PRODUCER_COMPRESSION", "snappy"),
        batch_size=_get_int_env("KAFKA_PRODUCER_BATCH_SIZE", 16384),
        linger_ms=_get_int_env("KAFKA_PRODUCER_LINGER_MS", 10),
        buffer_memory=_get_int_env("KAFKA_PRODUCER_BUFFER_MEMORY", 33554432),
        request_timeout_ms=_get_int_env("KAFKA_PRODUCER_REQUEST_TIMEOUT_MS", 30000),
    )


def _load_kafka_consumer(security: KafkaSecurityConfig, is_production: bool) -> KafkaConsumerConfig:
    """Load Kafka consumer configuration."""
    # Support both KAFKA_BOOTSTRAP_SERVERS and KAFKA_BROKERS (backend uses KAFKA_BROKERS)
    bootstrap_servers = _get_env("KAFKA_BOOTSTRAP_SERVERS") or _get_env("KAFKA_BROKERS")
    if not bootstrap_servers:
        raise ConfigError("Kafka bootstrap servers required: KAFKA_BOOTSTRAP_SERVERS or KAFKA_BROKERS not set")
    group_id = _get_env("KAFKA_CONSUMER_GROUP_ID", f"ai-service-{_require_env('NODE_ID', 'Node ID required')}")
    
    return KafkaConsumerConfig(
        bootstrap_servers=bootstrap_servers,
        security=security,
        group_id=group_id,
        auto_offset_reset=_get_env("KAFKA_CONSUMER_AUTO_OFFSET_RESET", "earliest"),
        enable_auto_commit=_get_bool_env("KAFKA_CONSUMER_ENABLE_AUTO_COMMIT", False),
        max_poll_records=_get_int_env("KAFKA_CONSUMER_MAX_POLL_RECORDS", 500),
        max_poll_interval_ms=_get_int_env("KAFKA_CONSUMER_MAX_POLL_INTERVAL_MS", 300000),
        session_timeout_ms=_get_int_env("KAFKA_CONSUMER_SESSION_TIMEOUT_MS", 10000),
        heartbeat_interval_ms=_get_int_env("KAFKA_CONSUMER_HEARTBEAT_INTERVAL_MS", 3000),
        fetch_min_bytes=_get_int_env("KAFKA_CONSUMER_FETCH_MIN_BYTES", 1),
        fetch_max_wait_ms=_get_int_env("KAFKA_CONSUMER_FETCH_MAX_WAIT_MS", 500),
    )


def _validate_key_path(path: str, is_production: bool):
    """Validate signing key path."""
    p = Path(path)
    
    # In production, require absolute path
    if is_production and not p.is_absolute():
        raise ConfigError(
            f"ED25519_SIGNING_KEY_PATH must be absolute in production: {path}"
        )
    
    # If path exists, validate it's a file
    if p.exists() and not p.is_file():
        raise ConfigError(
            f"ED25519_SIGNING_KEY_PATH is not a file: {path}"
        )


def _validate_secret_strength(secret: str, name: str, min_length: int):
    """Validate secret meets minimum strength requirements."""
    if len(secret) < min_length:
        raise ConfigError(
            f"{name} too weak: {len(secret)} chars (minimum {min_length} required)"
        )
    
    # Check for placeholder values
    if secret in ("CHANGE_ME", "changeme", "secret", "password"):
        raise ConfigError(
            f"{name} contains placeholder value. Set a real secret."
        )


def _validate_hex_key(key: str, name: str, expected_length: int):
    """Validate hex-encoded key."""
    if len(key) != expected_length:
        raise ConfigError(
            f"{name} invalid length: {len(key)} chars (expected {expected_length} hex chars)"
        )
    
    try:
        int(key, 16)
    except ValueError:
        raise ConfigError(
            f"{name} is not valid hexadecimal"
        )


def _require_env(key: str, error_msg: str) -> str:
    """Get required environment variable."""
    value = os.getenv(key)
    if not value:
        raise ConfigError(f"{error_msg}: {key} not set")
    return value


def _get_env(key: str, default: str = "") -> str:
    """Get optional environment variable."""
    return os.getenv(key, default)


def _get_bool_env(key: str, default: bool = False) -> bool:
    """Get boolean environment variable."""
    value = os.getenv(key)
    if value is None:
        return default
    return value.lower() in ("true", "1", "yes", "on")


def _get_int_env(key: str, default: int) -> int:
    """Get integer environment variable."""
    value = os.getenv(key)
    if value is None:
        return default
    try:
        return int(value)
    except ValueError:
        raise ConfigError(
            f"Invalid integer value for {key}: {value}"
        )
