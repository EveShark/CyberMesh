"""
FeedbackService - Backend to AI feedback loop orchestrator

Wires together:
- AnomalyLifecycleTracker
- ConfidenceCalibrator
- PolicyManager
- ThresholdManager
- AIConsumer (backend message consumer)

Security:
- All backend messages verified with Ed25519
- TLS encryption on Kafka
- Validator quorum required for policy updates
"""

import logging
import time
from typing import Optional, Callable
from .tracker import AnomalyLifecycleTracker
import os
from typing import TYPE_CHECKING

from .calibrator import ConfidenceCalibrator
from .policy_manager import PolicyManager
from .threshold_manager import ThresholdManager
from .storage import RedisStorage
from ..config.settings import Settings, FeedbackConfig
from ..contracts import PolicyUpdateEvent, CommitEvent

if TYPE_CHECKING:  # pragma: no cover
    from ..kafka.consumer import AIConsumer


class FeedbackService:
    """
    Orchestrates backend to AI feedback loop.
    
    Flow:
    1. AIConsumer receives backend messages
    2. Tracker updates anomaly lifecycle
    3. Calibrator retrains on validator feedback
    4. ThresholdManager adjusts detection thresholds
    5. PolicyManager applies validator config updates
    """
    
    def __init__(
        self,
        config: Settings,
        logger: Optional[logging.Logger] = None,
        *,
        extra_commit_handler: Optional[Callable[[CommitEvent], None]] = None,
    ):
        self.config = config
        self.logger = logger or logging.getLogger(__name__)
        
        feedback_cfg = getattr(config, "feedback", None)
        if feedback_cfg is None:
            env_flag = os.getenv("FEEDBACK_DISABLE_PERSISTENCE")
            disable_persistence = False
            if env_flag is not None:
                disable_persistence = env_flag.lower() in ("true", "1", "yes", "on")
            feedback_cfg = FeedbackConfig(disable_persistence=disable_persistence)
        else:
            disable_persistence = getattr(feedback_cfg, "disable_persistence", False)

        self.storage = RedisStorage(disabled=disable_persistence)
        self.feedback_config = feedback_cfg
        
        self.tracker = AnomalyLifecycleTracker(
            self.storage,
            self.feedback_config,
            self.logger
        )

        if disable_persistence:
            self.logger.warning("Feedback persistence disabled; tracker data kept in-memory only")
        
        self.calibrator = ConfidenceCalibrator(
            self.tracker,
            self.storage,
            self.feedback_config,
            self.logger
        )
        
        self.policy_manager = PolicyManager(self.storage)
        
        self.threshold_manager = ThresholdManager(
            self.tracker,
            self.storage,
            self.feedback_config
        )
        
        from ..kafka.consumer import AIConsumer

        self.consumer = AIConsumer(
            config,
            tracker=self.tracker,
            logger=self.logger,
            storage=self.storage,
            feedback_config=self.feedback_config,
            disable_persistence=disable_persistence,
        )
        # Preserve the consumer's built-in commit handler so anomaly lifecycle
        # transitions to COMMITTED continue to flow through the tracker.
        self._tracker_commit_handler = self.consumer.handlers.get("commit")
        self._extra_commit_handler = extra_commit_handler
        
        self._register_handlers()
        
        self._running = False
        self._last_calibration_check = 0
        self._last_threshold_check = 0
        
        self.logger.info("FeedbackService initialized")
    
    def _register_handlers(self):
        """Register handlers for backend messages"""
        self.consumer.register_handler("policy_update", self._handle_policy_update)
        self.consumer.register_handler("commit", self._handle_commit)
    
    def _handle_policy_update(self, msg: PolicyUpdateEvent):
        """
        Handle policy update from backend validators.
        
        Applies:
        - Threshold updates
        - Blacklist/whitelist updates
        - Feature flag updates
        - Calibration config updates
        """
        try:
            current_height = msg.effective_height
            
            success = self.policy_manager.handle_policy_update(msg, current_height)
            
            if success:
                self.logger.info(
                    f"Applied policy {msg.policy_id} "
                    f"(type={msg.rule_type}, action={msg.action})"
                )
            else:
                self.logger.warning(
                    f"Failed to apply policy {msg.policy_id} "
                    f"(type={msg.rule_type}, action={msg.action})"
                )
        
        except Exception as e:
            self.logger.error(f"Policy update error: {e}", exc_info=True)
    
    def _handle_commit(self, msg):
        """
        Handle block commit from backend.
        
        Triggers:
        - Calibration check (should we retrain?)
        - Threshold adjustment check
        """
        tracker_errors = False
        if self._tracker_commit_handler:
            # Let the original tracker handler run first so any failures prevent
            # offset commits and the message is retried.
            try:
                self._tracker_commit_handler(msg)
            except Exception as exc:  # pragma: no cover - defensive logging
                tracker_errors = True
                if self.logger:
                    self.logger.error(
                        "Tracker commit handler failed",
                        exc_info=True,
                        extra={"error": str(exc)},
                    )
                raise

        if self._extra_commit_handler and not tracker_errors:
            try:
                self._extra_commit_handler(msg)
            except Exception as exc:  # pragma: no cover - defensive logging
                if self.logger:
                    self.logger.error(
                        "Secondary commit handler failed",
                        exc_info=True,
                        extra={"error": str(exc)},
                    )
                raise

        try:
            self._check_calibration()
            self._check_threshold_adjustment()
        except Exception as e:
            self.logger.error(f"Commit handling error: {e}", exc_info=True)
    
    def _check_calibration(self):
        """Check if calibrator should retrain"""
        now = time.time()
        
        if now - self._last_calibration_check < 60:
            return
        
        self._last_calibration_check = now
        
        try:
            if self.calibrator.should_retrain():
                self.logger.info("Triggering calibrator retraining")
                
                training_data = self.calibrator.collect_training_data()
                if training_data:
                    raw_scores, labels = training_data
                    success = self.calibrator.train(raw_scores, labels)
                    
                    if success:
                        stats = self.calibrator.get_stats()
                        self.logger.info(
                            f"Calibrator retrained: "
                            f"samples={stats['training_samples']}, "
                            f"improvement={stats.get('improvement', 0):.4f}"
                        )
        
        except Exception as e:
            self.logger.error(f"Calibration check error: {e}", exc_info=True)
    
    def _check_threshold_adjustment(self):
        """Check if thresholds should be adjusted"""
        now = time.time()
        
        if now - self._last_threshold_check < 300:
            return
        
        self._last_threshold_check = now
        
        try:
            adjustments = self.threshold_manager.auto_adjust_all(window="short")
            
            adjusted_count = sum(1 for adj in adjustments.values() if adj is not None)
            
            if adjusted_count > 0:
                self.logger.info(f"Adjusted {adjusted_count} thresholds")
                
                for anomaly_type, adj in adjustments.items():
                    if adj:
                        self.logger.info(
                            f"Threshold adjusted: {anomaly_type} "
                            f"{adj.old_threshold:.3f} -> {adj.new_threshold:.3f} "
                            f"(acceptance={adj.acceptance_rate:.3f}, reason={adj.reason})"
                        )
        
        except Exception as e:
            self.logger.error(f"Threshold adjustment error: {e}", exc_info=True)
    
    def start(self):
        """Start feedback service"""
        if self._running:
            return
        
        self._running = True
        self.consumer.start()
        
        self.logger.info(
            "FeedbackService started - consuming backend messages",
            extra={
                "topics": [
                    self.config.kafka_topics.control_commits,
                    self.config.kafka_topics.control_policy,
                    self.config.kafka_topics.control_evidence
                ]
            }
        )
    
    def stop(self):
        """Stop feedback service"""
        if not self._running:
            return
        
        self._running = False
        self.consumer.stop()
        
        self.logger.info("FeedbackService stopped")
    
    def get_calibrated_threshold(self, anomaly_type: str, default: float = 0.85) -> float:
        """
        Get current threshold with policy overrides applied.
        
        Priority:
        1. PolicyManager override (validator-set)
        2. ThresholdManager (auto-adjusted)
        3. Default
        """
        override_key = f"{anomaly_type}_confidence_threshold"
        
        if self.policy_manager.has_override(override_key):
            return self.policy_manager.get_override(override_key)
        
        return self.threshold_manager.get_threshold(anomaly_type, default)
    
    def calibrate_score(self, raw_score: float) -> float:
        """
        Calibrate raw model score using trained calibrator.
        
        Returns calibrated score in [0, 1].
        """
        import numpy as np
        return self.calibrator.calibrate(np.array([raw_score]))[0]
    
    def get_stats(self) -> dict:
        """Get feedback service statistics"""
        return {
            "consumer": self.consumer.get_metrics(),
            "tracker": self.tracker.get_stats(),
            "calibrator": self.calibrator.get_stats(),
            "policy_manager": self.policy_manager.get_stats(),
            "threshold_manager": self.threshold_manager.get_stats()
        }
