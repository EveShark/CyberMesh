"""Runtime Kafka client adapters."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Dict, Optional

from sentinel.logging import get_logger

from .config import KafkaWorkerConfig

logger = get_logger(__name__)


@dataclass
class ConfluentRecord:
    """Adapter for confluent-kafka records."""

    topic: str
    value: bytes
    key: Optional[bytes] = None
    _raw: Any = None


class ConfluentKafkaClient:
    """confluent-kafka client implementing the worker's minimal interface."""

    def __init__(self, cfg: KafkaWorkerConfig):
        try:
            from confluent_kafka import Consumer, Producer  # type: ignore
        except Exception as exc:  # pylint: disable=broad-except
            raise RuntimeError(
                "confluent-kafka is required for Kafka gateway. Install sentinel[kafka]."
            ) from exc

        protocol = "SSL" if cfg.tls_enabled else "PLAINTEXT"
        if cfg.sasl_enabled:
            protocol = "SASL_SSL" if cfg.tls_enabled else "SASL_PLAINTEXT"

        common: Dict[str, Any] = {
            "bootstrap.servers": cfg.bootstrap_servers,
            "security.protocol": protocol,
        }
        if cfg.sasl_enabled:
            common["sasl.mechanism"] = cfg.sasl_mechanism
            common["sasl.username"] = cfg.sasl_username
            common["sasl.password"] = cfg.sasl_password

        consumer_cfg = dict(common)
        consumer_cfg.update(
            {
                "group.id": cfg.consumer_group_id,
                "auto.offset.reset": cfg.auto_offset_reset,
                "enable.auto.commit": False,
            }
        )
        producer_cfg = dict(common)
        producer_cfg.update({"acks": "all", "enable.idempotence": True})

        self._consumer = Consumer(consumer_cfg)
        self._producer = Producer(producer_cfg)
        topics = list(cfg.input_topics or (cfg.input_topic,))
        self._consumer.subscribe(topics)

    def poll(self, timeout_seconds: float) -> Optional[ConfluentRecord]:
        msg = self._consumer.poll(timeout=timeout_seconds)
        if msg is None:
            return None
        if msg.error():
            raise RuntimeError(f"kafka consume error: {msg.error()}")
        return ConfluentRecord(
            topic=msg.topic(),
            value=msg.value() or b"",
            key=msg.key(),
            _raw=msg,
        )

    def produce(
        self,
        topic: str,
        value: bytes,
        key: Optional[bytes] = None,
        headers: Optional[Dict[str, str]] = None,
    ) -> None:
        self._producer.produce(topic=topic, value=value, key=key, headers=headers or {})
        self._producer.flush(timeout=10.0)

    def commit(self, record: ConfluentRecord) -> None:
        if record._raw is None:
            return
        self._consumer.commit(message=record._raw, asynchronous=False)

    def close(self) -> None:
        try:
            self._consumer.close()
        finally:
            try:
                self._producer.flush(timeout=5.0)
            except Exception as exc:  # pylint: disable=broad-except
                logger.warning("kafka producer flush failed during close: %s", exc)
