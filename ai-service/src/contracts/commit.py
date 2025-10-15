"""
CommitEvent - Backend commit notification message (Backend → AI)

Topic: control.commits.v1
Backend producer: backend/pkg/ingest/kafka/producer.go
AI consumer: src/kafka/consumer.py

Security: Ed25519 signature verification from trusted backend validators
"""
import hashlib
import json
from pathlib import Path
from typing import Dict, Optional
from cryptography.hazmat.primitives.asymmetric import ed25519
from cryptography.hazmat.primitives import serialization

from ..utils.validators import validate_timestamp
from ..utils.errors import ContractError
from .generated import control_commits_pb2


class BackendValidatorTrustStore:
    """
    Trust store for backend validator public keys.
    
    Security:
    - Loads Ed25519 public keys from filesystem
    - Validates key format and size
    - Caches keys in memory
    - Quorum-based trust (3/4 validators required)
    """
    
    def __init__(self, keys_dir: Optional[Path] = None):
        """
        Initialize trust store.
        
        Args:
            keys_dir: Path to backend_validators/ directory
                     Defaults to config/keys/backend_validators/
        """
        if keys_dir is None:
            # Default to ai-service/config/keys/backend_validators/
            base = Path(__file__).parent.parent.parent
            keys_dir = base / "config" / "keys" / "backend_validators"
        
        self.keys_dir = Path(keys_dir)
        self._public_keys: Dict[int, ed25519.Ed25519PublicKey] = {}
        self._registry: Optional[dict] = None
        
        if not self.keys_dir.exists():
            raise ContractError(f"Backend validator keys directory not found: {self.keys_dir}")
        
        self._load_registry()
        self._load_public_keys()
    
    def _load_registry(self):
        """Load validator registry JSON."""
        registry_path = self.keys_dir / "validator_registry.json"
        if not registry_path.exists():
            raise ContractError(f"Validator registry not found: {registry_path}")
        
        try:
            with open(registry_path, 'r') as f:
                self._registry = json.load(f)
        except Exception as e:
            raise ContractError(f"Failed to load validator registry: {e}")
        
        # Validate registry structure
        if 'validators' not in self._registry:
            raise ContractError("Invalid registry: missing 'validators' field")
        
        if len(self._registry['validators']) < 4:
            raise ContractError(f"Invalid registry: expected 4 validators, got {len(self._registry['validators'])}")
    
    def _load_public_keys(self):
        """Load all validator public keys."""
        for validator in self._registry['validators']:
            node_id = validator['node_id']
            public_key_filename = validator['public_key_path']
            
            public_key_path = self.keys_dir / public_key_filename
            if not public_key_path.exists():
                raise ContractError(f"Validator {node_id} public key not found: {public_key_path}")
            
            try:
                with open(public_key_path, 'rb') as f:
                    public_key_pem = f.read()
                
                public_key = serialization.load_pem_public_key(public_key_pem)
                
                if not isinstance(public_key, ed25519.Ed25519PublicKey):
                    raise ContractError(f"Validator {node_id}: Invalid key type (expected Ed25519)")
                
                self._public_keys[node_id] = public_key
                
            except Exception as e:
                raise ContractError(f"Failed to load validator {node_id} public key: {e}")
    
    def get_public_key(self, node_id: int) -> Optional[ed25519.Ed25519PublicKey]:
        """Get public key for validator node_id."""
        return self._public_keys.get(node_id)
    
    def get_quorum_size(self) -> int:
        """Get quorum size (3/4 for BFT)."""
        return self._registry['bft_config']['quorum_size']
    
    def get_total_validators(self) -> int:
        """Get total validator count."""
        return self._registry['bft_config']['total_validators']


