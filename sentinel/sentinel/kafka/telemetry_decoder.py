"""Decode telemetry-layer Kafka payloads into Sentinel CanonicalEvent."""

from __future__ import annotations

import hashlib
import json
import os
import re
import sys
import time
from pathlib import Path
from typing import Any, Dict

from sentinel.agents.event_builder import build_flow_event, build_scan_findings_event
from sentinel.contracts import CanonicalEvent
from sentinel.contracts.schemas import ScanFindingV1, ScanFindingsV1
from sentinel.telemetry.adapters import normalize_flow_record
from sentinel.utils.error_codes import (
    ERR_INVALID_FIELDS,
    ERR_INVALID_JSON,
    ERR_INVALID_SCHEMA,
    ERR_MISSING_TENANT,
    format_error,
)

from .config import KafkaWorkerConfig


def _hash_event_id(topic: str, payload: bytes) -> str:
    return f"kafka:{topic}:{hashlib.sha256(payload).hexdigest()}"


def _load_json(value: bytes) -> Dict[str, Any]:
    try:
        obj = json.loads(value.decode("utf-8"))
    except Exception as exc:  # pylint: disable=broad-except
        raise ValueError(format_error(ERR_INVALID_JSON, f"invalid json: {exc}")) from exc
    if not isinstance(obj, dict):
        raise ValueError(format_error(ERR_INVALID_SCHEMA, "telemetry payload must be object"))
    return obj


def _resolve_proto_path() -> None:
    env_path = os.getenv("TELEMETRY_PROTO_PATH")
    candidates: list[Path] = []
    if env_path:
        candidates.append(Path(env_path))
    repo_guess = Path(__file__).resolve().parents[3] / "telemetry-layer" / "proto" / "gen" / "python"
    candidates.append(repo_guess)
    for candidate in candidates:
        if candidate.exists():
            text = str(candidate)
            if text not in sys.path:
                sys.path.insert(0, text)
            return


def _decode_proto_flow_v1(value: bytes) -> Dict[str, Any]:
    _resolve_proto_path()
    try:
        from telemetry_flow_v1_pb2 import FlowV1  # type: ignore
    except Exception as exc:  # pylint: disable=broad-except
        raise ValueError(format_error(ERR_INVALID_SCHEMA, f"flow protobuf module unavailable: {exc}")) from exc

    msg = FlowV1()
    msg.ParseFromString(value)
    record: Dict[str, Any] = {
        "schema": msg.schema or "flow.v1",
        "ts": int(msg.ts or 0),
        "tenant_id": msg.tenant_id or "",
        "flow_id": msg.flow_id or "",
        "src_ip": msg.src_ip or "",
        "dst_ip": msg.dst_ip or "",
        "src_port": int(msg.src_port or 0),
        "dst_port": int(msg.dst_port or 0),
        "proto": int(msg.proto or 0),
        "bytes_fwd": int(msg.bytes_fwd or 0),
        "bytes_bwd": int(msg.bytes_bwd or 0),
        "pkts_fwd": int(msg.pkts_fwd or 0),
        "pkts_bwd": int(msg.pkts_bwd or 0),
        "duration_ms": int(msg.duration_ms or 0),
        "source_id": msg.source_id or "",
        "source_event_ts_ms": int(getattr(msg, "source_event_ts_ms", 0) or 0),
        "telemetry_ingest_ts_ms": int(getattr(msg, "telemetry_ingest_ts_ms", 0) or 0),
    }
    if msg.source_type:
        record["source_type"] = int(msg.source_type)
    return record


def _decode_proto_cic_v1(value: bytes) -> Dict[str, Any]:
    _resolve_proto_path()
    try:
        from telemetry_feature_v1_pb2 import CicFeaturesV1  # type: ignore
    except Exception as exc:  # pylint: disable=broad-except
        raise ValueError(format_error(ERR_INVALID_SCHEMA, f"feature protobuf module unavailable: {exc}")) from exc

    msg = CicFeaturesV1()
    msg.ParseFromString(value)
    output: Dict[str, Any] = {}
    for field, val in msg.ListFields():
        name = field.name
        if isinstance(val, bytes):
            output[name] = val.hex()
        else:
            output[name] = val
    output.setdefault("schema", "cic.v1")
    return output


