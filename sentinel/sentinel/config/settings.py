"""Configuration settings for Sentinel."""

import os
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional, List
from dotenv import load_dotenv

from ..utils.errors import ConfigError


@dataclass(frozen=True)
class LLMConfig:
    """LLM provider configuration."""
    glm_api_key: Optional[str] = None
    glm_api_base: str = "https://open.bigmodel.cn/api/paas/v4"
    glm_model: str = "glm-4-plus"
    
    qwen_api_key: Optional[str] = None
    qwen_api_base: str = "https://dashscope.aliyuncs.com/api/v1"
    qwen_model: str = "qwen-max"
    
    ollama_host: str = "http://localhost:11434"
    ollama_model: str = "llama3.1:8b"


@dataclass(frozen=True)
class ThreatIntelConfig:
    """Threat intelligence API configuration."""
    # Threat Intel APIs
    virustotal_api_key: Optional[str] = None
    abuseipdb_api_key: Optional[str] = None
    otx_api_key: Optional[str] = None
    greynoise_api_key: Optional[str] = None
    shodan_api_key: Optional[str] = None
    urlvoid_api_key: Optional[str] = None
    
    # MaxMind GeoIP
    maxmind_account_id: Optional[str] = None
    maxmind_license_key: Optional[str] = None
    geoip_database_path: Optional[Path] = None
    geoip_city_database: str = "GeoLite2-City.mmdb"
    geoip_asn_database: str = "GeoLite2-ASN.mmdb"
    
    # Update settings
    threat_intel_enabled: bool = True
    threat_intel_update_interval: int = 3600
    threat_intel_cache_ttl: int = 86400


@dataclass(frozen=True)
class ModelConfig:
    """ML model configuration."""
    models_path: Path = field(default_factory=lambda: Path("../CyberMesh/ai-service/data/models"))
    model_registry_path: Optional[Path] = None
    
    def __post_init__(self):
        if self.model_registry_path is None:
            object.__setattr__(
                self,
                "model_registry_path",
                self.models_path / "model_registry.json"
            )


@dataclass(frozen=True)
class DetectionConfig:
    """Detection threshold configuration."""
    malware_threshold: float = 0.85
    fast_path_threshold: float = 0.95
    uncertain_threshold: float = 0.5
    min_confidence: float = 0.7


@dataclass(frozen=True)
class AgentConfig:
    """Agent configuration."""
    timeout_seconds: int = 30
    max_iterations: int = 5
    coordinator_model: str = "glm-4-plus"


@dataclass(frozen=True)
class SecurityConfig:
    """Security configuration."""
    secret_key: Optional[str] = None
    encryption_key: Optional[str] = None
    signing_key: Optional[str] = None


@dataclass(frozen=True)
class KafkaConfig:
    """Kafka configuration."""
    enabled: bool = False
    bootstrap_servers: Optional[str] = None
    tls_enabled: bool = True
    sasl_mechanism: str = "PLAIN"
    sasl_username: Optional[str] = None
    sasl_password: Optional[str] = None
    output_topic: str = "sentinel.verdicts.v1"
    consumer_group_id: str = "sentinel-consumer-1"


@dataclass(frozen=True)
class RedisConfig:
    """Redis configuration."""
    enabled: bool = False
    host: Optional[str] = None
    port: int = 6379
    password: Optional[str] = None
    tls_enabled: bool = True
    db: int = 0


@dataclass(frozen=True)
class FastPathConfig:
    """Fast path configuration."""
    yara_rules_path: Optional[Path] = None
    malware_hashes_path: Optional[Path] = None
    clean_hashes_path: Optional[Path] = None


@dataclass(frozen=True)
class OutputConfig:
    """Output configuration."""
    report_output_dir: Path = field(default_factory=lambda: Path("./reports"))


