from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import os


@dataclass(frozen=True)
class KafkaConfig:
    brokers: str
    input_topic: str
    output_topic: str
    dlq_topic: str
    consumer_group: str
    client_id: str
    auto_offset_reset: str
    tls_enabled: bool
    tls_ca_path: str
    tls_cert_path: str
    tls_key_path: str
    sasl_enabled: bool
    sasl_mechanism: str
    sasl_username: str
    sasl_password: str
    consumer_fetch_min_bytes: int
    consumer_fetch_max_wait_ms: int
    producer_acks: str
    producer_linger_ms: int
    producer_batch_size: int
    producer_compression_type: str
    producer_request_timeout_ms: int


@dataclass(frozen=True)
class FeatureConfig:
    schema_version: str
    feature_mask_enabled: bool
    output_mode: str
    input_encoding: str
    output_encoding: str
    registry_enabled: bool
    registry_url: str
    registry_subject_flow: str
    registry_subject_feature: str
    registry_timeout_sec: int
    registry_allow_fallback: bool
    registry_username: str
    registry_password: str
    derivation_policy: str
    dry_run: bool
    min_feature_coverage: float
    healthcheck_only: bool
    metrics_port: int
    role: str
    edge_source_ids: list[str]
    edge_source_types: list[str]
    max_messages: int
    consumer_poll_timeout_sec: float
    consumer_drain_max_messages: int
    consumer_drain_max_ms: int
    commit_mode: str
    commit_batch_size: int
    commit_interval_ms: int
    producer_sync_flush: bool
    producer_flush_interval_ms: int


@dataclass(frozen=True)
class PostgresConfig:
    enabled: bool
    dsn: str
    table: str


@dataclass(frozen=True)
class AppConfig:
    kafka: KafkaConfig
    features: FeatureConfig
    postgres: PostgresConfig


def _load_env_file(env_path: Path) -> None:
    if not env_path.exists():
        return
    for line in env_path.read_text(encoding="utf-8").splitlines():
        if not line or line.strip().startswith("#"):
            continue
        if "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip()
        if key and key not in os.environ:
            os.environ[key] = value


def _get_env(name: str, default: str | None = None) -> str | None:
    value = os.getenv(name)
    if value is None or value == "":
        return default
    return value


def _get_bool(name: str, default: bool = False) -> bool:
    value = _get_env(name)
    if value is None:
        return default
    return value.strip().lower() in {"1", "true", "yes", "on"}


def _require(name: str) -> str:
    value = _get_env(name)
    if value is None or value == "":
        raise ValueError(f"{name} is required")
    return value


