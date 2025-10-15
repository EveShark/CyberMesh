import threading
from enum import Enum
from typing import Callable, TypeVar, Any
from .errors import CircuitBreakerError
from .time import now


T = TypeVar('T')


class CircuitBreakerState(Enum):
    CLOSED = "closed"
    OPEN = "open"
    HALF_OPEN = "half_open"


class CircuitBreaker:
    def __init__(
        self,
        failure_threshold: int = 5,
        timeout_seconds: int = 30,
        recovery_threshold: int = 2
    ):
        if failure_threshold <= 0:
            raise ValueError("Failure threshold must be positive")
        if timeout_seconds <= 0:
            raise ValueError("Timeout must be positive")
        if recovery_threshold <= 0:
            raise ValueError("Recovery threshold must be positive")
        
        self.failure_threshold = failure_threshold
        self.timeout_seconds = timeout_seconds
        self.recovery_threshold = recovery_threshold
        
        self._state = CircuitBreakerState.CLOSED
        self._failure_count = 0
        self._success_count = 0
        self._last_failure_time = 0.0
        self._lock = threading.Lock()
    
    @property
    def state(self) -> CircuitBreakerState:
        with self._lock:
            return self._state
    
    def _should_attempt_reset(self) -> bool:
        return (
            self._state == CircuitBreakerState.OPEN and
            now() - self._last_failure_time >= self.timeout_seconds
        )
    
    def call(self, func: Callable[..., T], *args: Any, **kwargs: Any) -> T:
        with self._lock:
            if self._should_attempt_reset():
                self._state = CircuitBreakerState.HALF_OPEN
                self._success_count = 0
            
            if self._state == CircuitBreakerState.OPEN:
                raise CircuitBreakerError("Circuit breaker is OPEN")
        
        try:
            result = func(*args, **kwargs)
            self._on_success()
            return result
        except Exception as e:
            self._on_failure()
            raise e
    
    def _on_success(self):
        with self._lock:
            self._failure_count = 0
            
            if self._state == CircuitBreakerState.HALF_OPEN:
                self._success_count += 1
                if self._success_count >= self.recovery_threshold:
                    self._state = CircuitBreakerState.CLOSED
                    self._success_count = 0
    
    def _on_failure(self):
        with self._lock:
            self._failure_count += 1
            self._last_failure_time = now()
            
            if self._state == CircuitBreakerState.HALF_OPEN:
                self._state = CircuitBreakerState.OPEN
                self._success_count = 0
            
            elif self._failure_count >= self.failure_threshold:
                self._state = CircuitBreakerState.OPEN
    
    def reset(self):
        with self._lock:
            self._state = CircuitBreakerState.CLOSED
            self._failure_count = 0
            self._success_count = 0
            self._last_failure_time = 0.0
