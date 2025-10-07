import os
from typing import Optional
from dataclasses import dataclass


@dataclass(frozen=True)
class Ed25519Config:
    """Ed25519 signing key configuration."""
    signing_key_path: str
    signing_key_id: str
    domain_separation: str = "ai.v1"
    
    def validate(self, environment: str):
        """Validate signing key configuration."""
        import platform
        
        if not os.path.exists(self.signing_key_path):
            raise ValueError(f"Signing key not found: {self.signing_key_path}")
        
        env_lower = environment.lower()
        if env_lower == 'production':
            if platform.system() == 'Windows':
                raise ValueError(
                    "Windows hosts cannot be used for production signing keys; deploy to a POSIX-compliant host."
                )
            stat_info = os.stat(self.signing_key_path)
            if stat_info.st_mode & 0o077:
                raise ValueError(
                    f"Signing key {self.signing_key_path} has insecure permissions. "
                    f"Expected 0600, got {oct(stat_info.st_mode & 0o777)}"
                )
        
        if len(self.signing_key_id) == 0:
            raise ValueError("signing_key_id cannot be empty")
        
        if len(self.signing_key_id) > 128:
            raise ValueError(f"signing_key_id too long: {len(self.signing_key_id)} > 128")


@dataclass(frozen=True)
class JWTConfig:
    """JWT configuration for admin API authentication."""
    secret: str
    algorithm: str = "HS256"
    expiration_seconds: int = 3600
    
    def validate(self, environment: str):
        """Validate JWT configuration."""
        if len(self.secret) < 64:
            raise ValueError(f"JWT secret too weak: {len(self.secret)} chars, minimum 64 required")
        
        if environment == "production" and len(self.secret) < 128:
            raise ValueError("JWT secret must be at least 128 characters in production")
        
        if self.algorithm not in ("HS256", "HS384", "HS512"):
            raise ValueError(f"Unsupported JWT algorithm: {self.algorithm}")
        
        if self.expiration_seconds <= 0:
            raise ValueError("JWT expiration must be positive")


@dataclass(frozen=True)
class MTLSConfig:
    """Mutual TLS configuration for admin API."""
    enabled: bool
    ca_cert_path: Optional[str]
    server_cert_path: Optional[str]
    server_key_path: Optional[str]
    
    def validate(self, environment: str):
        """Validate mTLS configuration."""
        if not self.enabled:
            return
        
        if not self.ca_cert_path:
            raise ValueError("mTLS enabled but CA cert path not set")
        if not self.server_cert_path:
            raise ValueError("mTLS enabled but server cert path not set")
        if not self.server_key_path:
            raise ValueError("mTLS enabled but server key path not set")
        
        for path in [self.ca_cert_path, self.server_cert_path, self.server_key_path]:
            if path and not os.path.exists(path):
                raise ValueError(f"mTLS certificate/key not found: {path}")
        
        if self.server_key_path:
            stat_info = os.stat(self.server_key_path)
            if stat_info.st_mode & 0o077:
                raise ValueError(
                    f"Server key {self.server_key_path} has insecure permissions. "
                    f"Expected 0600"
                )


@dataclass(frozen=True)
class AESConfig:
    """AES-256-GCM configuration for at-rest encryption."""
    key_path: Optional[str]
    enabled: bool = False
    
    def validate(self, environment: str):
        """Validate AES configuration."""
        if not self.enabled:
            return
        
        if not self.key_path:
            raise ValueError("AES encryption enabled but key path not set")
        
        if not os.path.exists(self.key_path):
            raise ValueError(f"AES key not found: {self.key_path}")
        
        stat_info = os.stat(self.key_path)
        if stat_info.st_mode & 0o077:
            raise ValueError(
                f"AES key {self.key_path} has insecure permissions. "
                f"Expected 0600"
            )


@dataclass(frozen=True)
class SecurityConfig:
    """Complete security configuration."""
    ed25519: Ed25519Config
    jwt: JWTConfig
    mtls: MTLSConfig
    aes: AESConfig
    
    def validate(self, environment: str):
        """Validate all security configurations."""
        self.ed25519.validate(environment)
        self.jwt.validate(environment)
        self.mtls.validate(environment)
        self.aes.validate(environment)
        
        if environment == "production":
            if not (self.mtls.enabled or len(self.jwt.secret) >= 128):
                raise ValueError("Production requires mTLS or strong JWT (128+ chars)")


def load_security_config(environment: str) -> SecurityConfig:
    """Load and validate security configuration from environment variables."""
    ed25519 = Ed25519Config(
        signing_key_path=_require_env("AI_SIGNING_KEY_PATH"),
        signing_key_id=_require_env("AI_SIGNING_KEY_ID"),
        domain_separation=_get_env("AI_DOMAIN_SEPARATION", "ai.v1")
    )
    
    jwt = JWTConfig(
        secret=_require_env("JWT_SECRET"),
        algorithm=_get_env("JWT_ALGORITHM", "HS256"),
        expiration_seconds=_get_int_env("JWT_EXPIRATION_SECONDS", 3600)
    )
    
    mtls = MTLSConfig(
        enabled=_get_bool_env("MTLS_ENABLED", False),
        ca_cert_path=_get_env("MTLS_CA_CERT_PATH") or None,
        server_cert_path=_get_env("MTLS_SERVER_CERT_PATH") or None,
        server_key_path=_get_env("MTLS_SERVER_KEY_PATH") or None
    )
    
    aes = AESConfig(
        enabled=_get_bool_env("AES_ENCRYPTION_ENABLED", False),
        key_path=_get_env("AES_KEY_PATH") or None
    )
    
    config = SecurityConfig(
        ed25519=ed25519,
        jwt=jwt,
        mtls=mtls,
        aes=aes
    )
    
    config.validate(environment)
    
    return config


def _require_env(key: str) -> str:
    value = os.getenv(key)
    if not value:
        raise ValueError(f"Required environment variable {key} is not set")
    return value


def _get_env(key: str, default: str = "") -> str:
    return os.getenv(key, default)


def _get_bool_env(key: str, default: bool = False) -> bool:
    value = os.getenv(key)
    if value is None:
        return default
    return value.lower() in ("true", "1", "yes", "on")


def _get_int_env(key: str, default: int) -> int:
    value = os.getenv(key)
    if value is None:
        return default
    try:
        return int(value)
    except ValueError:
        raise ValueError(f"Environment variable {key} must be an integer, got: {value}")

