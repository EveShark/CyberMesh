"""Telemetry source configuration and registry."""

import json
import os
from dataclasses import dataclass, field
from enum import Enum
from pathlib import Path
from typing import Dict, List, Optional, Any, Type

from .contracts import Modality
from ...logging import get_logger

logger = get_logger(__name__)


class SourceMode(str, Enum):
    """Pipeline routing mode for a telemetry source."""
    LEGACY = "legacy"      # Route to legacy DetectionPipeline only
    AGENT = "agent"        # Route to DetectionAgentPipeline only
    BOTH = "both"          # Route to both pipelines


class DetectionMode(str, Enum):
    """Global detection mode controlling pipeline behavior."""
    LEGACY = "legacy"              # Only legacy pipeline publishes
    AGENT_SHADOW = "agent_shadow"  # Both run, only legacy publishes
    AGENT_PRIMARY = "agent_primary"  # Agent pipeline publishes (legacy as shadow)


class SourceType(str, Enum):
    """Type of telemetry source."""
    POSTGRES = "postgres"
    KAFKA = "kafka"
    FILE = "file"


@dataclass
class TelemetrySourceConfig:
    """Configuration for a single telemetry source."""
    source_id: str
    source_type: SourceType
    adapter_class: str
    mode: SourceMode
    modality: Modality
    
    # Optional fields
    tenant_id: Optional[str] = None
    connection_params: Dict[str, Any] = field(default_factory=dict)
    enabled: bool = True
    description: Optional[str] = None
    
    def should_use_legacy(self, global_mode: DetectionMode) -> bool:
        """
        Determine if this source should feed the legacy pipeline.
        
        Args:
            global_mode: Global detection mode
            
        Returns:
            True if legacy pipeline should process this source
        """
        if global_mode == DetectionMode.LEGACY:
            return True
        
        if global_mode == DetectionMode.AGENT_SHADOW:
            return self.mode in (SourceMode.LEGACY, SourceMode.BOTH)
        
        if global_mode == DetectionMode.AGENT_PRIMARY:
            return self.mode in (SourceMode.LEGACY, SourceMode.BOTH)
        
        return False
    
    def should_use_agent(self, global_mode: DetectionMode) -> bool:
        """
        Determine if this source should feed the agent pipeline.
        
        Args:
            global_mode: Global detection mode
            
        Returns:
            True if agent pipeline should process this source
        """
        if global_mode == DetectionMode.LEGACY:
            return False
        
        if global_mode == DetectionMode.AGENT_SHADOW:
            return self.mode in (SourceMode.AGENT, SourceMode.BOTH)
        
        if global_mode == DetectionMode.AGENT_PRIMARY:
            return self.mode in (SourceMode.AGENT, SourceMode.BOTH)
        
        return False
    
    def should_agent_publish(self, global_mode: DetectionMode) -> bool:
        """
        Determine if agent pipeline should publish for this source.
        
        Args:
            global_mode: Global detection mode
            
        Returns:
            True if agent pipeline decisions should be published
        """
        if global_mode != DetectionMode.AGENT_PRIMARY:
            return False
        
        return self.mode in (SourceMode.AGENT, SourceMode.BOTH)
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "source_id": self.source_id,
            "source_type": self.source_type.value,
            "adapter_class": self.adapter_class,
            "mode": self.mode.value,
            "modality": self.modality.value,
            "tenant_id": self.tenant_id,
            "connection_params": self.connection_params,
            "enabled": self.enabled,
            "description": self.description,
        }
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "TelemetrySourceConfig":
        source_type = data.get("source_type", "file")
        if isinstance(source_type, str):
            source_type = SourceType(source_type)
        
        mode = data.get("mode", "legacy")
        if isinstance(mode, str):
            mode = SourceMode(mode)
        
        modality = data.get("modality", "network_flow")
        if isinstance(modality, str):
            modality = Modality(modality)
        
        return cls(
            source_id=data["source_id"],
            source_type=source_type,
            adapter_class=data.get("adapter_class", ""),
            mode=mode,
            modality=modality,
            tenant_id=data.get("tenant_id"),
            connection_params=data.get("connection_params", {}),
            enabled=data.get("enabled", True),
            description=data.get("description"),
        )


