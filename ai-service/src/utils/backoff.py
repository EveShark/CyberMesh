import random
from typing import Optional
from .time import now
from .errors import BackoffError


class ExponentialBackoff:
    """Exponential backoff with jitter for retry operations."""
    
    def __init__(
        self,
        base_delay: float = 1.0,
        max_delay: float = 60.0,
        multiplier: float = 2.0,
        jitter: bool = True,
        max_attempts: Optional[int] = None
    ):
        if base_delay <= 0:
            raise BackoffError("base_delay must be positive")
        if max_delay < base_delay:
            raise BackoffError("max_delay must be >= base_delay")
        if multiplier <= 1.0:
            raise BackoffError("multiplier must be > 1.0")
        if max_attempts is not None and max_attempts <= 0:
            raise BackoffError("max_attempts must be positive")
        
        self.base_delay = base_delay
        self.max_delay = max_delay
        self.multiplier = multiplier
        self.jitter = jitter
        self.max_attempts = max_attempts
        
        self._attempt = 0
        self._last_attempt_time = 0.0
    
    def next_delay(self) -> float:
        """Calculate next backoff delay in seconds."""
        if self.max_attempts and self._attempt >= self.max_attempts:
            raise BackoffError(f"Maximum attempts ({self.max_attempts}) exceeded")
        
        delay = min(
            self.base_delay * (self.multiplier ** self._attempt),
            self.max_delay
        )
        
        if self.jitter:
            delay = delay * (0.5 + random.random() * 0.5)
        
        self._attempt += 1
        self._last_attempt_time = now()
        
        return delay
    
    def reset(self):
        """Reset backoff state."""
        self._attempt = 0
        self._last_attempt_time = 0.0
    
    @property
    def attempt_count(self) -> int:
        return self._attempt
    
    @property
    def can_retry(self) -> bool:
        if self.max_attempts is None:
            return True
        return self._attempt < self.max_attempts


class FixedBackoff:
    """Fixed delay backoff for testing or simple retry logic."""
    
    def __init__(self, delay: float, max_attempts: Optional[int] = None):
        if delay <= 0:
            raise BackoffError("delay must be positive")
        
        self.delay = delay
        self.max_attempts = max_attempts
        self._attempt = 0
    
    def next_delay(self) -> float:
        if self.max_attempts and self._attempt >= self.max_attempts:
            raise BackoffError(f"Maximum attempts ({self.max_attempts}) exceeded")
        
        self._attempt += 1
        return self.delay
    
    def reset(self):
        self._attempt = 0
    
    @property
    def attempt_count(self) -> int:
        return self._attempt
    
    @property
    def can_retry(self) -> bool:
        if self.max_attempts is None:
            return True
        return self._attempt < self.max_attempts
