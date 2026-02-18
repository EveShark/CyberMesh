"""Standalone OSS adapter framework for local ingest."""

from __future__ import annotations

import json
import hashlib
import os
import time
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple, Union
from concurrent.futures import ThreadPoolExecutor, TimeoutError

from ..contracts import CanonicalEvent, Modality
from ..logging import get_logger
from ..utils.attestation import sign_event
from ..utils.error_codes import (
    ERR_ATTESTATION_REQUIRED,
    ERR_DUPLICATE_EVENT,
    ERR_INVALID_FIELDS,
    ERR_INVALID_JSON,
    ERR_MISSING_TENANT,
    ERR_OVERSIZE,
    ERR_PARSE_TIMEOUT,
    ERR_UNSUPPORTED_FORMAT,
    format_error,
)
from ..utils.sanitizer import InputSanitizer

logger = get_logger(__name__)


@dataclass
class AdapterLimits:
    """Limits for adapter parsing and input size."""
    max_payload_bytes: int = 1_048_576
    max_records: int = 1000
    parse_timeout_ms: int = 2000
    max_findings: int = 200
    max_string_bytes: int = 4096


@dataclass
class AdapterSpec:
    """Adapter specification loaded from config."""
    version: str
    adapter: str
    modality: str
    features_version: str
    source: str
    required_fields: List[str]
    field_map: Dict[str, Union[str, List[str]]]
    transforms: Dict[str, Dict[str, Any]] = field(default_factory=dict)
    tenant_id: Optional[str] = None
    id_field: Optional[str] = None
    timestamp_field: Optional[str] = None
    records_path: Optional[str] = None
    limits: AdapterLimits = field(default_factory=AdapterLimits)

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "AdapterSpec":
        limits_data = data.get("limits", {}) or {}
        limits = AdapterLimits(
            max_payload_bytes=int(limits_data.get("max_payload_bytes", 1_048_576)),
            max_records=int(limits_data.get("max_records", 1000)),
            parse_timeout_ms=int(limits_data.get("parse_timeout_ms", 2000)),
            max_findings=int(limits_data.get("max_findings", 200)),
            max_string_bytes=int(limits_data.get("max_string_bytes", 4096)),
        )
        return cls(
            version=str(data.get("version", "v1")),
            adapter=str(data.get("adapter", "unknown")),
            modality=str(data.get("modality", "")),
            features_version=str(data.get("features_version", "")),
            source=str(data.get("source", data.get("adapter", "unknown"))),
            required_fields=list(data.get("required_fields", [])),
            field_map=dict(data.get("field_map", {})),
            transforms=dict(data.get("transforms", {})),
            tenant_id=data.get("tenant_id"),
            id_field=data.get("id_field"),
            timestamp_field=data.get("timestamp_field"),
            records_path=data.get("records_path"),
            limits=limits,
        )


