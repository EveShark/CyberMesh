"""Threat intelligence enrichment agent."""

import time
from typing import Any, Dict, List

from .base import BaseAgent
from .state import GraphState, AnalysisStage
from ..threat_intel import ThreatEnrichment, EnrichmentResult
from ..providers.base import ThreatLevel
from ..logging import get_logger

logger = get_logger(__name__)


class ThreatIntelAgent(BaseAgent):
    """
    Agent that enriches analysis with threat intelligence.
    
    Queries:
    - VirusTotal (hash, URL, IP, domain)
    - MalwareBazaar (hash)
    - URLhaus (URL)
    - AbuseIPDB (IP)
    - OTX (various)
    - GreyNoise (IP)
    """
    
    def __init__(self, enable_vt: bool = True):
        """
        Initialize threat intel agent.
        
        Args:
            enable_vt: Enable VirusTotal (rate limited)
        """
        self._enricher = None
        self._enable_vt = enable_vt
    
    @property
    def name(self) -> str:
        return "threat_intel_agent"
    
    def _get_enricher(self) -> ThreatEnrichment:
        """Lazy init enricher."""
        if self._enricher is None:
            self._enricher = ThreatEnrichment(
                enable_vt=self._enable_vt,
                enable_mb=True,
                enable_urlhaus=True,
                enable_abuseipdb=True,
                enable_otx=True,
                enable_greynoise=True,
                enable_shodan=True,
                enable_crowdsec=True,
                enable_feodo=True,
                enable_spamhaus=True,
            )
        return self._enricher
    
    def should_run(self, state: GraphState) -> bool:
        """Determine if threat intel enrichment should run."""
        parsed_file = state.get("parsed_file")
        if not parsed_file:
            return False
        
        # Always run if we have a parsed file with hash
        return bool(parsed_file.hashes.get("sha256"))
    
    def __call__(self, state: GraphState) -> Dict[str, Any]:
        """Execute threat intelligence enrichment."""
        self._log_start(state)
        t0 = time.perf_counter()
        
        parsed_file = state.get("parsed_file")
        if not parsed_file:
            return {
                "errors": ["No parsed file available for threat intel"],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
            }
        
        # Only track NEW findings/steps/indicators - LangGraph will merge via operator.add
        findings = []
        reasoning_steps = []
        indicators = []
        errors: List[str] = []
        
        try:
            enricher = self._get_enricher()
            result = enricher.enrich_file(parsed_file)
            
            # Add findings from enrichment
            if result.file_hash_intel:
                hi = result.file_hash_intel
                if hi.is_known_malware:
                    from ..providers.base import Finding
                    findings.append(Finding(
                        source="threat_intel",
                        severity="critical",
                        description=f"Known malware: {hi.malware_family or 'unclassified'} "
                                   f"(VT: {hi.vt_detections or 'N/A'})",
                        confidence=0.95,
                        metadata={
                            "family": hi.malware_family,
                            "vt_detections": hi.vt_detections,
                            "sources": hi.sources,
                        },
                    ))
                    reasoning_steps.append(
                        f"Threat Intel: KNOWN MALWARE - {hi.malware_family or 'detected'} "
                        f"({', '.join(hi.sources)})"
                    )
                elif hi.vt_detections:
                    findings.append(Finding(
                        source="threat_intel",
                        severity="high" if "0/" not in hi.vt_detections else "low",
                        description=f"VirusTotal: {hi.vt_detections} detections",
                        confidence=0.85,
                        metadata={"vt_detections": hi.vt_detections},
                    ))
                    reasoning_steps.append(f"Threat Intel: VT {hi.vt_detections} detections")
            
            # Add IP intel
            for ip_intel in result.ip_intel:
                if ip_intel.is_malicious:
                    from ..providers.base import Finding
                    findings.append(Finding(
                        source="threat_intel",
                        severity="high",
                        description=f"Malicious IP: {ip_intel.ip} "
                                   f"(abuse score: {ip_intel.abuse_score})",
                        confidence=0.85,
                    ))
                    indicators.append({
                        "type": "malicious_ip",
                        "value": ip_intel.ip,
                        "context": f"abuse_score={ip_intel.abuse_score}",
                    })
            
            # Add URL intel
            for url_intel in result.url_intel:
                if url_intel.is_malicious:
                    from ..providers.base import Finding
                    findings.append(Finding(
                        source="threat_intel",
                        severity="critical",
                        description=f"Malicious URL: {url_intel.url[:50]}... "
                                   f"({url_intel.threat_type or 'malware'})",
                        confidence=0.9,
                    ))
                    indicators.append({
                        "type": "malicious_url",
                        "value": url_intel.url,
                        "context": url_intel.threat_type,
                    })
            
            elapsed = (time.perf_counter() - t0) * 1000
            
            # Determine if we need LLM based on threat intel
            needs_llm = result.is_known_malicious or result.threat_score > 0.5
            
            updates = {
                "findings": findings,
                "reasoning_steps": reasoning_steps,
                "indicators": indicators,
                "threat_intel_result": result.to_dict(),
                "threat_intel_score": result.threat_score,
                "needs_llm_reasoning": state.get("needs_llm_reasoning", False) or needs_llm,
                "errors": errors,
                "metadata": {
                    **state.get("metadata", {}),
                    "threat_intel_time_ms": elapsed,
                    "threat_intel_sources": result.sources_matched,
                },
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
            }
            
            self._log_complete(updates)
            return updates
            
        except Exception as e:
            logger.error(f"Threat intel enrichment failed: {e}")
            errors.append(f"Threat intel failed: {str(e)}")
            return {
                "errors": errors,
                "reasoning_steps": ["Threat intel failed - see errors"],
                "current_stage": AnalysisStage.DEEP_ANALYSIS,
                "metadata": {
                    "threat_intel_error": str(e),
                },
            }
