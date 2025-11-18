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
import copy
import json
import threading
import time
import ipaddress
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
from ..utils.validators import validate_uuid


def _prepare_policy_payload(
    policy_id: str,
    rule_type: str,
    enforcement_action: str,
    payload: Any,
) -> Dict[str, Any]:
    """Normalize and validate policy payload before publishing."""
    validate_uuid(policy_id, "policy_id")

    if not rule_type or not isinstance(rule_type, str):
        raise ValidationError("rule_type is required")

    if not enforcement_action or not isinstance(enforcement_action, str):
        raise ValidationError("enforcement action is required")

    if isinstance(payload, bytes):
        try:
            parsed = json.loads(payload.decode("utf-8"))
        except Exception as exc:
            raise ValidationError(f"Failed to decode policy payload: {exc}") from exc
    elif isinstance(payload, str):
        try:
            parsed = json.loads(payload)
        except Exception as exc:
            raise ValidationError(f"Failed to decode policy payload: {exc}") from exc
    elif payload is None:
        parsed = {}
    elif isinstance(payload, dict):
        parsed = copy.deepcopy(payload)
    else:
        raise ValidationError(f"Unsupported payload type: {type(payload)}")

    parsed.pop("params", None)
    parsed.setdefault("schema_version", 1)
    parsed["policy_id"] = policy_id
    parsed["rule_type"] = rule_type
    parsed["action"] = enforcement_action

    _validate_policy_payload(rule_type, parsed)
    return parsed


def _validate_policy_payload(rule_type: str, payload: Dict[str, Any]):
    if rule_type != "block":
        raise ValidationError(f"Unsupported rule_type: {rule_type}")

    _validate_block_policy_payload(payload)


