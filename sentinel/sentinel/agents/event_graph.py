"""Generic event analysis engine for non-file modalities."""

from __future__ import annotations

from typing import Any, Dict
import time

from .state import AnalysisStage
from .merge_utils import run_parallel_agents
from .coordinator import CoordinatorAgent
from .process_agent import ProcessAgent
from .rules_hit_agent import RulesHitAgent
from .scan_findings_agent import ScannerFindingsAgent
from .sequence_risk_agent import SequenceRiskAgent
from .mcp_runtime_agent import MCPRuntimeControlsAgent
from .exfil_dlp_agent import ExfilDLPAgent
from .resilience_agent import ResilienceAgent
from ..contracts import CanonicalEvent
from ..providers.base import ThreatLevel
from ..logging import get_logger

logger = get_logger(__name__)


class EventAnalysisEngine:
    """Analyze non-file CanonicalEvents (process/rules/scan findings)."""

    def __init__(self):
        self._agents = [
            ProcessAgent(),
            RulesHitAgent(),
            ScannerFindingsAgent(),
            SequenceRiskAgent(),
            MCPRuntimeControlsAgent(),
            ExfilDLPAgent(),
            ResilienceAgent(),
        ]
        self._coordinator = CoordinatorAgent()

    def analyze_event(self, event: CanonicalEvent) -> Dict[str, Any]:
        t0 = time.perf_counter()
        state: Dict[str, Any] = {
            "event": event,
            "current_stage": AnalysisStage.INIT,
            "stages_completed": [],
            "static_results": [],
            "ml_results": [],
            "llm_results": [],
            "findings": [],
            "indicators": [],
            "threat_level": ThreatLevel.UNKNOWN,
            "confidence": 0.0,
            "final_score": 0.0,
            "reasoning_steps": [],
            "final_reasoning": "",
            "errors": [],
            "analysis_time_ms": 0.0,
            "metadata": {},
        }

        updates = run_parallel_agents(state, self._agents)
        if updates:
            updates["current_stage"] = AnalysisStage.DEEP_ANALYSIS
            state.update(updates)

        state.update(self._coordinator(state))
        state["analysis_time_ms"] = (time.perf_counter() - t0) * 1000
        return state
