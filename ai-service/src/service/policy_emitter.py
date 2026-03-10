"""Helpers for emitting policy recommendations from anomalies."""

from dataclasses import dataclass
from typing import Any, Dict, Optional
import ipaddress
import os
import time
import uuid
import math

from ..config.settings import PolicyPublishingConfig
from .policy_decision_engine import PolicyDecisionEngine


@dataclass(frozen=True)
class PolicyContext:
    anomaly_id: str
    anomaly_type: str
    severity: int
    confidence: float
    network_context: Dict[str, Any]
    metadata: Dict[str, Any]


@dataclass(frozen=True)
class PolicyCandidate:
    policy_id: str
    rule_type: str
    action: str
    payload: Dict[str, Any]


@dataclass(frozen=True)
class PolicyDecision:
    candidate: Optional[PolicyCandidate]
    reason: Optional[str]
    effective_confidence_threshold: Optional[float] = None
    effective_severity_threshold: Optional[int] = None


_ENGINE_CACHE: Dict[int, PolicyDecisionEngine] = {}


def _get_engine(config: PolicyPublishingConfig) -> PolicyDecisionEngine:
    cache_key = id(config)
    engine = _ENGINE_CACHE.get(cache_key)
    if engine is None:
        engine = PolicyDecisionEngine(config)
        _ENGINE_CACHE[cache_key] = engine
    return engine


