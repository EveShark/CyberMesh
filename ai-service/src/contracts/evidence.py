"""
EvidenceMessage - AI evidence submission message (AI → Backend)

Topic: ai.evidence.v1
Backend consumer: backend/pkg/ingest/kafka/consumer.go
Backend validator: backend/pkg/ingest/kafka/verifier.go
"""
import hashlib
from typing import List, Optional, Tuple
from ..utils.validators import validate_uuid4, validate_size, validate_timestamp
from ..utils.limits import SIZE_LIMITS
from ..utils.nonce import NonceManager
from ..utils.signer import Signer
from ..utils.errors import ContractError
from .generated import ai_evidence_pb2


class EvidenceMessage:
    """
    Cryptographically signed evidence submission with chain-of-custody.
    
    Security:
    - Ed25519 signature over protobuf-serialized proof_blob
    - 16-byte nonce for replay protection
    - SHA-256 content hash for integrity
    - Chain-of-custody with per-entry signatures
    - Domain separation: "ai.evidence.v1"
    """
    
    DOMAIN = "ai.evidence.v1"
    MAX_REFS = 1000
    MAX_COC_ENTRIES = 100
    
    def __init__(
        self,
        evidence_id: str,
        evidence_type: str,
        refs: List[bytes],
        proof_blob: bytes,
        chain_of_custody: List[Tuple[bytes, bytes, int, bytes]],
        timestamp: int,
        signer: Signer,
        nonce_manager: NonceManager,
    ):
        """
        Create and sign evidence message.
        
        Args:
            evidence_id: UUIDv4 string
            evidence_type: Evidence type (e.g., "pcap", "log", "network_flow")
            refs: List of 32-byte SHA-256 references to related anomalies/evidence
            proof_blob: Binary proof data (PCAP, logs, etc., max 256KB)
            chain_of_custody: List of (ref_hash, actor_id, ts, signature) tuples
            timestamp: Unix timestamp (seconds)
            signer: Signer instance for Ed25519 signing
            nonce_manager: NonceManager for replay protection
        
        Raises:
            ContractError: Validation failure
        """
        self._validate_inputs(
            evidence_id, evidence_type, refs, proof_blob,
            chain_of_custody, timestamp
        )
        
        self.evidence_id = evidence_id
        self.evidence_type = evidence_type
        self.refs = refs
        self.proof_blob = proof_blob
        self.chain_of_custody = chain_of_custody
        self.timestamp = timestamp
        self.signer = signer
        self.nonce_manager = nonce_manager
        
        self.content_hash = hashlib.sha256(proof_blob).digest()
        self.nonce = nonce_manager.generate()
        self.pubkey = signer.public_key_bytes
        
        sign_bytes = self._build_sign_bytes()
        self.signature, _, _ = signer.sign(sign_bytes, domain=self.DOMAIN)
    
    def _validate_inputs(
        self,
        evidence_id: str,
        evidence_type: str,
        refs: List[bytes],
        proof_blob: bytes,
        chain_of_custody: List[Tuple[bytes, bytes, int, bytes]],
        timestamp: int,
    ):
        """Validate all inputs before signing."""
        try:
            validate_uuid4(evidence_id)
            validate_timestamp(timestamp)
            validate_size(len(proof_blob), SIZE_LIMITS.MAX_EVIDENCE_SIZE, "proof_blob")
            
            if not evidence_type or len(evidence_type) > 64:
                raise ContractError("evidence_type must be 1-64 characters")
            
            if len(refs) > self.MAX_REFS:
                raise ContractError(f"refs count {len(refs)} exceeds max {self.MAX_REFS}")
            
            for i, ref in enumerate(refs):
                if len(ref) != 32:
                    raise ContractError(f"refs[{i}] must be 32 bytes (SHA-256)")
            
            if len(chain_of_custody) > self.MAX_COC_ENTRIES:
                raise ContractError(
                    f"chain_of_custody count {len(chain_of_custody)} exceeds max {self.MAX_COC_ENTRIES}"
                )
            
            for i, (ref_hash, actor_id, ts, sig) in enumerate(chain_of_custody):
                if len(ref_hash) != 32:
                    raise ContractError(f"coc[{i}].ref_hash must be 32 bytes")
                if len(actor_id) != 32:
                    raise ContractError(f"coc[{i}].actor_id must be 32 bytes")
                if ts <= 0:
                    raise ContractError(f"coc[{i}].ts must be positive")
                if len(sig) != 64:
                    raise ContractError(f"coc[{i}].signature must be 64 bytes")
                
        except Exception as e:
            raise ContractError(f"Evidence validation failed: {e}") from e
    
    def _build_sign_bytes(self) -> bytes:
        """Build sign bytes matching backend state.BuildSignBytes()."""
        coc_entries = [
            ai_evidence_pb2.ChainOfCustodyEntry(
                ref_hash=ref_hash,
                actor_id=actor_id,
                ts=ts,
                signature=sig,
            )
            for ref_hash, actor_id, ts, sig in self.chain_of_custody
        ]
        
        msg = ai_evidence_pb2.EvidenceEvent(
            id=self.evidence_id,
            evidence_type=self.evidence_type,
            refs=self.refs,
            proof_blob=self.proof_blob,
            coc=coc_entries,
            ts=self.timestamp,
            content_hash=self.content_hash,
            producer_id=self.pubkey,
            nonce=self.nonce,
        )
        
        return msg.SerializeToString()
    
    def to_bytes(self) -> bytes:
        """Serialize to protobuf bytes for Kafka transmission."""
        coc_entries = [
            ai_evidence_pb2.ChainOfCustodyEntry(
                ref_hash=ref_hash,
                actor_id=actor_id,
                ts=ts,
                signature=sig,
            )
            for ref_hash, actor_id, ts, sig in self.chain_of_custody
        ]
        
        msg = ai_evidence_pb2.EvidenceEvent(
            id=self.evidence_id,
            evidence_type=self.evidence_type,
            refs=self.refs,
            proof_blob=self.proof_blob,
            coc=coc_entries,
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
    ) -> "EvidenceMessage":
        """Parse and verify protobuf message."""
        try:
            msg = ai_evidence_pb2.EvidenceEvent()
            msg.ParseFromString(data)
        except Exception as e:
            raise ContractError(f"Failed to parse protobuf: {e}") from e
        
        if msg.alg != "Ed25519":
            raise ContractError(f"Unsupported signature algorithm: {msg.alg}")
        
        if len(msg.nonce) != 16:
            raise ContractError(f"Invalid nonce size: {len(msg.nonce)}")
        
        if len(msg.pubkey) != 32:
            raise ContractError(f"Invalid pubkey size: {len(msg.pubkey)}")
        
        if len(msg.signature) != 64:
            raise ContractError(f"Invalid signature size: {len(msg.signature)}")
        
        if msg.producer_id != msg.pubkey:
            raise ContractError("producer_id must equal pubkey")
        
        content_hash = hashlib.sha256(msg.proof_blob).digest()
        if msg.content_hash != content_hash:
            raise ContractError("Content hash mismatch")
        
        if nonce_manager and not nonce_manager.validate(msg.nonce):
            raise ContractError("Nonce validation failed")
        
        coc_list = [
            (entry.ref_hash, entry.actor_id, entry.ts, entry.signature)
            for entry in msg.coc
        ]
        
        coc_entries = [
            ai_evidence_pb2.ChainOfCustodyEntry(
                ref_hash=ref_hash,
                actor_id=actor_id,
                ts=ts,
                signature=sig,
            )
            for ref_hash, actor_id, ts, sig in coc_list
        ]
        
        sign_bytes = ai_evidence_pb2.EvidenceEvent(
            id=msg.id,
            evidence_type=msg.evidence_type,
            refs=list(msg.refs),
            proof_blob=msg.proof_blob,
            coc=coc_entries,
            ts=msg.ts,
            content_hash=msg.content_hash,
            producer_id=msg.producer_id,
            nonce=msg.nonce,
        ).SerializeToString()
        
        if not Signer.verify(sign_bytes, msg.signature, msg.pubkey, cls.DOMAIN):
            raise ContractError("Signature verification failed")
        
        instance = cls.__new__(cls)
        instance.evidence_id = msg.id
        instance.evidence_type = msg.evidence_type
        instance.refs = list(msg.refs)
        instance.proof_blob = msg.proof_blob
        instance.chain_of_custody = coc_list
        instance.timestamp = msg.ts
        instance.content_hash = msg.content_hash
        instance.nonce = msg.nonce
        instance.signature = msg.signature
        instance.pubkey = msg.pubkey
        instance.signer = None
        instance.nonce_manager = nonce_manager
        
        return instance
    
    def __repr__(self) -> str:
        return (
            f"EvidenceMessage(id={self.evidence_id}, type={self.evidence_type}, "
            f"refs={len(self.refs)}, coc_entries={len(self.chain_of_custody)})"
        )
