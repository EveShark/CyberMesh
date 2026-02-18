"""Qwen (Alibaba) LLM provider."""

import time
from typing import Set

import httpx

from .base_llm import BaseLLMProvider
from ..base import AnalysisResult, ThreatLevel
from ...parsers.base import ParsedFile, FileType
from ...utils.errors import LLMError
from ...logging import get_logger

logger = get_logger(__name__)


class QwenProvider(BaseLLMProvider):
    """
    Qwen-based analysis provider.
    
    Uses Alibaba's Qwen models via DashScope API for:
    - Code analysis and understanding
    - Threat explanation
    - Best for code-heavy analysis
    """
    
    def __init__(
        self,
        api_key: str,
        api_base: str = "https://dashscope.aliyuncs.com/api/v1",
        model: str = "qwen-max",
        timeout: int = 30
    ):
        super().__init__(model=model, timeout=timeout)
        self.api_key = api_key
        self.api_base = api_base.rstrip("/")
    
    @property
    def name(self) -> str:
        return "qwen_analyzer"
    
    @property
    def version(self) -> str:
        return f"1.0.0-{self.model}"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PE, FileType.PDF, FileType.OFFICE, FileType.SCRIPT}
    
    def get_cost_per_call(self) -> float:
        return 0.004  # Slightly more expensive than GLM
    
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
            logger.error(f"Qwen analysis failed: {e}")
            
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
        """Call Qwen API via DashScope."""
        url = f"{self.api_base}/services/aigc/text-generation/generation"
        
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }
        
        payload = {
            "model": self.model,
            "input": {
                "messages": [
                    {"role": "user", "content": prompt}
                ]
            },
            "parameters": {
                "temperature": 0.1,
                "max_tokens": 1024,
            }
        }
        
        try:
            with httpx.Client(timeout=self.timeout) as client:
                response = client.post(url, headers=headers, json=payload)
                response.raise_for_status()
                
                data = response.json()
                
                # DashScope response format
                if "output" in data and "text" in data["output"]:
                    return data["output"]["text"]
                
                # Alternative format
                if "output" in data and "choices" in data["output"]:
                    choices = data["output"]["choices"]
                    if choices and "message" in choices[0]:
                        return choices[0]["message"]["content"]
                
                raise LLMError(f"Unexpected Qwen response format: {data}")
                
        except httpx.TimeoutException:
            raise LLMError("Qwen API request timed out")
        except httpx.HTTPStatusError as e:
            raise LLMError(f"Qwen API error: {e.response.status_code} - {e.response.text}")
        except Exception as e:
            raise LLMError(f"Qwen API call failed: {e}")
