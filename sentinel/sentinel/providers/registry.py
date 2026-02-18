"""Provider registry with weight management."""

import json
import time
from dataclasses import dataclass, field, asdict
from pathlib import Path
from typing import Dict, List, Optional, Any

from .base import Provider, AnalysisResult
from ..parsers.base import FileType
from ..utils.errors import ProviderError
from ..logging import get_logger

logger = get_logger(__name__)


@dataclass
class ProviderStats:
    """Statistics and weight tracking for a provider."""
    name: str
    version: str
    
    weight: float = 0.5
    baseline_weight: float = 0.5
    
    accuracy: float = 0.0
    total_calls: int = 0
    correct_predictions: int = 0
    
    avg_latency_ms: float = 0.0
    cost_per_call: float = 0.0
    
    last_eval_timestamp: float = 0.0
    enabled: bool = True
    
    supported_types: List[str] = field(default_factory=list)
    
    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "ProviderStats":
        return cls(**data)


class ProviderRegistry:
    """
    Central registry for all detection providers.
    
    Manages provider instances, tracks statistics, and persists
    weights to enable continuous learning.
    """
    
    def __init__(self, registry_path: Optional[str] = None):
        """
        Initialize provider registry.
        
        Args:
            registry_path: Path to persist registry data
        """
        self.registry_path = Path(registry_path) if registry_path else None
        self.providers: Dict[str, Provider] = {}
        self.stats: Dict[str, ProviderStats] = {}
        
        if self.registry_path and self.registry_path.exists():
            self._load_registry()
    
    def register(
        self,
        provider: Provider,
        baseline_weight: Optional[float] = None,
        run_eval: bool = False,
        eval_suite: Optional[Any] = None
    ) -> ProviderStats:
        """
        Register a new provider.
        
        Args:
            provider: Provider instance to register
            baseline_weight: Initial weight (defaults to 0.5, or from benchmark)
            run_eval: Whether to run evaluation suite for baseline weight
            eval_suite: Optional EvalSuite instance for benchmarking
            
        Returns:
            ProviderStats for the registered provider
        """
        name = provider.name
        
        if name in self.providers:
            logger.warning(f"Provider {name} already registered, updating")
        
        self.providers[name] = provider
        
        # Determine baseline weight
        computed_weight = baseline_weight
        
        # Run benchmark if requested
        if run_eval:
            computed_weight = self._run_benchmark(provider, eval_suite)
            if computed_weight is None:
                computed_weight = baseline_weight if baseline_weight is not None else 0.5
        
        if computed_weight is None:
            computed_weight = 0.5
        
        if name in self.stats:
            stats = self.stats[name]
            stats.version = provider.version
            stats.avg_latency_ms = provider.get_avg_latency_ms()
            stats.cost_per_call = provider.get_cost_per_call()
            stats.supported_types = [t.value for t in provider.supported_types]
            # Update baseline if we ran eval
            if run_eval:
                stats.baseline_weight = computed_weight
                stats.weight = max(stats.weight, computed_weight)
                stats.last_eval_timestamp = time.time()
        else:
            stats = ProviderStats(
                name=name,
                version=provider.version,
                weight=computed_weight,
                baseline_weight=computed_weight,
                avg_latency_ms=provider.get_avg_latency_ms(),
                cost_per_call=provider.get_cost_per_call(),
                supported_types=[t.value for t in provider.supported_types],
                last_eval_timestamp=time.time() if run_eval else 0.0,
            )
            self.stats[name] = stats
        
        logger.info(f"Registered provider: {name} v{provider.version} (weight={stats.weight:.3f})")
        self._save_registry()
        
        return stats
    
    def _run_benchmark(self, provider: Provider, eval_suite: Optional[Any] = None) -> Optional[float]:
        """Run benchmark and return baseline weight."""
        try:
            from ..eval import EvalSuite, ProviderBenchmark
            
            if eval_suite is None:
                eval_suite = EvalSuite()
            
            if eval_suite.sample_count == 0:
                logger.warning("No eval samples available for benchmarking")
                return None
            
            benchmarker = ProviderBenchmark(eval_suite)
            result = benchmarker.benchmark(provider)
            
            logger.info(
                f"Benchmark for {provider.name}: "
                f"accuracy={result.accuracy:.1%}, F1={result.f1_score:.3f}, "
                f"baseline_weight={result.baseline_weight:.3f}"
            )
            
            return result.baseline_weight
            
        except Exception as e:
            logger.error(f"Benchmark failed for {provider.name}: {e}")
            return None
    
    def unregister(self, name: str) -> None:
        """Unregister a provider."""
        if name in self.providers:
            del self.providers[name]
        if name in self.stats:
            self.stats[name].enabled = False
        self._save_registry()
        logger.info(f"Unregistered provider: {name}")
    
    def get_provider(self, name: str) -> Optional[Provider]:
        """Get provider by name."""
        return self.providers.get(name)
    
    def get_stats(self, name: str) -> Optional[ProviderStats]:
        """Get provider stats by name."""
        return self.stats.get(name)
    
    def get_providers_for_type(self, file_type: FileType) -> List[Provider]:
        """
        Get all providers that support a file type.
        
        Args:
            file_type: File type to filter by
            
        Returns:
            List of compatible providers, sorted by weight descending
        """
        compatible = []
        for name, provider in self.providers.items():
            stats = self.stats.get(name)
            if stats and not stats.enabled:
                continue
            if file_type in provider.supported_types:
                weight = stats.weight if stats else 0.5
                compatible.append((provider, weight))
        
        compatible.sort(key=lambda x: x[1], reverse=True)
        return [p for p, _ in compatible]
    
    def get_all_providers(self) -> List[Provider]:
        """Get all registered providers."""
        return list(self.providers.values())
    
    def update_weight(
        self,
        name: str,
        was_correct: bool,
        alpha: float = 0.1
    ) -> None:
        """
        Update provider weight based on feedback.
        
        Uses exponential moving average (EMA) to blend
        live accuracy with baseline weight.
        
        Args:
            name: Provider name
            was_correct: Whether prediction was correct
            alpha: EMA decay factor (default 0.1)
        """
        stats = self.stats.get(name)
        if not stats:
            logger.warning(f"Cannot update weight for unknown provider: {name}")
            return
        
        stats.total_calls += 1
        if was_correct:
            stats.correct_predictions += 1
        
        new_accuracy = 1.0 if was_correct else 0.0
        stats.accuracy = (1 - alpha) * stats.accuracy + alpha * new_accuracy
        
        stats.weight = 0.6 * stats.accuracy + 0.4 * stats.baseline_weight
        stats.weight = max(0.1, min(1.0, stats.weight))
        
        self._save_registry()
        
        logger.debug(
            f"Updated {name} weight: {stats.weight:.3f} "
            f"(accuracy={stats.accuracy:.3f}, calls={stats.total_calls})"
        )
    
    def update_latency(self, name: str, latency_ms: float, alpha: float = 0.1) -> None:
        """Update provider latency EMA."""
        stats = self.stats.get(name)
        if stats:
            stats.avg_latency_ms = (1 - alpha) * stats.avg_latency_ms + alpha * latency_ms
    
    def set_baseline_weight(self, name: str, weight: float) -> None:
        """Set baseline weight from evaluation."""
        stats = self.stats.get(name)
        if stats:
            stats.baseline_weight = weight
            stats.weight = max(stats.weight, weight)
            stats.last_eval_timestamp = time.time()
            self._save_registry()
            logger.info(f"Set baseline weight for {name}: {weight:.3f}")
    
    def _load_registry(self) -> None:
        """Load registry from disk."""
        try:
            with open(self.registry_path, "r") as f:
                data = json.load(f)
            
            for name, stats_dict in data.get("stats", {}).items():
                self.stats[name] = ProviderStats.from_dict(stats_dict)
            
            logger.info(f"Loaded registry with {len(self.stats)} provider stats")
            
        except Exception as e:
            logger.warning(f"Failed to load registry: {e}")
    
    def _save_registry(self) -> None:
        """Save registry to disk."""
        if not self.registry_path:
            return
        
        try:
            self.registry_path.parent.mkdir(parents=True, exist_ok=True)
            
            data = {
                "stats": {name: stats.to_dict() for name, stats in self.stats.items()},
                "last_updated": time.time(),
            }
            
            with open(self.registry_path, "w") as f:
                json.dump(data, f, indent=2)
            
        except Exception as e:
            logger.error(f"Failed to save registry: {e}")
    
    def get_summary(self) -> Dict[str, Any]:
        """Get registry summary."""
        return {
            "total_providers": len(self.providers),
            "enabled_providers": sum(1 for s in self.stats.values() if s.enabled),
            "providers": [
                {
                    "name": s.name,
                    "version": s.version,
                    "weight": s.weight,
                    "accuracy": s.accuracy,
                    "total_calls": s.total_calls,
                    "enabled": s.enabled,
                }
                for s in self.stats.values()
            ],
        }
