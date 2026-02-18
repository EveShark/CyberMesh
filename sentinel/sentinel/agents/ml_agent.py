"""ML-based analysis agent."""

import time
from pathlib import Path
from typing import Any, Dict, List, Optional

from .base import BaseAgent
from .state import GraphState, Finding, AnalysisStage
from ..providers.base import ThreatLevel, AnalysisResult
from ..parsers.base import FileType
from ..logging import get_logger

logger = get_logger(__name__)


class MLAnalysisAgent(BaseAgent):
    """
    Agent for ML-based malware detection.
    
    Uses pre-trained models from CyberMesh:
    - PE imports model (malware_pe_imports.pkl)
    - Additional models as available
    """
    
    def __init__(self, models_path: Optional[str] = None):
        self.models_path = Path(models_path) if models_path else Path("../CyberMesh/ai-service/data/models")
        self.providers = {}
        self._load_providers()
    
    @property
    def name(self) -> str:
        return "ml_agent"
    
    def _load_providers(self) -> None:
        """Load available ML providers."""
        if not self.models_path.exists():
            logger.warning(f"Models path not found: {self.models_path}")
            return
        # Try to load PE malware model
        try:
            from ..providers.ml import MalwarePEProvider
            # High threshold to reduce false positives on legitimate Windows binaries
            # The EMBER-trained model has high FP rate, so we require very high confidence
            self.providers["pe"] = MalwarePEProvider(
                models_path=str(self.models_path),
                threshold=0.995,  # Require 99.5% confidence
            )
            logger.info(f"Loaded PE malware ML provider")
        except Exception as e:
            logger.warning(f"Could not load PE ML provider: {e}")
    
    def should_run(self, state: GraphState) -> bool:
        """Only run if we have ML providers and compatible file type."""
        if not self.providers:
            return False
        
        parsed_file = state.get("parsed_file")
        if not parsed_file:
            return False
        
        # Check if any provider supports this file type
        file_type = parsed_file.file_type
        for provider in self.providers.values():
            if file_type in provider.supported_types:
                return True
        
        return False
    
    def __call__(self, state: GraphState) -> Dict[str, Any]:
        self._log_start(state)
        t0 = time.perf_counter()
        
        parsed_file = state.get("parsed_file")
        if not parsed_file:
            return {
                "errors": ["No parsed file available for ML analysis"],
                "current_stage": AnalysisStage.ML_ANALYSIS,
            }

        if not self.providers:
            return {
                "errors": ["No ML providers available"],
                "current_stage": AnalysisStage.ML_ANALYSIS,
            }
        
        results: List[AnalysisResult] = []
        findings: List[Finding] = []
        reasoning_steps: List[str] = []
        errors: List[str] = []
        
        # Run applicable ML providers
        for provider_type, provider in self.providers.items():
            if parsed_file.file_type not in provider.supported_types:
                reasoning_steps.append(
                    f"ML provider {provider.name}: Skipped (file type {parsed_file.file_type.value} not supported)"
                )
                continue
            
            try:
                result = provider.analyze(parsed_file)
                results.append(result)
                
                # Convert to findings
                if result.threat_level == ThreatLevel.MALICIOUS:
                    findings.append(Finding(
                        source=provider.name,
                        severity="high",
                        description=f"ML model detected malware (score: {result.score:.1%})",
                        confidence=result.confidence,
                        metadata={"model_score": result.score},
                    ))
                    reasoning_steps.append(
                        f"ML model {provider.name}: MALICIOUS "
                        f"(score={result.score:.1%}, confidence={result.confidence:.1%})"
                    )
                elif result.threat_level == ThreatLevel.SUSPICIOUS:
                    findings.append(Finding(
                        source=provider.name,
                        severity="medium",
                        description=f"ML model flagged as suspicious (score: {result.score:.1%})",
                        confidence=result.confidence,
                        metadata={"model_score": result.score},
                    ))
                    reasoning_steps.append(
                        f"ML model {provider.name}: SUSPICIOUS "
                        f"(score={result.score:.1%}, confidence={result.confidence:.1%})"
                    )
                else:
                    reasoning_steps.append(
                        f"ML model {provider.name}: CLEAN "
                        f"(score={result.score:.1%}, confidence={result.confidence:.1%})"
                    )
                
                # Add findings from the result
                for finding_text in result.findings:
                    if finding_text not in [f.description for f in findings]:
                        findings.append(Finding(
                            source=provider.name,
                            severity="medium",
                            description=finding_text,
                            confidence=result.confidence,
                        ))
                        
            except Exception as e:
                logger.error(f"ML provider {provider.name} failed: {e}")
                reasoning_steps.append(f"ML model {provider.name}: ERROR - {str(e)[:50]}")
                errors.append(f"ML provider {provider.name} failed: {str(e)}")
        
        # Determine if LLM reasoning is needed
        needs_llm = self._should_trigger_llm_reasoning(state, results, findings)
        
        elapsed = (time.perf_counter() - t0) * 1000
        
        updates = {
            "ml_results": results,
            "findings": findings,
            "reasoning_steps": reasoning_steps,
            "stages_completed": [AnalysisStage.ML_ANALYSIS.value],
            "current_stage": AnalysisStage.ML_ANALYSIS,
            "needs_llm_reasoning": needs_llm,
            "errors": errors,
            "metadata": {"ml_analysis_time_ms": elapsed},
        }
        
        self._log_complete(updates)
        return updates
    
    def _should_trigger_llm_reasoning(
        self,
        state: GraphState,
        results: List[AnalysisResult],
        findings: List[Finding]
    ) -> bool:
        """Determine if LLM reasoning should be triggered."""
        # Always use LLM for uncertain cases
        for result in results:
            # Score between 0.4 and 0.7 is uncertain
            if 0.4 <= result.score <= 0.7:
                return True
            # Low confidence needs explanation
            if result.confidence < 0.6:
                return True
        
        # If static and ML disagree, need LLM to arbitrate
        static_results = state.get("static_results", [])
        if static_results and results:
            static_threat = max(r.threat_level.value for r in static_results)
            ml_threat = max(r.threat_level.value for r in results)
            if static_threat != ml_threat:
                return True
        
        # Complex files benefit from LLM explanation
        parsed_file = state.get("parsed_file")
        if parsed_file:
            # Scripts with obfuscation
            if parsed_file.metadata.get("obfuscation_indicators"):
                return True
            # Files with many suspicious indicators
            if len(findings) >= 3:
                return True
        
        return False