def _decode_proto_deepflow_v1(value: bytes) -> Dict[str, Any]:
    _resolve_proto_path()
    try:
        from telemetry_deepflow_v1_pb2 import DeepFlowV1  # type: ignore
    except Exception as exc:  # pylint: disable=broad-except
        raise ValueError(format_error(ERR_INVALID_SCHEMA, f"deepflow protobuf module unavailable: {exc}")) from exc

    msg = DeepFlowV1()
    msg.ParseFromString(value)
    return {
        "schema": msg.schema or "deepflow.v1",
        "ts": int(msg.ts or 0),
        "tenant_id": msg.tenant_id or "",
        "sensor_id": msg.sensor_id or "",
        "source_type": int(msg.source_type or 0),
        "src_ip": msg.src_ip or "",
        "dst_ip": msg.dst_ip or "",
        "src_port": int(msg.src_port or 0),
        "dst_port": int(msg.dst_port or 0),
        "proto": int(msg.proto or 0),
        "alert_type": msg.alert_type or "",
        "alert_category": msg.alert_category or "",
        "severity": msg.severity or "",
        "signature": msg.signature or "",
        "signature_id": msg.signature_id or "",
        "metadata": dict(msg.metadata or {}),
        "flow_id": msg.flow_id or "",
    }


def _decode_payload(value: bytes, *, encoding: str, schema_type: str) -> Dict[str, Any]:
    norm_encoding = (encoding or "json").strip().lower()
    norm_schema = (schema_type or "").strip().lower()

    if norm_encoding in ("protobuf", "proto", "pb"):
        if norm_schema == "flow_v1":
            return _decode_proto_flow_v1(value)
        if norm_schema == "cic_v1":
            return _decode_proto_cic_v1(value)
        if norm_schema == "deepflow_v1":
            return _decode_proto_deepflow_v1(value)
        raise ValueError(format_error(ERR_INVALID_SCHEMA, f"unsupported protobuf schema: {schema_type}"))

    data = _load_json(value)
    if "payload" in data and isinstance(data["payload"], dict):
        # Some producers wrap payloads in an envelope while still publishing to telemetry topics.
        return data["payload"]
    return data


def _to_flow_event(topic: str, payload: Dict[str, Any], raw_bytes: bytes) -> CanonicalEvent:
    tenant_id = str(payload.get("tenant_id") or "").strip()
    if not tenant_id:
        raise ValueError(format_error(ERR_MISSING_TENANT, "missing tenant_id"))
    features, errors = normalize_flow_record(payload)
    if errors or not features:
        detail = ", ".join(errors) if errors else "unable to normalize flow record"
        raise ValueError(format_error(ERR_INVALID_FIELDS, detail))

    source = f"kafka:{topic}"
    event_id = str(payload.get("flow_id") or _hash_event_id(topic, raw_bytes))
    event_ts = float(payload.get("ts") or time.time())
    labels = _ensure_trace_labels(_derive_labels_for_flow(payload), event_id=event_id, timestamp_s=event_ts, payload=payload)
    raw_context = {
        "_source_topic": topic,
        "_source_schema": payload.get("schema", ""),
        "_decoded_duration_ms": payload.get("duration_ms"),
        "_decoded_timing_known": payload.get("timing_known"),
    }
    return build_flow_event(
        features=features,
        raw_context=raw_context,
        tenant_id=tenant_id,
        source=source,
        event_id=event_id,
        timestamp=event_ts,
        labels=labels,
    )


def _parse_source_port(source_id: str) -> str:
    text = (source_id or "").strip()
    if not text:
        return ""
    # net.JoinHostPort format for IPv6: [ip]:port
    match = re.search(r":(\d+)$", text)
    if not match:
        return ""
    return match.group(1)


def _parse_port_map(raw: str) -> Dict[str, str]:
    mapping: Dict[str, str] = {}
    for item in (raw or "").split(","):
        token = item.strip()
        if not token or ":" not in token:
            continue
        port, value = token.split(":", 1)
        port = port.strip()
        value = value.strip()
        if port and value:
            mapping[port] = value
    return mapping


def _derive_labels_for_flow(payload: Dict[str, Any]) -> Dict[str, str]:
    labels: Dict[str, str] = {}

    # Direct payload labels (if present) take priority.
    for key in ("profile_mode", "scenario"):
        val = payload.get(key)
        if val is not None:
            sval = str(val).strip()
            if sval:
                labels[key] = sval

    if labels.get("profile_mode") and labels.get("scenario"):
        return labels

    source_id = str(payload.get("source_id") or "")
    source_port = _parse_source_port(source_id)
    if not source_port:
        return labels

    profile_map = _parse_port_map(os.getenv("SENTINEL_SOURCE_PORT_PROFILE_MAP", ""))
    scenario_map = _parse_port_map(os.getenv("SENTINEL_SOURCE_PORT_SCENARIO_MAP", ""))

    if "profile_mode" not in labels and source_port in profile_map:
        labels["profile_mode"] = profile_map[source_port]
    if "scenario" not in labels and source_port in scenario_map:
        labels["scenario"] = scenario_map[source_port]
    return labels


