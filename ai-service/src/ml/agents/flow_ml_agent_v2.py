"""FlowMLEngineAgentV2 - Canonical flow agent with legacy model support."""

from typing import List, Dict, Any, Optional

from .detection_agent import DetectionAgent
from .contracts import (
    DetectionCandidate,
    CanonicalEvent,
    Modality,
    ThreatType,
)
from .feature_extractors import extract_cic_features, CIC_FEATURE_COLUMNS
from ...logging import get_logger

logger = get_logger(__name__)


class FlowMLEngineAgentV2(DetectionAgent):
    """
    V2 Flow ML agent using canonical NetworkFlowFeaturesV1 schema.
    
    This agent:
    1. Accepts canonical events with modality=NETWORK_FLOW
    2. Can use existing 79-feature models via canonical->legacy mapping
    3. Supports future models trained directly on NetworkFlowFeaturesV1
    
    Configuration:
        use_legacy_feature_mapping: If True (default), maps canonical features
            to 79-feature vector for existing models. Set False when using
            models trained directly on NetworkFlowFeaturesV1.
    """
    
    def __init__(
        self,
        ml_engine=None,
        config: Optional[Dict[str, Any]] = None,
    ):
        """
        Initialize FlowMLEngineAgentV2.
        
        Args:
            ml_engine: Optional MLEngine instance for predictions
            config: Configuration dict with:
                - use_legacy_feature_mapping: bool (default True)
                - model_version: str (for metadata)
        """
        self._ml_engine = ml_engine
        self._config = config or {}
        self._use_legacy_mapping = self._config.get('use_legacy_feature_mapping', True)
        self._model_version = self._config.get('model_version', 'unknown')
    
    @property
    def agent_id(self) -> str:
        return "cybermesh.ml.v2"
    
    def input_modalities(self) -> List[Modality]:
        return [Modality.NETWORK_FLOW]
    
    def analyze(self, event: CanonicalEvent) -> List[DetectionCandidate]:
        """
        Analyze a network flow event.
        
        Args:
            event: CanonicalEvent with modality=NETWORK_FLOW
            
        Returns:
            List of DetectionCandidate objects
            
        Raises:
            ValueError: If event modality is not NETWORK_FLOW
        """
        if event.modality != Modality.NETWORK_FLOW:
            raise ValueError(
                f"FlowMLEngineAgentV2 only supports NETWORK_FLOW, got {event.modality.value}"
            )
        
        if event.features_version != "NetworkFlowFeaturesV1":
            raise ValueError(
                f"FlowMLEngineAgentV2 requires NetworkFlowFeaturesV1, got {event.features_version}"
            )
        
        # Extract features based on configuration
        if self._use_legacy_mapping:
            feature_vector = self._extract_legacy_features(event.features)
        else:
            feature_vector = self._extract_canonical_features(event.features)
        
        # Run prediction if ML engine available
        if self._ml_engine is None:
            logger.warning("No ML engine configured, returning empty candidates")
            return []
        
        try:
            predictions = self._ml_engine.predict(feature_vector)
            return self._convert_predictions(predictions, event)
        except Exception as e:
            logger.error(f"ML prediction failed: {e}")
            return []
    
    def _extract_legacy_features(self, features: Dict[str, Any]) -> List[float]:
        """
        Extract 79-feature vector from canonical features.
        
        Uses the same extraction logic as Phase 4 to ensure compatibility
        with existing models trained on CIC-DDoS2019 79-feature vectors.
        """
        return extract_cic_features(features)
    
    def _extract_canonical_features(self, features: Dict[str, Any]) -> Dict[str, Any]:
        """
        Extract features directly from canonical schema.
        
        Used when models are trained on NetworkFlowFeaturesV1 directly.
        Returns dict for models that accept named features.
        """
        return features
    
    def _convert_predictions(
        self,
        predictions: List,
        event: CanonicalEvent,
    ) -> List[DetectionCandidate]:
        """Convert ML engine predictions to DetectionCandidates."""
        candidates = []
        
        for pred in predictions:
            # Map legacy ThreatType to new ThreatType
            threat_type = self._map_threat_type(pred.threat_type)
            
            candidate = DetectionCandidate(
                agent_id=self.agent_id,
                signal_id=pred.engine_name if hasattr(pred, 'engine_name') else 'flow_ml_v2',
                threat_type=threat_type,
                raw_score=pred.raw_score,
                calibrated_score=pred.calibrated_score,
                confidence=pred.confidence,
                features=dict(pred.features) if hasattr(pred, 'features') and pred.features else {},
                metadata={
                    'model_version': self._model_version,
                    'use_legacy_mapping': self._use_legacy_mapping,
                    'event_id': event.id,
                    'features_version': event.features_version,
                },
            )
            candidates.append(candidate)
        
        return candidates
    
    def _map_threat_type(self, legacy_threat) -> ThreatType:
        """Map legacy ThreatType enum to contracts ThreatType."""
        if hasattr(legacy_threat, 'value'):
            threat_value = legacy_threat.value
        else:
            threat_value = str(legacy_threat)
        
        mapping = {
            'ddos': ThreatType.DDOS,
            'dos': ThreatType.DOS,
            'malware': ThreatType.MALWARE,
            'anomaly': ThreatType.ANOMALY,
            'network_intrusion': ThreatType.NETWORK_INTRUSION,
            'policy_violation': ThreatType.POLICY_VIOLATION,
            'botnet': ThreatType.BOTNET,
            'c2': ThreatType.C2_COMMUNICATION,
        }
        
        return mapping.get(threat_value.lower(), ThreatType.ANOMALY)
    
    def get_metadata(self) -> Dict[str, Any]:
        """Get agent metadata."""
        base = super().get_metadata()
        base.update({
            'use_legacy_feature_mapping': self._use_legacy_mapping,
            'model_version': self._model_version,
            'feature_count': len(CIC_FEATURE_COLUMNS) if self._use_legacy_mapping else 'variable',
            'ml_engine_available': self._ml_engine is not None,
        })
        return base
    
    @property
    def is_ready(self) -> bool:
        """Check if agent is ready for predictions."""
        return self._ml_engine is not None


def create_flow_ml_agent_v2(
    ml_engine=None,
    use_legacy_mapping: bool = True,
    model_version: str = "unknown",
) -> FlowMLEngineAgentV2:
    """
    Factory function to create FlowMLEngineAgentV2.
    
    Args:
        ml_engine: Optional MLEngine for predictions
        use_legacy_mapping: Use 79-feature mapping for legacy models
        model_version: Version string for metadata
        
    Returns:
        Configured FlowMLEngineAgentV2 instance
    """
    return FlowMLEngineAgentV2(
        ml_engine=ml_engine,
        config={
            'use_legacy_feature_mapping': use_legacy_mapping,
            'model_version': model_version,
        },
    )
