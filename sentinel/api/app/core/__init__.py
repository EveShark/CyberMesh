"""Core module - Auth, security, rate limiting, exceptions."""

from .auth import get_current_user, JWTBearer
from .exceptions import (
    SentinelAPIException,
    FileTooLargeError,
    InvalidFileTypeError,
    ScanNotFoundError,
    RateLimitExceededError,
)
from .security import validate_file, sanitize_filename, get_magic_type

__all__ = [
    "get_current_user",
    "JWTBearer",
    "SentinelAPIException",
    "FileTooLargeError",
    "InvalidFileTypeError",
    "ScanNotFoundError",
    "RateLimitExceededError",
    "validate_file",
    "sanitize_filename",
    "get_magic_type",
]
