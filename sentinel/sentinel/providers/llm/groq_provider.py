"""Groq LLM provider with security controls."""

import os
import time
import json
from typing import Optional, Dict, Any, List
from dataclasses import dataclass
from enum import Enum

import requests

from .base_llm import BaseLLMProvider, LLMResponse
from ...utils.rate_limiter import TokenBucketRateLimiter, RateLimitConfig, RateLimitExceeded
from ...utils.sanitizer import InputSanitizer, SanitizerConfig
from ...utils.errors import LLMError
from ...logging import get_logger

logger = get_logger(__name__)


class GroqModel(str, Enum):
    """Available Groq models."""
    GPT_OSS_120B = "openai/gpt-oss-120b"  # Primary - deep reasoning
    LLAMA_8B = "llama-3.1-8b-instant"  # Fallback - fast
    COMPOUND = "groq/compound"  # Web search (optional)


class ReasoningEffort(str, Enum):
    """Reasoning effort levels for gpt-oss-120b."""
    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"


@dataclass
class GroqConfig:
    """Groq provider configuration."""
    api_key: str = ""
    primary_model: str = GroqModel.GPT_OSS_120B.value
    fallback_model: str = GroqModel.LLAMA_8B.value
    enable_web_search: bool = False
    timeout_seconds: int = 30
    max_retries: int = 3
    retry_delay: float = 1.0
    default_reasoning_effort: str = ReasoningEffort.MEDIUM.value
    max_tokens: int = 1024
    temperature: float = 0.3
    
    # Rate limits (conservative, below Groq's actual limits)
    rate_limit_rpm: int = 25
    rate_limit_rpd: int = 5000
    rate_limit_tpm: int = 5000
    rate_limit_tpd: int = 400000


