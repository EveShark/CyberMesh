"""
Logging configuration for AI service.

Security-first structured logging with automatic secret redaction.
"""
import logging
import logging.handlers
import json
import sys
import os
from pathlib import Path
from datetime import datetime
from typing import Optional, Dict, Any
from ..utils.redaction import redact_dict


class JSONFormatter(logging.Formatter):
    """
    JSON log formatter for production environments.
    
    Outputs structured logs in JSON format for machine parsing.
    All sensitive data is automatically redacted.
    
    Security:
        - Secrets redacted via utils.redaction
        - Stack traces included but sanitized
        - No raw exception objects (converted to strings)
    """
    
    def __init__(self, include_trace_id: bool = True):
        super().__init__()
        self.include_trace_id = include_trace_id
    
    def format(self, record: logging.LogRecord) -> str:
        """Format log record as JSON."""
        # Base log data
        log_data = {
            "timestamp": datetime.utcfromtimestamp(record.created).isoformat() + "Z",
            "level": record.levelname,
            "logger": record.name,
            "module": record.module,
            "function": record.funcName,
            "line": record.lineno,
            "message": record.getMessage(),
        }
        
        # Add trace_id if present
        if self.include_trace_id and hasattr(record, "trace_id"):
            log_data["trace_id"] = record.trace_id
        
        # Add exception info if present
        if record.exc_info:
            log_data["exception"] = {
                "type": record.exc_info[0].__name__ if record.exc_info[0] else None,
                "message": str(record.exc_info[1]) if record.exc_info[1] else None,
                "traceback": self.formatException(record.exc_info),
            }
        
        # Add extra fields from record
        extra_fields = {}
        for key, value in record.__dict__.items():
            if key not in (
                "name", "msg", "args", "created", "filename", "funcName",
                "levelname", "levelno", "lineno", "module", "msecs",
                "message", "pathname", "process", "processName", "relativeCreated",
                "thread", "threadName", "exc_info", "exc_text", "stack_info",
                "trace_id",
            ):
                extra_fields[key] = value
        
        if extra_fields:
            log_data["extra"] = extra_fields
        
        # Redact sensitive data
        log_data = redact_dict(log_data)
        
        return json.dumps(log_data)


class HumanReadableFormatter(logging.Formatter):
    """
    Human-readable log formatter for development.
    
    Outputs colorized, readable logs for local debugging.
    Secrets are still redacted for security.
    """
    
    # ANSI color codes
    COLORS = {
        "DEBUG": "\033[36m",      # Cyan
        "INFO": "\033[32m",       # Green
        "WARNING": "\033[33m",    # Yellow
        "ERROR": "\033[31m",      # Red
        "CRITICAL": "\033[35m",   # Magenta
        "RESET": "\033[0m",       # Reset
    }
    
    def __init__(self, use_color: bool = True):
        super().__init__()
        self.use_color = use_color and sys.stderr.isatty()
    
    def format(self, record: logging.LogRecord) -> str:
        """Format log record as human-readable text."""
        # Color for level
        if self.use_color:
            level_color = self.COLORS.get(record.levelname, "")
            reset = self.COLORS["RESET"]
            level_text = f"{level_color}{record.levelname:8}{reset}"
        else:
            level_text = f"{record.levelname:8}"
        
        # Timestamp
        timestamp = datetime.utcfromtimestamp(record.created).strftime("%H:%M:%S.%f")[:-3]
        
        # Location
        location = f"{record.module}:{record.funcName}:{record.lineno}"
        
        # Message
        message = record.getMessage()
        
        # Build log line
        log_line = f"{timestamp} {level_text} [{location:30}] {message}"
        
        # Add trace_id if present
        if hasattr(record, "trace_id"):
            log_line += f" [trace_id={record.trace_id}]"
        
        # Add extra fields
        extra_fields = {}
        for key, value in record.__dict__.items():
            if key not in (
                "name", "msg", "args", "created", "filename", "funcName",
                "levelname", "levelno", "lineno", "module", "msecs",
                "message", "pathname", "process", "processName", "relativeCreated",
                "thread", "threadName", "exc_info", "exc_text", "stack_info",
                "trace_id",
            ):
                extra_fields[key] = value
        
        if extra_fields:
            # Redact extra fields
            extra_fields = redact_dict(extra_fields)
            extras_str = " ".join(f"{k}={v}" for k, v in extra_fields.items())
            log_line += f" | {extras_str}"
        
        # Add exception info
        if record.exc_info:
            log_line += "\n" + self.formatException(record.exc_info)
        
        return log_line


