"""Standalone Kafka gateway runner.

Consume CanonicalEvent envelopes from Kafka, analyze with Sentinel, publish results.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

BASE_DIR = Path(__file__).resolve().parents[1]
if str(BASE_DIR) not in sys.path:
    sys.path.insert(0, str(BASE_DIR))

from sentinel.agents import SentinelOrchestrator
from sentinel.kafka import ConfluentKafkaClient, KafkaGatewayWorker, load_kafka_worker_config
from sentinel.logging import configure_logging
from sentinel.observability import init_tracing_from_env, shutdown_tracing
from sentinel.utils.metrics import start_metrics_server


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Sentinel standalone Kafka gateway worker")
    parser.add_argument("--max-messages", type=int, default=0, help="Stop after N consumed messages (0 = run forever)")
    parser.add_argument("--stop-on-idle", action="store_true", help="Exit when poll() returns no messages")
    parser.add_argument("--sequential", action="store_true", help="Run Sentinel agents sequentially")
    parser.add_argument("--enable-llm", action="store_true", help="Enable LLM provider in analysis")
    parser.add_argument("--disable-fast-path", action="store_true", help="Disable hash/YARA/signature fast-path")
    parser.add_argument("--disable-threat-intel", action="store_true", help="Disable threat intel enrichers")
    parser.add_argument("--log-level", default="INFO", help="Logging level")
    parser.add_argument("--json-logs", action="store_true", help="Emit JSON logs")
    return parser


def main() -> int:
    args = build_parser().parse_args()
    configure_logging(level=args.log_level, json_format=args.json_logs)

    cfg = load_kafka_worker_config()
    if not cfg.enabled:
        print(json.dumps({"error": "ENABLE_KAFKA is false; worker is disabled"}, sort_keys=True))
        return 2

    metrics_server = start_metrics_server(os.getenv("SENTINEL_METRICS_ADDR", ":9202"))
    tracing_enabled = init_tracing_from_env("cybermesh-sentinel-kafka-gateway")

    kafka_client = ConfluentKafkaClient(cfg)
    orchestrator = SentinelOrchestrator(
        enable_llm=args.enable_llm,
        enable_fast_path=not args.disable_fast_path,
        enable_threat_intel=not args.disable_threat_intel,
        sequential=args.sequential,
    )
    worker = KafkaGatewayWorker(config=cfg, kafka_client=kafka_client, orchestrator=orchestrator)

    try:
        stats = worker.run(max_messages=args.max_messages, stop_on_idle=args.stop_on_idle)
    finally:
        worker.close()
        if tracing_enabled:
            shutdown_tracing()
        if metrics_server is not None:
            metrics_server.shutdown()
            metrics_server.server_close()

    print(json.dumps({"status": "ok", "stats": stats}, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
