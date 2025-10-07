from typing import Dict, Optional
from prometheus_client import Counter, Histogram, Gauge, CollectorRegistry, REGISTRY
from .errors import MetricsError


class MetricsCollector:
    """
    Prometheus metrics collector for AI service operations.
    
    Tracks:
    - Message publish rates and sizes
    - Signature operations
    - Nonce generation/validation
    - Rate limit violations
    - Circuit breaker state transitions
    - Pipeline processing latency
    """
    
    def __init__(self, registry: Optional[CollectorRegistry] = None):
        self.registry = registry or REGISTRY
        
        self.messages_published = Counter(
            'ai_messages_published_total',
            'Total messages published to Kafka',
            ['message_type', 'status'],
            registry=self.registry
        )
        
        self.message_size_bytes = Histogram(
            'ai_message_size_bytes',
            'Message payload size distribution',
            ['message_type'],
            buckets=[1024, 4096, 16384, 65536, 262144, 1048576, 2097152],
            registry=self.registry
        )
        
        self.signature_operations = Counter(
            'ai_signature_operations_total',
            'Cryptographic signature operations',
            ['operation', 'status'],
            registry=self.registry
        )
        
        self.nonce_operations = Counter(
            'ai_nonce_operations_total',
            'Nonce generation and validation operations',
            ['operation', 'status'],
            registry=self.registry
        )
        
        self.rate_limit_violations = Counter(
            'ai_rate_limit_violations_total',
            'Rate limit violations',
            ['limiter_type'],
            registry=self.registry
        )
        
        self.circuit_breaker_state = Gauge(
            'ai_circuit_breaker_state',
            'Circuit breaker state (0=closed, 1=open, 2=half_open)',
            ['breaker_name'],
            registry=self.registry
        )
        
        self.circuit_breaker_transitions = Counter(
            'ai_circuit_breaker_transitions_total',
            'Circuit breaker state transitions',
            ['breaker_name', 'from_state', 'to_state'],
            registry=self.registry
        )
        
        self.pipeline_latency_seconds = Histogram(
            'ai_pipeline_latency_seconds',
            'Detection pipeline processing latency',
            ['pipeline_type'],
            buckets=[0.01, 0.05, 0.1, 0.5, 1.0, 5.0, 10.0],
            registry=self.registry
        )
        
        self.kafka_publish_latency_seconds = Histogram(
            'ai_kafka_publish_latency_seconds',
            'Kafka message publish latency',
            ['topic'],
            buckets=[0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0],
            registry=self.registry
        )
        
        self.kafka_retries = Counter(
            'ai_kafka_retries_total',
            'Kafka publish retry attempts',
            ['topic', 'reason'],
            registry=self.registry
        )
        
        self.dlq_messages = Counter(
            'ai_dlq_messages_total',
            'Messages sent to dead letter queue',
            ['topic', 'reason'],
            registry=self.registry
        )
        
        self.validation_failures = Counter(
            'ai_validation_failures_total',
            'Message validation failures',
            ['message_type', 'failure_reason'],
            registry=self.registry
        )
    
    def record_message_published(self, message_type: str, status: str, size_bytes: int):
        """Record a published message."""
        self.messages_published.labels(message_type=message_type, status=status).inc()
        self.message_size_bytes.labels(message_type=message_type).observe(size_bytes)
    
    def record_signature_operation(self, operation: str, status: str):
        """Record a signature operation (sign/verify)."""
        self.signature_operations.labels(operation=operation, status=status).inc()
    
    def record_nonce_operation(self, operation: str, status: str):
        """Record a nonce operation (generate/validate)."""
        self.nonce_operations.labels(operation=operation, status=status).inc()
    
    def record_rate_limit_violation(self, limiter_type: str):
        """Record a rate limit violation."""
        self.rate_limit_violations.labels(limiter_type=limiter_type).inc()
    
    def set_circuit_breaker_state(self, breaker_name: str, state_value: int):
        """Set circuit breaker state gauge."""
        self.circuit_breaker_state.labels(breaker_name=breaker_name).set(state_value)
    
    def record_circuit_breaker_transition(self, breaker_name: str, from_state: str, to_state: str):
        """Record a circuit breaker state transition."""
        self.circuit_breaker_transitions.labels(
            breaker_name=breaker_name,
            from_state=from_state,
            to_state=to_state
        ).inc()
    
    def record_pipeline_latency(self, pipeline_type: str, latency_seconds: float):
        """Record pipeline processing latency."""
        self.pipeline_latency_seconds.labels(pipeline_type=pipeline_type).observe(latency_seconds)
    
    def record_kafka_publish_latency(self, topic: str, latency_seconds: float):
        """Record Kafka publish latency."""
        self.kafka_publish_latency_seconds.labels(topic=topic).observe(latency_seconds)
    
    def record_kafka_retry(self, topic: str, reason: str):
        """Record a Kafka publish retry."""
        self.kafka_retries.labels(topic=topic, reason=reason).inc()
    
    def record_dlq_message(self, topic: str, reason: str):
        """Record a message sent to DLQ."""
        self.dlq_messages.labels(topic=topic, reason=reason).inc()
    
    def record_validation_failure(self, message_type: str, failure_reason: str):
        """Record a validation failure."""
        self.validation_failures.labels(
            message_type=message_type,
            failure_reason=failure_reason
        ).inc()


_global_collector: Optional[MetricsCollector] = None


def get_metrics_collector() -> MetricsCollector:
    """Get or create the global metrics collector."""
    global _global_collector
    if _global_collector is None:
        _global_collector = MetricsCollector()
    return _global_collector


def init_metrics(registry: Optional[CollectorRegistry] = None) -> MetricsCollector:
    """Initialize metrics collector with custom registry."""
    global _global_collector
    _global_collector = MetricsCollector(registry=registry)
    return _global_collector
