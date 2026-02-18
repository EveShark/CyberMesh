"""LLM-based reasoning agent for threat explanation."""

import time
from typing import Any, Dict, List, Optional

from .base import BaseAgent
from .state import GraphState, Finding, AnalysisStage
from ..providers.base import ThreatLevel, AnalysisResult
from ..config import load_settings
from ..logging import get_logger

logger = get_logger(__name__)


REASONING_PROMPT = '''You are a malware analyst providing expert threat assessment.

## File Information
- Name: {file_name}
- Type: {file_type}
- Size: {file_size} bytes
- SHA256: {file_hash}
- Entropy: {entropy:.2f}

## Analysis Results

### Static Analysis Findings:
{static_findings}

### ML Model Results:
{ml_findings}

### Indicators Found:
{indicators}

### Previous Reasoning Steps:
{reasoning_steps}

## Your Task
Based on the above analysis, provide:
1. Your threat assessment (CLEAN, SUSPICIOUS, or MALICIOUS)
2. Confidence level (0.0-1.0)
3. A clear explanation of your reasoning
4. Any additional concerns or recommendations

Respond in this exact JSON format:
{{
    "threat_level": "clean|suspicious|malicious",
    "confidence": 0.0-1.0,
    "reasoning": "Your detailed explanation",
    "concerns": ["concern1", "concern2"],
    "recommendations": ["recommendation1", "recommendation2"]
}}'''


