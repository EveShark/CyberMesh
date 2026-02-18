"""Modality router for file and telemetry analysis graphs."""

from __future__ import annotations

import os
import threading
from typing import Any, Dict, Optional

from .graph import AnalysisEngine
from .telemetry_graph import TelemetryAnalysisEngine
from .event_graph import EventAnalysisEngine
from .state import AnalysisStage
from ..contracts import CanonicalEvent, Modality
from ..contracts.schemas import NetworkFlowFeaturesV1
from ..opa import OPAGate
from ..providers.base import ThreatLevel
from ..utils.attestation import verify_event


class SentinelOrchestrator:
    """Routes events to the correct analysis graph."""

    def __init__(
        self,
        enable_llm: bool = True,
        enable_fast_path: bool = True,
        enable_threat_intel: bool = True,
        models_path: Optional[str] = None,
        use_external_rules: bool = True,
        sequential: bool = False,
        enable_opa_gate: bool = True,
    ):
        # Lazy-init the file engine (YARA/signature compilation can be expensive and is
        # unnecessary for non-FILE modalities like OSS adapter findings or Phase-3 events).
        self._file_engine: Optional[AnalysisEngine] = None
        self._file_engine_lock = threading.Lock()
        self._file_engine_args: Dict[str, Any] = {
            "models_path": models_path,
            "enable_llm": enable_llm,
            "enable_fast_path": enable_fast_path,
            "enable_threat_intel": enable_threat_intel,
            "use_external_rules": use_external_rules,
            "sequential": sequential,
        }
        self.telemetry_engine = TelemetryAnalysisEngine(
            enable_threat_intel=enable_threat_intel,
            sequential=sequential,
        )
        self.event_engine = EventAnalysisEngine()
        self.opa_gate = OPAGate(enabled=enable_opa_gate)

    def _get_file_engine(self) -> AnalysisEngine:
        engine = self._file_engine
        if engine is not None:
            return engine
        with self._file_engine_lock:
            if self._file_engine is None:
                self._file_engine = AnalysisEngine(**self._file_engine_args)
            return self._file_engine

    def analyze_file(self, file_path: str, skip_fast_path: bool = False) -> Dict[str, Any]:
        result = self._get_file_engine().analyze(file_path, skip_fast_path=skip_fast_path)
        return self._apply_opa_gate(result, None)

    def analyze_flow(
        self,
        features: NetworkFlowFeaturesV1,
        raw_context: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        return self.telemetry_engine.analyze_flow(features, raw_context=raw_context)

    def analyze_event(self, event: CanonicalEvent) -> Dict[str, Any]:
        if event.modality == Modality.FILE:
            file_path = event.raw_context.get("file_path")
            if not file_path:
                raise ValueError("FILE event requires raw_context.file_path")
            return self.analyze_file(file_path)

        attestation_error = self._check_attestation(event)
        if attestation_error:
            return attestation_error

        if event.modality == Modality.NETWORK_FLOW:
            features = NetworkFlowFeaturesV1.from_dict(event.features)
            result = self.analyze_flow(features, raw_context=event.raw_context)
            return self._apply_opa_gate(result, event)

        if event.modality in (
            Modality.PROCESS_EVENT,
            Modality.SCAN_FINDINGS,
            Modality.ACTION_EVENT,
            Modality.MCP_RUNTIME,
            Modality.EXFIL_EVENT,
            Modality.RESILIENCE_EVENT,
        ):
            result = self.event_engine.analyze_event(event)
            return self._apply_opa_gate(result, event)

        raise ValueError(f"Unsupported modality: {event.modality.value}")

    def _check_attestation(self, event: CanonicalEvent) -> Optional[Dict[str, Any]]:
        key = os.getenv("SENTINEL_ADAPTER_HMAC_KEY")
        required = os.getenv("SENTINEL_ADAPTER_HMAC_REQUIRED", "").strip().lower() in (
            "1", "true", "yes", "on"
        )
        attestation = None
        is_oss_adapter = False
        if isinstance(event.raw_context, dict):
            attestation = event.raw_context.get("_attestation")
            is_oss_adapter = event.raw_context.get("_adapter") == "oss_adapter"

        should_enforce = required or (is_oss_adapter and bool(key))
        if not attestation:
            if should_enforce:
                return self._attestation_error_state(event, "attestation_missing", "attestation missing")
            return None

        if not key:
            return self._attestation_error_state(
                event,
                "attestation_key_missing",
                "attestation present but key missing",
            )

        ok, reason = verify_event(event, key, attestation)
        if not ok:
            return self._attestation_error_state(event, reason, "attestation invalid")
        return None

    def _attestation_error_state(
        self,
        event: CanonicalEvent,
        reason: str,
        detail: str,
    ) -> Dict[str, Any]:
        return {
            "threat_level": ThreatLevel.UNKNOWN,
            "final_score": 0.0,
            "confidence": 0.0,
            "findings": [],
            "indicators": [],
            "reasoning_steps": [f"Attestation failed: {reason}"],
            "final_reasoning": "Adapter output rejected due to attestation failure.",
            "errors": [f"Attestation failure: {detail}"],
            "analysis_time_ms": 0.0,
            "stages_completed": [AnalysisStage.INIT.value],
            "current_stage": AnalysisStage.INIT,
            "metadata": {
                "attestation": {
                    "status": "error",
                    "reason": reason,
                    "detail": detail,
                    "event_id": event.id,
                    "source": event.source,
                },
                "degraded": True,
                "degraded_reasons": [reason],
            },
        }

    def _apply_opa_gate(self, result: Dict[str, Any], event: Optional[CanonicalEvent]) -> Dict[str, Any]:
        if not isinstance(result, dict):
            return result

        metadata = result.setdefault("metadata", {})
        if event and isinstance(event.raw_context, dict):
            sanitization = event.raw_context.get("_sanitization")
            if sanitization:
                metadata.setdefault("sanitization", sanitization)

        input_data: Dict[str, Any] = {
            "analysis": {
                "threat_level": str(result.get("threat_level", "")),
                "final_score": result.get("final_score", 0.0),
                "confidence": result.get("confidence", 0.0),
                "findings_count": len(result.get("findings", []) or []),
                "indicators_count": len(result.get("indicators", []) or []),
                "errors": result.get("errors", []),
            }
        }
        if event:
            input_data["event"] = {
                "id": event.id,
                "timestamp": event.timestamp,
                "tenant_id": event.tenant_id,
                "source": event.source,
                "modality": event.modality.value,
                "features_version": event.features_version,
            }

        decision = self.opa_gate.evaluate(input_data)
        metadata["opa"] = decision.to_dict()

        if decision.status == "error":
            errors = result.setdefault("errors", [])
            errors.append(f"OPA gate error: {decision.error}")

        if decision.status == "deny":
            steps = result.setdefault("reasoning_steps", [])
            steps.append(f"OPA policy would deny (monitor-only): {decision.reason}")

        return result
