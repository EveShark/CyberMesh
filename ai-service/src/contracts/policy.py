"""
PolicyUpdateEvent - Backend policy update message (Backend → AI)

Topic: control.policy.v1
Backend producer: backend/pkg/ingest/kafka/producer.go
AI consumer: src/kafka/consumer.py

Security: Ed25519 signature verification + policy validation
"""
import hashlib
import json
from pathlib import Path
from typing import Any, Dict, Optional
from cryptography.hazmat.primitives.asymmetric import ed25519

from ..utils.validators import (
    validate_timestamp,
    validate_uuid4,
    validate_size,
    validate_required_string,
)
from ..utils.errors import ContractError
from ..utils.limits import SIZE_LIMITS
from ..utils.nonce import NonceManager
from ..utils.signer import Signer
from .generated import control_policy_pb2, ai_policy_pb2
from .commit import BackendValidatorTrustStore


class PolicyUpdateEvent:
    """
    Cryptographically signed policy update from backend validators.
    
    Security:
    - Ed25519 signature verification from trusted validators
    - Policy rule validation (type, data format, ranges)
    - Hash verification (rule_hash = SHA-256(rule_data))
    - Domain separation: "control.policy.v1"
    
    Supported rule types:
    - threshold: Adjust detection thresholds
    - blacklist: Add IPs to blacklist
    - whitelist: Add IPs to whitelist
    - feature_flag: Enable/disable features
    - rate_limit: Adjust rate limits
    - model_param: Update model parameters
    """
    
    DOMAIN = "control.policy.v1"
    
    # Valid policy actions
    VALID_ACTIONS = {"add", "remove", "update", "enable", "disable"}
    
    # Valid rule types
    VALID_RULE_TYPES = {
        "threshold", "blacklist", "whitelist",
        "feature_flag", "rate_limit", "model_param"
    }
    
    # Validation ranges for threshold rules
    THRESHOLD_RANGES = {
        "ddos_confidence": (0.5, 0.99),
        "malware_confidence": (0.5, 0.99),
        "anomaly_confidence": (0.5, 0.99),
        "severity_threshold": (1, 10),
    }
    
    # Class-level trust store (shared with CommitEvent)
    _trust_store: Optional[BackendValidatorTrustStore] = None
    
    @classmethod
    def initialize_trust_store(cls, keys_dir: Optional[Path] = None):
        """Initialize backend validator trust store (call once at startup)."""
        cls._trust_store = BackendValidatorTrustStore(keys_dir)
    
    def __init__(
        self,
        policy_id: str,
        action: str,
        rule_type: str,
        rule_data: bytes,
        rule_hash: bytes,
        requires_ack: bool,
        rollback_policy_id: str,
        timestamp: int,
        effective_height: int,
        expiration_height: int,
        producer_id: bytes,
        signature: bytes,
        pubkey: bytes,
        alg: str,
    ):
        """
        Parse and verify policy update from backend.
        
        Args:
            policy_id: Unique policy identifier
            action: "add", "remove", "update", "enable", "disable"
            rule_type: Rule category (threshold, blacklist, etc.)
            rule_data: JSON-encoded rule parameters
            rule_hash: SHA-256(rule_data) for integrity (32 bytes)
            requires_ack: True if AI must acknowledge successful application
            rollback_policy_id: Policy to revert to on failure (optional)
            timestamp: Unix timestamp (seconds)
            effective_height: Block height when policy takes effect
            expiration_height: Block height when policy expires (0 = no expiration)
            producer_id: Backend validator public key (32 bytes)
            signature: Ed25519 signature (64 bytes)
            pubkey: Ed25519 public key (32 bytes)
            alg: Signature algorithm (must be "Ed25519")
        
        Raises:
            ContractError: Validation or signature verification failure
        """
        if self._trust_store is None:
            raise ContractError("Trust store not initialized. Call PolicyUpdateEvent.initialize_trust_store() first.")
        
        self._validate_inputs(
            policy_id, action, rule_type, rule_data, rule_hash,
            requires_ack, rollback_policy_id, timestamp,
            effective_height, expiration_height, producer_id,
            signature, pubkey, alg
        )
        
        self.policy_id = policy_id
        self.action = action
        self.rule_type = rule_type
        self.rule_data = rule_data
        self.rule_hash = rule_hash
        self.requires_ack = requires_ack
        self.rollback_policy_id = rollback_policy_id
        self.timestamp = timestamp
        self.effective_height = effective_height
        self.expiration_height = expiration_height
        self.producer_id = producer_id
        self.signature = signature
        self.pubkey = pubkey
        self.alg = alg
        
        # Parse and validate rule data
        self.parsed_rule_data = self._parse_and_validate_rule_data()
        
        # Verify signature
        self._verify_signature()
    
    def _validate_inputs(
        self,
        policy_id: str,
        action: str,
        rule_type: str,
        rule_data: bytes,
        rule_hash: bytes,
        requires_ack: bool,
        rollback_policy_id: str,
        timestamp: int,
        effective_height: int,
        expiration_height: int,
        producer_id: bytes,
        signature: bytes,
        pubkey: bytes,
        alg: str,
    ):
        """Validate all inputs."""
        try:
            # Policy ID
            if not policy_id or len(policy_id) > 128:
                raise ContractError(f"Invalid policy_id length: {len(policy_id)} (1-128 chars)")
            
            # Action
            if action not in self.VALID_ACTIONS:
                raise ContractError(f"Invalid action: {action} (valid: {self.VALID_ACTIONS})")
            
            # Rule type
            if rule_type not in self.VALID_RULE_TYPES:
                raise ContractError(f"Invalid rule_type: {rule_type} (valid: {self.VALID_RULE_TYPES})")
            
            # Rule data
            if len(rule_data) == 0:
                raise ContractError("rule_data cannot be empty")
            
            if len(rule_data) > 65536:  # 64KB max
                raise ContractError(f"rule_data too large: {len(rule_data)} bytes (max: 65536)")
            
            # Rule hash
            if len(rule_hash) != 32:
                raise ContractError(f"Invalid rule_hash size: {len(rule_hash)} (expected 32 bytes)")
            
            # Verify rule_hash = SHA-256(rule_data)
            computed_hash = hashlib.sha256(rule_data).digest()
            if computed_hash != rule_hash:
                raise ContractError(
                    f"rule_hash mismatch: expected {rule_hash.hex()[:16]}..., "
                    f"got {computed_hash.hex()[:16]}..."
                )
            
            # Rollback policy ID
            if rollback_policy_id and len(rollback_policy_id) > 128:
                raise ContractError(f"Invalid rollback_policy_id length: {len(rollback_policy_id)}")
            
            # Timestamp
            validate_timestamp(timestamp)
            
            # Heights
            if effective_height < 0:
                raise ContractError(f"Invalid effective_height: {effective_height}")
            
            if expiration_height < 0:
                raise ContractError(f"Invalid expiration_height: {expiration_height}")
            
            if expiration_height > 0 and expiration_height <= effective_height:
                raise ContractError(
                    f"expiration_height ({expiration_height}) must be > "
                    f"effective_height ({effective_height})"
                )
            
            # Cryptographic fields
            if len(producer_id) != 32:
                raise ContractError(f"Invalid producer_id size: {len(producer_id)}")
            
            if len(signature) != 64:
                raise ContractError(f"Invalid signature size: {len(signature)}")
            
            if len(pubkey) != 32:
                raise ContractError(f"Invalid pubkey size: {len(pubkey)}")
            
            if pubkey != producer_id:
                raise ContractError("pubkey must match producer_id")
            
            if alg != "Ed25519":
                raise ContractError(f"Invalid algorithm: {alg}")
                
        except ContractError:
            raise
        except Exception as e:
            raise ContractError(f"PolicyUpdateEvent validation failed: {e}") from e
    
    def _parse_and_validate_rule_data(self) -> dict:
        """
        Parse and validate JSON rule data.
        
        Returns:
            Parsed rule data as dictionary
        
        Raises:
            ContractError: Invalid JSON or rule validation failure
        """
        try:
            # Parse JSON
            try:
                rule_data = json.loads(self.rule_data.decode('utf-8'))
            except json.JSONDecodeError as e:
                raise ContractError(f"Invalid JSON in rule_data: {e}")
            
            if not isinstance(rule_data, dict):
                raise ContractError(f"rule_data must be JSON object, got {type(rule_data)}")
            
            # Validate based on rule_type
            if self.rule_type == "threshold":
                self._validate_threshold_rule(rule_data)
            elif self.rule_type == "blacklist":
                self._validate_blacklist_rule(rule_data)
            elif self.rule_type == "whitelist":
                self._validate_whitelist_rule(rule_data)
            elif self.rule_type == "feature_flag":
                self._validate_feature_flag_rule(rule_data)
            elif self.rule_type == "rate_limit":
                self._validate_rate_limit_rule(rule_data)
            elif self.rule_type == "model_param":
                self._validate_model_param_rule(rule_data)
            
            return rule_data
            
        except ContractError:
            raise
        except Exception as e:
            raise ContractError(f"Rule data validation failed: {e}") from e
    
    def _validate_threshold_rule(self, rule_data: dict):
        """Validate threshold rule data."""
        for key, value in rule_data.items():
            if key not in self.THRESHOLD_RANGES:
                raise ContractError(f"Unknown threshold parameter: {key}")
            
            if not isinstance(value, (int, float)):
                raise ContractError(f"Threshold {key} must be numeric, got {type(value)}")
            
            min_val, max_val = self.THRESHOLD_RANGES[key]
            if not (min_val <= value <= max_val):
                raise ContractError(
                    f"Threshold {key} = {value} out of range [{min_val}, {max_val}]"
                )
    
    def _validate_blacklist_rule(self, rule_data: dict):
        """Validate blacklist rule data."""
        if "ips" not in rule_data:
            raise ContractError("Blacklist rule must contain 'ips' field")
        
        ips = rule_data["ips"]
        if not isinstance(ips, list):
            raise ContractError(f"Blacklist 'ips' must be list, got {type(ips)}")
        
        if len(ips) > 10000:
            raise ContractError(f"Blacklist too large: {len(ips)} IPs (max: 10000)")
        
        for ip in ips:
            if not isinstance(ip, str):
                raise ContractError(f"Blacklist IP must be string, got {type(ip)}")
    
    def _validate_whitelist_rule(self, rule_data: dict):
        """Validate whitelist rule data (same as blacklist)."""
        self._validate_blacklist_rule(rule_data)  # Same validation
    
    def _validate_feature_flag_rule(self, rule_data: dict):
        """Validate feature flag rule data."""
        valid_flags = {
            "enable_ddos_detection",
            "enable_malware_detection",
            "enable_anomaly_detection",
            "enable_evidence_generation",
        }
        
        for key, value in rule_data.items():
            if key not in valid_flags:
                raise ContractError(f"Unknown feature flag: {key}")
            
            if not isinstance(value, bool):
                raise ContractError(f"Feature flag {key} must be boolean, got {type(value)}")
    
    def _validate_rate_limit_rule(self, rule_data: dict):
        """Validate rate limit rule data."""
        valid_limits = {
            "anomaly_capacity",
            "evidence_capacity",
            "policy_capacity",
        }
        
        for key, value in rule_data.items():
            if key not in valid_limits:
                raise ContractError(f"Unknown rate limit: {key}")
            
            if not isinstance(value, int):
                raise ContractError(f"Rate limit {key} must be integer, got {type(value)}")
            
            if value < 0 or value > 100000:
                raise ContractError(f"Rate limit {key} = {value} out of range [0, 100000]")
    
    def _validate_model_param_rule(self, rule_data: dict):
        """Validate model parameter rule data."""
        # Model params are flexible, but check for dangerous values
        for key, value in rule_data.items():
            if not isinstance(key, str):
                raise ContractError(f"Model param key must be string")
            
            # Reject excessively large values
            if isinstance(value, (int, float)):
                if abs(value) > 1e9:
                    raise ContractError(f"Model param {key} = {value} too large")
    
    def _verify_signature(self):
        """
        Verify Ed25519 signature from backend validator.
        
        Signature format: domain||policy_id||action||rule_type||rule_hash||timestamp
        
        Raises:
            ContractError: Signature verification failure
        """
        try:
            # Build signed payload
            import struct
            
            sign_bytes = b""
            sign_bytes += self.DOMAIN.encode('utf-8')
            sign_bytes += self.policy_id.encode('utf-8')
            sign_bytes += self.action.encode('utf-8')
            sign_bytes += self.rule_type.encode('utf-8')
            sign_bytes += self.rule_hash
            sign_bytes += struct.pack('>q', self.timestamp)
            
            # Find validator public key in trust store
            from cryptography.hazmat.primitives import serialization
            
            validator_pubkey_obj = None
            for node_id, pubkey_obj in self._trust_store._public_keys.items():
                pubkey_bytes = pubkey_obj.public_bytes(
                    encoding=serialization.Encoding.Raw,
                    format=serialization.PublicFormat.Raw
                )
                if pubkey_bytes == self.pubkey:
                    validator_pubkey_obj = pubkey_obj
                    break
            
            if validator_pubkey_obj is None:
                raise ContractError(
                    f"Unknown validator public key: {self.pubkey.hex()[:16]}..."
                )
            
            # Verify Ed25519 signature
            validator_pubkey_obj.verify(self.signature, sign_bytes)
            
        except ContractError:
            raise
        except Exception as e:
            raise ContractError(f"Signature verification failed: {e}") from e
    
    @classmethod
    def from_bytes(cls, data: bytes) -> "PolicyUpdateEvent":
        """
        Deserialize from protobuf bytes.
        
        Args:
            data: Protobuf-serialized PolicyUpdateEvent
        
        Returns:
            PolicyUpdateEvent instance
        
        Raises:
            ContractError: Deserialization or validation failure
        """
        try:
            msg = control_policy_pb2.PolicyUpdateEvent()
            msg.ParseFromString(data)
            
            return cls(
                policy_id=msg.policy_id,
                action=msg.action,
                rule_type=msg.rule_type,
                rule_data=msg.rule_data,
                rule_hash=msg.rule_hash,
                requires_ack=msg.requires_ack,
                rollback_policy_id=msg.rollback_policy_id,
                timestamp=msg.timestamp,
                effective_height=msg.effective_height,
                expiration_height=msg.expiration_height,
                producer_id=msg.producer_id,
                signature=msg.signature,
                pubkey=msg.pubkey,
                alg=msg.alg,
            )
        except ContractError:
            raise
        except Exception as e:
            raise ContractError(f"Failed to parse PolicyUpdateEvent: {e}") from e
    
    def to_dict(self) -> dict:
        """Convert to dictionary (for logging/debugging)."""
        return {
            "policy_id": self.policy_id,
            "action": self.action,
            "rule_type": self.rule_type,
            "rule_data": self.parsed_rule_data,
            "requires_ack": self.requires_ack,
            "rollback_policy_id": self.rollback_policy_id,
            "timestamp": self.timestamp,
            "effective_height": self.effective_height,
            "expiration_height": self.expiration_height,
            "producer_id": self.producer_id.hex(),
            "alg": self.alg,
        }


