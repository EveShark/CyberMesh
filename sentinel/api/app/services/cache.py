"""In-memory cache for scan results."""

from typing import Optional, Dict, Any
from datetime import datetime, timedelta
from collections import OrderedDict
import threading


class ScanCache:
    """Simple in-memory LRU cache for scan results."""
    
    def __init__(self, max_size: int = 1000, ttl_seconds: int = 3600):
        """
        Initialize cache.
        
        Args:
            max_size: Maximum number of entries
            ttl_seconds: Time to live for entries
        """
        self.max_size = max_size
        self.ttl_seconds = ttl_seconds
        self._cache: OrderedDict[str, Dict[str, Any]] = OrderedDict()
        self._lock = threading.Lock()
    
    def get(self, key: str) -> Optional[Dict[str, Any]]:
        """
        Get cached value by key (file hash).
        
        Args:
            key: Cache key (usually file hash)
            
        Returns:
            Cached scan result or None
        """
        with self._lock:
            if key not in self._cache:
                return None
            
            entry = self._cache[key]
            
            # Check if expired
            if datetime.utcnow() > entry["expires_at"]:
                del self._cache[key]
                return None
            
            # Move to end (LRU)
            self._cache.move_to_end(key)
            
            return entry["data"]
    
    def set(self, key: str, value: Dict[str, Any]) -> None:
        """
        Set cache entry.
        
        Args:
            key: Cache key (usually file hash)
            value: Scan result to cache
        """
        with self._lock:
            # Remove oldest if at capacity
            while len(self._cache) >= self.max_size:
                self._cache.popitem(last=False)
            
            self._cache[key] = {
                "data": value,
                "created_at": datetime.utcnow(),
                "expires_at": datetime.utcnow() + timedelta(seconds=self.ttl_seconds),
            }
    
    def delete(self, key: str) -> bool:
        """Delete cache entry."""
        with self._lock:
            if key in self._cache:
                del self._cache[key]
                return True
            return False
    
    def clear(self) -> None:
        """Clear all cache entries."""
        with self._lock:
            self._cache.clear()
    
    def stats(self) -> Dict[str, Any]:
        """Get cache statistics."""
        with self._lock:
            now = datetime.utcnow()
            expired = sum(
                1 for entry in self._cache.values()
                if now > entry["expires_at"]
            )
            
            return {
                "size": len(self._cache),
                "max_size": self.max_size,
                "expired_entries": expired,
                "ttl_seconds": self.ttl_seconds,
            }


# Global cache instance
scan_cache = ScanCache()
