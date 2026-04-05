from __future__ import annotations

import os
import threading
import time
from collections import defaultdict, deque
from dataclasses import dataclass
from typing import Any, Deque, Dict, Tuple


def _env_bool(name: str, default: bool = False) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    return value.strip().lower() in ("1", "true", "yes", "on")


def _env_int(name: str, default: int, minimum: int = 1) -> int:
    value = os.getenv(name)
    if value is None:
        return default
    try:
        parsed = int(value)
    except ValueError:
        return default
    return max(minimum, parsed)


def _env_float(name: str, default: float, minimum: float = 0.0, maximum: float = 1.0) -> float:
    value = os.getenv(name)
    if value is None:
        return default
    try:
        parsed = float(value)
    except ValueError:
        return default
    return min(maximum, max(minimum, parsed))


@dataclass(frozen=True)
class AppEventsPolicyConfig:
    enabled: bool
    window_seconds: int
    cooldown_seconds: int
    auth_fail_threshold: int
    admin_mutation_threshold: int
    export_spike_threshold: int
    request_spike_threshold: int
    critical_multiplier: int
    action_auth_fail: str
    action_admin_mutation: str
    action_export_spike: str
    action_request_spike: str
    action_critical: str
    confidence_floor_auth: float
    confidence_floor_admin: float
    confidence_floor_export: float
    confidence_floor_request: float

    @classmethod
    def from_env(cls) -> "AppEventsPolicyConfig":
        return cls(
            enabled=_env_bool("APP_EVENTS_POLICY_PACK_ENABLED", False),
            window_seconds=_env_int("APP_EVENTS_POLICY_WINDOW_SECONDS", 120),
            cooldown_seconds=_env_int("APP_EVENTS_POLICY_COOLDOWN_SECONDS", 180),
            auth_fail_threshold=_env_int("APP_EVENTS_POLICY_AUTH_FAIL_THRESHOLD", 5),
            admin_mutation_threshold=_env_int("APP_EVENTS_POLICY_ADMIN_MUTATION_THRESHOLD", 6),
            export_spike_threshold=_env_int("APP_EVENTS_POLICY_EXPORT_SPIKE_THRESHOLD", 8),
            request_spike_threshold=_env_int("APP_EVENTS_POLICY_REQUEST_SPIKE_THRESHOLD", 30),
            critical_multiplier=_env_int("APP_EVENTS_POLICY_CRITICAL_MULTIPLIER", 2),
            action_auth_fail=os.getenv("APP_EVENTS_POLICY_ACTION_AUTH_FAIL", "force_reauth").strip() or "force_reauth",
            action_admin_mutation=os.getenv("APP_EVENTS_POLICY_ACTION_ADMIN_MUTATION", "throttle_action").strip()
            or "throttle_action",
            action_export_spike=os.getenv("APP_EVENTS_POLICY_ACTION_EXPORT_SPIKE", "throttle_action").strip()
            or "throttle_action",
            action_request_spike=os.getenv("APP_EVENTS_POLICY_ACTION_REQUEST_SPIKE", "throttle_action").strip()
            or "throttle_action",
            action_critical=os.getenv("APP_EVENTS_POLICY_ACTION_CRITICAL", "freeze_user").strip() or "freeze_user",
            confidence_floor_auth=_env_float("APP_EVENTS_POLICY_CONFIDENCE_FLOOR_AUTH", 0.92),
            confidence_floor_admin=_env_float("APP_EVENTS_POLICY_CONFIDENCE_FLOOR_ADMIN", 0.90),
            confidence_floor_export=_env_float("APP_EVENTS_POLICY_CONFIDENCE_FLOOR_EXPORT", 0.88),
            confidence_floor_request=_env_float("APP_EVENTS_POLICY_CONFIDENCE_FLOOR_REQUEST", 0.85),
        )


@dataclass(frozen=True)
class AppEventsPolicyDecision:
    matched: bool
    signal: str
    threshold: int
    count_in_window: int
    effective_severity: int
    effective_confidence: float
    recommended_action: str
    should_suppress: bool
    suppress_reason: str
    entity_key: str
    is_critical: bool

    @property
    def applies(self) -> bool:
        return self.matched and self.count_in_window >= self.threshold


