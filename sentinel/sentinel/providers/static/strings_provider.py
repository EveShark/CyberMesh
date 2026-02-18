"""String-based analysis provider."""

import re
import time
from typing import Set, List, Dict

from ..base import Provider, AnalysisResult, ThreatLevel, Indicator
from ...parsers.base import ParsedFile, FileType
from ...logging import get_logger

logger = get_logger(__name__)


class StringsProvider(Provider):
    """
    String-based detection provider.
    
    Analyzes extracted strings for:
    - Suspicious commands and patterns
    - Network indicators (URLs, IPs, domains)
    - Obfuscation patterns
    - Known malicious strings
    """
    
    SUSPICIOUS_PATTERNS: Dict[str, List[str]] = {
        "command_execution": [
            "cmd.exe", "powershell", "wscript", "cscript", "mshta",
            "rundll32", "regsvr32", "certutil", "bitsadmin",
        ],
        "download_execute": [
            "downloadstring", "downloadfile", "invoke-webrequest",
            "wget", "curl", "urldownloadtofile", "iex(",
        ],
        "persistence": [
            "schtasks", "at.exe", "reg add", "startup", "run=",
            "currentversion\\run", "winlogon",
        ],
        "evasion": [
            "bypass", "-nop", "-w hidden", "-enc", "encodedcommand",
            "frombase64", "gzipstream", "memorystream",
        ],
        "credential_theft": [
            "mimikatz", "sekurlsa", "lsass", "sam", "ntds",
            "credential", "password", "passwd",
        ],
        "network": [
            "socket", "connect(", "send(", "recv(",
            "wsastartup", "internetopen",
        ],
    }
    
    def __init__(self):
        self._latency_ema = 10.0
    
    @property
    def name(self) -> str:
        return "strings_analyzer"
    
    @property
    def version(self) -> str:
        return "1.0.0"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PE, FileType.PDF, FileType.OFFICE, FileType.SCRIPT}
    
    def get_cost_per_call(self) -> float:
        return 0.0
    
    def get_avg_latency_ms(self) -> float:
        return self._latency_ema
    
    def analyze(self, parsed_file: ParsedFile) -> AnalysisResult:
        t0 = time.perf_counter()
        
        findings = []
        indicators = []
        category_scores = {}
        
        all_strings = " ".join(parsed_file.strings).lower()
        
        for category, patterns in self.SUSPICIOUS_PATTERNS.items():
            matches = []
            for pattern in patterns:
                if pattern.lower() in all_strings:
                    matches.append(pattern)
            
            if matches:
                category_scores[category] = len(matches) / len(patterns)
                findings.append(f"{category}: found {len(matches)} suspicious patterns")
                for match in matches[:5]:
                    indicators.append(Indicator(
                        type="suspicious_string",
                        value=match,
                        context=category
                    ))
        
        if parsed_file.urls:
            findings.append(f"Found {len(parsed_file.urls)} URLs")
            for url in parsed_file.urls[:10]:
                indicators.append(Indicator(
                    type="url",
                    value=url,
                    context="extracted_url"
                ))
            category_scores["network_indicators"] = min(len(parsed_file.urls) / 5, 1.0)
        
        if parsed_file.ips:
            findings.append(f"Found {len(parsed_file.ips)} IP addresses")
            for ip in parsed_file.ips[:10]:
                indicators.append(Indicator(
                    type="ip",
                    value=ip,
                    context="extracted_ip"
                ))
            category_scores["network_indicators"] = max(
                category_scores.get("network_indicators", 0),
                min(len(parsed_file.ips) / 3, 1.0)
            )
        
        obfuscation_score = self._check_obfuscation(parsed_file.strings)
        if obfuscation_score > 0:
            category_scores["obfuscation"] = obfuscation_score
            findings.append(f"Potential obfuscation detected (score: {obfuscation_score:.2f})")
        
        if category_scores:
            weights = {
                "command_execution": 0.3,
                "download_execute": 0.3,
                "persistence": 0.2,
                "evasion": 0.2,
                "credential_theft": 0.3,
                "network": 0.1,
                "network_indicators": 0.15,
                "obfuscation": 0.2,
            }
            
            score = sum(
                category_scores.get(cat, 0) * weight
                for cat, weight in weights.items()
            )
            score = min(score, 1.0)
        else:
            score = 0.0
        
        # Check for high-risk patterns that should always trigger alert
        high_risk_patterns_found = any(
            cat in category_scores 
            for cat in ["command_execution", "credential_theft", "download_execute"]
        )
        
        if score >= 0.6:
            threat_level = ThreatLevel.MALICIOUS
        elif score >= 0.3 or high_risk_patterns_found:
            # Flag as suspicious if high-risk patterns detected
            threat_level = ThreatLevel.SUSPICIOUS
        else:
            threat_level = ThreatLevel.CLEAN
        
        confidence = min(0.5 + score * 0.4, 0.9)
        
        latency = (time.perf_counter() - t0) * 1000
        self._latency_ema = 0.9 * self._latency_ema + 0.1 * latency
        
        return AnalysisResult(
            provider_name=self.name,
            provider_version=self.version,
            threat_level=threat_level,
            score=score,
            confidence=confidence,
            findings=findings,
            indicators=indicators,
            latency_ms=latency,
            metadata={
                "category_scores": category_scores,
                "strings_analyzed": len(parsed_file.strings),
            }
        )
    
    def _check_obfuscation(self, strings: List[str]) -> float:
        """Check for obfuscation patterns."""
        score = 0.0
        
        base64_pattern = re.compile(r'^[A-Za-z0-9+/]{50,}={0,2}$')
        hex_pattern = re.compile(r'^[0-9a-fA-F]{32,}$')
        
        base64_count = 0
        hex_count = 0
        
        for s in strings:
            if base64_pattern.match(s):
                base64_count += 1
            if hex_pattern.match(s):
                hex_count += 1
        
        if base64_count > 3:
            score += 0.4
        if hex_count > 5:
            score += 0.3
        
        concat_patterns = ["+ '", "'+", '+ "', '"+', "` + `"]
        all_text = " ".join(strings)
        concat_count = sum(all_text.count(p) for p in concat_patterns)
        if concat_count > 10:
            score += 0.3
        
        return min(score, 1.0)