class LLMReasoningAgent(BaseAgent):
    """
    Agent for LLM-based threat reasoning and explanation.
    
    Uses LLM to:
    - Synthesize findings from static and ML analysis
    - Provide human-readable explanations
    - Make final verdict recommendations
    - Identify additional concerns
    """
    
    def __init__(self, api_key: Optional[str] = None):
        self.api_key = api_key
        self.provider = None
        self._init_provider()
    
    @property
    def name(self) -> str:
        return "llm_agent"
    
    def _init_provider(self) -> None:
        """Initialize LLM provider with fallback chain: Groq -> GLM -> Ollama."""
        # Try Groq first (primary)
        try:
            from ..providers.llm import GroqProvider
            self.provider = GroqProvider()
            logger.info("LLM provider initialized: Groq (primary)")
            return
        except Exception as e:
            logger.debug(f"Groq provider not available: {e}")
        
        # Try GLM as fallback
        try:
            settings = load_settings()
            api_key = self.api_key or settings.llm.glm_api_key
            
            if api_key:
                from ..providers.llm import GLMProvider
                self.provider = GLMProvider(
                    api_key=api_key,
                    model=settings.llm.glm_model,
                    timeout=settings.llm.timeout,
                )
                logger.info("LLM provider initialized: GLM (fallback)")
                return
        except Exception as e:
            logger.debug(f"GLM provider not available: {e}")
        
        # Try Ollama as last resort
        try:
            from ..providers.llm import OllamaProvider
            self.provider = OllamaProvider()
            logger.info("LLM provider initialized: Ollama (local fallback)")
            return
        except Exception as e:
            logger.debug(f"Ollama provider not available: {e}")
        
        logger.warning("No LLM provider available - LLM reasoning will be skipped")
    
    def should_run(self, state: GraphState) -> bool:
        """Run if LLM is needed and provider is available."""
        if not self.provider:
            return False
        return state.get("needs_llm_reasoning", False)
    
    def __call__(self, state: GraphState) -> Dict[str, Any]:
        self._log_start(state)
        t0 = time.perf_counter()

        if not self.provider:
            return {
                "reasoning_steps": ["LLM reasoning skipped: No provider configured"],
                "current_stage": AnalysisStage.LLM_REASONING,
                "stages_completed": [AnalysisStage.LLM_REASONING.value],
                "metadata": {"llm_skipped": "no_provider"},
            }

        parsed_file = state.get("parsed_file")
        if not parsed_file:
            return {
                "errors": ["No parsed file for LLM reasoning"],
                "current_stage": AnalysisStage.LLM_REASONING,
            }

        results: List[AnalysisResult] = []
        findings: List[Finding] = []
        reasoning_steps: List[str] = []
        errors: List[str] = []

        # Build the reasoning prompt
        try:
            prompt = self._build_prompt(state, parsed_file)
        except Exception as exc:  # pylint: disable=broad-except
            logger.error(f"LLM prompt build failed: {exc}")
            return {
                "errors": [f"LLM prompt build failed: {str(exc)}"],
                "reasoning_steps": ["LLM reasoning skipped: prompt build failed"],
                "current_stage": AnalysisStage.LLM_REASONING,
                "stages_completed": [AnalysisStage.LLM_REASONING.value],
                "metadata": {"llm_error": "prompt_build_failed"},
            }
        
        try:
            # Call LLM - handle both Groq and legacy providers
            if hasattr(self.provider, 'analyze_with_fallback'):
                # Groq provider
                llm_response = self.provider.analyze_with_fallback(
                    prompt=prompt,
                    system_prompt=self._get_system_prompt(),
                )
                
                if llm_response.error:
                    reasoning_steps.append(f"LLM error: {llm_response.error}")
                    errors.append(f"LLM provider error: {llm_response.error}")
                    return {
                        "reasoning_steps": reasoning_steps,
                        "errors": errors,
                        "current_stage": AnalysisStage.LLM_REASONING,
                        "stages_completed": [AnalysisStage.LLM_REASONING.value],
                        "metadata": {"llm_error": "provider_error"},
                    }
                
                # Parse the response content
                raw_response = self._parse_llm_response(llm_response.content)
                
                # Create a compatible result
                from ..providers.base import AnalysisResult, ThreatLevel
                threat_map = {
                    "clean": ThreatLevel.CLEAN,
                    "suspicious": ThreatLevel.SUSPICIOUS,
                    "malicious": ThreatLevel.MALICIOUS,
                }
                result = AnalysisResult(
                    provider_name="groq",
                    provider_version="1.0.0",
                    threat_level=threat_map.get(raw_response.get("threat_level", "").lower(), ThreatLevel.UNKNOWN),
                    score=0.5,
                    confidence=float(raw_response.get("confidence", 0.5)),
                    findings=[raw_response.get("reasoning", "")],
                    latency_ms=llm_response.latency_ms,
                    metadata={"raw_response": raw_response, "model": llm_response.model},
                )
                results.append(result)
            else:
                # Legacy provider (GLM, etc.)
                result = self.provider.analyze(parsed_file)
                results.append(result)
                raw_response = result.metadata.get("raw_response", {})
            
            reasoning = raw_response.get("reasoning", "")
            if reasoning:
                reasoning_steps.append(f"LLM Analysis: {reasoning}")
            
            concerns = raw_response.get("concerns", [])
            for concern in concerns:
                findings.append(Finding(
                    source="llm_reasoning",
                    severity="medium",
                    description=f"LLM concern: {concern}",
                    confidence=result.confidence,
                ))
            
            recommendations = raw_response.get("recommendations", [])
            if recommendations:
                reasoning_steps.append(f"Recommendations: {', '.join(recommendations)}")
            
            # Add main finding
            findings.append(Finding(
                source="llm_reasoning",
                severity=self._threat_to_severity(result.threat_level),
                description=f"LLM verdict: {result.threat_level.value} "
                           f"(confidence: {result.confidence:.1%})",
                confidence=result.confidence,
                metadata={"reasoning": reasoning},
            ))
            
        except Exception as e:
            logger.error(f"LLM reasoning failed: {e}")
            reasoning_steps.append(f"LLM reasoning error: {str(e)[:100]}")
            errors.append(f"LLM reasoning failed: {str(e)}")
        
        elapsed = (time.perf_counter() - t0) * 1000
        
        updates = {
            "llm_results": results,
            "findings": findings,
            "reasoning_steps": reasoning_steps,
            "stages_completed": [AnalysisStage.LLM_REASONING.value],
            "current_stage": AnalysisStage.LLM_REASONING,
            "errors": errors,
            "metadata": {"llm_analysis_time_ms": elapsed},
        }
        
        self._log_complete(updates)
        return updates
    
    def _build_prompt(self, state: GraphState, parsed_file) -> str:
        """Build the reasoning prompt."""
        # Format static findings
        static_findings = []
        for result in state.get("static_results", []):
            static_findings.append(
                f"- {result.provider_name}: {result.threat_level.value} "
                f"(score={result.score:.1%})"
            )
            for finding in result.findings[:3]:
                static_findings.append(f"  * {finding}")
        
        # Format ML findings
        ml_findings = []
        for result in state.get("ml_results", []):
            ml_findings.append(
                f"- {result.provider_name}: {result.threat_level.value} "
                f"(score={result.score:.1%})"
            )
            for finding in result.findings[:3]:
                ml_findings.append(f"  * {finding}")
        
        # Format indicators
        indicators = state.get("indicators", [])
        indicator_strs = [f"- [{i['type']}] {i['value']}" for i in indicators[:15]]
        
        # Format previous reasoning
        reasoning = state.get("reasoning_steps", [])
        
        return REASONING_PROMPT.format(
            file_name=parsed_file.file_name,
            file_type=parsed_file.file_type.value,
            file_size=parsed_file.file_size,
            file_hash=parsed_file.hashes.get("sha256", "unknown")[:32],
            entropy=parsed_file.entropy,
            static_findings="\n".join(static_findings) or "None",
            ml_findings="\n".join(ml_findings) or "None",
            indicators="\n".join(indicator_strs) or "None",
            reasoning_steps="\n".join(reasoning) or "None",
        )
    
    def _threat_to_severity(self, threat_level: ThreatLevel) -> str:
        """Convert threat level to severity."""
        mapping = {
            ThreatLevel.CLEAN: "low",
            ThreatLevel.SUSPICIOUS: "medium",
            ThreatLevel.MALICIOUS: "high",
            ThreatLevel.CRITICAL: "critical",
            ThreatLevel.UNKNOWN: "low",
        }
        return mapping.get(threat_level, "low")
    
    def _get_system_prompt(self) -> str:
        """Get system prompt for LLM reasoning."""
        return """You are an expert malware analyst. Analyze the provided file information and threat indicators.

Provide your assessment in this exact JSON format:
{
    "threat_level": "clean|suspicious|malicious",
    "confidence": 0.0-1.0,
    "reasoning": "Your detailed explanation",
    "concerns": ["concern1", "concern2"],
    "recommendations": ["recommendation1", "recommendation2"]
}

Be precise, technical, and base your assessment only on the evidence provided."""
    
    def _parse_llm_response(self, content: str) -> Dict[str, Any]:
        """Parse LLM response into structured format."""
        import json
        import re
        
        # Try to extract JSON from response
        json_match = re.search(r'\{[^{}]*\}', content, re.DOTALL)
        if json_match:
            try:
                return json.loads(json_match.group())
            except json.JSONDecodeError:
                pass
        
        # Try direct JSON parse
        try:
            return json.loads(content)
        except json.JSONDecodeError:
            pass
        
        # Fallback: extract information from text
        result = {
            "threat_level": "unknown",
            "confidence": 0.5,
            "reasoning": content[:500],
            "concerns": [],
            "recommendations": [],
        }
        
        lower_content = content.lower()
        if "malicious" in lower_content:
            result["threat_level"] = "malicious"
            result["confidence"] = 0.7
        elif "suspicious" in lower_content:
            result["threat_level"] = "suspicious"
            result["confidence"] = 0.6
        elif "clean" in lower_content or "benign" in lower_content:
            result["threat_level"] = "clean"
            result["confidence"] = 0.7
        
        return result
