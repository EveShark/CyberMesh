"""
ReputationEvent - AI quality feedback (Backend → AI)

Topic: control.reputation.v1
Purpose: Provide feedback on AI detection quality for model tuning
"""
from typing import Optional
from ..utils.signer import Signer
from ..utils.errors import ContractError
from .generated import control_reputation_pb2


class ReputationEvent:
    """
    AI quality feedback from backend to AI service.
    
    Security:
    - Ed25519 signature verification
    - Domain separation: "control.reputation.v1"
    """
    
    DOMAIN = "control.reputation.v1"
    
    def __init__(
        self,
        ai_message_id: str,
        ai_producer_id: bytes,
        message_type: str,
        score: int,
        reason: str,
        confirmed: bool,
        false_positive: bool,
        analyst_notes: str,
        incident_severity: int,
        prevented_attack: bool,
        timestamp: int,
        block_height: int,
        producer_id: bytes,
        signature: bytes,
        pubkey: bytes,
    ):
        """Parsed ReputationEvent."""
        self.ai_message_id = ai_message_id
        self.ai_producer_id = ai_producer_id
        self.message_type = message_type
        self.score = score
        self.reason = reason
        self.confirmed = confirmed
        self.false_positive = false_positive
        self.analyst_notes = analyst_notes
        self.incident_severity = incident_severity
        self.prevented_attack = prevented_attack
        self.timestamp = timestamp
        self.block_height = block_height
        self.producer_id = producer_id
        self.signature = signature
        self.pubkey = pubkey
    
    @classmethod
    def from_bytes(cls, data: bytes, verify_signature: bool = True) -> "ReputationEvent":
        """Parse and optionally verify protobuf message."""
        try:
            msg = control_reputation_pb2.ReputationEvent()
            msg.ParseFromString(data)
        except Exception as e:
            raise ContractError(f"Failed to parse protobuf: {e}") from e
        
        if msg.alg != "Ed25519":
            raise ContractError(f"Unsupported signature algorithm: {msg.alg}")
        
        if len(msg.pubkey) != 32:
            raise ContractError(f"Invalid pubkey size: {len(msg.pubkey)}")
        
        if len(msg.signature) != 64:
            raise ContractError(f"Invalid signature size: {len(msg.signature)}")
        
        if len(msg.ai_producer_id) != 32:
            raise ContractError(f"Invalid ai_producer_id size: {len(msg.ai_producer_id)}")
        
        if msg.score < -100 or msg.score > 100:
            raise ContractError(f"Invalid score: {msg.score} (must be -100 to +100)")
        
        if verify_signature:
            sign_bytes = control_reputation_pb2.ReputationEvent(
                ai_message_id=msg.ai_message_id,
                ai_producer_id=msg.ai_producer_id,
                message_type=msg.message_type,
                score=msg.score,
                reason=msg.reason,
                confirmed=msg.confirmed,
                false_positive=msg.false_positive,
                analyst_notes=msg.analyst_notes,
                incident_severity=msg.incident_severity,
                prevented_attack=msg.prevented_attack,
                timestamp=msg.timestamp,
                block_height=msg.block_height,
                producer_id=msg.producer_id,
            ).SerializeToString()
            
            if not Signer.verify(sign_bytes, msg.signature, msg.pubkey, cls.DOMAIN):
                raise ContractError("Signature verification failed")
        
        return cls(
            ai_message_id=msg.ai_message_id,
            ai_producer_id=msg.ai_producer_id,
            message_type=msg.message_type,
            score=msg.score,
            reason=msg.reason,
            confirmed=msg.confirmed,
            false_positive=msg.false_positive,
            analyst_notes=msg.analyst_notes,
            incident_severity=msg.incident_severity,
            prevented_attack=msg.prevented_attack,
            timestamp=msg.timestamp,
            block_height=msg.block_height,
            producer_id=msg.producer_id,
            signature=msg.signature,
            pubkey=msg.pubkey,
        )
    
    def __repr__(self) -> str:
        return (
            f"ReputationEvent(msg_id={self.ai_message_id}, type={self.message_type}, "
            f"score={self.score}, confirmed={self.confirmed})"
        )