def load_config() -> AppConfig:
    base_dir = Path(__file__).resolve().parents[1]
    env_path = Path(_get_env("TELEMETRY_ENV_FILE", str(base_dir / ".env")))
    _load_env_file(env_path)

    brokers = _get_env("KAFKA_BOOTSTRAP_SERVERS") or _require("KAFKA_BROKERS")
    input_topic = _require("FEATURE_INPUT_TOPIC")
    output_topic = _require("FEATURE_OUTPUT_TOPIC")
    dlq_topic = _require("FEATURE_DLQ_TOPIC")

    kafka = KafkaConfig(
        brokers=brokers,
        input_topic=input_topic,
        output_topic=output_topic,
        dlq_topic=dlq_topic,
        consumer_group=_require("KAFKA_CONSUMER_GROUP"),
        client_id=_get_env("KAFKA_CLIENT_ID", "telemetry-feature-transformer") or "telemetry-feature-transformer",
        auto_offset_reset=_get_env("KAFKA_CONSUMER_AUTO_OFFSET_RESET", "earliest") or "earliest",
        tls_enabled=_get_bool("KAFKA_TLS_ENABLED", False),
        tls_ca_path=_get_env("KAFKA_TLS_CA_PATH", "") or "",
        tls_cert_path=_get_env("KAFKA_TLS_CERT_PATH", "") or "",
        tls_key_path=_get_env("KAFKA_TLS_KEY_PATH", "") or "",
        sasl_enabled=_get_bool("KAFKA_SASL_ENABLED", False),
        sasl_mechanism=_get_env("KAFKA_SASL_MECHANISM", "plain") or "plain",
        sasl_username=_get_env("KAFKA_SASL_USERNAME") or _get_env("KAFKA_SASL_USER", "") or "",
        sasl_password=_get_env("KAFKA_SASL_PASSWORD", "") or "",
        consumer_fetch_min_bytes=max(1, int(_get_env("KAFKA_CONSUMER_FETCH_MIN_BYTES", "1") or 1)),
        consumer_fetch_max_wait_ms=max(1, int(_get_env("KAFKA_CONSUMER_FETCH_MAX_WAIT_MS", "500") or 500)),
        producer_acks=_get_env("KAFKA_PRODUCER_ACKS", "all") or "all",
        producer_linger_ms=max(0, int(_get_env("KAFKA_PRODUCER_LINGER_MS", "0") or 0)),
        producer_batch_size=max(1, int(_get_env("KAFKA_PRODUCER_BATCH_SIZE", "16384") or 16384)),
        producer_compression_type=_get_env("KAFKA_PRODUCER_COMPRESSION", "none") or "none",
        producer_request_timeout_ms=max(100, int(_get_env("KAFKA_PRODUCER_REQUEST_TIMEOUT_MS", "30000") or 30000)),
    )

    features = FeatureConfig(
        schema_version=_get_env("FEATURE_SCHEMA_VERSION", "cic.v1") or "cic.v1",
        feature_mask_enabled=_get_bool("FEATURE_MASK_ENABLED", True),
        output_mode=_get_env("FEATURE_OUTPUT_MODE", "kafka") or "kafka",
        input_encoding=_get_env("FEATURE_INPUT_ENCODING", _get_env("FLOW_ENCODING", "json") or "json") or "json",
        output_encoding=_get_env("FEATURE_OUTPUT_ENCODING", _get_env("FEATURE_ENCODING", "json") or "json") or "json",
        registry_enabled=_get_bool("SCHEMA_REGISTRY_ENABLED", False),
        registry_url=_get_env("SCHEMA_REGISTRY_URL", "") or "",
        registry_subject_flow=_get_env("SCHEMA_REGISTRY_SUBJECT_FLOW", "telemetry-flow-v1") or "telemetry-flow-v1",
        registry_subject_feature=_get_env("SCHEMA_REGISTRY_SUBJECT_FEATURE", "telemetry-feature-v1") or "telemetry-feature-v1",
        registry_timeout_sec=int(_get_env("SCHEMA_REGISTRY_TIMEOUT_SEC", "5") or 5),
        registry_allow_fallback=_get_bool("SCHEMA_REGISTRY_ALLOW_FALLBACK", False),
        registry_username=_get_env("SCHEMA_REGISTRY_USERNAME", "") or "",
        registry_password=_get_env("SCHEMA_REGISTRY_PASSWORD", "") or "",
        derivation_policy=_get_env("DERIVATION_POLICY", "none") or "none",
        dry_run=_get_bool("FEATURE_DRY_RUN", False),
        min_feature_coverage=float(_get_env("FEATURE_MIN_COVERAGE", "0.45") or 0.45),
        healthcheck_only=_get_bool("FEATURE_HEALTHCHECK_ONLY", False),
        metrics_port=int(_get_env("FEATURE_METRICS_PORT", "9108") or 9108),
        role=_get_env("FEATURE_ROLE", "central") or "central",
        edge_source_ids=_split_csv(_get_env("EDGE_FEATURE_SOURCE_IDS", "") or ""),
        edge_source_types=_split_csv(_get_env("EDGE_FEATURE_SOURCE_TYPES", "") or ""),
        max_messages=int(_get_env("FEATURE_MAX_MESSAGES", "0") or 0),
        consumer_poll_timeout_sec=max(0.01, float(_get_env("FEATURE_CONSUMER_POLL_TIMEOUT_SEC", "1.0") or 1.0)),
        consumer_drain_max_messages=max(1, int(_get_env("FEATURE_CONSUMER_DRAIN_MAX_MESSAGES", "1") or 1)),
        consumer_drain_max_ms=max(0, int(_get_env("FEATURE_CONSUMER_DRAIN_MAX_MS", "0") or 0)),
        commit_mode=(_get_env("FEATURE_COMMIT_MODE", "sync") or "sync").strip().lower(),
        commit_batch_size=max(1, int(_get_env("FEATURE_COMMIT_BATCH_SIZE", "1") or 1)),
        commit_interval_ms=max(1, int(_get_env("FEATURE_COMMIT_INTERVAL_MS", "1000") or 1000)),
        producer_sync_flush=_get_bool("FEATURE_PRODUCER_SYNC_FLUSH", True),
        producer_flush_interval_ms=max(1, int(_get_env("FEATURE_PRODUCER_FLUSH_INTERVAL_MS", "500") or 500)),
    )

    postgres = PostgresConfig(
        enabled=_get_bool("POSTGRES_ENABLED", False),
        dsn=_get_env("POSTGRES_DSN", "") or "",
        table=_get_env("POSTGRES_TABLE", "curated.live_flows") or "curated.live_flows",
    )

    return AppConfig(kafka=kafka, features=features, postgres=postgres)


def _split_csv(value: str) -> list[str]:
    if not value:
        return []
    items = [item.strip() for item in value.split(",")]
    return [item for item in items if item]
