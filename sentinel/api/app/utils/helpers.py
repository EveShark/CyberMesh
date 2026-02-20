"""Helper utilities."""

import uuid
from datetime import datetime, timezone


def generate_request_id() -> str:
    """Generate unique request ID."""
    return str(uuid.uuid4())


def get_timestamp() -> datetime:
    """Get current UTC timestamp."""
    return datetime.now(timezone.utc)


def format_duration(ms: int) -> str:
    """Format duration in human readable format."""
    if ms < 1000:
        return f"{ms}ms"
    elif ms < 60000:
        return f"{ms / 1000:.1f}s"
    else:
        return f"{ms / 60000:.1f}m"
