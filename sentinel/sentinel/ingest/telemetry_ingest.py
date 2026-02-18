"""Telemetry ingest helpers (standalone).

This module sits between raw telemetry payloads (JSON/NDJSON/CSV/IPFIX-JSON)
and the analysis layer. It is intentionally strict and deterministic:
- Stable error codes for common ingest failures.
- Optional per-batch dedupe using a deterministic event id.
- Bounded sanitization of raw_context for safety (PII/secrets).

This is safe to use from harness scripts and the standalone CLI.
"""

from __future__ import annotations

import hashlib
import json
import os
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, Iterator, List, Optional, Tuple

from ..agents.event_builder import build_flow_event
from ..contracts import CanonicalEvent
from ..telemetry.adapters import normalize_flow_record
from ..utils.error_codes import (
    ERR_DUPLICATE_EVENT,
    ERR_INVALID_JSON,
    ERR_INVALID_FIELDS,
    ERR_MISSING_TENANT,
    ERR_OVERSIZE,
    format_error,
)
from ..utils.sanitizer import InputSanitizer


@dataclass
class TelemetryIngestLimits:
    max_payload_bytes: int = 10 * 1024 * 1024
    max_records: int = 50_000
    max_future_skew_s: int = 300
    redact_raw_context: bool = True


def _env_int(name: str, default: int) -> int:
    raw = os.getenv(name)
    if raw is None:
        return default
    try:
        return int(raw.strip())
    except Exception:
        return default


def _extract_tenant_id(record: Dict[str, Any], default: str) -> Optional[str]:
    for key in ("tenant_id", "tenant", "tenantId"):
        value = record.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return default or None


def _extract_timestamp(record: Dict[str, Any]) -> Optional[float]:
    for key in ("ts", "timestamp", "time"):
        value = record.get(key)
        if value is None or value == "":
            continue
        try:
            return float(value)
        except Exception:
            continue
    return None


def _stable_flow_event_id(features: Dict[str, Any], tenant_id: str, record: Optional[Dict[str, Any]] = None) -> str:
    flow_id = features.get("flow_id")
    if isinstance(flow_id, str) and flow_id.strip():
        base = flow_id.strip()
        # Zeek datasets can reuse the same uid across multiple derived records (http/rdp/etc).
        # Add a stable digest of record attributes to avoid over-deduping distinct events.
        if isinstance(record, dict) and record.get("source_type") == "zeek":
            extras = {
                "source_id": record.get("source_id"),
                "alert_type": record.get("alert_type"),
                "method": record.get("method"),
                "host": record.get("host"),
                "uri": record.get("uri"),
                "status_code": record.get("status_code"),
                "request_body_len": record.get("request_body_len"),
                "response_body_len": record.get("response_body_len"),
            }
            digest = hashlib.sha256(
                json.dumps(extras, sort_keys=True, default=str).encode("utf-8")
            ).hexdigest()[:12]
            return f"flow:{tenant_id}:{base}:{digest}"
        return f"flow:{tenant_id}:{base}"

    core = {
        "tenant_id": tenant_id,
        "src_ip": features.get("src_ip"),
        "dst_ip": features.get("dst_ip"),
        "src_port": features.get("src_port"),
        "dst_port": features.get("dst_port"),
        "protocol": features.get("protocol"),
        "tot_fwd_pkts": features.get("tot_fwd_pkts"),
        "tot_bwd_pkts": features.get("tot_bwd_pkts"),
        "totlen_fwd_pkts": features.get("totlen_fwd_pkts"),
        "totlen_bwd_pkts": features.get("totlen_bwd_pkts"),
        "flow_duration": features.get("flow_duration"),
    }
    digest = hashlib.sha256(json.dumps(core, sort_keys=True, default=str).encode("utf-8")).hexdigest()[:24]
    return f"flow:{tenant_id}:sha256:{digest}"


