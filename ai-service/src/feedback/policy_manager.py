"""
PolicyManager - Dynamic policy update handler

Applies policy updates from backend validators to runtime configuration.
Supports rollback, audit logging, and acknowledgements.
"""

import json
import time
import logging
from typing import Dict, Any, Optional, List
from dataclasses import dataclass
from ..contracts.policy_update import PolicyUpdateEvent
from .storage import RedisStorage


@dataclass
class PolicyRecord:
    """Record of applied policy."""
    policy_id: str
    rule_type: str
    rule_data: Dict[str, Any]
    applied_at: float
    effective_height: int
    expiration_height: int
    producer_id: bytes
    previous_policy_id: Optional[str]
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "policy_id": self.policy_id,
            "rule_type": self.rule_type,
            "rule_data": self.rule_data,
            "applied_at": self.applied_at,
            "effective_height": self.effective_height,
            "expiration_height": self.expiration_height,
            "producer_id": self.producer_id.hex(),
            "previous_policy_id": self.previous_policy_id
        }
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "PolicyRecord":
        return cls(
            policy_id=data["policy_id"],
            rule_type=data["rule_type"],
            rule_data=data["rule_data"],
            applied_at=data["applied_at"],
            effective_height=data["effective_height"],
            expiration_height=data["expiration_height"],
            producer_id=bytes.fromhex(data["producer_id"]),
            previous_policy_id=data.get("previous_policy_id")
        )


