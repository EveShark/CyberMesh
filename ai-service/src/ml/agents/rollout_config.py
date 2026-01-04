"""Agent rollout configuration and state management."""

import os
import json
from dataclasses import dataclass, field
from enum import Enum
from pathlib import Path
from typing import Dict, List, Optional, Any

from ...logging import get_logger

logger = get_logger(__name__)


class RolloutState(str, Enum):
    """Agent rollout states."""
    OFF = "off"
    SHADOW = "shadow"
    PROD_LOW_WEIGHT = "prod_low_weight"
    PROD_FULL = "prod_full"


# Weight multipliers for each state
STATE_WEIGHT_FACTORS = {
    RolloutState.OFF: 0.0,
    RolloutState.SHADOW: 0.0,
    RolloutState.PROD_LOW_WEIGHT: 0.25,
    RolloutState.PROD_FULL: 1.0,
}


@dataclass
class AgentConfig:
    """Configuration for a single agent."""
    agent_id: str
    state: RolloutState = RolloutState.SHADOW
    base_weight: float = 0.5
    tenant_overrides: Dict[str, RolloutState] = field(default_factory=dict)
    
    # Metadata for governance
    features_version: Optional[str] = None
    dataset_lineage: Optional[str] = None
    calibration_method: Optional[str] = None
    calibration_version: Optional[str] = None
    target_latency_ms: Optional[float] = None
    max_cost_per_event: Optional[float] = None
    
    def get_state_for_tenant(self, tenant_id: str) -> RolloutState:
        """Get rollout state for a specific tenant."""
        return self.tenant_overrides.get(tenant_id, self.state)
    
    def get_effective_weight(self, tenant_id: Optional[str] = None) -> float:
        """
        Calculate effective weight based on rollout state.
        
        Args:
            tenant_id: Optional tenant for per-tenant overrides
            
        Returns:
            Effective weight (0.0 to base_weight)
        """
        state = self.get_state_for_tenant(tenant_id) if tenant_id else self.state
        factor = STATE_WEIGHT_FACTORS.get(state, 0.0)
        return self.base_weight * factor
    
    def is_active(self, tenant_id: Optional[str] = None) -> bool:
        """Check if agent should be instantiated (not OFF)."""
        state = self.get_state_for_tenant(tenant_id) if tenant_id else self.state
        return state != RolloutState.OFF
    
    def contributes_to_production(self, tenant_id: Optional[str] = None) -> bool:
        """Check if agent contributes to production decisions (has weight > 0)."""
        return self.get_effective_weight(tenant_id) > 0.0
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary."""
        return {
            "agent_id": self.agent_id,
            "state": self.state.value,
            "base_weight": self.base_weight,
            "tenant_overrides": {k: v.value for k, v in self.tenant_overrides.items()},
            "features_version": self.features_version,
            "dataset_lineage": self.dataset_lineage,
            "calibration_method": self.calibration_method,
            "calibration_version": self.calibration_version,
            "target_latency_ms": self.target_latency_ms,
            "max_cost_per_event": self.max_cost_per_event,
        }
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "AgentConfig":
        """Create from dictionary."""
        state = data.get("state", "shadow")
        if isinstance(state, str):
            state = RolloutState(state)
        
        tenant_overrides = {}
        for k, v in data.get("tenant_overrides", {}).items():
            if isinstance(v, str):
                tenant_overrides[k] = RolloutState(v)
            else:
                tenant_overrides[k] = v
        
        return cls(
            agent_id=data["agent_id"],
            state=state,
            base_weight=float(data.get("base_weight", 0.5)),
            tenant_overrides=tenant_overrides,
            features_version=data.get("features_version"),
            dataset_lineage=data.get("dataset_lineage"),
            calibration_method=data.get("calibration_method"),
            calibration_version=data.get("calibration_version"),
            target_latency_ms=data.get("target_latency_ms"),
            max_cost_per_event=data.get("max_cost_per_event"),
        )


@dataclass
class ProfileConfig:
    """Detection profile configuration."""
    name: str
    malicious_ratio: float = 0.4
    description: Optional[str] = None
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "name": self.name,
            "malicious_ratio": self.malicious_ratio,
            "description": self.description,
        }
    
    @classmethod
    def from_dict(cls, name: str, data: Dict[str, Any]) -> "ProfileConfig":
        return cls(
            name=name,
            malicious_ratio=float(data.get("malicious_ratio", 0.4)),
            description=data.get("description"),
        )


@dataclass
class RolloutConfig:
    """Complete rollout configuration for all agents."""
    agents: Dict[str, AgentConfig] = field(default_factory=dict)
    profiles: Dict[str, ProfileConfig] = field(default_factory=dict)
    default_profile: str = "balanced"
    low_weight_factor: float = 0.25
    
    def get_agent(self, agent_id: str) -> Optional[AgentConfig]:
        """Get agent config by ID."""
        return self.agents.get(agent_id)
    
    def get_effective_weights(self, tenant_id: Optional[str] = None) -> Dict[str, float]:
        """
        Get effective weights for all agents.
        
        Args:
            tenant_id: Optional tenant for per-tenant overrides
            
        Returns:
            Dict mapping agent_id to effective weight
        """
        return {
            agent_id: config.get_effective_weight(tenant_id)
            for agent_id, config in self.agents.items()
        }
    
    def get_active_agents(self, tenant_id: Optional[str] = None) -> List[str]:
        """Get list of agent IDs that should be instantiated."""
        return [
            agent_id
            for agent_id, config in self.agents.items()
            if config.is_active(tenant_id)
        ]
    
    def get_production_agents(self, tenant_id: Optional[str] = None) -> List[str]:
        """Get list of agent IDs that contribute to production decisions."""
        return [
            agent_id
            for agent_id, config in self.agents.items()
            if config.contributes_to_production(tenant_id)
        ]
    
    def validate(self) -> List[str]:
        """
        Validate configuration.
        
        Returns:
            List of validation errors (empty if valid)
        """
        errors = []
        
        # Check for at least one agent
        if not self.agents:
            errors.append("No agents configured")
        
        # Check weight ranges
        for agent_id, config in self.agents.items():
            if not (0.0 <= config.base_weight <= 1.0):
                errors.append(f"Agent {agent_id}: base_weight must be in [0, 1]")
        
        # Check profile exists
        if self.default_profile and self.profiles:
            if self.default_profile not in self.profiles:
                errors.append(f"Default profile '{self.default_profile}' not found in profiles")
        
        # Check low_weight_factor
        if not (0.0 < self.low_weight_factor <= 1.0):
            errors.append("low_weight_factor must be in (0, 1]")
        
        return errors
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary."""
        return {
            "agents": {k: v.to_dict() for k, v in self.agents.items()},
            "profiles": {k: v.to_dict() for k, v in self.profiles.items()},
            "default_profile": self.default_profile,
            "low_weight_factor": self.low_weight_factor,
        }
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "RolloutConfig":
        """Create from dictionary."""
        agents = {}
        for agent_id, agent_data in data.get("agents", {}).items():
            agent_data["agent_id"] = agent_id
            agents[agent_id] = AgentConfig.from_dict(agent_data)
        
        profiles = {}
        for name, profile_data in data.get("profiles", {}).items():
            profiles[name] = ProfileConfig.from_dict(name, profile_data)
        
        return cls(
            agents=agents,
            profiles=profiles,
            default_profile=data.get("default_profile", "balanced"),
            low_weight_factor=float(data.get("low_weight_factor", 0.25)),
        )


