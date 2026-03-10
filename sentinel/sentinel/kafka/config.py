"""Environment-driven config for the standalone Kafka gateway."""

from __future__ import annotations

import os
from dataclasses import dataclass


def _get_env(name: str, default: str = "") -> str:
    return os.getenv(name, default)


def _get_bool(name: str, default: bool) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    return value.strip().lower() in ("1", "true", "yes", "on")


def _get_int(name: str, default: int) -> int:
    value = os.getenv(name)
    if value is None:
        return default
    return int(value)


def _get_float(name: str, default: float) -> float:
    value = os.getenv(name)
    if value is None:
        return default
    return float(value)


def _parse_duration_seconds(value: str, default_seconds: int) -> int:
    raw = (value or "").strip().lower()
    if not raw:
        return default_seconds
    if raw.endswith("ms"):
        return max(int(float(raw[:-2]) / 1000.0), 0)
    if raw.endswith("s"):
        return max(int(float(raw[:-1])), 0)
    if raw.endswith("m"):
        return max(int(float(raw[:-1]) * 60), 0)
    if raw.endswith("h"):
        return max(int(float(raw[:-1]) * 3600), 0)
    return max(int(float(raw)), 0)


@dataclass(frozen=True)
class KafkaWorkerConfig:
    """Standalone Kafka worker config."""

    enabled: bool
    bootstrap_servers: str
    tls_enabled: bool
    sasl_enabled: bool
    sasl_mechanism: str
    sasl_username: str
    sasl_password: str
    input_topic: str
    input_topics: tuple[str, ...]
    topic_encoding_map: dict[str, str]
    topic_schema_map: dict[str, str]
    output_topic: str
    output_encoding: str
    dlq_topic: str
    consumer_group_id: str
    auto_offset_reset: str
    max_message_size: int
    max_timestamp_skew_seconds: int
    require_nonzero_duration_for_counted_flows: bool
    poll_timeout_seconds: float
    producer_linger_ms: int
    producer_flush_interval_ms: int
    producer_flush_timeout_seconds: float
    producer_max_pending_commits: int
    producer_queue_full_retries: int
    producer_queue_full_backoff_ms: int
    gateway_backpressure_sleep_ms: int

    def validate(self) -> None:
        if self.enabled and not self.bootstrap_servers:
            raise ValueError("KAFKA_BOOTSTRAP_SERVERS must be set when ENABLE_KAFKA=true")
        if self.max_message_size <= 0:
            raise ValueError("KAFKA_MAX_MESSAGE_SIZE must be > 0")
        if self.poll_timeout_seconds <= 0:
            raise ValueError("poll_timeout_seconds must be > 0")
        if self.producer_linger_ms < 0:
            raise ValueError("producer_linger_ms must be >= 0")
        if self.producer_flush_interval_ms <= 0:
            raise ValueError("producer_flush_interval_ms must be > 0")
        if self.producer_flush_timeout_seconds <= 0:
            raise ValueError("producer_flush_timeout_seconds must be > 0")
        if self.producer_max_pending_commits <= 0:
            raise ValueError("producer_max_pending_commits must be > 0")
        if self.producer_queue_full_retries <= 0:
            raise ValueError("producer_queue_full_retries must be > 0")
        if self.producer_queue_full_backoff_ms <= 0:
            raise ValueError("producer_queue_full_backoff_ms must be > 0")
        if self.gateway_backpressure_sleep_ms <= 0:
            raise ValueError("gateway_backpressure_sleep_ms must be > 0")
        if not self.input_topic:
            raise ValueError("Kafka input topic must be non-empty")
        if not self.input_topics:
            raise ValueError("Kafka input topics must be non-empty")
        if not self.output_topic:
            raise ValueError("Kafka output topic must be non-empty")
        if not self.dlq_topic:
            raise ValueError("Kafka DLQ topic must be non-empty")
        if self.output_encoding not in ("json", "protobuf"):
            raise ValueError("KAFKA_OUTPUT_ENCODING must be one of: json, protobuf")


def _first_csv_token(value: str) -> str:
    parts = [item.strip() for item in (value or "").split(",")]
    return next((p for p in parts if p), "")


def _parse_csv(value: str) -> list[str]:
    parts = [item.strip() for item in (value or "").split(",")]
    return [p for p in parts if p]


def _parse_topic_map(value: str) -> dict[str, str]:
    """
    Parse topic map env format:
      "topicA:valueA,topicB:valueB"
    """
    mapping: dict[str, str] = {}
    for item in _parse_csv(value):
        if ":" not in item:
            continue
        topic, mapped = item.split(":", 1)
        topic = topic.strip()
        mapped = mapped.strip()
        if topic and mapped:
            mapping[topic] = mapped
    return mapping


