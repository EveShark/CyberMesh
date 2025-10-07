"""
Message handlers for control messages from backend.

Handles messages from control.* topics:
- control.commits.v1 - Committed blocks
- control.reputation.v1 - Reputation updates
- control.policy.v1 - Policy updates
- control.evidence.v1 - Evidence requests

Security:
- All messages pre-verified (signature + validation) by consumer
- Handlers log safely (no secrets)
- Errors don't crash consumer thread
- Metrics tracked for monitoring
"""
import threading
from typing import Dict, Any
from ..contracts import (
    CommitEvent,
    ReputationEvent,
    PolicyUpdateEvent,
    EvidenceRequestEvent,
)
from ..logging import get_logger


class MessageHandlers:
    """
    Handlers for control messages from backend.
    
    Phase 3: Log received messages (structured logging)
    Future phases: Add actual processing logic
    
    Security:
    - Messages already signature-verified before reaching handlers
    - Defensive: Additional validation in each handler
    - Safe logging: No secrets, structured format
    - Error isolation: Handler failure doesn't crash consumer
    
    Metrics:
    - Tracks processed/failed count per handler type
    - Accessible via get_metrics()
    """
    
    def __init__(self, logger=None, service_manager=None):
        """
        Initialize message handlers.
        
        Args:
            logger: Logger instance (optional, creates one if not provided)
            service_manager: ServiceManager instance (for accessing pipeline, publisher)
        """
        self.logger = logger or get_logger("message_handlers")
        self.service_manager = service_manager
        
        # Metrics per handler type
        self._metrics = {
            "commit": {"processed": 0, "failed": 0},
            "reputation": {"processed": 0, "failed": 0},
            "policy_update": {"processed": 0, "failed": 0},
            "evidence_request": {"processed": 0, "failed": 0},
        }
        self._metrics_lock = threading.Lock()
    
    def handle_commit_event(self, event: CommitEvent):
        """
        Handle committed block from backend.
        
        Args:
            event: Validated and signature-verified CommitEvent
            
        Phase 6.1: Run ML detection pipeline on commit event
        - Load telemetry data
        - Extract features
        - Run Rules + Math engines
        - Publish anomaly if detected
        """
        try:
            self.logger.info(
                "Block committed by backend",
                extra={
                    "event_type": "commit",
                    "height": event.height,
                    "block_hash": event.block_hash[:16].hex() if event.block_hash else None,
                    "timestamp": event.timestamp,
                    "validator_id": event.validator_id,
                    "transaction_count": event.transaction_count,
                }
            )
            
            # Phase 6.1: Run ML detection pipeline
            if self.service_manager and self.service_manager.detection_pipeline:
                import uuid
                self._run_detection_pipeline(event)
            
            with self._metrics_lock:
                self._metrics["commit"]["processed"] += 1
                
        except Exception as e:
            with self._metrics_lock:
                self._metrics["commit"]["failed"] += 1
            
            self.logger.error(
                "Failed to handle commit event",
                exc_info=True,
                extra={
                    "event_type": "commit",
                    "error": str(e),
                }
            )
    
    def _run_detection_pipeline(self, event: CommitEvent):
        """
        Run ML detection pipeline and publish anomaly if detected.
        
        Args:
            event: Commit event that triggered detection
        """
        try:
            # Run pipeline
            result = self.service_manager.detection_pipeline.process(
                trigger_event={'commit_height': event.height}
            )
            
            # Record metrics
            if self.service_manager.ml_metrics and result.decision:
                self.service_manager.ml_metrics.record_pipeline_result(
                    decision=result.decision,
                    latencies=result.latency_ms,
                    total_latency=result.total_latency_ms,
                    feature_count=result.feature_count,
                    candidate_count=result.candidate_count
                )
            
            # Check if we should publish
            if result.decision and result.decision.should_publish:
                import uuid
                self._publish_detection(result, event)
            elif result.decision:
                self.logger.info(
                    "Detection abstained",
                    extra={
                        'reason': result.decision.abstention_reason,
                        'score': result.decision.final_score,
                        'confidence': result.decision.confidence
                    }
                )
            
            # Log latency warnings
            if result.total_latency_ms > 50.0:
                self.logger.warning(
                    f"Pipeline latency exceeded target: {result.total_latency_ms:.2f}ms > 50ms"
                )
                
        except Exception as e:
            self.logger.error(f"Detection pipeline failed: {e}", exc_info=True)
    
    def _publish_detection(self, result, event: CommitEvent):
        """
        Publish anomaly detection to backend.
        
        Args:
            result: InstrumentedResult from pipeline
            event: Original commit event
        """
        import uuid
        decision = result.decision
        evidence_bytes = decision.metadata.get('evidence', b'')
        
        # Map score to severity (1-10)
        severity = max(1, min(10, int(decision.final_score * 10)))
        
        # Publish anomaly
        anomaly_id = self.service_manager.publisher.publish_anomaly(
            anomaly_id=str(uuid.uuid4()),
            anomaly_type=decision.threat_type.value,
            source='ml_pipeline',
            severity=severity,
            confidence=decision.confidence,
            payload=evidence_bytes,
            model_version='phase_6.1'
        )
        
        self.logger.info(
            "Published anomaly detection",
            extra={
                'anomaly_id': anomaly_id,
                'threat_type': decision.threat_type.value,
                'score': decision.final_score,
                'confidence': decision.confidence,
                'llr': decision.llr,
                'latency_ms': result.total_latency_ms
            }
        )
    
    def handle_reputation_event(self, event: ReputationEvent):
        """
        Handle reputation update from backend.
        
        Args:
            event: Validated and signature-verified ReputationEvent
            
        Phase 3: Log the reputation update
        Future: Update internal reputation model, adjust trust scores
        """
        try:
            self.logger.info(
                "Reputation updated by backend",
                extra={
                    "event_type": "reputation",
                    "node_id": event.node_id,
                    "new_score": event.new_score,
                    "old_score": event.old_score,
                    "reason": event.reason,
                    "timestamp": event.timestamp,
                }
            )
            
            with self._metrics_lock:
                self._metrics["reputation"]["processed"] += 1
                
        except Exception as e:
            with self._metrics_lock:
                self._metrics["reputation"]["failed"] += 1
            
            self.logger.error(
                "Failed to handle reputation event",
                exc_info=True,
                extra={
                    "event_type": "reputation",
                    "error": str(e),
                }
            )
    
    def handle_policy_update_event(self, event: PolicyUpdateEvent):
        """
        Handle policy update from backend.
        
        Args:
            event: Validated and signature-verified PolicyUpdateEvent
            
        Phase 3: Log the policy update
        Future: Update validation rules, reload detection models
        """
        try:
            self.logger.info(
                "Policy updated by backend",
                extra={
                    "event_type": "policy_update",
                    "policy_id": event.policy_id,
                    "policy_version": event.version,
                    "policy_type": event.policy_type,
                    "action": event.action,  # "add", "update", "remove"
                    "timestamp": event.timestamp,
                }
            )
            
            with self._metrics_lock:
                self._metrics["policy_update"]["processed"] += 1
                
        except Exception as e:
            with self._metrics_lock:
                self._metrics["policy_update"]["failed"] += 1
            
            self.logger.error(
                "Failed to handle policy update event",
                exc_info=True,
                extra={
                    "event_type": "policy_update",
                    "error": str(e),
                }
            )
    
    def handle_evidence_request_event(self, event: EvidenceRequestEvent):
        """
        Handle evidence request from backend.
        
        Args:
            event: Validated and signature-verified EvidenceRequestEvent
            
        Phase 3: Log the evidence request
        Future: Generate evidence, package with chain-of-custody, send back
        """
        try:
            self.logger.info(
                "Evidence requested by backend",
                extra={
                    "event_type": "evidence_request",
                    "request_id": event.request_id,
                    "anomaly_id": event.anomaly_id,
                    "evidence_types": event.evidence_types,
                    "priority": event.priority,
                    "deadline": event.deadline,
                    "requested_by": event.requested_by,
                }
            )
            
            with self._metrics_lock:
                self._metrics["evidence_request"]["processed"] += 1
                
        except Exception as e:
            with self._metrics_lock:
                self._metrics["evidence_request"]["failed"] += 1
            
            self.logger.error(
                "Failed to handle evidence request event",
                exc_info=True,
                extra={
                    "event_type": "evidence_request",
                    "error": str(e),
                }
            )
    
    def get_metrics(self) -> Dict[str, Any]:
        """
        Get handler metrics.
        
        Returns:
            Dictionary with processed/failed counts per handler type
        """
        with self._metrics_lock:
            return {
                handler_type: {
                    "processed": counts["processed"],
                    "failed": counts["failed"],
                }
                for handler_type, counts in self._metrics.items()
            }