def load_rollout_config(
    config_path: Optional[str] = None,
    env_prefix: str = "AGENT_",
) -> RolloutConfig:
    """
    Load rollout configuration from file and environment.
    
    Priority (highest to lowest):
    1. Environment variables (AGENT_STATE_*, AGENT_WEIGHT_*)
    2. Config file (JSON/YAML)
    3. Defaults
    
    Args:
        config_path: Path to config file (JSON)
        env_prefix: Prefix for environment variables
        
    Returns:
        RolloutConfig instance
    """
    config = RolloutConfig()
    
    # Load from file if provided
    if config_path:
        path = Path(config_path)
        if path.exists():
            try:
                with open(path) as f:
                    data = json.load(f)
                config = RolloutConfig.from_dict(data)
                logger.info(f"Loaded rollout config from {config_path}")
            except Exception as e:
                logger.warning(f"Failed to load config from {config_path}: {e}")
    
    # Apply environment variable overrides
    config = _apply_env_overrides(config, env_prefix)
    
    # Add default profiles if none defined
    if not config.profiles:
        config.profiles = {
            "balanced": ProfileConfig(name="balanced", malicious_ratio=0.4),
            "security_first": ProfileConfig(name="security_first", malicious_ratio=0.3),
            "low_fp": ProfileConfig(name="low_fp", malicious_ratio=0.6),
        }
    
    return config


