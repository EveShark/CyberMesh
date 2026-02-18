"""
Event builders for creating CanonicalEvents from canonical features.

Supports both file and network flow modalities.
"""

import uuid
import time
from typing import Dict, Any, Optional, List

from ..contracts import CanonicalEvent, Modality
from ..contracts.schemas import (
    FileFeaturesV1,
    NetworkFlowFeaturesV1,
    ProcessEventV1,
    ScanFindingsV1,
)


def build_file_event(
    features: FileFeaturesV1,
    raw_context: Dict[str, Any],
    tenant_id: str,
    source: str = "sentinel_file_agent",
    event_id: Optional[str] = None,
    timestamp: Optional[float] = None,
) -> CanonicalEvent:
    """
    Build a CanonicalEvent for file analysis.
    
    Args:
        features: FileFeaturesV1 with file features
        raw_context: Additional context (must include "file_path")
        tenant_id: Tenant identifier
        source: Event source identifier
        event_id: Optional event ID (generated if not provided)
        timestamp: Optional timestamp (current time if not provided)
        
    Returns:
        CanonicalEvent ready for SentinelAgent.analyze()
        
    Raises:
        ValueError: If raw_context missing required fields
    """
    if "file_path" not in raw_context:
        raise ValueError("raw_context must contain 'file_path'")
    
    return CanonicalEvent(
        id=event_id or str(uuid.uuid4()),
        timestamp=timestamp or time.time(),
        source=source,
        tenant_id=tenant_id,
        modality=Modality.FILE,
        features_version="FileFeaturesV1",
        features=features.to_dict(),
        raw_context=raw_context,
    )


def build_file_event_from_path(
    file_path: str,
    tenant_id: str,
    source: str = "sentinel_file_agent",
) -> CanonicalEvent:
    """
    Build a minimal CanonicalEvent from just a file path.
    
    This creates an event with minimal features that SentinelAgent
    will populate by parsing the file.
    
    Args:
        file_path: Path to file to analyze
        tenant_id: Tenant identifier
        source: Event source identifier
        
    Returns:
        CanonicalEvent with minimal features
    """
    from pathlib import Path
    
    path = Path(file_path)
    
    # Minimal features - SentinelAgent will re-parse anyway
    features = FileFeaturesV1(
        sha256="",  # Will be populated by parser
        file_name=path.name,
        file_size=path.stat().st_size if path.exists() else 0,
        file_type="unknown",
        entropy=0.0,
    )
    
    return CanonicalEvent(
        id=str(uuid.uuid4()),
        timestamp=time.time(),
        source=source,
        tenant_id=tenant_id,
        modality=Modality.FILE,
        features_version="FileFeaturesV1",
        features=features.to_dict(),
        raw_context={"file_path": str(path.absolute())},
    )


def build_flow_event(
    features: NetworkFlowFeaturesV1,
    raw_context: Dict[str, Any],
    tenant_id: str,
    source: str = "flow_adapter",
    event_id: Optional[str] = None,
    timestamp: Optional[float] = None,
    labels: Optional[Dict[str, str]] = None,
) -> CanonicalEvent:
    """
    Build a CanonicalEvent for network flow analysis.
    
    Args:
        features: NetworkFlowFeaturesV1 with flow features
        raw_context: Additional context (e.g., db_id, original_label)
        tenant_id: Tenant identifier
        source: Event source identifier (e.g., "cicddos2019_adapter")
        event_id: Optional event ID (generated if not provided)
        timestamp: Optional timestamp (current time if not provided)
        labels: Optional labels for training/evaluation (e.g., {"attack_type": "ddos"})
        
    Returns:
        CanonicalEvent with modality=NETWORK_FLOW
    """
    return CanonicalEvent(
        id=event_id or str(uuid.uuid4()),
        timestamp=timestamp or time.time(),
        source=source,
        tenant_id=tenant_id,
        modality=Modality.NETWORK_FLOW,
        features_version="NetworkFlowFeaturesV1",
        features=features.to_dict(),
        raw_context=raw_context,
        labels=labels or {},
    )


def build_flow_events_batch(
    features_list: List[NetworkFlowFeaturesV1],
    raw_contexts: List[Dict[str, Any]],
    tenant_id: str,
    source: str = "flow_adapter",
    labels_list: Optional[List[Dict[str, str]]] = None,
) -> List[CanonicalEvent]:
    """
    Build multiple CanonicalEvents for a batch of flows.
    
    Args:
        features_list: List of NetworkFlowFeaturesV1
        raw_contexts: List of raw contexts (same length as features_list)
        tenant_id: Tenant identifier
        source: Event source identifier
        labels_list: Optional list of labels (same length as features_list)
        
    Returns:
        List of CanonicalEvents
        
    Raises:
        ValueError: If list lengths don't match
    """
    if len(features_list) != len(raw_contexts):
        raise ValueError(
            f"features_list ({len(features_list)}) and raw_contexts ({len(raw_contexts)}) "
            "must have same length"
        )
    
    if labels_list is not None and len(labels_list) != len(features_list):
        raise ValueError(
            f"labels_list ({len(labels_list)}) must match features_list ({len(features_list)})"
        )
    
    events = []
    base_ts = time.time()
    
    for i, (features, raw_ctx) in enumerate(zip(features_list, raw_contexts)):
        labels = labels_list[i] if labels_list else None
        event = build_flow_event(
            features=features,
            raw_context=raw_ctx,
            tenant_id=tenant_id,
            source=source,
            timestamp=base_ts + (i * 0.001),  # Slight offset for ordering
            labels=labels,
        )
        events.append(event)
    
    return events


def build_process_event(
    features: ProcessEventV1,
    raw_context: Dict[str, Any],
    tenant_id: str,
    source: str = "process_adapter",
    event_id: Optional[str] = None,
    timestamp: Optional[float] = None,
    labels: Optional[Dict[str, str]] = None,
) -> CanonicalEvent:
    """Build a CanonicalEvent for process/runtime analysis."""
    return CanonicalEvent(
        id=event_id or str(uuid.uuid4()),
        timestamp=timestamp or time.time(),
        source=source,
        tenant_id=tenant_id,
        modality=Modality.PROCESS_EVENT,
        features_version="ProcessEventV1",
        features=features.to_dict(),
        raw_context=raw_context,
        labels=labels or {},
    )


def build_scan_findings_event(
    features: ScanFindingsV1,
    raw_context: Dict[str, Any],
    tenant_id: str,
    source: str = "scanner_adapter",
    event_id: Optional[str] = None,
    timestamp: Optional[float] = None,
    labels: Optional[Dict[str, str]] = None,
) -> CanonicalEvent:
    """Build a CanonicalEvent for scan/rule findings analysis."""
    return CanonicalEvent(
        id=event_id or str(uuid.uuid4()),
        timestamp=timestamp or time.time(),
        source=source,
        tenant_id=tenant_id,
        modality=Modality.SCAN_FINDINGS,
        features_version="ScanFindingsV1",
        features=features.to_dict(),
        raw_context=raw_context,
        labels=labels or {},
    )
