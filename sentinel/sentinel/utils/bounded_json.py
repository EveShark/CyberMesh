"""Bounded JSON-like copies for safety.

Goal: prevent untrusted/raw payloads from causing unbounded memory growth or
log/metadata bloat. This is applied to raw_context in ingest paths.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Dict, Tuple


@dataclass
class BoundsSummary:
    depth_truncated: int = 0
    items_dropped: int = 0
    strings_truncated: int = 0

    def to_dict(self) -> Dict[str, int]:
        return {
            "depth_truncated": int(self.depth_truncated),
            "items_dropped": int(self.items_dropped),
            "strings_truncated": int(self.strings_truncated),
        }


def bound_json(
    value: Any,
    *,
    max_depth: int = 6,
    max_items: int = 200,
    max_string_bytes: int = 4096,
) -> Tuple[Any, BoundsSummary]:
    """Return a bounded copy of a JSON-like structure plus a summary."""
    summary = BoundsSummary()

    def _truncate_str(s: str) -> str:
        nonlocal summary
        if not max_string_bytes:
            return s
        b = s.encode("utf-8", errors="ignore")
        if len(b) <= max_string_bytes:
            return s
        summary.strings_truncated += 1
        return b[:max_string_bytes].decode("utf-8", errors="ignore") + "...[truncated]"

    def _walk(v: Any, depth: int) -> Any:
        nonlocal summary
        if depth > max_depth:
            summary.depth_truncated += 1
            return "[... depth truncated ...]"

        if isinstance(v, str):
            return _truncate_str(v)

        if isinstance(v, dict):
            out: Dict[str, Any] = {}
            for i, (k, vv) in enumerate(v.items()):
                if max_items and i >= max_items:
                    summary.items_dropped += max(0, len(v) - max_items)
                    break
                out[str(k)] = _walk(vv, depth + 1)
            return out

        if isinstance(v, list):
            if max_items and len(v) > max_items:
                summary.items_dropped += len(v) - max_items
            return [_walk(item, depth + 1) for item in v[: (max_items or len(v))]]

        return v

    return _walk(value, 0), summary