class OSSAdapter:
    """Convert OSS tool outputs to CanonicalEvent using AdapterSpec."""

    def __init__(self, spec: AdapterSpec):
        self.spec = spec
        self.modality = self._parse_modality(spec.modality)

    def records_to_events(
        self,
        records: List[Dict[str, Any]],
        tenant_id: Optional[str] = None,
    ) -> Tuple[List[CanonicalEvent], List[str]]:
        events: List[CanonicalEvent] = []
        errors: List[str] = []
        seen_ids: set[str] = set()
        for idx, record in enumerate(records):
            event, err = self._record_to_event(record, tenant_id=tenant_id)
            if err:
                errors.append(f"record {idx + 1}: {err}")
                continue
            if event:
                if event.id in seen_ids:
                    errors.append(
                        f"record {idx + 1}: {format_error(ERR_DUPLICATE_EVENT, f'duplicate event_id {event.id}')}"
                    )
                    continue
                seen_ids.add(event.id)
                self._attach_attestation(event)
                events.append(event)
        return events, errors

    def _record_to_event(
        self,
        record: Dict[str, Any],
        tenant_id: Optional[str] = None,
    ) -> Tuple[Optional[CanonicalEvent], Optional[str]]:
        features: Dict[str, Any] = {}
        for target_field, source_path in self.spec.field_map.items():
            value = _get_value(record, source_path)
            transform = self.spec.transforms.get(target_field)
            value = _apply_transform(value, transform, features)
            features[target_field] = value

        missing = [
            field for field in self.spec.required_fields
            if _is_missing(features.get(field))
        ]
        if missing:
            return None, format_error(
                ERR_INVALID_FIELDS,
                f"missing required fields: {', '.join(missing)}",
            )

        sanitization = _sanitize_features(features, self.spec.limits)

        # Security-first tenant isolation:
        # - In strict mode, require tenant_id to come from the record or caller.
        # - In non-strict mode (default), allow a spec-level fallback for single-tenant setups.
        tenant_strict = _env_true("SENTINEL_TENANT_STRICT", default=False)
        if tenant_strict:
            resolved_tenant = tenant_id or record.get("tenant_id")
        else:
            resolved_tenant = tenant_id or self.spec.tenant_id or record.get("tenant_id")
        if not resolved_tenant:
            return None, format_error(ERR_MISSING_TENANT, "missing tenant_id")

        event_id = None
        if self.spec.id_field:
            event_id = _get_value(record, self.spec.id_field)
        if not event_id:
            event_id = _hash_event(features, self.modality.value, resolved_tenant)

        timestamp = _parse_timestamp(record, self.spec.timestamp_field)
        if timestamp is None:
            timestamp = time.time()

        attestation_key = os.getenv("SENTINEL_ADAPTER_HMAC_KEY")
        attestation_required = os.getenv("SENTINEL_ADAPTER_HMAC_REQUIRED", "").strip().lower() in (
            "1", "true", "yes", "on"
        )
        if attestation_required and not attestation_key:
            return None, format_error(
                ERR_ATTESTATION_REQUIRED,
                "attestation required but SENTINEL_ADAPTER_HMAC_KEY is not set",
            )

        raw_context = dict(record)
        raw_context["_adapter"] = "oss_adapter"
        if sanitization:
            raw_context["_sanitization"] = sanitization
        if _env_true("SENTINEL_ADAPTER_REDACT_RAW", default=True):
            raw_context = _sanitize_raw_context(raw_context)

        return CanonicalEvent(
            id=str(event_id),
            timestamp=timestamp,
            source=self.spec.source,
            tenant_id=resolved_tenant,
            modality=self.modality,
            features_version=self.spec.features_version,
            features=features,
            raw_context=raw_context,
            labels={},
        ), None

    def _attach_attestation(self, event: CanonicalEvent) -> None:
        attestation_key = os.getenv("SENTINEL_ADAPTER_HMAC_KEY")
        if not attestation_key:
            return
        key_id = os.getenv("SENTINEL_ADAPTER_HMAC_KID")
        attestation = sign_event(event, attestation_key, key_id=key_id)
        if isinstance(event.raw_context, dict):
            event.raw_context["_attestation"] = attestation

    @staticmethod
    def _parse_modality(value: str) -> Modality:
        try:
            return Modality(value)
        except ValueError:
            raise ValueError(f"Unsupported modality: {value}")


def load_adapter_spec(path: str | Path) -> AdapterSpec:
    """Load adapter spec from JSON or YAML (if available)."""
    path = Path(path)
    if not path.exists():
        raise FileNotFoundError(f"Adapter spec not found: {path}")

    if path.suffix.lower() in (".yaml", ".yml"):
        try:
            import yaml  # type: ignore
        except Exception as exc:  # pylint: disable=broad-except
            raise RuntimeError("YAML adapter specs require PyYAML") from exc
        data = yaml.safe_load(path.read_text(encoding="utf-8"))
        return AdapterSpec.from_dict(data or {})

    data = json.loads(path.read_text(encoding="utf-8"))
    return AdapterSpec.from_dict(data or {})


def load_records(
    path: str | Path,
    fmt: str,
    limits: AdapterLimits,
    records_path: Optional[str] = None,
) -> Tuple[List[Dict[str, Any]], List[str]]:
    """Load records from JSON or NDJSON with limits."""
    path = Path(path)
    if not path.exists():
        return [], [f"input file not found: {path}"]

    if limits.max_payload_bytes and path.stat().st_size > limits.max_payload_bytes:
        return [], [format_error(
            ERR_OVERSIZE,
            f"payload exceeds max_payload_bytes ({limits.max_payload_bytes})",
        )]

    timeout = max(limits.parse_timeout_ms / 1000.0, 0.0)
    if timeout:
        with ThreadPoolExecutor(max_workers=1) as executor:
            future = executor.submit(_load_records_inner, path, fmt, limits, records_path)
            try:
                return future.result(timeout=timeout)
            except TimeoutError:
                future.cancel()
                return [], [format_error(
                    ERR_PARSE_TIMEOUT,
                    f"parse timeout after {limits.parse_timeout_ms}ms",
                )]
    return _load_records_inner(path, fmt, limits, records_path)


