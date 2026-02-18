"""LangGraph workflow for threat analysis."""

import os
import time
from concurrent.futures import ThreadPoolExecutor, TimeoutError
from pathlib import Path
from typing import Any, Dict, Optional

from langgraph.graph import StateGraph, END

from .state import GraphState, AnalysisStage
from .static_agent import StaticAnalysisAgent
from .ml_agent import MLAnalysisAgent
from .malware_agent import MalwareAgent
from .script_agent import ScriptAgent
from .llm_agent import LLMReasoningAgent
from .threat_intel_agent import ThreatIntelAgent
from .coordinator import CoordinatorAgent
from .merge_utils import run_parallel_agents, run_sequential_agents
from .degraded import build_degraded_metadata, merge_degraded_reasons
from ..parsers import get_parser_for_file, detect_file_type
from ..providers.base import ThreatLevel
from ..logging import get_logger
from ..utils.timeouts import resolve_timeout_seconds

logger = get_logger(__name__)


def _env_true(name: str) -> bool:
    value = os.getenv(name, "").strip().lower()
    return value in ("1", "true", "yes", "on")


def create_analysis_graph(
    models_path: Optional[str] = None,
    llm_api_key: Optional[str] = None,
    enable_llm: bool = True,
    enable_threat_intel: bool = True,
    use_external_rules: bool = True,
    sequential: bool = False,
) -> StateGraph:
    """
    Create the LangGraph analysis workflow.
    
    Graph structure (hybrid):
    
        [parse] -> [parallel_core] -> [should_llm?] -> [coordinator] -> END
    
    parallel_core runs independent agents concurrently and merges results.
    
    Args:
        models_path: Path to ML models
        llm_api_key: API key for LLM provider
        enable_llm: Whether to enable LLM reasoning
        enable_threat_intel: Whether to enable threat intel enrichment
        
    Returns:
        Compiled StateGraph
    """
    # Initialize agents
    static_agent = StaticAnalysisAgent()
    ml_agent = MLAnalysisAgent(models_path=models_path)
    malware_agent = MalwareAgent(models_path=models_path, use_external_rules=use_external_rules)
    script_agent = ScriptAgent()
    llm_agent = LLMReasoningAgent(api_key=llm_api_key) if enable_llm else None
    threat_intel_agent = ThreatIntelAgent() if enable_threat_intel else None
    coordinator = CoordinatorAgent()
    
    # Create graph
    workflow = StateGraph(GraphState)
    
    # Add nodes
    workflow.add_node("parse", parse_file_node)

    def parallel_core_node(state: GraphState) -> Dict[str, Any]:
        """Run core analysis agents and merge their outputs."""
        agents = [static_agent, script_agent]
        if threat_intel_agent:
            agents.append(threat_intel_agent)

        # Prefer MalwareAgent over MLAnalysisAgent to avoid duplicate ML results
        if malware_agent and malware_agent.should_run(state):
            agents.append(malware_agent)
        else:
            agents.append(ml_agent)

        if sequential:
            updates = run_sequential_agents(state, agents)
        else:
            updates = run_parallel_agents(state, agents)
        ml_available = bool(getattr(malware_agent, "providers", {})) or bool(
            getattr(ml_agent, "providers", {})
        )
        if not ml_available:
            state_reasons = state.get("metadata", {}).get("degraded_reasons", [])
            update_reasons = updates.get("metadata", {}).get("degraded_reasons", [])
            reasons = merge_degraded_reasons(
                merge_degraded_reasons(state_reasons, update_reasons),
                ["ml_models_unavailable"],
            )
            updates.setdefault("metadata", {})
            updates["metadata"]["degraded"] = True
            updates["metadata"]["degraded_reasons"] = reasons
        if updates:
            updates["current_stage"] = AnalysisStage.DEEP_ANALYSIS
        return updates

    workflow.add_node("parallel_core", parallel_core_node)
    if llm_agent:
        workflow.add_node("llm", llm_agent)
    workflow.add_node("coordinator", coordinator)
    
    # Define edges
    workflow.set_entry_point("parse")
    workflow.add_edge("parse", "parallel_core")

    # Conditional edge after parallel core
    if llm_agent:
        def should_run_llm(state: GraphState) -> str:
            """Determine if LLM reasoning should run."""
            if state.get("needs_llm_reasoning") and llm_agent.should_run(state):
                return "llm"
            return "coordinator"

        workflow.add_conditional_edges(
            "parallel_core",
            should_run_llm,
            {
                "llm": "llm",
                "coordinator": "coordinator",
            }
        )
        workflow.add_edge("llm", "coordinator")
    else:
        workflow.add_edge("parallel_core", "coordinator")
    
    workflow.add_edge("coordinator", END)
    
    return workflow.compile()


