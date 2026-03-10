"""Base agent interface."""

from abc import ABC, abstractmethod
from typing import Any, Dict

from .state import GraphState
from ..logging import get_logger

logger = get_logger(__name__)


class BaseAgent(ABC):
    """
    Base class for all analysis agents.
    
    Each agent is a node in the LangGraph workflow that:
    1. Receives the shared state
    2. Performs analysis
    3. Updates and returns the state
    """
    
    @property
    @abstractmethod
    def name(self) -> str:
        """Agent name for logging and identification."""
        pass
    
    @abstractmethod
    def __call__(self, state: GraphState) -> Dict[str, Any]:
        """
        Execute agent logic.
        
        Args:
            state: Current workflow state
            
        Returns:
            Dictionary of state updates
        """
        pass
    
    def should_run(self, state: GraphState) -> bool:
        """
        Check if this agent should run.
        
        Override in subclasses for conditional execution.
        """
        return True
    
    def _log_start(self, state: GraphState) -> None:
        """Log agent start."""
        file_name = state.get("parsed_file")
        if file_name and hasattr(file_name, "file_name"):
            file_name = file_name.file_name
        else:
            file_name = state.get("file_path", "unknown")
        if not file_name or file_name == "unknown":
            event = state.get("event")
            labels = getattr(event, "labels", {}) if event is not None else {}
            scenario = str(labels.get("scenario") or "").strip() if isinstance(labels, dict) else ""
            profile_mode = str(labels.get("profile_mode") or "").strip() if isinstance(labels, dict) else ""
            source_event_id = str(labels.get("source_event_id") or getattr(event, "id", "") or "").strip() if isinstance(labels, dict) else ""
            modality = getattr(getattr(event, "modality", None), "value", "") if event is not None else ""
            parts = [part for part in (scenario, profile_mode, modality) if part]
            if parts:
                file_name = "/".join(parts)
                if source_event_id:
                    file_name = f"{file_name}:{source_event_id}"
        logger.info(f"[{self.name}] Starting analysis of {file_name}")
    
    def _log_complete(self, updates: Dict[str, Any]) -> None:
        """Log agent completion."""
        findings_count = len(updates.get("findings", []))
        logger.info(f"[{self.name}] Complete - {findings_count} findings")
