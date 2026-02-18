"""Rules hit agent for Sigma/BZAR findings."""

import time
from typing import Any, Dict, List

from .base import BaseAgent
from .state import Finding, AnalysisStage
from ..contracts import Modality
from ..contracts.schemas import ScanFindingsV1
from ..providers.base import AnalysisResult, ThreatLevel
from ..logging import get_logger

logger = get_logger(__name__)


class RulesHitAgent(BaseAgent):
    """Agent for SCAN_FINDINGS modality when tool is Sigma/BZAR."""

    @property
    def name(self) -> str:
        return "rules_hit_agent"

    def should_run(self, state: Dict[str, Any]) -> bool:
        event = state.get("event")
        if not event or event.modality != Modality.SCAN_FINDINGS:
            return False
        tool = str(event.features.get("tool", "")).lower()
        return tool in ("sigma", "bzar")

    def __call__(self, state: Dict[str, Any]) -> Dict[str, Any]:
        self._log_start(state)
        t0 = time.perf_counter()

        event = state.get("event")
        if not event:
            return {"errors": ["Missing event for rules hit analysis"], "current_stage": AnalysisStage.DEEP_ANALYSIS}

        try:
            features = ScanFindingsV1.from_dict(event.features)
        except Exception as exc:  # pylint: disable=broad-except
            return {"errors": [f"Invalid rules findings: {exc}"], "current_stage": AnalysisStage.DEEP_ANALYSIS}

        findings: List[Finding] = []
        errors: List[str] = []
        max_sev = "low"
        for item in features.findings:
            sev = (item.severity or "medium").lower()
            max_sev = _max_severity(max_sev, sev)
            findings.append(Finding(
                source=features.tool or self.name,
                severity=sev,
                description=item.description or item.rule_name,
                confidence=0.7,
                metadata={"rule_id": item.rule_id},
            ))

        threat_level = _severity_to_threat(max_sev)
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
            "reasoning_steps": [f"Rule hits: {len(findings)} findings"],
            "stages_completed": [AnalysisStage.DEEP_ANALYSIS.value],
            "current_stage": AnalysisStage.DEEP_ANALYSIS,
            "errors": errors,
            "metadata": {"rules_hit_time_ms": result.latency_ms},
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


def _max_severity(left: str, right: str) -> str:
    order = {"low": 0, "medium": 1, "high": 2, "critical": 3}
    return left if order.get(left, 0) >= order.get(right, 0) else right


def _threat_to_score(threat_level: ThreatLevel) -> float:
    mapping = {
        ThreatLevel.CLEAN: 0.1,
        ThreatLevel.SUSPICIOUS: 0.5,
        ThreatLevel.MALICIOUS: 0.85,
        ThreatLevel.CRITICAL: 0.95,
        ThreatLevel.UNKNOWN: 0.5,
    }
    return mapping.get(threat_level, 0.5)
