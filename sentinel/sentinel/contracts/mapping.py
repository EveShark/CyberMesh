"""
Mapping functions between Sentinel types and unified contracts.

These functions translate Sentinel's native types (ThreatLevel, AnalysisResult)
to the shared contract types (ThreatType, DetectionCandidate).
"""

from typing import Optional, Dict, Any

from .types import ThreatType, DetectionCandidate
from ..providers.base import ThreatLevel, AnalysisResult


def map_threat_level_to_type(
    threat_level: ThreatLevel,
    context: Optional[Dict[str, Any]] = None
) -> Optional[ThreatType]:
    """
    Map Sentinel ThreatLevel to unified ThreatType.
    
    Args:
        threat_level: Sentinel's threat level enum
        context: Optional context for more specific mapping
            - is_c2: bool - C2 communication detected
            - is_botnet: bool - Botnet activity detected
            - is_network_flow: bool - Network flow analysis
    
    Returns:
        ThreatType or None if no candidate should be emitted
    """
    # CLEAN and UNKNOWN don't produce candidates
    if threat_level in (ThreatLevel.CLEAN, ThreatLevel.UNKNOWN):
        return None
    
    # Check context for specific threat types
    if context:
        if context.get("is_c2"):
            return ThreatType.C2_COMMUNICATION
        if context.get("is_botnet"):
            return ThreatType.BOTNET
        if context.get("is_network_flow"):
            if threat_level == ThreatLevel.SUSPICIOUS:
                return ThreatType.NETWORK_INTRUSION
            else:
                return ThreatType.DDOS
    
    # Default mapping
    if threat_level == ThreatLevel.SUSPICIOUS:
        return ThreatType.ANOMALY
    elif threat_level in (ThreatLevel.MALICIOUS, ThreatLevel.CRITICAL):
        return ThreatType.MALWARE
    
    return ThreatType.ANOMALY


def map_analysis_result_to_candidate(
    result: AnalysisResult,
    agent_prefix: str = "sentinel",
    context: Optional[Dict[str, Any]] = None
) -> Optional[DetectionCandidate]:
    """
    Map Sentinel AnalysisResult to unified DetectionCandidate.
    
    Args:
        result: Sentinel's analysis result from a provider
        agent_prefix: Prefix for agent_id (default: "sentinel")
        context: Optional context for threat type mapping
    
    Returns:
        DetectionCandidate or None if result is CLEAN/UNKNOWN
    """
    # Map threat level to type
    threat_type = map_threat_level_to_type(result.threat_level, context)
    if threat_type is None:
        return None
    
    # Determine agent category from provider name
    provider_name = result.provider_name.lower()
    if "ml" in provider_name or "malware" in provider_name:
        category = "ml"
    elif "intel" in provider_name or "vt" in provider_name or "abuse" in provider_name:
        category = "intel"
    elif "yara" in provider_name:
        category = "yara"
    else:
        category = "static"
    
    # Build agent_id
    agent_id = f"{agent_prefix}.{category}.{result.provider_name}"
    
    # Convert indicators to list of dicts
    indicators = []
    for ind in result.indicators:
        indicators.append({
            "type": ind.type,
            "value": ind.value,
            "context": ind.context,
            "confidence": ind.confidence,
        })
    
    # Build metadata
    metadata = {
        "provider_name": result.provider_name,
        "provider_version": result.provider_version,
        "latency_ms": result.latency_ms,
        "schema_version": "FileFeaturesV1",  # Sentinel is file-based
        **result.metadata,
    }
    if result.error:
        metadata["error"] = result.error
    
    return DetectionCandidate(
        agent_id=agent_id,
        signal_id=result.provider_name,
        threat_type=threat_type,
        raw_score=result.score,
        calibrated_score=result.score,  # Sentinel doesn't calibrate separately
        confidence=result.confidence,
        features={},  # Could extract from metadata if available
        findings=result.findings,
        indicators=indicators,
        metadata=metadata,
    )


def build_agent_id(system: str, category: str, name: str) -> str:
    """
    Build hierarchical agent ID.
    
    Convention: {system}.{category}.{name}
    
    Examples:
        - cybermesh.ml.ddos
        - sentinel.static.entropy
        - sentinel.intel.crowdsec
    """
    return f"{system}.{category}.{name}"
