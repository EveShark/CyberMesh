"""Base parser interface and common types."""

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from enum import Enum
from pathlib import Path
from typing import Any, Dict, List, Optional, Set


class FileType(str, Enum):
    """Supported file types."""
    PE = "pe"
    ELF = "elf"
    PDF = "pdf"
    OFFICE = "office"
    SCRIPT = "script"
    ANDROID = "android"
    PCAP = "pcap"
    ARCHIVE = "archive"
    UNKNOWN = "unknown"


@dataclass
class ParsedFile:
    """
    Parsed file representation.
    
    Contains extracted features and metadata from file parsing.
    """
    file_path: str
    file_name: str
    file_type: FileType
    file_size: int
    
    hashes: Dict[str, str] = field(default_factory=dict)
    entropy: float = 0.0
    
    strings: List[str] = field(default_factory=list)
    urls: List[str] = field(default_factory=list)
    ips: List[str] = field(default_factory=list)
    domains: List[str] = field(default_factory=list)
    
    imports: List[str] = field(default_factory=list)
    exports: List[str] = field(default_factory=list)
    sections: List[Dict[str, Any]] = field(default_factory=list)
    
    embedded_files: List[str] = field(default_factory=list)
    scripts: List[str] = field(default_factory=list)
    macros: List[str] = field(default_factory=list)
    permissions: List[str] = field(default_factory=list)
    
    metadata: Dict[str, Any] = field(default_factory=dict)
    raw_features: Dict[str, Any] = field(default_factory=dict)
    
    errors: List[str] = field(default_factory=list)
    
    def has_suspicious_strings(self) -> bool:
        """Check if file contains suspicious strings."""
        suspicious_patterns = [
            "cmd.exe", "powershell", "wget", "curl", "eval(",
            "base64", "exec(", "system(", "shell", "bypass",
            "invoke-", "downloadstring", "iex", "encodedcommand"
        ]
        all_strings = " ".join(self.strings).lower()
        return any(p in all_strings for p in suspicious_patterns)
    
    def has_network_indicators(self) -> bool:
        """Check if file has network indicators."""
        return bool(self.urls or self.ips or self.domains)
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary."""
        return {
            "file_path": self.file_path,
            "file_name": self.file_name,
            "file_type": self.file_type.value,
            "file_size": self.file_size,
            "hashes": self.hashes,
            "entropy": self.entropy,
            "strings_count": len(self.strings),
            "urls": self.urls,
            "ips": self.ips,
            "domains": self.domains,
            "imports_count": len(self.imports),
            "exports_count": len(self.exports),
            "sections_count": len(self.sections),
            "embedded_files_count": len(self.embedded_files),
            "scripts_count": len(self.scripts),
            "macros_count": len(self.macros),
            "metadata": self.metadata,
            "errors": self.errors,
        }


class Parser(ABC):
    """
    Base parser interface.
    
    All file type parsers must implement this interface.
    """
    
    @property
    @abstractmethod
    def name(self) -> str:
        """Parser name."""
        pass
    
    @property
    @abstractmethod
    def supported_extensions(self) -> Set[str]:
        """Set of supported file extensions (lowercase, with dot)."""
        pass
    
    @property
    @abstractmethod
    def supported_types(self) -> Set[FileType]:
        """Set of supported file types."""
        pass
    
    @abstractmethod
    def parse(self, file_path: str) -> ParsedFile:
        """
        Parse a file and extract features.
        
        Args:
            file_path: Path to file to parse
            
        Returns:
            ParsedFile with extracted features
            
        Raises:
            ParserError: If parsing fails
        """
        pass
    
    @abstractmethod
    def can_parse(self, file_path: str) -> bool:
        """
        Check if this parser can handle the file.
        
        Args:
            file_path: Path to file
            
        Returns:
            True if parser can handle this file
        """
        pass
