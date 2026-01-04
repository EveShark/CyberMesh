"""DetectionAgent interface."""

from abc import ABC, abstractmethod
from typing import List

from .contracts import DetectionCandidate, CanonicalEvent, Modality


class DetectionAgent(ABC):
    """
    Abstract base class for detection agents.
    
    Processes canonical events and produces detection candidates.
    Implementations should be stateless and modality-aware.
    """
    
    @property
    @abstractmethod
    def agent_id(self) -> str:
        """Unique agent identifier (e.g., 'cybermesh.rules.ddos')."""
        pass
    
    @abstractmethod
    def input_modalities(self) -> List[Modality]:
        """Return the modalities this agent can handle."""
        pass
    
    @abstractmethod
    def analyze(self, event: CanonicalEvent) -> List[DetectionCandidate]:
        """
        Run detection for a single canonical event.
        
        Args:
            event: Canonical event with features and context
            
        Returns:
            List of DetectionCandidate objects (may be empty)
            
        Raises:
            ValueError: If event modality not supported
        """
        pass
    
    def can_process(self, event: CanonicalEvent) -> bool:
        """Check if this agent can process the given event."""
        return event.modality in self.input_modalities()
    
    def get_metadata(self) -> dict:
        """Get agent metadata for observability."""
        return {
            "agent_id": self.agent_id,
            "input_modalities": [m.value for m in self.input_modalities()],
        }
