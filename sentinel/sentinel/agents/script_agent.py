"""Specialized script analysis agent for deobfuscation and behavioral analysis."""

import base64
import re
import time
from typing import Any, Dict, List, Optional, Tuple

from .base import BaseAgent
from .state import GraphState, Finding, AnalysisStage
from ..providers.base import ThreatLevel
from ..providers.base import AnalysisResult
from ..parsers.base import FileType
from ..logging import get_logger

logger = get_logger(__name__)


class ScriptAgent(BaseAgent):
    """
    Specialized agent for script file analysis.
    
    Handles:
    - PowerShell (PS1, PSM1)
    - JavaScript (JS, JSE)
    - VBScript (VBS, VBE)
    - Batch files (BAT, CMD)
    - Shell scripts (SH)
    
    Capabilities:
    - Obfuscation detection and partial deobfuscation
    - Base64/encoded payload extraction
    - Behavioral pattern identification
    - IOC extraction from deobfuscated content
    """
    
    # Common obfuscation patterns
    OBFUSCATION_PATTERNS = {
        "base64": r'[A-Za-z0-9+/]{40,}={0,2}',
        "char_codes": r'(?:chr\(|char\(|fromcharcode)\s*\d+',
        "string_concat": r"(?:\"\s*\+\s*\"|'\s*\+\s*'|\$\w+\s*\+\s*\$\w+)",
        "reverse_string": r'(?:-join\s*\[char\[\]\]|\[::-1\]|reverse\()',
        "invoke_expression": r'(?:iex|invoke-expression|eval|execute)',
        "encoded_command": r'-e(?:nc(?:odedcommand)?)?[\s]+[A-Za-z0-9+/=]+',
        "variable_substitution": r'\$\{[^}]+\}',
        "tick_obfuscation": r'`[a-zA-Z]',  # PowerShell tick escape
        "caret_obfuscation": r'\^[a-zA-Z]',  # CMD caret escape
    }
    
    # Suspicious behavioral patterns
    BEHAVIORAL_PATTERNS = {
        "download_execute": [
            r'(?:invoke-webrequest|wget|curl|downloadstring|downloadfile)',
            r'(?:new-object\s+net\.webclient)',
            r'(?:bitstransfer|start-bitstransfer)',
        ],
        "persistence": [
            r'(?:currentversion\\run|scheduled\s*task|schtasks)',
            r'(?:new-itemproperty|set-itemproperty).*(?:run|runonce)',
            r'(?:wscript\.shell).*(?:regwrite)',
        ],
        "credential_access": [
            r'(?:mimikatz|sekurlsa|lsadump)',
            r'(?:get-credential|convertto-securestring)',
            r'(?:sam|system|security).*(?:hive|registry)',
        ],
        "defense_evasion": [
            r'(?:set-executionpolicy|bypass)',
            r'(?:amsi|antimalware).*(?:bypass|disable)',
            r'(?:-windowstyle\s+hidden|-w\s+hidden)',
            r'(?:disable-windowsoptionalfeature)',
        ],
        "process_injection": [
            r'(?:virtualalloc|virtualallocex)',
            r'(?:writeprocessmemory|ntwritevirtualmemory)',
            r'(?:createremotethread|rtlcreateuserthread)',
        ],
        "command_execution": [
            r'(?:start-process|invoke-command)',
            r'(?:cmd\s*/c|powershell\s+-)',
            r'(?:wscript|cscript).*(?:\.run|\.exec)',
        ],
    }
    
    def __init__(self):
        self._deobfuscation_cache = {}
    
    @property
    def name(self) -> str:
        return "script_agent"
    
    def should_run(self, state: GraphState) -> bool:
        """Only run for script files."""
        parsed_file = state.get("parsed_file")
        if not parsed_file:
            return False
        return parsed_file.file_type == FileType.SCRIPT
    
    def __call__(self, state: GraphState) -> Dict[str, Any]:
        self._log_start(state)
        t0 = time.perf_counter()
        
        parsed_file = state.get("parsed_file")
        if not parsed_file:
            return {
                "errors": ["No parsed file available for script analysis"],
                "current_stage": AnalysisStage.ML_ANALYSIS,
            }
        
        if parsed_file.file_type != FileType.SCRIPT:
            return {
                "reasoning_steps": [f"Script agent skipped: file type is {parsed_file.file_type.value}"],
                "current_stage": AnalysisStage.ML_ANALYSIS,
            }

        findings: List[Finding] = []
        indicators = []
        reasoning_steps: List[str] = []
        errors: List[str] = []
        
        # Get script content from metadata
        content = parsed_file.metadata.get("content", "")
        if not content:
            # Try to read from file
            try:
                with open(parsed_file.file_path, "r", encoding="utf-8", errors="ignore") as f:
                    content = f.read()
            except Exception as e:
                logger.warning(f"Could not read script content: {e}")
                errors.append(f"Script content read failed: {str(e)}")
                content = ""
        
        if not content:
            return {
                "errors": ["Could not extract script content"],
                "reasoning_steps": ["Script content unavailable"],
                "current_stage": AnalysisStage.ML_ANALYSIS,
            }
        
        # 1. Detect obfuscation
        obfuscation_detected, obfuscation_types = self._detect_obfuscation(content)
        
        if obfuscation_detected:
            findings.append(Finding(
                source="script_agent",
                severity="medium",
                description=f"Obfuscation detected: {', '.join(obfuscation_types)}",
                confidence=0.8,
            ))
            reasoning_steps.append(f"Obfuscation: {', '.join(obfuscation_types)}")
        else:
            reasoning_steps.append("Obfuscation: None detected")
        
        # 2. Attempt deobfuscation
        deobfuscated_content, deobfuscation_success = self._attempt_deobfuscation(content)
        
        if deobfuscation_success:
            reasoning_steps.append("Deobfuscation: Partial success")
            # Re-analyze deobfuscated content
            analysis_content = deobfuscated_content
        else:
            reasoning_steps.append("Deobfuscation: Not applicable or failed")
            analysis_content = content
        
        # 3. Extract IOCs from content
        extracted_iocs = self._extract_iocs(analysis_content)
        
        for ioc_type, values in extracted_iocs.items():
            for value in values[:10]:  # Limit per type
                indicators.append({
                    "type": ioc_type,
                    "value": value,
                    "context": "script_extraction",
                })
        
        if extracted_iocs:
            ioc_summary = ", ".join(f"{k}:{len(v)}" for k, v in extracted_iocs.items())
            reasoning_steps.append(f"IOCs extracted: {ioc_summary}")
        
        # 4. Identify behavioral patterns
        behaviors = self._identify_behaviors(analysis_content)
        
        for behavior, matches in behaviors.items():
            severity = self._behavior_severity(behavior)
            findings.append(Finding(
                source="script_agent",
                severity=severity,
                description=f"Behavioral pattern: {behavior} ({len(matches)} matches)",
                confidence=0.75,
                metadata={"matches": matches[:5]},
            ))
        
        if behaviors:
            behavior_list = list(behaviors.keys())
            reasoning_steps.append(f"Behaviors: {', '.join(behavior_list)}")
        else:
            reasoning_steps.append("Behaviors: None detected")
        
        # 5. Determine overall threat level
        threat_level = self._assess_threat_level(
            obfuscation_types, behaviors, extracted_iocs
        )

        # Build analysis result for coordinator weighting
        score = self._threat_to_score(threat_level)
        result = AnalysisResult(
            provider_name=self.name,
            provider_version="1.0.0",
            threat_level=threat_level,
            score=score,
            confidence=0.75 if threat_level != ThreatLevel.CLEAN else 0.6,
            findings=[f.description for f in findings],
            indicators=[],
            metadata={"behaviors": list(behaviors.keys())},
        )
        
        # 6. Determine if LLM reasoning is needed
        needs_llm = self._should_trigger_llm(
            obfuscation_detected, behaviors, threat_level
        )
        
        elapsed = (time.perf_counter() - t0) * 1000
        
        updates = {
            "static_results": [result],
            "findings": findings,
            "indicators": indicators,
            "reasoning_steps": reasoning_steps,
            "stages_completed": [AnalysisStage.ML_ANALYSIS.value],
            "current_stage": AnalysisStage.ML_ANALYSIS,
            "needs_llm_reasoning": needs_llm,
            "errors": errors,
            "metadata": {
                "script_analysis_time_ms": elapsed,
                "obfuscation_types": obfuscation_types,
                "behaviors_detected": list(behaviors.keys()),
                "ioc_counts": {k: len(v) for k, v in extracted_iocs.items()},
                "deobfuscation_success": deobfuscation_success,
            },
        }
        
        self._log_complete(updates)
        return updates
    
    def _detect_obfuscation(self, content: str) -> Tuple[bool, List[str]]:
        """Detect obfuscation techniques in script content."""
        detected_types = []
        content_lower = content.lower()
        
        for obf_type, pattern in self.OBFUSCATION_PATTERNS.items():
            if re.search(pattern, content, re.IGNORECASE):
                detected_types.append(obf_type)
        
        # Additional heuristics
        # High ratio of non-printable or encoded chars
        printable_ratio = sum(1 for c in content if c.isprintable()) / max(len(content), 1)
        if printable_ratio < 0.7:
            detected_types.append("binary_encoding")
        
        # Very long lines (common in obfuscated scripts)
        lines = content.split("\n")
        long_lines = sum(1 for line in lines if len(line) > 500)
        if long_lines > 0:
            detected_types.append("long_lines")
        
        return len(detected_types) > 0, detected_types
    
    def _attempt_deobfuscation(self, content: str) -> Tuple[str, bool]:
        """Attempt to deobfuscate script content."""
        deobfuscated = content
        success = False
        
        # 1. Decode Base64 strings
        base64_pattern = r'["\']([A-Za-z0-9+/]{40,}={0,2})["\']'
        for match in re.finditer(base64_pattern, content):
            try:
                encoded = match.group(1)
                decoded = base64.b64decode(encoded).decode("utf-8", errors="ignore")
                if decoded and len(decoded) > 10 and decoded.isprintable():
                    deobfuscated = deobfuscated.replace(match.group(0), f'"{decoded}"')
                    success = True
            except Exception:
                pass
        
        # 2. Decode PowerShell -EncodedCommand
        enc_pattern = r'-e(?:nc(?:odedcommand)?)?[\s]+([A-Za-z0-9+/=]+)'
        for match in re.finditer(enc_pattern, content, re.IGNORECASE):
            try:
                encoded = match.group(1)
                # PowerShell uses UTF-16LE
                decoded = base64.b64decode(encoded).decode("utf-16-le", errors="ignore")
                if decoded and len(decoded) > 5:
                    deobfuscated += f"\n# Decoded command:\n# {decoded}"
                    success = True
            except Exception:
                pass
        
        # 3. Resolve simple char code obfuscation
        # [char]65 -> 'A'
        char_pattern = r'\[char\]\s*(\d+)'
        def replace_char(m):
            try:
                return f"'{chr(int(m.group(1)))}'"
            except:
                return m.group(0)
        
        new_content = re.sub(char_pattern, replace_char, deobfuscated, flags=re.IGNORECASE)
        if new_content != deobfuscated:
            deobfuscated = new_content
            success = True
        
        return deobfuscated, success
    
    def _extract_iocs(self, content: str) -> Dict[str, List[str]]:
        """Extract Indicators of Compromise from content."""
        iocs = {
            "urls": [],
            "ips": [],
            "domains": [],
            "emails": [],
            "file_paths": [],
            "registry_keys": [],
        }
        
        # URLs
        url_pattern = r'https?://[^\s<>"\'`\)\]\}]+'
        iocs["urls"] = list(set(re.findall(url_pattern, content, re.IGNORECASE)))
        
        # IPs
        ip_pattern = r'\b(?:\d{1,3}\.){3}\d{1,3}\b'
        ips = re.findall(ip_pattern, content)
        # Filter private/local IPs
        iocs["ips"] = [ip for ip in set(ips) if not ip.startswith(("10.", "192.168.", "127.", "0."))]
        
        # Domains (simplified)
        domain_pattern = r'\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+(?:com|net|org|io|xyz|top|info|ru|cn)\b'
        iocs["domains"] = list(set(re.findall(domain_pattern, content, re.IGNORECASE)))
        
        # File paths (Windows)
        path_pattern = r'[A-Za-z]:\\(?:[^\\/:*?"<>|\r\n]+\\)*[^\\/:*?"<>|\r\n]*'
        iocs["file_paths"] = list(set(re.findall(path_pattern, content)))[:20]
        
        # Registry keys
        reg_pattern = r'(?:HKLM|HKCU|HKCR|HKU|HKCC)(?:\\[^\\"\'\s]+)+'
        iocs["registry_keys"] = list(set(re.findall(reg_pattern, content, re.IGNORECASE)))
        
        # Filter empty
        return {k: v for k, v in iocs.items() if v}
    
    def _identify_behaviors(self, content: str) -> Dict[str, List[str]]:
        """Identify behavioral patterns in script content."""
        behaviors = {}
        
        for behavior, patterns in self.BEHAVIORAL_PATTERNS.items():
            matches = []
            for pattern in patterns:
                found = re.findall(pattern, content, re.IGNORECASE)
                matches.extend(found)
            
            if matches:
                behaviors[behavior] = list(set(matches))
        
        return behaviors
    
    def _behavior_severity(self, behavior: str) -> str:
        """Map behavior to severity level."""
        high_severity = {"credential_access", "process_injection", "defense_evasion"}
        medium_severity = {"download_execute", "persistence", "command_execution"}
        
        if behavior in high_severity:
            return "high"
        elif behavior in medium_severity:
            return "medium"
        return "low"
    
    def _assess_threat_level(
        self,
        obfuscation_types: List[str],
        behaviors: Dict[str, List[str]],
        iocs: Dict[str, List[str]]
    ) -> ThreatLevel:
        """Assess overall threat level based on analysis."""
        score = 0
        
        # Obfuscation scoring
        high_risk_obf = {"invoke_expression", "encoded_command", "base64"}
        if any(o in high_risk_obf for o in obfuscation_types):
            score += 3
        elif obfuscation_types:
            score += 1
        
        # Behavior scoring
        high_risk_behaviors = {"credential_access", "process_injection"}
        medium_risk_behaviors = {"download_execute", "persistence", "defense_evasion"}
        
        for behavior in behaviors:
            if behavior in high_risk_behaviors:
                score += 4
            elif behavior in medium_risk_behaviors:
                score += 2
            else:
                score += 1
        
        # IOC scoring
        if iocs.get("urls") or iocs.get("ips"):
            score += 1
        
        # Determine level
        if score >= 6:
            return ThreatLevel.MALICIOUS
        elif score >= 3:
            return ThreatLevel.SUSPICIOUS
        else:
            return ThreatLevel.CLEAN

    def _threat_to_score(self, threat_level: ThreatLevel) -> float:
        if threat_level == ThreatLevel.MALICIOUS:
            return 0.9
        if threat_level == ThreatLevel.SUSPICIOUS:
            return 0.6
        if threat_level == ThreatLevel.CLEAN:
            return 0.1
        return 0.0
    
    def _should_trigger_llm(
        self,
        obfuscation_detected: bool,
        behaviors: Dict[str, List[str]],
        threat_level: ThreatLevel
    ) -> bool:
        """Determine if LLM reasoning should be triggered."""
        # Complex obfuscation needs LLM explanation
        if obfuscation_detected and threat_level != ThreatLevel.CLEAN:
            return True
        
        # Multiple behaviors need synthesis
        if len(behaviors) >= 2:
            return True
        
        # Suspicious but not clearly malicious
        if threat_level == ThreatLevel.SUSPICIOUS:
            return True
        
        return False
