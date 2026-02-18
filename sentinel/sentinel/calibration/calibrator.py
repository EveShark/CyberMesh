"""
Probability calibrators for transforming raw model scores to calibrated probabilities.

Implements:
- Platt Scaling: Fits logistic regression to transform scores (parametric)
- Isotonic Regression: Non-parametric piecewise constant calibration
"""

import json
import pickle
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from pathlib import Path
from typing import List, Optional, Tuple, Dict, Any
import numpy as np

from ..logging import get_logger

logger = get_logger(__name__)


@dataclass
class CalibrationConfig:
    """Configuration for calibration."""
    method: str = "platt"  # "platt" or "isotonic"
    malicious_threshold: float = 0.85  # Probability threshold for MALICIOUS
    suspicious_threshold: float = 0.50  # Probability threshold for SUSPICIOUS
    min_samples: int = 50  # Minimum samples required for calibration
    cross_validation_folds: int = 3  # CV folds for calibration


class BaseCalibrator(ABC):
    """Base class for probability calibrators."""
    
    def __init__(self, config: Optional[CalibrationConfig] = None):
        self.config = config or CalibrationConfig()
        self.is_fitted = False
        self.calibration_error: float = 1.0  # Expected calibration error
        self.brier_score: float = 1.0
        
    @abstractmethod
    def fit(self, scores: np.ndarray, labels: np.ndarray) -> "BaseCalibrator":
        """
        Fit the calibrator on validation data.
        
        Args:
            scores: Raw model scores (n_samples,)
            labels: Binary labels, 1 for malicious, 0 for clean (n_samples,)
            
        Returns:
            self
        """
        pass
    
    @abstractmethod
    def transform(self, scores: np.ndarray) -> np.ndarray:
        """
        Transform raw scores to calibrated probabilities.
        
        Args:
            scores: Raw model scores (n_samples,)
            
        Returns:
            Calibrated probabilities (n_samples,)
        """
        pass
    
    def fit_transform(self, scores: np.ndarray, labels: np.ndarray) -> np.ndarray:
        """Fit calibrator and transform scores."""
        self.fit(scores, labels)
        return self.transform(scores)
    
    @abstractmethod
    def save(self, path: Path) -> None:
        """Save calibrator parameters to disk."""
        pass
    
    @abstractmethod
    def load(self, path: Path) -> "BaseCalibrator":
        """Load calibrator parameters from disk."""
        pass
    
    def get_confidence(self) -> float:
        """
        Get confidence in calibration quality.
        
        Returns:
            Confidence score (1.0 - calibration_error), clamped to [0.1, 0.95]
        """
        if not self.is_fitted:
            return 0.5
        confidence = 1.0 - self.calibration_error
        return max(0.1, min(0.95, confidence))


