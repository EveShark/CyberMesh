"""RulesEngineAgent - Wraps RulesEngine as a DetectionAgent."""

from typing import List

from .detection_agent import DetectionAgent
from .contracts import (
    DetectionCandidate,
    CanonicalEvent,
    Modality,
    ThreatType,
)
from .feature_extractors import extract_semantics
from ..detectors import RulesEngine
from ..types import DetectionCandidate as LegacyCandidate
from ...logging import get_logger

logger = get_logger(__name__)


class RulesEngineAgent(DetectionAgent):
    """
    DetectionAgent wrapper for RulesEngine.
    
    Extracts semantic features from canonical events and runs
    threshold-based rules for DDoS, port scan, and SYN flood detection.
    """
    
    def __init__(self, rules_engine: RulesEngine):
        """
        Initialize with existing RulesEngine instance.
        
        Args:
            rules_engine: Configured RulesEngine from legacy pipeline
        """
        self._engine = rules_engine
    
    @property
    def agent_id(self) -> str:
        return "cybermesh.rules"
    
    def input_modalities(self) -> List[Modality]:
        return [Modality.NETWORK_FLOW]
    
    def analyze(self, event: CanonicalEvent) -> List[DetectionCandidate]:
        """
        Run rules on a canonical network flow event.
        
        Args:
            event: CanonicalEvent with modality=NETWORK_FLOW
            
        Returns:
            List of DetectionCandidate from triggered rules
        """
        if event.modality != Modality.NETWORK_FLOW:
            raise ValueError(
                f"RulesEngineAgent only supports NETWORK_FLOW, got {event.modality.value}"
            )
        
        if event.features_version != "NetworkFlowFeaturesV1":
            raise ValueError(
                f"Expected NetworkFlowFeaturesV1, got {event.features_version}"
            )
        
        # Extract semantic features for RulesEngine
        semantics = extract_semantics(event.features)
        
        # Run legacy engine
        legacy_candidates = self._engine.predict(semantics)
        
        # Convert to unified DetectionCandidate
        return [self._convert_candidate(lc) for lc in legacy_candidates]
    
    def _convert_candidate(self, legacy: LegacyCandidate) -> DetectionCandidate:
        """Convert legacy DetectionCandidate to unified contract."""
        return DetectionCandidate(
            agent_id=self.agent_id,
            signal_id=legacy.engine_name,
            threat_type=ThreatType(legacy.threat_type.value),
            raw_score=legacy.raw_score,
            calibrated_score=legacy.calibrated_score,
            confidence=legacy.confidence,
            features=legacy.features,
            findings=[f"Rule triggered: {legacy.engine_name}"],
            metadata={
                "engine_type": legacy.engine_type.value,
                **legacy.metadata,
            },
        )
