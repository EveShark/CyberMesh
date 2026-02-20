"""Token bucket rate limiting."""

from typing import Optional, Dict
from datetime import datetime, timedelta
from fastapi import Request
from slowapi import Limiter
from slowapi.util import get_remote_address

from ..config import get_settings


def get_user_identifier(request: Request) -> str:
    """Get rate limit key from user ID or IP address."""
    # Try to get user ID from request state (set by auth middleware)
    user_id = getattr(request.state, "user_id", None)
    if user_id:
        return f"user:{user_id}"
    
    # Fall back to IP address
    return f"ip:{get_remote_address(request)}"


# Initialize rate limiter
settings = get_settings()
limiter = Limiter(
    key_func=get_user_identifier,
    default_limits=[f"{settings.rate_limit_per_minute}/minute"],
    enabled=settings.enable_rate_limit,
)


class InMemoryRateLimiter:
    """Simple in-memory token bucket rate limiter."""
    
    def __init__(
        self,
        requests_per_minute: int = 10,
        requests_per_hour: int = 100,
    ):
        self.requests_per_minute = requests_per_minute
        self.requests_per_hour = requests_per_hour
        self._buckets: Dict[str, Dict] = {}
    
    def _get_bucket(self, key: str) -> Dict:
        """Get or create bucket for key."""
        now = datetime.utcnow()
        
        if key not in self._buckets:
            self._buckets[key] = {
                "minute_tokens": self.requests_per_minute,
                "minute_reset": now + timedelta(minutes=1),
                "hour_tokens": self.requests_per_hour,
                "hour_reset": now + timedelta(hours=1),
            }
        
        bucket = self._buckets[key]
        
        # Reset minute bucket if needed
        if now >= bucket["minute_reset"]:
            bucket["minute_tokens"] = self.requests_per_minute
            bucket["minute_reset"] = now + timedelta(minutes=1)
        
        # Reset hour bucket if needed
        if now >= bucket["hour_reset"]:
            bucket["hour_tokens"] = self.requests_per_hour
            bucket["hour_reset"] = now + timedelta(hours=1)
        
        return bucket
    
    def is_allowed(self, key: str) -> tuple[bool, Dict]:
        """
        Check if request is allowed.
        
        Returns:
            Tuple of (allowed, rate_limit_info)
        """
        bucket = self._get_bucket(key)
        
        info = {
            "limit": self.requests_per_minute,
            "remaining": max(0, bucket["minute_tokens"] - 1),
            "reset": int(bucket["minute_reset"].timestamp()),
        }
        
        # Check minute limit
        if bucket["minute_tokens"] <= 0:
            retry_after = int((bucket["minute_reset"] - datetime.utcnow()).total_seconds())
            info["retry_after"] = max(1, retry_after)
            return False, info
        
        # Check hour limit
        if bucket["hour_tokens"] <= 0:
            retry_after = int((bucket["hour_reset"] - datetime.utcnow()).total_seconds())
            info["retry_after"] = max(1, retry_after)
            return False, info
        
        # Consume token
        bucket["minute_tokens"] -= 1
        bucket["hour_tokens"] -= 1
        
        return True, info
    
    def get_headers(self, info: Dict) -> Dict[str, str]:
        """Get rate limit headers to include in response."""
        headers = {
            "X-RateLimit-Limit": str(info["limit"]),
            "X-RateLimit-Remaining": str(info["remaining"]),
            "X-RateLimit-Reset": str(info["reset"]),
        }
        
        if "retry_after" in info:
            headers["Retry-After"] = str(info["retry_after"])
        
        return headers


# Global rate limiter instance
rate_limiter = InMemoryRateLimiter(
    requests_per_minute=settings.rate_limit_per_minute,
    requests_per_hour=settings.rate_limit_per_hour,
)
