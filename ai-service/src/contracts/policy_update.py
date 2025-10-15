"""
PolicyUpdateEvent - Dynamic policy update (Backend → AI)

Topic: control.policy.v1
Purpose: Update AI detection rules/policies dynamically
"""
import hashlib
import json
from typing import Any, Dict, Optional
from ..utils.signer import Signer
from ..utils.errors import ContractError
from .generated import control_policy_pb2


class PolicyUpdateEvent:
    """
    Dynamic policy update from backend to AI service.
    
    Security:
    - Ed25519 signature verification
    - SHA-256 rule_hash verification
    - Domain separation: "control.policy.v1"
    """
    
    DOMAIN = "control.policy.v1"
    
    def __init__(
        self,
        policy_id: str,
        action: str,
        rule_type: str,
        rule_data: Dict[str, Any],
        rule_hash: bytes,
        requires_ack: bool,
        rollback_policy_id: str,
        timestamp: int,
        effective_height: int,
        expiration_height: int,
        producer_id: bytes,
        signature: bytes,
        pubkey: bytes,
    ):
        """Parsed PolicyUpdateEvent."""
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
    
    @classmethod
    def from_bytes(cls, data: bytes, verify_signature: bool = True) -> "PolicyUpdateEvent":
        """Parse and optionally verify protobuf message."""
        try:
            msg = control_policy_pb2.PolicyUpdateEvent()
            msg.ParseFromString(data)
        except Exception as e:
            raise ContractError(f"Failed to parse protobuf: {e}") from e
        
        if msg.alg != "Ed25519":
            raise ContractError(f"Unsupported signature algorithm: {msg.alg}")
        
        if len(msg.pubkey) != 32:
            raise ContractError(f"Invalid pubkey size: {len(msg.pubkey)}")
        
        if len(msg.signature) != 64:
            raise ContractError(f"Invalid signature size: {len(msg.signature)}")
        
        if len(msg.rule_hash) != 32:
            raise ContractError(f"Invalid rule_hash size: {len(msg.rule_hash)}")
        
        try:
            rule_data = json.loads(msg.rule_data.decode("utf-8"))
        except Exception as e:
            raise ContractError(f"Failed to decode rule_data JSON: {e}") from e
        
        computed_hash = hashlib.sha256(msg.rule_data).digest()
        if msg.rule_hash != computed_hash:
            raise ContractError("Rule hash mismatch")
        
        if verify_signature:
            sign_bytes = control_policy_pb2.PolicyUpdateEvent(
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
            ).SerializeToString()
            
            if not Signer.verify(sign_bytes, msg.signature, msg.pubkey, cls.DOMAIN):
                raise ContractError("Signature verification failed")
        
        return cls(
            policy_id=msg.policy_id,
            action=msg.action,
            rule_type=msg.rule_type,
            rule_data=rule_data,
            rule_hash=msg.rule_hash,
            requires_ack=msg.requires_ack,
            rollback_policy_id=msg.rollback_policy_id,
            timestamp=msg.timestamp,
            effective_height=msg.effective_height,
            expiration_height=msg.expiration_height,
            producer_id=msg.producer_id,
            signature=msg.signature,
            pubkey=msg.pubkey,
        )
    
    def __repr__(self) -> str:
        return (
            f"PolicyUpdateEvent(id={self.policy_id}, action={self.action}, "
            f"type={self.rule_type}, effective_height={self.effective_height})"
        )
