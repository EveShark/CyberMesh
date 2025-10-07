"""
Adaptive detection components that integrate with FeedbackService.

Provides:
- Threshold override mechanism
- Score calibration wrapper
- PolicyManager integration
"""

from typing import Optional, Dict
import numpy as np


class AdaptiveDetection:
    """
    Wrapper that applies FeedbackService adaptations to detection pipeline.
    
    Used by EnsembleVoter to get dynamic thresholds and calibrated scores.
    """
    
    def __init__(self, feedback_service=None):
        """
        Initialize adaptive detection.
        
        Args:
            feedback_service: FeedbackService instance (optional)
        """
        self.feedback_service = feedback_service
    
    def get_threshold(self, anomaly_type: str, default: float) -> float:
        """
        Get threshold with FeedbackService overrides applied.
        
        Priority:
        1. FeedbackService (validator + auto-adjusted)
        2. Default from config
        
        Args:
            anomaly_type: Threat type (ddos, malware, etc.)
            default: Fallback threshold
            
        Returns:
            Final threshold to use
        """
        if self.feedback_service:
            return self.feedback_service.get_calibrated_threshold(anomaly_type, default)
        return default
    
    def calibrate_score(self, raw_score: float) -> float:
        """
        Calibrate raw model score using FeedbackService.
        
        Args:
            raw_score: Uncalibrated model output [0, 1]
            
        Returns:
            Calibrated score [0, 1]
        """
        if self.feedback_service:
            return self.feedback_service.calibrate_score(raw_score)
        return raw_score
    
    def calibrate_scores_batch(self, raw_scores: np.ndarray) -> np.ndarray:
        """
        Calibrate batch of scores.
        
        Args:
            raw_scores: Array of uncalibrated scores
            
        Returns:
            Array of calibrated scores
        """
        if self.feedback_service:
            import numpy as np
            calibrated = []
            for score in raw_scores:
                calibrated.append(self.feedback_service.calibrate_score(score))
            return np.array(calibrated)
        return raw_scores
    
    def get_policy_override(self, key: str, default=None):
        """
        Get policy override from FeedbackService.
        
        Args:
            key: Config key (e.g., "enable_ddos_pipeline")
            default: Default value if no override
            
        Returns:
            Override value or default
        """
        if self.feedback_service and self.feedback_service.policy_manager:
            return self.feedback_service.policy_manager.get_override(key, default)
        return default
