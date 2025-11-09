"""
Feedback Loop Components - Backend to AI Learning

This module implements the feedback loop that allows AI to learn from
backend validator consensus decisions.

Components:
- AnomalyLifecycleTracker: Tracks anomaly state transitions (detected->committed)
- PolicyManager: Applies backend policy updates (thresholds, blacklists, flags)
- ConfidenceCalibrator: Retrains Platt scaling based on validator acceptance
- ThresholdManager: Dynamically adjusts detection thresholds
- RedisStorage: Redis persistence layer with circuit breaker

Architecture:
    Backend Commits Block
           |
           v
    CommitEvent (Kafka)
           |
           v
    AnomalyLifecycleTracker
           |
           +---> Calculate acceptance rate
           +---> Trigger calibration (if needed)
           +---> Update metrics

    Backend Policy Update
           |
           v
    PolicyUpdateEvent (Kafka)
           |
           v
    PolicyManager
           |
           +---> Validate policy
           +---> Apply threshold changes
           +---> Update feature flags
           +---> Store in Redis

Security:
- All backend messages verified with Ed25519 signatures
- Policy changes validated against strict ranges
- Redis operations wrapped in circuit breaker
- Audit trail for all policy changes
"""

from .storage import RedisStorage
from .tracker import (
    AnomalyLifecycleTracker,
    AnomalyState,
    AnomalyLifecycle,
    AcceptanceMetrics,
    LatencyStats,
    VALID_TRANSITIONS
)
from .calibrator import ConfidenceCalibrator
from .policy_manager import PolicyManager, PolicyRecord
from .threshold_manager import ThresholdManager, ThresholdAdjustment
from .service import FeedbackService

__all__ = [
    "RedisStorage",
    "AnomalyLifecycleTracker",
    "AnomalyState",
    "AnomalyLifecycle",
    "AcceptanceMetrics",
    "LatencyStats",
    "VALID_TRANSITIONS",
    "ConfidenceCalibrator",
    "PolicyManager",
    "PolicyRecord",
    "ThresholdManager",
    "ThresholdAdjustment",
    "FeedbackService",
]
