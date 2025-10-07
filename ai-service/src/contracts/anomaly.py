"""
AnomalyMessage - AI anomaly detection message (AI → Backend)

Topic: ai.anomalies.v1
Backend consumer: backend/pkg/ingest/kafka/consumer.go
Backend validator: backend/pkg/ingest/kafka/verifier.go
"""
import hashlib
from typing import Optional
from ..utils.validators import (
    validate_uuid4,
    validate_severity,
    validate_confidence,
    validate_size,
    validate_timestamp,
)
from ..utils.limits import SIZE_LIMITS
from ..utils.nonce import NonceManager
from ..utils.signer import Signer
from ..utils.errors import ContractError
from .generated import ai_anomaly_pb2


class AnomalyMessage:
    """
    Cryptographically signed anomaly detection message.
    
    Security:
    - Ed25519 signature over protobuf-serialized payload
    - 16-byte nonce for replay protection
    - SHA-256 content hash for integrity
    - Domain separation: "ai.anomaly.v1"
    """
    
    DOMAIN = "ai.anomaly.v1"
    
    def __init__(
        self,
        anomaly_id: str,
        anomaly_type: str,
        source: str,
        severity: int,
        confidence: float,
        timestamp: int,
        payload: bytes,
        model_version: str,
        signer: Signer,
        nonce_manager: NonceManager,
    ):
        """
        Create and sign anomaly message.
        
        Args:
            anomaly_id: UUIDv4 string
            anomaly_type: Anomaly type (e.g., "ddos", "port_scan")
            source: Source IP/hostname
            severity: 1-10 (1=low, 10=critical)
            confidence: 0.0-1.0
            timestamp: Unix timestamp (seconds)
            payload: Binary payload data (max 512KB)
            model_version: AI model version string
            signer: Signer instance for Ed25519 signing
            nonce_manager: NonceManager for replay protection
        
        Raises:
            ContractError: Validation failure
        """
        self._validate_inputs(
            anomaly_id, anomaly_type, source, severity, confidence,
            timestamp, payload, model_version
        )
        
        self.anomaly_id = anomaly_id
        self.anomaly_type = anomaly_type
        self.source = source
        self.severity = severity
        self.confidence = confidence
        self.timestamp = timestamp
        self.payload = payload
        self.model_version = model_version
        self.signer = signer
        self.nonce_manager = nonce_manager
        
        self.content_hash = hashlib.sha256(payload).digest()
        self.nonce = nonce_manager.generate()
        self.pubkey = signer.public_key_bytes
        
        sign_bytes = self._build_sign_bytes()
        self.signature, _, _ = signer.sign(sign_bytes, domain=self.DOMAIN)
    
    def _validate_inputs(
        self,
        anomaly_id: str,
        anomaly_type: str,
        source: str,
        severity: int,
        confidence: float,
        timestamp: int,
        payload: bytes,
        model_version: str,
    ):
        """Validate all inputs before signing."""
        try:
            validate_uuid4(anomaly_id)
            validate_severity(severity)
            validate_confidence(confidence)
            validate_timestamp(timestamp)
            validate_size(len(payload), SIZE_LIMITS.MAX_PAYLOAD_SIZE, "payload")
            
            if not anomaly_type or len(anomaly_type) > 64:
                raise ContractError("anomaly_type must be 1-64 characters")
            
            if not source or len(source) > 256:
                raise ContractError("source must be 1-256 characters")
            
            if not model_version or len(model_version) > 64:
                raise ContractError("model_version must be 1-64 characters")
                
        except Exception as e:
            raise ContractError(f"Anomaly validation failed: {e}") from e
    
    def _build_sign_bytes(self) -> bytes:
        """
        Build sign bytes matching backend state.BuildSignBytes() format.
        
        Backend format: domain||ts(8B)||producer_id_len(2B)||producer_id||nonce(16B)||content_hash(32B)
        Domain prefix is provided by Signer.sign() via the DOMAIN constant.
        """
        import struct
        
        # Timestamp (8 bytes, big-endian int64)
        ts_bytes = struct.pack('>q', self.timestamp)
        
        # Producer ID length (2 bytes, big-endian uint16) + producer ID
        producer_id_len = struct.pack('>H', len(self.pubkey))
        producer_id_bytes = producer_id_len + self.pubkey
        
        # Nonce (16 bytes)
        if len(self.nonce) != 16:
            raise ContractError(f"nonce must be exactly 16 bytes, got {len(self.nonce)}")
        
        # Content hash (32 bytes)
        if len(self.content_hash) != 32:
            raise ContractError(f"content_hash must be exactly 32 bytes, got {len(self.content_hash)}")
        
        # Concatenate all parts (domain will be added by Signer.sign())
        return ts_bytes + producer_id_bytes + self.nonce + self.content_hash
    
    def to_bytes(self) -> bytes:
        """
        Serialize to protobuf bytes for Kafka transmission.
        
        Returns:
            Protobuf-encoded bytes
        """
        msg = ai_anomaly_pb2.AnomalyEvent(
            id=self.anomaly_id,
            type=self.anomaly_type,
            source=self.source,
            severity=self.severity,
            confidence=self.confidence,
            ts=self.timestamp,
            content_hash=self.content_hash,
            payload=self.payload,
            model_version=self.model_version,
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
    ) -> "AnomalyMessage":
        """
        Parse and verify protobuf message.
        
        Args:
            data: Protobuf-encoded bytes
            nonce_manager: Optional NonceManager for replay protection
        
        Returns:
            Parsed AnomalyMessage instance
        
        Raises:
            ContractError: Parse or verification failure
        """
        try:
            msg = ai_anomaly_pb2.AnomalyEvent()
            msg.ParseFromString(data)
        except Exception as e:
            raise ContractError(f"Failed to parse protobuf: {e}") from e
        
        if msg.alg != "Ed25519":
            raise ContractError(f"Unsupported signature algorithm: {msg.alg}")
        
        if len(msg.nonce) != 16:
            raise ContractError(f"Invalid nonce size: {len(msg.nonce)} (expected 16)")
        
        if len(msg.pubkey) != 32:
            raise ContractError(f"Invalid pubkey size: {len(msg.pubkey)} (expected 32)")
        
        if len(msg.signature) != 64:
            raise ContractError(f"Invalid signature size: {len(msg.signature)} (expected 64)")
        
        if msg.producer_id != msg.pubkey:
            raise ContractError("producer_id must equal pubkey")
        
        content_hash = hashlib.sha256(msg.payload).digest()
        if msg.content_hash != content_hash:
            raise ContractError("Content hash mismatch")
        
        if nonce_manager and not nonce_manager.validate(msg.nonce):
            raise ContractError("Nonce validation failed (replay or expired)")
        
        sign_bytes = ai_anomaly_pb2.AnomalyEvent(
            id=msg.id,
            type=msg.type,
            source=msg.source,
            severity=msg.severity,
            confidence=msg.confidence,
            ts=msg.ts,
            content_hash=msg.content_hash,
            payload=msg.payload,
            model_version=msg.model_version,
            producer_id=msg.producer_id,
            nonce=msg.nonce,
        ).SerializeToString()
        
        if not Signer.verify(sign_bytes, msg.signature, msg.pubkey, cls.DOMAIN):
            raise ContractError("Signature verification failed")
        
        instance = cls.__new__(cls)
        instance.anomaly_id = msg.id
        instance.anomaly_type = msg.type
        instance.source = msg.source
        instance.severity = msg.severity
        instance.confidence = msg.confidence
        instance.timestamp = msg.ts
        instance.payload = msg.payload
        instance.model_version = msg.model_version
        instance.content_hash = msg.content_hash
        instance.nonce = msg.nonce
        instance.signature = msg.signature
        instance.pubkey = msg.pubkey
        instance.signer = None
        instance.nonce_manager = nonce_manager
        
        return instance
    
    def __repr__(self) -> str:
        return (
            f"AnomalyMessage(id={self.anomaly_id}, type={self.anomaly_type}, "
            f"severity={self.severity}, confidence={self.confidence:.2f})"
        )
