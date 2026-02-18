"""Shared detection contracts and canonical schemas."""

from .types import (
    ThreatType,
    DetectionCandidate,
    EnsembleDecision,
    CanonicalEvent,
    Modality,
)
from .schemas import (
    NetworkFlowFeaturesV1,
    FileFeaturesV1,
    ProcessEventV1,
    ScanFindingV1,
    ScanFindingsV1,
    ActionEventV1,
    MCPRuntimeEventV1,
    ExfilEventV1,
    ResilienceEventV1,
)
from .mapping import (
    map_threat_level_to_type,
    map_analysis_result_to_candidate,
)

__all__ = [
    "ThreatType",
    "DetectionCandidate",
    "EnsembleDecision",
    "CanonicalEvent",
    "Modality",
    "NetworkFlowFeaturesV1",
    "FileFeaturesV1",
    "ProcessEventV1",
    "ScanFindingV1",
    "ScanFindingsV1",
    "ActionEventV1",
    "MCPRuntimeEventV1",
    "ExfilEventV1",
    "ResilienceEventV1",
    "map_threat_level_to_type",
    "map_analysis_result_to_candidate",
]
