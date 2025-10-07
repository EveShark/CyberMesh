import time
from typing import Protocol


class Clock(Protocol):
    """Protocol for time providers."""
    
    def now(self) -> float:
        """Return current Unix timestamp in seconds."""
        ...
    
    def now_ms(self) -> int:
        """Return current Unix timestamp in milliseconds."""
        ...


class SystemClock:
    """Production clock using system time."""
    
    def now(self) -> float:
        return time.time()
    
    def now_ms(self) -> int:
        return int(time.time() * 1000)


class FixedClock:
    """Fixed clock for deterministic testing."""
    
    def __init__(self, timestamp: float):
        self._timestamp = timestamp
    
    def now(self) -> float:
        return self._timestamp
    
    def now_ms(self) -> int:
        return int(self._timestamp * 1000)
    
    def set(self, timestamp: float):
        self._timestamp = timestamp
    
    def advance(self, seconds: float):
        self._timestamp += seconds


_default_clock: Clock = SystemClock()


def set_clock(clock: Clock):
    global _default_clock
    _default_clock = clock


def get_clock() -> Clock:
    return _default_clock


def now() -> float:
    return _default_clock.now()


def now_ms() -> int:
    return _default_clock.now_ms()
