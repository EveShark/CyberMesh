"""SentinelAgent wrapper for CyberMesh integration."""

import sys
import time
from pathlib import Path
from typing import List, Optional, Dict, Any

from .detection_agent import DetectionAgent
from .contracts import (
    DetectionCandidate,
    CanonicalEvent,
    Modality,
    ThreatType,
)
from ...logging import get_logger

logger = get_logger(__name__)

# Sentinel import path - configurable via environment
SENTINEL_PATH = None


def _ensure_sentinel_path():
    """Add Sentinel to sys.path if not already present."""
    global SENTINEL_PATH
    if SENTINEL_PATH is None:
        import os
        sentinel_path = os.getenv('SENTINEL_PATH', 'B:\\sentinel')
        if sentinel_path and sentinel_path not in sys.path:
            sys.path.insert(0, sentinel_path)
        SENTINEL_PATH = sentinel_path


def _convert_sentinel_candidate(sentinel_candidate) -> DetectionCandidate:
    """
    Convert Sentinel's DetectionCandidate to CyberMesh's DetectionCandidate.
    
    Both use the same schema, but may be different class instances.
    """
    # Map Sentinel ThreatType to CyberMesh ThreatType
    threat_type_map = {
        'ddos': ThreatType.DDOS,
        'dos': ThreatType.DOS,
        'malware': ThreatType.MALWARE,
        'anomaly': ThreatType.ANOMALY,
        'network_intrusion': ThreatType.NETWORK_INTRUSION,
        'policy_violation': ThreatType.POLICY_VIOLATION,
        'botnet': ThreatType.BOTNET,
        'c2': ThreatType.C2_COMMUNICATION,
    }
    
    threat_value = sentinel_candidate.threat_type
    if hasattr(threat_value, 'value'):
        threat_value = threat_value.value
    
    return DetectionCandidate(
        agent_id=sentinel_candidate.agent_id,
        signal_id=sentinel_candidate.signal_id,
        threat_type=threat_type_map.get(threat_value, ThreatType.ANOMALY),
        raw_score=sentinel_candidate.raw_score,
        calibrated_score=sentinel_candidate.calibrated_score,
        confidence=sentinel_candidate.confidence,
        features=dict(sentinel_candidate.features) if sentinel_candidate.features else {},
        findings=list(sentinel_candidate.findings) if sentinel_candidate.findings else [],
        indicators=list(sentinel_candidate.indicators) if sentinel_candidate.indicators else [],
        metadata=dict(sentinel_candidate.metadata) if sentinel_candidate.metadata else {},
    )


def _convert_to_sentinel_event(event: CanonicalEvent):
    """
    Convert CyberMesh CanonicalEvent to Sentinel CanonicalEvent.
    
    Both use the same schema, but may be different class instances.
    """
    _ensure_sentinel_path()
    from sentinel.contracts import CanonicalEvent as SentinelCanonicalEvent
    from sentinel.contracts import Modality as SentinelModality
    
    modality_map = {
        Modality.NETWORK_FLOW: SentinelModality.NETWORK_FLOW,
        Modality.FILE: SentinelModality.FILE,
        Modality.DNS: SentinelModality.DNS,
        Modality.PROXY: SentinelModality.PROXY,
    }
    
    return SentinelCanonicalEvent(
        id=event.id,
        timestamp=event.timestamp,
        source=event.source,
        tenant_id=event.tenant_id,
        modality=modality_map.get(event.modality, SentinelModality.FILE),
        features_version=event.features_version,
        features=dict(event.features),
        raw_context=dict(event.raw_context),
        labels=dict(event.labels) if event.labels else {},
    )


