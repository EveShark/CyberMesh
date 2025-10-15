"""
Kafka integration layer for AI ↔ Backend communication.
"""
from .producer import AIProducer
from .consumer import AIConsumer
from .manager import KafkaManager

__all__ = [
    "AIProducer",
    "AIConsumer",
    "KafkaManager",
]
