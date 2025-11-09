"""
Pseudonymous token generation for sensitive network identifiers.

Security:
- Uses HMAC-SHA256 with a secret key sourced from environment
- Never logs raw inputs or keys
- Deterministic per (secret, input)

Env precedence (first available is used):
- TOKEN_HMAC_SECRET (hex or raw string)
- AI_RATE_LIMIT_SECRET (hex)
- SECRET_KEY (raw string)

In production, at least one must be set; otherwise tokenization falls back
to a constant key which is NOT allowed for production (guarded).
"""
import hmac
import hashlib
import os
from typing import Optional, Any


_FALLBACK_DEV_KEY = b"cybermesh-dev-token-key"


def _load_key() -> bytes:
    env = (os.getenv("ENVIRONMENT") or "").lower()

    # Highest priority: explicit token secret
    key = os.getenv("TOKEN_HMAC_SECRET")
    if key:
        try:
            return bytes.fromhex(key)
        except ValueError:
            return key.encode("utf-8")

    # Next: rate limit secret (hex)
    rl = os.getenv("AI_RATE_LIMIT_SECRET")
    if rl:
        try:
            return bytes.fromhex(rl)
        except ValueError:
            pass

    # Next: generic secret key (raw)
    sk = os.getenv("SECRET_KEY")
    if sk:
        return sk.encode("utf-8")

    # Development fallback (never allowed in production)
    if env == "production":
        raise RuntimeError("Tokenization secret not configured in production")
    return _FALLBACK_DEV_KEY


def hmac_token(value: str, key: Optional[bytes] = None) -> str:
    """Return hex-encoded HMAC-SHA256 over the provided string value."""
    if value is None:
        value = ""
    k = key or _load_key()
    mac = hmac.new(k, value.encode("utf-8"), hashlib.sha256).digest()
    return mac.hex()


def token_ip(ip: Optional[str]) -> str:
    """Tokenize an IP string (IPv4/IPv6 as plain string)."""
    return hmac_token(ip or "")


def _normalize_component(value: Optional[Any]) -> str:
    if value is None:
        return ""
    if isinstance(value, bytes):
        try:
            return value.decode("utf-8", errors="ignore").lower()
        except Exception:
            return value.hex()
    text = str(value)
    return text.lower()


def token_flow_key(
    src_ip: Optional[Any],
    dst_ip: Optional[Any],
    src_port: Optional[int],
    dst_port: Optional[int],
    proto: Optional[Any],
) -> str:
    """
    Tokenize a 5-tuple flow identifier. Ports and proto normalized to strings.
    Layout: src|dst|sport|dport|proto (all lowercased)
    """
    parts = [
        _normalize_component(src_ip),
        _normalize_component(dst_ip),
        str(src_port or 0),
        str(dst_port or 0),
        _normalize_component(proto),
    ]
    return hmac_token("|".join(parts))
