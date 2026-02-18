"""
SentinelAgent - Wraps Sentinel providers as a DetectionAgent.

Provides a unified interface for file-based threat detection using
the existing provider infrastructure.
"""

from typing import List, Optional

from .detection_agent import DetectionAgent
from ..contracts import (
    DetectionCandidate,
    CanonicalEvent,
    Modality,
    FileFeaturesV1,
)
from ..contracts.mapping import map_analysis_result_to_candidate
from ..providers.registry import ProviderRegistry
from ..router.smart_router import SmartRouter, RoutingConfig, RoutingStrategy
from ..parsers import get_parser_for_file
from ..parsers.base import ParsedFile, FileType
from ..logging import get_logger

logger = get_logger(__name__)


class SentinelAgent(DetectionAgent):
    """
    DetectionAgent implementation that wraps Sentinel's existing providers.
    
    This agent:
    1. Accepts FILE modality events with FileFeaturesV1 schema
    2. Routes to appropriate providers using SmartRouter
    3. Converts AnalysisResults to DetectionCandidates
    
    Example usage:
        registry = ProviderRegistry()
        registry.register(EntropyProvider())
        registry.register(StringsProvider())
        
        agent = SentinelAgent(registry)
        event = build_file_event(features, raw_context, tenant_id, source)
        candidates = agent.analyze(event)
    """
    
    def __init__(
        self,
        registry: ProviderRegistry,
        router: Optional[SmartRouter] = None,
        config: Optional[RoutingConfig] = None,
    ):
        """
        Initialize SentinelAgent.
        
        Args:
            registry: Provider registry with registered providers
            router: Optional pre-configured router (created if not provided)
            config: Optional routing config (defaults to ENSEMBLE strategy)
        """
        self._registry = registry
        self._config = config or RoutingConfig(strategy=RoutingStrategy.ENSEMBLE)
        self._router = router or SmartRouter(registry, self._config)
    
    @property
    def agent_id(self) -> str:
        """Unique agent identifier."""
        return "sentinel.file"
    
    def input_modalities(self) -> List[Modality]:
        """Return supported modalities (FILE only)."""
        return [Modality.FILE]
    
    def analyze(self, event: CanonicalEvent) -> List[DetectionCandidate]:
        """
        Analyze a file event using Sentinel providers.
        
        Args:
            event: Canonical event with modality=FILE and features_version=FileFeaturesV1
            
        Returns:
            List of DetectionCandidate objects from all providers
            
        Raises:
            ValueError: If event modality is not FILE or features_version is wrong
        """
        # Validate event
        if event.modality != Modality.FILE:
            raise ValueError(
                f"SentinelAgent only supports FILE modality, got {event.modality.value}"
            )
        
        if event.features_version != "FileFeaturesV1":
            raise ValueError(
                f"SentinelAgent requires FileFeaturesV1, got {event.features_version}"
            )
        
        # Get file path from raw_context
        file_path = event.raw_context.get("file_path")
        if not file_path:
            raise ValueError("event.raw_context must contain 'file_path'")
        
        # Parse file using existing parsers
        parser = get_parser_for_file(file_path)
        if parser is None:
            logger.warning(f"No parser available for file: {file_path}")
            return []
        try:
            parsed_file = parser.parse(file_path)
        except Exception as exc:  # pylint: disable=broad-except
            logger.error(f"Parser failed for {file_path}: {exc}")
            return []
        
        # Route to providers
        provider_names = self._router.route(parsed_file.file_type)
        if not provider_names:
            logger.warning(f"No providers available for file type: {parsed_file.file_type.value}")
            return []
        
        # Build context for threat type mapping
        context = self._build_mapping_context(event.features)
        
        # Run each provider and collect candidates
        candidates: List[DetectionCandidate] = []
        
        for name in provider_names:
            provider = self._registry.get_provider(name)
            if provider is None:
                continue
            
            if not provider.can_analyze(parsed_file):
                continue
            
            try:
                result = provider.analyze(parsed_file)
                
                # Update registry latency stats
                self._registry.update_latency(name, result.latency_ms)
                
                # Convert to DetectionCandidate
                candidate = map_analysis_result_to_candidate(
                    result,
                    agent_prefix="sentinel",
                    context=context,
                )
                
                if candidate is not None:
                    candidates.append(candidate)
                    logger.debug(
                        f"Provider {name} produced candidate: "
                        f"{candidate.threat_type.value} @ {candidate.calibrated_score:.2f}"
                    )
                else:
                    logger.debug(f"Provider {name} returned CLEAN/UNKNOWN, no candidate")
                    
            except Exception as e:
                logger.error(f"Provider {name} failed: {e}")
                continue
        
        logger.info(
            f"SentinelAgent analyzed {file_path}: "
            f"{len(candidates)} candidates from {len(provider_names)} providers"
        )
        
        return candidates
    
    def _build_mapping_context(self, features: dict) -> dict:
        """
        Build context dict for threat type mapping from file features.
        
        Extracts threat intel hints from FileFeaturesV1 to inform
        more specific threat type mapping (e.g., C2, botnet).
        """
        return {
            "is_c2": features.get("ti_is_c2", False),
            "is_botnet": features.get("ti_is_botnet", False),
            "is_network_flow": False,  # This is a file agent
        }
    
    def get_metadata(self) -> dict:
        """Get agent metadata including provider info."""
        base = super().get_metadata()
        base.update({
            "provider_count": len(self._registry.providers),
            "providers": list(self._registry.providers.keys()),
            "routing_strategy": self._config.strategy.value,
        })
        return base
