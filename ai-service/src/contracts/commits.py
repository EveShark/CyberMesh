"""
CommitEvent - Block commit notification (Backend → AI)

Topic: control.commits.v1
Purpose: Notify AI service that backend committed a block with AI-submitted transactions
"""
from typing import Optional
from ..utils.signer import Signer
from ..utils.errors import ContractError
from .generated import control_commits_pb2


class CommitEvent:
    """
    Block commit notification from backend to AI service.
    
    Security:
    - Ed25519 signature verification
    - Domain separation: "control.commits.v1"
    """
    
    DOMAIN = "control.commits.v1"
    
    def __init__(
        self,
        height: int,
        block_hash: bytes,
        state_root: bytes,
        tx_count: int,
        anomaly_count: int,
        evidence_count: int,
        policy_count: int,
        timestamp: int,
        producer_id: bytes,
        signature: bytes,
        pubkey: bytes,
    ):
        """Parsed CommitEvent."""
        self.height = height
        self.block_hash = block_hash
        self.state_root = state_root
        self.tx_count = tx_count
        self.anomaly_count = anomaly_count
        self.evidence_count = evidence_count
        self.policy_count = policy_count
        self.timestamp = timestamp
        self.producer_id = producer_id
        self.signature = signature
        self.pubkey = pubkey
    
    @classmethod
    def from_bytes(cls, data: bytes, verify_signature: bool = True) -> "CommitEvent":
        """
        Parse and optionally verify protobuf message.
        
        Args:
            data: Protobuf-encoded bytes
            verify_signature: If True, verify Ed25519 signature
        
        Returns:
            Parsed CommitEvent
        
        Raises:
            ContractError: Parse or verification failure
        """
        try:
            msg = control_commits_pb2.CommitEvent()
            msg.ParseFromString(data)
        except Exception as e:
            raise ContractError(f"Failed to parse protobuf: {e}") from e
        
        if msg.alg != "Ed25519":
            raise ContractError(f"Unsupported signature algorithm: {msg.alg}")
        
        if len(msg.pubkey) != 32:
            raise ContractError(f"Invalid pubkey size: {len(msg.pubkey)}")
        
        if len(msg.signature) != 64:
            raise ContractError(f"Invalid signature size: {len(msg.signature)}")
        
        if len(msg.block_hash) != 32:
            raise ContractError(f"Invalid block_hash size: {len(msg.block_hash)}")
        
        if len(msg.state_root) != 32:
            raise ContractError(f"Invalid state_root size: {len(msg.state_root)}")
        
        if verify_signature:
            sign_bytes = control_commits_pb2.CommitEvent(
                height=msg.height,
                block_hash=msg.block_hash,
                state_root=msg.state_root,
                tx_count=msg.tx_count,
                anomaly_count=msg.anomaly_count,
                evidence_count=msg.evidence_count,
                policy_count=msg.policy_count,
                timestamp=msg.timestamp,
                producer_id=msg.producer_id,
            ).SerializeToString()
            
            if not Signer.verify(sign_bytes, msg.signature, msg.pubkey, cls.DOMAIN):
                raise ContractError("Signature verification failed")
        
        return cls(
            height=msg.height,
            block_hash=msg.block_hash,
            state_root=msg.state_root,
            tx_count=msg.tx_count,
            anomaly_count=msg.anomaly_count,
            evidence_count=msg.evidence_count,
            policy_count=msg.policy_count,
            timestamp=msg.timestamp,
            producer_id=msg.producer_id,
            signature=msg.signature,
            pubkey=msg.pubkey,
        )
    
    def __repr__(self) -> str:
        return (
            f"CommitEvent(height={self.height}, tx_count={self.tx_count}, "
            f"anomaly={self.anomaly_count}, evidence={self.evidence_count}, policy={self.policy_count})"
        )
