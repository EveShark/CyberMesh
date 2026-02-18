"""Base provider interface and result types."""

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from enum import Enum
from typing import Any, Dict, List, Optional, Set

from ..parsers.base import ParsedFile, FileType


class ThreatLevel(str, Enum):
    """Threat assessment levels."""
    CLEAN = "clean"
    SUSPICIOUS = "suspicious"
    MALICIOUS = "malicious"
    CRITICAL = "critical"
    UNKNOWN = "unknown"


@dataclass
class Indicator:
    """Indicator of Compromise (IOC)."""
    type: str
    value: str
    context: str = ""
    confidence: float = 1.0


@dataclass
class AnalysisResult:
    """
    Result from a provider analysis.
    
    Standardized output format for all providers.
    """
    provider_name: str
    provider_version: str
    
    threat_level: ThreatLevel
    score: float
    confidence: float
    
    findings: List[str] = field(default_factory=list)
    indicators: List[Indicator] = field(default_factory=list)
    
    latency_ms: float = 0.0
    error: Optional[str] = None
    
    metadata: Dict[str, Any] = field(default_factory=dict)
    
    def __post_init__(self):
        if not (0.0 <= self.score <= 1.0):
            raise ValueError(f"score must be in [0,1], got {self.score}")
        if not (0.0 <= self.confidence <= 1.0):
            raise ValueError(f"confidence must be in [0,1], got {self.confidence}")
    
    def is_threat(self) -> bool:
        """Check if result indicates a threat."""
        return self.threat_level in (ThreatLevel.MALICIOUS, ThreatLevel.CRITICAL)
    
    def is_suspicious(self) -> bool:
        """Check if result indicates suspicious activity."""
        return self.threat_level == ThreatLevel.SUSPICIOUS
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary."""
        return {
            "provider_name": self.provider_name,
            "provider_version": self.provider_version,
            "threat_level": self.threat_level.value,
            "score": self.score,
            "confidence": self.confidence,
            "findings": self.findings,
            "indicators": [
                {"type": i.type, "value": i.value, "context": i.context}
                for i in self.indicators
            ],
            "latency_ms": self.latency_ms,
            "error": self.error,
            "metadata": self.metadata,
        }


class Provider(ABC):
    """
    Base interface for all detection providers.
    
    Providers analyze parsed files and return threat assessments.
    Each provider must implement the analyze method and declare
    its capabilities.
    """
    
    @property
    @abstractmethod
    def name(self) -> str:
        """Unique provider identifier."""
        pass
    
    @property
    @abstractmethod
    def version(self) -> str:
        """Provider version string."""
        pass
    
    @property
    @abstractmethod
    def supported_types(self) -> Set[FileType]:
        """Set of file types this provider can analyze."""
        pass
    
    @abstractmethod
    def analyze(self, parsed_file: ParsedFile) -> AnalysisResult:
        """
        Analyze a parsed file.
        
        Args:
            parsed_file: Parsed file data
            
        Returns:
            AnalysisResult with threat assessment
        """
        pass
    
    @abstractmethod
    def get_cost_per_call(self) -> float:
        """
        Get estimated cost per analysis call (USD).
        
        Returns:
            Cost in USD (0.0 for local/free providers)
        """
        pass
    
    @abstractmethod
    def get_avg_latency_ms(self) -> float:
        """
        Get average analysis latency in milliseconds.
        
        Returns:
            Average latency in ms
        """
        pass
    
    def can_analyze(self, parsed_file: ParsedFile) -> bool:
        """Check if this provider can analyze the given file."""
        return parsed_file.file_type in self.supported_types
    
    def get_metadata(self) -> Dict[str, Any]:
        """Get provider metadata."""
        return {
            "name": self.name,
            "version": self.version,
            "supported_types": [t.value for t in self.supported_types],
            "cost_per_call": self.get_cost_per_call(),
            "avg_latency_ms": self.get_avg_latency_ms(),
        }
