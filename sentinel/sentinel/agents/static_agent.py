"""Static analysis agent using rule-based and heuristic providers."""

import time
from typing import Any, Dict, List

from .base import BaseAgent
from .state import GraphState, Finding, AnalysisStage
from ..providers.base import ThreatLevel, AnalysisResult
from ..providers.static import EntropyProvider, StringsProvider
from ..logging import get_logger

logger = get_logger(__name__)


class StaticAnalysisAgent(BaseAgent):
    """
    Agent for static file analysis.
    
    Runs:
    - Entropy analysis (packed/encrypted detection)
    - String pattern analysis (suspicious patterns)
    - File structure analysis
    """
    
    def __init__(self):
        self.entropy_provider = EntropyProvider()
        self.strings_provider = StringsProvider()
    
    @property
    def name(self) -> str:
        return "static_agent"
    
    def __call__(self, state: GraphState) -> Dict[str, Any]:
        self._log_start(state)
        t0 = time.perf_counter()
        
        parsed_file = state.get("parsed_file")
        if not parsed_file:
            return {
                "errors": ["No parsed file available for static analysis"],
                "current_stage": AnalysisStage.STATIC_ANALYSIS,
            }
        
        results: List[AnalysisResult] = []
        findings: List[Finding] = []
        indicators: List[Dict[str, str]] = []
        reasoning_steps: List[str] = []
        errors: List[str] = []
        
        # Run entropy analysis
        try:
            entropy_result = self.entropy_provider.analyze(parsed_file)
            results.append(entropy_result)
            
            if entropy_result.threat_level != ThreatLevel.CLEAN:
                findings.append(Finding(
                    source=self.entropy_provider.name,
                    severity=self._threat_to_severity(entropy_result.threat_level),
                    description=f"High entropy detected: {parsed_file.entropy:.2f}",
                    confidence=entropy_result.confidence,
                ))
                reasoning_steps.append(
                    f"Entropy analysis: {entropy_result.threat_level.value} "
                    f"(entropy={parsed_file.entropy:.2f}, threshold=7.0)"
                )
            else:
                reasoning_steps.append(
                    f"Entropy analysis: CLEAN (entropy={parsed_file.entropy:.2f})"
                )
                
        except Exception as e:
            logger.error(f"Entropy analysis failed: {e}")
            errors.append(f"Entropy analysis failed: {str(e)}")
            reasoning_steps.append("Entropy analysis: ERROR")
        
        # Run strings analysis
        try:
            strings_result = self.strings_provider.analyze(parsed_file)
            results.append(strings_result)
            
            for finding_text in strings_result.findings:
                severity = "medium"
                if "command_execution" in finding_text.lower():
                    severity = "high"
                elif "credential" in finding_text.lower():
                    severity = "high"
                    
                findings.append(Finding(
                    source=self.strings_provider.name,
                    severity=severity,
                    description=finding_text,
                    confidence=strings_result.confidence,
                ))
            
            for indicator in strings_result.indicators:
                indicators.append({
                    "type": indicator.type,
                    "value": indicator.value,
                    "context": indicator.context or "static_analysis",
                })
            
            if strings_result.threat_level != ThreatLevel.CLEAN:
                reasoning_steps.append(
                    f"Strings analysis: {strings_result.threat_level.value} "
                    f"({len(strings_result.findings)} suspicious patterns found)"
                )
            else:
                reasoning_steps.append("Strings analysis: CLEAN (no suspicious patterns)")
                
        except Exception as e:
            logger.error(f"Strings analysis failed: {e}")
            errors.append(f"Strings analysis failed: {str(e)}")
            reasoning_steps.append("Strings analysis: ERROR")
        
        # Analyze file metadata
        metadata_findings = self._analyze_metadata(parsed_file)
        findings.extend(metadata_findings)
        
        # Check if deep analysis is needed
        needs_deep = self._should_trigger_deep_analysis(results, findings)
        
        # Check if LLM reasoning is needed (for scripts and complex cases)
        needs_llm = self._should_trigger_llm(parsed_file, results, findings)
        
        elapsed = (time.perf_counter() - t0) * 1000
        
        updates = {
            "static_results": results,
            "findings": findings,
            "indicators": indicators,
            "reasoning_steps": reasoning_steps,
            "stages_completed": [AnalysisStage.STATIC_ANALYSIS.value],
            "current_stage": AnalysisStage.STATIC_ANALYSIS,
            "needs_deep_analysis": needs_deep,
            "needs_llm_reasoning": needs_llm,
            "errors": errors,
            "metadata": {"static_analysis_time_ms": elapsed},
        }
        
        self._log_complete(updates)
        return updates
    
    def _analyze_metadata(self, parsed_file) -> List[Finding]:
        """Analyze file metadata for suspicious indicators."""
        findings = []
        
        # Check for suspicious imports (PE files)
        # NOTE: Many legitimate Windows programs have "suspicious" APIs (CreateProcess, etc.)
        # Only flag as high severity for very high counts to reduce false positives
        suspicious_imports = parsed_file.metadata.get("suspicious_imports", [])
        if suspicious_imports:
            # Require MORE imports to trigger, and use lower severity
            # This prevents Windows system files from being flagged
            if len(suspicious_imports) >= 20:
                severity = "high"
            elif len(suspicious_imports) >= 10:
                severity = "medium" 
            else:
                severity = "low"
            
            findings.append(Finding(
                source="metadata_analysis",
                severity=severity,
                description=f"Found {len(suspicious_imports)} suspicious API imports",
                confidence=0.5,  # Reduced confidence - imports alone don't mean malware
                metadata={"imports": suspicious_imports[:10]},
            ))
        
        # Check for suspicious patterns in scripts
        suspicious_patterns = parsed_file.metadata.get("suspicious_patterns", [])
        if suspicious_patterns:
            findings.append(Finding(
                source="metadata_analysis",
                severity="high",
                description=f"Found {len(suspicious_patterns)} suspicious script patterns",
                confidence=0.75,
                metadata={"patterns": suspicious_patterns[:10]},
            ))
        
        # Check for obfuscation
        obfuscation = parsed_file.metadata.get("obfuscation_indicators", [])
        if obfuscation:
            findings.append(Finding(
                source="metadata_analysis",
                severity="medium",
                description=f"Detected obfuscation: {', '.join(obfuscation[:3])}",
                confidence=0.65,
            ))
        
        # Check for macros in Office files
        if parsed_file.metadata.get("has_macros"):
            suspicious_vba = parsed_file.metadata.get("suspicious_vba", [])
            severity = "high" if suspicious_vba else "medium"
            findings.append(Finding(
                source="metadata_analysis",
                severity=severity,
                description=f"Document contains macros" + 
                           (f" with {len(suspicious_vba)} suspicious patterns" if suspicious_vba else ""),
                confidence=0.8 if suspicious_vba else 0.5,
            ))
        
        # Check for JavaScript in PDFs
        if parsed_file.metadata.get("has_javascript"):
            findings.append(Finding(
                source="metadata_analysis",
                severity="high",
                description="PDF contains embedded JavaScript",
                confidence=0.8,
            ))
        
        # Check for dangerous permissions in Android
        dangerous_perms = parsed_file.metadata.get("dangerous_permissions", [])
        if dangerous_perms:
            findings.append(Finding(
                source="metadata_analysis",
                severity="high" if len(dangerous_perms) > 5 else "medium",
                description=f"APK requests {len(dangerous_perms)} dangerous permissions",
                confidence=0.7,
                metadata={"permissions": dangerous_perms[:10]},
            ))
        
        return findings
    
    def _should_trigger_deep_analysis(
        self, 
        results: List[AnalysisResult], 
        findings: List[Finding]
    ) -> bool:
        """Determine if deeper analysis is needed."""
        # Check if any result is suspicious or malicious
        for result in results:
            if result.threat_level in (ThreatLevel.SUSPICIOUS, ThreatLevel.MALICIOUS):
                return True
        
        # Check if we have high severity findings
        high_severity_count = sum(1 for f in findings if f.severity in ("high", "critical"))
        if high_severity_count >= 2:
            return True
        
        # Check for specific indicators
        for finding in findings:
            if any(keyword in finding.description.lower() for keyword in 
                   ["obfuscation", "macro", "javascript", "dangerous permission"]):
                return True
        
        return False
    
    def _threat_to_severity(self, threat_level: ThreatLevel) -> str:
        """Convert threat level to severity string."""
        mapping = {
            ThreatLevel.CLEAN: "low",
            ThreatLevel.SUSPICIOUS: "medium",
            ThreatLevel.MALICIOUS: "high",
            ThreatLevel.CRITICAL: "critical",
            ThreatLevel.UNKNOWN: "low",
        }
        return mapping.get(threat_level, "low")
    
    def _should_trigger_llm(
        self,
        parsed_file,
        results: List[AnalysisResult],
        findings: List[Finding]
    ) -> bool:
        """Determine if LLM reasoning should be triggered."""
        from ..parsers.base import FileType
        
        # Always use LLM for script files with findings
        if parsed_file.file_type == FileType.SCRIPT:
            if findings or parsed_file.metadata.get("suspicious_patterns"):
                return True
            if parsed_file.metadata.get("obfuscation_indicators"):
                return True
        
        # High severity findings need explanation
        high_severity = sum(1 for f in findings if f.severity in ("high", "critical"))
        if high_severity >= 2:
            return True
        
        # Suspicious/malicious results need LLM reasoning
        for result in results:
            if result.threat_level in (ThreatLevel.SUSPICIOUS, ThreatLevel.MALICIOUS):
                return True
        
        # Complex indicators need synthesis
        if len(findings) >= 4:
            return True
        
        return False