def parse_file_node(state: GraphState) -> Dict[str, Any]:
    """Parse file node - entry point of the graph."""
    file_path = state.get("file_path", "")
    
    if not file_path:
        return {
            "errors": ["No file path provided"],
            "current_stage": AnalysisStage.INIT,
        }
    
    try:
        parser = get_parser_for_file(file_path)
        if not parser:
            file_type = detect_file_type(file_path)
            return {
                "errors": [f"No parser available for file type: {file_type.value}"],
                "current_stage": AnalysisStage.INIT,
            }
        
        timeout = resolve_timeout_seconds()
        future = None
        if timeout:
            with ThreadPoolExecutor(max_workers=1) as executor:
                future = executor.submit(parser.parse, file_path)
                parsed_file = future.result(timeout=timeout)
        else:
            parsed_file = parser.parse(file_path)
        logger.info(f"Parsed {parsed_file.file_name} ({parsed_file.file_type.value})")

        # If parser reports "unsupported file type", surface degraded deterministically.
        if getattr(parsed_file, "errors", None):
            for err in list(parsed_file.errors):
                if isinstance(err, str) and err.startswith("ERR_UNSUPPORTED_FILETYPE:"):
                    return {
                        "parsed_file": parsed_file,
                        "current_stage": AnalysisStage.INIT,
                        "stages_completed": [AnalysisStage.INIT.value],
                        "errors": list(parsed_file.errors),
                        # This is a hard capability gap; mark degraded explicitly.
                        "metadata": {
                            "degraded": True,
                            "degraded_reasons": ["unsupported_filetype"],
                        },
                    }
        
        return {
            "parsed_file": parsed_file,
            "current_stage": AnalysisStage.INIT,
            "stages_completed": [AnalysisStage.INIT.value],
        }
        
    except TimeoutError:
        if future:
            future.cancel()
        return {
            "errors": [f"Parsing timed out after {timeout:.1f}s"],
            "current_stage": AnalysisStage.INIT,
        }
    except Exception as e:
        logger.error(f"File parsing failed: {e}")
        return {
            "errors": [f"Parsing error: {str(e)}"],
            "current_stage": AnalysisStage.INIT,
        }


