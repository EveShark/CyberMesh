import os
from typing import Optional, Dict, Any
from dataclasses import dataclass


@dataclass(frozen=True)
class KafkaTopicsConfig:
    """Kafka topic names."""
    ai_anomalies: str
    ai_evidence: str
    ai_policy: str
    control_commits: str
    control_reputation: str
    control_policy: str
    control_policy_ack: str
    control_evidence: str
    dlq: str
    
    def validate(self):
        """Validate topic names are non-empty."""
        topics = [
            self.ai_anomalies,
            self.ai_evidence,
            self.ai_policy,
            self.control_commits,
            self.control_reputation,
            self.control_policy,
            self.control_policy_ack,
            self.control_evidence,
            self.dlq
        ]
        
        for topic in topics:
            if not topic or len(topic) == 0:
                raise ValueError("Topic name cannot be empty")
            if len(topic) > 249:
                raise ValueError(f"Topic name too long: {topic}")


@dataclass(frozen=True)
class KafkaSecurityConfig:
    """Kafka security settings."""
    tls_enabled: bool
    sasl_mechanism: str
    sasl_username: Optional[str]
    sasl_password: Optional[str]
    ca_cert_path: Optional[str]
    client_cert_path: Optional[str]
    client_key_path: Optional[str]
    
    ALLOWED_SASL_MECHANISMS = ("NONE", "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512", "GSSAPI", "OAUTHBEARER")
    
    def validate(self, environment: str):
        """Validate Kafka security configuration."""
        if environment == "production" and not self.tls_enabled:
            raise ValueError("TLS is required for Kafka in production")
        
        if self.tls_enabled and environment == "production":
            if not self.ca_cert_path:
                raise ValueError("CA certificate required for TLS in production")
            
            if self.ca_cert_path and not os.path.exists(self.ca_cert_path):
                raise ValueError(f"CA certificate not found: {self.ca_cert_path}")
        
        if self.sasl_mechanism not in self.ALLOWED_SASL_MECHANISMS:
            raise ValueError(
                f"Unsupported SASL mechanism: {self.sasl_mechanism}. "
                f"Allowed: {', '.join(self.ALLOWED_SASL_MECHANISMS)}"
            )
        
        if environment == "production" and self.sasl_mechanism == "PLAIN":
            raise ValueError("PLAIN SASL mechanism not allowed in production, use SCRAM")
        
        if self.sasl_mechanism != "NONE":
            if not self.sasl_username:
                raise ValueError(f"SASL username required for mechanism: {self.sasl_mechanism}")
            
            if not self.sasl_password:
                raise ValueError(f"SASL password required for mechanism: {self.sasl_mechanism}")
            
            if len(self.sasl_password) < 16:
                raise ValueError("SASL password too weak, minimum 16 characters required")
            
            if environment == "production" and len(self.sasl_password) < 32:
                raise ValueError("SASL password must be at least 32 characters in production")


@dataclass(frozen=True)
class KafkaProducerConfig:
    """Kafka producer configuration."""
    bootstrap_servers: str
    security: KafkaSecurityConfig
    acks: str = "all"
    retries: int = 10
    max_in_flight_requests: int = 5
    idempotence_enabled: bool = True
    compression_type: str = "snappy"
    batch_size: int = 16384
    linger_ms: int = 10
    buffer_memory: int = 33554432
    request_timeout_ms: int = 30000
    
    def validate(self, environment: str):
        """Validate producer configuration."""
        self.security.validate(environment)
        
        if self.acks not in ("all", "-1", "1", "0"):
            raise ValueError(f"Invalid acks value: {self.acks}")
        
        if environment == "production" and self.acks not in ("all", "-1"):
            raise ValueError("Producer acks must be 'all' in production")
        
        if not self.idempotence_enabled and environment == "production":
            raise ValueError("Idempotence must be enabled in production")
        
        if self.compression_type not in ("none", "gzip", "snappy", "lz4", "zstd"):
            raise ValueError(f"Invalid compression type: {self.compression_type}")
        
        if self.retries < 0:
            raise ValueError("Retries cannot be negative")
        
        if self.max_in_flight_requests < 1:
            raise ValueError("max_in_flight_requests must be at least 1")
        
        if self.batch_size <= 0:
            raise ValueError(f"batch_size must be positive, got: {self.batch_size}")
        
        if self.linger_ms < 0:
            raise ValueError(f"linger_ms cannot be negative, got: {self.linger_ms}")
        
        if self.buffer_memory <= 0:
            raise ValueError(f"buffer_memory must be positive, got: {self.buffer_memory}")
        
        if self.request_timeout_ms <= 0:
            raise ValueError(f"request_timeout_ms must be positive, got: {self.request_timeout_ms}")
    
    def to_kafka_config(self) -> Dict[str, Any]:
        """Convert to kafka-python producer config dict."""
        config = {
            "bootstrap_servers": self.bootstrap_servers.split(","),
            "acks": self.acks,
            "retries": self.retries,
            "max_in_flight_requests_per_connection": self.max_in_flight_requests,
            # Note: kafka-python doesn't support enable_idempotence
            # Using acks='all' + retries provides at-least-once delivery
            "compression_type": self.compression_type,
            "batch_size": self.batch_size,
            "linger_ms": self.linger_ms,
            "buffer_memory": self.buffer_memory,
            "request_timeout_ms": self.request_timeout_ms,
        }
        
        # Security protocol based on TLS + SASL settings
        if self.security.sasl_mechanism == "NONE":
            config["security_protocol"] = "SSL" if self.security.tls_enabled else "PLAINTEXT"
        else:
            config["security_protocol"] = "SASL_SSL" if self.security.tls_enabled else "SASL_PLAINTEXT"
            config["sasl_mechanism"] = self.security.sasl_mechanism
            config["sasl_plain_username"] = self.security.sasl_username
            config["sasl_plain_password"] = self.security.sasl_password
        
        if self.security.tls_enabled:
            if self.security.ca_cert_path:
                config["ssl_cafile"] = self.security.ca_cert_path
            if self.security.client_cert_path:
                config["ssl_certfile"] = self.security.client_cert_path
            if self.security.client_key_path:
                config["ssl_keyfile"] = self.security.client_key_path
        
        return config