class PolicyMessage:
    """AI policy recommendation/violation message (AI → Backend)."""

    DOMAIN = "ai.policy.v1"
    RULE_MAX_LENGTH = 256

    def __init__(
        self,
        policy_id: str,
        action: str,
        timestamp: int,
        payload,
        signer: Signer,
        nonce_manager: NonceManager,
        rule: Optional[str] = None,
    ):
        payload_bytes = self._ensure_bytes(payload)
        self._validate_core_inputs(policy_id, action, timestamp, payload_bytes)

        parsed_payload = self._try_parse_json(payload_bytes)
        params_bytes = self._extract_params_bytes(parsed_payload, payload_bytes)
        rule_value = self._determine_rule(rule, action, parsed_payload)

        # Enforce size limits on serialized params
        validate_size(len(params_bytes), SIZE_LIMITS.MAX_POLICY_PARAMS_SIZE, "policy_params")

        self.policy_id = policy_id
        self.action = action
        self.timestamp = timestamp
        self.rule = rule_value
        self.params = params_bytes
        self.signer = signer
        self.nonce_manager = nonce_manager
        self.raw_payload = payload_bytes
        self.parsed_payload = parsed_payload if isinstance(parsed_payload, dict) else None

        self.content_hash = hashlib.sha256(self.params).digest()
        self.nonce = nonce_manager.generate()
        if len(self.nonce) != NonceManager.NONCE_SIZE:
            raise ContractError(
                f"Generated nonce must be {NonceManager.NONCE_SIZE} bytes, got {len(self.nonce)}"
            )

        self.pubkey = signer.public_key_bytes
        sign_bytes = self._build_sign_bytes()
        self.signature, _, _ = signer.sign(sign_bytes, domain=self.DOMAIN)

    @staticmethod
    def _ensure_bytes(payload) -> bytes:
        if isinstance(payload, bytes):
            return payload
        if isinstance(payload, str):
            return payload.encode("utf-8")
        if isinstance(payload, dict):
            try:
                return json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
            except (TypeError, ValueError) as e:
                raise ContractError(f"Failed to serialize payload dict: {e}") from e
        raise ContractError(f"Unsupported payload type: {type(payload)}")

    @staticmethod
    def _try_parse_json(payload_bytes: bytes) -> Optional[Any]:
        try:
            return json.loads(payload_bytes.decode("utf-8"))
        except Exception:
            return None

    def _extract_params_bytes(self, parsed_payload: Optional[Any], payload_bytes: bytes) -> bytes:
        if isinstance(parsed_payload, dict) and "params" in parsed_payload:
            try:
                return json.dumps(
                    parsed_payload["params"],
                    sort_keys=True,
                    separators=(",", ":"),
                ).encode("utf-8")
            except (TypeError, ValueError) as e:
                raise ContractError(f"Failed to serialize params: {e}") from e
        return payload_bytes

    def _determine_rule(
        self,
        rule_override: Optional[str],
        action: str,
        parsed_payload: Optional[Any],
    ) -> str:
        candidate: Optional[str] = None

        if rule_override and isinstance(rule_override, str) and rule_override.strip():
            candidate = rule_override.strip()
        elif isinstance(parsed_payload, dict):
            value = parsed_payload.get("rule")
            if isinstance(value, str) and value.strip():
                candidate = value.strip()

        if not candidate:
            candidate = f"{action}_policy"

        if len(candidate) > self.RULE_MAX_LENGTH:
            raise ContractError(
                f"rule length {len(candidate)} exceeds maximum {self.RULE_MAX_LENGTH}"
            )

        return candidate

    def _validate_core_inputs(
        self,
        policy_id: str,
        action: str,
        timestamp: int,
        payload_bytes: bytes,
    ):
        try:
            if not validate_uuid4(policy_id):
                raise ContractError("policy_id must be a valid UUIDv4 string")

            validate_required_string(action, "action", max_length=64)
            validate_timestamp(timestamp)
            validate_size(len(payload_bytes), SIZE_LIMITS.MAX_POLICY_PARAMS_SIZE, "payload")

        except ContractError:
            raise
        except Exception as e:
            raise ContractError(f"Policy message validation failed: {e}") from e

    def _build_sign_bytes(self) -> bytes:
        msg = ai_policy_pb2.PolicyEvent(
            id=self.policy_id,
            action=self.action,
            rule=self.rule,
            params=self.params,
            ts=self.timestamp,
            content_hash=self.content_hash,
            producer_id=self.pubkey,
            nonce=self.nonce,
        )
        return msg.SerializeToString()

    def to_bytes(self) -> bytes:
        msg = ai_policy_pb2.PolicyEvent(
            id=self.policy_id,
            action=self.action,
            rule=self.rule,
            params=self.params,
            ts=self.timestamp,
            content_hash=self.content_hash,
            producer_id=self.pubkey,
            nonce=self.nonce,
            signature=self.signature,
            pubkey=self.pubkey,
            alg="Ed25519",
        )
        return msg.SerializeToString()

    @classmethod
    def from_bytes(
        cls,
        data: bytes,
        nonce_manager: Optional[NonceManager] = None,
    ) -> "PolicyMessage":
        try:
            msg = ai_policy_pb2.PolicyEvent()
            msg.ParseFromString(data)
        except Exception as e:
            raise ContractError(f"Failed to parse PolicyEvent protobuf: {e}") from e

        if msg.alg != "Ed25519":
            raise ContractError(f"Unsupported signature algorithm: {msg.alg}")

        if len(msg.nonce) != NonceManager.NONCE_SIZE:
            raise ContractError(f"Invalid nonce size: {len(msg.nonce)} (expected {NonceManager.NONCE_SIZE})")

        if len(msg.pubkey) != 32:
            raise ContractError(f"Invalid pubkey size: {len(msg.pubkey)} (expected 32)")

        if len(msg.signature) != 64:
            raise ContractError(f"Invalid signature size: {len(msg.signature)} (expected 64)")

        if msg.producer_id != msg.pubkey:
            raise ContractError("producer_id must equal pubkey")

        expected_hash = hashlib.sha256(msg.params).digest()
        if msg.content_hash != expected_hash:
            raise ContractError("Content hash mismatch")

        if nonce_manager and not nonce_manager.validate(msg.nonce):
            raise ContractError("Nonce validation failed (replay or expired)")

        sign_bytes = ai_policy_pb2.PolicyEvent(
            id=msg.id,
            action=msg.action,
            rule=msg.rule,
            params=msg.params,
            ts=msg.ts,
            content_hash=msg.content_hash,
            producer_id=msg.producer_id,
            nonce=msg.nonce,
        ).SerializeToString()

        if not Signer.verify(sign_bytes, msg.signature, msg.pubkey, cls.DOMAIN):
            raise ContractError("Signature verification failed")

        instance = cls.__new__(cls)
        instance.policy_id = msg.id
        instance.action = msg.action
        instance.rule = msg.rule
        instance.timestamp = msg.ts
        instance.params = msg.params
        instance.content_hash = msg.content_hash
        instance.nonce = msg.nonce
        instance.signature = msg.signature
        instance.pubkey = msg.pubkey
        instance.signer = None
        instance.nonce_manager = nonce_manager
        instance.raw_payload = msg.params
        instance.parsed_payload = None
        return instance
