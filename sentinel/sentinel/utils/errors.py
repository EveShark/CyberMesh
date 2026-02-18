"""Exception classes for Sentinel."""


class SentinelError(Exception):
    """Base exception for all Sentinel errors."""
    pass


class ConfigError(SentinelError):
    """Configuration loading or validation failed."""
    pass


class ParserError(SentinelError):
    """File parsing failed."""
    pass


class ProviderError(SentinelError):
    """Provider analysis failed."""
    pass


class ValidationError(SentinelError):
    """Input validation failed."""
    pass


class RouterError(SentinelError):
    """Router selection failed."""
    pass


class AgentError(SentinelError):
    """Agent execution failed."""
    pass


class OutputError(SentinelError):
    """Output generation failed."""
    pass


class ModelLoadError(SentinelError):
    """ML model loading failed."""
    pass


class SignatureError(SentinelError):
    """Model signature verification failed."""
    pass


class SignerError(SentinelError):
    """Ed25519 signing/verification failed."""
    pass


class LLMError(ProviderError):
    """LLM API call failed."""
    pass


class TimeoutError(SentinelError):
    """Operation timed out."""
    pass
