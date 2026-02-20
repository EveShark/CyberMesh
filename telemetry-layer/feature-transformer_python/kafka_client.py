from __future__ import annotations

from typing import Dict

from confluent_kafka import Consumer, Producer

from config import KafkaConfig


def _security_protocol(cfg: KafkaConfig) -> str:
    if cfg.tls_enabled and cfg.sasl_enabled:
        return "SASL_SSL"
    if cfg.tls_enabled:
        return "SSL"
    if cfg.sasl_enabled:
        return "SASL_PLAINTEXT"
    return "PLAINTEXT"


def _build_security(cfg: KafkaConfig) -> Dict[str, str]:
    config: Dict[str, str] = {}
    config["security.protocol"] = _security_protocol(cfg)
    if cfg.tls_enabled:
        if cfg.tls_ca_path:
            config["ssl.ca.location"] = cfg.tls_ca_path
        if cfg.tls_cert_path:
            config["ssl.certificate.location"] = cfg.tls_cert_path
        if cfg.tls_key_path:
            config["ssl.key.location"] = cfg.tls_key_path
    if cfg.sasl_enabled:
        config["sasl.mechanisms"] = cfg.sasl_mechanism.upper()
        config["sasl.username"] = cfg.sasl_username
        config["sasl.password"] = cfg.sasl_password
    return config


def build_consumer(cfg: KafkaConfig) -> Consumer:
    config = {
        "bootstrap.servers": cfg.brokers,
        "group.id": cfg.consumer_group,
        "client.id": cfg.client_id,
        "enable.auto.commit": False,
        "auto.offset.reset": cfg.auto_offset_reset,
        "fetch.min.bytes": cfg.consumer_fetch_min_bytes,
        "fetch.wait.max.ms": cfg.consumer_fetch_max_wait_ms,
    }
    config.update(_build_security(cfg))
    return Consumer(config)


def build_producer(cfg: KafkaConfig) -> Producer:
    config = {
        "bootstrap.servers": cfg.brokers,
        "client.id": cfg.client_id,
        "enable.idempotence": True,
        "acks": cfg.producer_acks,
        "linger.ms": cfg.producer_linger_ms,
        "batch.size": cfg.producer_batch_size,
        "compression.type": cfg.producer_compression_type,
        "request.timeout.ms": cfg.producer_request_timeout_ms,
    }
    config.update(_build_security(cfg))
    return Producer(config)
