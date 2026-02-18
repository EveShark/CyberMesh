"""Resilience agent for detection-plane health telemetry."""

from __future__ import annotations

import os
import time
from typing import Any, Dict, List

from .base import BaseAgent
from .state import Finding, AnalysisStage
from ..contracts import Modality
from ..contracts.schemas import ResilienceEventV1
from ..providers.base import AnalysisResult, ThreatLevel
from ..logging import get_logger
from ..utils.error_codes import ERR_INVALID_SCHEMA, ERR_MISSING_EVENT, format_error

logger = get_logger(__name__)


class ResilienceAgent(BaseAgent):
    """Detects degraded or failing detection-plane components."""

    @property
    def name(self) -> str:
        return "resilience_agent"

    def should_run(self, state: Dict[str, Any]) -> bool:
        event = state.get("event")
        return bool(event and getattr(event, "modality", None) == Modality.RESILIENCE_EVENT)

    def __call__(self, state: Dict[str, Any]) -> Dict[str, Any]:
        self._log_start(state)
        t0 = time.perf_counter()

        event = state.get("event")
        if not event:
            return {
                "errors": [format_error(ERR_MISSING_EVENT, "Missing event for resilience analysis")],
                "error_codes": [ERR_MISSING_EVENT],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
            }

        try:
            features = ResilienceEventV1.from_dict(event.features)
        except Exception as exc:  # pylint: disable=broad-except
            return {
                "errors": [format_error(ERR_INVALID_SCHEMA, f"Invalid resilience event: {exc}")],
                "error_codes": [ERR_INVALID_SCHEMA],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
            }

        findings: List[Finding] = []
        errors: List[str] = []

        severity = "low"
        if features.status in ("error", "failed"):
            severity = "high"
            findings.append(_finding("high", f"{features.component} reported error state"))
        elif features.status in ("degraded", "warning"):
            severity = "medium"
            findings.append(_finding("medium", f"{features.component} reported degraded state"))

        max_latency = float(os.getenv("SENTINEL_RESILIENCE_MAX_LATENCY_MS", "5000"))
        if features.latency_ms and features.latency_ms > max_latency:
            findings.append(_finding("medium", f"High latency: {features.latency_ms:.0f} ms"))

        max_backlog = int(os.getenv("SENTINEL_RESILIENCE_MAX_BACKLOG", "1000"))
        if features.backlog_depth and features.backlog_depth > max_backlog:
            findings.append(_finding("medium", f"Backlog depth high: {features.backlog_depth}"))

        max_missed = int(os.getenv("SENTINEL_RESILIENCE_MAX_MISSED_HEARTBEATS", "3"))
        if features.missed_heartbeats and features.missed_heartbeats >= max_missed:
            findings.append(_finding("medium", f"Missed heartbeats: {features.missed_heartbeats}"))

        gap_ms = _telemetry_gap_ms(features.last_seen_ts, features.observed_ts)
        max_gap_ms = float(os.getenv("SENTINEL_RESILIENCE_MAX_GAP_MS", "300000"))
        if gap_ms is not None and gap_ms > max_gap_ms:
            findings.append(_finding("medium", f"Telemetry gap detected: {int(gap_ms)} ms"))

        max_skew_ms = float(os.getenv("SENTINEL_RESILIENCE_MAX_CLOCK_SKEW_MS", "300000"))
        if gap_ms is not None and gap_ms < -max_skew_ms:
            findings.append(_finding("medium", f"Clock skew detected: {int(abs(gap_ms))} ms"))

        severity = _max_severity(findings, severity)
        threat_level = _severity_to_threat(severity)
        score = _threat_to_score(threat_level)
        confidence = 0.7 if findings else 0.5

        result = AnalysisResult(
            provider_name=self.name,
            provider_version="1.0.0",
            threat_level=threat_level,
            score=score,
            confidence=confidence,
            findings=[f.description for f in findings],
            latency_ms=(time.perf_counter() - t0) * 1000,
        )

        updates = {
            "static_results": [result],
            "findings": findings,
            "reasoning_steps": [f"Resilience classified as {threat_level.value}"],
            "stages_completed": [AnalysisStage.DEEP_ANALYSIS.value],
            "current_stage": AnalysisStage.DEEP_ANALYSIS,
            "errors": errors,
            "metadata": {"resilience_time_ms": result.latency_ms},
        }

        self._log_complete(updates)
        return updates


def _finding(severity: str, description: str) -> Finding:
    return Finding(
        source="resilience_agent",
        severity=severity,
        description=description,
        confidence=0.7 if severity in ("high", "critical") else 0.6,
    )


def _to_epoch_seconds(value: float | None) -> float | None:
    if value is None:
        return None
    try:
        numeric = float(value)
    except (TypeError, ValueError):
        return None
    if numeric > 1e12:
        return numeric / 1000.0
    return numeric


def _telemetry_gap_ms(last_seen_ts: float | None, observed_ts: float | None) -> float | None:
    last_seen = _to_epoch_seconds(last_seen_ts)
    observed = _to_epoch_seconds(observed_ts)
    if last_seen is None or observed is None:
        return None
    return (observed - last_seen) * 1000.0


def _max_severity(findings: List[Finding], default: str) -> str:
    if not findings:
        return default
    order = {"low": 0, "medium": 1, "high": 2, "critical": 3}
    return max(findings, key=lambda f: order.get(f.severity, 0)).severity


def _severity_to_threat(severity: str) -> ThreatLevel:
    mapping = {
        "low": ThreatLevel.CLEAN,
        "medium": ThreatLevel.SUSPICIOUS,
        "high": ThreatLevel.MALICIOUS,
        "critical": ThreatLevel.CRITICAL,
    }
    return mapping.get(severity, ThreatLevel.UNKNOWN)


def _threat_to_score(threat_level: ThreatLevel) -> float:
    mapping = {
        ThreatLevel.CLEAN: 0.1,
        ThreatLevel.SUSPICIOUS: 0.5,
        ThreatLevel.MALICIOUS: 0.85,
        ThreatLevel.CRITICAL: 0.95,
        ThreatLevel.UNKNOWN: 0.5,
    }
    return mapping.get(threat_level, 0.5)