def build_policy_candidate(
    context: PolicyContext,
    config: PolicyPublishingConfig,
) -> PolicyDecision:
    """Derive a policy candidate from anomaly context.

    Returns a candidate when the anomaly meets configured thresholds.
    Otherwise returns the reason for skipping.
    """

    if not config.enabled:
        return PolicyDecision(candidate=None, reason="disabled")

    decision = _get_engine(config).evaluate(context)
    if not decision.should_publish:
        return PolicyDecision(
            candidate=None,
            reason=decision.reason or "policy_suppressed",
            effective_confidence_threshold=decision.effective_confidence_threshold,
            effective_severity_threshold=decision.effective_severity_threshold,
        )

    target_ip = _select_target_ip(context.network_context)
    if not target_ip:
        return PolicyDecision(candidate=None, reason="missing_target")

    try:
        ipaddress.ip_address(target_ip)
    except ValueError:
        return PolicyDecision(candidate=None, reason="invalid_target_ip")

    ttl_seconds = max(1, int(config.ttl_seconds))
    cidr_max = int(config.cidr_max_prefix_len)
    if cidr_max <= 0 or cidr_max > 128:
        cidr_max = 24

    policy_id = str(uuid.uuid4())

    target_scope = config.scope
    direction = config.direction
    enforcement_action = "drop"

    tenant = (
        context.metadata.get("tenant")
        or context.metadata.get("tenant_id")
        or context.network_context.get("tenant")
        or context.network_context.get("tenant_id")
    )
    if isinstance(tenant, str):
        tenant = tenant.strip()
    else:
        tenant = ""
    if not tenant:
        return PolicyDecision(candidate=None, reason="missing_tenant")

    target: Dict[str, Any] = {
        "ips": [target_ip],
        "cidrs": [],
        "ports": [],
        "protocols": [],
        "direction": direction,
        "scope": target_scope,
        "tenant": tenant,
        "region": context.metadata.get("region") or None,
        "selectors": {},
    }

    namespace = (
        context.metadata.get("namespace")
        or context.metadata.get("kubernetes_namespace")
        or os.getenv("POLICY_PUBLISHING_NAMESPACE")
        or os.getenv("POLICY_GATEWAY_NAMESPACE")
    )
    if namespace:
        # Keep both keys for backward compatibility while backend gateway profile
        # standardizes on target.selectors.namespace.
        target["selectors"] = {
            "namespace": namespace,
            "kubernetes_namespace": namespace,
        }

    guardrails: Dict[str, Any] = {
        "ttl_seconds": ttl_seconds,
        "cidr_max_prefix_len": cidr_max,
        # approval_required is for manual staging workflows (human approval) and should
        # not be overloaded to mean "emit ACK".
        "approval_required": False,
        # requires_ack means enforcement should emit an ACK after applying the policy.
        "requires_ack": bool(config.requires_ack),
        "max_policies_per_minute": int(config.max_policies_per_minute),
        "fast_path_enabled": bool(config.fast_path_enabled),
        "fast_path_ttl_seconds": int(config.fast_path_ttl_seconds),
        "fast_path_signals_required": int(config.fast_path_signals_required),
        "fast_path_confidence_min": float(config.fast_path_confidence_min),
        "fast_path_canary_scope": bool(config.canary_scope),
        "dry_run": bool(config.dry_run),
        "canary_scope": bool(config.canary_scope),
    }

    if config.max_targets > 0:
        guardrails["max_targets"] = int(config.max_targets)

    trace_id = str(uuid.uuid4())
    ai_event_ts_ms = int(time.time() * 1000)
    source_event_id = _extract_source_event_id(context.metadata)
    source_event_ts_ms = _extract_source_event_ts_ms(
        context.metadata,
        network_context=context.network_context,
    )
    telemetry_ingest_ts_ms = _extract_telemetry_ingest_ts_ms(
        context.metadata,
        network_context=context.network_context,
    )
    trace_stages = _extract_trace_stages(
        context.metadata,
        source_event_ts_ms=source_event_ts_ms,
        telemetry_ingest_ts_ms=telemetry_ingest_ts_ms,
        ai_event_ts_ms=ai_event_ts_ms,
    )
    if source_event_id:
        # Keep one stable lineage id across Sentinel->AI->Backend->ACK for true causal joins.
        trace_id = source_event_id

    payload: Dict[str, Any] = {
        "schema_version": 1,
        "policy_id": policy_id,
        "rule_type": "block",
        "action": enforcement_action,
        "tenant": tenant,
        # Reused by enforcement ACK as qc_reference for cross-service causal tracing.
        "qc_reference": trace_id,
        "target": {k: v for k, v in target.items() if v not in (None, [], {})},
        "guardrails": guardrails,
        "criteria": {
            "severity": context.severity,
            "min_severity": int(decision.effective_severity_threshold),
            "min_confidence": decision.effective_confidence_threshold,
            "anomaly_type": context.anomaly_type,
        },
        "metadata": {
            "anomaly_id": context.anomaly_id,
            "anomaly_type": context.anomaly_type,
            "trace_id": trace_id,
            "ai_event_ts_ms": ai_event_ts_ms,
            "source_event_id": source_event_id,
            "source_event_ts_ms": source_event_ts_ms,
            "telemetry_ingest_ts_ms": telemetry_ingest_ts_ms,
            "sentinel_event_id": context.metadata.get("sentinel_event_id"),
            "source_service": "ai-service",
            "trace_version": 1,
        },
        "trace": {
            "id": trace_id,
            "ai_event_ts_ms": ai_event_ts_ms,
            "source_event_id": source_event_id,
            "source_event_ts_ms": source_event_ts_ms,
            "telemetry_ingest_ts_ms": telemetry_ingest_ts_ms,
            "source": "ai-service",
            "version": 1,
            "stages": trace_stages,
        },
    }

    return PolicyDecision(
        candidate=PolicyCandidate(
            policy_id=policy_id,
            rule_type="block",
            action=enforcement_action,
            payload=payload,
        ),
        reason=None,
        effective_confidence_threshold=decision.effective_confidence_threshold,
        effective_severity_threshold=decision.effective_severity_threshold,
    )


def _select_target_ip(network_context: Dict[str, Any]) -> Optional[str]:
    preferred_keys = ("src_ip", "source_ip", "dst_ip", "destination_ip")
    for key in preferred_keys:
        value = network_context.get(key)
        if value and isinstance(value, str) and value.lower() not in {"unknown", "null", "none"}:
            return value
    return None


