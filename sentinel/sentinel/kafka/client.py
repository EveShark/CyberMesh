"""Runtime Kafka client adapters."""

from __future__ import annotations

from dataclasses import dataclass
import time
from typing import Any, Dict, Optional

from sentinel.logging import get_logger
from sentinel.observability import inject_context_headers
from sentinel.utils.metrics import get_metrics_collector

from .config import KafkaWorkerConfig

logger = get_logger(__name__)
_metrics = get_metrics_collector()


@dataclass
class ConfluentRecord:
    """Adapter for confluent-kafka records."""

    topic: str
    value: bytes
    key: Optional[bytes] = None
    headers: Optional[list[tuple[str, Any]]] = None
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
        producer_cfg.update(
            {
                "acks": "all",
                "enable.idempotence": True,
                "linger.ms": cfg.producer_linger_ms,
            }
        )

        self._consumer = Consumer(consumer_cfg)
        self._producer = Producer(producer_cfg)
        self._queue_full_retries = cfg.producer_queue_full_retries
        self._queue_full_backoff = max(0.001, float(cfg.producer_queue_full_backoff_ms) / 1000.0)
        self._delivery_errors: list[str] = []
        topics = list(cfg.input_topics or (cfg.input_topic,))
        self._consumer.subscribe(topics)

    def poll(self, timeout_seconds: float) -> Optional[ConfluentRecord]:
        start = time.perf_counter()
        msg = self._consumer.poll(timeout=timeout_seconds)
        if msg is None:
            _metrics.record_operation("kafka_poll", time.perf_counter() - start, "empty")
            return None
        if msg.error():
            _metrics.record_operation("kafka_poll", time.perf_counter() - start, "error")
            raise RuntimeError(f"kafka consume error: {msg.error()}")
        _metrics.record_operation("kafka_poll", time.perf_counter() - start, "ok")
        return ConfluentRecord(
            topic=msg.topic(),
            value=msg.value() or b"",
            key=msg.key(),
            headers=msg.headers() or [],
            _raw=msg,
        )

    def produce(
        self,
        topic: str,
        value: bytes,
        key: Optional[bytes] = None,
        headers: Optional[Dict[str, str]] = None,
    ) -> None:
        start = time.perf_counter()
        last_error: Optional[Exception] = None
        for _ in range(self._queue_full_retries):
            try:
                otel_headers = inject_context_headers(existing_headers=headers or {})
                self._producer.produce(
                    topic=topic,
                    value=value,
                    key=key,
                    headers=otel_headers,
                    on_delivery=self._delivery_callback,
                )
                self._producer.poll(0)
                _metrics.record_operation("kafka_produce", time.perf_counter() - start, "ok")
                return
            except BufferError as exc:
                # Local producer queue is full; pump callbacks and retry quickly.
                last_error = exc
                self._producer.poll(self._queue_full_backoff)
                time.sleep(self._queue_full_backoff)
        _metrics.record_operation("kafka_produce", time.perf_counter() - start, "error")
        raise RuntimeError(f"kafka producer queue is full: {last_error}")

    def commit(self, record: ConfluentRecord) -> None:
        start = time.perf_counter()
        if record._raw is None:
            _metrics.record_operation("kafka_commit", time.perf_counter() - start, "skipped")
            return
        self._consumer.commit(message=record._raw, asynchronous=False)
        _metrics.record_operation("kafka_commit", time.perf_counter() - start, "ok")

    def flush(self, timeout_seconds: float) -> None:
        start = time.perf_counter()
        self._producer.poll(0)
        remaining = self._producer.flush(timeout=timeout_seconds)
        if self._delivery_errors:
            err = self._delivery_errors.pop(0)
            _metrics.record_operation("kafka_flush", time.perf_counter() - start, "error")
            raise RuntimeError(f"kafka delivery failure: {err}")
        if remaining > 0:
            _metrics.record_operation("kafka_flush", time.perf_counter() - start, "error")
            raise RuntimeError(f"kafka producer flush timed out with {remaining} undelivered messages")
        _metrics.record_operation("kafka_flush", time.perf_counter() - start, "ok")

    def close(self) -> None:
        try:
            self._consumer.close()
        finally:
            try:
                self.flush(timeout_seconds=5.0)
            except Exception as exc:  # pylint: disable=broad-except
                logger.warning("kafka producer flush failed during close: %s", exc)

    def _delivery_callback(self, err: Any, msg: Any) -> None:
        _ = msg
        if err is not None:
            self._delivery_errors.append(str(err))
