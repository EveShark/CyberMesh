"""Helpers for provisional fast-lane mitigations."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Dict, Optional

from ..utils.id_gen import generate_uuid7
from .policy_emitter import PolicyCandidate, PolicyContext

FAST_ALLOWED_ANOMALY_TYPES = {"ddos"}
FAST_ALLOWED_ACTIONS = {"drop", "rate_limit"}
FAST_ALLOWED_SCOPES = {"cluster", "namespace"}
FAST_ALLOWED_SELECTOR_KEYS = {"namespace", "kubernetes_namespace"}
FAST_MAX_TTL_SECONDS = 60


@dataclass(frozen=True)
class FastMitigationDecision:
    publish: bool
    mitigation_id: Optional[str]
    payload: Optional[Dict[str, Any]]
    reason: Optional[str]


def decide_fast_mitigation(
    *,
    candidate: PolicyCandidate,
    context: PolicyContext,
    config: Any,
    aggregation_mode: str,
    signal_count: int,
) -> FastMitigationDecision:
    if not getattr(config, "fast_path_enabled", False):
        return FastMitigationDecision(False, None, None, "fast_path_disabled")

    if str(context.anomaly_type or "").strip().lower() not in FAST_ALLOWED_ANOMALY_TYPES:
        return FastMitigationDecision(False, None, None, "fast_path_anomaly_type")

    if str(candidate.rule_type or "").strip().lower() != "block":
        return FastMitigationDecision(False, None, None, "fast_path_rule_type")

    if str(candidate.action or "").strip().lower() not in FAST_ALLOWED_ACTIONS:
        return FastMitigationDecision(False, None, None, "fast_path_action")

    if aggregation_mode not in {"publish_new", "refresh_existing"}:
        return FastMitigationDecision(False, None, None, "fast_path_aggregation_mode")

    confidence_min = float(getattr(config, "fast_path_confidence_min", 0.9))
    signals_required = max(1, int(getattr(config, "fast_path_signals_required", 2)))
    emergency_severity = max(1, int(getattr(config, "aggregation_emergency_severity", 10)))
    emergency_confidence = float(getattr(config, "aggregation_emergency_confidence", 0.995))

    emergency = context.severity >= emergency_severity or context.confidence >= emergency_confidence
    if context.confidence < confidence_min and not emergency:
        return FastMitigationDecision(False, None, None, "fast_path_confidence")
    if signal_count < signals_required and not emergency:
        return FastMitigationDecision(False, None, None, "fast_path_signals")

    payload = dict(candidate.payload or {})
    for field in ("tenant", "region"):
        payload.pop(field, None)
    target = dict(payload.get("target") or {})
    target.pop("tenant", None)
    payload["target"] = target
    allowed, reason = _validate_fast_payload_shape(payload)
    if not allowed:
        return FastMitigationDecision(False, None, None, reason)
    payload["provisional"] = True
    payload["promotion_requested"] = True

    guardrails = dict(payload.get("guardrails") or {})
    fast_ttl = guardrails.get("fast_path_ttl_seconds") or getattr(config, "fast_path_ttl_seconds", 30)
    guardrails["ttl_seconds"] = int(fast_ttl)
    payload["guardrails"] = guardrails

    metadata = dict(payload.get("metadata") or {})
    metadata["fast_lane"] = {
        "enabled": True,
        "mode": aggregation_mode,
        "signal_count": int(signal_count),
        "emergency": bool(emergency),
    }
    payload["metadata"] = metadata
    payload["fast_path"] = True

    return FastMitigationDecision(
        publish=True,
        mitigation_id=generate_uuid7(),
        payload=payload,
        reason="fast_path_emergency" if emergency else "fast_path_threshold_met",
    )


def _validate_fast_payload_shape(payload: Dict[str, Any]) -> tuple[bool, str]:
    target = payload.get("target")
    if not isinstance(target, dict):
        return False, "fast_path_target"
    metadata = payload.get("metadata")
    if not isinstance(metadata, dict):
        return False, "fast_path_metadata"
    if not isinstance(metadata.get("trace_id"), str) or not str(metadata.get("trace_id")).strip():
        return False, "fast_path_trace_id"
    if not isinstance(metadata.get("anomaly_id"), str) or not str(metadata.get("anomaly_id")).strip():
        return False, "fast_path_anomaly_id"

    scope = str(target.get("scope") or "").strip().lower()
    if scope not in FAST_ALLOWED_SCOPES:
        return False, "fast_path_scope"
    for field in ("tenant", "region"):
        if str(target.get(field) or "").strip():
            return False, "fast_path_scope"
        if str(payload.get(field) or "").strip():
            return False, "fast_path_scope"

    ips = target.get("ips")
    cidrs = target.get("cidrs")
    if not isinstance(ips, list):
        ips = []
    if not isinstance(cidrs, list):
        cidrs = []
    if len(ips) == 0 and len(cidrs) == 0:
        return False, "fast_path_targets"

    selectors = target.get("selectors")
    if selectors is None:
        selectors = {}
    if not isinstance(selectors, dict):
        return False, "fast_path_selectors"
    selector_keys = {str(key).strip().lower() for key in selectors.keys()}
    if not selector_keys.issubset(FAST_ALLOWED_SELECTOR_KEYS):
        return False, "fast_path_selectors"
    if scope != "namespace" and selector_keys:
        return False, "fast_path_selectors"

    guardrails = payload.get("guardrails")
    if not isinstance(guardrails, dict):
        return False, "fast_path_guardrails"
    ttl_seconds = guardrails.get("fast_path_ttl_seconds") or guardrails.get("ttl_seconds")
    if not isinstance(ttl_seconds, int) or ttl_seconds <= 0:
        return False, "fast_path_ttl"
    if ttl_seconds > FAST_MAX_TTL_SECONDS:
        return False, "fast_path_ttl"

    return True, ""
