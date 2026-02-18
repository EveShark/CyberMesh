"""Logging configuration with structured output and secret redaction."""

import logging
import logging.handlers
import json
import sys
import os
from pathlib import Path
from datetime import datetime
from typing import Optional, Any

from ..utils.redaction import redact_dict


class JSONFormatter(logging.Formatter):
    """JSON log formatter for production environments."""
    
    def format(self, record: logging.LogRecord) -> str:
        log_data = {
            "timestamp": datetime.utcfromtimestamp(record.created).isoformat() + "Z",
            "level": record.levelname,
            "logger": record.name,
            "module": record.module,
            "function": record.funcName,
            "line": record.lineno,
            "message": record.getMessage(),
        }
        
        if record.exc_info:
            log_data["exception"] = {
                "type": record.exc_info[0].__name__ if record.exc_info[0] else None,
                "message": str(record.exc_info[1]) if record.exc_info[1] else None,
                "traceback": self.formatException(record.exc_info),
            }
        
        extra_fields = {}
        for key, value in record.__dict__.items():
            if key not in (
                "name", "msg", "args", "created", "filename", "funcName",
                "levelname", "levelno", "lineno", "module", "msecs",
                "message", "pathname", "process", "processName", "relativeCreated",
                "thread", "threadName", "exc_info", "exc_text", "stack_info",
            ):
                extra_fields[key] = value
        
        if extra_fields:
            log_data["extra"] = extra_fields
        
        log_data = redact_dict(log_data)
        return json.dumps(log_data)


class HumanReadableFormatter(logging.Formatter):
    """Human-readable log formatter for development."""
    
    COLORS = {
        "DEBUG": "\033[36m",
        "INFO": "\033[32m",
        "WARNING": "\033[33m",
        "ERROR": "\033[31m",
        "CRITICAL": "\033[35m",
        "RESET": "\033[0m",
    }
    
    def __init__(self, use_color: bool = True):
        super().__init__()
        self.use_color = use_color and sys.stderr.isatty()
    
    def format(self, record: logging.LogRecord) -> str:
        if self.use_color:
            level_color = self.COLORS.get(record.levelname, "")
            reset = self.COLORS["RESET"]
            level_text = f"{level_color}{record.levelname:8}{reset}"
        else:
            level_text = f"{record.levelname:8}"
        
        timestamp = datetime.utcfromtimestamp(record.created).strftime("%H:%M:%S.%f")[:-3]
        location = f"{record.module}:{record.funcName}:{record.lineno}"
        message = record.getMessage()
        
        log_line = f"{timestamp} {level_text} [{location:30}] {message}"
        
        if record.exc_info:
            log_line += "\n" + self.formatException(record.exc_info)
        
        return log_line


def configure_logging(
    level: str = "INFO",
    log_file: Optional[str] = None,
    json_format: bool = False,
) -> None:
    """
    Configure logging for Sentinel.
    
    Args:
        level: Log level (DEBUG, INFO, WARNING, ERROR, CRITICAL)
        log_file: Path to log file (None = stdout only)
        json_format: Use JSON format (True) or human-readable (False)
    """
    numeric_level = getattr(logging, level.upper(), logging.INFO)
    
    root_logger = logging.getLogger()
    root_logger.setLevel(numeric_level)
    root_logger.handlers.clear()
    
    if json_format:
        formatter = JSONFormatter()
    else:
        formatter = HumanReadableFormatter()
    
    console_handler = logging.StreamHandler(sys.stdout)
    console_handler.setLevel(numeric_level)
    console_handler.setFormatter(formatter)
    root_logger.addHandler(console_handler)
    
    if log_file:
        log_path = Path(log_file)
        log_path.parent.mkdir(parents=True, exist_ok=True)
        
        file_handler = logging.handlers.RotatingFileHandler(
            filename=str(log_path),
            maxBytes=100 * 1024 * 1024,
            backupCount=7,
            encoding="utf-8",
        )
        file_handler.setLevel(numeric_level)
        file_handler.setFormatter(formatter)
        root_logger.addHandler(file_handler)


def get_logger(name: str) -> logging.Logger:
    """Get a logger for a module."""
    return logging.getLogger(name)
