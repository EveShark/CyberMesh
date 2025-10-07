"""
Kafka manager - orchestrates producer and consumer lifecycle.
"""
from typing import Optional
from .producer import AIProducer
from .consumer import AIConsumer
from ..config.settings import Settings
from ..utils.circuit_breaker import CircuitBreaker
# Logger type hint (optional dependency)


class KafkaManager:
    """
    Manages Kafka producer and consumer.
    
    Coordinates:
    - Producer initialization and lifecycle
    - Consumer initialization and lifecycle
    - Graceful shutdown
    """
    
    def __init__(
        self,
        config: Settings,
        circuit_breaker: CircuitBreaker,
        logger = None,
    ):
        self.config = config
        self.logger = logger
        
        self.producer = AIProducer(config, circuit_breaker)
        self.consumer = AIConsumer(config, logger)
        
        self._started = False
    
    def start(self):
        """Start consumer (producer is always ready)"""
        if self._started:
            return
        
        self.consumer.start()
        self._started = True
        
        if self.logger:
            self.logger.info("Kafka manager started")
    
    def stop(self):
        """Stop producer and consumer"""
        if not self._started:
            return
        
        self.producer.flush()
        self.producer.close()
        self.consumer.stop()
        
        self._started = False
        
        if self.logger:
            self.logger.info("Kafka manager stopped")
    
    def get_producer(self) -> AIProducer:
        """Get producer instance"""
        return self.producer
    
    def get_consumer(self) -> AIConsumer:
        """Get consumer instance"""
        return self.consumer
    
    def get_metrics(self) -> dict:
        """Get all Kafka metrics"""
        producer_metrics = self.producer.get_metrics()
        consumer_metrics = self.consumer.get_metrics()
        
        return {
            "producer": producer_metrics,
            "consumer": consumer_metrics,
        }
