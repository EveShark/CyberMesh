import threading
from .errors import RateLimitError
from .time import now


class TokenBucket:
    def __init__(self, capacity: int, refill_rate: float):
        if capacity <= 0:
            raise ValueError("Capacity must be positive")
        if refill_rate <= 0:
            raise ValueError("Refill rate must be positive")
        
        self.capacity = capacity
        self.refill_rate = refill_rate
        self.tokens = float(capacity)
        self.last_refill = now()
        self._lock = threading.Lock()
    
    def _refill(self):
        current_time = now()
        elapsed = current_time - self.last_refill
        tokens_to_add = elapsed * self.refill_rate
        self.tokens = min(self.capacity, self.tokens + tokens_to_add)
        self.last_refill = current_time
    
    def consume(self, tokens: int = 1) -> bool:
        with self._lock:
            self._refill()
            if self.tokens >= tokens:
                self.tokens -= tokens
                return True
            return False
    
    def try_consume(self, tokens: int = 1):
        if not self.consume(tokens):
            raise RateLimitError("Rate limit exceeded")
    
    def available(self) -> int:
        with self._lock:
            self._refill()
            return int(self.tokens)


class MultiTierRateLimiter:
    def __init__(
        self,
        global_capacity: int,
        global_refill_rate: float,
        per_type_configs: dict
    ):
        self.global_limiter = TokenBucket(global_capacity, global_refill_rate)
        self.type_limiters = {
            msg_type: TokenBucket(config["capacity"], config["refill_rate"])
            for msg_type, config in per_type_configs.items()
        }
    
    def consume(self, message_type: str, tokens: int = 1):
        if not self.global_limiter.consume(tokens):
            raise RateLimitError("Global rate limit exceeded")
        
        if message_type in self.type_limiters:
            if not self.type_limiters[message_type].consume(tokens):
                self.global_limiter.tokens += tokens
                raise RateLimitError(f"Rate limit exceeded for message type: {message_type}")
    
    def available_global(self) -> int:
        return self.global_limiter.available()
    
    def available_for_type(self, message_type: str) -> int:
        if message_type in self.type_limiters:
            return self.type_limiters[message_type].available()
        return 0