@dataclass(frozen=True)
class KafkaConsumerConfig:
    """Kafka consumer configuration."""
    bootstrap_servers: str
    security: KafkaSecurityConfig
    group_id: str
    auto_offset_reset: str = "earliest"
    enable_auto_commit: bool = False
    max_poll_records: int = 500
    max_poll_interval_ms: int = 300000
    session_timeout_ms: int = 10000
    heartbeat_interval_ms: int = 3000
    fetch_min_bytes: int = 1
    fetch_max_wait_ms: int = 500
    
    def validate(self, environment: str):
        """Validate consumer configuration."""
        self.security.validate(environment)
        
        if len(self.group_id) == 0:
            raise ValueError("Consumer group_id cannot be empty")
        
        if self.auto_offset_reset not in ("earliest", "latest", "none"):
            raise ValueError(f"Invalid auto_offset_reset: {self.auto_offset_reset}")
        
        if environment == "production" and self.enable_auto_commit:
            raise ValueError("Auto-commit must be disabled in production for manual offset management")
        
        if self.max_poll_records < 1:
            raise ValueError("max_poll_records must be at least 1")
        
        if self.max_poll_interval_ms <= 0:
            raise ValueError(f"max_poll_interval_ms must be positive, got: {self.max_poll_interval_ms}")
        
        if self.session_timeout_ms <= 0:
            raise ValueError(f"session_timeout_ms must be positive, got: {self.session_timeout_ms}")
        
        if self.heartbeat_interval_ms <= 0:
            raise ValueError(f"heartbeat_interval_ms must be positive, got: {self.heartbeat_interval_ms}")
        
        if self.fetch_min_bytes < 0:
            raise ValueError(f"fetch_min_bytes cannot be negative, got: {self.fetch_min_bytes}")
        
        if self.fetch_max_wait_ms < 0:
            raise ValueError(f"fetch_max_wait_ms cannot be negative, got: {self.fetch_max_wait_ms}")
        
        if self.session_timeout_ms < self.heartbeat_interval_ms * 3:
            raise ValueError("session_timeout_ms must be at least 3x heartbeat_interval_ms")
    
    def to_kafka_config(self) -> Dict[str, Any]:
        """Convert to kafka-python consumer config dict."""
        config = {
            "bootstrap_servers": self.bootstrap_servers.split(","),
            "group_id": self.group_id,
            "auto_offset_reset": self.auto_offset_reset,
            "enable_auto_commit": self.enable_auto_commit,
            "max_poll_records": self.max_poll_records,
            "max_poll_interval_ms": self.max_poll_interval_ms,
            "session_timeout_ms": self.session_timeout_ms,
            "heartbeat_interval_ms": self.heartbeat_interval_ms,
            "fetch_min_bytes": self.fetch_min_bytes,
            "fetch_max_wait_ms": self.fetch_max_wait_ms,
        }
        
        # Security protocol based on TLS + SASL settings
        if self.security.sasl_mechanism == "NONE":
            config["security_protocol"] = "SSL" if self.security.tls_enabled else "PLAINTEXT"
        else:
            config["security_protocol"] = "SASL_SSL" if self.security.tls_enabled else "SASL_PLAINTEXT"
            config["sasl_mechanism"] = self.security.sasl_mechanism
            config["sasl_plain_username"] = self.security.sasl_username
            config["sasl_plain_password"] = self.security.sasl_password
        
        if self.security.tls_enabled:
            if self.security.ca_cert_path:
                config["ssl_cafile"] = self.security.ca_cert_path
            if self.security.client_cert_path:
                config["ssl_certfile"] = self.security.client_cert_path
            if self.security.client_key_path:
                config["ssl_keyfile"] = self.security.client_key_path
        
        return config


