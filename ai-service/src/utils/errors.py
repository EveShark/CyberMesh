class CyberMeshError(Exception):
    """Base exception for all CyberMesh AI service errors."""
    pass


class SecurityError(CyberMeshError):
    """Base exception for security-related errors."""
    pass


class SignerError(SecurityError):
    """Cryptographic signing operation failed."""
    pass


class NonceError(SecurityError):
    """Nonce generation or validation failed."""
    pass


class ValidationError(CyberMeshError):
    """Input validation failed."""
    pass


class ConfigError(CyberMeshError):
    """Configuration loading or validation failed."""
    pass


class RateLimitError(CyberMeshError):
    """Rate limit exceeded."""
    pass


class StorageError(CyberMeshError):
    """Storage/persistence layer error (Redis, database, etc.)."""
    pass


class CircuitBreakerError(CyberMeshError):
    """Circuit breaker is open, operation not allowed."""
    pass


class SchemaError(CyberMeshError):
    """Protobuf schema encoding/decoding failed."""
    pass


class ContractError(SecurityError):
    """Contract message validation, signing, or verification failed."""
    pass


class SecretsError(SecurityError):
    """Secret loading or encryption operation failed."""
    pass


class BackoffError(CyberMeshError):
    """Backoff operation failed."""
    pass


class MetricsError(CyberMeshError):
    """Metrics collection or export failed."""
    pass


class KafkaError(CyberMeshError):
    """Kafka producer or consumer operation failed."""
    pass


class SignatureError(SecurityError):
    """Digital signature operation failed."""
    pass


class ServiceError(CyberMeshError):
    """Service lifecycle or operation error."""
    pass
