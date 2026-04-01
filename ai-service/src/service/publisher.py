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
import queue
import threading
import time
import ipaddress
import os
from typing import Optional, Dict, Any, Callable
from datetime import datetime, timezone

from ..contracts import (
    AnomalyMessage,
    EvidenceMessage,
    FastMitigationMessage,
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

    _normalize_guardrails_allowlist(parsed)
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
    action = action.strip().lower()

    if action == "remove":
        guardrails = payload.get("guardrails")
        if guardrails is not None and not isinstance(guardrails, dict):
            raise ValidationError("guardrails must be an object")
        rollback_policy_id = payload.get("rollback_policy_id")
        if rollback_policy_id is not None and rollback_policy_id != "":
            validate_uuid(rollback_policy_id, "rollback_policy_id")
        return

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
    if scope and scope not in {"cluster", "namespace", "node", "tenant", "region"}:
        raise ValidationError("target.scope must be cluster|namespace|node|tenant|region")

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
        if not isinstance(allowlist, dict):
            raise ValidationError("guardrails.allowlist must be an object")
        _validate_allowlist_entries(allowlist.get("ips"), "guardrails.allowlist.ips", allow_cidrs=False)
        _validate_allowlist_entries(allowlist.get("cidrs"), "guardrails.allowlist.cidrs", allow_cidrs=True)
        _validate_allowlist_entries(allowlist.get("namespaces"), "guardrails.allowlist.namespaces", allow_cidrs=False, allow_ips=False)

    fast_path_ttl = guardrails.get("fast_path_ttl_seconds")
    if fast_path_ttl is not None and (not isinstance(fast_path_ttl, int) or fast_path_ttl <= 0):
        raise ValidationError("guardrails.fast_path_ttl_seconds must be positive integer")

    fast_path_signals = guardrails.get("fast_path_signals_required")
    if fast_path_signals is not None and (not isinstance(fast_path_signals, int) or fast_path_signals <= 0):
        raise ValidationError("guardrails.fast_path_signals_required must be positive integer")

    fast_path_conf = guardrails.get("fast_path_confidence_min")
    if fast_path_conf is not None and (not isinstance(fast_path_conf, (int, float)) or not (0 <= float(fast_path_conf) <= 1)):
        raise ValidationError("guardrails.fast_path_confidence_min must be between 0 and 1")

    max_ppm = guardrails.get("max_policies_per_minute")
    if max_ppm is not None and (not isinstance(max_ppm, int) or max_ppm <= 0):
        raise ValidationError("guardrails.max_policies_per_minute must be positive integer")

    for bool_field in (
        "dry_run",
        "canary_scope",
        "approval_required",
        "requires_ack",
        "fast_path_enabled",
        "fast_path_canary_scope",
    ):
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


def _prepare_fast_mitigation_payload(
    mitigation_id: str,
    policy_id: str,
    rule_type: str,
    enforcement_action: str,
    payload: Any,
) -> Dict[str, Any]:
    normalized = _prepare_policy_payload(policy_id, rule_type, enforcement_action, payload)
    validate_uuid(mitigation_id, "mitigation_id")
    if enforcement_action not in {"drop", "rate_limit"}:
        raise ValidationError("fast mitigation action must be drop or rate_limit")

    guardrails = dict(normalized.get("guardrails") or {})
    fast_ttl = guardrails.get("fast_path_ttl_seconds") or guardrails.get("ttl_seconds")
    if not isinstance(fast_ttl, int) or fast_ttl <= 0:
        raise ValidationError("fast mitigation requires positive guardrails.fast_path_ttl_seconds or ttl_seconds")
    if int(fast_ttl) > FastMitigationMessage.MAX_FAST_TTL_SECONDS:
        raise ValidationError(
            f"fast mitigation ttl_seconds exceeds max {FastMitigationMessage.MAX_FAST_TTL_SECONDS}"
        )

    metadata = dict(normalized.get("metadata") or {})
    trace_id = metadata.get("trace_id")
    anomaly_id = metadata.get("anomaly_id")
    if not isinstance(trace_id, str) or not trace_id.strip():
        raise ValidationError("fast mitigation requires metadata.trace_id")
    if not isinstance(anomaly_id, str) or not anomaly_id.strip():
        raise ValidationError("fast mitigation requires metadata.anomaly_id")
    metadata["mitigation_id"] = mitigation_id
    metadata["mitigation_source"] = "ai-service"

    normalized["guardrails"] = guardrails
    normalized["metadata"] = metadata
    normalized["mitigation_id"] = mitigation_id
    normalized["policy_id"] = policy_id
    normalized["provisional"] = True
    normalized["promotion_requested"] = True
    normalized["guardrails"]["ttl_seconds"] = int(fast_ttl)
    return normalized


def _normalize_guardrails_allowlist(payload: Dict[str, Any]) -> None:
    guardrails = payload.get("guardrails")
    if not isinstance(guardrails, dict):
        return
    allowlist = guardrails.get("allowlist")
    if allowlist is None:
        return
    if isinstance(allowlist, dict):
        normalized = {
            "ips": _coerce_string_list(allowlist.get("ips")),
            "cidrs": _coerce_string_list(allowlist.get("cidrs")),
            "namespaces": _coerce_string_list(allowlist.get("namespaces")),
        }
        if not normalized["ips"] and not normalized["cidrs"] and not normalized["namespaces"]:
            raise ValidationError("guardrails.allowlist cannot be empty")
        guardrails["allowlist"] = normalized
        return
    if not isinstance(allowlist, list):
        raise ValidationError("guardrails.allowlist must be an object")

    ips: list[str] = []
    cidrs: list[str] = []
    namespaces: list[str] = []
    for entry in allowlist:
        if not isinstance(entry, str) or not entry.strip():
            raise ValidationError("guardrails.allowlist entries must be non-empty strings")
        value = entry.strip()
        if value.lower().startswith("ns:"):
            ns = value[3:].strip()
            if not ns:
                raise ValidationError("guardrails.allowlist namespace entry must be non-empty")
            namespaces.append(ns)
            continue
        if "/" in value:
            cidrs.append(value)
        else:
            ips.append(value)
    if not ips and not cidrs and not namespaces:
        raise ValidationError("guardrails.allowlist cannot be empty")
    guardrails["allowlist"] = {"ips": ips, "cidrs": cidrs, "namespaces": namespaces}


def _coerce_string_list(value: Any) -> list[str]:
    if value is None:
        return []
    if not isinstance(value, list):
        raise ValidationError("allowlist fields must be lists of strings")
    out: list[str] = []
    for item in value:
        if not isinstance(item, str) or not item.strip():
            raise ValidationError("allowlist fields must contain non-empty strings")
        out.append(item.strip())
    return out


def _validate_allowlist_entries(
    entries: Any,
    field: str,
    *,
    allow_cidrs: bool,
    allow_ips: bool = True,
) -> None:
    if entries is None:
        return
    if not isinstance(entries, list):
        raise ValidationError(f"{field} must be a list")
    for entry in entries:
        if not isinstance(entry, str) or not entry:
            raise ValidationError(f"{field} entries must be non-empty strings")
        try:
            if allow_ips and not allow_cidrs:
                ipaddress.ip_address(entry)
                continue
            if allow_cidrs:
                network = ipaddress.ip_network(entry, strict=False)
                if network.version == 6 and network.prefixlen < PolicyMessage.MIN_IPV6_PREFIX:
                    raise ValidationError(
                        f"{field} IPv6 CIDR {entry} is broader than /{PolicyMessage.MIN_IPV6_PREFIX}"
                    )
                continue
            if not allow_ips:
                if not entry.strip():
                    raise ValidationError(f"{field} entries must be non-empty strings")
                continue
        except ValueError as exc:
            raise ValidationError(f"invalid {field} entry: {entry}") from exc
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
            "fast_mitigations_sent": 0,
            "fast_mitigations_failed": 0,
            "fast_mitigation_async_enqueued": 0,
            "fast_mitigation_async_rejected": 0,
            "total_retries": 0,
            "circuit_breaker_trips": 0,
        }
        self._metrics_lock = threading.Lock()
        
        # Delivery tracking
        self._pending_deliveries: Dict[str, Dict[str, Any]] = {}
        self._delivery_lock = threading.Lock()
        self._fast_mitigation_queue_size = max(1, int(os.getenv("AI_FAST_MITIGATION_QUEUE_SIZE", "256")))
        self._fast_mitigation_workers = max(1, int(os.getenv("AI_FAST_MITIGATION_WORKERS", "2")))
        self._fast_mitigation_queue: "queue.Queue[Dict[str, Any]]" = queue.Queue(
            maxsize=self._fast_mitigation_queue_size
        )
        self._fast_mitigation_threads: list[threading.Thread] = []
        for idx in range(self._fast_mitigation_workers):
            worker = threading.Thread(
                target=self._fast_mitigation_worker_loop,
                name=f"fast-mitigation-worker-{idx}",
                daemon=True,
            )
            worker.start()
            self._fast_mitigation_threads.append(worker)
    
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
            trace_id = (
                (normalized_payload.get("trace") or {}).get("id")
                or (normalized_payload.get("metadata") or {}).get("trace_id")
                or normalized_payload.get("trace_id")
                or normalized_payload.get("qc_reference")
                or ""
            )
            if self._policy_stage_markers_enabled() and trace_id:
                self.logger.info(
                    "policy stage marker",
                    extra={
                        "stage": "t_ai_producer_send_start",
                        "policy_id": policy_id,
                        "trace_id": str(trace_id),
                        "t_ms": int(time.time() * 1000),
                    },
                )
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
            if success and self._policy_stage_markers_enabled() and trace_id:
                self.logger.info(
                    "policy stage marker",
                    extra={
                        "stage": "t_ai_producer_ack",
                        "policy_id": policy_id,
                        "trace_id": str(trace_id),
                        "t_ms": int(time.time() * 1000),
                    },
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

    def publish_fast_mitigation(
        self,
        *,
        mitigation_id: str,
        policy_id: str,
        rule_type: str,
        enforcement_action: str,
        payload: Any,
        timestamp: Optional[int] = None,
        callback: Optional[Callable[[bool, Optional[str]], None]] = None,
    ) -> str:
        return self._publish_fast_mitigation_impl(
            mitigation_id=mitigation_id,
            policy_id=policy_id,
            rule_type=rule_type,
            enforcement_action=enforcement_action,
            payload=payload,
            timestamp=timestamp,
            callback=callback,
        )

    def publish_fast_mitigation_async(
        self,
        *,
        mitigation_id: str,
        policy_id: str,
        rule_type: str,
        enforcement_action: str,
        payload: Any,
        timestamp: Optional[int] = None,
        callback: Optional[Callable[[bool, Optional[str]], None]] = None,
    ) -> bool:
        item = {
            "mitigation_id": mitigation_id,
            "policy_id": policy_id,
            "rule_type": rule_type,
            "enforcement_action": enforcement_action,
            "payload": payload,
            "timestamp": timestamp,
            "callback": callback,
        }
        try:
            self._fast_mitigation_queue.put_nowait(item)
        except queue.Full:
            with self._metrics_lock:
                self._metrics["fast_mitigation_async_rejected"] += 1
            self.logger.warning(
                "Fast mitigation queue full; async publish rejected",
                extra={
                    "mitigation_id": mitigation_id,
                    "policy_id": policy_id,
                    "queue_depth": self._fast_mitigation_queue.qsize(),
                },
            )
            return False
        with self._metrics_lock:
            self._metrics["fast_mitigation_async_enqueued"] += 1
        return True

    def _publish_fast_mitigation_impl(
        self,
        *,
        mitigation_id: str,
        policy_id: str,
        rule_type: str,
        enforcement_action: str,
        payload: Any,
        timestamp: Optional[int] = None,
        callback: Optional[Callable[[bool, Optional[str]], None]] = None,
    ) -> str:
        if not self.circuit_breaker.call(lambda: True):
            with self._metrics_lock:
                self._metrics["circuit_breaker_trips"] += 1
            raise KafkaError("Circuit breaker is open, rejecting message")

        normalized_payload = _prepare_fast_mitigation_payload(
            mitigation_id=mitigation_id,
            policy_id=policy_id,
            rule_type=rule_type,
            enforcement_action=enforcement_action,
            payload=payload,
        )
        ts_value = timestamp if timestamp is not None else int(time.time())

        try:
            msg = FastMitigationMessage(
                mitigation_id=mitigation_id,
                policy_id=policy_id,
                action=enforcement_action,
                rule_type=rule_type,
                timestamp=ts_value,
                payload=normalized_payload,
                signer=self.signer,
                nonce_manager=self.nonce_manager,
            )
        except Exception:
            with self._metrics_lock:
                self._metrics["fast_mitigations_failed"] += 1
            raise

        message_id = self._generate_message_id()
        with self._delivery_lock:
            self._pending_deliveries[message_id] = {
                "type": "fast_mitigation",
                "timestamp": time.time(),
                "callback": callback,
            }

        try:
            success = self.producer.send_fast_mitigation(msg)
            if success:
                self._handle_delivery(message_id, "fast_mitigation", True, None)
            else:
                self._handle_delivery(message_id, "fast_mitigation", False, "Send failed")

            self.logger.info(
                "Fast mitigation message published",
                extra={
                    "message_id": message_id,
                    "mitigation_id": mitigation_id,
                    "policy_id": policy_id,
                    "action": enforcement_action,
                },
            )
            return message_id
        except Exception as exc:
            with self._delivery_lock:
                self._pending_deliveries.pop(message_id, None)
            with self._metrics_lock:
                self._metrics["fast_mitigations_failed"] += 1
            self.logger.error(
                "Failed to publish fast mitigation",
                exc_info=True,
                extra={
                    "message_id": message_id,
                    "mitigation_id": mitigation_id,
                    "policy_id": policy_id,
                    "error": str(exc),
                },
            )
            raise

    def _fast_mitigation_worker_loop(self) -> None:
        while True:
            item = self._fast_mitigation_queue.get()
            try:
                self._publish_fast_mitigation_impl(**item)
            except Exception as exc:
                self.logger.warning(
                    "Fast mitigation async publish failed",
                    extra={
                        "mitigation_id": item.get("mitigation_id"),
                        "policy_id": item.get("policy_id"),
                        "error": str(exc),
                    },
                    exc_info=True,
                )
            finally:
                self._fast_mitigation_queue.task_done()

    def _policy_stage_markers_enabled(self) -> bool:
        return os.getenv("POLICY_STAGE_MARKERS_ENABLED", "false").strip().lower() in ("1", "true", "yes", "on")

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
            message_type: Message type (anomaly, evidence, policy_violation, fast_mitigation)
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
                elif message_type == "fast_mitigation":
                    self._metrics["fast_mitigations_sent"] += 1
            else:
                # Map message_type to correct metric key
                if message_type == "anomaly":
                    self._metrics["anomalies_failed"] += 1
                elif message_type == "evidence":
                    self._metrics["evidence_failed"] += 1
                elif message_type == "policy_violation":
                    self._metrics["policy_violations_failed"] += 1
                elif message_type == "fast_mitigation":
                    self._metrics["fast_mitigations_failed"] += 1
        
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
            snapshot = dict(self._metrics)
        snapshot["fast_mitigation_queue_depth"] = self._fast_mitigation_queue.qsize()
        return snapshot
    
    def get_pending_count(self) -> int:
        """
        Get count of pending deliveries.
        
        Returns:
            Number of messages awaiting delivery confirmation
        """
        with self._delivery_lock:
            return len(self._pending_deliveries)
