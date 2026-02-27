"""Helpers for emitting policy recommendations from anomalies."""

from dataclasses import dataclass
from typing import Any, Dict, Optional
import ipaddress
import os
import time
import uuid

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

    if context.severity < config.severity_threshold:
        return PolicyDecision(candidate=None, reason="severity_below_threshold")

    decision = _get_engine(config).evaluate(context)
    if not decision.should_publish:
        return PolicyDecision(candidate=None, reason=decision.reason or "policy_suppressed")

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

    target: Dict[str, Any] = {
        "ips": [target_ip],
        "cidrs": [],
        "ports": [],
        "protocols": [],
        "direction": direction,
        "scope": target_scope,
        "tenant_id": context.metadata.get("tenant_id") or None,
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

    payload: Dict[str, Any] = {
        "schema_version": 1,
        "policy_id": policy_id,
        "rule_type": "block",
        "action": enforcement_action,
        # Reused by enforcement ACK as qc_reference for cross-service causal tracing.
        "qc_reference": trace_id,
        "target": {k: v for k, v in target.items() if v not in (None, [], {})},
        "guardrails": guardrails,
        "criteria": {
            "severity": context.severity,
            "min_confidence": decision.effective_confidence_threshold,
            "anomaly_type": context.anomaly_type,
        },
        "metadata": {
            "anomaly_id": context.anomaly_id,
            "anomaly_type": context.anomaly_type,
            "trace_id": trace_id,
            "ai_event_ts_ms": ai_event_ts_ms,
            "source_service": "ai-service",
            "trace_version": 1,
        },
        "trace": {
            "id": trace_id,
            "ai_event_ts_ms": ai_event_ts_ms,
            "source": "ai-service",
            "version": 1,
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
    )


def _select_target_ip(network_context: Dict[str, Any]) -> Optional[str]:
    preferred_keys = ("src_ip", "source_ip", "dst_ip", "destination_ip")
    for key in preferred_keys:
        value = network_context.get(key)
        if value and isinstance(value, str) and value.lower() not in {"unknown", "null", "none"}:
            return value
    return None