class PlattCalibrator(BaseCalibrator):
    """
    Platt Scaling calibrator.
    
    Fits a logistic regression model: P(y=1|s) = 1 / (1 + exp(A*s + B))
    where s is the raw score and A, B are learned parameters.
    
    Best for:
    - Small to medium datasets (100-1000 samples)
    - When score distribution is roughly sigmoid-shaped
    """
    
    def __init__(self, config: Optional[CalibrationConfig] = None):
        super().__init__(config)
        self.A: float = -1.0  # Scale parameter
        self.B: float = 0.0   # Shift parameter
        
    def fit(self, scores: np.ndarray, labels: np.ndarray) -> "PlattCalibrator":
        """Fit Platt scaling parameters using maximum likelihood."""
        scores = np.asarray(scores).flatten()
        labels = np.asarray(labels).flatten()
        
        if len(scores) < self.config.min_samples:
            logger.warning(
                f"Insufficient samples for calibration: {len(scores)} < {self.config.min_samples}"
            )
            # Use default parameters
            self.A = -1.0
            self.B = 0.0
            self.is_fitted = False
            return self
        
        # Use scikit-learn's LogisticRegression for Platt scaling
        try:
            from sklearn.linear_model import LogisticRegression
            
            # Reshape for sklearn
            X = scores.reshape(-1, 1)
            y = labels
            
            # Fit logistic regression
            lr = LogisticRegression(
                solver='lbfgs',
                max_iter=1000,
                C=1e10,  # No regularization for Platt scaling
            )
            lr.fit(X, y)
            
            # Extract parameters: P(y=1|s) = sigmoid(A*s + B)
            # sklearn uses: P(y=1|s) = sigmoid(coef_*s + intercept_)
            self.A = float(lr.coef_[0, 0])
            self.B = float(lr.intercept_[0])
            
            # Compute calibration metrics
            calibrated_probs = self.transform(scores)
            self._compute_metrics(calibrated_probs, labels)
            
            self.is_fitted = True
            logger.info(f"Platt calibrator fitted: A={self.A:.4f}, B={self.B:.4f}, ECE={self.calibration_error:.4f}")
            
        except ImportError:
            logger.error("scikit-learn required for Platt scaling")
            self.is_fitted = False
            
        except Exception as e:
            logger.error(f"Platt scaling fit failed: {e}")
            self.is_fitted = False
            
        return self
    
    def transform(self, scores: np.ndarray) -> np.ndarray:
        """Transform raw scores using fitted Platt scaling."""
        scores = np.asarray(scores).flatten()
        
        # Sigmoid transformation: P = 1 / (1 + exp(-(A*s + B)))
        logits = self.A * scores + self.B
        
        # Clip to avoid overflow
        logits = np.clip(logits, -500, 500)
        
        probs = 1.0 / (1.0 + np.exp(-logits))
        
        # Clip to valid probability range
        return np.clip(probs, 0.001, 0.999)
    
    def _compute_metrics(self, probs: np.ndarray, labels: np.ndarray) -> None:
        """Compute calibration metrics."""
        # Brier score
        self.brier_score = float(np.mean((probs - labels) ** 2))
        
        # Expected Calibration Error (ECE)
        n_bins = 10
        bin_boundaries = np.linspace(0, 1, n_bins + 1)
        
        ece = 0.0
        total_samples = len(probs)
        
        for i in range(n_bins):
            bin_mask = (probs >= bin_boundaries[i]) & (probs < bin_boundaries[i + 1])
            bin_size = np.sum(bin_mask)
            
            if bin_size > 0:
                bin_accuracy = np.mean(labels[bin_mask])
                bin_confidence = np.mean(probs[bin_mask])
                ece += (bin_size / total_samples) * abs(bin_accuracy - bin_confidence)
        
        self.calibration_error = float(ece)
    
    def save(self, path: Path) -> None:
        """Save Platt parameters to JSON."""
        path = Path(path)
        path.parent.mkdir(parents=True, exist_ok=True)
        
        data = {
            "method": "platt",
            "A": self.A,
            "B": self.B,
            "is_fitted": self.is_fitted,
            "calibration_error": self.calibration_error,
            "brier_score": self.brier_score,
            "config": {
                "malicious_threshold": self.config.malicious_threshold,
                "suspicious_threshold": self.config.suspicious_threshold,
            }
        }
        
        with open(path, "w") as f:
            json.dump(data, f, indent=2)
        
        logger.info(f"Saved Platt calibrator to {path}")
    
    def load(self, path: Path) -> "PlattCalibrator":
        """Load Platt parameters from JSON."""
        path = Path(path)
        
        with open(path, "r") as f:
            data = json.load(f)
        
        if data.get("method") != "platt":
            raise ValueError(f"Expected Platt calibrator, got {data.get('method')}")
        
        self.A = data["A"]
        self.B = data["B"]
        self.is_fitted = data.get("is_fitted", True)
        self.calibration_error = data.get("calibration_error", 0.1)
        self.brier_score = data.get("brier_score", 0.1)
        
        if "config" in data:
            self.config.malicious_threshold = data["config"].get("malicious_threshold", 0.85)
            self.config.suspicious_threshold = data["config"].get("suspicious_threshold", 0.50)
        
        logger.info(f"Loaded Platt calibrator from {path}")
        return self


