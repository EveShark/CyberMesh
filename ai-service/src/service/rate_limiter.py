"""
Token bucket rate limiter for detection publishing.

Prevents spamming backend with excessive detections.
"""

import threading
import time
from typing import Optional


class RateLimiter:
    """
    Token bucket rate limiter.
    
    Algorithm:
    - Bucket holds max_per_second tokens
    - Tokens refill at max_per_second rate
    - Each detection consumes 1 token
    - If no tokens available, detection is rate limited
    
    Thread Safety:
    - Safe to call from multiple threads
    - Uses lock to protect token bucket state
    
    Example:
        limiter = RateLimiter(max_per_second=100)
        
        for anomaly in anomalies:
            if limiter.acquire():
                publish(anomaly)
            else:
                log("Rate limited")
    """
    
    def __init__(self, max_per_second: int = 100):
        """
        Initialize rate limiter.
        
        Args:
            max_per_second: Maximum operations per second (default: 100)
        """
        self.max_per_second = max_per_second
        self._tokens = float(max_per_second)
        self._last_refill = time.time()
        self._lock = threading.Lock()
        
        # Statistics
        self._total_requests = 0
        self._total_allowed = 0
        self._total_rate_limited = 0
    
    def acquire(self, tokens: float = 1.0) -> bool:
        """
        Try to acquire tokens.
        
        Args:
            tokens: Number of tokens to acquire (default: 1.0)
        
        Returns:
            True if acquired (operation allowed)
            False if rate limited (operation denied)
        
        Thread Safety:
            - Safe to call from multiple threads
            - Uses lock to protect token bucket
        """
        with self._lock:
            now = time.time()
            elapsed = now - self._last_refill
            
            # Refill tokens based on elapsed time
            tokens_to_add = elapsed * self.max_per_second
            self._tokens = min(
                self.max_per_second,
                self._tokens + tokens_to_add
            )
            self._last_refill = now
            
            # Update statistics
            self._total_requests += 1
            
            # Check if we have enough tokens
            if self._tokens >= tokens:
                self._tokens -= tokens
                self._total_allowed += 1
                return True
            else:
                self._total_rate_limited += 1
                return False
    
    def reset(self) -> None:
        """
        Reset rate limiter (refill bucket to max).
        
        Thread Safety:
            - Safe to call from any thread
        """
        with self._lock:
            self._tokens = float(self.max_per_second)
            self._last_refill = time.time()
    
    def get_stats(self) -> dict:
        """
        Get rate limiter statistics.
        
        Returns:
            Dictionary with:
            - total_requests: Total acquire attempts
            - total_allowed: Total allowed operations
            - total_rate_limited: Total denied operations
            - current_tokens: Current token count
            - max_per_second: Configured rate limit
            - rate_limited_percentage: % of requests rate limited
        
        Thread Safety:
            - Returns snapshot of stats (safe to read)
        """
        with self._lock:
            rate_limited_pct = 0.0
            if self._total_requests > 0:
                rate_limited_pct = (
                    self._total_rate_limited / self._total_requests * 100
                )
            
            return {
                "total_requests": self._total_requests,
                "total_allowed": self._total_allowed,
                "total_rate_limited": self._total_rate_limited,
                "current_tokens": self._tokens,
                "max_per_second": self.max_per_second,
                "rate_limited_percentage": rate_limited_pct
            }
    
    def set_rate(self, max_per_second: int) -> None:
        """
        Update rate limit (dynamic adjustment).
        
        Args:
            max_per_second: New rate limit
        
        Thread Safety:
            - Safe to call from any thread
        """
        with self._lock:
            old_rate = self.max_per_second
            self.max_per_second = max_per_second
            
            # Adjust current tokens proportionally
            if old_rate > 0:
                ratio = max_per_second / old_rate
                self._tokens = min(max_per_second, self._tokens * ratio)
