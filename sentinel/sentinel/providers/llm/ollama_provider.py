"""Ollama local LLM provider."""

import time
from typing import Set

import httpx

from .base_llm import BaseLLMProvider
from ..base import AnalysisResult, ThreatLevel
from ...parsers.base import ParsedFile, FileType
from ...utils.errors import LLMError
from ...logging import get_logger

logger = get_logger(__name__)


class OllamaProvider(BaseLLMProvider):
    """
    Ollama-based local analysis provider.
    
    Uses locally running Ollama for privacy-focused analysis.
    No data leaves the machine.
    """
    
    def __init__(
        self,
        host: str = "http://localhost:11434",
        model: str = "llama3.1:8b",
        timeout: int = 60
    ):
        super().__init__(model=model, timeout=timeout)
        self.host = host.rstrip("/")
        self._available = None
    
    @property
    def name(self) -> str:
        return "ollama_analyzer"
    
    @property
    def version(self) -> str:
        return f"1.0.0-{self.model}"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PE, FileType.PDF, FileType.OFFICE, FileType.SCRIPT}
    
    def get_cost_per_call(self) -> float:
        return 0.0  # Free, runs locally
    
    def get_avg_latency_ms(self) -> float:
        return self._latency_ema
    
    def is_available(self) -> bool:
        """Check if Ollama is running."""
        if self._available is not None:
            return self._available
        
        try:
            with httpx.Client(timeout=5) as client:
                response = client.get(f"{self.host}/api/tags")
                self._available = response.status_code == 200
        except Exception:
            self._available = False
        
        return self._available
    
    def analyze(self, parsed_file: ParsedFile) -> AnalysisResult:
        t0 = time.perf_counter()
        
        if not self.is_available():
            return AnalysisResult(
                provider_name=self.name,
                provider_version=self.version,
                threat_level=ThreatLevel.UNKNOWN,
                score=0.5,
                confidence=0.0,
                error="Ollama not available",
                latency_ms=(time.perf_counter() - t0) * 1000,
            )
        
        try:
            prompt = self._build_prompt(parsed_file)
            response_text = self._call_llm(prompt)
            parsed_response = self._parse_response(response_text)
            
            latency = (time.perf_counter() - t0) * 1000
            self._latency_ema = 0.9 * self._latency_ema + 0.1 * latency
            
            return self._create_result(parsed_response, latency)
            
        except Exception as e:
            latency = (time.perf_counter() - t0) * 1000
            logger.error(f"Ollama analysis failed: {e}")
            
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
        """Call Ollama API."""
        url = f"{self.host}/api/generate"
        
        payload = {
            "model": self.model,
            "prompt": prompt,
            "stream": False,
            "options": {
                "temperature": 0.1,
                "num_predict": 1024,
            }
        }
        
        try:
            with httpx.Client(timeout=self.timeout) as client:
                response = client.post(url, json=payload)
                response.raise_for_status()
                
                data = response.json()
                
                if "response" in data:
                    return data["response"]
                
                raise LLMError(f"Unexpected Ollama response format: {data}")
                
        except httpx.TimeoutException:
            raise LLMError("Ollama request timed out")
        except httpx.HTTPStatusError as e:
            raise LLMError(f"Ollama error: {e.response.status_code}")
        except httpx.ConnectError:
            self._available = False
            raise LLMError("Ollama not running - start with 'ollama serve'")
        except Exception as e:
            raise LLMError(f"Ollama call failed: {e}")