class SentinelFileAgent(DetectionAgent):
    """
    DetectionAgent wrapper that delegates file analysis to Sentinel.
    
    Uses library mode integration - imports Sentinel modules directly
    and runs analysis in-process for lower latency.
    
    Configuration:
        - SENTINEL_PATH env var: Path to Sentinel installation
        - SENTINEL_ENABLED env var: Enable/disable this agent
    """
    
    def __init__(
        self,
        sentinel_path: Optional[str] = None,
        config: Optional[Dict] = None,
    ):
        """
        Initialize SentinelFileAgent.
        
        Args:
            sentinel_path: Path to Sentinel installation (or use SENTINEL_PATH env)
            config: Optional configuration dict
        """
        self._config = config or {}
        self._sentinel_agent = None
        self._initialized = False
        self._init_error: Optional[str] = None
        
        # Set path before initialization
        if sentinel_path:
            global SENTINEL_PATH
            if sentinel_path not in sys.path:
                sys.path.insert(0, sentinel_path)
            SENTINEL_PATH = sentinel_path
        
        # Lazy initialization - defer until first use
        self._lazy_init_attempted = False
    
    def _lazy_init(self) -> bool:
        """
        Lazily initialize Sentinel components.
        
        Returns:
            True if initialization succeeded
        """
        if self._lazy_init_attempted:
            return self._initialized
        
        self._lazy_init_attempted = True
        
        try:
            _ensure_sentinel_path()
            
            from sentinel.agents import SentinelAgent
            from sentinel.providers.registry import ProviderRegistry
            from sentinel.router.smart_router import SmartRouter, RoutingConfig, RoutingStrategy
            
            # Import and register providers
            from sentinel.providers.static import EntropyProvider, StringsProvider
            from sentinel.providers.ml import MalwarePEMLProvider
            from sentinel.providers.yara_provider import YaraProvider
            from sentinel.providers.threat_intel import ThreatIntelProvider
            
            registry = ProviderRegistry()
            
            # Register static providers
            registry.register(EntropyProvider())
            registry.register(StringsProvider())
            
            # Register ML provider (may fail if models not available)
            try:
                registry.register(MalwarePEMLProvider())
            except Exception as e:
                logger.warning(f"MalwarePEMLProvider not available: {e}")
            
            # Register YARA provider
            try:
                rules_path = self._config.get('yara_rules_path')
                if rules_path:
                    registry.register(YaraProvider(rules_path=rules_path))
                else:
                    registry.register(YaraProvider())
            except Exception as e:
                logger.warning(f"YaraProvider not available: {e}")
            
            # Register threat intel provider
            try:
                registry.register(ThreatIntelProvider())
            except Exception as e:
                logger.warning(f"ThreatIntelProvider not available: {e}")
            
            # Create router with ensemble strategy
            routing_config = RoutingConfig(strategy=RoutingStrategy.ENSEMBLE)
            router = SmartRouter(registry, routing_config)
            
            # Create Sentinel agent
            self._sentinel_agent = SentinelAgent(
                registry=registry,
                router=router,
                config=routing_config,
            )
            
            self._initialized = True
            logger.info(
                f"SentinelFileAgent initialized with {len(registry.providers)} providers"
            )
            return True
            
        except ImportError as e:
            self._init_error = f"Sentinel import failed: {e}"
            logger.error(self._init_error)
            return False
        except Exception as e:
            self._init_error = f"Sentinel initialization failed: {e}"
            logger.error(self._init_error)
            return False
    
    @property
    def agent_id(self) -> str:
        """Unique agent identifier."""
        return "sentinel.file"
    
    def input_modalities(self) -> List[Modality]:
        """Return supported modalities (FILE only)."""
        return [Modality.FILE]
    
    def analyze(self, event: CanonicalEvent) -> List[DetectionCandidate]:
        """
        Analyze a file event using Sentinel.
        
        Args:
            event: Canonical event with modality=FILE
            
        Returns:
            List of DetectionCandidate objects
            
        Raises:
            ValueError: If event modality is not FILE
            RuntimeError: If Sentinel initialization failed
        """
        # Validate modality
        if event.modality != Modality.FILE:
            raise ValueError(
                f"SentinelFileAgent only supports FILE modality, got {event.modality.value}"
            )
        
        # Validate features version
        if event.features_version != "FileFeaturesV1":
            raise ValueError(
                f"SentinelFileAgent requires FileFeaturesV1, got {event.features_version}"
            )
        
        # Lazy initialization
        if not self._lazy_init():
            raise RuntimeError(
                f"Sentinel not available: {self._init_error or 'Unknown error'}"
            )
        
        # Validate file_path in raw_context
        file_path = event.raw_context.get("file_path")
        if not file_path:
            raise ValueError("event.raw_context must contain 'file_path'")
        
        # Verify file exists
        if not Path(file_path).exists():
            raise ValueError(f"File not found: {file_path}")
        
        try:
            # Convert to Sentinel event format
            sentinel_event = _convert_to_sentinel_event(event)
            
            # Run Sentinel analysis
            t0 = time.perf_counter()
            sentinel_candidates = self._sentinel_agent.analyze(sentinel_event)
            latency_ms = (time.perf_counter() - t0) * 1000
            
            # Convert candidates back to CyberMesh format
            candidates = []
            for sc in sentinel_candidates:
                candidate = _convert_sentinel_candidate(sc)
                candidate.metadata['sentinel_latency_ms'] = latency_ms
                candidates.append(candidate)
            
            logger.debug(
                f"SentinelFileAgent analyzed {file_path}: "
                f"{len(candidates)} candidates in {latency_ms:.1f}ms"
            )
            
            return candidates
            
        except Exception as e:
            logger.error(f"SentinelFileAgent analysis failed for {file_path}: {e}")
            raise
    
    def get_metadata(self) -> dict:
        """Get agent metadata."""
        base = super().get_metadata()
        base.update({
            "initialized": self._initialized,
            "init_error": self._init_error,
            "sentinel_path": SENTINEL_PATH,
        })
        
        if self._initialized and self._sentinel_agent:
            sentinel_meta = self._sentinel_agent.get_metadata()
            base.update({
                "provider_count": sentinel_meta.get("provider_count", 0),
                "providers": sentinel_meta.get("providers", []),
            })
        
        return base
    
    @property
    def is_available(self) -> bool:
        """Check if Sentinel is available for use."""
        if not self._lazy_init_attempted:
            return self._lazy_init()
        return self._initialized


def build_file_event(
    file_path: str,
    tenant_id: str,
    features: Optional[Dict[str, Any]] = None,
    source: str = "cybermesh",
) -> CanonicalEvent:
    """
    Build a CanonicalEvent for a file.
    
    Args:
        file_path: Path to the file to analyze
        tenant_id: Tenant identifier
        features: Optional pre-computed features
        source: Event source
        
    Returns:
        CanonicalEvent with modality=FILE
    """
    import uuid
    
    return CanonicalEvent(
        id=str(uuid.uuid4()),
        timestamp=time.time(),
        source=source,
        tenant_id=tenant_id,
        modality=Modality.FILE,
        features_version="FileFeaturesV1",
        features=features or {},
        raw_context={"file_path": file_path},
    )
