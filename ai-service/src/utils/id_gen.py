import uuid
import time
import secrets
from typing import Optional


def generate_uuid4() -> str:
    """Generate a UUIDv4 string."""
    return str(uuid.uuid4())


def generate_ulid() -> str:
    """
    Generate a ULID (Universally Unique Lexicographically Sortable Identifier).
    
    Format: 26 characters (timestamp 10 + random 16)
    Properties:
    - Lexicographically sortable by creation time
    - 128-bit compatibility with UUID
    - Case insensitive, URL-safe
    - No special characters
    """
    timestamp_ms = int(time.time() * 1000)
    
    encoding = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
    
    timestamp_chars = []
    for _ in range(10):
        timestamp_chars.append(encoding[timestamp_ms % 32])
        timestamp_ms //= 32
    timestamp_chars.reverse()
    
    random_bits = secrets.randbits(80)
    random_chars = []
    for _ in range(16):
        random_chars.append(encoding[random_bits % 32])
        random_bits //= 32
    
    return ''.join(timestamp_chars) + ''.join(random_chars)


def validate_uuid4(value: str) -> bool:
    """
    Validate if string is a valid UUIDv4.
    
    Returns True if valid, False otherwise.
    """
    try:
        parsed = uuid.UUID(value, version=4)
        return str(parsed) == value
    except (ValueError, AttributeError):
        return False


def validate_ulid(value: str) -> bool:
    """
    Validate if string is a valid ULID format.
    
    Returns True if valid, False otherwise.
    """
    if len(value) != 26:
        return False
    
    valid_chars = set("0123456789ABCDEFGHJKMNPQRSTVWXYZ")
    return all(c in valid_chars for c in value.upper())


def generate_message_id(ulid: bool = False) -> str:
    """
    Generate a message ID.
    
    Args:
        ulid: If True, generate ULID; otherwise UUIDv4
    
    Returns:
        Message ID string
    """
    return generate_ulid() if ulid else generate_uuid4()


def generate_key_id(node_id: str, date_str: Optional[str] = None) -> str:
    """
    Generate a key ID for signing keys.
    
    Format: ai-node-{node_id}-{date}
    Example: ai-node-1-20250115
    """
    if date_str is None:
        date_str = time.strftime("%Y%m%d")
    
    return f"ai-node-{node_id}-{date_str}"
