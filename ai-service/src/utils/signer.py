from typing import Tuple
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
from cryptography.exceptions import InvalidSignature
from .errors import SignerError
from .secrets import load_private_key, load_public_key, generate_keypair as gen_keypair


class Signer:
    def __init__(self, private_key_path: str, key_id: str, domain_separation: str = "ai.v1"):
        self.key_id = key_id
        self.domain_separation = domain_separation.encode("utf-8")
        self.private_key = load_private_key(private_key_path)
        self.public_key, self.public_key_bytes = load_public_key(self.private_key)
    
    def sign(self, data: bytes, domain: str = None) -> Tuple[bytes, bytes, str]:
        """
        Sign data with Ed25519.
        
        Args:
            data: Raw data to sign
            domain: Optional domain override (uses self.domain_separation if None)
        
        Returns:
            (signature, public_key_bytes, key_id)
        """
        domain_bytes = domain.encode("utf-8") if domain else self.domain_separation
        message = domain_bytes + data
        signature = self.private_key.sign(message)
        return signature, self.public_key_bytes, self.key_id
    
    @staticmethod
    def verify(data: bytes, signature: bytes, public_key_bytes: bytes, domain_separation: str) -> bool:
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
        try:
            public_key = Ed25519PublicKey.from_public_bytes(public_key_bytes)
            message = domain_separation.encode("utf-8") + data
            public_key.verify(signature, message)
            return True
        except InvalidSignature:
            return False
        except Exception:
            return False


def generate_keypair(output_path: str) -> Tuple[bytes, bytes]:
    return gen_keypair(output_path)
