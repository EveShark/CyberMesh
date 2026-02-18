"""Hashing utilities for file and content verification."""

import hashlib
from pathlib import Path
from typing import Union


def compute_file_hash(file_path: Union[str, Path], algorithm: str = "sha256") -> str:
    """
    Compute hash of a file.
    
    Args:
        file_path: Path to file
        algorithm: Hash algorithm (sha256, sha1, md5)
        
    Returns:
        Hex-encoded hash string
    """
    hasher = hashlib.new(algorithm)
    
    with open(file_path, "rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            hasher.update(chunk)
    
    return hasher.hexdigest()


def compute_content_hash(content: bytes, algorithm: str = "sha256") -> str:
    """
    Compute hash of bytes content.
    
    Args:
        content: Bytes to hash
        algorithm: Hash algorithm (sha256, sha1, md5)
        
    Returns:
        Hex-encoded hash string
    """
    hasher = hashlib.new(algorithm)
    hasher.update(content)
    return hasher.hexdigest()


def compute_sha256(file_path: Union[str, Path]) -> str:
    """Convenience function for SHA256 hash."""
    return compute_file_hash(file_path, "sha256")


def compute_multi_hash(file_path: Union[str, Path]) -> dict:
    """
    Compute multiple hashes for a file.
    
    Args:
        file_path: Path to file
        
    Returns:
        Dictionary with md5, sha1, sha256 hashes
    """
    md5 = hashlib.md5()
    sha1 = hashlib.sha1()
    sha256 = hashlib.sha256()
    
    with open(file_path, "rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            md5.update(chunk)
            sha1.update(chunk)
            sha256.update(chunk)
    
    return {
        "md5": md5.hexdigest(),
        "sha1": sha1.hexdigest(),
        "sha256": sha256.hexdigest(),
    }
