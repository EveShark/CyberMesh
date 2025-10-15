import secrets
import struct
import threading
from typing import Set
from .errors import NonceError
from .time import now, now_ms
from .limits import TIME_LIMITS


class NonceManager:
    """
    Generate cryptographically secure 16-byte nonces.
    
    Format (16 bytes total):
    - 8 bytes: timestamp_ms (big-endian uint64)
    - 4 bytes: instance_id (big-endian uint32)
    - 4 bytes: random (cryptographically random uint32)
    
    Backend expects exactly 16 bytes binary (state.NonceSize = 16).
    """
    
    NONCE_SIZE = 16
    
    def __init__(self, instance_id: int, ttl_seconds: int = TIME_LIMITS.NONCE_TTL_SECONDS):
        if instance_id < 0 or instance_id > 0xFFFFFFFF:
            raise NonceError(f"instance_id must be 32-bit unsigned integer, got: {instance_id}")
        
        self.instance_id = instance_id
        self.ttl_seconds = ttl_seconds
        self._used_nonces: Set[bytes] = set()
        self._lock = threading.Lock()
        self._last_cleanup = now()
    
    def generate(self) -> bytes:
        """Generate a unique 16-byte nonce."""
        timestamp_ms = now_ms()
        random_value = secrets.randbits(32)
        
        # Pack: 8 bytes timestamp + 4 bytes instance_id + 4 bytes random
        nonce_bytes = struct.pack('>QII', timestamp_ms, self.instance_id, random_value)
        
        if len(nonce_bytes) != self.NONCE_SIZE:
            raise NonceError(f"Internal error: nonce size mismatch {len(nonce_bytes)} != {self.NONCE_SIZE}")
        
        with self._lock:
            if nonce_bytes in self._used_nonces:
                raise NonceError("Nonce collision detected - this should be statistically impossible")
            
            self._used_nonces.add(nonce_bytes)
            self._maybe_cleanup()
        
        return nonce_bytes
    
    def validate(self, nonce_bytes: bytes) -> bool:
        """Validate nonce: check size, age, and replay protection."""
        if len(nonce_bytes) != self.NONCE_SIZE:
            return False
        
        try:
            timestamp_ms, instance_id, random_value = struct.unpack('>QII', nonce_bytes)
        except struct.error:
            return False
        
        current_time_ms = now_ms()
        age_seconds = (current_time_ms - timestamp_ms) / 1000.0
        
        if age_seconds < 0 or age_seconds > self.ttl_seconds:
            return False
        
        with self._lock:
            if nonce_bytes in self._used_nonces:
                return False
            
            self._used_nonces.add(nonce_bytes)
            self._maybe_cleanup()
        
        return True
    
    def _maybe_cleanup(self):
        """Remove expired nonces from replay protection set."""
        current_time = now()
        if current_time - self._last_cleanup < TIME_LIMITS.NONCE_CLEANUP_INTERVAL_SECONDS:
            return
        
        current_time_ms = now_ms()
        cutoff_ms = current_time_ms - (self.ttl_seconds * 1000)
        
        nonces_to_remove = set()
        for nonce_bytes in self._used_nonces:
            try:
                timestamp_ms = struct.unpack('>Q', nonce_bytes[:8])[0]
                if timestamp_ms < cutoff_ms:
                    nonces_to_remove.add(nonce_bytes)
            except struct.error:
                nonces_to_remove.add(nonce_bytes)
        
        self._used_nonces -= nonces_to_remove
        self._last_cleanup = current_time