class AppEventsPolicyPack:
    """Signal-based policy booster for generic app events.

    This pack is intentionally client-agnostic and keyed by app/source metadata.
    It does not force policy publication by itself; it only boosts/suppresses
    severity/confidence decisions for downstream policy emission logic.
    """

    def __init__(self, config: AppEventsPolicyConfig):
        self._cfg = config
        self._lock = threading.Lock()
        self._signal_events: Dict[Tuple[str, str], Deque[int]] = defaultdict(deque)
        self._action_cooldown_until_ms: Dict[Tuple[str, str], int] = {}

    @classmethod
    def from_env(cls) -> "AppEventsPolicyPack":
        return cls(AppEventsPolicyConfig.from_env())

    @property
    def enabled(self) -> bool:
        return self._cfg.enabled

    def evaluate(
        self,
        *,
        labels: Dict[str, Any],
        metadata: Dict[str, Any],
        network_context: Dict[str, Any],
        anomaly_type: str,
        severity: int,
        confidence: float,
        now_ms: int | None = None,
    ) -> AppEventsPolicyDecision:
        if not self._cfg.enabled:
            return AppEventsPolicyDecision(
                matched=False,
                signal="",
                threshold=0,
                count_in_window=0,
                effective_severity=severity,
                effective_confidence=confidence,
                recommended_action="",
                should_suppress=False,
                suppress_reason="disabled",
                entity_key="",
                is_critical=False,
            )

        event_category = _pick_str(metadata, labels, key="event_category")
        event_name = _pick_str(metadata, labels, key="event_name")
        source_type = _pick_str(metadata, labels, network_context, key="source_type")
        app_id = _pick_str(metadata, labels, key="app_id")

        if not self._is_app_event_context(
            app_id=app_id,
            source_type=source_type,
            event_category=event_category,
            event_name=event_name,
        ):
            return AppEventsPolicyDecision(
                matched=False,
                signal="",
                threshold=0,
                count_in_window=0,
                effective_severity=severity,
                effective_confidence=confidence,
                recommended_action="",
                should_suppress=False,
                suppress_reason="not_app_event",
                entity_key="",
                is_critical=False,
            )

        signal = self._classify_signal(event_category=event_category, event_name=event_name, anomaly_type=anomaly_type)
        if signal == "":
            return AppEventsPolicyDecision(
                matched=False,
                signal="",
                threshold=0,
                count_in_window=0,
                effective_severity=severity,
                effective_confidence=confidence,
                recommended_action="",
                should_suppress=False,
                suppress_reason="no_signal_match",
                entity_key="",
                is_critical=False,
            )

        entity_key = self._entity_key(app_id=app_id, metadata=metadata, labels=labels, network_context=network_context)
        if entity_key == "":
            entity_key = "global"

        ts_ms = int(now_ms if now_ms is not None else int(time.time() * 1000))
        threshold = self._threshold_for_signal(signal)
        count_in_window = self._record_and_count(signal=signal, entity_key=entity_key, ts_ms=ts_ms)

        action = self._action_for_signal(signal)
        is_critical = count_in_window >= (threshold * max(1, self._cfg.critical_multiplier))
        if is_critical:
            action = self._cfg.action_critical

        effective_severity = max(severity, self._severity_for_signal(signal, critical=is_critical))
        effective_confidence = max(confidence, self._confidence_floor_for_signal(signal))

        should_suppress = False
        suppress_reason = ""
        if count_in_window >= threshold:
            should_suppress, suppress_reason = self._check_and_mark_cooldown(
                entity_key=entity_key,
                signal=signal,
                ts_ms=ts_ms,
            )

        return AppEventsPolicyDecision(
            matched=True,
            signal=signal,
            threshold=threshold,
            count_in_window=count_in_window,
            effective_severity=effective_severity,
            effective_confidence=effective_confidence,
            recommended_action=action,
            should_suppress=should_suppress,
            suppress_reason=suppress_reason,
            entity_key=entity_key,
            is_critical=is_critical,
        )

    def _record_and_count(self, *, signal: str, entity_key: str, ts_ms: int) -> int:
        window_ms = self._cfg.window_seconds * 1000
        key = (signal, entity_key)
        with self._lock:
            q = self._signal_events[key]
            q.append(ts_ms)
            cutoff = ts_ms - window_ms
            while q and q[0] < cutoff:
                q.popleft()
            return len(q)

    def _check_and_mark_cooldown(self, *, entity_key: str, signal: str, ts_ms: int) -> tuple[bool, str]:
        cooldown_ms = self._cfg.cooldown_seconds * 1000
        key = (entity_key, signal)
        with self._lock:
            until = int(self._action_cooldown_until_ms.get(key, 0))
            if ts_ms < until:
                return True, "cooldown_active"
            self._action_cooldown_until_ms[key] = ts_ms + cooldown_ms
        return False, ""

    def _threshold_for_signal(self, signal: str) -> int:
        if signal == "auth_failure_burst":
            return self._cfg.auth_fail_threshold
        if signal == "admin_mutation_burst":
            return self._cfg.admin_mutation_threshold
        if signal == "export_spike":
            return self._cfg.export_spike_threshold
        return self._cfg.request_spike_threshold

    def _action_for_signal(self, signal: str) -> str:
        if signal == "auth_failure_burst":
            return self._cfg.action_auth_fail
        if signal == "admin_mutation_burst":
            return self._cfg.action_admin_mutation
        if signal == "export_spike":
            return self._cfg.action_export_spike
        return self._cfg.action_request_spike

    def _severity_for_signal(self, signal: str, *, critical: bool) -> int:
        if critical:
            return 10
        if signal in ("auth_failure_burst", "admin_mutation_burst"):
            return 9
        return 8

    def _confidence_floor_for_signal(self, signal: str) -> float:
        if signal == "auth_failure_burst":
            return self._cfg.confidence_floor_auth
        if signal == "admin_mutation_burst":
            return self._cfg.confidence_floor_admin
        if signal == "export_spike":
            return self._cfg.confidence_floor_export
        return self._cfg.confidence_floor_request

    @staticmethod
    def _is_app_event_context(*, app_id: str, source_type: str, event_category: str, event_name: str) -> bool:
        app_present = app_id.strip() != ""
        source_token = source_type.strip().lower()
        category_token = event_category.strip().lower()
        name_token = event_name.strip().lower()
        app_source = (
            source_token in {"supabase", "vercel", "app_events", "app"}
            or source_token.startswith("app_")
            or source_token.endswith("_app")
            or source_token.endswith("-app")
        )
        app_category = category_token in {"auth", "admin", "business", "security", "request"}
        return app_present or app_source or app_category or ("login" in name_token or "export" in name_token)

    @staticmethod
    def _classify_signal(*, event_category: str, event_name: str, anomaly_type: str) -> str:
        category = event_category.strip().lower()
        name = event_name.strip().lower()
        anom = anomaly_type.strip().lower()
        tokens = f"{category} {name} {anom}"

        if category == "auth" and any(t in tokens for t in ("login", "signin", "auth")) and any(
            t in tokens for t in ("fail", "denied", "invalid", "error", "blocked")
        ):
            return "auth_failure_burst"
        if category == "admin" and any(
            t in tokens for t in ("create", "update", "delete", "remove", "role", "permission", "config", "billing")
        ):
            return "admin_mutation_burst"
        if any(t in tokens for t in ("export", "download", "upload", "exfil", "bulk")):
            return "export_spike"
        if category == "request" or any(t in tokens for t in ("scrape", "crawl", "request_spike", "rate")):
            return "request_spike"
        return ""

    @staticmethod
    def _entity_key(
        *,
        app_id: str,
        metadata: Dict[str, Any],
        labels: Dict[str, Any],
        network_context: Dict[str, Any],
    ) -> str:
        tenant = _pick_str(metadata, labels, network_context, key="tenant_id")
        user = _pick_str(metadata, labels, network_context, key="user_id")
        source = _pick_str(metadata, labels, network_context, key="source_id")
        return "|".join([app_id.strip().lower(), tenant.strip().lower(), user.strip().lower(), source.strip().lower()])


def _pick_str(*sources: Dict[str, Any], key: str) -> str:
    for source in sources:
        if not isinstance(source, dict):
            continue
        value = source.get(key)
        if isinstance(value, str):
            value = value.strip()
            if value:
                return value
    return ""
