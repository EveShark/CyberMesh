"""Thread-safe token bucket rate limiter for API calls."""

import threading
import time
from dataclasses import dataclass, field
from typing import Optional, Dict
from datetime import datetime, date

from .errors import SentinelError
import logging

logger = logging.getLogger(__name__)


class RateLimitExceeded(SentinelError):
    """Rate limit has been exceeded."""
    
    def __init__(self, limit_type: str, reset_seconds: float):
        self.limit_type = limit_type
        self.reset_seconds = reset_seconds
        super().__init__(f"Rate limit exceeded: {limit_type}. Reset in {reset_seconds:.1f}s")


@dataclass
class RateLimitConfig:
    """Configuration for rate limiting."""
    requests_per_minute: int = 25
    requests_per_day: int = 5000
    tokens_per_minute: int = 5000
    tokens_per_day: int = 400000
    burst_allowance: int = 5
    cooldown_seconds: float = 60.0


@dataclass
class UsageStats:
    """Track usage statistics."""
    requests_today: int = 0
    tokens_today: int = 0
    requests_this_minute: int = 0
    tokens_this_minute: int = 0
    last_request_time: float = 0.0
    last_minute_reset: float = 0.0
    last_day_reset: str = ""
    total_requests: int = 0
    total_tokens: int = 0
    rate_limit_hits: int = 0
    errors: int = 0


class TokenBucketRateLimiter:
    """
    Thread-safe rate limiter using token bucket algorithm.
    
    Supports:
    - Requests per minute (RPM)
    - Requests per day (RPD)
    - Tokens per minute (TPM)
    - Tokens per day (TPD)
    - Burst allowance
    - Cooldown on limit hit
    """
    
    def __init__(self, config: Optional[RateLimitConfig] = None):
        self.config = config or RateLimitConfig()
        self._lock = threading.RLock()
        self._stats = UsageStats()
        self._cooldown_until: float = 0.0
        self._burst_tokens: float = float(self.config.burst_allowance)
        self._last_burst_refill: float = time.time()
        
        # Initialize day tracking
        self._stats.last_day_reset = str(date.today())
        self._stats.last_minute_reset = time.time()
    
    def _reset_if_needed(self) -> None:
        """Reset counters if time period has elapsed."""
        now = time.time()
        today = str(date.today())
        
        # Reset daily counters
        if self._stats.last_day_reset != today:
            logger.info(f"Daily reset: {self._stats.requests_today} requests, {self._stats.tokens_today} tokens yesterday")
            self._stats.requests_today = 0
            self._stats.tokens_today = 0
            self._stats.last_day_reset = today
        
        # Reset minute counters (every 60 seconds)
        if now - self._stats.last_minute_reset >= 60.0:
            self._stats.requests_this_minute = 0
            self._stats.tokens_this_minute = 0
            self._stats.last_minute_reset = now
        
        # Refill burst tokens (1 token per second, up to max)
        elapsed = now - self._last_burst_refill
        self._burst_tokens = min(
            self._burst_tokens + elapsed,
            float(self.config.burst_allowance)
        )
        self._last_burst_refill = now
    
    def check_limit(self, estimated_tokens: int = 100) -> None:
        """
        Check if request is allowed under current limits.
        
        Args:
            estimated_tokens: Estimated tokens for the request
            
        Raises:
            RateLimitExceeded: If any limit would be exceeded
        """
        with self._lock:
            now = time.time()
            self._reset_if_needed()
            
            # Check cooldown
            if now < self._cooldown_until:
                wait_time = self._cooldown_until - now
                raise RateLimitExceeded("cooldown", wait_time)
            
            # Check requests per minute
            if self._stats.requests_this_minute >= self.config.requests_per_minute:
                if self._burst_tokens < 1.0:
                    self._stats.rate_limit_hits += 1
                    wait_time = 60.0 - (now - self._stats.last_minute_reset)
                    raise RateLimitExceeded("requests_per_minute", max(wait_time, 1.0))
            
            # Check requests per day
            if self._stats.requests_today >= self.config.requests_per_day:
                self._stats.rate_limit_hits += 1
                raise RateLimitExceeded("requests_per_day", self._seconds_until_midnight())
            
            # Check tokens per minute
            if self._stats.tokens_this_minute + estimated_tokens > self.config.tokens_per_minute:
                self._stats.rate_limit_hits += 1
                wait_time = 60.0 - (now - self._stats.last_minute_reset)
                raise RateLimitExceeded("tokens_per_minute", max(wait_time, 1.0))
            
            # Check tokens per day
            if self._stats.tokens_today + estimated_tokens > self.config.tokens_per_day:
                self._stats.rate_limit_hits += 1
                raise RateLimitExceeded("tokens_per_day", self._seconds_until_midnight())
    
    def record_request(self, tokens_used: int, success: bool = True) -> None:
        """
        Record a completed request.
        
        Args:
            tokens_used: Actual tokens used
            success: Whether request succeeded
        """
        with self._lock:
            self._reset_if_needed()
            
            self._stats.requests_this_minute += 1
            self._stats.requests_today += 1
            self._stats.tokens_this_minute += tokens_used
            self._stats.tokens_today += tokens_used
            self._stats.total_requests += 1
            self._stats.total_tokens += tokens_used
            self._stats.last_request_time = time.time()
            
            # Consume burst token if over minute limit
            if self._stats.requests_this_minute > self.config.requests_per_minute:
                self._burst_tokens = max(0, self._burst_tokens - 1.0)
            
            if not success:
                self._stats.errors += 1
            
            # Log at thresholds
            day_pct = (self._stats.requests_today / self.config.requests_per_day) * 100
            if day_pct >= 90 and self._stats.requests_today % 100 == 0:
                logger.warning(f"Rate limit warning: {day_pct:.0f}% of daily requests used")
    
    def trigger_cooldown(self) -> None:
        """Trigger cooldown period (e.g., after API error)."""
        with self._lock:
            self._cooldown_until = time.time() + self.config.cooldown_seconds
            logger.warning(f"Rate limiter cooldown triggered for {self.config.cooldown_seconds}s")
    
    def get_stats(self) -> Dict:
        """Get current usage statistics."""
        with self._lock:
            self._reset_if_needed()
            return {
                "requests_today": self._stats.requests_today,
                "requests_limit_day": self.config.requests_per_day,
                "requests_pct": (self._stats.requests_today / self.config.requests_per_day) * 100,
                "tokens_today": self._stats.tokens_today,
                "tokens_limit_day": self.config.tokens_per_day,
                "tokens_pct": (self._stats.tokens_today / self.config.tokens_per_day) * 100,
                "requests_this_minute": self._stats.requests_this_minute,
                "tokens_this_minute": self._stats.tokens_this_minute,
                "burst_tokens_remaining": self._burst_tokens,
                "total_requests": self._stats.total_requests,
                "total_tokens": self._stats.total_tokens,
                "rate_limit_hits": self._stats.rate_limit_hits,
                "errors": self._stats.errors,
                "in_cooldown": time.time() < self._cooldown_until,
            }
    
    def get_remaining(self) -> Dict:
        """Get remaining capacity."""
        with self._lock:
            self._reset_if_needed()
            return {
                "requests_minute": max(0, self.config.requests_per_minute - self._stats.requests_this_minute),
                "requests_day": max(0, self.config.requests_per_day - self._stats.requests_today),
                "tokens_minute": max(0, self.config.tokens_per_minute - self._stats.tokens_this_minute),
                "tokens_day": max(0, self.config.tokens_per_day - self._stats.tokens_today),
            }
    
    def _seconds_until_midnight(self) -> float:
        """Calculate seconds until midnight."""
        now = datetime.now()
        midnight = datetime(now.year, now.month, now.day) + timedelta(days=1)
        return (midnight - now).total_seconds()