def _extract_source_event_id(metadata: Dict[str, Any]) -> str:
    if not isinstance(metadata, dict):
        return ""
    for key in ("source_event_id", "telemetry_event_id", "input_event_id", "flow_id"):
        value = metadata.get(key)
        if isinstance(value, str):
            value = value.strip()
            if value:
                return value
    return ""


def _extract_source_event_ts_ms(metadata: Dict[str, Any], *, network_context: Optional[Dict[str, Any]] = None) -> int:
    if isinstance(network_context, dict):
        for key in ("source_event_ts_ms", "telemetry_event_ts_ms", "input_event_ts_ms", "event_ts_ms"):
            value = network_context.get(key)
            normalized = _normalize_unix_ms(value)
            if normalized > 0:
                return normalized
    if not isinstance(metadata, dict):
        return 0
    for key in ("source_event_ts_ms", "telemetry_event_ts_ms", "input_event_ts_ms", "event_ts_ms", "timestamp"):
        value = metadata.get(key)
        normalized = _normalize_unix_ms(value)
        if normalized > 0:
            return normalized
    return 0


def _extract_telemetry_ingest_ts_ms(metadata: Dict[str, Any], *, network_context: Optional[Dict[str, Any]] = None) -> int:
    if isinstance(network_context, dict):
        for key in ("telemetry_ingest_ts_ms", "t_telemetry_ingest_ms", "telemetry_ingest_time_ms"):
            value = network_context.get(key)
            normalized = _normalize_unix_ms(value)
            if normalized > 0:
                return normalized
    if not isinstance(metadata, dict):
        return 0
    for key in ("telemetry_ingest_ts_ms", "t_telemetry_ingest_ms", "telemetry_ingest_time_ms"):
        value = metadata.get(key)
        normalized = _normalize_unix_ms(value)
        if normalized > 0:
            return normalized
    return 0


def _extract_trace_stages(
    metadata: Dict[str, Any],
    *,
    source_event_ts_ms: int,
    telemetry_ingest_ts_ms: int,
    ai_event_ts_ms: int,
) -> Dict[str, int]:
    stages: Dict[str, int] = {}
    if telemetry_ingest_ts_ms > 0:
        stages["t_telemetry_ingest"] = telemetry_ingest_ts_ms
    elif source_event_ts_ms > 0:
        stages["t_telemetry_ingest"] = source_event_ts_ms
    if ai_event_ts_ms > 0:
        stages["t_ai_decision_done"] = ai_event_ts_ms
    if not isinstance(metadata, dict):
        return stages
    mapping = {
        "t_sentinel_consume": (
            "t_sentinel_consume_ms",
            "sentinel_consume_ts_ms",
        ),
        "t_sentinel_analysis_done": (
            "t_sentinel_analysis_done_ms",
            "sentinel_analysis_done_ts_ms",
        ),
        "t_sentinel_emit": (
            "t_sentinel_emit_ms",
            "sentinel_emit_ts_ms",
        ),
        "t_ai_sentinel_consume": (
            "t_ai_sentinel_consume_ms",
            "ai_sentinel_consume_ts_ms",
        ),
    }
    for stage, keys in mapping.items():
        for key in keys:
            normalized = _normalize_unix_ms(metadata.get(key))
            if normalized > 0:
                stages[stage] = normalized
                break
    return stages


def _normalize_unix_ms(value: Any) -> int:
    if value is None:
        return 0
    try:
        v = float(value)
    except (TypeError, ValueError):
        return 0
    if math.isnan(v) or math.isinf(v):
        return 0
    # seconds
    if 946684800 <= v <= 4102444800:
        return int(v * 1000)
    # milliseconds
    if 946684800000 <= v <= 4102444800000:
        return int(v)
    # microseconds
    if 946684800000000 <= v <= 4102444800000000:
        return int(v / 1000)
    # nanoseconds
    if 946684800000000000 <= v <= 4102444800000000000:
        return int(v / 1_000_000)
    return 0


