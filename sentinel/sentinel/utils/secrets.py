"""
Secure secrets management for Sentinel.

Security principles:
- Load keys from environment only (never hardcode)
- Validate keys on startup (fail fast)
- Redact keys in all logs
- Support key rotation
"""

import os
import re
from dataclasses import dataclass
from typing import Dict, Optional, List
from functools import lru_cache

import logging

logger = logging.getLogger(__name__)


# Key patterns for validation
KEY_PATTERNS = {
    "virustotal": r"^[a-f0-9]{64}$",
    "groq": r"^gsk_[A-Za-z0-9]{52}$",
    "abuseipdb": r"^[a-f0-9]{64,}$",
    "otx": r"^[a-f0-9]{64}$",
    "shodan": r"^[A-Za-z0-9]{32}$",
    "greynoise": r"^[A-Za-z0-9]{64}$",
}


@dataclass
class APIKeyConfig:
    """Configuration for an API key."""
    env_var: str
    required: bool = False
    pattern: Optional[str] = None
    description: str = ""


# Registry of all API keys
API_KEYS_REGISTRY: Dict[str, APIKeyConfig] = {
    "virustotal": APIKeyConfig(
        env_var="VIRUSTOTAL_API_KEY",
        required=False,
        pattern=KEY_PATTERNS.get("virustotal"),
        description="VirusTotal API key for hash/URL/IP reputation",
    ),
    "malwarebazaar": APIKeyConfig(
        env_var="MALWAREBAZAAR_API_KEY",
        required=False,
        pattern=r"^[a-f0-9]{40,}$",
        description="MalwareBazaar Auth key for hash lookups",
    ),
    "urlhaus": APIKeyConfig(
        env_var="URLHAUS_API_KEY",
        required=False,
        pattern=r"^[a-f0-9]{40,}$",
        description="URLhaus Auth key for URL lookups",
    ),
    "groq": APIKeyConfig(
        env_var="GROQ_API_KEY",
        required=False,
        pattern=KEY_PATTERNS.get("groq"),
        description="Groq API key for LLM inference",
    ),
    "abuseipdb": APIKeyConfig(
        env_var="ABUSEIPDB_API_KEY",
        required=False,
        pattern=KEY_PATTERNS.get("abuseipdb"),
        description="AbuseIPDB API key for IP reputation",
    ),
    "otx": APIKeyConfig(
        env_var="OTX_API_KEY",
        required=False,
        pattern=KEY_PATTERNS.get("otx"),
        description="AlienVault OTX API key for threat intel",
    ),
    "shodan": APIKeyConfig(
        env_var="SHODAN_API_KEY",
        required=False,
        pattern=KEY_PATTERNS.get("shodan"),
        description="Shodan API key for infrastructure intel",
    ),
    "greynoise": APIKeyConfig(
        env_var="GREYNOISE_API_KEY",
        required=False,
        pattern=KEY_PATTERNS.get("greynoise"),
        description="GreyNoise API key for IP noise classification",
    ),
    "crowdsec": APIKeyConfig(
        env_var="CROWDSEC_API_KEY",
        required=False,
        pattern=r"^[A-Za-z0-9]{32,}$",
        description="CrowdSec CTI API key for botnet/DDoS intel",
    ),
}


class SecretsManager:
    """
    Secure secrets manager for API keys.
    
    Features:
    - Environment-only loading
    - Key validation
    - Redaction for logging
    - Availability tracking
    """
    
    def __init__(self):
        self._keys: Dict[str, str] = {}
        self._available: Dict[str, bool] = {}
        self._load_all_keys()
    
    def _load_all_keys(self) -> None:
        """Load all registered API keys from environment."""
        for key_name, config in API_KEYS_REGISTRY.items():
            value = os.environ.get(config.env_var, "").strip()
            
            if not value:
                self._available[key_name] = False
                if config.required:
                    logger.error(f"Required API key missing: {config.env_var}")
                else:
                    logger.debug(f"Optional API key not set: {config.env_var}")
                continue
            
            # Validate pattern if specified
            if config.pattern and not re.match(config.pattern, value):
                logger.warning(
                    f"API key {key_name} does not match expected pattern "
                    f"(env: {config.env_var})"
                )
                self._available[key_name] = False
                continue
            
            self._keys[key_name] = value
            self._available[key_name] = True
            logger.debug(f"Loaded API key: {key_name} ({self.redact(value)})")
    
    def get(self, key_name: str) -> Optional[str]:
        """
        Get an API key by name.
        
        Args:
            key_name: Name of the key (e.g., 'virustotal', 'groq')
            
        Returns:
            The API key value or None if not available
        """
        return self._keys.get(key_name)
    
    def is_available(self, key_name: str) -> bool:
        """Check if an API key is available and valid."""
        return self._available.get(key_name, False)
    
    def get_available_providers(self) -> List[str]:
        """Get list of providers with valid API keys."""
        return [k for k, v in self._available.items() if v]
    
    def get_missing_providers(self) -> List[str]:
        """Get list of providers without API keys."""
        return [k for k, v in self._available.items() if not v]
    
    @staticmethod
    def redact(value: str, visible_chars: int = 4) -> str:
        """
        Redact an API key for safe logging.
        
        Args:
            value: The secret value to redact
            visible_chars: Number of chars to show at start/end
            
        Returns:
            Redacted string like "abc...xyz"
        """
        if not value or len(value) <= visible_chars * 2:
            return "[REDACTED]"
        return f"{value[:visible_chars]}...{value[-visible_chars:]}"
    
    def redact_in_text(self, text: str) -> str:
        """
        Redact all known API keys in a text string.
        
        Args:
            text: Text that might contain API keys
            
        Returns:
            Text with all API keys redacted
        """
        result = text
        for key_name, value in self._keys.items():
            if value and value in result:
                result = result.replace(value, f"[{key_name.upper()}_KEY_REDACTED]")
        return result
    
    def get_status(self) -> Dict:
        """Get status of all API keys."""
        status = {}
        for key_name, config in API_KEYS_REGISTRY.items():
            status[key_name] = {
                "available": self._available.get(key_name, False),
                "env_var": config.env_var,
                "required": config.required,
                "description": config.description,
            }
        return status
    
    def require(self, key_name: str) -> str:
        """
        Get an API key, raising error if not available.
        
        Args:
            key_name: Name of the key
            
        Returns:
            The API key value
            
        Raises:
            ValueError: If key is not available
        """
        value = self.get(key_name)
        if not value:
            config = API_KEYS_REGISTRY.get(key_name)
            env_var = config.env_var if config else f"{key_name.upper()}_API_KEY"
            raise ValueError(
                f"API key '{key_name}' is required but not available. "
                f"Set {env_var} environment variable."
            )
        return value


# Global singleton instance
_secrets_manager: Optional[SecretsManager] = None


def get_secrets_manager() -> SecretsManager:
    """Get the global secrets manager instance."""
    global _secrets_manager
    if _secrets_manager is None:
        _secrets_manager = SecretsManager()
    return _secrets_manager


def get_api_key(key_name: str) -> Optional[str]:
    """Convenience function to get an API key."""
    return get_secrets_manager().get(key_name)


def is_api_available(key_name: str) -> bool:
    """Convenience function to check API availability."""
    return get_secrets_manager().is_available(key_name)


def redact_secret(value: str) -> str:
    """Convenience function to redact a secret."""
    return SecretsManager.redact(value)
