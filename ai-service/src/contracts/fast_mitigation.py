"""
FastMitigationMessage - provisional low-latency mitigation message (AI -> Enforcement).

Topic: control.fast_mitigation.v1

Security:
- Ed25519 signature verification
- Nonce-based replay protection
- Canonical JSON payload hashing
- Domain separation distinct from durable policy path
"""
from __future__ import annotations

import hashlib
import json
import struct
from typing import Any, Dict, Optional

from ..utils.errors import ContractError
from ..utils.limits import SIZE_LIMITS
from ..utils.nonce import NonceManager
from ..utils.signer import Signer
from ..utils.validators import (
    validate_required_string,
    validate_size,
    validate_timestamp,
    validate_uuid,
)


class FastMitigationMessage:
    DOMAIN = "control.fast_mitigation.v1"
    VALID_ACTIONS = {"drop", "rate_limit"}
    VALID_RULE_TYPES = {"block"}
    MAX_FAST_TTL_SECONDS = 60

    def __init__(
        self,
        *,
        mitigation_id: str,
        policy_id: str,
        action: str,
        rule_type: str,
        timestamp: int,
        payload: Dict[str, Any] | str,
        signer: Signer,
        nonce_manager: NonceManager,
    ):
        payload_bytes = self._serialize_payload(payload)
        self._validate_core_inputs(
            mitigation_id=mitigation_id,
            policy_id=policy_id,
            action=action,
            rule_type=rule_type,
            timestamp=timestamp,
            payload_bytes=payload_bytes,
        )

        self.mitigation_id = mitigation_id
        self.policy_id = policy_id
        self.action = action
        self.rule_type = rule_type
        self.timestamp = timestamp
        self.payload = payload_bytes
        self.content_hash = hashlib.sha256(payload_bytes).digest()
        self.nonce = nonce_manager.generate()
        self.pubkey = signer.public_key_bytes
        self.signature, _, self.key_id = signer.sign(
            self._build_sign_bytes(),
            domain=self.DOMAIN,
        )
        self.alg = "Ed25519"

    @staticmethod
    def _serialize_payload(payload: Dict[str, Any] | str) -> bytes:
        if isinstance(payload, str):
            payload_bytes = payload.encode("utf-8")
        elif isinstance(payload, dict):
            try:
                payload_bytes = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
            except (TypeError, ValueError) as exc:
                raise ContractError(f"failed to serialize fast mitigation payload: {exc}") from exc
        else:
            raise ContractError(f"unsupported fast mitigation payload type: {type(payload)}")
        return payload_bytes

    def _validate_core_inputs(
        self,
        *,
        mitigation_id: str,
        policy_id: str,
        action: str,
        rule_type: str,
        timestamp: int,
        payload_bytes: bytes,
    ) -> None:
        validate_uuid(mitigation_id, "mitigation_id")
        validate_uuid(policy_id, "policy_id")
        validate_required_string(action, "action", max_length=64)
        validate_required_string(rule_type, "rule_type", max_length=64)
        validate_timestamp(timestamp)
        validate_size(len(payload_bytes), SIZE_LIMITS.MAX_POLICY_PARAMS_SIZE, "payload")

        if action not in self.VALID_ACTIONS:
            raise ContractError(f"unsupported fast mitigation action: {action}")
        if rule_type not in self.VALID_RULE_TYPES:
            raise ContractError(f"unsupported fast mitigation rule_type: {rule_type}")

        try:
            payload = json.loads(payload_bytes.decode("utf-8"))
        except Exception as exc:
            raise ContractError(f"fast mitigation payload is not valid json: {exc}") from exc

        if not isinstance(payload, dict):
            raise ContractError("fast mitigation payload must be a JSON object")

        for key in ("target", "guardrails", "metadata"):
            if key not in payload or not isinstance(payload.get(key), dict):
                raise ContractError(f"fast mitigation payload missing object field: {key}")

        if payload.get("provisional") is not True:
            raise ContractError("fast mitigation payload must set provisional=true")
        if payload.get("promotion_requested") is not True:
            raise ContractError("fast mitigation payload must set promotion_requested=true")

        guardrails = payload["guardrails"]
        ttl_seconds = guardrails.get("ttl_seconds")
        if not isinstance(ttl_seconds, int) or ttl_seconds <= 0:
            raise ContractError("fast mitigation guardrails.ttl_seconds must be positive integer")
        if ttl_seconds > self.MAX_FAST_TTL_SECONDS:
            raise ContractError(f"fast mitigation guardrails.ttl_seconds exceeds max {self.MAX_FAST_TTL_SECONDS}")

        metadata = payload["metadata"]
        trace_id = metadata.get("trace_id")
        anomaly_id = metadata.get("anomaly_id")
        if not isinstance(trace_id, str) or not trace_id.strip():
            raise ContractError("fast mitigation payload metadata.trace_id is required")
        if not isinstance(anomaly_id, str) or not anomaly_id.strip():
            raise ContractError("fast mitigation payload metadata.anomaly_id is required")
        for key in ("source_event_id", "sentinel_event_id"):
            if key in metadata and metadata.get(key) is not None and not isinstance(metadata.get(key), str):
                raise ContractError(f"fast mitigation payload metadata.{key} must be a string when present")

    def _build_sign_bytes(self) -> bytes:
        mitigation_id_bytes = self.mitigation_id.encode("utf-8")
        policy_id_bytes = self.policy_id.encode("utf-8")
        action_bytes = self.action.encode("utf-8")
        rule_type_bytes = self.rule_type.encode("utf-8")
        ts_bytes = struct.pack(">q", self.timestamp)
        mitigation_id_len = struct.pack(">H", len(mitigation_id_bytes))
        policy_id_len = struct.pack(">H", len(policy_id_bytes))
        action_len = struct.pack(">H", len(action_bytes))
        rule_type_len = struct.pack(">H", len(rule_type_bytes))
        pubkey_len = struct.pack(">H", len(self.pubkey))
        if len(self.nonce) != NonceManager.NONCE_SIZE:
            raise ContractError(f"nonce must be {NonceManager.NONCE_SIZE} bytes")
        return b"".join([
            b"\x01",
            mitigation_id_len,
            mitigation_id_bytes,
            policy_id_len,
            policy_id_bytes,
            action_len,
            action_bytes,
            rule_type_len,
            rule_type_bytes,
            ts_bytes,
            pubkey_len,
            self.pubkey,
            self.nonce,
            self.content_hash,
        ])

    def to_bytes(self) -> bytes:
        envelope = {
            "schema_version": "control.fast_mitigation.v1",
            "mitigation_id": self.mitigation_id,
            "policy_id": self.policy_id,
            "action": self.action,
            "rule_type": self.rule_type,
            "timestamp": self.timestamp,
            "payload": json.loads(self.payload.decode("utf-8")),
            "content_hash": self.content_hash.hex(),
            "nonce": self.nonce.hex(),
            "signature": self.signature.hex(),
            "pubkey": self.pubkey.hex(),
            "producer_id": self.pubkey.hex(),
            "alg": self.alg,
        }
        return json.dumps(envelope, sort_keys=True, separators=(",", ":")).encode("utf-8")

    @classmethod
    def from_bytes(
        cls,
        data: bytes,
        *,
        nonce_manager: Optional[NonceManager] = None,
    ) -> "FastMitigationMessage":
        try:
            envelope = json.loads(data.decode("utf-8"))
        except Exception as exc:
            raise ContractError(f"failed to decode fast mitigation envelope: {exc}") from exc

        if not isinstance(envelope, dict):
            raise ContractError("fast mitigation envelope must be a JSON object")

        schema_version = str(envelope.get("schema_version") or "").strip()
        if schema_version != cls.DOMAIN:
            raise ContractError(f"unexpected fast mitigation schema_version: {schema_version}")

        mitigation_id = str(envelope.get("mitigation_id") or "")
        policy_id = str(envelope.get("policy_id") or "")
        action = str(envelope.get("action") or "")
        rule_type = str(envelope.get("rule_type") or "")
        timestamp = int(envelope.get("timestamp") or 0)
        payload = envelope.get("payload")
        content_hash_hex = str(envelope.get("content_hash") or "")
        nonce_hex = str(envelope.get("nonce") or "")
        signature_hex = str(envelope.get("signature") or "")
        pubkey_hex = str(envelope.get("pubkey") or "")
        alg = str(envelope.get("alg") or "")

        if alg != "Ed25519":
            raise ContractError(f"unsupported signature algorithm: {alg}")

        payload_bytes = cls._serialize_payload(payload if isinstance(payload, dict) else {})
        obj = cls.__new__(cls)
        obj._validate_core_inputs(
            mitigation_id=mitigation_id,
            policy_id=policy_id,
            action=action,
            rule_type=rule_type,
            timestamp=timestamp,
            payload_bytes=payload_bytes,
        )

        obj.mitigation_id = mitigation_id
        obj.policy_id = policy_id
        obj.action = action
        obj.rule_type = rule_type
        obj.timestamp = timestamp
        obj.payload = payload_bytes
        obj.content_hash = bytes.fromhex(content_hash_hex)
        obj.nonce = bytes.fromhex(nonce_hex)
        obj.signature = bytes.fromhex(signature_hex)
        obj.pubkey = bytes.fromhex(pubkey_hex)
        obj.key_id = ""
        obj.alg = alg

        expected_hash = hashlib.sha256(payload_bytes).digest()
        if obj.content_hash != expected_hash:
            raise ContractError("fast mitigation content hash mismatch")

        if nonce_manager and not nonce_manager.validate(obj.nonce):
            raise ContractError("fast mitigation nonce validation failed")

        if not Signer.verify(obj._build_sign_bytes(), obj.signature, obj.pubkey, cls.DOMAIN):
            raise ContractError("fast mitigation signature verification failed")

        return obj
