"""
Calibration module for Sentinel.

Provides probability calibration for ML models to ensure
predicted scores represent true probabilities.

Key components:
- PlattCalibrator: Logistic regression-based calibration (parametric)
- IsotonicCalibrator: Non-parametric calibration for larger datasets
- CalibrationEvaluator: Reliability diagrams and Brier score computation
"""

from .calibrator import (
    BaseCalibrator,
    PlattCalibrator,
    IsotonicCalibrator,
    CalibrationConfig,
)
from .evaluator import CalibrationEvaluator, CalibrationMetrics
from .manager import CalibrationManager

__all__ = [
    "BaseCalibrator",
    "PlattCalibrator", 
    "IsotonicCalibrator",
    "CalibrationConfig",
    "CalibrationEvaluator",
    "CalibrationMetrics",
    "CalibrationManager",
]