@dataclass
class TelemetryRegistry:
    """Registry of all telemetry sources."""
    sources: Dict[str, TelemetrySourceConfig] = field(default_factory=dict)
    global_mode: DetectionMode = DetectionMode.LEGACY
    
    def add_source(self, config: TelemetrySourceConfig) -> None:
        """Add a telemetry source configuration."""
        self.sources[config.source_id] = config
        logger.debug(f"Registered telemetry source: {config.source_id}")
    
    def get_source(self, source_id: str) -> Optional[TelemetrySourceConfig]:
        """Get source config by ID."""
        return self.sources.get(source_id)
    
    def get_enabled_sources(self) -> List[TelemetrySourceConfig]:
        """Get all enabled sources."""
        return [s for s in self.sources.values() if s.enabled]
    
    def get_sources_for_mode(self, mode: SourceMode) -> List[TelemetrySourceConfig]:
        """Get sources with a specific routing mode."""
        return [
            s for s in self.sources.values()
            if s.enabled and s.mode == mode
        ]
    
    def get_sources_for_modality(self, modality: Modality) -> List[TelemetrySourceConfig]:
        """Get sources for a specific modality."""
        return [
            s for s in self.sources.values()
            if s.enabled and s.modality == modality
        ]
    
    def get_legacy_sources(self) -> List[TelemetrySourceConfig]:
        """Get sources that should feed the legacy pipeline."""
        return [
            s for s in self.sources.values()
            if s.enabled and s.should_use_legacy(self.global_mode)
        ]
    
    def get_agent_sources(self) -> List[TelemetrySourceConfig]:
        """Get sources that should feed the agent pipeline."""
        return [
            s for s in self.sources.values()
            if s.enabled and s.should_use_agent(self.global_mode)
        ]
    
    def get_agent_publish_sources(self) -> List[TelemetrySourceConfig]:
        """Get sources where agent pipeline should publish."""
        return [
            s for s in self.sources.values()
            if s.enabled and s.should_agent_publish(self.global_mode)
        ]
    
    def validate(self) -> List[str]:
        """
        Validate registry configuration.
        
        Returns:
            List of validation errors (empty if valid)
        """
        errors = []
        
        if not self.sources:
            errors.append("No telemetry sources configured")
        
        # Check for duplicate source IDs (shouldn't happen with dict, but verify data)
        source_ids = list(self.sources.keys())
        if len(source_ids) != len(set(source_ids)):
            errors.append("Duplicate source IDs detected")
        
        # Check adapter class is specified for agent/both sources
        for source_id, config in self.sources.items():
            if config.mode in (SourceMode.AGENT, SourceMode.BOTH):
                if not config.adapter_class:
                    errors.append(f"Source {source_id}: adapter_class required for mode={config.mode.value}")
        
        return errors
    
    def get_routing_summary(self) -> Dict[str, Any]:
        """Get summary of source routing for logging/debugging."""
        return {
            "global_mode": self.global_mode.value,
            "total_sources": len(self.sources),
            "enabled_sources": len(self.get_enabled_sources()),
            "legacy_sources": len(self.get_legacy_sources()),
            "agent_sources": len(self.get_agent_sources()),
            "agent_publish_sources": len(self.get_agent_publish_sources()),
            "by_modality": {
                m.value: len(self.get_sources_for_modality(m))
                for m in Modality
            },
            "by_mode": {
                m.value: len(self.get_sources_for_mode(m))
                for m in SourceMode
            },
        }
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "global_mode": self.global_mode.value,
            "sources": {k: v.to_dict() for k, v in self.sources.items()},
        }
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "TelemetryRegistry":
        global_mode = data.get("global_mode", "legacy")
        if isinstance(global_mode, str):
            global_mode = DetectionMode(global_mode)
        
        sources = {}
        for source_id, source_data in data.get("sources", {}).items():
            source_data["source_id"] = source_id
            sources[source_id] = TelemetrySourceConfig.from_dict(source_data)
        
        return cls(sources=sources, global_mode=global_mode)


def load_telemetry_registry(
    config_path: Optional[str] = None,
    env_prefix: str = "TELEMETRY_",
) -> TelemetryRegistry:
    """
    Load telemetry registry from file and environment.
    
    Args:
        config_path: Path to JSON config file
        env_prefix: Prefix for environment variables
        
    Returns:
        TelemetryRegistry instance
    """
    registry = TelemetryRegistry()
    
    # Load global mode from environment
    global_mode_str = os.getenv(f"{env_prefix}DETECTION_MODE", "legacy")
    try:
        registry.global_mode = DetectionMode(global_mode_str.lower())
    except ValueError:
        logger.warning(f"Invalid DETECTION_MODE: {global_mode_str}, using 'legacy'")
        registry.global_mode = DetectionMode.LEGACY
    
    # Load from file if provided
    if config_path:
        path = Path(config_path)
        if path.exists():
            try:
                with open(path) as f:
                    data = json.load(f)
                registry = TelemetryRegistry.from_dict(data)
                logger.info(f"Loaded telemetry registry from {config_path}")
            except Exception as e:
                logger.warning(f"Failed to load telemetry config from {config_path}: {e}")
    
    return registry


def create_default_registry() -> TelemetryRegistry:
    """Create default telemetry registry with CIC-DDoS2019 source."""
    return TelemetryRegistry(
        global_mode=DetectionMode.LEGACY,
        sources={
            "postgres_cic": TelemetrySourceConfig(
                source_id="postgres_cic",
                source_type=SourceType.POSTGRES,
                adapter_class="CICDDoS2019Adapter",
                mode=SourceMode.LEGACY,
                modality=Modality.NETWORK_FLOW,
                description="CIC-DDoS2019 dataset from PostgreSQL",
            ),
        },
    )
