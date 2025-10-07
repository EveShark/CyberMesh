"""
ThresholdManager - Dynamic threshold adjustment

Automatically adjusts detection thresholds based on:
- Validator acceptance rates
- False positive indicators
- Backend feedback signals
- Time-weighted metrics
"""

import time
import logging
from typing import Dict, Any, Optional, Tuple
from dataclasses import dataclass
from .tracker import AnomalyLifecycleTracker
from .storage import RedisStorage
from ..config.settings import FeedbackConfig


@dataclass
class ThresholdAdjustment:
    """Record of threshold adjustment."""
    anomaly_type: str
    old_threshold: float
    new_threshold: float
    reason: str
    timestamp: float
    acceptance_rate: float
    sample_count: int
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "anomaly_type": self.anomaly_type,
            "old_threshold": self.old_threshold,
            "new_threshold": self.new_threshold,
            "reason": self.reason,
            "timestamp": self.timestamp,
            "acceptance_rate": self.acceptance_rate,
            "sample_count": self.sample_count
        }


class ThresholdManager:
    """
    Dynamically adjust detection thresholds based on validator feedback.
    
    Strategy:
    - If acceptance_rate < target_min: INCREASE threshold (be more selective)
    - If acceptance_rate > target_max: DECREASE threshold (detect more)
    - Use exponential moving average for stability
    - Clamp adjustments to prevent runaway changes
    """
    
    REDIS_PREFIX = "thresholds"
    REDIS_CURRENT = f"{REDIS_PREFIX}:current"
    REDIS_HISTORY = f"{REDIS_PREFIX}:history"
    
    # Adjustment parameters
    MIN_THRESHOLD = 0.50  # Never go below 50%
    MAX_THRESHOLD = 0.99  # Never go above 99%
    MIN_ADJUSTMENT = 0.01  # Minimum change
    MAX_ADJUSTMENT = 0.10  # Maximum change per adjustment
    ADJUSTMENT_FACTOR = 0.05  # 5% adjustment per step
    
    def __init__(
        self,
        tracker: AnomalyLifecycleTracker,
        storage: RedisStorage,
        config: FeedbackConfig
    ):
        """
        Initialize ThresholdManager.
        
        Args:
            tracker: AnomalyLifecycleTracker instance
            storage: RedisStorage instance
            config: FeedbackConfig instance
        """
        self.tracker = tracker
        self.storage = storage
        self.config = config
        self.logger = logging.getLogger(__name__)
        
        # Current thresholds (loaded from Redis or defaults)
        self.thresholds: Dict[str, float] = {}
        
        # Load current thresholds
        self._load_thresholds()
    
    def _load_thresholds(self):
        """Load current thresholds from Redis."""
        try:
            stored = self.storage.get(self.REDIS_CURRENT)
            if stored:
                self.thresholds = stored
                self.logger.info(f"Loaded {len(self.thresholds)} thresholds from Redis")
            else:
                # Initialize with defaults
                self.thresholds = {
                    "ddos": 0.85,
                    "malware": 0.90,
                    "port_scan": 0.80,
                    "data_exfil": 0.88
                }
                self._persist_thresholds()
                self.logger.info("Initialized default thresholds")
        except Exception as e:
            self.logger.error(f"Failed to load thresholds: {e}")
            self.thresholds = {}
    
    def _persist_thresholds(self):
        """Persist thresholds to Redis."""
        try:
            self.storage.set(self.REDIS_CURRENT, self.thresholds)
        except Exception as e:
            self.logger.error(f"Failed to persist thresholds: {e}")
    
    def get_threshold(self, anomaly_type: str, default: float = 0.85) -> float:
        """Get current threshold for anomaly type."""
        return self.thresholds.get(anomaly_type, default)
    
    def set_threshold(self, anomaly_type: str, threshold: float):
        """Manually set threshold (for testing or admin override)."""
        if not (self.MIN_THRESHOLD <= threshold <= self.MAX_THRESHOLD):
            raise ValueError(
                f"Threshold must be in [{self.MIN_THRESHOLD}, {self.MAX_THRESHOLD}]"
            )
        
        old_threshold = self.thresholds.get(anomaly_type, 0.85)
        self.thresholds[anomaly_type] = threshold
        self._persist_thresholds()
        
        self.logger.info(
            f"Manually set threshold for {anomaly_type}: "
            f"{old_threshold:.3f} → {threshold:.3f}"
        )
    
    def adjust_threshold(
        self,
        anomaly_type: str,
        acceptance_rate: float,
        sample_count: int,
        window: str = "short"
    ) -> Optional[ThresholdAdjustment]:
        """
        Adjust threshold based on acceptance rate.
        
        Args:
            anomaly_type: Type of anomaly
            acceptance_rate: Current acceptance rate (0.0-1.0)
            sample_count: Number of samples in window
            window: Time window used ("realtime", "short", "medium", "long")
            
        Returns:
            ThresholdAdjustment if threshold was changed, None otherwise
        """
        # Get minimum samples for this window
        min_samples = {
            "realtime": self.config.min_samples_realtime,
            "short": self.config.min_samples_short,
            "medium": self.config.min_samples_medium,
            "long": self.config.min_samples_long
        }.get(window, self.config.min_samples_short)
        
        # Not enough samples
        if sample_count < min_samples:
            return None
        
        # Get current threshold
        current_threshold = self.get_threshold(anomaly_type)
        
        # Calculate adjustment
        adjustment, reason = self._calculate_adjustment(
            acceptance_rate,
            current_threshold
        )
        
        if adjustment == 0:
            return None
        
        # Apply adjustment
        new_threshold = current_threshold + adjustment
        new_threshold = max(self.MIN_THRESHOLD, min(self.MAX_THRESHOLD, new_threshold))
        
        # Record adjustment
        adj_record = ThresholdAdjustment(
            anomaly_type=anomaly_type,
            old_threshold=current_threshold,
            new_threshold=new_threshold,
            reason=reason,
            timestamp=time.time(),
            acceptance_rate=acceptance_rate,
            sample_count=sample_count
        )
        
        # Update threshold
        self.thresholds[anomaly_type] = new_threshold
        self._persist_thresholds()
        
        # Save to history
        self._save_adjustment(adj_record)
        
        self.logger.info(
            f"Adjusted threshold for {anomaly_type}: "
            f"{current_threshold:.3f} → {new_threshold:.3f} "
            f"(acceptance_rate={acceptance_rate:.3f}, samples={sample_count}, "
            f"reason={reason})"
        )
        
        return adj_record
    
    def _calculate_adjustment(
        self,
        acceptance_rate: float,
        current_threshold: float
    ) -> Tuple[float, str]:
        """
        Calculate threshold adjustment based on acceptance rate.
        
        Returns:
            (adjustment, reason)
        """
        # Acceptance rate too low - increase threshold (be more selective)
        if acceptance_rate < self.config.acceptance_rate_critical:
            adjustment = self.ADJUSTMENT_FACTOR * 2.0  # 10% increase
            reason = f"critical_acceptance_rate<{self.config.acceptance_rate_critical}"
        
        elif acceptance_rate < self.config.acceptance_rate_low:
            adjustment = self.ADJUSTMENT_FACTOR  # 5% increase
            reason = f"low_acceptance_rate<{self.config.acceptance_rate_low}"
        
        elif acceptance_rate < self.config.acceptance_rate_target_min:
            adjustment = self.ADJUSTMENT_FACTOR / 2.0  # 2.5% increase
            reason = f"below_target_min<{self.config.acceptance_rate_target_min}"
        
        # Acceptance rate too high - decrease threshold (detect more)
        elif acceptance_rate > self.config.acceptance_rate_high:
            adjustment = -self.ADJUSTMENT_FACTOR * 2.0  # 10% decrease
            reason = f"high_acceptance_rate>{self.config.acceptance_rate_high}"
        
        elif acceptance_rate > self.config.acceptance_rate_target_max:
            adjustment = -self.ADJUSTMENT_FACTOR  # 5% decrease
            reason = f"above_target_max>{self.config.acceptance_rate_target_max}"
        
        # Within target range - no adjustment
        else:
            adjustment = 0.0
            reason = "within_target_range"
        
        # Clamp adjustment
        if adjustment != 0:
            adjustment = max(-self.MAX_ADJUSTMENT, min(self.MAX_ADJUSTMENT, adjustment))
            adjustment = max(self.MIN_ADJUSTMENT, abs(adjustment)) * (1 if adjustment > 0 else -1)
        
        return adjustment, reason
    
    def auto_adjust_all(self, window: str = "short") -> Dict[str, Optional[ThresholdAdjustment]]:
        """
        Auto-adjust thresholds for all anomaly types based on recent metrics.
        
        Args:
            window: Time window to use ("realtime", "short", "medium", "long")
            
        Returns:
            Dict mapping anomaly_type to ThresholdAdjustment (or None if no change)
        """
        adjustments = {}
        
        for anomaly_type in self.thresholds.keys():
            try:
                # Get metrics for this anomaly type
                metrics = self.tracker.get_acceptance_metrics_by_type(
                    anomaly_type,
                    window_name=window
                )
                
                if not metrics:
                    continue
                
                # Adjust based on acceptance rate
                adjustment = self.adjust_threshold(
                    anomaly_type,
                    metrics.acceptance_rate,
                    metrics.total_published,
                    window
                )
                
                adjustments[anomaly_type] = adjustment
                
            except Exception as e:
                self.logger.error(f"Failed to adjust {anomaly_type}: {e}")
                adjustments[anomaly_type] = None
        
        return adjustments
    
    def _save_adjustment(self, adjustment: ThresholdAdjustment):
        """Save adjustment to history."""
        try:
            key = f"{self.REDIS_HISTORY}:{adjustment.anomaly_type}:{int(adjustment.timestamp)}"
            self.storage.set(key, adjustment.to_dict())
        except Exception as e:
            self.logger.error(f"Failed to save adjustment: {e}")
    
    def get_adjustment_history(
        self,
        anomaly_type: Optional[str] = None,
        limit: int = 100
    ) -> list[Dict[str, Any]]:
        """
        Get threshold adjustment history.
        
        Args:
            anomaly_type: Filter by anomaly type (None for all)
            limit: Maximum number of records to return
            
        Returns:
            List of adjustment records
        """
        try:
            pattern = f"{self.REDIS_HISTORY}:{anomaly_type}:*" if anomaly_type else f"{self.REDIS_HISTORY}:*"
            keys = self.storage.keys_by_pattern(pattern)
            
            # Get records
            records = []
            for key in keys[:limit]:
                data = self.storage.get(key)
                if data:
                    records.append(data)
            
            # Sort by timestamp (newest first)
            records.sort(key=lambda x: x.get("timestamp", 0), reverse=True)
            
            return records[:limit]
            
        except Exception as e:
            self.logger.error(f"Failed to get adjustment history: {e}")
            return []
    
    def get_stats(self) -> Dict[str, Any]:
        """Get threshold manager statistics."""
        return {
            "total_thresholds": len(self.thresholds),
            "thresholds": self.thresholds.copy(),
            "min_threshold": self.MIN_THRESHOLD,
            "max_threshold": self.MAX_THRESHOLD,
            "adjustment_factor": self.ADJUSTMENT_FACTOR,
            "recent_adjustments": len(self.get_adjustment_history(limit=10))
        }
    
    def reset_thresholds(self):
        """Reset all thresholds to defaults (for testing)."""
        self.thresholds = {
            "ddos": 0.85,
            "malware": 0.90,
            "port_scan": 0.80,
            "data_exfil": 0.88
        }
        self._persist_thresholds()
        self.logger.info("Reset all thresholds to defaults")
