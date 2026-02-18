"""Timeout helpers for agent execution."""

from __future__ import annotations

import os
from typing import Optional


def resolve_timeout_seconds(
    timeout_seconds: Optional[float] = None,
    env_var: str = "SENTINEL_AGENT_TIMEOUT_SECONDS",
    default: float = 30.0,
) -> Optional[float]:
    """Resolve timeout in seconds from explicit value or environment."""
    if timeout_seconds is not None:
        try:
            value = float(timeout_seconds)
        except (TypeError, ValueError):
            return default
        return value if value > 0 else default

    raw = os.getenv(env_var)
    if not raw:
        # Backward compatibility: older configs used AGENT_TIMEOUT_SECONDS.
        raw = os.getenv("AGENT_TIMEOUT_SECONDS")
    if raw:
        try:
            value = float(raw)
        except ValueError:
            return default
        return value if value > 0 else default

    return default
