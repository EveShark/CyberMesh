"""GLM (Zhipu AI) LLM provider."""

import time
from typing import Set, Optional

import httpx

from .base_llm import BaseLLMProvider
from ..base import AnalysisResult, ThreatLevel
from ...parsers.base import ParsedFile, FileType
from ...utils.errors import LLMError
from ...logging import get_logger

logger = get_logger(__name__)


class GLMProvider(BaseLLMProvider):
    """
    GLM-4 based analysis provider.
    
    Uses Zhipu AI's GLM models for:
    - Code analysis and understanding
    - Threat explanation
    - Behavioral reasoning
    """
    
    def __init__(
        self,
        api_key: str,
        api_base: str = "https://open.bigmodel.cn/api/paas/v4",
        model: str = "glm-4-plus",
        timeout: int = 30
    ):
        super().__init__(model=model, timeout=timeout)
        self.api_key = api_key
        self.api_base = api_base.rstrip("/")
    
    @property
    def name(self) -> str:
        return "glm_analyzer"
    
    @property
    def version(self) -> str:
        return f"1.0.0-{self.model}"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PE, FileType.PDF, FileType.OFFICE, FileType.SCRIPT}
    
    def get_cost_per_call(self) -> float:
        return 0.003
    
    def get_avg_latency_ms(self) -> float:
        return self._latency_ema
    
    def analyze(self, parsed_file: ParsedFile) -> AnalysisResult:
        t0 = time.perf_counter()
        
        try:
            prompt = self._build_prompt(parsed_file)
            response_text = self._call_llm(prompt)
            parsed_response = self._parse_response(response_text)
            
            latency = (time.perf_counter() - t0) * 1000
            self._latency_ema = 0.9 * self._latency_ema + 0.1 * latency
            
            return self._create_result(parsed_response, latency)
            
        except Exception as e:
            latency = (time.perf_counter() - t0) * 1000
            logger.error(f"GLM analysis failed: {e}")
            
            return AnalysisResult(
                provider_name=self.name,
                provider_version=self.version,
                threat_level=ThreatLevel.UNKNOWN,
                score=0.5,
                confidence=0.0,
                error=str(e),
                latency_ms=latency,
            )
    
    def _call_llm(self, prompt: str) -> str:
        """Call GLM API."""
        url = f"{self.api_base}/chat/completions"
        
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }
        
        payload = {
            "model": self.model,
            "messages": [
                {"role": "user", "content": prompt}
            ],
            "temperature": 0.1,
            "max_tokens": 1024,
        }
        
        try:
            with httpx.Client(timeout=self.timeout) as client:
                response = client.post(url, headers=headers, json=payload)
                response.raise_for_status()
                
                data = response.json()
                
                if "choices" in data and len(data["choices"]) > 0:
                    return data["choices"][0]["message"]["content"]
                
                raise LLMError(f"Unexpected GLM response format: {data}")
                
        except httpx.TimeoutException:
            raise LLMError("GLM API request timed out")
        except httpx.HTTPStatusError as e:
            raise LLMError(f"GLM API error: {e.response.status_code} - {e.response.text}")
        except Exception as e:
            raise LLMError(f"GLM API call failed: {e}")
