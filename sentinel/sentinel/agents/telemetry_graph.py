"""Telemetry analysis graph for NetworkFlowFeaturesV1."""

from __future__ import annotations

import os
import operator
from typing import Any, Dict, List, Optional, Annotated, TypedDict

from langgraph.graph import StateGraph, END

from .state import AnalysisStage, Finding, merge_dicts
from .flow_agent import FlowAgent
from .telemetry_agent import TelemetryThreatIntelAgent
from .merge_utils import run_parallel_agents, run_sequential_agents
from .coordinator import CoordinatorAgent
from ..contracts.schemas import NetworkFlowFeaturesV1
from ..providers.base import ThreatLevel, AnalysisResult
from ..logging import get_logger

logger = get_logger(__name__)


def _env_true(name: str) -> bool:
    value = os.getenv(name, "").strip().lower()
    return value in ("1", "true", "yes", "on")


class TelemetryState(TypedDict, total=False):
    """Telemetry graph state."""
    flow_features: Optional[NetworkFlowFeaturesV1]
    raw_context: Dict[str, Any]

    current_stage: AnalysisStage
    stages_completed: Annotated[List[str], operator.add]

    static_results: Annotated[List[AnalysisResult], operator.add]
    ml_results: Annotated[List[AnalysisResult], operator.add]
    llm_results: Annotated[List[AnalysisResult], operator.add]

    findings: Annotated[List[Finding], operator.add]
    indicators: Annotated[List[Dict[str, str]], operator.add]

    threat_level: ThreatLevel
    confidence: float
    final_score: float
    reasoning_steps: Annotated[List[str], operator.add]
    final_reasoning: str

    errors: Annotated[List[str], operator.add]
    metadata: Annotated[Dict[str, Any], merge_dicts]


def create_telemetry_graph(
    enable_threat_intel: bool = True,
    sequential: bool = False,
) -> StateGraph:
    """Create telemetry analysis graph."""
    rules_agent = FlowAgent()
    intel_agent = TelemetryThreatIntelAgent() if enable_threat_intel else None
    coordinator = CoordinatorAgent()

    workflow = StateGraph(TelemetryState)

    def validate_flow_node(state: TelemetryState) -> Dict[str, Any]:
        features = state.get("flow_features")
        if not features:
            return {
                "errors": ["No flow features provided"],
                "current_stage": AnalysisStage.INIT,
            }

        missing = []
        for field in NetworkFlowFeaturesV1.required_fields():
            if getattr(features, field, None) is None:
                missing.append(field)

        if missing:
            return {
                "errors": [f"Missing required fields: {', '.join(missing)}"],
                "current_stage": AnalysisStage.INIT,
            }

        return {
            "current_stage": AnalysisStage.INIT,
            "stages_completed": [AnalysisStage.INIT.value],
        }

    def parallel_core_node(state: TelemetryState) -> Dict[str, Any]:
        agents = [rules_agent]
        if intel_agent:
            agents.append(intel_agent)
        if sequential:
            updates = run_sequential_agents(state, agents)
        else:
            updates = run_parallel_agents(state, agents)
        if updates:
            updates["current_stage"] = AnalysisStage.DEEP_ANALYSIS
        return updates

    workflow.add_node("validate", validate_flow_node)
    workflow.add_node("parallel_core", parallel_core_node)
    workflow.add_node("coordinator", coordinator)

    workflow.set_entry_point("validate")
    workflow.add_edge("validate", "parallel_core")
    workflow.add_edge("parallel_core", "coordinator")
    workflow.add_edge("coordinator", END)

    return workflow.compile()


class TelemetryAnalysisEngine:
    """High-level interface for running telemetry analysis."""

    def __init__(self, enable_threat_intel: bool = True, sequential: bool = False):
        sequential = bool(sequential or _env_true("SENTINEL_SEQUENTIAL"))
        self.graph = create_telemetry_graph(
            enable_threat_intel=enable_threat_intel,
            sequential=sequential,
        )

    def analyze_flow(
        self,
        features: NetworkFlowFeaturesV1,
        raw_context: Optional[Dict[str, Any]] = None,
    ) -> TelemetryState:
        initial_state: TelemetryState = {
            "flow_features": features,
            "raw_context": raw_context or {},
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
            "metadata": {},
        }

        return self.graph.invoke(initial_state)