class SecretRedactionFilter(logging.Filter):
    """
    Filter that redacts secrets from log records.
    
    This is a defense-in-depth measure in addition to redact_dict.
    Catches secrets in the message string itself.
    
    Security:
        - Redacts API keys, passwords, tokens, signatures
        - Redacts hex-encoded secrets (64+ chars)
        - Preserves first/last 4 chars for debugging (e.g., "sk_****xyz")
    """
    
    # Patterns that indicate secrets
    SECRET_PATTERNS = [
        "password", "passwd", "pwd", "secret", "api_key", "apikey",
        "token", "auth", "credential", "private_key", "signature",
        "bearer", "jwt", "session", "cookie",
    ]
    
    def filter(self, record: logging.LogRecord) -> bool:
        """Redact secrets from log record message."""
        # Redact message
        if record.msg:
            record.msg = self._redact_message(str(record.msg))
        
        # Redact args
        if record.args:
            if isinstance(record.args, dict):
                record.args = redact_dict(record.args)
            elif isinstance(record.args, (list, tuple)):
                record.args = tuple(
                    redact_dict(arg) if isinstance(arg, dict) else self._redact_value(arg)
                    for arg in record.args
                )
        
        return True
    
    def _redact_message(self, message: str) -> str:
        """Redact secrets from message string."""
        # Check for patterns in message
        lower_msg = message.lower()
        for pattern in self.SECRET_PATTERNS:
            if pattern in lower_msg:
                # Find the pattern and redact the value after it
                import re
                # Match pattern: "password=value" or "password: value"
                regex = re.compile(
                    rf"({pattern}[\s:=]+)([^\s,;}}]+)",
                    re.IGNORECASE
                )
                message = regex.sub(r"\1<REDACTED>", message)
        
        # Redact long hex strings (likely secrets)
        import re
        message = re.sub(
            r"\b[a-fA-F0-9]{64,}\b",
            lambda m: f"{m.group(0)[:4]}...{m.group(0)[-4:]}",
            message
        )
        
        return message
    
    def _redact_value(self, value: Any) -> Any:
        """Redact a single value if it looks like a secret."""
        if isinstance(value, str) and len(value) > 32:
            # Long strings might be secrets
            lower_val = value.lower()
            for pattern in self.SECRET_PATTERNS:
                if pattern in lower_val:
                    return "<REDACTED>"
            
            # Hex-encoded secrets
            if all(c in "0123456789abcdefABCDEF" for c in value):
                return f"{value[:4]}...{value[-4:]}"
        
        return value


def configure_logging(
    level: str = "INFO",
    log_file: Optional[str] = None,
    json_format: bool = False,
    max_bytes: int = 100 * 1024 * 1024,  # 100MB
    backup_count: int = 7,
    include_trace_id: bool = True,
) -> None:
    """
    Configure logging for AI service.
    
    Args:
        level: Log level (DEBUG, INFO, WARNING, ERROR, CRITICAL)
        log_file: Path to log file (None = stdout only)
        json_format: Use JSON format (True) or human-readable (False)
        max_bytes: Max size per log file (default 100MB)
        backup_count: Number of backup files to keep (default 7)
        include_trace_id: Include trace_id in logs (default True)
        
    Security:
        - All logs pass through SecretRedactionFilter
        - File permissions set to 0600 (owner read/write only)
        - Log directory created with 0700 permissions
        
    Example:
        # Development
        configure_logging(level="DEBUG", log_file="./logs/service.log")
        
        # Production
        configure_logging(level="INFO", json_format=True)
    """
    # Parse level
    numeric_level = getattr(logging, level.upper(), logging.INFO)
    
    # Get root logger
    root_logger = logging.getLogger()
    root_logger.setLevel(numeric_level)
    
    # Remove existing handlers
    root_logger.handlers.clear()
    
    # Choose formatter
    if json_format:
        formatter = JSONFormatter(include_trace_id=include_trace_id)
    else:
        formatter = HumanReadableFormatter()
    
    # Add secret redaction filter
    secret_filter = SecretRedactionFilter()
    
    # Console handler (always)
    console_handler = logging.StreamHandler(sys.stdout)
    console_handler.setLevel(numeric_level)
    console_handler.setFormatter(formatter)
    console_handler.addFilter(secret_filter)
    root_logger.addHandler(console_handler)
    
    # File handler (optional)
    if log_file:
        log_path = Path(log_file)
        
        # Create log directory with secure permissions
        log_path.parent.mkdir(parents=True, exist_ok=True)
        if sys.platform != "win32":
            os.chmod(log_path.parent, 0o700)
        
        # Rotating file handler
        file_handler = logging.handlers.RotatingFileHandler(
            filename=str(log_path),
            maxBytes=max_bytes,
            backupCount=backup_count,
            encoding="utf-8",
        )
        file_handler.setLevel(numeric_level)
        file_handler.setFormatter(formatter)
        file_handler.addFilter(secret_filter)
        root_logger.addHandler(file_handler)
        
        # Set file permissions (owner read/write only)
        if sys.platform != "win32" and log_path.exists():
            os.chmod(log_path, 0o600)
    
    # Log configuration complete
    root_logger.info(
        "Logging configured",
        extra={
            "level": level,
            "json_format": json_format,
            "log_file": str(log_file) if log_file else None,
        }
    )


def get_logger(name: str) -> logging.Logger:
    """
    Get a logger for a module.
    
    Args:
        name: Logger name (typically __name__)
        
    Returns:
        Configured logger instance
        
    Example:
        logger = get_logger(__name__)
        logger.info("Service started", extra={"port": 8080})
    """
    return logging.getLogger(name)