def _load_records_inner(
    path: Path,
    fmt: str,
    limits: AdapterLimits,
    records_path: Optional[str],
) -> Tuple[List[Dict[str, Any]], List[str]]:
    fmt = fmt.lower()
    if fmt not in ("json", "ndjson", "text"):
        return [], [format_error(ERR_UNSUPPORTED_FORMAT, f"unsupported format: {fmt}")]

    if fmt == "text":
        # Plaintext ingest is intentionally minimal and deterministic: treat the file as a set of
        # newline-delimited lines and leave semantic interpretation to adapter transforms.
        text = path.read_text(encoding="utf-8", errors="ignore")
        lines = [ln for ln in (s.strip() for s in text.splitlines()) if ln]
        # Prevent pathological memory usage from very large text artifacts.
        if limits.max_records and len(lines) > limits.max_records:
            lines = lines[: limits.max_records]
        return [{"lines": lines}], []

    if fmt == "json":
        try:
            payload = json.loads(path.read_text(encoding="utf-8"))
        except json.JSONDecodeError as exc:
            text = path.read_text(encoding="utf-8")
            decoder = json.JSONDecoder()
            idx = 0
            payloads: List[Any] = []
            length = len(text)
            try:
                while idx < length:
                    while idx < length and text[idx].isspace():
                        idx += 1
                    if idx >= length:
                        break
                    obj, end = decoder.raw_decode(text, idx)
                    payloads.append(obj)
                    idx = end
            except json.JSONDecodeError:
                return [], [format_error(ERR_INVALID_JSON, f"invalid json: {exc}")]
            if not payloads:
                return [], [format_error(ERR_INVALID_JSON, f"invalid json: {exc}")]
            records: List[Any] = []
            for item in payloads:
                extracted = _extract_records(item, records_path)
                if extracted is None:
                    return [], [format_error(
                        ERR_INVALID_JSON,
                        "json payload must be object or list",
                    )]
                records.extend(extracted)
            return _limit_records(records, limits), []
        records = _extract_records(payload, records_path)
        if records is None:
            return [], [format_error(
                ERR_INVALID_JSON,
                "json payload must be object or list",
            )]
        return _limit_records(records, limits), []

    records: List[Dict[str, Any]] = []
    errors: List[str] = []
    with path.open("rb") as handle:
        for idx, raw in enumerate(handle):
            if limits.max_records and len(records) >= limits.max_records:
                break
            if not raw.strip():
                continue
            try:
                record = json.loads(raw.decode("utf-8"))
            except Exception as exc:  # pylint: disable=broad-except
                errors.append(format_error(
                    ERR_INVALID_JSON,
                    f"line {idx + 1}: invalid json ({exc})",
                ))
                continue
            if isinstance(record, dict):
                records.append(record)
            else:
                errors.append(format_error(
                    ERR_INVALID_JSON,
                    f"line {idx + 1}: expected object",
                ))
    return records, errors


def _limit_records(records: List[Any], limits: AdapterLimits) -> List[Dict[str, Any]]:
    output: List[Dict[str, Any]] = []
    for item in records[: limits.max_records or len(records)]:
        if isinstance(item, dict):
            output.append(item)
    return output


def _get_value(record: Dict[str, Any], path: Union[str, List[str], None]) -> Any:
    """Get nested value using dotted path(s)."""
    if not path:
        return None
    if isinstance(path, str):
        if path == "$":
            return record
        if path.startswith("$const:"):
            return path[len("$const:"):]
        if isinstance(record, dict) and path in record:
            return record[path]
    if isinstance(path, list):
        for candidate in path:
            value = _get_value(record, candidate)
            if value is not None:
                return value
        return None
    current: Any = record
    for part in path.split("."):
        if isinstance(current, dict) and part in current:
            current = current[part]
        else:
            return None
    return current


def _extract_records(payload: Any, records_path: Optional[str]) -> Optional[List[Any]]:
    if records_path:
        if records_path == "*":
            if isinstance(payload, dict):
                return list(payload.values())
            if isinstance(payload, list):
                return payload
            return []
        value = _get_value(payload if isinstance(payload, dict) else {"_": payload}, records_path)
        if isinstance(value, list):
            return value
        if isinstance(value, dict):
            return [value]
        return []
    if isinstance(payload, list):
        return payload
    if isinstance(payload, dict):
        return [payload]
    return None


def _is_missing(value: Any) -> bool:
    if value is None:
        return True
    if value == "":
        return True
    if isinstance(value, (list, dict)) and not value:
        return True
    return False