class CommitEvent:
    """
    Cryptographically signed commit notification from backend.
    
    Security:
    - Ed25519 signature verification from trusted validators
    - Timestamp validation (no future timestamps)
    - Hash validation (block_hash, state_root must be 32 bytes)
    - Domain separation: "control.commits.v1"
    
    Use case:
    Backend commits a block → AI receives notification → Update anomaly lifecycle tracker
    """
    
    DOMAIN = "control.commits.v1"
    
    # Class-level trust store (shared across all instances)
    _trust_store: Optional[BackendValidatorTrustStore] = None
    
    @classmethod
    def initialize_trust_store(cls, keys_dir: Optional[Path] = None):
        """
        Initialize backend validator trust store (call once at startup).
        
        Args:
            keys_dir: Path to backend_validators/ directory
        
        Raises:
            ContractError: If trust store initialization fails
        """
        cls._trust_store = BackendValidatorTrustStore(keys_dir)
    
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
        alg: str,
    ):
        """
        Parse and verify commit event from backend.
        
        Args:
            height: Block height (positive integer)
            block_hash: SHA-256 block hash (32 bytes)
            state_root: SHA-256 state merkle root (32 bytes)
            tx_count: Total transactions in block
            anomaly_count: Number of anomaly transactions committed
            evidence_count: Number of evidence transactions committed
            policy_count: Number of policy transactions committed
            timestamp: Unix timestamp (seconds)
            producer_id: Backend validator public key (32 bytes)
            signature: Ed25519 signature (64 bytes)
            pubkey: Ed25519 public key (32 bytes, must match producer_id)
            alg: Signature algorithm (must be "Ed25519")
        
        Raises:
            ContractError: Validation or signature verification failure
        """
        if self._trust_store is None:
            raise ContractError("Trust store not initialized. Call CommitEvent.initialize_trust_store() first.")
        
        self._validate_inputs(
            height, block_hash, state_root, tx_count, anomaly_count,
            evidence_count, policy_count, timestamp, producer_id,
            signature, pubkey, alg
        )
        
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
        self.alg = alg
        
        # Verify signature
        self._verify_signature()
    
    def _validate_inputs(
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
        alg: str,
    ):
        """Validate all inputs before signature verification."""
        try:
            # Block height
            if height < 0:
                raise ContractError(f"Invalid height: {height} (must be >= 0)")
            
            # Hashes
            if len(block_hash) != 32:
                raise ContractError(f"Invalid block_hash size: {len(block_hash)} (expected 32 bytes)")
            
            if len(state_root) != 32:
                raise ContractError(f"Invalid state_root size: {len(state_root)} (expected 32 bytes)")
            
            # Counts
            if tx_count < 0:
                raise ContractError(f"Invalid tx_count: {tx_count}")
            
            if anomaly_count < 0:
                raise ContractError(f"Invalid anomaly_count: {anomaly_count}")
            
            if evidence_count < 0:
                raise ContractError(f"Invalid evidence_count: {evidence_count}")
            
            if policy_count < 0:
                raise ContractError(f"Invalid policy_count: {policy_count}")
            
            # Sanity check: sum of typed counts shouldn't exceed total
            typed_sum = anomaly_count + evidence_count + policy_count
            if typed_sum > tx_count:
                raise ContractError(
                    f"Transaction count mismatch: anomaly({anomaly_count}) + "
                    f"evidence({evidence_count}) + policy({policy_count}) = {typed_sum} > {tx_count}"
                )
            
            # Timestamp
            validate_timestamp(timestamp)
            
            # Cryptographic fields
            if len(producer_id) != 32:
                raise ContractError(f"Invalid producer_id size: {len(producer_id)} (expected 32 bytes)")
            
            if len(signature) != 64:
                raise ContractError(f"Invalid signature size: {len(signature)} (expected 64 bytes)")
            
            if len(pubkey) != 32:
                raise ContractError(f"Invalid pubkey size: {len(pubkey)} (expected 32 bytes)")
            
            if pubkey != producer_id:
                raise ContractError("pubkey must match producer_id")
            
            if alg != "Ed25519":
                raise ContractError(f"Invalid algorithm: {alg} (expected Ed25519)")
                
        except ContractError:
            raise
        except Exception as e:
            raise ContractError(f"CommitEvent validation failed: {e}") from e
    
    def _verify_signature(self):
        """
        Verify Ed25519 signature from backend validator.
        
        Signature format: domain||height||block_hash||state_root||timestamp
        
        Raises:
            ContractError: Signature verification failure
        """
        try:
            # Build signed payload (matching backend's signing logic)
            # Backend signs: domain + height + block_hash + state_root + timestamp
            import struct
            
            sign_bytes = b""
            sign_bytes += self.DOMAIN.encode('utf-8')
            sign_bytes += struct.pack('>q', self.height)  # int64 big-endian
            sign_bytes += self.block_hash
            sign_bytes += self.state_root
            sign_bytes += struct.pack('>q', self.timestamp)  # int64 big-endian
            
            # Load validator public key from trust store
            # Producer_id is the raw public key bytes, find matching validator
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
                    f"Unknown validator public key: {self.pubkey.hex()[:16]}... "
                    f"(not in trust store)"
                )
            
            # Verify Ed25519 signature
            validator_pubkey_obj.verify(self.signature, sign_bytes)
            
        except ContractError:
            raise
        except Exception as e:
            raise ContractError(f"Signature verification failed: {e}") from e
    
    @classmethod
    def from_bytes(cls, data: bytes) -> "CommitEvent":
        """
        Deserialize from protobuf bytes.
        
        Args:
            data: Protobuf-serialized CommitEvent
        
        Returns:
            CommitEvent instance
        
        Raises:
            ContractError: Deserialization or validation failure
        """
        try:
            msg = control_commits_pb2.CommitEvent()
            msg.ParseFromString(data)
            
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
                alg=msg.alg,
            )
        except ContractError:
            raise
        except Exception as e:
            raise ContractError(f"Failed to parse CommitEvent: {e}") from e
    
    def to_dict(self) -> dict:
        """Convert to dictionary (for logging/debugging)."""
        return {
            "height": self.height,
            "block_hash": self.block_hash.hex(),
            "state_root": self.state_root.hex(),
            "tx_count": self.tx_count,
            "anomaly_count": self.anomaly_count,
            "evidence_count": self.evidence_count,
            "policy_count": self.policy_count,
            "timestamp": self.timestamp,
            "producer_id": self.producer_id.hex(),
            "alg": self.alg,
        }
