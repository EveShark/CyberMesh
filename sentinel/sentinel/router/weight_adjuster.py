"""Weight adjustment based on feedback."""

from typing import List, Tuple

from ..providers.registry import ProviderRegistry
from ..logging import get_logger

logger = get_logger(__name__)


class WeightAdjuster:
    """
    Adjusts provider weights based on live feedback.
    
    Algorithm:
    1. Receive feedback (provider_name, was_correct)
    2. Update running accuracy with EMA
    3. Blend accuracy with baseline weight
    4. Persist updated weights
    """
    
    def __init__(self, registry: ProviderRegistry, alpha: float = 0.1):
        """
        Initialize weight adjuster.
        
        Args:
            registry: Provider registry to update
            alpha: EMA decay factor (default 0.1)
        """
        self.registry = registry
        self.alpha = alpha
    
    def record_feedback(self, provider_name: str, was_correct: bool) -> None:
        """
        Record feedback for a provider prediction.
        
        Args:
            provider_name: Provider that made the prediction
            was_correct: Whether prediction matched ground truth
        """
        self.registry.update_weight(provider_name, was_correct, self.alpha)
        
        logger.debug(
            f"Recorded feedback for {provider_name}: "
            f"correct={was_correct}, alpha={self.alpha}"
        )
    
    def record_batch_feedback(self, feedbacks: List[Tuple[str, bool]]) -> None:
        """
        Record multiple feedbacks at once.
        
        Args:
            feedbacks: List of (provider_name, was_correct) tuples
        """
        for provider_name, was_correct in feedbacks:
            self.record_feedback(provider_name, was_correct)
    
    def record_analysis_result(
        self,
        provider_name: str,
        predicted_malicious: bool,
        actual_malicious: bool
    ) -> None:
        """
        Record feedback based on analysis result.
        
        Args:
            provider_name: Provider name
            predicted_malicious: Provider's prediction
            actual_malicious: Ground truth
        """
        was_correct = predicted_malicious == actual_malicious
        self.record_feedback(provider_name, was_correct)
    
    def get_weight(self, provider_name: str) -> float:
        """Get current weight for a provider."""
        stats = self.registry.get_stats(provider_name)
        return stats.weight if stats else 0.5
    
    def get_accuracy(self, provider_name: str) -> float:
        """Get current accuracy for a provider."""
        stats = self.registry.get_stats(provider_name)
        return stats.accuracy if stats else 0.0
    
    def reset_provider(self, provider_name: str) -> None:
        """Reset provider stats to baseline."""
        stats = self.registry.get_stats(provider_name)
        if stats:
            stats.weight = stats.baseline_weight
            stats.accuracy = 0.0
            stats.total_calls = 0
            stats.correct_predictions = 0
            self.registry._save_registry()
            logger.info(f"Reset stats for provider: {provider_name}")
