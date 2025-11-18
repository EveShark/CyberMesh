"""Contract helpers for control.policy.ack.v1 messages."""

from dataclasses import dataclass
from typing import Optional
import uuid

from .. import control_policy_ack_pb2
from ..utils.errors import ContractError


@dataclass(frozen=True)
class PolicyAckEvent:
    """Parsed acknowledgement emitted by the enforcement agent."""

    policy_id: str
    result: str
    reason: Optional[str]
    error_code: Optional[str]
    scope_identifier: Optional[str]
    tenant: Optional[str]
    region: Optional[str]
    qc_reference: Optional[str]
    controller_instance: Optional[str]
    fast_path: bool
    applied_at: int
    acked_at: int
    rule_hash: bytes
    producer_id: bytes

    @property
    def succeeded(self) -> bool:
        return self.result.lower() == "applied"

    @classmethod
    def from_bytes(cls, data: bytes) -> "PolicyAckEvent":
        try:
            msg = control_policy_ack_pb2.PolicyAckEvent()
            msg.ParseFromString(data)
        except Exception as exc:  # pragma: no cover - defensive
            raise ContractError(f"Failed to parse PolicyAckEvent protobuf: {exc}") from exc

        return cls._from_proto(msg)

    @classmethod
    def _from_proto(cls, msg: control_policy_ack_pb2.PolicyAckEvent) -> "PolicyAckEvent":
        policy_id = msg.policy_id.strip()
        if not policy_id:
            raise ContractError("PolicyAckEvent missing policy_id")

        try:
            uuid.UUID(policy_id)
        except ValueError as exc:
            raise ContractError(f"PolicyAckEvent invalid policy_id: {policy_id}") from exc

        result = msg.result.strip().lower()
        if result not in {"applied", "failed"}:
            raise ContractError(f"PolicyAckEvent result must be 'applied' or 'failed', got {msg.result}")

        applied_at = int(msg.applied_at)
        acked_at = int(msg.acked_at)
        if applied_at < 0 or acked_at < 0:
            raise ContractError("PolicyAckEvent timestamps must be non-negative")

        rule_hash = bytes(msg.rule_hash)
        producer_id = bytes(msg.producer_id)

        return cls(
            policy_id=policy_id,
            result=result,
            reason=msg.reason.strip() or None,
            error_code=msg.error_code.strip() or None,
            scope_identifier=msg.scope_identifier.strip() or None,
            tenant=msg.tenant.strip() or None,
            region=msg.region.strip() or None,
            qc_reference=msg.qc_reference.strip() or None,
            controller_instance=msg.controller_instance.strip() or None,
            fast_path=bool(msg.fast_path),
            applied_at=applied_at,
            acked_at=acked_at,
            rule_hash=rule_hash,
            producer_id=producer_id,
        )

    def to_dict(self) -> dict:
        return {
            "policy_id": self.policy_id,
            "result": self.result,
            "reason": self.reason,
            "error_code": self.error_code,
            "scope_identifier": self.scope_identifier,
            "tenant": self.tenant,
            "region": self.region,
            "qc_reference": self.qc_reference,
            "controller_instance": self.controller_instance,
            "fast_path": self.fast_path,
            "applied_at": self.applied_at,
            "acked_at": self.acked_at,
            "rule_hash": self.rule_hash.hex() if self.rule_hash else None,
            "producer_id": self.producer_id.hex() if self.producer_id else None,
        }
