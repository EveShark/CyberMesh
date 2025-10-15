from typing import Optional
from .errors import ValidationError, TimestampSkewError
from .time import now
from .limits import VALUE_LIMITS, TIME_LIMITS
from .id_gen import validate_uuid4


def validate_size(data_size: int, max_size: int, data_type: str):
    if data_size < 0:
        raise ValidationError(f"{data_type} size cannot be negative")
    
    if data_size > max_size:
        raise ValidationError(
            f"{data_type} size {data_size} exceeds maximum {max_size} bytes"
        )


def validate_message_size(data_size: int, message_type: str):
    """Deprecated: Use validate_size directly with SIZE_LIMITS."""
    from .limits import SIZE_LIMITS
    if message_type == "anomaly":
        max_size = SIZE_LIMITS.MAX_PAYLOAD_SIZE
    elif message_type == "evidence":
        max_size = SIZE_LIMITS.MAX_EVIDENCE_SIZE
    elif message_type == "policy":
        max_size = SIZE_LIMITS.MAX_POLICY_PARAMS_SIZE
    else:
        raise ValidationError(f"Unknown message type: {message_type}")
    validate_size(data_size, max_size, message_type)


def validate_severity(severity: int):
    if not (1 <= severity <= 10):
        raise ValidationError(f"Severity {severity} must be in range [1, 10]")


def validate_confidence(confidence: float):
    if not (VALUE_LIMITS.CONFIDENCE_MIN <= confidence <= VALUE_LIMITS.CONFIDENCE_MAX):
        raise ValidationError(
            f"Confidence {confidence} must be in range [{VALUE_LIMITS.CONFIDENCE_MIN}, {VALUE_LIMITS.CONFIDENCE_MAX}]"
        )


def validate_timestamp(timestamp: int, max_skew_seconds: int = None):
    """
    Validate timestamp is within acceptable skew from current time.
    
    Args:
        timestamp: Unix timestamp in seconds
        max_skew_seconds: Maximum allowed clock skew (default: TIME_LIMITS.TIMESTAMP_SKEW_SECONDS)
    
    Raises:
        ValidationError: If timestamp is invalid or outside acceptable skew
    """
    if timestamp <= 0:
        raise ValidationError(f"Timestamp must be positive, got: {timestamp}")
    
    if max_skew_seconds is None:
        from .limits import TIME_LIMITS
        max_skew_seconds = TIME_LIMITS.TIMESTAMP_SKEW_SECONDS
    
    from .time import now
    current_time = now()
    
    if timestamp > current_time + max_skew_seconds:
        raise TimestampSkewError(
            f"Timestamp {timestamp} is too far in the future (current: {current_time}, max skew: {max_skew_seconds}s)"
        )
    
    if timestamp < current_time - max_skew_seconds:
        raise TimestampSkewError(
            f"Timestamp {timestamp} is too far in the past (current: {current_time}, max skew: {max_skew_seconds}s)"
        )


def validate_uuid(value: str, field_name: str = "ID"):
    if not validate_uuid4(value):
        raise ValidationError(f"{field_name} must be a valid UUIDv4")


def validate_required_string(value: Optional[str], field_name: str, max_length: Optional[int] = None):
    if not value:
        raise ValidationError(f"{field_name} is required")
    
    if not isinstance(value, str):
        raise ValidationError(f"{field_name} must be a string")
    
    if max_length and len(value) > max_length:
        raise ValidationError(
            f"{field_name} length {len(value)} exceeds maximum {max_length}"
        )


def validate_hex_string(value: str, field_name: str, expected_length: Optional[int] = None):
    if not value:
        raise ValidationError(f"{field_name} is required")
    
    try:
        int(value, 16)
    except ValueError:
        raise ValidationError(f"{field_name} must be a valid hexadecimal string")
    
    if expected_length and len(value) != expected_length:
        raise ValidationError(
            f"{field_name} length {len(value)} must be exactly {expected_length}"
        )