def _apply_env_overrides(config: RolloutConfig, prefix: str) -> RolloutConfig:
    """Apply environment variable overrides to config."""
    
    # Scan for AGENT_STATE_* and AGENT_WEIGHT_* variables
    for key, value in os.environ.items():
        if not key.startswith(prefix):
            continue
        
        suffix = key[len(prefix):]
        
        # Handle AGENT_STATE_<agent_id>
        if suffix.startswith("STATE_"):
            agent_id = suffix[6:].lower().replace("_", ".")
            try:
                state = RolloutState(value.lower())
                if agent_id in config.agents:
                    config.agents[agent_id].state = state
                else:
                    config.agents[agent_id] = AgentConfig(
                        agent_id=agent_id,
                        state=state,
                    )
                logger.debug(f"Env override: {agent_id} state = {state.value}")
            except ValueError:
                logger.warning(f"Invalid state value for {key}: {value}")
        
        # Handle AGENT_WEIGHT_<agent_id>
        elif suffix.startswith("WEIGHT_"):
            agent_id = suffix[7:].lower().replace("_", ".")
            try:
                weight = float(value)
                if agent_id in config.agents:
                    config.agents[agent_id].base_weight = weight
                else:
                    config.agents[agent_id] = AgentConfig(
                        agent_id=agent_id,
                        base_weight=weight,
                    )
                logger.debug(f"Env override: {agent_id} weight = {weight}")
            except ValueError:
                logger.warning(f"Invalid weight value for {key}: {value}")
    
    return config


def create_default_config() -> RolloutConfig:
    """Create default rollout configuration for CyberMesh + Sentinel."""
    return RolloutConfig(
        agents={
            "cybermesh.rules": AgentConfig(
                agent_id="cybermesh.rules",
                state=RolloutState.SHADOW,
                base_weight=0.3,
                features_version="NetworkFlowFeaturesV1",
                dataset_lineage="CIC-DDoS2019",
            ),
            "cybermesh.math": AgentConfig(
                agent_id="cybermesh.math",
                state=RolloutState.SHADOW,
                base_weight=0.2,
                features_version="NetworkFlowFeaturesV1",
                dataset_lineage="CIC-DDoS2019",
            ),
            "cybermesh.ml": AgentConfig(
                agent_id="cybermesh.ml",
                state=RolloutState.SHADOW,
                base_weight=0.5,
                features_version="NetworkFlowFeaturesV1",
                dataset_lineage="CIC-DDoS2019",
                calibration_method="isotonic",
            ),
            "cybermesh.ml.v2": AgentConfig(
                agent_id="cybermesh.ml.v2",
                state=RolloutState.SHADOW,
                base_weight=0.5,
                features_version="NetworkFlowFeaturesV1",
                dataset_lineage="CIC-DDoS2019",
                calibration_method="isotonic",
            ),
            "cybermesh.dns": AgentConfig(
                agent_id="cybermesh.dns",
                state=RolloutState.SHADOW,
                base_weight=0.4,
                features_version="DNSFeaturesV1",
                dataset_lineage="internal",
            ),
            "sentinel.file": AgentConfig(
                agent_id="sentinel.file",
                state=RolloutState.SHADOW,
                base_weight=0.4,
                features_version="FileFeaturesV1",
                dataset_lineage="EMBER2024",
            ),
        },
        profiles={
            "balanced": ProfileConfig(name="balanced", malicious_ratio=0.4),
            "security_first": ProfileConfig(name="security_first", malicious_ratio=0.3),
            "low_fp": ProfileConfig(name="low_fp", malicious_ratio=0.6),
        },
        default_profile="balanced",
        low_weight_factor=0.25,
    )
