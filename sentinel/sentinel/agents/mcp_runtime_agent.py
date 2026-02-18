"""MCP runtime controls agent for tool-call telemetry."""

from __future__ import annotations

import json
import os
import re
import time
from typing import Any, Dict, List

from .base import BaseAgent
from .state import Finding, AnalysisStage
from ..contracts import Modality
from ..contracts.schemas import MCPRuntimeEventV1
from ..providers.base import AnalysisResult, ThreatLevel
from ..logging import get_logger
from ..utils.error_codes import ERR_INVALID_SCHEMA, ERR_MISSING_EVENT, format_error

logger = get_logger(__name__)

_BASE64_RE = re.compile(r"^[A-Za-z0-9+/=]{200,}$")


class MCPRuntimeControlsAgent(BaseAgent):
    """Detects MCP runtime anomalies (schema changes, large args, prompt injection)."""

    @property
    def name(self) -> str:
        return "mcp_runtime_controls_agent"

    def should_run(self, state: Dict[str, Any]) -> bool:
        event = state.get("event")
        return bool(event and getattr(event, "modality", None) == Modality.MCP_RUNTIME)

    def __call__(self, state: Dict[str, Any]) -> Dict[str, Any]:
        self._log_start(state)
        t0 = time.perf_counter()

        event = state.get("event")
        if not event:
            return {
                "errors": [format_error(ERR_MISSING_EVENT, "Missing event for MCP runtime analysis")],
                "error_codes": [ERR_MISSING_EVENT],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
            }

        try:
            features = MCPRuntimeEventV1.from_dict(event.features)
        except Exception as exc:  # pylint: disable=broad-except
            return {
                "errors": [format_error(ERR_INVALID_SCHEMA, f"Invalid MCP runtime event: {exc}")],
                "error_codes": [ERR_INVALID_SCHEMA],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
            }

        findings: List[Finding] = []
        errors: List[str] = []

        attrs = features.attributes or {}
        args = features.arguments or {}

        allowed_servers = _env_set("SENTINEL_MCP_ALLOWED_SERVERS")
        if allowed_servers and features.server_id and features.server_id not in allowed_servers:
            findings.append(Finding(
                source=self.name,
                severity="high",
                description=f"Unexpected MCP server: {features.server_id}",
                confidence=0.8,
            ))

        if features.tool_name and not features.tool_schema_hash:
            findings.append(Finding(
                source=self.name,
                severity="medium",
                description="Tool schema hash missing",
                confidence=0.6,
            ))

        max_args = int(os.getenv("SENTINEL_MCP_MAX_ARG_BYTES", "8192"))
        arg_size = _estimate_arg_size(features, args)
        if arg_size and arg_size > max_args:
            findings.append(Finding(
                source=self.name,
                severity="medium",
                description=f"Large MCP arguments ({arg_size} bytes)",
                confidence=0.7,
                metadata={"arguments_size_bytes": arg_size},
            ))

        if _has_encoded_payload(args):
            findings.append(Finding(
                source=self.name,
                severity="medium",
                description="Encoded or base64-like payload detected in arguments",
                confidence=0.6,
            ))

        if _has_prompt_injection(attrs):
            findings.append(Finding(
                source=self.name,
                severity="high",
                description="Prompt injection content detected in tool output",
                confidence=0.8,
            ))

        if attrs.get("tool_schema_changed") is True:
            findings.append(Finding(
                source=self.name,
                severity="high",
                description="Tool schema changed at runtime",
                confidence=0.8,
            ))

        threat_level = _severity_to_threat(_max_severity(findings))
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
            "reasoning_steps": [f"MCP runtime classified as {threat_level.value}"],
            "stages_completed": [AnalysisStage.DEEP_ANALYSIS.value],
            "current_stage": AnalysisStage.DEEP_ANALYSIS,
            "errors": errors,
            "metadata": {"mcp_runtime_time_ms": result.latency_ms},
        }

        self._log_complete(updates)
        return updates


def _env_set(name: str) -> List[str]:
    raw = os.getenv(name, "")
    return [item.strip() for item in raw.split(",") if item.strip()]


def _estimate_arg_size(features: MCPRuntimeEventV1, args: Dict[str, Any]) -> int:
    if features.arguments_size_bytes:
        return int(features.arguments_size_bytes)
    try:
        return len(json.dumps(args).encode("utf-8"))
    except Exception:  # pylint: disable=broad-except
        return 0


def _has_encoded_payload(args: Dict[str, Any]) -> bool:
    if not args:
        return False
    for value in args.values():
        if isinstance(value, str) and _BASE64_RE.match(value):
            return True
    return False


def _has_prompt_injection(attrs: Dict[str, Any]) -> bool:
    for key in ("output_preview", "output_text", "result_text"):
        value = attrs.get(key)
        if isinstance(value, str):
            text = value.lower()
            if "ignore previous" in text or "system prompt" in text or "developer message" in text:
                return True
    return False


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
