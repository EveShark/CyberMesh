"""YARA rule-based detection provider."""

import time
from typing import Set, List

from ..base import Provider, AnalysisResult, ThreatLevel, Indicator
from ...parsers.base import ParsedFile, FileType
from ...fastpath.yara_engine import YaraEngine
from ...logging import get_logger

logger = get_logger(__name__)


class YaraProvider(Provider):
    """
    YARA rule-based malware detection.
    
    Wraps the FastPath YaraEngine as a full Provider
    that can be used with the router and benchmark system.
    """
    
    def __init__(self, rules_path: str = None, use_external_rules: bool = True):
        """
        Initialize YARA provider.
        
        Args:
            rules_path: Path to YARA rules directory
        """
        self._latency_ema = 5.0
        self.engine = YaraEngine(
            rules_path=rules_path,
            use_external_rules=use_external_rules,
        )
    
    @property
    def name(self) -> str:
        return "yara_rules"
    
    @property
    def version(self) -> str:
        return "1.0.0"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {
            FileType.PE, FileType.PDF, FileType.OFFICE, 
            FileType.SCRIPT, FileType.ANDROID
        }
    
    def get_cost_per_call(self) -> float:
        return 0.0
    
    def get_avg_latency_ms(self) -> float:
        return self._latency_ema
    
    def analyze(self, parsed_file: ParsedFile) -> AnalysisResult:
        t0 = time.perf_counter()
        
        try:
            # Get file content for YARA matching
            with open(parsed_file.file_path, "rb") as f:
                data = f.read()
            
            matches = self.engine.match_data(data)
            
            findings = []
            indicators = []
            max_severity = "low"
            
            severity_scores = {"low": 0.3, "medium": 0.6, "high": 0.8, "critical": 0.95}
            max_score = 0.0
            
            # Rules to ignore (informational, capability detection, not malware indicators)
            # These rules detect capabilities/features that legitimate software also has
            informational_rules = {
                # PE structure checks
                "IsPE32", "IsPE64", "IsNET_EXE", "IsNET_DLL", "IsDLL", 
                "IsConsole", "IsWindowsGUI", "HasOverlay", "HasDebugData",
                "HasRichSignature", "HasDigitalSignature", "HasTaggantSignature",
                "IsBeyondImageSize", 
                # Generic patterns (too broad)
                "domain", "IP", "url", "contains_base64",
                # Anti-debug/VM (legitimate software uses these)
                "anti_dbg", "vmdetect", "DebuggerCheck__QueryInfo",
                "DebuggerException__SetConsoleCtrl", "antisb_threatExpert",
                # Capability rules (legitimate APIs that Windows tools use)
                "screenshot", "keylogger", "win_registry", "win_mutex",
                "win_token", "escalate_priv", "disable_dep", "inject_thread",
                "create_process", "persistence", "network_http", "network_tcp",
                "network_dns", "win_files_operation", "win_hook",
                # Compiler signatures (not malware indicators)
                "Microsoft_Visual_Cpp_80", "Microsoft_Visual_Cpp_80_DLL",
                "Microsoft_Visual_Cpp_v50v60_MFC", "Microsoft_Visual_Basic_v50",
                "Borland_Delphi", "MinGW", "AutoIt",
                # Library detection
                "Str_Win32_Winsock2_Library", "System_Tools",
            }
            
            for match in matches:
                # Skip informational/benign rules
                if match.rule_name in informational_rules or "PECheck" in match.tags:
                    continue
                
                findings.append(f"YARA: {match.rule_name} - {match.description}")
                indicators.append(Indicator(
                    type="yara_rule",
                    value=match.rule_name,
                    context=match.description,
                ))
                
                match_score = severity_scores.get(match.severity, 0.5)
                if match_score > max_score:
                    max_score = match_score
                    max_severity = match.severity
            
            # Determine threat level
            if max_score >= 0.8:
                threat_level = ThreatLevel.MALICIOUS
            elif max_score >= 0.6:
                threat_level = ThreatLevel.SUSPICIOUS
            elif max_score >= 0.3:
                threat_level = ThreatLevel.SUSPICIOUS
            else:
                threat_level = ThreatLevel.CLEAN
            
            # No matches = clean
            if not matches:
                threat_level = ThreatLevel.CLEAN
                max_score = 0.0
            
            confidence = min(0.9, 0.5 + len(matches) * 0.1) if matches else 0.7
            
            latency = (time.perf_counter() - t0) * 1000
            self._latency_ema = 0.9 * self._latency_ema + 0.1 * latency
            
            return AnalysisResult(
                provider_name=self.name,
                provider_version=self.version,
                threat_level=threat_level,
                score=max_score,
                confidence=confidence,
                findings=findings,
                indicators=indicators,
                latency_ms=latency,
                metadata={
                    "yara_matches": len(matches),
                    "max_severity": max_severity,
                }
            )
            
        except Exception as e:
            logger.error(f"YARA analysis failed: {e}")
            return AnalysisResult(
                provider_name=self.name,
                provider_version=self.version,
                threat_level=ThreatLevel.UNKNOWN,
                score=0.0,
                confidence=0.0,
                error=str(e),
                latency_ms=(time.perf_counter() - t0) * 1000,
            )
