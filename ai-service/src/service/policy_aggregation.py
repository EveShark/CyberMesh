"""Hot-path policy aggregation for burst suppression."""

from __future__ import annotations

import hashlib
import json
import threading
import time
from dataclasses import dataclass
from typing import Dict, Optional, TYPE_CHECKING

from .policy_emitter import PolicyCandidate, PolicyContext

if TYPE_CHECKING:  # pragma: no cover
    from ..config.settings import PolicyPublishingConfig


@dataclass(frozen=True)
class AggregationDecision:
    publish: bool
    mode: str
    reason: Optional[str]
    aggregation_key: str
    policy_id: Optional[str] = None
    signal_count: int = 0
    refresh_count: int = 0


@dataclass
class _AggregationState:
    policy_id: Optional[str]
    first_seen_at: float
    last_published_at: float
    last_seen_at: float
    publish_count: int
    suppressed_count: int
    refresh_count: int
    signal_count: int
    max_severity: int
    max_confidence: float


class PolicyAggregationManager:
    """Suppress duplicate policy emissions for the same target window."""

    def __init__(self, config: "PolicyPublishingConfig"):
        self._config = config
        self._lock = threading.Lock()
        self._state: Dict[str, _AggregationState] = {}

    def admit(
        self,
        *,
        candidate: PolicyCandidate,
        context: PolicyContext,
        now: Optional[float] = None,
    ) -> AggregationDecision:
        if not getattr(self._config, "aggregation_enabled", True):
            return AggregationDecision(
                publish=True,
                mode="publish_new",
                reason=None,
                aggregation_key=self._aggregation_key(candidate, context),
                policy_id=candidate.policy_id,
                signal_count=1,
            )

        current = time.time() if now is None else float(now)
        cooldown = max(0.0, float(getattr(self._config, "aggregation_cooldown_seconds", 120)))
        min_signals = max(1, int(getattr(self._config, "aggregation_min_signals", 1)))
        escalation_window = max(
            1.0, float(getattr(self._config, "aggregation_escalation_window_seconds", 300))
        )
        refresh_margin = max(
            0.0, float(getattr(self._config, "aggregation_refresh_margin_seconds", 60))
        )
        emergency_severity = max(1, int(getattr(self._config, "aggregation_emergency_severity", 10)))
        emergency_confidence = float(getattr(self._config, "aggregation_emergency_confidence", 0.995))
        ttl_seconds = max(1.0, float(getattr(self._config, "ttl_seconds", 900)))
        key = self._aggregation_key(candidate, context)
        emergency = context.severity >= emergency_severity or context.confidence >= emergency_confidence

        with self._lock:
            self._prune_locked(current)
            state = self._state.get(key)
            if state is None:
                signal_count = 1
                if signal_count < min_signals and not emergency:
                    self._state[key] = _AggregationState(
                        policy_id=None,
                        first_seen_at=current,
                        last_published_at=0.0,
                        last_seen_at=current,
                        publish_count=0,
                        suppressed_count=1,
                        refresh_count=0,
                        signal_count=signal_count,
                        max_severity=context.severity,
                        max_confidence=context.confidence,
                    )
                    return AggregationDecision(
                        publish=False,
                        mode="suppress",
                        reason="aggregation_min_signals",
                        aggregation_key=key,
                        signal_count=signal_count,
                    )
                self._state[key] = _AggregationState(
                    policy_id=candidate.policy_id,
                    first_seen_at=current,
                    last_published_at=current,
                    last_seen_at=current,
                    publish_count=1,
                    suppressed_count=0,
                    refresh_count=0,
                    signal_count=signal_count,
                    max_severity=context.severity,
                    max_confidence=context.confidence,
                )
                return AggregationDecision(
                    publish=True,
                    mode="publish_new",
                    reason=None,
                    aggregation_key=key,
                    policy_id=candidate.policy_id,
                    signal_count=signal_count,
                )

            prev_max_severity = state.max_severity
            prev_max_confidence = state.max_confidence
            state.last_seen_at = current
            state.signal_count += 1

            if state.policy_id is None:
                if context.severity > state.max_severity:
                    state.max_severity = context.severity
                if context.confidence > state.max_confidence:
                    state.max_confidence = context.confidence
                if state.signal_count < min_signals and not emergency:
                    state.suppressed_count += 1
                    return AggregationDecision(
                        publish=False,
                        mode="suppress",
                        reason="aggregation_min_signals",
                        aggregation_key=key,
                        signal_count=state.signal_count,
                    )
                state.policy_id = candidate.policy_id
                state.last_published_at = current
                state.publish_count += 1
                return AggregationDecision(
                    publish=True,
                    mode="publish_new",
                    reason="aggregation_min_signals_satisfied",
                    aggregation_key=key,
                    policy_id=state.policy_id,
                    signal_count=state.signal_count,
                )

            since_last_publish = current - state.last_published_at
            escalate = (
                since_last_publish <= escalation_window
                and (
                    context.severity > prev_max_severity
                    or context.confidence > prev_max_confidence + 1e-9
                )
            )
            refresh_for_expiry = refresh_margin > 0 and since_last_publish >= max(0.0, ttl_seconds-refresh_margin)
            if context.severity > state.max_severity:
                state.max_severity = context.severity
            if context.confidence > state.max_confidence:
                state.max_confidence = context.confidence

            if cooldown <= 0 or since_last_publish >= cooldown or escalate or refresh_for_expiry:
                state.last_published_at = current
                state.refresh_count += 1
                return AggregationDecision(
                    publish=True,
                    mode="refresh_existing",
                    reason="aggregation_refresh_expiry" if refresh_for_expiry and not escalate else (
                        "aggregation_escalation" if escalate else "aggregation_cooldown_elapsed"
                    ),
                    aggregation_key=key,
                    policy_id=state.policy_id,
                    signal_count=state.signal_count,
                    refresh_count=state.refresh_count,
                )

            state.suppressed_count += 1
            return AggregationDecision(
                publish=False,
                mode="suppress",
                reason="aggregation_cooldown",
                aggregation_key=key,
                policy_id=state.policy_id,
                signal_count=state.signal_count,
                refresh_count=state.refresh_count,
            )

    def snapshot(self) -> Dict[str, int]:
        with self._lock:
            active_keys = len(self._state)
            published = sum(item.publish_count for item in self._state.values())
            suppressed = sum(item.suppressed_count for item in self._state.values())
            refreshed = sum(item.refresh_count for item in self._state.values())
        return {
            "active_keys": active_keys,
            "published": published,
            "suppressed": suppressed,
            "refreshed": refreshed,
        }

    def _prune_locked(self, now: float) -> None:
        ttl = max(
            float(getattr(self._config, "aggregation_cooldown_seconds", 120)),
            float(getattr(self._config, "ttl_seconds", 900)),
            float(getattr(self._config, "aggregation_window_seconds", 300)),
        )
        ttl = max(1.0, ttl)
        stale = [key for key, state in self._state.items() if (now - state.last_seen_at) >= ttl]
        for key in stale:
            self._state.pop(key, None)

        max_keys = max(1, int(getattr(self._config, "aggregation_max_keys", 50000)))
        if len(self._state) <= max_keys:
            return

        overflow = len(self._state) - max_keys
        for key, _state in sorted(self._state.items(), key=lambda item: item[1].last_seen_at):
            self._state.pop(key, None)
            overflow -= 1
            if overflow <= 0:
                break

    @staticmethod
    def _normalize_ports(raw_ports) -> list[str]:
        normalized: list[str] = []
        for entry in raw_ports or []:
            if isinstance(entry, bool):
                continue
            if isinstance(entry, int):
                if 0 <= entry <= 65535:
                    normalized.append(str(entry))
                continue
            if isinstance(entry, str):
                token = entry.strip().lower()
                if token:
                    normalized.append(token)
                continue
            if isinstance(entry, dict):
                from_v = entry.get("from")
                to_v = entry.get("to")
                if isinstance(from_v, int) and isinstance(to_v, int):
                    normalized.append(f"{from_v}-{to_v}")
                continue
            if isinstance(entry, (list, tuple)) and len(entry) == 2:
                start, end = entry
                if isinstance(start, int) and isinstance(end, int):
                    normalized.append(f"{start}-{end}")
                continue
        return sorted(set(normalized))

    @staticmethod
    def _aggregation_key(candidate: PolicyCandidate, context: PolicyContext) -> str:
        payload = candidate.payload if isinstance(candidate.payload, dict) else {}
        target = payload.get("target") if isinstance(payload.get("target"), dict) else {}
        metadata = payload.get("metadata") if isinstance(payload.get("metadata"), dict) else {}
        selectors = target.get("selectors") if isinstance(target.get("selectors"), dict) else {}

        canonical_target = {
            "scope": str(target.get("scope") or "").strip().lower(),
            "tenant": str(target.get("tenant") or metadata.get("tenant_id") or "").strip().lower(),
            "region": str(target.get("region") or metadata.get("region") or "").strip().lower(),
            "ips": sorted(str(value).strip().lower() for value in (target.get("ips") or []) if str(value).strip()),
            "cidrs": sorted(str(value).strip().lower() for value in (target.get("cidrs") or []) if str(value).strip()),
            "protocols": sorted(str(value).strip().lower() for value in (target.get("protocols") or []) if str(value).strip()),
            "ports": PolicyAggregationManager._normalize_ports(target.get("ports")),
            "selectors": {str(k).strip().lower(): str(v).strip().lower() for k, v in sorted(selectors.items())},
            "scope_identifier": str(
                payload.get("scope_identifier")
                or metadata.get("scope_identifier")
                or context.metadata.get("scope_identifier")
                or ""
            ).strip().lower(),
            "action": str(candidate.action or payload.get("action") or "").strip().lower(),
            "rule_type": str(candidate.rule_type or payload.get("rule_type") or "").strip().lower(),
        }
        encoded = json.dumps(canonical_target, sort_keys=True, separators=(",", ":")).encode("utf-8")
        return hashlib.sha256(encoded).hexdigest()
