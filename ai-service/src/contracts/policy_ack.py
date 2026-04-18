"""Contract helpers for control.enforcement_ack.v1 messages."""

from dataclasses import dataclass
from typing import Optional
import uuid

from .. import control_policy_ack_pb2
from ..utils.errors import ContractError


class PolicyAckContractError(ContractError):
    """Structured contract error for policy ACK parsing failures."""

    def __init__(self, reason_code: str, message: str):
        super().__init__(message)
        self.reason_code = reason_code


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
    trace_id: Optional[str]
    source_event_id: Optional[str]
    sentinel_event_id: Optional[str]
    controller_instance: Optional[str]
    fast_path: bool
    applied_at: int
    acked_at: int
    rule_hash: bytes
    producer_id: bytes
    ack_event_id: Optional[str] = None
    request_id: Optional[str] = None
    command_id: Optional[str] = None
    workflow_id: Optional[str] = None

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
            raise PolicyAckContractError(
                "invalid_policy_ack_missing_policy_id",
                "PolicyAckEvent missing policy_id",
            )

        try:
            uuid.UUID(policy_id)
        except ValueError as exc:
            raise PolicyAckContractError(
                "invalid_policy_ack_invalid_policy_id",
                f"PolicyAckEvent invalid policy_id: {policy_id}",
            ) from exc

        result = msg.result.strip().lower()
        if result not in {"applied", "failed", "rejected"}:
            raise PolicyAckContractError(
                "invalid_policy_ack_invalid_result",
                f"PolicyAckEvent result must be 'applied', 'failed', or 'rejected', got {msg.result}",
            )

        applied_at = int(msg.applied_at)
        acked_at = int(msg.acked_at)
        if applied_at < 0 or acked_at < 0:
            raise PolicyAckContractError(
                "invalid_policy_ack_negative_timestamps",
                "PolicyAckEvent timestamps must be non-negative",
            )

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
            trace_id=getattr(msg, "trace_id", "").strip() or None,
            source_event_id=getattr(msg, "source_event_id", "").strip() or None,
            sentinel_event_id=getattr(msg, "sentinel_event_id", "").strip() or None,
            request_id=getattr(msg, "request_id", "").strip() or None,
            command_id=getattr(msg, "command_id", "").strip() or None,
            workflow_id=getattr(msg, "workflow_id", "").strip() or None,
            controller_instance=msg.controller_instance.strip() or None,
            fast_path=bool(msg.fast_path),
            applied_at=applied_at,
            acked_at=acked_at,
            rule_hash=rule_hash,
            producer_id=producer_id,
            ack_event_id=getattr(msg, "ack_event_id", "").strip() or None,
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
            "trace_id": self.trace_id,
            "source_event_id": self.source_event_id,
            "sentinel_event_id": self.sentinel_event_id,
            "ack_event_id": self.ack_event_id,
            "request_id": self.request_id,
            "command_id": self.command_id,
            "workflow_id": self.workflow_id,
            "controller_instance": self.controller_instance,
            "fast_path": self.fast_path,
            "applied_at": self.applied_at,
            "acked_at": self.acked_at,
            "rule_hash": self.rule_hash.hex() if self.rule_hash else None,
            "producer_id": self.producer_id.hex() if self.producer_id else None,
        }
