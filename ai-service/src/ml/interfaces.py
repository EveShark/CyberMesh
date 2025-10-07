"""
Engine interfaces for ML detection pipeline.

Clean ABC interfaces with predict → List[DetectionCandidate] contract.
"""

from abc import ABC, abstractmethod
from typing import List
import numpy as np

from .types import DetectionCandidate, EngineType


class Engine(ABC):
    """
    Base interface for all detection engines.
    
    All engines (ML, Rules, Math) must implement this interface.
    Contract: predict() returns calibrated DetectionCandidates.
    """
    
    @abstractmethod
    def predict(self, features: np.ndarray, batch: bool = False) -> List[DetectionCandidate]:
        """
        Run detection on feature vector(s).
        
        Args:
            features: Feature vector(s)
                - Single sample: shape (n_features,)
                - Batch: shape (n_samples, n_features) if batch=True
            batch: Enable batch inference (default: False)
        
        Returns:
            List of DetectionCandidate with calibrated scores.
            Empty list if no threats detected or confidence too low.
        
        Raises:
            ValueError: If features shape invalid
            RuntimeError: If engine not initialized
        """
        pass
    
    @abstractmethod
    def calibrate(self, method: str = "platt") -> None:
        """
        Apply calibration to model outputs.
        
        Args:
            method: Calibration method ("platt" or "isotonic")
        
        Raises:
            ValueError: If method not supported
            RuntimeError: If calibration data not available
        """
        pass
    
    @property
    @abstractmethod
    def engine_type(self) -> EngineType:
        """
        Returns engine type (ML, RULES, or MATH).
        
        Returns:
            EngineType enum value
        """
        pass
    
    @property
    @abstractmethod
    def is_ready(self) -> bool:
        """
        Check if engine is ready for inference.
        
        Returns:
            True if initialized and calibrated, False otherwise
        """
        pass
    
    @abstractmethod
    def get_metadata(self) -> dict:
        """
        Get engine metadata for instrumentation.
        
        Returns:
            Dictionary with version, model names, config, etc.
        """
        pass