def _validate_block_policy_payload(payload: Dict[str, Any]):
    schema_version = payload.get("schema_version")
    if not isinstance(schema_version, int) or schema_version <= 0:
        raise ValidationError("schema_version must be positive integer")

    policy_id = payload.get("policy_id")
    validate_uuid(policy_id, "policy_id")

    if payload.get("rule_type") != "block":
        raise ValidationError("rule_type must be 'block'")

    action = payload.get("action")
    if not isinstance(action, str) or not action:
        raise ValidationError("action is required in payload")

    target = payload.get("target")
    if not isinstance(target, dict):
        raise ValidationError("target must be an object")

    ips = target.get("ips", []) or []
    cidrs = target.get("cidrs", []) or []
    selectors = target.get("selectors") or {}

    if not isinstance(ips, list) or not all(isinstance(ip, str) for ip in ips):
        raise ValidationError("target.ips must be a list of strings")
    if not isinstance(cidrs, list) or not all(isinstance(c, str) for c in cidrs):
        raise ValidationError("target.cidrs must be a list of strings")
    if selectors and not isinstance(selectors, dict):
        raise ValidationError("target.selectors must be an object")

    if not ips and not cidrs and not selectors:
        raise ValidationError("target must include ips, cidrs, or selectors")

    for ip in ips:
        try:
            ipaddress.ip_address(ip)
        except ValueError as exc:
            raise ValidationError(f"invalid IP address in target: {ip}") from exc

    for cidr in cidrs:
        try:
            network = ipaddress.ip_network(cidr, strict=False)
        except ValueError as exc:
            raise ValidationError(f"invalid CIDR in target: {cidr}") from exc

        cidr_limit = payload.get("guardrails", {}).get("cidr_max_prefix_len")
        if cidr_limit is not None and isinstance(cidr_limit, int):
            if network.version == 4 and network.prefixlen < cidr_limit:
                raise ValidationError(
                    f"CIDR {cidr} prefix length {network.prefixlen} exceeds guardrail /{cidr_limit}"
                )

        if network.version == 6 and network.prefixlen < PolicyMessage.MIN_IPV6_PREFIX:
            raise ValidationError(
                f"CIDR {cidr} IPv6 prefix length {network.prefixlen} exceeds guardrail /{PolicyMessage.MIN_IPV6_PREFIX}"
            )

    direction = target.get("direction")
    if direction and direction not in {"ingress", "egress"}:
        raise ValidationError("target.direction must be 'ingress' or 'egress'")

    scope = target.get("scope")
    if scope and scope not in {"cluster", "namespace", "node"}:
        raise ValidationError("target.scope must be cluster|namespace|node")

    guardrails = payload.get("guardrails")
    if not isinstance(guardrails, dict):
        raise ValidationError("guardrails must be an object")

    ttl_seconds = guardrails.get("ttl_seconds")
    if not isinstance(ttl_seconds, int) or ttl_seconds <= 0:
        raise ValidationError("guardrails.ttl_seconds must be positive integer")

    cidr_max = guardrails.get("cidr_max_prefix_len")
    if cidr_max is not None and (not isinstance(cidr_max, int) or cidr_max <= 0 or cidr_max > 32):
        raise ValidationError("guardrails.cidr_max_prefix_len must be 1-32")

    max_targets = guardrails.get("max_targets")
    if max_targets is not None:
        if not isinstance(max_targets, int) or max_targets <= 0:
            raise ValidationError("guardrails.max_targets must be positive integer")
        if len(ips) + len(cidrs) > max_targets:
            raise ValidationError("target list exceeds guardrails.max_targets")

    allowlist = guardrails.get("allowlist")
    if allowlist is not None:
        if not isinstance(allowlist, list):
            raise ValidationError("guardrails.allowlist must be a list")
        if len(allowlist) == 0:
            raise ValidationError("guardrails.allowlist cannot be empty")
        for entry in allowlist:
            if not isinstance(entry, str) or not entry:
                raise ValidationError("guardrails.allowlist entries must be non-empty strings")
            try:
                # Accept CIDRs or single IP addresses
                if "/" in entry:
                    network = ipaddress.ip_network(entry, strict=False)
                    if network.version == 6 and network.prefixlen < PolicyMessage.MIN_IPV6_PREFIX:
                        raise ValidationError(
                            f"guardrails.allowlist IPv6 CIDR {entry} is broader than /{PolicyMessage.MIN_IPV6_PREFIX}"
                        )
                else:
                    ipaddress.ip_address(entry)
            except ValueError as exc:
                raise ValidationError(f"invalid allowlist entry: {entry}") from exc

    for bool_field in ("dry_run", "canary_scope", "approval_required"):
        if bool_field in guardrails and not isinstance(guardrails[bool_field], bool):
            raise ValidationError(f"guardrails.{bool_field} must be boolean")

    criteria = payload.get("criteria")
    if criteria is not None:
        if not isinstance(criteria, dict):
            raise ValidationError("criteria must be an object")

        min_confidence = criteria.get("min_confidence")
        if min_confidence is not None:
            if not isinstance(min_confidence, (int, float)) or not (0 <= min_confidence <= 1):
                raise ValidationError("criteria.min_confidence must be between 0 and 1")

        attempts_per_window = criteria.get("attempts_per_window")
        if attempts_per_window is not None and (not isinstance(attempts_per_window, int) or attempts_per_window <= 0):
            raise ValidationError("criteria.attempts_per_window must be positive integer")

        window_s = criteria.get("window_s")
        if window_s is not None and (not isinstance(window_s, int) or window_s <= 0):
            raise ValidationError("criteria.window_s must be positive integer")

    audit = payload.get("audit")
    if audit is not None:
        if not isinstance(audit, dict):
            raise ValidationError("audit must be an object")
        reason_code = audit.get("reason_code")
        if reason_code is not None and not isinstance(reason_code, str):
            raise ValidationError("audit.reason_code must be a string")
        evidence_refs = audit.get("evidence_refs")
        if evidence_refs is not None:
            if not isinstance(evidence_refs, list) or not all(isinstance(ref, str) for ref in evidence_refs):
                raise ValidationError("audit.evidence_refs must be a list of strings")
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
        rule_type: str,
        enforcement_action: str,
        payload: Any,
        timestamp: Optional[int] = None,
        callback: Optional[Callable[[bool, Optional[str]], None]] = None
    ) -> str:
        """
        Publish policy violation message to backend.
        
        Args:
            policy_id: UUIDv4 string
            rule_type: Policy rule type (e.g., "block")
            enforcement_action: Enforcement action (e.g., "drop", "rate_limit")
            payload: Dict/JSON payload describing the policy
            timestamp: Unix timestamp (seconds); defaults to now
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
        
        normalized_payload = _prepare_policy_payload(policy_id, rule_type, enforcement_action, payload)
        payload_json = json.dumps(normalized_payload, sort_keys=True, separators=(",", ":"))

        ts_value = timestamp if timestamp is not None else int(time.time())

        # Create signed policy message using contract
        try:
            policy_msg = PolicyMessage(
                policy_id=policy_id,
                action=enforcement_action,
                timestamp=ts_value,
                payload=payload_json,
                signer=self.signer,
                nonce_manager=self.nonce_manager,
                rule=rule_type,
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
                    "rule_type": rule_type,
                    "action": enforcement_action,
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
