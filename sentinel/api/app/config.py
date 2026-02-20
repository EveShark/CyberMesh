"""Application configuration loaded from environment variables."""

import os
from pathlib import Path
from typing import Optional
from pydantic_settings import BaseSettings, SettingsConfigDict
from functools import lru_cache


class Settings(BaseSettings):
    """Application settings with environment variable support."""
    
    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )
    
    # App
    app_env: str = "development"
    app_debug: bool = True
    app_host: str = "0.0.0.0"
    app_port: int = 8000
    
    # Supabase
    supabase_url: str
    supabase_key: str
    supabase_jwt_secret: Optional[str] = None
    
    # Feature Flags
    enable_async_scan: bool = True
    enable_rate_limit: bool = True
    enable_cache: bool = True
    
    # Limits
    max_file_size_mb: int = 50
    scan_timeout_seconds: int = 60
    rate_limit_per_minute: int = 10
    rate_limit_per_hour: int = 100
    
    # Sentinel Paths
    sentinel_path: str = "../"
    yara_rules_path: str = "../rules"
    malware_hashes_path: str = "../data/malware_hashes.json"
    
    # Data Retention
    scan_retention_days: int = 90
    
    # CORS
    cors_origins: str = "*"
    
    @property
    def max_file_size_bytes(self) -> int:
        return self.max_file_size_mb * 1024 * 1024
    
    @property
    def sentinel_absolute_path(self) -> Path:
        return Path(__file__).parent.parent.parent / self.sentinel_path
    
    @property
    def is_production(self) -> bool:
        return self.app_env == "production"


@lru_cache()
def get_settings() -> Settings:
    """Get cached settings instance."""
    return Settings()
