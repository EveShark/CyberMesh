"""Calibration sync module for mapping Sentinel providers to CyberMesh agent IDs."""

import json
import logging
from pathlib import Path
from typing import Dict, Any, Optional

logger = logging.getLogger(__name__)

PROVIDER_TO_AGENT_ID = {
    "entropy": "sentinel.entropy",
    "strings": "sentinel.strings",
    "malware_pe_ml": "sentinel.malware_pe_ml",
    "yara": "sentinel.yara",
    "threat_intel": "sentinel.threat_intel",
    "static_analysis": "sentinel.static_analysis",
    "pe_analyzer": "sentinel.pe_analyzer",
    "sandbox": "sentinel.sandbox",
}

AGENT_ID_TO_PROVIDER = {v: k for k, v in PROVIDER_TO_AGENT_ID.items()}


def get_agent_id(provider_name: str) -> Optional[str]:
    """Get CyberMesh agent_id for a Sentinel provider."""
    return PROVIDER_TO_AGENT_ID.get(provider_name)


def get_provider_name(agent_id: str) -> Optional[str]:
    """Get Sentinel provider name for a CyberMesh agent_id."""
    return AGENT_ID_TO_PROVIDER.get(agent_id)


def load_sentinel_agent_calibration(config_path: str) -> Dict[str, Dict[str, Any]]:
    """
    Load calibrated_config.json and map to agent_id structure.
    
    Args:
        config_path: Path to calibrated_config.json
        
    Returns:
        Dictionary mapping agent_id to calibration config:
        {
            "sentinel.entropy": {"weight": 1.0, "threshold": 0.5, "enabled": True},
            ...
        }
    """
    path = Path(config_path)
    if not path.exists():
        raise FileNotFoundError(f"Calibration config not found: {config_path}")
    
    with open(path, 'r') as f:
        cfg = json.load(f)
    
    agents = {}
    providers = cfg.get("providers", {})
    
    for provider_name, p_cfg in providers.items():
        agent_id = PROVIDER_TO_AGENT_ID.get(provider_name)
        if not agent_id:
            logger.debug(f"No agent_id mapping for provider: {provider_name}")
            continue
        
        weight = p_cfg.get("weight", 1.0)
        if weight < 0:
            weight = 0.0
        
        agents[agent_id] = {
            "weight": weight,
            "threshold": p_cfg.get("threshold", 0.5),
            "enabled": p_cfg.get("enabled", True),
            "calibration_method": p_cfg.get("calibration_method"),
            "calibration_date": p_cfg.get("calibration_date"),
        }
    
    return agents


def load_profile_thresholds(config_path: str) -> Dict[str, Dict[str, float]]:
    """
    Load profile-specific thresholds from calibrated_config.json.
    
    Args:
        config_path: Path to calibrated_config.json
        
    Returns:
        Dictionary mapping profile name to thresholds:
        {
            "balanced": {"publish_threshold": 0.6, "high_confidence_threshold": 0.8},
            "security_first": {"publish_threshold": 0.4, ...},
            ...
        }
    """
    path = Path(config_path)
    if not path.exists():
        raise FileNotFoundError(f"Calibration config not found: {config_path}")
    
    with open(path, 'r') as f:
        cfg = json.load(f)
    
    profiles = cfg.get("profiles", {})
    result = {}
    
    for profile_name, p_cfg in profiles.items():
        result[profile_name] = {
            "publish_threshold": p_cfg.get("publish_threshold", 0.5),
            "high_confidence_threshold": p_cfg.get("high_confidence_threshold", 0.8),
            "abstain_threshold": p_cfg.get("abstain_threshold", 0.3),
        }
    
    return result


def sync_to_rollout_config(
    sentinel_calibration: Dict[str, Dict[str, Any]],
    rollout_config: Any,
) -> Any:
    """
    Update RolloutConfig with Sentinel agent weights.
    
    Args:
        sentinel_calibration: Output from load_sentinel_agent_calibration()
        rollout_config: CyberMesh RolloutConfig instance
        
    Returns:
        Updated RolloutConfig
    """
    for agent_id, cal in sentinel_calibration.items():
        if hasattr(rollout_config, 'agents') and agent_id in rollout_config.agents:
            agent_cfg = rollout_config.agents[agent_id]
            if hasattr(agent_cfg, 'base_weight'):
                agent_cfg.base_weight = cal["weight"]
            if hasattr(agent_cfg, 'enabled'):
                agent_cfg.enabled = cal["enabled"]
        elif hasattr(rollout_config, 'add_agent'):
            rollout_config.add_agent(
                agent_id=agent_id,
                base_weight=cal["weight"],
                enabled=cal["enabled"],
            )
    
    return rollout_config


def validate_calibration_consistency(
    sentinel_config_path: str,
    cybermesh_rollout_config: Any,
) -> Dict[str, Any]:
    """
    Validate that Sentinel and CyberMesh calibrations are consistent.
    
    Args:
        sentinel_config_path: Path to Sentinel's calibrated_config.json
        cybermesh_rollout_config: CyberMesh RolloutConfig instance
        
    Returns:
        Validation report with any inconsistencies found
    """
    sentinel_cal = load_sentinel_agent_calibration(sentinel_config_path)
    
    report = {
        "consistent": True,
        "sentinel_agents": list(sentinel_cal.keys()),
        "cybermesh_agents": [],
        "weight_mismatches": [],
        "missing_in_cybermesh": [],
        "missing_in_sentinel": [],
    }
    
    if hasattr(cybermesh_rollout_config, 'agents'):
        report["cybermesh_agents"] = list(cybermesh_rollout_config.agents.keys())
        
        for agent_id, s_cal in sentinel_cal.items():
            if agent_id not in cybermesh_rollout_config.agents:
                report["missing_in_cybermesh"].append(agent_id)
                report["consistent"] = False
            else:
                cm_agent = cybermesh_rollout_config.agents[agent_id]
                cm_weight = getattr(cm_agent, 'base_weight', None)
                if cm_weight is not None and abs(cm_weight - s_cal["weight"]) > 0.01:
                    report["weight_mismatches"].append({
                        "agent_id": agent_id,
                        "sentinel_weight": s_cal["weight"],
                        "cybermesh_weight": cm_weight,
                    })
                    report["consistent"] = False
        
        for agent_id in cybermesh_rollout_config.agents:
            if agent_id.startswith("sentinel.") and agent_id not in sentinel_cal:
                report["missing_in_sentinel"].append(agent_id)
    
    return report
