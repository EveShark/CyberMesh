"""
Message publisher for sending messages to backend via Kafka.

Handles:
- Message signing and envelope wrapping
- Nonce generation for replay protection
- Delivery callbacks and metrics
- Retry logic with circuit breaker
- Chain-of-custody management

Security:
- All messages signed with Ed25519
- Nonces prevent replay attacks
- Validation before sending
- Delivery confirmation tracking
- No secrets in logs

Usage:
    publisher = MessagePublisher(producer, signer, nonce_manager, circuit_breaker)
    await publisher.publish_anomaly(anomaly_msg)
"""
import threading
import time
from typing import Optional, Dict, Any, Callable
from datetime import datetime, timezone

from ..contracts import (
    AnomalyMessage,
    EvidenceMessage,
    PolicyMessage,
)
from ..kafka.producer import AIProducer
from ..utils import Signer, NonceManager, CircuitBreaker
from ..utils.errors import ValidationError, KafkaError
from ..logging import get_logger


class MessagePublisher:
    """
    High-level message publisher with signing, validation, and delivery tracking.
    
    Features:
    - Automatic envelope wrapping (version, timestamp, signature)
    - Nonce generation for replay protection
    - Pre-send validation (size limits, required fields)
    - Delivery callbacks with metrics
    - Circuit breaker integration
    - Chain-of-custody management
    
    Security:
    - Messages signed before sending
    - Validation prevents malformed messages
    - Metrics track delivery failures
    - Circuit breaker prevents cascade failures
    """
    
    def __init__(
        self,
        producer: AIProducer,
        signer: Signer,
        nonce_manager: NonceManager,
        circuit_breaker: CircuitBreaker,
        logger=None
    ):
        """
        Initialize message publisher.
        
        Args:
            producer: Kafka producer instance
            signer: Ed25519 signer for message signing
            nonce_manager: Nonce generator for replay protection
            circuit_breaker: Circuit breaker for failure protection
            logger: Logger instance (optional)
        """
        self.producer = producer
        self.signer = signer
        self.nonce_manager = nonce_manager
        self.circuit_breaker = circuit_breaker
        self.logger = logger or get_logger("message_publisher")
        
        # Metrics
        self._metrics = {
            "anomalies_sent": 0,
            "anomalies_failed": 0,
            "evidence_sent": 0,
            "evidence_failed": 0,
            "policy_violations_sent": 0,
            "policy_violations_failed": 0,
            "total_retries": 0,
            "circuit_breaker_trips": 0,
        }
        self._metrics_lock = threading.Lock()
        
        # Delivery tracking
        self._pending_deliveries: Dict[str, Dict[str, Any]] = {}
        self._delivery_lock = threading.Lock()
    
    def publish_anomaly(
        self,
        anomaly_id: str,
        anomaly_type: str,
        source: str,
        severity: int,
        confidence: float,
        payload: bytes,
        model_version: str = "v1.0.0",
        callback: Optional[Callable[[bool, Optional[str]], None]] = None
    ) -> str:
        """
        Publish anomaly detection message to backend.
        
        Args:
            anomaly_id: UUIDv4 string identifying this anomaly
            anomaly_type: Type of anomaly (e.g., "ddos", "port_scan")
            source: Source IP/hostname where anomaly detected
            severity: Severity level 1-10 (1=low, 10=critical)
            confidence: Confidence score 0.0-1.0
            payload: Binary payload data (JSON/proto, max 512KB)
            model_version: AI model version string
            callback: Optional callback(success: bool, error: str) called on delivery
            
        Returns:
            Message ID (for tracking)
            
        Raises:
            ValidationError: If message validation fails
            KafkaError: If circuit breaker is open
            ContractError: If contract validation fails
            
        Security:
        - Signs message with Ed25519
        - Generates nonce for replay protection
        - Validates before sending
        - Tracks delivery status
        """
        # Check circuit breaker
        if not self.circuit_breaker.call(lambda: True):
            with self._metrics_lock:
                self._metrics["circuit_breaker_trips"] += 1
            raise KafkaError("Circuit breaker is open, rejecting message")
        
        # Get current timestamp
        import time
        timestamp = int(time.time())
        
        # Create signed anomaly message using contract
        try:
            anomaly_msg = AnomalyMessage(
                anomaly_id=anomaly_id,
                anomaly_type=anomaly_type,
                source=source,
                severity=severity,
                confidence=confidence,
                timestamp=timestamp,
                payload=payload,
                model_version=model_version,
                signer=self.signer,
                nonce_manager=self.nonce_manager,
            )
        except Exception as e:
            with self._metrics_lock:
                self._metrics["anomalies_failed"] += 1
            raise
        
        # Generate message ID for tracking
        message_id = self._generate_message_id()
        
        # Track pending delivery
        with self._delivery_lock:
            self._pending_deliveries[message_id] = {
                "type": "anomaly",
                "timestamp": time.time(),
                "callback": callback,
            }
        
        # Send via producer (it handles serialization)
        try:
            success = self.producer.send_anomaly(anomaly_msg)
            
            # Call delivery handler
            if success:
                self._handle_delivery(message_id, "anomaly", True, None)
            else:
                self._handle_delivery(message_id, "anomaly", False, "Send failed")
            
            self.logger.info(
                "Anomaly message published",
                extra={
                    "message_id": message_id,
                    "anomaly_id": anomaly_id,
                    "anomaly_type": anomaly_type,
                    "severity": severity,
                }
            )
            
            return message_id
            
        except Exception as e:
            # Remove from pending
            with self._delivery_lock:
                self._pending_deliveries.pop(message_id, None)
            
            with self._metrics_lock:
                self._metrics["anomalies_failed"] += 1
            
            self.logger.error(
                "Failed to publish anomaly",
                exc_info=True,
                extra={
                    "message_id": message_id,
                    "error": str(e),
                }
            )
            raise
    
    def publish_evidence(
        self,
        evidence_id: str,
        evidence_type: str,
        refs: list,
        proof_blob: bytes,
        chain_of_custody: list,
        callback: Optional[Callable[[bool, Optional[str]], None]] = None
    ) -> str:
        """
        Publish evidence submission message to backend.
        
        Args:
            evidence_id: UUIDv4 string
            evidence_type: Type of evidence (e.g., "pcap", "log")
            refs: List of 32-byte SHA-256 hashes referencing related anomalies
            proof_blob: Binary proof data (max 256KB)
            chain_of_custody: List of (ref_hash, actor_id, ts, signature) tuples
            callback: Optional callback(success: bool, error: str) called on delivery
            
        Returns:
            Message ID (for tracking)
            
        Raises:
            ValidationError: If message validation fails
            KafkaError: If circuit breaker is open
            ContractError: If contract validation fails
        """
        
        # Check circuit breaker
        if not self.circuit_breaker.call(lambda: True):
            with self._metrics_lock:
                self._metrics["circuit_breaker_trips"] += 1
            raise KafkaError("Circuit breaker is open, rejecting message")
        
        # Get current timestamp
        import time
        timestamp = int(time.time())
        
        # Create signed evidence message using contract
        try:
            evidence_msg = EvidenceMessage(
                evidence_id=evidence_id,
                evidence_type=evidence_type,
                refs=refs,
                proof_blob=proof_blob,
                chain_of_custody=chain_of_custody,
                timestamp=timestamp,
                signer=self.signer,
                nonce_manager=self.nonce_manager,
            )
        except Exception as e:
            with self._metrics_lock:
                self._metrics["evidence_failed"] += 1
            raise
        
        # Generate message ID for tracking
        message_id = self._generate_message_id()
        
        # Track pending delivery
        with self._delivery_lock:
            self._pending_deliveries[message_id] = {
                "type": "evidence",
                "timestamp": time.time(),
                "callback": callback,
            }
        
        # Send via producer (it handles serialization)
        try:
            success = self.producer.send_evidence(evidence_msg)
            
            # Call delivery handler
            if success:
                self._handle_delivery(message_id, "evidence", True, None)
            else:
                self._handle_delivery(message_id, "evidence", False, "Send failed")
            
            self.logger.info(
                "Evidence message published",
                extra={
                    "message_id": message_id,
                    "evidence_id": evidence_id,
                    "evidence_type": evidence_type,
                    "ref_count": len(refs),
                }
            )
            
            return message_id
            
        except Exception as e:
            # Remove from pending
            with self._delivery_lock:
                self._pending_deliveries.pop(message_id, None)
            
            with self._metrics_lock:
                self._metrics["evidence_failed"] += 1
            
            self.logger.error(
                "Failed to publish evidence",
                exc_info=True,
                extra={
                    "message_id": message_id,
                    "error": str(e),
                }
            )
            raise
    
    def publish_policy_violation(
        self,
        policy_id: str,
        action: str,
        timestamp: int,
        payload: bytes,
        callback: Optional[Callable[[bool, Optional[str]], None]] = None
    ) -> str:
        """
        Publish policy violation message to backend.
        
        Args:
            policy_id: UUIDv4 string
            action: Action type (e.g., "create", "update", "delete")
            timestamp: Unix timestamp (seconds)
            payload: Binary payload data (max 64KB)
            callback: Optional callback(success: bool, error: str) called on delivery
            
        Returns:
            Message ID (for tracking)
            
        Raises:
            ValidationError: If message validation fails
            KafkaError: If circuit breaker is open
            ContractError: If contract validation fails
        """
        # Check circuit breaker
        if not self.circuit_breaker.call(lambda: True):
            with self._metrics_lock:
                self._metrics["circuit_breaker_trips"] += 1
            raise KafkaError("Circuit breaker is open, rejecting message")
        
        # Create signed policy message using contract
        try:
            policy_msg = PolicyMessage(
                policy_id=policy_id,
                action=action,
                timestamp=timestamp,
                payload=payload,
                signer=self.signer,
                nonce_manager=self.nonce_manager,
            )
        except Exception as e:
            with self._metrics_lock:
                self._metrics["policy_violations_failed"] += 1
            raise
        
        # Generate message ID for tracking
        message_id = self._generate_message_id()
        
        # Track pending delivery
        with self._delivery_lock:
            self._pending_deliveries[message_id] = {
                "type": "policy_violation",
                "timestamp": time.time(),
                "callback": callback,
            }
        
        # Send via producer (it handles serialization)
        try:
            success = self.producer.send_policy(policy_msg)
            
            # Call delivery handler
            if success:
                self._handle_delivery(message_id, "policy_violation", True, None)
            else:
                self._handle_delivery(message_id, "policy_violation", False, "Send failed")
            
            self.logger.info(
                "Policy message published",
                extra={
                    "message_id": message_id,
                    "policy_id": policy_id,
                    "action": action,
                }
            )
            
            return message_id
            
        except Exception as e:
            # Remove from pending
            with self._delivery_lock:
                self._pending_deliveries.pop(message_id, None)
            
            with self._metrics_lock:
                self._metrics["policy_violations_failed"] += 1
            
            self.logger.error(
                "Failed to publish policy violation",
                exc_info=True,
                extra={
                    "message_id": message_id,
                    "error": str(e),
                }
            )
            raise
    
    def _handle_delivery(
        self,
        message_id: str,
        message_type: str,
        success: bool,
        error: Optional[str]
    ):
        """
        Handle delivery callback from Kafka producer.
        
        Args:
            message_id: Message ID
            message_type: Message type (anomaly, evidence, policy_violation)
            success: Whether delivery succeeded
            error: Error message if failed
        """
        # Remove from pending
        with self._delivery_lock:
            pending = self._pending_deliveries.pop(message_id, None)
        
        if not pending:
            self.logger.warning(
                "Delivery callback for unknown message",
                extra={"message_id": message_id}
            )
            return
        
        # Update metrics (handle irregular plurals)
        with self._metrics_lock:
            if success:
                # Map message_type to correct metric key
                if message_type == "anomaly":
                    self._metrics["anomalies_sent"] += 1
                elif message_type == "evidence":
                    self._metrics["evidence_sent"] += 1
                elif message_type == "policy_violation":
                    self._metrics["policy_violations_sent"] += 1
            else:
                # Map message_type to correct metric key
                if message_type == "anomaly":
                    self._metrics["anomalies_failed"] += 1
                elif message_type == "evidence":
                    self._metrics["evidence_failed"] += 1
                elif message_type == "policy_violation":
                    self._metrics["policy_violations_failed"] += 1
        
        # Call user callback if provided
        if pending.get("callback"):
            try:
                pending["callback"](success, error)
            except Exception as e:
                self.logger.error(
                    "User callback failed",
                    exc_info=True,
                    extra={
                        "message_id": message_id,
                        "error": str(e),
                    }
                )
        
        # Log result
        if success:
            self.logger.debug(
                "Message delivered successfully",
                extra={
                    "message_id": message_id,
                    "message_type": message_type,
                }
            )
        else:
            self.logger.error(
                "Message delivery failed",
                extra={
                    "message_id": message_id,
                    "message_type": message_type,
                    "error": error,
                }
            )
    
    def _generate_message_id(self) -> str:
        """
        Generate unique message ID for tracking.
        
        Returns:
            Message ID (timestamp + random)
        """
        import secrets
        timestamp = int(time.time() * 1000)
        random_part = secrets.token_hex(8)
        return f"{timestamp}-{random_part}"
    
    def get_metrics(self) -> Dict[str, int]:
        """
        Get publisher metrics.
        
        Returns:
            Dictionary with sent/failed counts per message type
        """
        with self._metrics_lock:
            return dict(self._metrics)
    
    def get_pending_count(self) -> int:
        """
        Get count of pending deliveries.
        
        Returns:
            Number of messages awaiting delivery confirmation
        """
        with self._delivery_lock:
            return len(self._pending_deliveries)
