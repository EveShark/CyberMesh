import os
from typing import Tuple
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey, Ed25519PublicKey
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from .errors import SecretsError


def load_private_key(path: str) -> Ed25519PrivateKey:
    """
    Load an Ed25519 private key from PEM file.
    
    Security requirements:
    - File must exist and be readable
    - File permissions should be 0600 (owner read/write only)
    - Key must be valid Ed25519 format
    """
    if not os.path.exists(path):
        raise SecretsError(f"Private key file not found: {path}")
    
    # Skip permission check on Windows or in development
    import sys
    environment = (os.getenv('ENVIRONMENT') or '').lower()
    if environment == 'production':
        if sys.platform == 'win32':
            raise SecretsError(
                "Windows hosts are unsupported for production key storage; move the private key to a POSIX-compliant host."
            )
        stat_info = os.stat(path)
        if stat_info.st_mode & 0o077:
            raise SecretsError(
                f"Private key file {path} has insecure permissions. "
                f"Expected 0600, got {oct(stat_info.st_mode & 0o777)}"
            )
    
    try:
        with open(path, "rb") as f:
            key_data = f.read()
        
        private_key = serialization.load_pem_private_key(
            key_data,
            password=None
        )
        
        if not isinstance(private_key, Ed25519PrivateKey):
            raise SecretsError("Key is not an Ed25519 private key")
        
        return private_key
    
    except SecretsError:
        raise
    except Exception as e:
        raise SecretsError(f"Failed to load private key: {e}")


def load_public_key(private_key: Ed25519PrivateKey) -> Tuple[Ed25519PublicKey, bytes]:
    """
    Extract public key from private key.
    
    Returns:
        Tuple of (PublicKey object, raw bytes)
    """
    try:
        public_key = private_key.public_key()
        public_key_bytes = public_key.public_bytes(
            encoding=serialization.Encoding.Raw,
            format=serialization.PublicFormat.Raw
        )
        return public_key, public_key_bytes
    except Exception as e:
        raise SecretsError(f"Failed to extract public key: {e}")


def generate_keypair(output_path: str) -> Tuple[bytes, bytes]:
    """
    Generate a new Ed25519 keypair and save to file.
    
    Returns:
        Tuple of (private_pem, public_raw)
    """
    try:
        private_key = Ed25519PrivateKey.generate()
        public_key = private_key.public_key()
        
        private_pem = private_key.private_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PrivateFormat.PKCS8,
            encryption_algorithm=serialization.NoEncryption()
        )
        
        public_raw = public_key.public_bytes(
            encoding=serialization.Encoding.Raw,
            format=serialization.PublicFormat.Raw
        )
        
        os.makedirs(os.path.dirname(output_path) or '.', exist_ok=True)
        
        with open(output_path, "wb") as f:
            f.write(private_pem)
        
        os.chmod(output_path, 0o600)
        
        return private_pem, public_raw
    
    except Exception as e:
        raise SecretsError(f"Failed to generate keypair: {e}")


class AESEncryptor:
    """
    AES-256-GCM encryption for at-rest data (model artifacts, cached secrets).
    
    Security properties:
    - Authenticated encryption (prevents tampering)
    - 256-bit key strength
    - 96-bit nonces (never reused)
    - 128-bit authentication tag
    """
    
    def __init__(self, key: bytes):
        if len(key) != 32:
            raise SecretsError("AES-256 requires 32-byte key")
        
        self._cipher = AESGCM(key)
    
    @classmethod
    def from_hex(cls, key_hex: str) -> 'AESEncryptor':
        """Create encryptor from hex-encoded key."""
        try:
            key = bytes.fromhex(key_hex)
        except ValueError:
            raise SecretsError("Invalid hex-encoded key")
        
        return cls(key)
    
    @staticmethod
    def generate_key() -> bytes:
        """Generate a new 256-bit AES key."""
        return os.urandom(32)
    
    def encrypt(self, plaintext: bytes, associated_data: bytes = b"") -> Tuple[bytes, bytes]:
        """
        Encrypt plaintext with optional associated data.
        
        Returns:
            Tuple of (nonce, ciphertext)
        """
        try:
            nonce = os.urandom(12)
            ciphertext = self._cipher.encrypt(nonce, plaintext, associated_data)
            return nonce, ciphertext
        except Exception as e:
            raise SecretsError(f"Encryption failed: {e}")
    
    def decrypt(self, nonce: bytes, ciphertext: bytes, associated_data: bytes = b"") -> bytes:
        """
        Decrypt ciphertext with nonce and optional associated data.
        
        Raises SecretsError if authentication fails.
        """
        try:
            plaintext = self._cipher.decrypt(nonce, ciphertext, associated_data)
            return plaintext
        except Exception as e:
            raise SecretsError(f"Decryption failed (authentication error): {e}")


def load_or_generate_aes_key(path: str) -> bytes:
    """
    Load AES key from file, or generate and save if not exists.
    
    File format: 64 hex characters (32 bytes)
    """
    if os.path.exists(path):
        stat_info = os.stat(path)
        if stat_info.st_mode & 0o077:
            raise SecretsError(
                f"AES key file {path} has insecure permissions. "
                f"Expected 0600, got {oct(stat_info.st_mode & 0o777)}"
            )
        
        try:
            with open(path, "r") as f:
                key_hex = f.read().strip()
            
            key = bytes.fromhex(key_hex)
            
            if len(key) != 32:
                raise SecretsError(f"Invalid AES key length: {len(key)} bytes")
            
            return key
        
        except SecretsError:
            raise
        except Exception as e:
            raise SecretsError(f"Failed to load AES key: {e}")
    
    else:
        key = AESEncryptor.generate_key()
        
        os.makedirs(os.path.dirname(path) or '.', exist_ok=True)
        
        with open(path, "w") as f:
            f.write(key.hex())
        
        os.chmod(path, 0o600)
        
        return key

