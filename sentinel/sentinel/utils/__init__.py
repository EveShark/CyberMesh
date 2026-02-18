"""Utility functions and classes."""

from .errors import (
    SentinelError,
    ConfigError,
    ParserError,
    ProviderError,
    ValidationError,
    SignerError,
)
from .hash import compute_file_hash, compute_content_hash, compute_sha256, compute_multi_hash
from .redaction import redact_dict, redact_secrets, SENSITIVE_FIELDS
from .id_gen import generate_analysis_id
from .signer import Signer, verify_model_fingerprint, compute_file_fingerprint
from .rate_limiter import TokenBucketRateLimiter, RateLimitConfig, RateLimitExceeded
from .sanitizer import InputSanitizer, SanitizerConfig

__all__ = [
    "SentinelError",
    "ConfigError",
    "ParserError",
    "ProviderError",
    "ValidationError",
    "SignerError",
    "compute_file_hash",
    "compute_content_hash",
    "compute_sha256",
    "compute_multi_hash",
    "redact_dict",
    "redact_secrets",
    "SENSITIVE_FIELDS",
    "generate_analysis_id",
    "Signer",
    "verify_model_fingerprint",
    "compute_file_fingerprint",
    "TokenBucketRateLimiter",
    "RateLimitConfig",
    "RateLimitExceeded",
    "InputSanitizer",
    "SanitizerConfig",
]
