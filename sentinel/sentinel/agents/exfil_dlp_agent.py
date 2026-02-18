"""Exfiltration / DLP agent for data transfer events."""

from __future__ import annotations

import os
import time
from typing import Any, Dict, List

from .base import BaseAgent
from .state import Finding, AnalysisStage
from ..contracts import Modality
from ..contracts.schemas import ExfilEventV1
from ..providers.base import AnalysisResult, ThreatLevel
from ..logging import get_logger
from ..utils.error_codes import ERR_INVALID_SCHEMA, ERR_MISSING_EVENT, format_error

logger = get_logger(__name__)


class ExfilDLPAgent(BaseAgent):
    """Detects potential data exfiltration or DLP violations."""

    @property
    def name(self) -> str:
        return "exfil_dlp_agent"

    def should_run(self, state: Dict[str, Any]) -> bool:
        event = state.get("event")
        return bool(event and getattr(event, "modality", None) == Modality.EXFIL_EVENT)

    def __call__(self, state: Dict[str, Any]) -> Dict[str, Any]:
        self._log_start(state)
        t0 = time.perf_counter()

        event = state.get("event")
        if not event:
            return {
                "errors": [format_error(ERR_MISSING_EVENT, "Missing event for exfil analysis")],
                "error_codes": [ERR_MISSING_EVENT],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
            }

        try:
            features = ExfilEventV1.from_dict(event.features)
        except Exception as exc:  # pylint: disable=broad-except
            return {
                "errors": [format_error(ERR_INVALID_SCHEMA, f"Invalid exfil event: {exc}")],
                "error_codes": [ERR_INVALID_SCHEMA],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
            }

        findings: List[Finding] = []
        errors: List[str] = []

        warn_bytes = int(os.getenv("SENTINEL_EXFIL_WARN_BYTES", str(5 * 1024 * 1024)))
        high_bytes = int(os.getenv("SENTINEL_EXFIL_HIGH_BYTES", str(50 * 1024 * 1024)))

        if features.bytes_out >= high_bytes:
            findings.append(_finding("high", f"Large outbound transfer ({features.bytes_out} bytes)"))
        elif features.bytes_out >= warn_bytes:
            findings.append(_finding("medium", f"Notable outbound transfer ({features.bytes_out} bytes)"))

        chunk_threshold = int(os.getenv("SENTINEL_EXFIL_CHUNK_COUNT", "50"))
        if features.object_count and features.object_count >= chunk_threshold:
            findings.append(_finding(
                "medium",
                f"High object count suggests chunked transfer ({features.object_count})",
            ))

        sensitive = _has_sensitive_classification(features.data_classification)
        if sensitive:
            findings.append(_finding("high", "Sensitive data classification in outbound transfer"))

        allowed_domains = _env_set("SENTINEL_EXFIL_ALLOWED_DOMAINS")
        dest_domain = (features.dst_domain or features.destination or "").lower()
        if allowed_domains and dest_domain and dest_domain not in allowed_domains:
            findings.append(_finding("medium", f"Destination not in allowlist: {dest_domain}"))

        if features.encoding or features.compressed:
            findings.append(_finding("medium", "Encoded or compressed data transfer"))

        severity = _max_severity(findings)
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
            "reasoning_steps": [f"Exfil/DLP classified as {threat_level.value}"],
            "stages_completed": [AnalysisStage.DEEP_ANALYSIS.value],
            "current_stage": AnalysisStage.DEEP_ANALYSIS,
            "errors": errors,
            "metadata": {"exfil_dlp_time_ms": result.latency_ms},
        }

        self._log_complete(updates)
        return updates


def _finding(severity: str, description: str) -> Finding:
    return Finding(
        source="exfil_dlp_agent",
        severity=severity,
        description=description,
        confidence=0.7 if severity in ("high", "critical") else 0.6,
    )


def _env_set(name: str) -> List[str]:
    raw = os.getenv(name, "")
    return [item.strip().lower() for item in raw.split(",") if item.strip()]


def _has_sensitive_classification(labels: List[str]) -> bool:
    if not labels:
        return False
    sensitive = {"secret", "confidential", "pii", "credential", "key"}
    return any(label.lower() in sensitive for label in labels)


def _max_severity(findings: List[Finding]) -> str:
    if not findings:
        return "low"
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
