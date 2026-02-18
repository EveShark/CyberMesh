"""Sequence risk agent for action/toolchain events."""

from __future__ import annotations

import time
from typing import Any, Dict, List

from .base import BaseAgent
from .state import Finding, AnalysisStage
from ..contracts import Modality
from ..contracts.schemas import ActionEventV1
from ..providers.base import AnalysisResult, ThreatLevel
from ..logging import get_logger
from ..utils.error_codes import ERR_INVALID_SCHEMA, ERR_MISSING_EVENT, format_error

logger = get_logger(__name__)


class SequenceRiskAgent(BaseAgent):
    """Detects risky action chains and intent-to-action mismatches."""

    @property
    def name(self) -> str:
        return "sequence_risk_agent"

    def should_run(self, state: Dict[str, Any]) -> bool:
        event = state.get("event")
        return bool(event and getattr(event, "modality", None) == Modality.ACTION_EVENT)

    def __call__(self, state: Dict[str, Any]) -> Dict[str, Any]:
        self._log_start(state)
        t0 = time.perf_counter()

        event = state.get("event")
        if not event:
            return {
                "errors": [format_error(ERR_MISSING_EVENT, "Missing event for sequence analysis")],
                "error_codes": [ERR_MISSING_EVENT],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
            }

        try:
            features = ActionEventV1.from_dict(event.features)
        except Exception as exc:  # pylint: disable=broad-except
            return {
                "errors": [format_error(ERR_INVALID_SCHEMA, f"Invalid action event: {exc}")],
                "error_codes": [ERR_INVALID_SCHEMA],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
            }

        actions = _extract_actions(features)
        severity, reason = _score_actions(actions, features)
        threat_level = _severity_to_threat(severity)
        score = _threat_to_score(threat_level)
        confidence = 0.7 if severity in ("medium", "high", "critical") else 0.5

        findings: List[Finding] = []
        if severity in ("medium", "high", "critical"):
            findings.append(Finding(
                source=self.name,
                severity=severity,
                description=reason,
                confidence=confidence,
                metadata={
                    "event_name": features.event_name,
                    "chain_id": features.chain_id,
                    "session_id": features.session_id,
                },
            ))

        reasoning_steps = [f"Sequence risk classified as {threat_level.value} ({reason})"]

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
            "errors": [],
            "metadata": {"sequence_risk_time_ms": result.latency_ms},
        }

        self._log_complete(updates)
        return updates


def _extract_actions(features: ActionEventV1) -> List[str]:
    actions: List[str] = []
    if features.event_name:
        actions.append(features.event_name)
    if features.event_action:
        actions.append(features.event_action)
    if features.attributes:
        for key in ("sequence", "actions", "chain"):
            seq = features.attributes.get(key)
            if isinstance(seq, list):
                actions.extend([str(item) for item in seq])
    return [a.lower() for a in actions if a]


def _score_actions(actions: List[str], features: ActionEventV1) -> tuple[str, str]:
    if not actions:
        return "low", "No sequence data provided"

    # Detect common malicious chains
    has_read = any(a in actions for a in ("read", "open", "list", "access", "download"))
    has_secret = any("secret" in a or "credential" in a or "key" in a for a in actions)
    has_encode = any(a in actions for a in ("encode", "compress", "zip", "base64"))
    has_send = any(a in actions for a in ("upload", "exfil", "send", "post", "transfer"))

    if has_read and has_secret and has_encode and has_send:
        return "critical", "Sensitive data accessed, encoded, and sent outbound"
    if has_read and has_send:
        return "high", "Data read followed by outbound transfer"

    # Category-based hints
    categories = _listify(features.event_category)
    if any(c in ("exfiltration", "credential_access", "impact") for c in categories):
        return "medium", "Event category indicates elevated risk"

    if features.bytes_out and features.bytes_out > 5 * 1024 * 1024:
        return "medium", "Large outbound transfer observed in action event"

    return "low", "No high-risk chain detected"


def _listify(value: Any) -> List[str]:
    if value is None:
        return []
    if isinstance(value, list):
        return [str(v).lower() for v in value]
    return [str(value).lower()]


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
