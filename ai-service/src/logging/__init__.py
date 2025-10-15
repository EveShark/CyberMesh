"""
Logging infrastructure for AI service.

Provides structured, security-first logging with:
- JSON formatting for production (machine-readable)
- Human-readable formatting for development
- Automatic secret redaction
- Log rotation (size + time based)
- Context enrichment (timestamp, module, function, trace_id)
"""
from .config import configure_logging, get_logger

__all__ = [
    "configure_logging",
    "get_logger",
]
