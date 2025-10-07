import hashlib
from .schema import ProtobufMessage, encode_canonical


def compute_content_hash(message: ProtobufMessage) -> str:
    canonical_bytes = encode_canonical(message)
    hash_digest = hashlib.sha256(canonical_bytes).digest()
    return hash_digest.hex()


def verify_content_hash(message: ProtobufMessage, expected_hash: str) -> bool:
    try:
        computed_hash = compute_content_hash(message)
        return computed_hash == expected_hash
    except Exception:
        return False