def load_kafka_config(environment: str) -> tuple[KafkaTopicsConfig, KafkaProducerConfig, KafkaConsumerConfig]:
    """Load and validate Kafka configuration from environment variables."""
    bootstrap_servers = _require_env("KAFKA_BOOTSTRAP_SERVERS")
    
    sasl_mechanism = _get_env("KAFKA_SASL_MECHANISM", "SCRAM-SHA-256")
    
    if sasl_mechanism == "NONE":
        sasl_username = None
        sasl_password = None
    else:
        sasl_username = _require_env("KAFKA_SASL_USERNAME")
        sasl_password = _require_env("KAFKA_SASL_PASSWORD")
    
    security = KafkaSecurityConfig(
        tls_enabled=_get_bool_env("KAFKA_TLS_ENABLED", True),
        sasl_mechanism=sasl_mechanism,
        sasl_username=sasl_username,
        sasl_password=sasl_password,
        ca_cert_path=_get_env("KAFKA_CA_CERT_PATH") or None,
        client_cert_path=_get_env("KAFKA_CLIENT_CERT_PATH") or None,
        client_key_path=_get_env("KAFKA_CLIENT_KEY_PATH") or None
    )
    
    topics = KafkaTopicsConfig(
        ai_anomalies=_get_env("TOPIC_AI_ANOMALIES", "ai.anomalies.v1"),
        ai_evidence=_get_env("TOPIC_AI_EVIDENCE", "ai.evidence.v1"),
        ai_policy=_get_env("TOPIC_AI_POLICY", "ai.policy.v1"),
        control_commits=_get_env("TOPIC_CONTROL_COMMITS", "control.commits.v1"),
        control_reputation=_get_env("TOPIC_CONTROL_REPUTATION", "control.reputation.v1"),
        control_policy=_get_env("TOPIC_CONTROL_POLICY", "control.policy.v1"),
        control_evidence=_get_env("TOPIC_CONTROL_EVIDENCE", "control.evidence.v1"),
        dlq=_get_env("TOPIC_DLQ", "ai.dlq.v1")
    )
    
    producer = KafkaProducerConfig(
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
        request_timeout_ms=_get_int_env("KAFKA_PRODUCER_REQUEST_TIMEOUT_MS", 30000)
    )
    
    consumer = KafkaConsumerConfig(
        bootstrap_servers=bootstrap_servers,
        security=security,
        group_id=_get_env("KAFKA_CONSUMER_GROUP_ID", f"ai-service-{_require_env('NODE_ID')}"),
        auto_offset_reset=_get_env("KAFKA_CONSUMER_AUTO_OFFSET_RESET", "earliest"),
        enable_auto_commit=_get_bool_env("KAFKA_CONSUMER_ENABLE_AUTO_COMMIT", False),
        max_poll_records=_get_int_env("KAFKA_CONSUMER_MAX_POLL_RECORDS", 500),
        max_poll_interval_ms=_get_int_env("KAFKA_CONSUMER_MAX_POLL_INTERVAL_MS", 300000),
        session_timeout_ms=_get_int_env("KAFKA_CONSUMER_SESSION_TIMEOUT_MS", 10000),
        heartbeat_interval_ms=_get_int_env("KAFKA_CONSUMER_HEARTBEAT_INTERVAL_MS", 3000),
        fetch_min_bytes=_get_int_env("KAFKA_CONSUMER_FETCH_MIN_BYTES", 1),
        fetch_max_wait_ms=_get_int_env("KAFKA_CONSUMER_FETCH_MAX_WAIT_MS", 500)
    )
    
    topics.validate()
    producer.validate(environment)
    consumer.validate(environment)
    
    return topics, producer, consumer


def _require_env(key: str) -> str:
    value = os.getenv(key)
    if not value:
        raise ValueError(f"Required environment variable {key} is not set")
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
        raise ValueError(f"Environment variable {key} must be an integer, got: {value}")