from datetime import timedelta


class MultiModelRateLimiter:
    """
    Rate limiter that tracks usage per model.
    
    Allows different limits for different models while
    sharing overall account limits.
    """
    
    def __init__(self, global_config: Optional[RateLimitConfig] = None):
        self._global = TokenBucketRateLimiter(global_config)
        self._per_model: Dict[str, TokenBucketRateLimiter] = {}
        self._lock = threading.Lock()
    
    def get_limiter(self, model: str) -> TokenBucketRateLimiter:
        """Get or create limiter for a specific model."""
        with self._lock:
            if model not in self._per_model:
                # Per-model limits are more generous (shared global limit)
                model_config = RateLimitConfig(
                    requests_per_minute=30,
                    requests_per_day=10000,
                    tokens_per_minute=10000,
                    tokens_per_day=1000000,
                )
                self._per_model[model] = TokenBucketRateLimiter(model_config)
            return self._per_model[model]
    
    def check_limit(self, model: str, estimated_tokens: int = 100) -> None:
        """Check both global and per-model limits."""
        self._global.check_limit(estimated_tokens)
        self.get_limiter(model).check_limit(estimated_tokens)
    
    def record_request(self, model: str, tokens_used: int, success: bool = True) -> None:
        """Record request for both global and per-model tracking."""
        self._global.record_request(tokens_used, success)
        self.get_limiter(model).record_request(tokens_used, success)
    
    def get_global_stats(self) -> Dict:
        """Get global usage statistics."""
        return self._global.get_stats()
    
    def get_model_stats(self, model: str) -> Dict:
        """Get per-model usage statistics."""
        return self.get_limiter(model).get_stats()
