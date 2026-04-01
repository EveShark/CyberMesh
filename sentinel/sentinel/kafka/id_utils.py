"""Shared identifier helpers for Sentinel Kafka workers."""

from __future__ import annotations

import hashlib
import re
import secrets
from typing import Any


def is_valid_trace_id(value: Any) -> bool:
    text = str(value or "").strip()
    if len(text) != 32 or text.lower() != text:
        return False
    if not re.fullmatch(r"[0-9a-f]{32}", text):
        return False
    return text != "0" * 32


def generate_trace_id() -> str:
    return secrets.token_hex(16)


def derive_trace_id(*values: Any) -> str:
    for value in values:
        if is_valid_trace_id(value):
            return str(value).strip()
    seed = ""
    for value in values:
        text = str(value or "").strip()
        if text:
            seed = text
            break
    if seed:
        return hashlib.sha256(seed.encode("utf-8")).hexdigest()[:32]
    return generate_trace_id()