def _normalize_event_ts_ms(value: Any) -> int:
    if value is None:
        return 0
    try:
        parsed = float(value)
    except (TypeError, ValueError):
        return 0
    if parsed <= 0:
        return 0
    if parsed >= 946684800000:
        return int(parsed)
    return int(parsed * 1000.0)


def _ensure_trace_labels(labels: Dict[str, str], *, event_id: str, timestamp_s: float, payload: Dict[str, Any] | None = None) -> Dict[str, str]:
    out = dict(labels or {})
    out.setdefault("trace_id", event_id)
    out.setdefault("source_event_id", event_id)
    payload = payload or {}
    source_event_ts_ms = _normalize_event_ts_ms(payload.get("source_event_ts_ms"))
    telemetry_ingest_ts_ms = _normalize_event_ts_ms(payload.get("telemetry_ingest_ts_ms"))
    out.setdefault("source_event_ts_ms", str(source_event_ts_ms or int(float(timestamp_s) * 1000.0)))
    if telemetry_ingest_ts_ms > 0:
        out.setdefault("telemetry_ingest_ts_ms", str(telemetry_ingest_ts_ms))
    return out


def _to_deepflow_event(topic: str, payload: Dict[str, Any], raw_bytes: bytes) -> CanonicalEvent:
    tenant_id = str(payload.get("tenant_id") or "").strip()
    if not tenant_id:
        raise ValueError(format_error(ERR_MISSING_TENANT, "missing tenant_id"))
    event_id = str(payload.get("flow_id") or _hash_event_id(topic, raw_bytes))
    event_ts = float(payload.get("ts") or time.time())
    severity = str(payload.get("severity") or "medium")
    signature = str(payload.get("signature") or payload.get("alert_type") or "deepflow_alert")
    signature_id = str(payload.get("signature_id") or "deepflow")
    description = str(payload.get("alert_category") or payload.get("alert_type") or "deepflow finding")
    finding = ScanFindingV1(
        rule_id=signature_id,
        rule_name=signature,
        severity=severity,
        description=description,
        location=str(payload.get("flow_id") or ""),
        evidence=json.dumps(payload.get("metadata") or {}, sort_keys=True)[:1024],
        tags=["deepflow", str(payload.get("source_type") or "").lower()],
    )
    features = ScanFindingsV1(
        tool="deepflow",
        findings=[finding],
        summary=description,
    )
    raw_context = {
        "_source_topic": topic,
        "_source_schema": payload.get("schema", ""),
        "flow_id": payload.get("flow_id", ""),
        "sensor_id": payload.get("sensor_id", ""),
    }
    return build_scan_findings_event(
        features=features,
        raw_context=raw_context,
        tenant_id=tenant_id,
        source=f"kafka:{topic}",
        event_id=event_id,
        timestamp=event_ts,
        labels=_ensure_trace_labels({}, event_id=event_id, timestamp_s=event_ts, payload=payload),
    )


def decode_telemetry_topic_message(topic: str, value: bytes, cfg: KafkaWorkerConfig) -> CanonicalEvent:
    """Decode a telemetry topic payload into a CanonicalEvent."""

    schema_type = cfg.topic_schema_map.get(topic, "canonical_event")
    encoding = cfg.topic_encoding_map.get(topic, "json")
    payload = _decode_payload(value, encoding=encoding, schema_type=schema_type)
    if not isinstance(payload, dict):
        raise ValueError(format_error(ERR_INVALID_SCHEMA, "decoded payload must be object"))

    if schema_type in ("flow_v1", "cic_v1"):
        if cfg.require_nonzero_duration_for_counted_flows:
            fwd_pkts = int(payload.get("pkts_fwd") or payload.get("tot_fwd_pkts") or 0)
            bwd_pkts = int(payload.get("pkts_bwd") or payload.get("tot_bwd_pkts") or 0)
            fwd_bytes = int(payload.get("bytes_fwd") or payload.get("totlen_fwd_pkts") or 0)
            bwd_bytes = int(payload.get("bytes_bwd") or payload.get("totlen_bwd_pkts") or 0)
            duration_ms = float(payload.get("duration_ms") or 0.0)
            has_counters = (fwd_pkts + bwd_pkts + fwd_bytes + bwd_bytes) > 0
            if has_counters and duration_ms <= 0:
                raise ValueError(
                    format_error(
                        ERR_INVALID_FIELDS,
                        "duration_ms must be > 0 when counters are present",
                    )
                )
        return _to_flow_event(topic, payload, value)
    if schema_type == "deepflow_v1":
        return _to_deepflow_event(topic, payload, value)
    raise ValueError(format_error(ERR_INVALID_SCHEMA, f"unsupported topic schema: {schema_type}"))
