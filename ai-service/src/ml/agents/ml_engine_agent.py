"""MLEngineAgent - Wraps MLEngine as a DetectionAgent."""

from typing import List
import numpy as np

from .detection_agent import DetectionAgent
from .contracts import (
    DetectionCandidate,
    CanonicalEvent,
    Modality,
    ThreatType,
)
from .feature_extractors import extract_cic_features, CIC_FEATURE_COLUMNS
from ..detectors import MLEngine
from ..types import DetectionCandidate as LegacyCandidate
from ...logging import get_logger

logger = get_logger(__name__)


class MLEngineAgent(DetectionAgent):
    """
    DetectionAgent wrapper for MLEngine.
    
    Extracts exactly 79 CIC-DDoS2019 features in training order
    from canonical events and runs ML model inference.
    """
    
    def __init__(self, ml_engine: MLEngine):
        """
        Initialize with existing MLEngine instance.
        
        Args:
            ml_engine: Configured MLEngine from legacy pipeline
        """
        self._engine = ml_engine
    
    @property
    def agent_id(self) -> str:
        return "cybermesh.ml"
    
    def input_modalities(self) -> List[Modality]:
        return [Modality.NETWORK_FLOW]
    
    def analyze(self, event: CanonicalEvent) -> List[DetectionCandidate]:
        """
        Run ML inference on a canonical network flow event.
        
        Extracts exactly 79 features in CIC-DDoS2019 training order.
        Extra fields in NetworkFlowFeaturesV1 are ignored for legacy models.
        
        Args:
            event: CanonicalEvent with modality=NETWORK_FLOW
            
        Returns:
            List of DetectionCandidate from ML models
        """
        if event.modality != Modality.NETWORK_FLOW:
            raise ValueError(
                f"MLEngineAgent only supports NETWORK_FLOW, got {event.modality.value}"
            )
        
        if event.features_version != "NetworkFlowFeaturesV1":
            raise ValueError(
                f"Expected NetworkFlowFeaturesV1, got {event.features_version}"
            )
        
        # Extract exactly 79 features in CIC training order
        feature_vector = extract_cic_features(event.features)
        
        # Verify feature count
        if len(feature_vector) != 79:
            logger.error(f"Feature extraction produced {len(feature_vector)} features, expected 79")
            return []
        
        # Run legacy ML engine
        legacy_candidates = self._engine.predict(feature_vector)
        
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
            findings=[f"ML model detection: {legacy.engine_name}"],
            metadata={
                "engine_type": legacy.engine_type.value,
                "feature_count": 79,
                "schema_version": "NetworkFlowFeaturesV1",
                **legacy.metadata,
            },
        )
    
    def get_metadata(self) -> dict:
        """Get agent metadata including feature info."""
        base = super().get_metadata()
        base.update({
            "feature_columns": CIC_FEATURE_COLUMNS,
            "feature_count": 79,
            "models_loaded": [k for k, v in self._engine.models.items() if v is not None],
        })
        return base
