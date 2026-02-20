import json
import logging
from typing import Any, Dict, Set


SENSITIVE_FIELDS = {
    "signature", "private_key", "secret", "password", "token",
    "api_key", "auth", "credential", "payload", "content",
    "sasl_password", "sasl_username"
}


def redact_dict(data: Dict[str, Any], sensitive_fields: Set[str] = SENSITIVE_FIELDS) -> Dict[str, Any]:
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


def safe_log_dict(logger: logging.Logger, level: int, message: str, data: Dict[str, Any]):
    redacted_data = redact_dict(data)
    log_message = f"{message}: {json.dumps(redacted_data)}"
    logger.log(level, log_message)


class SecureLogger:
    def __init__(self, name: str):
        self.logger = logging.getLogger(name)
    
    def _format_extras(self, **kwargs) -> str:
        if not kwargs:
            return ""
        redacted = redact_dict(kwargs)
        return " " + " ".join(f"{k}={v}" for k, v in redacted.items())
    
    def info(self, message: str, **kwargs):
        self.logger.info(message + self._format_extras(**kwargs))
    
    def warning(self, message: str, **kwargs):
        self.logger.warning(message + self._format_extras(**kwargs))
    
    def error(self, message: str, **kwargs):
        self.logger.error(message + self._format_extras(**kwargs))
    
    def debug(self, message: str, **kwargs):
        self.logger.debug(message + self._format_extras(**kwargs))
    
    def critical(self, message: str, **kwargs):
        self.logger.critical(message + self._format_extras(**kwargs))


def configure_logging(level: str = "INFO"):
    log_level = getattr(logging, level.upper(), logging.INFO)
    
    logging.basicConfig(
        level=log_level,
        format='{"timestamp":"%(asctime)s","level":"%(levelname)s","logger":"%(name)s","message":"%(message)s"}',
        datefmt='%Y-%m-%dT%H:%M:%S'
    )