class GroqProvider(BaseLLMProvider):
    """
    Groq LLM provider with full security controls.
    
    Features:
    - Rate limiting (token bucket algorithm)
    - Input sanitization (redacts sensitive data)
    - Model fallback (120B -> 8B)
    - Retry with exponential backoff
    - Usage tracking
    - Optional web search via compound model
    
    Models:
    - gpt-oss-120b: Deep reasoning, slower, higher quality
    - llama-3.1-8b-instant: Fast, lower quality, fallback
    - groq/compound: Web search capabilities (disabled by default)
    """
    
    API_BASE = "https://api.groq.com/openai/v1/chat/completions"
    
    def __init__(self, config: Optional[GroqConfig] = None):
        """
        Initialize Groq provider.
        
        Args:
            config: Provider configuration (loads from env if not provided)
        """
        self.config = config or self._load_config_from_env()
        
        # Validate API key
        if not self.config.api_key:
            raise LLMError("GROQ_API_KEY not configured")
        
        # Initialize rate limiter
        rate_config = RateLimitConfig(
            requests_per_minute=self.config.rate_limit_rpm,
            requests_per_day=self.config.rate_limit_rpd,
            tokens_per_minute=self.config.rate_limit_tpm,
            tokens_per_day=self.config.rate_limit_tpd,
        )
        self._rate_limiter = TokenBucketRateLimiter(rate_config)
        
        # Initialize sanitizer
        self._sanitizer = InputSanitizer(SanitizerConfig(
            max_input_length=4000,
            redact_file_paths=True,
            redact_internal_ips=True,
            redact_usernames=True,
            redact_api_keys=True,
        ))
        
        # Track usage
        self._total_requests = 0
        self._total_tokens = 0
        self._errors = 0
        
        # Validate connection
        self._validate_api_key()
        
        logger.info(
            f"GroqProvider initialized: primary={self.config.primary_model}, "
            f"fallback={self.config.fallback_model}, web_search={self.config.enable_web_search}"
        )
    
    @property
    def name(self) -> str:
        return "groq"
    
    @property
    def version(self) -> str:
        return "1.0.0"
    
    @property
    def supported_types(self):
        """Groq supports all file types via text analysis."""
        from ...parsers.base import FileType
        return {FileType.PE, FileType.PDF, FileType.OFFICE, FileType.SCRIPT, FileType.ANDROID, FileType.PCAP}
    
    def get_cost_per_call(self) -> float:
        """Groq free tier - no cost."""
        return 0.0
    
    def get_avg_latency_ms(self) -> float:
        """Average latency in milliseconds."""
        return 1000.0  # ~1 second typical
    
    def _call_llm(self, prompt: str) -> str:
        """Make LLM API call - used by base class."""
        response = self.analyze(prompt)
        return response.content if not response.error else ""
    
    def _load_config_from_env(self) -> GroqConfig:
        """Load configuration from environment variables."""
        return GroqConfig(
            api_key=os.getenv("GROQ_API_KEY", ""),
            primary_model=os.getenv("GROQ_PRIMARY_MODEL", GroqModel.GPT_OSS_120B.value),
            fallback_model=os.getenv("GROQ_FALLBACK_MODEL", GroqModel.LLAMA_8B.value),
            enable_web_search=os.getenv("GROQ_ENABLE_WEB_SEARCH", "false").lower() == "true",
            timeout_seconds=int(os.getenv("GROQ_TIMEOUT_SECONDS", "30")),
            max_retries=int(os.getenv("GROQ_MAX_RETRIES", "3")),
            default_reasoning_effort=os.getenv("GROQ_REASONING_EFFORT", ReasoningEffort.MEDIUM.value),
            rate_limit_rpm=int(os.getenv("GROQ_RATE_LIMIT_RPM", "25")),
            rate_limit_rpd=int(os.getenv("GROQ_RATE_LIMIT_RPD", "5000")),
            rate_limit_tpm=int(os.getenv("GROQ_RATE_LIMIT_TPM", "5000")),
            rate_limit_tpd=int(os.getenv("GROQ_RATE_LIMIT_TPD", "400000")),
        )
    
    def _validate_api_key(self) -> None:
        """Validate API key on startup."""
        try:
            # Simple validation request
            response = requests.post(
                self.API_BASE,
                headers=self._get_headers(),
                json={
                    "messages": [{"role": "user", "content": "test"}],
                    "model": self.config.fallback_model,
                    "max_completion_tokens": 1,
                },
                timeout=10,
            )
            
            if response.status_code == 401:
                raise LLMError("Invalid GROQ_API_KEY")
            elif response.status_code != 200:
                logger.warning(f"Groq API validation returned {response.status_code}")
                
        except requests.exceptions.RequestException as e:
            logger.warning(f"Could not validate Groq API key: {e}")
    
    def _get_headers(self) -> Dict[str, str]:
        """Get request headers (API key never logged)."""
        return {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {self.config.api_key}",
        }
    
    def analyze(
        self,
        prompt: str,
        system_prompt: Optional[str] = None,
        use_primary: bool = True,
        reasoning_effort: Optional[str] = None,
    ) -> LLMResponse:
        """
        Analyze content using Groq LLM.
        
        Args:
            prompt: User prompt (will be sanitized)
            system_prompt: System prompt for context
            use_primary: Use primary model (120B) or fallback (8B)
            reasoning_effort: Reasoning effort for 120B model
            
        Returns:
            LLMResponse with analysis results
        """
        t0 = time.perf_counter()
        
        # 1. Sanitize input
        safe_prompt = self._sanitizer.sanitize(prompt, context="user_prompt")
        safe_system = self._sanitizer.sanitize(
            system_prompt or self._default_system_prompt(),
            context="system_prompt"
        )
        
        # 2. Estimate tokens for rate limiting
        estimated_tokens = len(safe_prompt.split()) * 2 + len(safe_system.split()) * 2
        
        # 3. Check rate limit
        try:
            self._rate_limiter.check_limit(estimated_tokens)
        except RateLimitExceeded as e:
            logger.warning(f"Rate limit exceeded: {e}")
            return LLMResponse(
                content="",
                model="",
                tokens_used=0,
                latency_ms=0,
                error=f"Rate limit exceeded: {e.limit_type}",
                metadata={"rate_limit_reset": e.reset_seconds},
            )
        
        # 4. Select model
        model = self.config.primary_model if use_primary else self.config.fallback_model
        
        # 5. Make request with retry
        response = self._make_request_with_retry(
            model=model,
            prompt=safe_prompt,
            system_prompt=safe_system,
            reasoning_effort=reasoning_effort,
        )
        
        # 6. Record usage
        if response.tokens_used > 0:
            self._rate_limiter.record_request(response.tokens_used, success=not response.error)
            self._total_requests += 1
            self._total_tokens += response.tokens_used
        
        if response.error:
            self._errors += 1
        
        response.latency_ms = (time.perf_counter() - t0) * 1000
        return response
    
    def analyze_with_fallback(
        self,
        prompt: str,
        system_prompt: Optional[str] = None,
    ) -> LLMResponse:
        """
        Analyze with automatic fallback to faster model.
        
        Tries primary (120B) first, falls back to 8B on failure/timeout.
        """
        # Try primary model
        response = self.analyze(
            prompt=prompt,
            system_prompt=system_prompt,
            use_primary=True,
        )
        
        # Fallback on error or timeout
        if response.error and "rate limit" not in response.error.lower():
            logger.info(f"Primary model failed, trying fallback: {response.error}")
            response = self.analyze(
                prompt=prompt,
                system_prompt=system_prompt,
                use_primary=False,
            )
        
        return response
    
    def web_search(self, query: str) -> LLMResponse:
        """
        Perform web search using compound model.
        
        Only works if enable_web_search=True.
        Query is sanitized before sending.
        """
        if not self.config.enable_web_search:
            return LLMResponse(
                content="",
                model="",
                tokens_used=0,
                latency_ms=0,
                error="Web search is disabled",
            )
        
        # Extra sanitization for web queries
        safe_query = self._sanitizer.sanitize(query, context="web_query")
        
        # Check rate limit
        try:
            self._rate_limiter.check_limit(500)
        except RateLimitExceeded as e:
            return LLMResponse(
                content="",
                model="",
                tokens_used=0,
                latency_ms=0,
                error=f"Rate limit exceeded: {e.limit_type}",
            )
        
        t0 = time.perf_counter()
        
        try:
            response = requests.post(
                self.API_BASE,
                headers=self._get_headers(),
                json={
                    "messages": [{"role": "user", "content": safe_query}],
                    "model": GroqModel.COMPOUND.value,
                    "max_completion_tokens": 512,
                    "compound_custom": {
                        "tools": {
                            "enabled_tools": ["web_search"],
                        }
                    },
                },
                timeout=self.config.timeout_seconds,
            )
            
            if response.status_code == 200:
                data = response.json()
                content = data["choices"][0]["message"]["content"]
                tokens = data.get("usage", {}).get("total_tokens", 0)
                
                self._rate_limiter.record_request(tokens, success=True)
                
                return LLMResponse(
                    content=content,
                    model=GroqModel.COMPOUND.value,
                    tokens_used=tokens,
                    latency_ms=(time.perf_counter() - t0) * 1000,
                )
            else:
                return LLMResponse(
                    content="",
                    model=GroqModel.COMPOUND.value,
                    tokens_used=0,
                    latency_ms=(time.perf_counter() - t0) * 1000,
                    error=f"API error: {response.status_code}",
                )
                
        except Exception as e:
            return LLMResponse(
                content="",
                model=GroqModel.COMPOUND.value,
                tokens_used=0,
                latency_ms=(time.perf_counter() - t0) * 1000,
                error=str(e),
            )
    
    def _make_request_with_retry(
        self,
        model: str,
        prompt: str,
        system_prompt: str,
        reasoning_effort: Optional[str] = None,
    ) -> LLMResponse:
        """Make API request with exponential backoff retry."""
        
        messages = [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": prompt},
        ]
        
        payload = {
            "messages": messages,
            "model": model,
            "temperature": self.config.temperature,
            "max_completion_tokens": self.config.max_tokens,
        }
        
        # Add reasoning effort for 120B model
        if model == GroqModel.GPT_OSS_120B.value:
            effort = reasoning_effort or self.config.default_reasoning_effort
            payload["reasoning_effort"] = effort
        
        last_error = None
        
        for attempt in range(self.config.max_retries):
            try:
                response = requests.post(
                    self.API_BASE,
                    headers=self._get_headers(),
                    json=payload,
                    timeout=self.config.timeout_seconds,
                )
                
                if response.status_code == 200:
                    data = response.json()
                    content = data["choices"][0]["message"]["content"]
                    tokens = data.get("usage", {}).get("total_tokens", 0)
                    
                    return LLMResponse(
                        content=content,
                        model=model,
                        tokens_used=tokens,
                        latency_ms=0,  # Set by caller
                        metadata={
                            "finish_reason": data["choices"][0].get("finish_reason"),
                            "prompt_tokens": data.get("usage", {}).get("prompt_tokens", 0),
                            "completion_tokens": data.get("usage", {}).get("completion_tokens", 0),
                        }
                    )
                
                elif response.status_code == 429:
                    # Rate limited by Groq
                    logger.warning(f"Groq rate limit hit (attempt {attempt + 1})")
                    self._rate_limiter.trigger_cooldown()
                    last_error = "Rate limited by Groq API"
                    
                elif response.status_code >= 500:
                    # Server error - retry
                    last_error = f"Server error: {response.status_code}"
                    logger.warning(f"Groq server error (attempt {attempt + 1}): {response.status_code}")
                    
                else:
                    # Client error - don't retry
                    error_detail = response.json().get("error", {}).get("message", response.text)
                    return LLMResponse(
                        content="",
                        model=model,
                        tokens_used=0,
                        latency_ms=0,
                        error=f"API error {response.status_code}: {error_detail}",
                    )
                    
            except requests.exceptions.Timeout:
                last_error = "Request timeout"
                logger.warning(f"Groq timeout (attempt {attempt + 1})")
                
            except requests.exceptions.RequestException as e:
                last_error = str(e)
                logger.warning(f"Groq request error (attempt {attempt + 1}): {e}")
            
            # Exponential backoff
            if attempt < self.config.max_retries - 1:
                delay = self.config.retry_delay * (2 ** attempt)
                time.sleep(delay)
        
        return LLMResponse(
            content="",
            model=model,
            tokens_used=0,
            latency_ms=0,
            error=f"All retries failed: {last_error}",
        )
    
    def _default_system_prompt(self) -> str:
        """Default system prompt for threat analysis."""
        return """You are a cybersecurity expert analyzing potentially malicious files.

Your task:
1. Analyze the provided file characteristics and findings
2. Identify malicious indicators and techniques (MITRE ATT&CK if applicable)
3. Assess the threat level (clean, suspicious, malicious)
4. Provide a confidence score (0-100%)
5. Explain your reasoning concisely

Be precise and technical. Do not speculate beyond the evidence provided."""
    
    def get_usage_stats(self) -> Dict[str, Any]:
        """Get provider usage statistics."""
        rate_stats = self._rate_limiter.get_stats()
        
        return {
            "provider": self.name,
            "total_requests": self._total_requests,
            "total_tokens": self._total_tokens,
            "errors": self._errors,
            "rate_limit": rate_stats,
            "models": {
                "primary": self.config.primary_model,
                "fallback": self.config.fallback_model,
            },
            "web_search_enabled": self.config.enable_web_search,
        }
    
    def get_remaining_capacity(self) -> Dict[str, int]:
        """Get remaining API capacity."""
        return self._rate_limiter.get_remaining()
