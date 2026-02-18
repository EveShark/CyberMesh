"""
DetectionAgent interface.

Abstract base class for detection agents that process canonical events
and produce detection candidates.
"""

from abc import ABC, abstractmethod
from typing import List

from ..contracts import DetectionCandidate, CanonicalEvent, Modality


class DetectionAgent(ABC):
    """
    Abstract base class for detection agents.
    
    Processes canonical events and produces detection candidates.
    Implementations should be stateless and modality-aware.
    """
    
    @property
    @abstractmethod
    def agent_id(self) -> str:
        """Unique agent identifier (e.g., 'sentinel.file')."""
        pass
    
    @abstractmethod
    def input_modalities(self) -> List[Modality]:
        """
        Return the modalities this agent can handle.
        
        Examples:
            - [Modality.FILE] for file-based analysis
            - [Modality.NETWORK_FLOW] for network flow analysis
            - [Modality.FILE, Modality.NETWORK_FLOW] for multi-modal agents
        """
        pass
    
    @abstractmethod
    def analyze(self, event: CanonicalEvent) -> List[DetectionCandidate]:
        """
        Run detection for a single canonical event.
        
        Args:
            event: Canonical event with features and context
            
        Returns:
            List of DetectionCandidate objects (may be empty if no threats detected)
            
        Raises:
            ValueError: If event modality not supported by this agent
        """
        pass
    
    def can_process(self, event: CanonicalEvent) -> bool:
        """
        Check if this agent can process the given event.
        
        Args:
            event: Canonical event to check
            
        Returns:
            True if event modality is in this agent's supported modalities
        """
        return event.modality in self.input_modalities()
    
    def get_metadata(self) -> dict:
        """
        Get agent metadata for observability.
        
        Returns:
            Dict with agent_id, supported modalities, and any config info
        """
        return {
            "agent_id": self.agent_id,
            "input_modalities": [m.value for m in self.input_modalities()],
        }
