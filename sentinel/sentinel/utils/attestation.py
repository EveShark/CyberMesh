"""HMAC attestation for adapter outputs."""

from __future__ import annotations

import hashlib
import hmac
import json
from typing import Any, Dict, Optional, Tuple

from ..contracts import CanonicalEvent


def build_attestation_payload(event: CanonicalEvent) -> Dict[str, Any]:
    """Create a stable payload for signing."""
    return {
        "id": event.id,
        "tenant_id": event.tenant_id,
        "source": event.source,
        "modality": event.modality.value,
        "features_version": event.features_version,
        "features": event.features,
    }


def sign_event(event: CanonicalEvent, key: str, key_id: Optional[str] = None) -> Dict[str, Any]:
    """Create HMAC signature for a CanonicalEvent."""
    payload = build_attestation_payload(event)
    raw = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
    signature = hmac.new(key.encode("utf-8"), raw, hashlib.sha256).hexdigest()
    attestation = {
        "alg": "hmac-sha256",
        "sig": signature,
        "scope": "canonical_event_v1",
    }
    if key_id:
        attestation["kid"] = key_id
    return attestation


def verify_event(
    event: CanonicalEvent,
    key: str,
    attestation: Dict[str, Any],
) -> Tuple[bool, str]:
    """Verify HMAC attestation for a CanonicalEvent."""
    if not isinstance(attestation, dict):
        return False, "attestation_invalid_format"
    if attestation.get("alg") != "hmac-sha256":
        return False, "attestation_alg_unsupported"
    signature = attestation.get("sig")
    if not signature:
        return False, "attestation_missing_sig"
    payload = build_attestation_payload(event)
    raw = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
    expected = hmac.new(key.encode("utf-8"), raw, hashlib.sha256).hexdigest()
    if not hmac.compare_digest(signature, expected):
        return False, "attestation_invalid"
    return True, "attestation_valid"
