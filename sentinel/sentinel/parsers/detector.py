"""File type detection and parser selection."""

import struct
from pathlib import Path
from typing import Optional, Dict, Type

from .base import Parser, ParsedFile, FileType
from ..utils.errors import ParserError
from ..logging import get_logger
from .unsupported_parser import UnsupportedParser

logger = get_logger(__name__)


MAGIC_SIGNATURES: Dict[bytes, FileType] = {
    b"MZ": FileType.PE,
    b"\x7fELF": FileType.ELF,
    b"%PDF": FileType.PDF,
    b"PK\x03\x04": FileType.OFFICE,
    b"PK\x05\x06": FileType.OFFICE,
    b"\xd0\xcf\x11\xe0": FileType.OFFICE,
    b"\xca\xfe\xba\xbe": FileType.ANDROID,
    b"dex\n": FileType.ANDROID,
    b"\xa1\xb2\xc3\xd4": FileType.PCAP,
    b"\xd4\xc3\xb2\xa1": FileType.PCAP,
    b"\x0a\x0d\x0d\x0a": FileType.PCAP,
}


EXTENSION_MAP: Dict[str, FileType] = {
    ".exe": FileType.PE,
    ".dll": FileType.PE,
    ".sys": FileType.PE,
    ".scr": FileType.PE,
    ".pdf": FileType.PDF,
    ".docx": FileType.OFFICE,
    ".xlsx": FileType.OFFICE,
    ".pptx": FileType.OFFICE,
    ".doc": FileType.OFFICE,
    ".xls": FileType.OFFICE,
    ".ppt": FileType.OFFICE,
    ".ps1": FileType.SCRIPT,
    ".psm1": FileType.SCRIPT,
    ".psd1": FileType.SCRIPT,
    ".js": FileType.SCRIPT,
    ".jse": FileType.SCRIPT,
    ".vbs": FileType.SCRIPT,
    ".vbe": FileType.SCRIPT,
    ".wsf": FileType.SCRIPT,
    ".bat": FileType.SCRIPT,
    ".cmd": FileType.SCRIPT,
    ".sh": FileType.SCRIPT,
    ".py": FileType.SCRIPT,
    ".apk": FileType.ANDROID,
    ".dex": FileType.ANDROID,
    ".pcap": FileType.PCAP,
    ".pcapng": FileType.PCAP,
    ".cap": FileType.PCAP,
    ".zip": FileType.ARCHIVE,
    ".rar": FileType.ARCHIVE,
    ".7z": FileType.ARCHIVE,
    ".tar": FileType.ARCHIVE,
    ".gz": FileType.ARCHIVE,
    ".elf": FileType.ELF,
}


def detect_file_type(file_path: str) -> FileType:
    """
    Detect file type using magic bytes and extension.
    
    Args:
        file_path: Path to file
        
    Returns:
        Detected FileType
    """
    path = Path(file_path)
    
    try:
        with open(file_path, "rb") as f:
            header = f.read(16)
        
        for magic, file_type in MAGIC_SIGNATURES.items():
            if header.startswith(magic):
                logger.debug(f"Detected {file_type.value} via magic bytes: {file_path}")
                return file_type
        
        if header.startswith(b"PK"):
            if _is_android_apk(file_path):
                return FileType.ANDROID
            return FileType.OFFICE
        
    except IOError as e:
        logger.warning(f"Could not read file header: {e}")
    
    ext = path.suffix.lower()
    if ext in EXTENSION_MAP:
        logger.debug(f"Detected {EXTENSION_MAP[ext].value} via extension: {file_path}")
        return EXTENSION_MAP[ext]
    
    return FileType.UNKNOWN


def _is_android_apk(file_path: str) -> bool:
    """Check if ZIP file is an Android APK."""
    try:
        import zipfile
        with zipfile.ZipFile(file_path, 'r') as zf:
            names = zf.namelist()
            return "AndroidManifest.xml" in names or "classes.dex" in names
    except Exception:
        return False


_PARSER_REGISTRY: Dict[FileType, Type[Parser]] = {}


def register_parser(file_type: FileType, parser_class: Type[Parser]) -> None:
    """Register a parser for a file type."""
    _PARSER_REGISTRY[file_type] = parser_class


def get_parser_for_file(file_path: str) -> Optional[Parser]:
    """
    Get appropriate parser for a file.
    
    Args:
        file_path: Path to file
        
    Returns:
        Parser instance or None if no parser available
    """
    file_type = detect_file_type(file_path)
    
    if file_type in _PARSER_REGISTRY:
        parser_class = _PARSER_REGISTRY[file_type]
        return parser_class()

    # v1 behavior: explicitly-detected but unsupported types get a safe parser
    # that returns a ParsedFile with a stable error code (no crashes/no ambiguity).
    if file_type in (FileType.ELF,):
        return UnsupportedParser(file_type)
    
    logger.warning(f"No parser registered for file type: {file_type.value}")
    return None


def get_parser_by_type(file_type: FileType) -> Optional[Parser]:
    """Get parser by file type."""
    if file_type in _PARSER_REGISTRY:
        return _PARSER_REGISTRY[file_type]()
    return None
