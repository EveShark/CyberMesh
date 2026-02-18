"""Ed25519 signature verification for model integrity."""

import hashlib
import logging
from pathlib import Path
from typing import Optional, Tuple

from .errors import SignerError

# Use standard logging to avoid circular import
logger = logging.getLogger(__name__)

# Try to import cryptography for Ed25519 support
try:
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
    from cryptography.exceptions import InvalidSignature
    CRYPTO_AVAILABLE = True
except ImportError:
    CRYPTO_AVAILABLE = False
    logger.warning("cryptography not installed - signature verification disabled")


class Signer:
    """
    Ed25519 signature operations.
    
    Used for verifying model integrity and signing analysis results.
    """
    
    def __init__(
        self,
        private_key_path: Optional[str] = None,
        key_id: str = "sentinel-1",
        domain_separation: str = "sentinel.v1"
    ):
        """
        Initialize signer.
        
        Args:
            private_key_path: Path to Ed25519 private key (PEM format)
            key_id: Identifier for this signing key
            domain_separation: Domain string for cryptographic separation
        """
        self.key_id = key_id
        self.domain_separation = domain_separation.encode("utf-8")
        self.private_key = None
        self.public_key = None
        self.public_key_bytes = None
        
        if private_key_path and Path(private_key_path).exists():
            self._load_private_key(private_key_path)
    
    def _load_private_key(self, path: str) -> None:
        """Load Ed25519 private key from PEM file."""
        if not CRYPTO_AVAILABLE:
            raise SignerError("cryptography library not installed")
        
        try:
            from cryptography.hazmat.primitives import serialization
            
            with open(path, "rb") as f:
                self.private_key = serialization.load_pem_private_key(
                    f.read(),
                    password=None,
                )
            
            self.public_key = self.private_key.public_key()
            self.public_key_bytes = self.public_key.public_bytes(
                encoding=serialization.Encoding.Raw,
                format=serialization.PublicFormat.Raw,
            )
            
            logger.info(f"Loaded signing key: {self.key_id}")
            
        except Exception as e:
            raise SignerError(f"Failed to load private key: {e}")
    
    def sign(self, data: bytes, domain: Optional[str] = None) -> Tuple[bytes, bytes, str]:
        """
        Sign data with Ed25519.
        
        Args:
            data: Raw data to sign
            domain: Optional domain override
            
        Returns:
            (signature, public_key_bytes, key_id)
        """
        if not self.private_key:
            raise SignerError("No private key loaded")
        
        domain_bytes = domain.encode("utf-8") if domain else self.domain_separation
        message = domain_bytes + data
        signature = self.private_key.sign(message)
        
        return signature, self.public_key_bytes, self.key_id
    
    @staticmethod
    def verify(
        data: bytes,
        signature: bytes,
        public_key_bytes: bytes,
        domain_separation: str
    ) -> bool:
        """
        Verify Ed25519 signature.
        
        Args:
            data: Raw data that was signed
            signature: Ed25519 signature (64 bytes)
            public_key_bytes: Ed25519 public key (32 bytes)
            domain_separation: Domain string for cryptographic separation
            
        Returns:
            True if signature valid, False otherwise
        """
        if not CRYPTO_AVAILABLE:
            logger.warning("Signature verification skipped - cryptography not installed")
            return True
        
        try:
            public_key = Ed25519PublicKey.from_public_bytes(public_key_bytes)
            message = domain_separation.encode("utf-8") + data
            public_key.verify(signature, message)
            return True
        except InvalidSignature:
            return False
        except Exception as e:
            logger.error(f"Signature verification error: {e}")
            return False


def compute_file_fingerprint(file_path: str) -> str:
    """
    Compute SHA256 fingerprint of a file.
    
    Args:
        file_path: Path to file
        
    Returns:
        Hex-encoded SHA256 hash
    """
    hasher = hashlib.sha256()
    
    with open(file_path, "rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            hasher.update(chunk)
    
    return hasher.hexdigest()


def verify_model_fingerprint(
    model_path: str,
    expected_fingerprint: str,
    strict: bool = False
) -> bool:
    """
    Verify model file integrity by checking fingerprint.
    
    Args:
        model_path: Path to model file (.pkl)
        expected_fingerprint: Expected SHA256 fingerprint from registry
        strict: If True, raise error on mismatch. If False, log warning.
        
    Returns:
        True if fingerprint matches, False otherwise
    """
    if not expected_fingerprint:
        logger.debug(f"No fingerprint for {model_path}, skipping verification")
        return True
    
    actual = compute_file_fingerprint(model_path)
    
    if actual == expected_fingerprint:
        logger.debug(f"Model fingerprint verified: {Path(model_path).name}")
        return True
    
    message = (
        f"Model fingerprint mismatch for {Path(model_path).name}: "
        f"expected {expected_fingerprint[:16]}..., got {actual[:16]}..."
    )
    
    if strict:
        raise SignerError(message)
    
    logger.warning(message)
    return False


def generate_keypair(output_path: str) -> Tuple[bytes, bytes]:
    """
    Generate new Ed25519 keypair.
    
    Args:
        output_path: Path to save private key (PEM format)
        
    Returns:
        (private_key_bytes, public_key_bytes)
    """
    if not CRYPTO_AVAILABLE:
        raise SignerError("cryptography library not installed")
    
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    from cryptography.hazmat.primitives import serialization
    
    private_key = Ed25519PrivateKey.generate()
    public_key = private_key.public_key()
    
    # Save private key
    private_pem = private_key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    )
    
    with open(output_path, "wb") as f:
        f.write(private_pem)
    
    # Get raw bytes
    private_bytes = private_key.private_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PrivateFormat.Raw,
    )
    
    public_bytes = public_key.public_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PublicFormat.Raw,
    )
    
    logger.info(f"Generated keypair: {output_path}")
    
    return private_bytes, public_bytes
