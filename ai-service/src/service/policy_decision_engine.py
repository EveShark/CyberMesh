"""Dynamic policy decision engine with bounded adaptive thresholds."""

from __future__ import annotations

import os
import threading
import time
from dataclasses import dataclass
from typing import Dict, Optional, TYPE_CHECKING

if TYPE_CHECKING:  # pragma: no cover
    from .policy_emitter import PolicyContext
from ..config.settings import PolicyPublishingConfig


@dataclass(frozen=True)
class DecisionOutcome:
    should_publish: bool
    reason: Optional[str]
    effective_confidence_threshold: float
    effective_severity_threshold: int


@dataclass
class _DecisionState:
    ema_score: float
    last_publish: bool
    updated_at: float


class PolicyDecisionEngine:
    """Applies math-driven policy gating while preserving fail-closed behavior."""

    def __init__(self, config: PolicyPublishingConfig):
        self._config = config
        self._lock = threading.Lock()
        self._state: Dict[str, _DecisionState] = {}

    def evaluate(self, context: "PolicyContext") -> DecisionOutcome:
        effective_severity_threshold = self._effective_severity_threshold(context)
        if not self._config.enabled:
            return DecisionOutcome(
                False,
                "disabled",
                self._config.confidence_threshold,
                effective_severity_threshold,
            )

        if context.severity < effective_severity_threshold:
            return DecisionOutcome(
                False,
                "severity_below_threshold",
                self._config.confidence_threshold,
                effective_severity_threshold,
            )

        if self._env_bool("POLICY_PUBLISHING_BENIGN_BLOCK_ENABLED", False):
            profile = str(context.metadata.get("profile_mode") or "").strip().lower()
            if profile in {"benign", "baseline"}:
                return DecisionOutcome(
                    False,
                    "benign_profile_blocked",
                    self._config.confidence_threshold,
                    effective_severity_threshold,
                )

        effective_threshold = self._effective_threshold(context)
        if context.confidence < effective_threshold:
            return DecisionOutcome(
                False,
                "confidence_below_threshold",
                effective_threshold,
                effective_severity_threshold,
            )

        score = self._event_score(context)
        state_key = self._state_key(context)
        with self._lock:
            st = self._state.get(state_key)
            now = time.time()
            alpha = self._env_float("POLICY_PUBLISHING_EMA_ALPHA", 0.30, 0.01, 1.0)
            if st is None:
                st = _DecisionState(ema_score=score, last_publish=False, updated_at=now)
            else:
                st.ema_score = (alpha * score) + ((1.0 - alpha) * st.ema_score)
                st.updated_at = now

            high = max(effective_threshold, self._env_float("POLICY_PUBLISHING_HYSTERESIS_HIGH", effective_threshold, 0.0, 1.0))
            low = self._env_float("POLICY_PUBLISHING_HYSTERESIS_LOW", max(0.0, high - 0.08), 0.0, high)

            if st.last_publish:
                should_publish = st.ema_score >= low
                reason = None if should_publish else "hysteresis_hold"
            else:
                should_publish = st.ema_score >= high
                reason = None if should_publish else "hysteresis_below_high"

            st.last_publish = should_publish
            self._state[state_key] = st

        return DecisionOutcome(
            should_publish,
            reason,
            effective_threshold,
            effective_severity_threshold,
        )

    def _effective_severity_threshold(self, context: "PolicyContext") -> int:
        anomaly_key = self._normalize_anomaly_type(context.anomaly_type)
        overrides = getattr(self._config, "severity_threshold_overrides", {}) or {}
        configured = overrides.get(anomaly_key)
        if configured is not None:
            return max(1, min(10, int(configured)))

        env_key = f"POLICY_PUBLISHING_SEVERITY_THRESHOLD_{anomaly_key.upper()}"
        if env_key in os.environ:
            return self._env_int(env_key, self._config.severity_threshold, 1, 10)
        return max(1, min(10, int(self._config.severity_threshold)))

    def _effective_threshold(self, context: "PolicyContext") -> float:
        anomaly_key = self._normalize_anomaly_type(context.anomaly_type)
        base = self._class_threshold(anomaly_key, self._config.confidence_threshold)

        if not self._env_bool("POLICY_PUBLISHING_DYNAMIC_ENABLED", True):
            return base

        min_samples = self._env_int("POLICY_PUBLISHING_DYNAMIC_MIN_SAMPLES", 100, 0, 1_000_000)
        sample_count = int(context.metadata.get("sample_count") or 0)
        if sample_count < min_samples:
            return base

        fpr = self._safe_float(context.metadata.get("false_positive_rate"), None)
        harmful = self._safe_float(context.metadata.get("harmful_fp_rate"), None)
        acceptance = self._safe_float(context.metadata.get("acceptance_rate"), None)

        step = self._env_float("POLICY_PUBLISHING_DYNAMIC_STEP", 0.03, 0.001, 0.5)
        fp_target = self._env_float("POLICY_PUBLISHING_TARGET_FPR", 0.10, 0.0, 1.0)
        harmful_target = self._env_float("POLICY_PUBLISHING_TARGET_HARMFUL_FP", 0.01, 0.0, 1.0)
        acc_min = self._env_float("POLICY_PUBLISHING_ACCEPTANCE_MIN", 0.60, 0.0, 1.0)
        acc_max = self._env_float("POLICY_PUBLISHING_ACCEPTANCE_MAX", 0.90, 0.0, 1.0)
        threshold = base

        if harmful is not None and harmful > harmful_target:
            threshold += step * 2.0
        if fpr is not None and fpr > fp_target:
            threshold += step
        if acceptance is not None:
            if acceptance < acc_min:
                threshold += step
            elif acceptance > acc_max:
                threshold -= step

        t_min = self._env_float("POLICY_PUBLISHING_THRESHOLD_MIN", 0.50, 0.0, 1.0)
        t_max = self._env_float("POLICY_PUBLISHING_THRESHOLD_MAX", 0.99, t_min, 1.0)
        return min(t_max, max(t_min, threshold))

    def _event_score(self, context: "PolicyContext") -> float:
        severity_score = max(0.0, min(1.0, float(context.severity) / 10.0))
        confidence = max(0.0, min(1.0, float(context.confidence)))
        sev_weight = self._env_float("POLICY_PUBLISHING_SEVERITY_WEIGHT", 0.40, 0.0, 1.0)
        conf_weight = self._env_float("POLICY_PUBLISHING_CONFIDENCE_WEIGHT", 0.60, 0.0, 1.0)
        total = sev_weight + conf_weight
        if total <= 0:
            conf_weight = 1.0
            sev_weight = 0.0
            total = 1.0
        blended = ((conf_weight * confidence) + (sev_weight * severity_score)) / total
        if self._env_bool("POLICY_PUBLISHING_SCORE_FLOOR_TO_CONFIDENCE", True):
            # Prevent contradictory suppressions where confidence has already
            # passed class thresholds but blended scoring drops below hysteresis.
            return max(blended, confidence)
        return blended

    def _class_threshold(self, anomaly_key: str, default: float) -> float:
        env_key = f"POLICY_PUBLISHING_THRESHOLD_{anomaly_key.upper()}"
        return self._env_float(env_key, default, 0.0, 1.0)

    @staticmethod
    def _normalize_anomaly_type(anomaly_type: str) -> str:
        key = (anomaly_type or "anomaly").strip().lower().replace("-", "_")
        aliases = {
            "portscan": "port_scan",
            "dos": "ddos",
            "network_intrusion": "anomaly",
        }
        return aliases.get(key, key)

    @staticmethod
    def _state_key(context: "PolicyContext") -> str:
        target = (
            context.network_context.get("src_ip")
            or context.network_context.get("source_ip")
            or context.network_context.get("dst_ip")
            or context.network_context.get("destination_ip")
            or "unknown"
        )
        return f"{PolicyDecisionEngine._normalize_anomaly_type(context.anomaly_type)}::{target}"

    @staticmethod
    def _safe_float(value, default: Optional[float]) -> Optional[float]:
        if value is None:
            return default
        try:
            return float(value)
        except (TypeError, ValueError):
            return default

    @staticmethod
    def _env_bool(name: str, default: bool) -> bool:
        value = os.getenv(name)
        if value is None:
            return default
        return value.strip().lower() in ("1", "true", "yes", "on")

    @staticmethod
    def _env_int(name: str, default: int, min_v: int, max_v: int) -> int:
        value = os.getenv(name)
        if value is None:
            return default
        try:
            parsed = int(value)
        except ValueError:
            return default
        return max(min_v, min(max_v, parsed))

    @staticmethod
    def _env_float(name: str, default: float, min_v: float, max_v: float) -> float:
        value = os.getenv(name)
        if value is None:
            return default
        try:
            parsed = float(value)
        except ValueError:
            return default
        return max(min_v, min(max_v, parsed))
