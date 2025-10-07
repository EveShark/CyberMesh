"""
Confidence calibration using validator feedback.

Retrains calibration models (isotonic regression or Platt scaling) based on
validator acceptance/rejection decisions to improve confidence score reliability.
"""

import time
import pickle
import joblib
import numpy as np
from pathlib import Path
from typing import Optional, Tuple, List, Dict
from sklearn.isotonic import IsotonicRegression
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import brier_score_loss

from ..utils.errors import ValidationError, StorageError
from ..config.settings import FeedbackConfig
from .storage import RedisStorage
from .tracker import AnomalyLifecycleTracker, AnomalyState
import logging


class ConfidenceCalibrator:
    """
    Calibrates confidence scores using validator feedback.
    
    Process:
    1. Collect (raw_score, validator_accepted) pairs from tracker
    2. Train isotonic regression or Platt scaling
    3. Persist model to Redis + filesystem
    4. Apply calibration to new predictions
    
    Retraining triggers:
    - Minimum samples reached (default: 1000)
    - Acceptance rate drops below threshold (default: 60%)
    - Time since last training exceeds interval (default: 1 hour)
    
    Model persistence:
    - Hot model in Redis (fast access)
    - Versioned backups on filesystem (reliability)
    """
    
    def __init__(
        self,
        tracker: AnomalyLifecycleTracker,
        storage: RedisStorage,
        config: FeedbackConfig,
        logger: Optional[logging.Logger] = None
    ):
        self.tracker = tracker
        self.storage = storage
        self.config = config
        self.logger = logger or logging.getLogger("calibrator")
        
        # Calibration model (isotonic or logistic)
        self.model: Optional[IsotonicRegression | LogisticRegression] = None
        self.model_version: int = 0
        self.last_train_time: float = 0.0
        self.training_samples: int = 0
        
        # Model performance tracking
        self.brier_score_before: Optional[float] = None
        self.brier_score_after: Optional[float] = None
        
        # Load existing model if available
        self._load_model()
    
    def calibrate(self, raw_scores: np.ndarray) -> np.ndarray:
        """
        Apply calibration to raw confidence scores.
        
        Args:
            raw_scores: Raw model outputs [0, 1], shape (n,) or (n, 1)
        
        Returns:
            Calibrated probabilities [0, 1], same shape as input
        
        Raises:
            ValidationError: If input invalid
        """
        if self.model is None:
            self.logger.warning("No calibration model available, returning raw scores")
            return raw_scores
        
        # Validate input
        if not isinstance(raw_scores, np.ndarray):
            raw_scores = np.array(raw_scores)
        
        if len(raw_scores.shape) == 2 and raw_scores.shape[1] == 1:
            raw_scores = raw_scores.flatten()
        
        if len(raw_scores) == 0:
            return raw_scores
        
        # Clip to valid range
        raw_scores = np.clip(raw_scores, 0.0, 1.0)
        
        try:
            # Apply calibration (handle different model types)
            if self.config.calibration_method == "platt":
                # LogisticRegression needs 2D input
                X = raw_scores.reshape(-1, 1)
                calibrated = self.model.predict_proba(X)[:, 1]
            else:
                # IsotonicRegression handles 1D
                calibrated = self.model.predict(raw_scores)
            
            # Ensure output in valid range
            calibrated = np.clip(calibrated, 0.0, 1.0)
            
            return calibrated
            
        except Exception as e:
            self.logger.error(f"Calibration failed: {e}, returning raw scores")
            return raw_scores
    
    def collect_training_data(
        self,
        lookback_seconds: Optional[int] = None
    ) -> Tuple[np.ndarray, np.ndarray]:
        """
        Collect training data from tracker.
        
        Queries tracker for anomalies with:
        - raw_score available
        - Final state: COMMITTED (label=1) or REJECTED (label=0)
        
        Args:
            lookback_seconds: How far back to query (None = all data)
        
        Returns:
            Tuple of (raw_scores, labels) arrays
            - raw_scores: shape (n,)
            - labels: shape (n,), 1 = accepted, 0 = rejected
        """
        raw_scores = []
        labels = []
        
        # Query time range
        now = time.time()
        start_time = now - lookback_seconds if lookback_seconds else 0
        
        # Get all anomaly IDs from timeline
        try:
            # Get committed anomalies
            committed_ids = self.storage._client.zrangebyscore(
                f"anomaly:timeline:{AnomalyState.COMMITTED.value}",
                start_time,
                now
            )
            
            for anomaly_id in committed_ids:
                lifecycle = self.tracker.get_lifecycle(anomaly_id)
                if lifecycle and lifecycle.raw_score is not None:
                    raw_scores.append(lifecycle.raw_score)
                    labels.append(1)  # Accepted
            
            # Get rejected anomalies
            rejected_ids = self.storage._client.zrangebyscore(
                f"anomaly:timeline:{AnomalyState.REJECTED.value}",
                start_time,
                now
            )
            
            for anomaly_id in rejected_ids:
                lifecycle = self.tracker.get_lifecycle(anomaly_id)
                if lifecycle and lifecycle.raw_score is not None:
                    raw_scores.append(lifecycle.raw_score)
                    labels.append(0)  # Rejected
            
            if len(raw_scores) == 0:
                self.logger.warning("No training data available (raw_score missing)")
                return np.array([]), np.array([])
            
            self.logger.info(
                f"Collected {len(raw_scores)} samples "
                f"({sum(labels)} accepted, {len(labels) - sum(labels)} rejected)"
            )
            
            return np.array(raw_scores), np.array(labels)
            
        except Exception as e:
            raise StorageError(f"Failed to collect training data: {e}") from e
    
    def train(
        self,
        raw_scores: Optional[np.ndarray] = None,
        labels: Optional[np.ndarray] = None
    ) -> bool:
        """
        Train calibration model on validator feedback.
        
        Args:
            raw_scores: Raw model outputs (optional, will collect if None)
            labels: Validator decisions 1=accepted, 0=rejected (optional)
        
        Returns:
            True if training successful
        
        Raises:
            ValidationError: If insufficient data or invalid input
            StorageError: If model persistence fails
        """
        # Collect data if not provided
        if raw_scores is None or labels is None:
            raw_scores, labels = self.collect_training_data()
        
        # Validate minimum samples
        if len(raw_scores) < self.config.calibration_min_samples:
            raise ValidationError(
                f"Insufficient samples for training: {len(raw_scores)} "
                f"< {self.config.calibration_min_samples}"
            )
        
        # Validate input shapes
        if len(raw_scores) != len(labels):
            raise ValidationError(
                f"Shape mismatch: raw_scores={len(raw_scores)}, labels={len(labels)}"
            )
        
        # Check for class imbalance
        accepted = np.sum(labels)
        rejected = len(labels) - accepted
        
        if accepted == 0 or rejected == 0:
            raise ValidationError(
                f"Need both classes for calibration: accepted={accepted}, rejected={rejected}"
            )
        
        self.logger.info(
            f"Training calibration model: method={self.config.calibration_method}, "
            f"samples={len(raw_scores)}, accepted={accepted}, rejected={rejected}"
        )
        
        try:
            # Calculate pre-calibration Brier score
            self.brier_score_before = brier_score_loss(labels, raw_scores)
            
            # Train model
            if self.config.calibration_method == "isotonic":
                self.model = IsotonicRegression(out_of_bounds="clip")
                self.model.fit(raw_scores, labels)
                
            elif self.config.calibration_method == "platt":
                self.model = LogisticRegression()
                # Reshape for sklearn
                X = raw_scores.reshape(-1, 1)
                self.model.fit(X, labels)
            
            else:
                raise ValidationError(f"Unknown calibration method: {self.config.calibration_method}")
            
            # Calculate post-calibration Brier score
            calibrated_scores = self.calibrate(raw_scores)
            self.brier_score_after = brier_score_loss(labels, calibrated_scores)
            
            # Update metadata
            self.model_version += 1
            self.last_train_time = time.time()
            self.training_samples = len(raw_scores)
            
            # Persist model
            self._save_model()
            
            improvement = self.brier_score_before - self.brier_score_after
            self.logger.info(
                f"Calibration training complete: version={self.model_version}, "
                f"brier_before={self.brier_score_before:.4f}, "
                f"brier_after={self.brier_score_after:.4f}, "
                f"improvement={improvement:.4f}"
            )
            
            return True
            
        except Exception as e:
            self.logger.error(f"Training failed: {e}")
            raise
    
    def should_retrain(self) -> Tuple[bool, str]:
        """
        Check if retraining is needed.
        
        Checks:
        1. Enough new samples available
        2. Acceptance rate dropped below threshold
        3. Time since last training exceeds interval
        
        Returns:
            Tuple of (should_retrain, reason)
        """
        # Check time since last training
        time_since_train = time.time() - self.last_train_time
        if time_since_train > self.config.calibration_retrain_interval:
            return True, f"time_interval_exceeded_{int(time_since_train)}s"
        
        # Check if we have enough new samples
        raw_scores, labels = self.collect_training_data()
        new_samples = len(raw_scores) - self.training_samples
        
        if new_samples >= self.config.calibration_min_samples:
            return True, f"new_samples_available_{new_samples}"
        
        # Check acceptance rate
        try:
            metrics = self.tracker.get_acceptance_metrics("realtime")
            if metrics and metrics.has_minimum_samples:
                if metrics.acceptance_rate < self.config.calibration_acceptance_threshold:
                    return True, f"low_acceptance_rate_{metrics.acceptance_rate:.2%}"
        except Exception as e:
            self.logger.warning(f"Failed to check acceptance rate: {e}")
        
        return False, "no_retrain_needed"
    
    def _save_model(self):
        """Save calibration model to Redis and filesystem."""
        if self.model is None:
            return
        
        try:
            # Serialize model
            model_bytes = pickle.dumps({
                'model': self.model,
                'version': self.model_version,
                'method': self.config.calibration_method,
                'trained_at': self.last_train_time,
                'training_samples': self.training_samples,
                'brier_score_before': self.brier_score_before,
                'brier_score_after': self.brier_score_after
            })
            
            # Save to Redis (hot model) - use SET with binary data
            if self.config.calibration_save_to_redis:
                # Redis decode_responses=True doesn't work for binary data
                # Use raw client without decode_responses
                import redis
                redis_url = f"rediss://default:{self.storage.password}@{self.storage.host}:{self.storage.port}/{self.storage.db}"
                raw_client = redis.from_url(redis_url, decode_responses=False, ssl_cert_reqs=None)
                raw_client.set(self.config.calibration_redis_key, model_bytes)
                raw_client.close()
                self.logger.info(f"Saved model to Redis: {self.config.calibration_redis_key}")
            
            # Save to filesystem (versioned backup)
            model_dir = Path(self.config.calibration_model_path)
            model_dir.mkdir(parents=True, exist_ok=True)
            
            model_file = model_dir / f"calibration_v{self.model_version}.pkl"
            with open(model_file, 'wb') as f:
                f.write(model_bytes)
            
            # Also save as "latest"
            latest_file = model_dir / "calibration_latest.pkl"
            with open(latest_file, 'wb') as f:
                f.write(model_bytes)
            
            self.logger.info(f"Saved model to filesystem: {model_file}")
            
        except Exception as e:
            raise StorageError(f"Failed to save model: {e}") from e
    
    def _load_model(self):
        """Load calibration model from Redis or filesystem."""
        model_data = None
        
        # Try Redis first (fastest) - use raw client for binary data
        if self.config.calibration_save_to_redis:
            try:
                import redis
                redis_url = f"rediss://default:{self.storage.password}@{self.storage.host}:{self.storage.port}/{self.storage.db}"
                raw_client = redis.from_url(redis_url, decode_responses=False, ssl_cert_reqs=None)
                model_bytes = raw_client.get(self.config.calibration_redis_key)
                raw_client.close()
                
                if model_bytes:
                    model_data = pickle.loads(model_bytes)
                    self.logger.info("Loaded model from Redis")
            except Exception as e:
                self.logger.warning(f"Failed to load model from Redis: {e}")
        
        # Fallback to filesystem
        if model_data is None:
            try:
                model_file = Path(self.config.calibration_model_path) / "calibration_latest.pkl"
                if model_file.exists():
                    with open(model_file, 'rb') as f:
                        model_data = pickle.load(f)
                    self.logger.info(f"Loaded model from filesystem: {model_file}")
            except Exception as e:
                self.logger.warning(f"Failed to load model from filesystem: {e}")
        
        # Apply loaded model
        if model_data:
            self.model = model_data['model']
            self.model_version = model_data['version']
            self.last_train_time = model_data['trained_at']
            self.training_samples = model_data['training_samples']
            self.brier_score_before = model_data.get('brier_score_before')
            self.brier_score_after = model_data.get('brier_score_after')
            
            self.logger.info(
                f"Calibration model loaded: version={self.model_version}, "
                f"samples={self.training_samples}, "
                f"method={self.config.calibration_method}"
            )
        else:
            self.logger.info("No existing calibration model found, will train on first use")
    
    def get_stats(self) -> Dict:
        """Get calibrator statistics."""
        return {
            "model_version": self.model_version,
            "model_loaded": self.model is not None,
            "method": self.config.calibration_method,
            "last_train_time": self.last_train_time,
            "training_samples": self.training_samples,
            "brier_score_before": self.brier_score_before,
            "brier_score_after": self.brier_score_after,
            "improvement": (self.brier_score_before - self.brier_score_after) 
                          if self.brier_score_before and self.brier_score_after else None
        }