@dataclass(frozen=True)
class Settings:
    """Complete Sentinel configuration."""
    environment: str
    node_id: str
    log_level: str
    log_format: str
    
    security: SecurityConfig
    llm: LLMConfig
    threat_intel: ThreatIntelConfig
    models: ModelConfig
    detection: DetectionConfig
    agent: AgentConfig
    kafka: KafkaConfig
    redis: RedisConfig
    fast_path: FastPathConfig
    output: OutputConfig
    
    def validate(self) -> None:
        """Validate configuration."""
        if not (0.0 <= self.detection.malware_threshold <= 1.0):
            raise ConfigError("malware_threshold must be in [0.0, 1.0]")
        if not (0.0 <= self.detection.fast_path_threshold <= 1.0):
            raise ConfigError("fast_path_threshold must be in [0.0, 1.0]")
        if not (0.0 <= self.detection.uncertain_threshold <= 1.0):
            raise ConfigError("uncertain_threshold must be in [0.0, 1.0]")
        if self.agent.timeout_seconds <= 0:
            raise ConfigError("agent timeout_seconds must be positive")
        if self.agent.max_iterations <= 0:
            raise ConfigError("agent max_iterations must be positive")


def load_settings(env_file: Optional[str] = None) -> Settings:
    """
    Load settings from environment variables.
    
    Args:
        env_file: Optional path to .env file
        
    Returns:
        Validated Settings instance
        
    Raises:
        ConfigError: If configuration is invalid
    """
    if env_file:
        load_dotenv(env_file)
    else:
        load_dotenv()
    
    def get_env(key: str, default: str = "") -> str:
        return os.getenv(key, default)
    
    def get_bool(key: str, default: bool = False) -> bool:
        value = os.getenv(key)
        if value is None:
            return default
        return value.lower() in ("true", "1", "yes", "on")
    
    def get_int(key: str, default: int) -> int:
        value = os.getenv(key)
        if value is None:
            return default
        try:
            return int(value)
        except ValueError:
            raise ConfigError(f"{key} must be an integer, got: {value}")
    
    def get_float(key: str, default: float) -> float:
        value = os.getenv(key)
        if value is None:
            return default
        try:
            return float(value)
        except ValueError:
            raise ConfigError(f"{key} must be a float, got: {value}")
    
    def get_path(key: str, default: str) -> Path:
        return Path(get_env(key, default))
    
    llm = LLMConfig(
        glm_api_key=get_env("GLM_API_KEY") or None,
        glm_api_base=get_env("GLM_API_BASE", "https://open.bigmodel.cn/api/paas/v4"),
        glm_model=get_env("GLM_MODEL", "glm-4-plus"),
        qwen_api_key=get_env("QWEN_API_KEY") or None,
        qwen_api_base=get_env("QWEN_API_BASE", "https://dashscope.aliyuncs.com/api/v1"),
        qwen_model=get_env("QWEN_MODEL", "qwen-max"),
        ollama_host=get_env("OLLAMA_HOST", "http://localhost:11434"),
        ollama_model=get_env("OLLAMA_MODEL", "llama3.1:8b"),
    )
    
    threat_intel = ThreatIntelConfig(
        virustotal_api_key=get_env("VIRUSTOTAL_API_KEY") or None,
        abuseipdb_api_key=get_env("ABUSEIPDB_API_KEY") or None,
        otx_api_key=get_env("OTX_API_KEY") or None,
        greynoise_api_key=get_env("GREYNOISE_API_KEY") or None,
        shodan_api_key=get_env("SHODAN_API_KEY") or None,
        urlvoid_api_key=get_env("URLVOID_API_KEY") or None,
        maxmind_account_id=get_env("MAXMIND_ACCOUNT_ID") or None,
        maxmind_license_key=get_env("MAXMIND_LICENSE_KEY") or None,
        geoip_database_path=get_path("GEOIP_DATABASE_PATH", "") if get_env("GEOIP_DATABASE_PATH") else None,
        geoip_city_database=get_env("GEOIP_CITY_DATABASE", "GeoLite2-City.mmdb"),
        geoip_asn_database=get_env("GEOIP_ASN_DATABASE", "GeoLite2-ASN.mmdb"),
        threat_intel_enabled=get_bool("THREAT_INTEL_ENABLED", True),
        threat_intel_update_interval=get_int("THREAT_INTEL_UPDATE_INTERVAL", 3600),
        threat_intel_cache_ttl=get_int("THREAT_INTEL_CACHE_TTL", 86400),
    )
    
    models = ModelConfig(
        models_path=get_path("MODELS_PATH", "../CyberMesh/ai-service/data/models"),
    )
    
    detection = DetectionConfig(
        malware_threshold=get_float("MALWARE_THRESHOLD", 0.85),
        fast_path_threshold=get_float("FAST_PATH_THRESHOLD", 0.95),
        uncertain_threshold=get_float("UNCERTAIN_THRESHOLD", 0.5),
        min_confidence=get_float("MIN_CONFIDENCE", 0.7),
    )
    
    agent = AgentConfig(
        timeout_seconds=get_int("AGENT_TIMEOUT_SECONDS", 30),
        max_iterations=get_int("MAX_AGENT_ITERATIONS", 5),
        coordinator_model=get_env("COORDINATOR_MODEL", "glm-4-plus"),
    )
    
    security = SecurityConfig(
        secret_key=get_env("SECRET_KEY") or None,
        encryption_key=get_env("ENCRYPTION_KEY") or None,
        signing_key=get_env("SIGNING_KEY") or None,
    )
    
    kafka = KafkaConfig(
        enabled=get_bool("ENABLE_KAFKA", False),
        bootstrap_servers=get_env("KAFKA_BOOTSTRAP_SERVERS") or None,
        tls_enabled=get_bool("KAFKA_TLS_ENABLED", True),
        sasl_mechanism=get_env("KAFKA_SASL_MECHANISM", "PLAIN"),
        sasl_username=get_env("KAFKA_SASL_USERNAME") or None,
        sasl_password=get_env("KAFKA_SASL_PASSWORD") or None,
        output_topic=get_env("KAFKA_OUTPUT_TOPIC", "sentinel.verdicts.v1"),
        consumer_group_id=get_env("KAFKA_CONSUMER_GROUP_ID", "sentinel-consumer-1"),
    )
    
    redis = RedisConfig(
        enabled=get_bool("ENABLE_REDIS", False),
        host=get_env("REDIS_HOST") or None,
        port=get_int("REDIS_PORT", 6379),
        password=get_env("REDIS_PASSWORD") or None,
        tls_enabled=get_bool("REDIS_TLS_ENABLED", True),
        db=get_int("REDIS_DB", 0),
    )
    
    # Default paths for fast path - always try to load if files exist
    default_malware_hashes = get_path("MALWARE_HASHES_PATH", "./data/malware_hashes.json")
    default_clean_hashes = get_path("CLEAN_HASHES_PATH", "./data/clean_hashes.json")
    
    fast_path = FastPathConfig(
        yara_rules_path=get_path("YARA_RULES_PATH", "./data/yara_rules") if get_env("YARA_RULES_PATH") else None,
        malware_hashes_path=default_malware_hashes if default_malware_hashes.exists() else None,
        clean_hashes_path=default_clean_hashes if default_clean_hashes.exists() else None,
    )
    
    output = OutputConfig(
        report_output_dir=get_path("REPORT_OUTPUT_DIR", "./reports"),
    )
    
    settings = Settings(
        environment=get_env("ENVIRONMENT", "development"),
        node_id=get_env("NODE_ID", "sentinel-1"),
        log_level=get_env("LOG_LEVEL", "INFO"),
        log_format=get_env("LOG_FORMAT", "human"),
        security=security,
        llm=llm,
        threat_intel=threat_intel,
        models=models,
        detection=detection,
        agent=agent,
        kafka=kafka,
        redis=redis,
        fast_path=fast_path,
        output=output,
    )
    
    settings.validate()
    return settings
