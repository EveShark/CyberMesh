from dataclasses import dataclass


@dataclass(frozen=True)
class SizeLimits:
    """
    Size limits for message components (aligned with backend).
    
    Backend limits (backend/pkg/ingest/kafka/schema.go):
    - MaxPayloadSize: 512KB
    - MaxProofSize: 256KB
    """
    MAX_PAYLOAD_SIZE: int = 512 * 1024      # Anomaly payload
    MAX_EVIDENCE_SIZE: int = 256 * 1024     # Evidence proof_blob
    MAX_POLICY_PARAMS_SIZE: int = 512 * 1024  # Policy params


@dataclass(frozen=True)
class TimeLimits:
    """Time-related validation limits."""
    TIMESTAMP_SKEW_SECONDS: int = 60
    NONCE_TTL_SECONDS: int = 900
    NONCE_CLEANUP_INTERVAL_SECONDS: int = 60


@dataclass(frozen=True)
class ValueLimits:
    """Value range limits."""
    SEVERITY_MIN: int = 1
    SEVERITY_MAX: int = 10
    CONFIDENCE_MIN: float = 0.0
    CONFIDENCE_MAX: float = 1.0


@dataclass(frozen=True)
class StringLimits:
    """String field length limits."""
    ID_LENGTH: int = 36
    KEY_ID_MAX_LENGTH: int = 128
    TYPE_MAX_LENGTH: int = 64
    NONCE_LENGTH: int = 32


SIZE_LIMITS = SizeLimits()
TIME_LIMITS = TimeLimits()
VALUE_LIMITS = ValueLimits()
STRING_LIMITS = StringLimits()
