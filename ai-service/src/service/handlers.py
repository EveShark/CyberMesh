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
from typing import Dict, Any, Optional, TYPE_CHECKING
from ..contracts import (
    CommitEvent,
    ReputationEvent,
    PolicyUpdateEvent,
    PolicyAckEvent,
    EvidenceRequestEvent,
)
from ..logging import get_logger
import json
import time
import hashlib
from ..utils.tokens import token_ip, token_flow_key
from ..contracts import EvidenceMessage
from ..utils.errors import ValidationError, StorageError

if TYPE_CHECKING:  # pragma: no cover
    from ..feedback.tracker import AnomalyLifecycleTracker


def _extract_raw_score(decision) -> Optional[float]:
    if not decision or not getattr(decision, "candidates", None):
        return None

    try:
        from ..ml.types import DetectionCandidate  # local import to avoid cycle

        typed_candidates = [
            c for c in decision.candidates
            if isinstance(c, DetectionCandidate) and c.threat_type == decision.threat_type
        ]
        target = typed_candidates[0] if typed_candidates else decision.candidates[0]
        raw = getattr(target, "raw_score", None)
        if raw is None:
            return None
        return float(raw)
    except Exception:
        return None


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
            "policy_ack": {"processed": 0, "failed": 0, "applied": 0, "failed_result": 0, "missing_mapping": 0},
            "evidence_request": {"processed": 0, "failed": 0},
        }
        self._metrics_lock = threading.Lock()

    def _tracker(self) -> Optional["AnomalyLifecycleTracker"]:
        if not self.service_manager or not getattr(self.service_manager, "feedback_service", None):
            return None
        return getattr(self.service_manager.feedback_service, "tracker", None)

    def _record_lifecycle_events(
        self,
        *,
        anomaly_id: str,
        anomaly_type: str,
        severity: int,
        confidence: float,
        raw_score: Optional[float],
        timestamp: float,
    ) -> None:
        tracker = self._tracker()
        if tracker is None:
            return

        try:
            tracker.record_detected(
                anomaly_id=anomaly_id,
                anomaly_type=anomaly_type,
                confidence=confidence,
                severity=severity,
                raw_score=raw_score,
                timestamp=timestamp,
            )
        except Exception as err:
            self.logger.warning(
                "Tracker detected-state failure",
                extra={"anomaly_id": anomaly_id, "error": str(err)}
            )
            return

        try:
            tracker.record_published(anomaly_id=anomaly_id, timestamp=timestamp)
        except Exception as err:
            self.logger.warning(
                "Tracker published-state failure",
                extra={"anomaly_id": anomaly_id, "error": str(err)}
            )
    
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
                    "producer_id": event.producer_id.hex() if event.producer_id else None,
                    "tx_count": event.tx_count,
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
            if self.service_manager.ml_metrics:
                self.service_manager.ml_metrics.record_pipeline_result(
                    decision=result.decision,
                    latencies=result.latency_ms,
                    total_latency=result.total_latency_ms,
                    feature_count=result.feature_count,
                    candidate_count=result.candidate_count
                )

            # Persist detection history for API consumers
            if self.service_manager and result.decision:
                decision = result.decision
                severity = max(1, min(10, int(decision.final_score * 10)))
                block_hash_prefix = (
                    event.block_hash[:8].hex() if getattr(event, "block_hash", None) else None
                )
                self.service_manager.record_detection_event(
                    source="commit",
                    validator_id=getattr(event, "producer_id", None),
                    threat_type=decision.threat_type.value if decision.threat_type else "unknown",
                    severity=severity,
                    confidence=decision.confidence,
                    final_score=decision.final_score,
                    should_publish=decision.should_publish,
                    metadata={
                        "block_height": getattr(event, "height", None),
                        "block_hash": block_hash_prefix,
                        "transaction_count": getattr(event, "transaction_count", None),
                        "abstention_reason": decision.abstention_reason,
                    },
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
        
        # Map score to severity (1-10)
        severity = max(1, min(10, int(decision.final_score * 10)))

        # Derive tokens from minimal network context (no PII in anomaly)
        net_ctx = (decision.metadata or {}).get('network_context') or {}
        src_ip = net_ctx.get('src_ip')
        dst_ip = net_ctx.get('dst_ip')
        src_port = net_ctx.get('src_port')
        dst_port = net_ctx.get('dst_port')
        proto = net_ctx.get('protocol')

        source_token = token_ip(src_ip) if src_ip is not None else None
        target_token = token_ip(dst_ip) if dst_ip is not None else None
        flow_token = token_flow_key(src_ip, dst_ip, src_port, dst_port, proto)

        # Model version best-effort
        model_version = (decision.metadata or {}).get('model_version') or 'phase_6.1'

        # Detection timestamp from commit event if available
        detection_ts = getattr(event, 'timestamp', None) or int(time.time())

        # Build JSON payload with required metadata + pseudonymous tokens
        anomaly_uuid = str(uuid.uuid4())
        payload_obj = {
            'anomaly_id': anomaly_uuid,
            'detection_timestamp': int(detection_ts),
            'threat_type': decision.threat_type.value if decision.threat_type else 'unknown',
            'severity': int(severity),
            'confidence': float(decision.confidence),
            'final_score': float(decision.final_score),
            'llr': float(decision.llr),
            'source_token': source_token,
            'target_token': target_token,
            'flow_key': flow_token,
            'model_version': model_version,
        }
        payload_bytes = json.dumps(payload_obj, separators=(",", ":")).encode('utf-8')

        # Pre-compute anomaly content hash (for evidence linking)
        anomaly_content_hash = hashlib.sha256(payload_bytes).digest()

        raw_score = _extract_raw_score(decision)

        # Publish anomaly (payload must be JSON)
        message_id = None
        try:
            message_id = self.service_manager.publisher.publish_anomaly(
                anomaly_id=anomaly_uuid,
                anomaly_type=decision.threat_type.value,
                source='ml_pipeline',
                severity=severity,
                confidence=decision.confidence,
                payload=payload_bytes,
                model_version=model_version
            )
        except Exception as err:
            self.logger.error(
                "Failed to publish anomaly detection",
                exc_info=True,
                extra={
                    'anomaly_id': anomaly_uuid,
                    'error': str(err)
                }
            )
            return
        
        self.logger.info(
            "Published anomaly detection",
            extra={
                'anomaly_id': anomaly_uuid,
                'message_id': message_id,
                'threat_type': decision.threat_type.value,
                'score': decision.final_score,
                'confidence': decision.confidence,
                'llr': decision.llr,
                'latency_ms': result.total_latency_ms
            }
        )

        self._record_lifecycle_events(
            anomaly_id=anomaly_uuid,
            anomaly_type=decision.threat_type.value if decision.threat_type else 'unknown',
            severity=int(severity),
            confidence=float(decision.confidence),
            raw_score=raw_score,
            timestamp=float(detection_ts),
        )

        # Publish supporting evidence linked to anomaly (Option 1)
        try:
            ev_bytes = (decision.metadata or {}).get('evidence')
            if ev_bytes:
                ev_msg = EvidenceMessage.from_bytes(ev_bytes)
                # Re-publish evidence with explicit ref to anomaly content hash
                self.service_manager.publisher.publish_evidence(
                    evidence_id=str(uuid.uuid4()),
                    evidence_type='ml_detection',
                    refs=[anomaly_content_hash],
                    proof_blob=ev_msg.proof_blob,
                    chain_of_custody=ev_msg.chain_of_custody,
                )
                self.logger.info(
                    "Published supporting evidence",
                    extra={'anomaly_id': anomaly_uuid}
                )
        except Exception as e:
            # Do not fail handler on evidence publication errors
            self.logger.error("Failed to publish evidence", exc_info=True, extra={'error': str(e)})
    
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

    def handle_policy_ack_event(self, event: PolicyAckEvent):
        """Handle enforcement acknowledgement from backend agents."""

        tracker = self._tracker()
        anomaly_id: Optional[str] = None
        ack_latency_ms: Optional[float] = None

        if event.applied_at and event.acked_at:
            ack_latency_ms = max(0.0, (event.acked_at - event.applied_at) * 1000.0)

        if tracker is not None:
            try:
                anomaly_id = tracker.record_policy_ack(
                    policy_id=event.policy_id,
                    result=event.result,
                    reason=event.reason,
                    error_code=event.error_code,
                    applied_at=event.applied_at if event.applied_at else None,
                    acked_at=event.acked_at if event.acked_at else None,
                    fast_path=event.fast_path,
                )
            except (ValidationError, StorageError) as err:
                with self._metrics_lock:
                    self._metrics["policy_ack"]["failed"] += 1

                self.logger.error(
                    "Failed to persist policy acknowledgement",
                    exc_info=True,
                    extra={
                        "event_type": "policy_ack",
                        "policy_id": event.policy_id,
                        "error": str(err),
                    },
                )
                return
            except Exception as err:  # pragma: no cover
                with self._metrics_lock:
                    self._metrics["policy_ack"]["failed"] += 1
                self.logger.error(
                    "Unexpected error handling policy acknowledgement",
                    exc_info=True,
                    extra={
                        "event_type": "policy_ack",
                        "policy_id": event.policy_id,
                        "error": str(err),
                    },
                )
                return

        with self._metrics_lock:
            metrics = self._metrics["policy_ack"]
            metrics["processed"] += 1
            if event.result == "applied":
                metrics["applied"] += 1
            else:
                metrics["failed_result"] += 1
            if anomaly_id is None:
                metrics["missing_mapping"] += 1

        log_extra = {
            "event_type": "policy_ack",
            "policy_id": event.policy_id,
            "result": event.result,
            "reason": event.reason,
            "error_code": event.error_code,
            "controller": event.controller_instance,
            "scope": event.scope_identifier,
            "tenant": event.tenant,
            "region": event.region,
            "fast_path": event.fast_path,
            "ack_latency_ms": ack_latency_ms,
        }

        if anomaly_id:
            log_extra["anomaly_id"] = anomaly_id

        self.logger.info("Policy enforcement acknowledgement", extra=log_extra)

        if event.result != "applied":
            self.logger.warning(
                "Policy enforcement failed",
                extra={
                    "policy_id": event.policy_id,
                    "anomaly_id": anomaly_id,
                    "reason": event.reason,
                    "error_code": event.error_code,
                },
            )
    
    def get_metrics(self) -> Dict[str, Any]:
        """
        Get handler metrics.
        
        Returns:
            Dictionary with processed/failed counts per handler type
        """
        with self._metrics_lock:
            snapshot: Dict[str, Any] = {}
            for handler_type, counts in self._metrics.items():
                entry = {
                    "processed": counts.get("processed", 0),
                    "failed": counts.get("failed", 0),
                }
                if handler_type == "policy_ack":
                    entry["applied"] = counts.get("applied", 0)
                    entry["failed_result"] = counts.get("failed_result", 0)
                    entry["missing_mapping"] = counts.get("missing_mapping", 0)
                snapshot[handler_type] = entry
            return snapshot
