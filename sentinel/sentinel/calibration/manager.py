"""
Calibration Manager for coordinating provider calibration.

Handles:
- Loading/saving calibration parameters per provider
- Running calibration on training data
- Applying calibration during inference
"""

import json
from pathlib import Path
from typing import Dict, List, Optional, Any
import numpy as np

from .calibrator import (
    BaseCalibrator,
    PlattCalibrator,
    IsotonicCalibrator,
    CalibrationConfig,
    create_calibrator,
)
from .evaluator import CalibrationEvaluator, CalibrationMetrics, print_calibration_report
from ..logging import get_logger

logger = get_logger(__name__)


class CalibrationManager:
    """
    Manages calibration for multiple providers.
    
    Features:
    - Per-provider calibrator storage
    - Automatic calibration method selection
    - Calibration persistence
    - Fallback behavior when uncalibrated
    """
    
    DEFAULT_CALIBRATION_DIR = Path("data/calibration/models")
    
    def __init__(
        self,
        calibration_dir: Optional[Path] = None,
        default_method: str = "platt",
        auto_load: bool = True
    ):
        """
        Initialize calibration manager.
        
        Args:
            calibration_dir: Directory for storing calibration parameters
            default_method: Default calibration method ("platt" or "isotonic")
            auto_load: Automatically load existing calibrations on init
        """
        self.calibration_dir = Path(calibration_dir) if calibration_dir else self.DEFAULT_CALIBRATION_DIR
        self.default_method = default_method
        self.evaluator = CalibrationEvaluator()
        
        # Provider name -> Calibrator
        self._calibrators: Dict[str, BaseCalibrator] = {}
        
        # Provider name -> CalibrationMetrics
        self._metrics: Dict[str, CalibrationMetrics] = {}
        
        if auto_load:
            self._load_all_calibrations()
    
    def _load_all_calibrations(self) -> None:
        """Load all existing calibrations from disk."""
        if not self.calibration_dir.exists():
            return
        
        for calib_file in self.calibration_dir.glob("*.json"):
            provider_name = calib_file.stem
            try:
                self.load_calibration(provider_name)
            except Exception as e:
                logger.warning(f"Failed to load calibration for {provider_name}: {e}")
    
    def calibrate_provider(
        self,
        provider_name: str,
        scores: np.ndarray,
        labels: np.ndarray,
        method: Optional[str] = None,
        config: Optional[CalibrationConfig] = None,
        save: bool = True,
        verbose: bool = True
    ) -> CalibrationMetrics:
        """
        Calibrate a provider using validation data.
        
        Args:
            provider_name: Name of the provider to calibrate
            scores: Raw model scores from the provider (n_samples,)
            labels: Binary labels, 1=malicious, 0=clean (n_samples,)
            method: Calibration method (default: auto-select based on data size)
            config: Optional calibration configuration
            save: Save calibration to disk after fitting
            verbose: Print calibration report
            
        Returns:
            CalibrationMetrics with evaluation results
        """
        scores = np.asarray(scores).flatten()
        labels = np.asarray(labels).flatten()
        
        n_samples = len(scores)
        logger.info(f"Calibrating {provider_name} with {n_samples} samples")
        
        # Auto-select method based on data size
        if method is None:
            if n_samples >= 1000:
                method = "isotonic"  # More flexible with enough data
            else:
                method = "platt"  # More robust with limited data
        
        # Create calibrator
        config = config or CalibrationConfig(method=method)
        calibrator = create_calibrator(method, config)
        
        # Fit calibrator
        calibrator.fit(scores, labels)
        
        if not calibrator.is_fitted:
            logger.warning(f"Calibration failed for {provider_name}")
            return CalibrationMetrics()
        
        # Store calibrator
        self._calibrators[provider_name] = calibrator
        
        # Evaluate calibration
        calibrated_probs = calibrator.transform(scores)
        metrics = self.evaluator.evaluate(calibrated_probs, labels)
        self._metrics[provider_name] = metrics
        
        # Save to disk
        if save:
            self.save_calibration(provider_name)
        
        # Print report
        if verbose:
            print_calibration_report(metrics, provider_name)
        
        return metrics
    
    def transform(
        self,
        provider_name: str,
        score: float,
        fallback_identity: bool = True
    ) -> float:
        """
        Transform a raw score to calibrated probability.
        
        Args:
            provider_name: Name of the provider
            score: Raw score to transform
            fallback_identity: If True, return score unchanged when uncalibrated
            
        Returns:
            Calibrated probability
        """
        if provider_name not in self._calibrators:
            if fallback_identity:
                logger.debug(f"No calibration for {provider_name}, using raw score")
                return max(0.0, min(1.0, score))
            else:
                raise ValueError(f"No calibration available for {provider_name}")
        
        calibrator = self._calibrators[provider_name]
        calibrated = calibrator.transform(np.array([score]))[0]
        return float(calibrated)
    
    def get_confidence(self, provider_name: str) -> float:
        """
        Get calibration confidence for a provider.
        
        Returns:
            Confidence score based on calibration quality (0.1 to 0.95)
        """
        if provider_name not in self._calibrators:
            return 0.5  # Default confidence when uncalibrated
        
        return self._calibrators[provider_name].get_confidence()
    
    def get_thresholds(self, provider_name: str) -> Dict[str, float]:
        """Get decision thresholds for a provider."""
        if provider_name in self._calibrators:
            calibrator = self._calibrators[provider_name]
            return {
                "malicious": calibrator.config.malicious_threshold,
                "suspicious": calibrator.config.suspicious_threshold,
            }
        
        # Default thresholds
        return {
            "malicious": 0.85,
            "suspicious": 0.50,
        }
    
    def is_calibrated(self, provider_name: str) -> bool:
        """Check if a provider has calibration."""
        return (
            provider_name in self._calibrators and
            self._calibrators[provider_name].is_fitted
        )
    
    def get_metrics(self, provider_name: str) -> Optional[CalibrationMetrics]:
        """Get calibration metrics for a provider."""
        return self._metrics.get(provider_name)
    
    def save_calibration(self, provider_name: str) -> None:
        """Save calibration for a provider to disk."""
        if provider_name not in self._calibrators:
            raise ValueError(f"No calibration to save for {provider_name}")
        
        self.calibration_dir.mkdir(parents=True, exist_ok=True)
        
        calibrator = self._calibrators[provider_name]
        
        # Determine file extension based on method
        if isinstance(calibrator, PlattCalibrator):
            file_path = self.calibration_dir / f"{provider_name}.json"
        else:
            file_path = self.calibration_dir / f"{provider_name}.pkl"
        
        calibrator.save(file_path)
        
        # Also save metrics if available
        if provider_name in self._metrics:
            metrics_path = self.calibration_dir / f"{provider_name}_metrics.json"
            self.evaluator.save_evaluation(
                self._metrics[provider_name],
                metrics_path,
                provider_name
            )
    
    def load_calibration(self, provider_name: str) -> BaseCalibrator:
        """Load calibration for a provider from disk."""
        # Try JSON first (Platt)
        json_path = self.calibration_dir / f"{provider_name}.json"
        pkl_path = self.calibration_dir / f"{provider_name}.pkl"
        
        if json_path.exists():
            calibrator = PlattCalibrator()
            calibrator.load(json_path)
        elif pkl_path.exists():
            calibrator = IsotonicCalibrator()
            calibrator.load(pkl_path)
        else:
            raise FileNotFoundError(f"No calibration found for {provider_name}")
        
        self._calibrators[provider_name] = calibrator
        
        # Try to load metrics
        metrics_path = self.calibration_dir / f"{provider_name}_metrics.json"
        if metrics_path.exists():
            try:
                with open(metrics_path) as f:
                    data = json.load(f)
                # Convert dict back to CalibrationMetrics (simplified)
                self._metrics[provider_name] = CalibrationMetrics(
                    brier_score=data.get("metrics", {}).get("brier_score", 1.0),
                    expected_calibration_error=data.get("metrics", {}).get("expected_calibration_error", 1.0),
                )
            except Exception:
                pass
        
        logger.info(f"Loaded calibration for {provider_name}")
        return calibrator
    
    def get_all_providers(self) -> List[str]:
        """Get list of all calibrated providers."""
        return list(self._calibrators.keys())
    
    def get_summary(self) -> Dict[str, Any]:
        """Get summary of all calibrations."""
        summary = {
            "calibration_dir": str(self.calibration_dir),
            "default_method": self.default_method,
            "providers": {}
        }
        
        for name, calibrator in self._calibrators.items():
            metrics = self._metrics.get(name)
            summary["providers"][name] = {
                "is_calibrated": calibrator.is_fitted,
                "method": "platt" if isinstance(calibrator, PlattCalibrator) else "isotonic",
                "confidence": calibrator.get_confidence(),
                "ece": metrics.expected_calibration_error if metrics else None,
                "brier_score": metrics.brier_score if metrics else None,
            }
        
        return summary
    
    def print_summary(self) -> None:
        """Print summary of all calibrations."""
        summary = self.get_summary()
        
        print(f"\n{'='*70}")
        print("CALIBRATION MANAGER SUMMARY")
        print(f"{'='*70}")
        print(f"Calibration directory: {summary['calibration_dir']}")
        print(f"Default method: {summary['default_method']}")
        print(f"Calibrated providers: {len(summary['providers'])}")
        
        if summary['providers']:
            print(f"\n{'Provider':<25} {'Method':<10} {'Fitted':<8} {'ECE':<10} {'Confidence':<10}")
            print(f"{'-'*63}")
            
            for name, info in summary['providers'].items():
                ece_str = f"{info['ece']:.4f}" if info['ece'] is not None else "N/A"
                print(f"{name:<25} {info['method']:<10} {'Yes' if info['is_calibrated'] else 'No':<8} {ece_str:<10} {info['confidence']:.2f}")
        
        print(f"{'='*70}\n")
