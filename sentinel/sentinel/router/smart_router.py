"""Smart provider routing based on weights, cost, and latency."""

from dataclasses import dataclass
from enum import Enum
from typing import List, Optional

from ..providers.registry import ProviderRegistry
from ..parsers.base import FileType
from ..logging import get_logger

logger = get_logger(__name__)


class RoutingStrategy(str, Enum):
    """Provider selection strategies."""
    SINGLE_BEST = "single_best"
    ENSEMBLE = "ensemble"
    CASCADE = "cascade"
    COST_OPTIMIZED = "cost_optimized"


@dataclass
class RoutingConfig:
    """Router configuration."""
    strategy: RoutingStrategy = RoutingStrategy.ENSEMBLE
    max_providers: int = 3
    max_cost_per_file: float = 0.01
    max_latency_ms: float = 5000
    min_confidence: float = 0.7


class SmartRouter:
    """
    Intelligent provider selection and routing.
    
    Selects providers based on:
    - File type compatibility
    - Provider weight (accuracy from benchmarks + live feedback)
    - Cost constraints
    - Latency requirements
    """
    
    def __init__(self, registry: ProviderRegistry, config: Optional[RoutingConfig] = None):
        self.registry = registry
        self.config = config or RoutingConfig()
    
    def route(self, file_type: FileType) -> List[str]:
        """
        Select providers for a file type.
        
        Args:
            file_type: File type to analyze
            
        Returns:
            List of provider names to use (ordered by priority)
        """
        compatible = self._get_compatible_providers(file_type)
        
        if not compatible:
            logger.warning(f"No providers available for file type: {file_type.value}")
            return []
        
        if self.config.strategy == RoutingStrategy.SINGLE_BEST:
            return self._select_single_best(compatible)
        
        elif self.config.strategy == RoutingStrategy.ENSEMBLE:
            return self._select_ensemble(compatible)
        
        elif self.config.strategy == RoutingStrategy.CASCADE:
            return self._select_cascade(compatible)
        
        elif self.config.strategy == RoutingStrategy.COST_OPTIMIZED:
            return self._select_cost_optimized(compatible)
        
        return [compatible[0][0]]
    
    def _get_compatible_providers(self, file_type: FileType) -> List[tuple]:
        """Get providers compatible with file type, with stats."""
        compatible = []
        
        for name, provider in self.registry.providers.items():
            stats = self.registry.stats.get(name)
            if stats and not stats.enabled:
                continue
            
            if file_type in provider.supported_types:
                weight = stats.weight if stats else 0.5
                cost = stats.cost_per_call if stats else 0.0
                latency = stats.avg_latency_ms if stats else 1000
                compatible.append((name, weight, cost, latency))
        
        compatible.sort(key=lambda x: x[1], reverse=True)
        return compatible
    
    def _select_single_best(self, providers: List[tuple]) -> List[str]:
        """Select single best provider by weight."""
        if providers:
            return [providers[0][0]]
        return []
    
    def _select_ensemble(self, providers: List[tuple]) -> List[str]:
        """Select top N providers for ensemble."""
        selected = []
        total_cost = 0.0
        
        for name, weight, cost, latency in providers:
            if total_cost + cost > self.config.max_cost_per_file:
                continue
            
            selected.append(name)
            total_cost += cost
            
            if len(selected) >= self.config.max_providers:
                break
        
        logger.debug(f"Ensemble selection: {selected} (total_cost=${total_cost:.4f})")
        return selected
    
    def _select_cascade(self, providers: List[tuple]) -> List[str]:
        """Select providers for cascade (cheap/fast first)."""
        sorted_providers = sorted(providers, key=lambda x: (x[2], -x[1]))
        return [p[0] for p in sorted_providers[:self.config.max_providers]]
    
    def _select_cost_optimized(self, providers: List[tuple]) -> List[str]:
        """Select providers optimizing for cost within quality threshold."""
        min_weight = 0.4
        
        filtered = [p for p in providers if p[1] >= min_weight]
        
        if not filtered:
            filtered = providers[:3]
        
        filtered.sort(key=lambda x: x[2])
        
        return [p[0] for p in filtered[:self.config.max_providers]]
    
    def get_provider_info(self, file_type: FileType) -> dict:
        """Get information about providers for a file type."""
        compatible = self._get_compatible_providers(file_type)
        
        return {
            "file_type": file_type.value,
            "available_providers": len(compatible),
            "providers": [
                {
                    "name": name,
                    "weight": weight,
                    "cost": cost,
                    "latency_ms": latency,
                }
                for name, weight, cost, latency in compatible
            ],
            "selected": self.route(file_type),
            "strategy": self.config.strategy.value,
        }