class IsotonicCalibrator(BaseCalibrator):
    """
    Isotonic Regression calibrator.
    
    Non-parametric calibration that finds a monotonically increasing
    step function mapping raw scores to probabilities.
    
    Best for:
    - Large datasets (1000+ samples)
    - When the score-probability relationship is not sigmoid-shaped
    """
    
    def __init__(self, config: Optional[CalibrationConfig] = None):
        super().__init__(config)
        self._isotonic = None
        
    def fit(self, scores: np.ndarray, labels: np.ndarray) -> "IsotonicCalibrator":
        """Fit isotonic regression."""
        scores = np.asarray(scores).flatten()
        labels = np.asarray(labels).flatten()
        
        if len(scores) < self.config.min_samples * 2:  # Isotonic needs more data
            logger.warning(
                f"Insufficient samples for isotonic calibration: {len(scores)}"
            )
            self.is_fitted = False
            return self
        
        try:
            from sklearn.isotonic import IsotonicRegression
            
            self._isotonic = IsotonicRegression(
                y_min=0.001,
                y_max=0.999,
                out_of_bounds='clip'
            )
            self._isotonic.fit(scores, labels)
            
            # Compute calibration metrics
            calibrated_probs = self.transform(scores)
            self._compute_metrics(calibrated_probs, labels)
            
            self.is_fitted = True
            logger.info(f"Isotonic calibrator fitted: ECE={self.calibration_error:.4f}")
            
        except ImportError:
            logger.error("scikit-learn required for isotonic calibration")
            self.is_fitted = False
            
        except Exception as e:
            logger.error(f"Isotonic fit failed: {e}")
            self.is_fitted = False
            
        return self
    
    def transform(self, scores: np.ndarray) -> np.ndarray:
        """Transform raw scores using fitted isotonic regression."""
        scores = np.asarray(scores).flatten()
        
        if self._isotonic is None or not self.is_fitted:
            # Fallback: return scores clipped to [0, 1]
            return np.clip(scores, 0.001, 0.999)
        
        probs = self._isotonic.transform(scores)
        return np.clip(probs, 0.001, 0.999)
    
    def _compute_metrics(self, probs: np.ndarray, labels: np.ndarray) -> None:
        """Compute calibration metrics."""
        self.brier_score = float(np.mean((probs - labels) ** 2))
        
        # Expected Calibration Error
        n_bins = 10
        bin_boundaries = np.linspace(0, 1, n_bins + 1)
        
        ece = 0.0
        total_samples = len(probs)
        
        for i in range(n_bins):
            bin_mask = (probs >= bin_boundaries[i]) & (probs < bin_boundaries[i + 1])
            bin_size = np.sum(bin_mask)
            
            if bin_size > 0:
                bin_accuracy = np.mean(labels[bin_mask])
                bin_confidence = np.mean(probs[bin_mask])
                ece += (bin_size / total_samples) * abs(bin_accuracy - bin_confidence)
        
        self.calibration_error = float(ece)
    
    def save(self, path: Path) -> None:
        """Save isotonic calibrator to pickle."""
        path = Path(path)
        path.parent.mkdir(parents=True, exist_ok=True)
        
        data = {
            "method": "isotonic",
            "isotonic": self._isotonic,
            "is_fitted": self.is_fitted,
            "calibration_error": self.calibration_error,
            "brier_score": self.brier_score,
            "config": {
                "malicious_threshold": self.config.malicious_threshold,
                "suspicious_threshold": self.config.suspicious_threshold,
            }
        }
        
        with open(path, "wb") as f:
            pickle.dump(data, f)
        
        logger.info(f"Saved isotonic calibrator to {path}")
    
    def load(self, path: Path) -> "IsotonicCalibrator":
        """Load isotonic calibrator from pickle."""
        path = Path(path)
        
        with open(path, "rb") as f:
            data = pickle.load(f)
        
        if data.get("method") != "isotonic":
            raise ValueError(f"Expected isotonic calibrator, got {data.get('method')}")
        
        self._isotonic = data["isotonic"]
        self.is_fitted = data.get("is_fitted", True)
        self.calibration_error = data.get("calibration_error", 0.1)
        self.brier_score = data.get("brier_score", 0.1)
        
        if "config" in data:
            self.config.malicious_threshold = data["config"].get("malicious_threshold", 0.85)
            self.config.suspicious_threshold = data["config"].get("suspicious_threshold", 0.50)
        
        logger.info(f"Loaded isotonic calibrator from {path}")
        return self


def create_calibrator(method: str = "platt", config: Optional[CalibrationConfig] = None) -> BaseCalibrator:
    """Factory function to create a calibrator."""
    config = config or CalibrationConfig(method=method)
    
    if method == "platt":
        return PlattCalibrator(config)
    elif method == "isotonic":
        return IsotonicCalibrator(config)
    else:
        raise ValueError(f"Unknown calibration method: {method}")
