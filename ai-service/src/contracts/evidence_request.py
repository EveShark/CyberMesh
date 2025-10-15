"""
EvidenceRequestEvent - Evidence request/validation notification (Backend → AI)

Topic: control.evidence.v1
Purpose: Request additional evidence or notify AI of evidence validation results
"""
from typing import List, Optional
from ..utils.signer import Signer
from ..utils.errors import ContractError
from .generated import control_evidence_pb2


class EvidenceRequestEvent:
    """
    Evidence request/validation notification from backend to AI service.
    
    Security:
    - Ed25519 signature verification
    - Domain separation: "control.evidence.v1"
    """
    
    DOMAIN = "control.evidence.v1"
    
    def __init__(
        self,
        request_id: str,
        anomaly_id: str,
        evidence_id: str,
        request_type: str,
        requested_types: List[str],
        request_reason: str,
        deadline_ts: int,
        evidence_accepted: bool,
        rejection_reason: str,
        evidence_quality_score: int,
        challenge_reason: str,
        challenge_data: bytes,
        timestamp: int,
        block_height: int,
        producer_id: bytes,
        signature: bytes,
        pubkey: bytes,
    ):
        """Parsed EvidenceRequestEvent."""
        self.request_id = request_id
        self.anomaly_id = anomaly_id
        self.evidence_id = evidence_id
        self.request_type = request_type
        self.requested_types = requested_types
        self.request_reason = request_reason
        self.deadline_ts = deadline_ts
        self.evidence_accepted = evidence_accepted
        self.rejection_reason = rejection_reason
        self.evidence_quality_score = evidence_quality_score
        self.challenge_reason = challenge_reason
        self.challenge_data = challenge_data
        self.timestamp = timestamp
        self.block_height = block_height
        self.producer_id = producer_id
        self.signature = signature
        self.pubkey = pubkey
    
    @classmethod
    def from_bytes(cls, data: bytes, verify_signature: bool = True) -> "EvidenceRequestEvent":
        """Parse and optionally verify protobuf message."""
        try:
            msg = control_evidence_pb2.EvidenceRequestEvent()
            msg.ParseFromString(data)
        except Exception as e:
            raise ContractError(f"Failed to parse protobuf: {e}") from e
        
        if msg.alg != "Ed25519":
            raise ContractError(f"Unsupported signature algorithm: {msg.alg}")
        
        if len(msg.pubkey) != 32:
            raise ContractError(f"Invalid pubkey size: {len(msg.pubkey)}")
        
        if len(msg.signature) != 64:
            raise ContractError(f"Invalid signature size: {len(msg.signature)}")
        
        if msg.evidence_quality_score < 1 or msg.evidence_quality_score > 10:
            raise ContractError(
                f"Invalid evidence_quality_score: {msg.evidence_quality_score} (must be 1-10)"
            )
        
        if verify_signature:
            sign_bytes = control_evidence_pb2.EvidenceRequestEvent(
                request_id=msg.request_id,
                anomaly_id=msg.anomaly_id,
                evidence_id=msg.evidence_id,
                request_type=msg.request_type,
                requested_types=list(msg.requested_types),
                request_reason=msg.request_reason,
                deadline_ts=msg.deadline_ts,
                evidence_accepted=msg.evidence_accepted,
                rejection_reason=msg.rejection_reason,
                evidence_quality_score=msg.evidence_quality_score,
                challenge_reason=msg.challenge_reason,
                challenge_data=msg.challenge_data,
                timestamp=msg.timestamp,
                block_height=msg.block_height,
                producer_id=msg.producer_id,
            ).SerializeToString()
            
            if not Signer.verify(sign_bytes, msg.signature, msg.pubkey, cls.DOMAIN):
                raise ContractError("Signature verification failed")
        
        return cls(
            request_id=msg.request_id,
            anomaly_id=msg.anomaly_id,
            evidence_id=msg.evidence_id,
            request_type=msg.request_type,
            requested_types=list(msg.requested_types),
            request_reason=msg.request_reason,
            deadline_ts=msg.deadline_ts,
            evidence_accepted=msg.evidence_accepted,
            rejection_reason=msg.rejection_reason,
            evidence_quality_score=msg.evidence_quality_score,
            challenge_reason=msg.challenge_reason,
            challenge_data=msg.challenge_data,
            timestamp=msg.timestamp,
            block_height=msg.block_height,
            producer_id=msg.producer_id,
            signature=msg.signature,
            pubkey=msg.pubkey,
        )
    
    def __repr__(self) -> str:
        return (
            f"EvidenceRequestEvent(id={self.request_id}, type={self.request_type}, "
            f"anomaly={self.anomaly_id}, evidence={self.evidence_id})"
        )