def _sanitize_raw_context(raw_context: Dict[str, Any]) -> Dict[str, Any]:
    from ..utils.bounded_json import bound_json

    max_depth = int(os.getenv("SENTINEL_RAW_CONTEXT_MAX_DEPTH", "6"))
    max_items = int(os.getenv("SENTINEL_RAW_CONTEXT_MAX_ITEMS", "200"))
    max_string_bytes = int(os.getenv("SENTINEL_RAW_CONTEXT_MAX_STRING_BYTES", "4096"))

    bounded, summary = bound_json(
        raw_context,
        max_depth=max(0, max_depth),
        max_items=max(0, max_items),
        max_string_bytes=max(0, max_string_bytes),
    )

    sanitizer = InputSanitizer()
    sanitized = sanitizer.sanitize_dict(bounded if isinstance(bounded, dict) else {"_raw": bounded})
    if isinstance(sanitized, dict):
        s = summary.to_dict()
        if any(v > 0 for v in s.values()):
            sanitized["_raw_context_bounds"] = s
    return sanitized


def load_telemetry_jsonl(
    path: str | Path,
    limits: Optional[TelemetryIngestLimits] = None,
) -> Tuple[Iterator[Dict[str, Any]], List[str]]:
    limits = limits or TelemetryIngestLimits()
    path = Path(path)
    if not path.exists():
        return iter(()), [format_error(ERR_INVALID_FIELDS, f"input file not found: {path}")]

    if limits.max_payload_bytes and path.stat().st_size > limits.max_payload_bytes:
        return iter(()), [format_error(ERR_OVERSIZE, f"payload exceeds max_payload_bytes ({limits.max_payload_bytes})")]

    def _iter() -> Iterator[Dict[str, Any]]:
        with path.open("rb") as f:
            for raw in f:
                line = raw.strip()
                if not line:
                    continue
                try:
                    obj = json.loads(line.decode("utf-8"))
                except Exception as exc:
                    yield {"_parse_error": format_error(ERR_INVALID_JSON, f"invalid json ({exc})")}
                    continue
                if isinstance(obj, dict):
                    yield obj
                else:
                    yield {"_parse_error": format_error(ERR_INVALID_JSON, "expected object")}

    return _iter(), []


def telemetry_records_to_flow_events(
    records: Iterator[Dict[str, Any]],
    tenant_default: str,
    source: str = "telemetry_ingest",
    limits: Optional[TelemetryIngestLimits] = None,
) -> Tuple[List[CanonicalEvent], List[str]]:
    limits = limits or TelemetryIngestLimits(
        max_future_skew_s=_env_int("SENTINEL_MAX_FUTURE_SKEW_S", 300),
    )

    events: List[CanonicalEvent] = []
    errors: List[str] = []
    seen_ids: set[str] = set()

    now = time.time()
    max_records = limits.max_records or 0

    for idx, record in enumerate(records):
        if max_records and idx >= max_records:
            break

        if "_parse_error" in record:
            errors.append(f"record {idx + 1}: {record.get('_parse_error')}")
            continue

        tenant_id = _extract_tenant_id(record, tenant_default)
        if not tenant_id:
            errors.append(f"record {idx + 1}: {format_error(ERR_MISSING_TENANT, 'missing tenant_id')}")
            continue

        ts = _extract_timestamp(record)
        if ts is not None and (ts - now) > float(limits.max_future_skew_s):
            errors.append(
                f"record {idx + 1}: {format_error(ERR_INVALID_FIELDS, f'future timestamp beyond skew ({limits.max_future_skew_s}s)')}"
            )
            ts = now

        features_obj, norm_errors = normalize_flow_record(record)
        if norm_errors or not features_obj:
            errors.append(
                f"record {idx + 1}: {format_error(ERR_INVALID_FIELDS, 'normalize_error: ' + '; '.join(norm_errors))}"
            )
            continue

        features = features_obj.to_dict()
        event_id = _stable_flow_event_id(features, tenant_id=tenant_id, record=record)
        if event_id in seen_ids:
            errors.append(f"record {idx + 1}: {format_error(ERR_DUPLICATE_EVENT, f'duplicate event_id {event_id}')}")
            continue
        seen_ids.add(event_id)

        raw_context = dict(record)
        raw_context["_adapter"] = "telemetry_ingest"
        if limits.redact_raw_context:
            raw_context = _sanitize_raw_context(raw_context)

        events.append(
            build_flow_event(
                features=features_obj,
                raw_context=raw_context,
                tenant_id=tenant_id,
                source=source,
                event_id=event_id,
                timestamp=ts,
            )
        )

    return events, errors
