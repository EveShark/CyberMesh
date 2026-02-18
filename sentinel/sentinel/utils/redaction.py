"""Secret redaction for logging and output."""

import json
import logging
from typing import Any, Dict, Set


SENSITIVE_FIELDS: Set[str] = {
    "signature", "private_key", "secret", "password", "token",
    "api_key", "auth", "credential", "payload", "content",
    "sasl_password", "sasl_username", "key", "apikey"
}


def redact_dict(
    data: Dict[str, Any],
    sensitive_fields: Set[str] = SENSITIVE_FIELDS
) -> Dict[str, Any]:
    """
    Recursively redact sensitive fields from a dictionary.
    
    Args:
        data: Dictionary to redact
        sensitive_fields: Set of field name patterns to redact
        
    Returns:
        New dictionary with sensitive values replaced with [REDACTED]
    """
    redacted = {}
    for key, value in data.items():
        key_lower = key.lower()
        
        if any(sensitive in key_lower for sensitive in sensitive_fields):
            redacted[key] = "[REDACTED]"
        elif isinstance(value, dict):
            redacted[key] = redact_dict(value, sensitive_fields)
        elif isinstance(value, (list, tuple)):
            redacted[key] = [
                redact_dict(item, sensitive_fields) if isinstance(item, dict) else item
                for item in value
            ]
        else:
            redacted[key] = value
    
    return redacted


def safe_log_dict(
    logger: logging.Logger,
    level: int,
    message: str,
    data: Dict[str, Any]
) -> None:
    """Log a dictionary with sensitive fields redacted."""
    redacted_data = redact_dict(data)
    log_message = f"{message}: {json.dumps(redacted_data)}"
    logger.log(level, log_message)


def redact_secrets(text: str, patterns: Set[str] = None) -> str:
    """
    Redact secrets from a string.
    
    Args:
        text: Text to redact
        patterns: Optional set of patterns to redact
        
    Returns:
        Text with secrets replaced by [REDACTED]
    """
    import re
    
    if patterns is None:
        patterns = SENSITIVE_FIELDS
    
    result = text
    
    # Redact common secret patterns
    # API keys (32+ hex chars)
    result = re.sub(r'[a-fA-F0-9]{32,}', '[REDACTED]', result)
    
    # Bearer tokens
    result = re.sub(r'Bearer\s+\S+', 'Bearer [REDACTED]', result)
    
    # Key=value patterns for sensitive fields
    for pattern in patterns:
        result = re.sub(
            rf'({pattern})\s*[=:]\s*\S+',
            rf'\1=[REDACTED]',
            result,
            flags=re.IGNORECASE
        )
    
    return result
