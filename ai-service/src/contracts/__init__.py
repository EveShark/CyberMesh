"""
CyberMesh Contracts Layer

Message wrappers for AI ↔ Backend communication via Kafka.

Producer Messages (AI → Backend):
- AnomalyMessage: Detected security anomalies
- EvidenceMessage: Supporting evidence with chain-of-custody
- PolicyMessage: Policy violation submissions

Consumer Messages (Backend → AI):
- CommitEvent: Block commit notifications
- ReputationEvent: AI quality feedback
- PolicyUpdateEvent: Dynamic policy updates
- EvidenceRequestEvent: Evidence requests and validation results
"""

from .anomaly import AnomalyMessage
from .evidence import EvidenceMessage
from .commit import CommitEvent, BackendValidatorTrustStore
from .policy import PolicyUpdateEvent, PolicyMessage

# TODO: These are old placeholder implementations, not yet updated
from .reputation import ReputationEvent
from .evidence_request import EvidenceRequestEvent

__all__ = [
    # AI -> Backend (producers)
    "AnomalyMessage",
    "EvidenceMessage",
    "PolicyMessage",
    # Backend -> AI (consumers)
    "CommitEvent",
    "PolicyUpdateEvent",
    "BackendValidatorTrustStore",
    # Placeholders (not yet implemented)
    "ReputationEvent",
    "EvidenceRequestEvent",
]