def _default_schema_for_topic(topic: str) -> str:
    t = topic.strip().lower()
    if "telemetry.features" in t:
        return "cic_v1"
    if "telemetry.flow" in t:
        return "flow_v1"
    if "telemetry.deepflow" in t:
        return "deepflow_v1"
    return "canonical_event"


def load_kafka_worker_config() -> KafkaWorkerConfig:
    """Load Kafka worker config from env with k8s-compatible fallbacks."""

    input_topic_csv = _get_env("KAFKA_INPUT_TOPICS")
    first_input = _get_env("KAFKA_INPUT_TOPIC") or _first_csv_token(input_topic_csv)
    if not first_input:
        first_input = _get_env("TOPIC_TELEMETRY_FEATURES") or "telemetry.features.v1"
    input_topics = _parse_csv(input_topic_csv) if input_topic_csv else [first_input]
    if first_input not in input_topics:
        input_topics.insert(0, first_input)

    topic_encoding_map = _parse_topic_map(_get_env("KAFKA_TOPIC_ENCODING_MAP", ""))
    topic_schema_map = _parse_topic_map(_get_env("KAFKA_TOPIC_SCHEMA_MAP", ""))
    for topic in input_topics:
        topic_encoding_map.setdefault(topic, _get_env("KAFKA_INPUT_ENCODING", "json").strip().lower() or "json")
        topic_schema_map.setdefault(topic, _default_schema_for_topic(topic))

    output_topic = (
        _get_env("KAFKA_OUTPUT_TOPIC")
        or _get_env("TOPIC_AI_ANOMALIES")
        or "ai.anomalies.v1"
    )
    dlq_topic = (
        _get_env("KAFKA_DLQ_TOPIC")
        or _get_env("TOPIC_DLQ")
        or "ai.dlq.v1"
    )
    skew_seconds = _parse_duration_seconds(_get_env("KAFKA_MAX_TIMESTAMP_SKEW", "5m"), 300)

    cfg = KafkaWorkerConfig(
        enabled=_get_bool("ENABLE_KAFKA", False),
        bootstrap_servers=_get_env("KAFKA_BOOTSTRAP_SERVERS"),
        tls_enabled=_get_bool("KAFKA_TLS_ENABLED", True),
        sasl_enabled=_get_bool("KAFKA_SASL_ENABLED", False),
        sasl_mechanism=_get_env("KAFKA_SASL_MECHANISM", "PLAIN"),
        sasl_username=_get_env("KAFKA_SASL_USERNAME"),
        sasl_password=_get_env("KAFKA_SASL_PASSWORD"),
        input_topic=first_input,
        input_topics=tuple(input_topics),
        topic_encoding_map=topic_encoding_map,
        topic_schema_map=topic_schema_map,
        output_topic=output_topic,
        output_encoding=_get_env("KAFKA_OUTPUT_ENCODING", "protobuf").strip().lower() or "protobuf",
        dlq_topic=dlq_topic,
        consumer_group_id=_get_env("KAFKA_CONSUMER_GROUP_ID", "sentinel-standalone"),
        auto_offset_reset=_get_env("KAFKA_CONSUMER_AUTO_OFFSET_RESET", "latest"),
        max_message_size=_get_int("KAFKA_MAX_MESSAGE_SIZE", 1_048_576),
        max_timestamp_skew_seconds=skew_seconds,
        require_nonzero_duration_for_counted_flows=_get_bool(
            "KAFKA_REQUIRE_NONZERO_DURATION_FOR_COUNTED_FLOWS",
            False,
        ),
        poll_timeout_seconds=float(_get_env("KAFKA_POLL_TIMEOUT_SECONDS", "1.0")),
        producer_linger_ms=_get_int("KAFKA_PRODUCER_LINGER_MS", 10),
        producer_flush_interval_ms=_get_int("KAFKA_PRODUCER_FLUSH_INTERVAL_MS", 200),
        producer_flush_timeout_seconds=_get_float("KAFKA_PRODUCER_FLUSH_TIMEOUT_SECONDS", 2.0),
        producer_max_pending_commits=_get_int("KAFKA_PRODUCER_MAX_PENDING_COMMITS", 64),
        producer_queue_full_retries=_get_int("KAFKA_PRODUCER_QUEUE_FULL_RETRIES", 5),
        producer_queue_full_backoff_ms=_get_int("KAFKA_PRODUCER_QUEUE_FULL_BACKOFF_MS", 10),
        gateway_backpressure_sleep_ms=_get_int("KAFKA_GATEWAY_BACKPRESSURE_SLEEP_MS", 20),
    )
    cfg.validate()
    return cfg
