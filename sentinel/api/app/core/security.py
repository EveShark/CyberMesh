"""File validation and security utilities."""

import re
import uuid
import hashlib
from pathlib import Path
from typing import Tuple, Optional, BinaryIO

# Magic bytes signatures for common file types
MAGIC_SIGNATURES = {
    b"MZ": "PE",
    b"\x7fELF": "ELF",
    b"PK\x03\x04": "ZIP",
    b"PK\x05\x06": "ZIP",
    b"%PDF": "PDF",
    b"\xd0\xcf\x11\xe0": "OLE",  # Office docs
    b"\x50\x4b\x03\x04\x14\x00\x06\x00": "OOXML",  # Modern Office
    b"Rar!\x1a\x07": "RAR",
    b"\x1f\x8b": "GZIP",
    b"BZ": "BZIP2",
    b"\xca\xfe\xba\xbe": "Mach-O",
    b"dex\n": "DEX",
    b"\x00\x00\x00": "Unknown",
}

# Allowed file extensions
ALLOWED_EXTENSIONS = {
    ".exe", ".dll", ".sys", ".scr",  # PE
    ".pdf",  # PDF
    ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",  # Office
    ".ps1", ".psm1", ".bat", ".cmd", ".vbs", ".js", ".wsf",  # Scripts
    ".sh", ".py", ".rb", ".pl",  # Unix scripts
    ".apk", ".dex",  # Android
    ".elf", ".so",  # Linux
    ".zip", ".rar", ".7z", ".tar", ".gz",  # Archives
    ".jar", ".class",  # Java
    ".msi", ".cab",  # Windows installers
}


def sanitize_filename(filename: str) -> str:
    """
    Sanitize filename to prevent path traversal and other attacks.
    
    Args:
        filename: Original filename
        
    Returns:
        Safe filename with UUID prefix
    """
    if not filename:
        return f"{uuid.uuid4().hex[:8]}_unnamed"
    
    # Remove path components
    filename = Path(filename).name
    
    # Remove dangerous characters
    filename = re.sub(r'[<>:"/\\|?*\x00-\x1f]', '', filename)
    
    # Remove leading/trailing dots and spaces
    filename = filename.strip('. ')
    
    # Limit length
    if len(filename) > 200:
        name, ext = split_extension(filename)
        filename = name[:200-len(ext)] + ext
    
    # Add UUID prefix for uniqueness
    return f"{uuid.uuid4().hex[:8]}_{filename}"


def split_extension(filename: str) -> Tuple[str, str]:
    """Split filename into name and extension."""
    path = Path(filename)
    return path.stem, path.suffix.lower()


def get_magic_type(data: bytes) -> str:
    """
    Detect file type from magic bytes.
    
    Args:
        data: First 16+ bytes of file
        
    Returns:
        Detected file type string
    """
    if len(data) < 2:
        return "Unknown"
    
    # Check each signature
    for magic, file_type in MAGIC_SIGNATURES.items():
        if data.startswith(magic):
            return file_type
    
    # Check for text/script files
    try:
        # Try to decode as UTF-8
        text = data[:100].decode('utf-8', errors='ignore')
        
        # Check for script indicators
        if text.startswith('#!'):
            return "Script"
        if text.strip().startswith('<?'):
            return "PHP"
        if '<html' in text.lower() or '<!doctype' in text.lower():
            return "HTML"
        if text.strip().startswith('{') or text.strip().startswith('['):
            return "JSON"
        
        # Check for PowerShell
        ps_keywords = ['param', 'function', '$', 'Write-', 'Get-', 'Set-']
        if any(kw in text for kw in ps_keywords):
            return "PowerShell"
            
        # Check for batch
        batch_keywords = ['@echo', 'echo off', 'set ', 'goto ', 'if ']
        if any(kw.lower() in text.lower() for kw in batch_keywords):
            return "Batch"
            
    except:
        pass
    
    return "Unknown"


def validate_file(
    filename: str,
    file_size: int,
    file_header: bytes,
    max_size_bytes: int,
) -> Tuple[bool, Optional[str]]:
    """
    Validate uploaded file.
    
    Args:
        filename: Original filename
        file_size: Size in bytes
        file_header: First 16+ bytes for magic detection
        max_size_bytes: Maximum allowed size
        
    Returns:
        Tuple of (is_valid, error_message)
    """
    # Check size
    if file_size > max_size_bytes:
        return False, f"File size {file_size} exceeds maximum {max_size_bytes}"
    
    if file_size == 0:
        return False, "File is empty"
    
    # Check extension
    _, ext = split_extension(filename)
    if ext and ext not in ALLOWED_EXTENSIONS:
        # Allow files without extension (will be detected by magic)
        if ext:
            return False, f"File extension '{ext}' is not allowed"
    
    # Detect type from magic bytes
    detected_type = get_magic_type(file_header)
    
    # All types are allowed for scanning, but we note the type
    return True, None


def compute_file_hash(file_path: str) -> str:
    """Compute SHA256 hash of a file."""
    sha256 = hashlib.sha256()
    
    with open(file_path, 'rb') as f:
        for chunk in iter(lambda: f.read(8192), b''):
            sha256.update(chunk)
    
    return sha256.hexdigest()


def compute_hash_from_bytes(data: bytes) -> str:
    """Compute SHA256 hash from bytes."""
    return hashlib.sha256(data).hexdigest()