def _env_true(name: str, default: bool = False) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    value = raw.strip().lower()
    return value in ("1", "true", "yes", "on")


def _sanitize_raw_context(data: Dict[str, Any]) -> Dict[str, Any]:
    from ..utils.bounded_json import bound_json

    max_depth = int(os.getenv("SENTINEL_RAW_CONTEXT_MAX_DEPTH", "6"))
    max_items = int(os.getenv("SENTINEL_RAW_CONTEXT_MAX_ITEMS", "200"))
    max_string_bytes = int(os.getenv("SENTINEL_RAW_CONTEXT_MAX_STRING_BYTES", "4096"))

    bounded, summary = bound_json(
        data,
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


def _sanitize_features(features: Dict[str, Any], limits: AdapterLimits) -> Dict[str, Any]:
    summary: Dict[str, Any] = {}

    if "findings" in features and isinstance(features["findings"], list):
        findings = []
        dropped = 0
        for item in features["findings"]:
            if not isinstance(item, dict):
                dropped += 1
                continue
            rule_id = item.get("rule_id")
            rule_name = item.get("rule_name")
            description = item.get("description")
            if not (rule_id or rule_name or description):
                dropped += 1
                continue
            severity = str(item.get("severity", "medium")).lower()
            if severity not in ("low", "medium", "high", "critical"):
                severity = "medium"
            item["severity"] = severity
            _truncate_strings(item, limits.max_string_bytes, summary, prefix="finding")
            findings.append(item)
            if len(findings) >= limits.max_findings:
                break
        if dropped:
            summary["findings_dropped"] = dropped
        if len(features["findings"]) > len(findings):
            summary["findings_trimmed"] = len(features["findings"]) - len(findings)
        features["findings"] = findings

    _truncate_strings_deep(
        features,
        limits.max_string_bytes,
        summary,
        prefix="feature",
        skip_keys={"findings"},
        max_items=limits.max_findings or 200,
    )
    return summary


def _truncate_strings(target: Dict[str, Any], max_bytes: int, summary: Dict[str, Any], prefix: str) -> None:
    if not max_bytes:
        return
    truncated = 0
    for key, value in list(target.items()):
        if isinstance(value, str) and len(value.encode("utf-8")) > max_bytes:
            target[key] = value.encode("utf-8")[:max_bytes].decode("utf-8", errors="ignore") + "...[truncated]"
            truncated += 1
    if truncated:
        summary[f"{prefix}_strings_truncated"] = summary.get(f"{prefix}_strings_truncated", 0) + truncated


def _truncate_strings_deep(
    target: Any,
    max_bytes: int,
    summary: Dict[str, Any],
    prefix: str,
    skip_keys: Optional[set[str]] = None,
    max_items: int = 200,
) -> None:
    if not max_bytes:
        return
    skip_keys = skip_keys or set()

    def _truncate_value(value: Any) -> Any:
        if isinstance(value, str) and len(value.encode("utf-8")) > max_bytes:
            summary[f"{prefix}_strings_truncated"] = summary.get(f"{prefix}_strings_truncated", 0) + 1
            return value.encode("utf-8")[:max_bytes].decode("utf-8", errors="ignore") + "...[truncated]"
        return value

    if isinstance(target, dict):
        for key, value in list(target.items()):
            if key in skip_keys:
                continue
            if isinstance(value, (dict, list)):
                _truncate_strings_deep(value, max_bytes, summary, prefix, skip_keys, max_items)
            else:
                target[key] = _truncate_value(value)
        return

    if isinstance(target, list):
        if max_items and len(target) > max_items:
            summary[f"{prefix}_list_trimmed"] = summary.get(f"{prefix}_list_trimmed", 0) + (len(target) - max_items)
            del target[max_items:]
        for idx, item in enumerate(list(target)):
            if isinstance(item, (dict, list)):
                _truncate_strings_deep(item, max_bytes, summary, prefix, skip_keys, max_items)
            else:
                target[idx] = _truncate_value(item)


def _apply_transform(value: Any, transform: Optional[Dict[str, Any]], features: Dict[str, Any]) -> Any:
    if transform is None:
        return value
    t_type = transform.get("type")
    if t_type == "enum":
        mapping = transform.get("map", {})
        key = str(value).lower() if isinstance(value, str) else value
        return mapping.get(key, mapping.get(str(value), value))
    if t_type == "int":
        try:
            return int(float(value))
        except Exception:  # pylint: disable=broad-except
            return None
    if t_type == "float":
        try:
            return float(value)
        except Exception:  # pylint: disable=broad-except
            return None
    if t_type == "str":
        return "" if value is None else str(value)
    if t_type == "bool":
        if isinstance(value, bool):
            return value
        if isinstance(value, str):
            return value.strip().lower() in ("true", "1", "yes", "on")
        return bool(value)
    if t_type == "lower":
        return str(value).lower() if value is not None else None
    if t_type == "upper":
        return str(value).upper() if value is not None else None
    if t_type == "scale":
        factor = float(transform.get("factor", 1.0))
        try:
            return float(value) * factor
        except Exception:  # pylint: disable=broad-except
            return None
    if t_type == "rate":
        numerator_fields = transform.get("numerator", [])
        denominator_field = transform.get("denominator")
        units = transform.get("units", "us")
        total = 0.0
        for field in numerator_fields:
            total += float(features.get(field, 0) or 0)
        denom = float(features.get(denominator_field, 0) or 0)
        if denom <= 0:
            return 0.0
        if units == "ms":
            denom = denom / 1000.0
        elif units == "us":
            denom = denom / 1_000_000.0
        return total / denom if denom > 0 else 0.0
    if t_type == "listify":
        if value is None:
            return []
        if isinstance(value, list):
            return value
        if isinstance(value, tuple):
            return list(value)
        return [value]
    if t_type == "map_list":
        mapping = transform.get("map", {})
        defaults = transform.get("defaults", {})
        lower_fields = set(transform.get("lower_fields", []))
        upper_fields = set(transform.get("upper_fields", []))
        field_transforms = transform.get("field_transforms", {})
        if isinstance(value, dict):
            value = [value]
        if not isinstance(value, list):
            return []
        mapped: List[Dict[str, Any]] = []
        for item in value:
            if not isinstance(item, dict):
                continue
            output: Dict[str, Any] = {}
            for target_field, source_path in mapping.items():
                field_value = _get_value(item, source_path)
                if field_value is None:
                    field_value = defaults.get(target_field)
                field_transform = field_transforms.get(target_field)
                if field_transform:
                    field_value = _apply_transform(field_value, field_transform, {})
                if isinstance(field_value, str):
                    if target_field in lower_fields:
                        field_value = field_value.lower()
                    if target_field in upper_fields:
                        field_value = field_value.upper()
                output[target_field] = field_value
            mapped.append(output)
        return mapped
    if t_type == "text_lines_to_findings":
        # Convert a list of text lines into a ScanFindingsV1-compatible list of findings.
        # This is used for tool outputs that are "artifacts" (compiled queries) rather than
        # runtime detections (e.g., sigma-cli output).
        if value is None:
            return []
        if isinstance(value, str):
            lines = [value]
        elif isinstance(value, list):
            lines = [x for x in value if isinstance(x, str) and x.strip()]
        else:
            return []
        rule_name = str(transform.get("rule_name", "artifact"))
        severity = str(transform.get("severity", "low")).lower()
        description = str(transform.get("description", "text artifact"))
        out: List[Dict[str, Any]] = []
        for line in lines:
            out.append(
                {
                    "rule_name": rule_name,
                    "severity": severity,
                    "description": description,
                    "evidence": line.strip(),
                }
            )
        return out
    return value


def _hash_event(features: Dict[str, Any], modality: str, tenant_id: str) -> str:
    payload = json.dumps(
        {"modality": modality, "tenant_id": tenant_id, "features": features},
        sort_keys=True,
        separators=(",", ":"),
    )
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()


def _parse_timestamp(record: Dict[str, Any], field: Optional[str]) -> Optional[float]:
    if not field:
        return None
    value = _get_value(record, field)
    if value is None:
        return None
    if isinstance(value, (int, float)):
        return float(value)
    if isinstance(value, str):
        try:
            return float(value)
        except ValueError:
            try:
                cleaned = value.strip()
                if cleaned.endswith("Z"):
                    cleaned = cleaned[:-1] + "+00:00"
                if "." in cleaned:
                    base, rest = cleaned.split(".", 1)
                    tz_idx = max(rest.find("+"), rest.find("-"))
                    if tz_idx > 0:
                        frac = rest[:tz_idx]
                        tz = rest[tz_idx:]
                    else:
                        frac = rest
                        tz = ""
                    frac = frac[:6]
                    cleaned = base + "." + frac + tz
                return datetime.fromisoformat(cleaned).timestamp()
            except ValueError:
                return None
    return None
