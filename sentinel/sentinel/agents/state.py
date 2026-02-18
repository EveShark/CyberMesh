"""Agent state definitions for LangGraph workflow."""

from dataclasses import dataclass, field
from enum import Enum
from typing import Any, Dict, List, Optional, Annotated
import operator

from ..parsers.base import ParsedFile, FileType
from ..providers.base import ThreatLevel, AnalysisResult


class AnalysisStage(str, Enum):
    """Analysis workflow stages."""
    INIT = "init"
    STATIC_ANALYSIS = "static_analysis"
    ML_ANALYSIS = "ml_analysis"
    DEEP_ANALYSIS = "deep_analysis"
    LLM_REASONING = "llm_reasoning"
    AGGREGATION = "aggregation"
    COMPLETE = "complete"


@dataclass
class Finding:
    """A single analysis finding."""
    source: str
    severity: str  # low, medium, high, critical
    description: str
    confidence: float = 0.0
    metadata: Dict[str, Any] = field(default_factory=dict)


@dataclass 
class AgentState:
    """
    Shared state for the analysis workflow.
    
    This state is passed between agents in the LangGraph workflow.
    Uses Annotated types for proper state reduction.
    """
    # Input
    file_path: str = ""
    parsed_file: Optional[ParsedFile] = None
    
    # Stage tracking
    current_stage: AnalysisStage = AnalysisStage.INIT
    stages_completed: List[str] = field(default_factory=list)
    
    # Results from each agent
    static_results: List[AnalysisResult] = field(default_factory=list)
    ml_results: List[AnalysisResult] = field(default_factory=list)
    llm_results: List[AnalysisResult] = field(default_factory=list)
    
    # Aggregated findings
    findings: List[Finding] = field(default_factory=list)
    indicators: List[Dict[str, str]] = field(default_factory=list)
    
    # Verdicts
    threat_level: ThreatLevel = ThreatLevel.UNKNOWN
    confidence: float = 0.0
    final_score: float = 0.0
    
    # Reasoning chain
    reasoning_steps: List[str] = field(default_factory=list)
    final_reasoning: str = ""
    
    # Control flow
    needs_deep_analysis: bool = False
    needs_llm_reasoning: bool = False
    
    # Errors
    errors: List[str] = field(default_factory=list)
    
    # Metadata
    analysis_time_ms: float = 0.0
    metadata: Dict[str, Any] = field(default_factory=dict)


def merge_lists(left: List, right: List) -> List:
    """Merge two lists (used for state reduction)."""
    return left + right


def merge_dicts(left: Dict, right: Dict) -> Dict:
    """Merge two dicts (used for state reduction)."""
    result = left.copy()
    result.update(right)
    return result


# Type definitions for LangGraph state with reducers
from typing import TypedDict

class GraphState(TypedDict, total=False):
    """LangGraph compatible state definition."""
    file_path: str
    parsed_file: Optional[ParsedFile]
    
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
    
    needs_deep_analysis: bool
    needs_llm_reasoning: bool
    
    errors: Annotated[List[str], operator.add]
    
    analysis_time_ms: float
    metadata: Annotated[Dict[str, Any], merge_dicts]
