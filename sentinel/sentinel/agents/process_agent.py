"""Process event analysis agent (Falco and similar sources)."""

import time
from typing import Any, Dict, List

from .base import BaseAgent
from .state import Finding, AnalysisStage
from ..contracts import Modality
from ..contracts.schemas import ProcessEventV1
from ..providers.base import AnalysisResult, ThreatLevel
from ..logging import get_logger

logger = get_logger(__name__)


class ProcessAgent(BaseAgent):
    """Agent for PROCESS_EVENT modality."""

    @property
    def name(self) -> str:
        return "process_agent"

    def should_run(self, state: Dict[str, Any]) -> bool:
        event = state.get("event")
        return bool(event and getattr(event, "modality", None) == Modality.PROCESS_EVENT)

    def __call__(self, state: Dict[str, Any]) -> Dict[str, Any]:
        self._log_start(state)
        t0 = time.perf_counter()

        event = state.get("event")
        if not event:
            return {"errors": ["Missing event for process analysis"], "current_stage": AnalysisStage.DEEP_ANALYSIS}

        try:
            features = ProcessEventV1.from_dict(event.features)
        except Exception as exc:  # pylint: disable=broad-except
            return {"errors": [f"Invalid process features: {exc}"], "current_stage": AnalysisStage.DEEP_ANALYSIS}

        severity = (features.severity or "").lower()
        threat_level = _severity_to_threat(severity)
        score = _threat_to_score(threat_level)
        confidence = 0.7 if severity else 0.5

        findings: List[Finding] = []
        errors: List[str] = []
        description = f"Process event: {features.event_type} - {features.process_name}"
        if features.rule:
            description = f"Rule triggered: {features.rule} ({features.process_name})"
        findings.append(Finding(
            source=self.name,
            severity=severity or "medium",
            description=description,
            confidence=confidence,
        ))

        reasoning_steps = [f"Process event {features.event_type} classified as {threat_level.value}"]

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
            "reasoning_steps": reasoning_steps,
            "stages_completed": [AnalysisStage.DEEP_ANALYSIS.value],
            "current_stage": AnalysisStage.DEEP_ANALYSIS,
            "errors": errors,
            "metadata": {"process_analysis_time_ms": result.latency_ms},
        }

        self._log_complete(updates)
        return updates


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
