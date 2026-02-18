"""Unique ID generation utilities."""

import time
import uuid
import hashlib
from typing import Optional


def generate_analysis_id(file_hash: Optional[str] = None) -> str:
    """
    Generate unique analysis ID.
    
    Format: sentinel_{timestamp}_{random}
    
    Args:
        file_hash: Optional file hash to include
        
    Returns:
        Unique analysis ID string
    """
    timestamp = int(time.time() * 1000)
    random_part = uuid.uuid4().hex[:8]
    
    if file_hash:
        return f"sentinel_{timestamp}_{file_hash[:8]}_{random_part}"
    return f"sentinel_{timestamp}_{random_part}"


def generate_provider_id(provider_name: str, version: str) -> str:
    """
    Generate deterministic provider ID.
    
    Args:
        provider_name: Provider name
        version: Provider version
        
    Returns:
        Provider ID string
    """
    content = f"{provider_name}:{version}"
    hash_part = hashlib.sha256(content.encode()).hexdigest()[:12]
    return f"{provider_name}_{hash_part}"


def generate_report_id() -> str:
    """Generate unique report ID."""
    return f"report_{uuid.uuid4().hex}"