class PolicyManager:
    """
    Manages dynamic policy updates from backend.
    
    Supported rule types:
    - threshold: Update detection thresholds
    - blacklist: Update IP/domain blacklists
    - whitelist: Update IP/domain whitelists
    - feature_flag: Enable/disable features
    - rate_limit: Update rate limits
    - calibration: Update calibration config
    """
    
    REDIS_PREFIX = "policy"
    REDIS_ACTIVE = f"{REDIS_PREFIX}:active"
    REDIS_HISTORY = f"{REDIS_PREFIX}:history"
    
    def __init__(self, storage: RedisStorage):
        """
        Initialize PolicyManager.
        
        Args:
            storage: RedisStorage instance
        """
        self.storage = storage
        self.logger = logging.getLogger(__name__)
        
        # Runtime config overrides (applied to detection pipeline)
        self.overrides: Dict[str, Any] = {}
        
        # Load active policies from Redis
        self._load_active_policies()
    
    def _load_active_policies(self):
        """Load active policies from Redis on startup."""
        try:
            active = self.storage.get(self.REDIS_ACTIVE)
            if active:
                self.overrides = active
                self.logger.info(f"Loaded {len(self.overrides)} active policy overrides")
        except Exception as e:
            self.logger.error(f"Failed to load active policies: {e}")
            self.overrides = {}
    
    def handle_policy_update(self, event: PolicyUpdateEvent, current_height: int) -> bool:
        """
        Handle policy update event.
        
        Args:
            event: Parsed PolicyUpdateEvent
            current_height: Current blockchain height
            
        Returns:
            True if policy applied successfully
        """
        try:
            # Validate timing
            if current_height < event.effective_height:
                self.logger.warning(
                    f"Policy {event.policy_id} not yet effective "
                    f"(current={current_height}, effective={event.effective_height})"
                )
                return False
            
            if event.expiration_height > 0 and current_height >= event.expiration_height:
                self.logger.warning(
                    f"Policy {event.policy_id} expired "
                    f"(current={current_height}, expiration={event.expiration_height})"
                )
                return False
            
            # Handle action
            if event.action == "apply":
                return self._apply_policy(event, current_height)
            elif event.action == "revert":
                return self._revert_policy(event)
            else:
                self.logger.error(f"Unknown policy action: {event.action}")
                return False
                
        except Exception as e:
            self.logger.error(f"Failed to handle policy update: {e}", exc_info=True)
            return False
    
    def _apply_policy(self, event: PolicyUpdateEvent, current_height: int) -> bool:
        """Apply policy based on rule type."""
        # Get current policy for this rule type (for rollback)
        previous_policy_id = None
        for pid, record_dict in self._get_active_policies().items():
            if record_dict["rule_type"] == event.rule_type:
                previous_policy_id = pid
                break
        
        # Validate and apply rule
        if event.rule_type == "threshold":
            success = self._apply_threshold_policy(event.rule_data)
        elif event.rule_type == "blacklist":
            success = self._apply_blacklist_policy(event.rule_data)
        elif event.rule_type == "whitelist":
            success = self._apply_whitelist_policy(event.rule_data)
        elif event.rule_type == "feature_flag":
            success = self._apply_feature_flag_policy(event.rule_data)
        elif event.rule_type == "rate_limit":
            success = self._apply_rate_limit_policy(event.rule_data)
        elif event.rule_type == "calibration":
            success = self._apply_calibration_policy(event.rule_data)
        else:
            self.logger.error(f"Unknown rule type: {event.rule_type}")
            return False
        
        if not success:
            return False
        
        # Record policy
        record = PolicyRecord(
            policy_id=event.policy_id,
            rule_type=event.rule_type,
            rule_data=event.rule_data,
            applied_at=time.time(),
            effective_height=event.effective_height,
            expiration_height=event.expiration_height,
            producer_id=event.producer_id,
            previous_policy_id=previous_policy_id
        )
        
        # Save to Redis
        self._save_policy_record(record)
        
        self.logger.info(
            f"Applied policy {event.policy_id} (type={event.rule_type}, "
            f"height={current_height})"
        )
        
        return True
    
    def _revert_policy(self, event: PolicyUpdateEvent) -> bool:
        """Revert to rollback policy."""
        if not event.rollback_policy_id:
            self.logger.error(f"No rollback_policy_id specified for revert")
            return False
        
        # Get rollback policy
        record = self._get_policy_record(event.rollback_policy_id)
        if not record:
            self.logger.error(f"Rollback policy not found: {event.rollback_policy_id}")
            return False
        
        # Reapply rollback policy
        if record.rule_type == "threshold":
            success = self._apply_threshold_policy(record.rule_data)
        elif record.rule_type == "blacklist":
            success = self._apply_blacklist_policy(record.rule_data)
        elif record.rule_type == "whitelist":
            success = self._apply_whitelist_policy(record.rule_data)
        elif record.rule_type == "feature_flag":
            success = self._apply_feature_flag_policy(record.rule_data)
        elif record.rule_type == "rate_limit":
            success = self._apply_rate_limit_policy(record.rule_data)
        elif record.rule_type == "calibration":
            success = self._apply_calibration_policy(record.rule_data)
        else:
            self.logger.error(f"Unknown rule type: {record.rule_type}")
            return False
        
        if success:
            self.logger.info(f"Reverted to policy {event.rollback_policy_id}")
        
        return success
    
    def _apply_threshold_policy(self, rule_data: Dict[str, Any]) -> bool:
        """Apply threshold policy."""
        try:
            # Validate thresholds
            for key, value in rule_data.items():
                if not isinstance(value, (int, float)):
                    self.logger.error(f"Invalid threshold value: {key}={value}")
                    return False
                if not (0.0 <= value <= 1.0):
                    self.logger.error(f"Threshold out of range: {key}={value}")
                    return False
            
            # Apply overrides
            for key, value in rule_data.items():
                self.overrides[key] = value
            
            self._persist_overrides()
            return True
            
        except Exception as e:
            self.logger.error(f"Failed to apply threshold policy: {e}")
            return False
    
    def _apply_blacklist_policy(self, rule_data: Dict[str, Any]) -> bool:
        """Apply blacklist policy."""
        try:
            # Validate structure
            if "ips" not in rule_data and "domains" not in rule_data:
                self.logger.error("Blacklist must contain 'ips' or 'domains'")
                return False
            
            # Apply overrides
            if "ips" in rule_data:
                self.overrides["blacklist_ips"] = rule_data["ips"]
            if "domains" in rule_data:
                self.overrides["blacklist_domains"] = rule_data["domains"]
            
            self._persist_overrides()
            return True
            
        except Exception as e:
            self.logger.error(f"Failed to apply blacklist policy: {e}")
            return False
    
    def _apply_whitelist_policy(self, rule_data: Dict[str, Any]) -> bool:
        """Apply whitelist policy."""
        try:
            # Validate structure
            if "ips" not in rule_data and "domains" not in rule_data:
                self.logger.error("Whitelist must contain 'ips' or 'domains'")
                return False
            
            # Apply overrides
            if "ips" in rule_data:
                self.overrides["whitelist_ips"] = rule_data["ips"]
            if "domains" in rule_data:
                self.overrides["whitelist_domains"] = rule_data["domains"]
            
            self._persist_overrides()
            return True
            
        except Exception as e:
            self.logger.error(f"Failed to apply whitelist policy: {e}")
            return False
    
    def _apply_feature_flag_policy(self, rule_data: Dict[str, Any]) -> bool:
        """Apply feature flag policy."""
        try:
            # Validate flags
            for key, value in rule_data.items():
                if not isinstance(value, bool):
                    self.logger.error(f"Invalid feature flag: {key}={value}")
                    return False
            
            # Apply overrides
            for key, value in rule_data.items():
                self.overrides[key] = value
            
            self._persist_overrides()
            return True
            
        except Exception as e:
            self.logger.error(f"Failed to apply feature flag policy: {e}")
            return False
    
    def _apply_rate_limit_policy(self, rule_data: Dict[str, Any]) -> bool:
        """Apply rate limit policy."""
        try:
            # Validate rate limits
            for key, value in rule_data.items():
                if not isinstance(value, (int, float)):
                    self.logger.error(f"Invalid rate limit: {key}={value}")
                    return False
                if value <= 0:
                    self.logger.error(f"Rate limit must be positive: {key}={value}")
                    return False
            
            # Apply overrides
            for key, value in rule_data.items():
                self.overrides[key] = value
            
            self._persist_overrides()
            return True
            
        except Exception as e:
            self.logger.error(f"Failed to apply rate limit policy: {e}")
            return False
    
    def _apply_calibration_policy(self, rule_data: Dict[str, Any]) -> bool:
        """Apply calibration policy."""
        try:
            # Validate calibration config
            if "calibration_method" in rule_data:
                if rule_data["calibration_method"] not in ["isotonic", "platt"]:
                    self.logger.error(f"Invalid calibration method: {rule_data['calibration_method']}")
                    return False
            
            if "calibration_min_samples" in rule_data:
                if rule_data["calibration_min_samples"] <= 0:
                    self.logger.error("calibration_min_samples must be positive")
                    return False
            
            if "calibration_acceptance_threshold" in rule_data:
                val = rule_data["calibration_acceptance_threshold"]
                if not (0.0 < val < 1.0):
                    self.logger.error(f"calibration_acceptance_threshold out of range: {val}")
                    return False
            
            # Apply overrides
            for key, value in rule_data.items():
                self.overrides[key] = value
            
            self._persist_overrides()
            return True
            
        except Exception as e:
            self.logger.error(f"Failed to apply calibration policy: {e}")
            return False
    
    def _persist_overrides(self):
        """Persist overrides to Redis."""
        try:
            self.storage.set(self.REDIS_ACTIVE, self.overrides)
        except Exception as e:
            self.logger.error(f"Failed to persist overrides: {e}")
    
    def _save_policy_record(self, record: PolicyRecord):
        """Save policy record to history."""
        try:
            # Save to history
            key = f"{self.REDIS_HISTORY}:{record.policy_id}"
            self.storage.set(key, record.to_dict())
            
            # Update active policy index
            active_policies = self._get_active_policies()
            
            # Remove previous policy of same type
            if record.previous_policy_id:
                active_policies.pop(record.previous_policy_id, None)
            
            # Add new policy
            active_policies[record.policy_id] = record.to_dict()
            
            # Save active index
            self.storage.set(f"{self.REDIS_PREFIX}:active:index", active_policies)
            
        except Exception as e:
            self.logger.error(f"Failed to save policy record: {e}")
    
    def _get_policy_record(self, policy_id: str) -> Optional[PolicyRecord]:
        """Get policy record from history."""
        try:
            key = f"{self.REDIS_HISTORY}:{policy_id}"
            data = self.storage.get(key)
            if data:
                return PolicyRecord.from_dict(data)
            return None
        except Exception as e:
            self.logger.error(f"Failed to get policy record: {e}")
            return None
    
    def _get_active_policies(self) -> Dict[str, Dict[str, Any]]:
        """Get active policies index."""
        try:
            data = self.storage.get(f"{self.REDIS_PREFIX}:active:index")
            return data if data else {}
        except Exception as e:
            self.logger.error(f"Failed to get active policies: {e}")
            return {}
    
    def get_override(self, key: str, default: Any = None) -> Any:
        """Get policy override value."""
        return self.overrides.get(key, default)
    
    def has_override(self, key: str) -> bool:
        """Check if policy override exists."""
        return key in self.overrides
    
    def get_all_overrides(self) -> Dict[str, Any]:
        """Get all policy overrides."""
        return self.overrides.copy()
    
    def clear_overrides(self):
        """Clear all policy overrides (for testing)."""
        self.overrides = {}
        self._persist_overrides()
        self.logger.info("Cleared all policy overrides")
    
    def get_stats(self) -> Dict[str, Any]:
        """Get policy manager statistics."""
        active_policies = self._get_active_policies()
        
        return {
            "total_overrides": len(self.overrides),
            "active_policies": len(active_policies),
            "override_keys": list(self.overrides.keys()),
            "policy_types": list(set(
                p["rule_type"] for p in active_policies.values()
            ))
        }