class AnalysisEngine:
    """
    High-level interface for running file analysis.
    
    Wraps the LangGraph workflow for easy use.
    Supports fast path for quick triage.
    """
    
    def __init__(
        self,
        models_path: Optional[str] = None,
        llm_api_key: Optional[str] = None,
        enable_llm: bool = True,
        enable_fast_path: bool = True,
        enable_threat_intel: bool = True,
        use_external_rules: bool = True,
        sequential: bool = False,
    ):
        sequential = bool(sequential or _env_true("SENTINEL_SEQUENTIAL"))
        self.models_path = Path(models_path) if models_path else Path("../CyberMesh/ai-service/data/models")
        self._models_path_exists = self.models_path.exists()
        if not self._models_path_exists:
            logger.warning(
                f"Models path not available; ML providers disabled: {self.models_path}"
            )
        self.graph = create_analysis_graph(
            models_path=str(self.models_path),
            llm_api_key=llm_api_key,
            enable_llm=enable_llm,
            enable_threat_intel=enable_threat_intel,
            use_external_rules=use_external_rules,
            sequential=sequential,
        )
        self.enable_fast_path = enable_fast_path
        self.fast_path_router = None
        self._degraded_reasons = []
        
        if enable_fast_path:
            try:
                from ..fastpath import FastPathRouter
                from ..config import load_settings
                
                # Try to load hash database from default location
                settings = load_settings()
                malware_hashes_path = None
                if settings.fast_path.malware_hashes_path:
                    malware_hashes_path = str(settings.fast_path.malware_hashes_path)
                else:
                    # Fallback to default path
                    default_path = Path("B:/sentinel/data/malware_hashes.json")
                    if default_path.exists():
                        malware_hashes_path = str(default_path)
                
                self.fast_path_router = FastPathRouter(
                    malware_hashes_path=malware_hashes_path,
                    use_external_rules=use_external_rules,
                )
                logger.info(f"Fast path enabled: {self.fast_path_router.stats}")
            except Exception as e:
                logger.warning(f"Could not initialize fast path: {e}")
        self._degraded_reasons = self._compute_fast_path_degraded()
        if not self._models_path_exists:
            self._degraded_reasons = merge_degraded_reasons(
                self._degraded_reasons, ["models_path_missing"]
            )

    def _compute_fast_path_degraded(self) -> list:
        """Compute degraded reasons from fast-path availability."""
        reasons = []
        if not self.enable_fast_path:
            return reasons

        if not self.fast_path_router:
            return ["fast_path_unavailable"]

        stats = self.fast_path_router.stats
        if stats.get("yara_enabled") and not stats.get("yara_available"):
            reasons.append("yara_unavailable")
        if stats.get("hash_lookup_enabled") and not stats.get("hash_db_stats"):
            reasons.append("hash_db_unavailable")
        if stats.get("signatures_enabled") and stats.get("signature_count", 0) == 0:
            reasons.append("signatures_unavailable")
        return reasons
    
    def analyze(self, file_path: str, skip_fast_path: bool = False) -> GraphState:
        """
        Analyze a file using the agentic workflow.
        
        Args:
            file_path: Path to file to analyze
            skip_fast_path: Skip fast path evaluation
            
        Returns:
            Final GraphState with analysis results
        """
        t0 = time.perf_counter()
        degraded_base = list(self._degraded_reasons)
        
        # Fast path evaluation
        fast_path_result = None
        if self.enable_fast_path and self.fast_path_router and not skip_fast_path:
            fast_path_result = self.fast_path_router.evaluate_file(file_path)
            logger.info(
                f"Fast path: {fast_path_result.verdict.value} "
                f"({fast_path_result.latency_ms:.1f}ms)"
            )
            
            # If fast path gives definitive answer, skip full analysis
            if fast_path_result.skip_full_analysis:
                from .state import Finding
                
                findings = []
                indicators = []
                reasoning_steps = [f"Fast path: {fast_path_result.reasoning}"]
                
                # Add YARA matches as findings
                for ym in fast_path_result.yara_matches:
                    findings.append(Finding(
                        source="yara",
                        severity=ym.severity,
                        description=f"YARA: {ym.rule_name} - {ym.description}",
                        confidence=0.9,
                    ))
                
                # Add signature matches
                for sm in fast_path_result.signature_matches:
                    findings.append(Finding(
                        source="signature",
                        severity=sm.severity,
                        description=f"{sm.name}: {sm.description}",
                        confidence=sm.confidence,
                    ))
                
                total_time = (time.perf_counter() - t0) * 1000
                
                return {
                    "file_path": file_path,
                    "parsed_file": None,
                    "current_stage": AnalysisStage.COMPLETE,
                    "stages_completed": ["fast_path"],
                    "static_results": [],
                    "ml_results": [],
                    "llm_results": [],
                    "findings": findings,
                    "indicators": indicators,
                    "threat_level": fast_path_result.threat_level,
                    "confidence": fast_path_result.confidence,
                    "final_score": fast_path_result.confidence,
                    "reasoning_steps": reasoning_steps,
                    "final_reasoning": fast_path_result.reasoning,
                    "needs_deep_analysis": False,
                    "needs_llm_reasoning": False,
                    "errors": [],
                    "analysis_time_ms": total_time,
                    "metadata": {
                        "fast_path": True,
                        "fast_path_result": fast_path_result.to_dict(),
                        "models_path_exists": self._models_path_exists,
                        "models_path": str(self.models_path),
                        **build_degraded_metadata(degraded_base),
                    },
                }
        
        initial_state: GraphState = {
            "file_path": file_path,
            "parsed_file": None,
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
            "needs_deep_analysis": False,
            "needs_llm_reasoning": False,
            "errors": [],
            "analysis_time_ms": 0.0,
            "metadata": {
                **build_degraded_metadata(degraded_base),
                "models_path_exists": self._models_path_exists,
                "models_path": str(self.models_path),
            },
        }
        
        # Run the graph
        final_state = self.graph.invoke(initial_state)
        
        # Update total time
        total_time = (time.perf_counter() - t0) * 1000
        final_state["analysis_time_ms"] = total_time
        meta = final_state.get("metadata", {})
        reasons = merge_degraded_reasons(self._degraded_reasons, meta.get("degraded_reasons", []))
        meta.update(build_degraded_metadata(reasons))
        final_state["metadata"] = meta
        
        logger.info(
            f"Analysis complete: {final_state['threat_level'].value} "
            f"in {total_time:.0f}ms"
        )
        
        return final_state
    
    def analyze_batch(self, file_paths: list) -> list:
        """Analyze multiple files."""
        return [self.analyze(fp) for fp in file_paths]
