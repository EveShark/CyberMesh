"""Base LLM provider with common functionality."""

import json
import re
from abc import abstractmethod
from dataclasses import dataclass, field
from typing import Dict, Any, Optional, List

from ..base import Provider, AnalysisResult, ThreatLevel
from ...parsers.base import ParsedFile
from ...logging import get_logger
from ...utils.sanitizer import InputSanitizer, SanitizerConfig

logger = get_logger(__name__)


@dataclass
class LLMResponse:
    """Response from an LLM provider."""
    content: str
    model: str
    tokens_used: int
    latency_ms: float
    error: Optional[str] = None
    metadata: Dict[str, Any] = field(default_factory=dict)


ANALYSIS_PROMPT = '''You are a malware analyst. Analyze the following file information and determine if it is malicious.

File Information:
- Name: {file_name}
- Type: {file_type}
- Size: {file_size} bytes
- SHA256: {sha256}
- Entropy: {entropy}

Extracted Strings (sample):
{strings_sample}

URLs Found:
{urls}

IP Addresses Found:
{ips}

{additional_context}

Provide your analysis in the following JSON format:
{{
    "threat_level": "clean|suspicious|malicious",
    "confidence": 0.0-1.0,
    "findings": ["finding1", "finding2"],
    "reasoning": "Brief explanation of your assessment"
}}

Only output the JSON, no additional text.'''


class BaseLLMProvider(Provider):
    """
    Base class for LLM-based analysis providers.
    
    Provides common functionality for:
    - Prompt construction
    - Response parsing
    - Error handling
    """
    
    def __init__(self, model: str, timeout: int = 30):
        self.model = model
        self.timeout = timeout
        self._latency_ema = 2000.0
        self._sanitizer = InputSanitizer(SanitizerConfig())
    
    def _build_prompt(self, parsed_file: ParsedFile) -> str:
        """Build analysis prompt from parsed file."""
        strings_sample = "\n".join(parsed_file.strings[:20]) if parsed_file.strings else "None"
        urls = "\n".join(parsed_file.urls[:10]) if parsed_file.urls else "None"
        ips = "\n".join(parsed_file.ips[:10]) if parsed_file.ips else "None"
        
        additional = []
        if parsed_file.imports:
            additional.append(f"Import count: {len(parsed_file.imports)}")
            suspicious = parsed_file.metadata.get("suspicious_imports", [])
            if suspicious:
                additional.append(f"Suspicious imports: {', '.join(suspicious[:10])}")
        
        if parsed_file.sections:
            section_info = ", ".join(
                f"{s['name']}(entropy:{s.get('entropy', 0):.2f})"
                for s in parsed_file.sections[:5]
            )
            additional.append(f"Sections: {section_info}")
        
        prompt = ANALYSIS_PROMPT.format(
            file_name=parsed_file.file_name,
            file_type=parsed_file.file_type.value,
            file_size=parsed_file.file_size,
            sha256=parsed_file.hashes.get("sha256", "unknown"),
            entropy=parsed_file.entropy,
            strings_sample=strings_sample,
            urls=urls,
            ips=ips,
            additional_context="\n".join(additional) if additional else "No additional context",
        )
        return self._sanitizer.sanitize(prompt, context="llm_prompt")
    
    def _parse_response(self, response_text: str) -> Dict[str, Any]:
        """Parse LLM response into structured format."""
        json_match = re.search(r'\{[^{}]*\}', response_text, re.DOTALL)
        if json_match:
            try:
                return json.loads(json_match.group())
            except json.JSONDecodeError:
                pass
        
        try:
            return json.loads(response_text)
        except json.JSONDecodeError:
            pass
        
        result = {
            "threat_level": "unknown",
            "confidence": 0.5,
            "findings": [],
            "reasoning": response_text[:500],
        }
        
        lower_text = response_text.lower()
        if "malicious" in lower_text or "malware" in lower_text:
            result["threat_level"] = "malicious"
            result["confidence"] = 0.7
        elif "suspicious" in lower_text:
            result["threat_level"] = "suspicious"
            result["confidence"] = 0.6
        elif "clean" in lower_text or "benign" in lower_text:
            result["threat_level"] = "clean"
            result["confidence"] = 0.7
        
        return result
    
    def _create_result(
        self,
        parsed_response: Dict[str, Any],
        latency_ms: float,
        error: Optional[str] = None
    ) -> AnalysisResult:
        """Create AnalysisResult from parsed LLM response."""
        threat_map = {
            "clean": ThreatLevel.CLEAN,
            "suspicious": ThreatLevel.SUSPICIOUS,
            "malicious": ThreatLevel.MALICIOUS,
            "critical": ThreatLevel.CRITICAL,
        }
        
        threat_str = parsed_response.get("threat_level", "unknown").lower()
        threat_level = threat_map.get(threat_str, ThreatLevel.UNKNOWN)
        
        confidence = float(parsed_response.get("confidence", 0.5))
        confidence = max(0.0, min(1.0, confidence))
        
        findings = parsed_response.get("findings", [])
        if isinstance(findings, str):
            findings = [findings]
        
        reasoning = parsed_response.get("reasoning", "")
        if reasoning:
            findings.append(f"LLM reasoning: {reasoning}")
        
        score_map = {
            ThreatLevel.CLEAN: 0.1,
            ThreatLevel.SUSPICIOUS: 0.5,
            ThreatLevel.MALICIOUS: 0.85,
            ThreatLevel.CRITICAL: 0.95,
            ThreatLevel.UNKNOWN: 0.5,
        }
        score = score_map.get(threat_level, 0.5)
        
        return AnalysisResult(
            provider_name=self.name,
            provider_version=self.version,
            threat_level=threat_level,
            score=score,
            confidence=confidence,
            findings=findings,
            latency_ms=latency_ms,
            error=error,
            metadata={
                "model": self.model,
                "raw_response": parsed_response,
            }
        )
    
    @abstractmethod
    def _call_llm(self, prompt: str) -> str:
        """Make LLM API call. Implemented by subclasses."""
        pass
