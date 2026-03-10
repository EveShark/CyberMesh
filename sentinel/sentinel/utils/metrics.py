"""Lightweight server-side metrics for standalone Sentinel runtime paths."""

from __future__ import annotations

import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Dict, Tuple


_DEFAULT_BUCKETS = (0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0)


class _Histogram:
    def __init__(self, buckets: Tuple[float, ...]):
        self.buckets = buckets
        self.counts = [0 for _ in buckets]
        self.count = 0
        self.sum = 0.0

    def observe(self, seconds: float) -> None:
        self.count += 1
        self.sum += float(seconds)
        for idx, bucket in enumerate(self.buckets):
            if seconds <= bucket:
                self.counts[idx] += 1


class SentinelMetricsCollector:
    def __init__(self) -> None:
        self._lock = threading.RLock()
        self._operations: Dict[Tuple[str, str], int] = {}
        self._latencies: Dict[Tuple[str, str], _Histogram] = {}

    def reset(self) -> None:
        with self._lock:
            self._operations.clear()
            self._latencies.clear()

    def record_operation(self, operation: str, duration_seconds: float, status: str = "ok") -> None:
        key = (operation, status)
        with self._lock:
            self._operations[key] = self._operations.get(key, 0) + 1
            hist = self._latencies.get(key)
            if hist is None:
                hist = _Histogram(_DEFAULT_BUCKETS)
                self._latencies[key] = hist
            hist.observe(duration_seconds)

    def render_prometheus(self) -> str:
        with self._lock:
            lines = [
                "# HELP sentinel_service_operations_total Sentinel service operations by operation and status",
                "# TYPE sentinel_service_operations_total counter",
            ]
            for operation, status in sorted(self._operations):
                lines.append(
                    f'sentinel_service_operations_total{{operation="{operation}",status="{status}"}} {self._operations[(operation, status)]}'
                )

            lines.extend(
                [
                    "# HELP sentinel_service_operation_latency_seconds Sentinel service operation latency",
                    "# TYPE sentinel_service_operation_latency_seconds histogram",
                ]
            )
            for operation, status in sorted(self._latencies):
                hist = self._latencies[(operation, status)]
                for bucket, count in zip(hist.buckets, hist.counts):
                    lines.append(
                        f'sentinel_service_operation_latency_seconds_bucket{{operation="{operation}",status="{status}",le="{_fmt(bucket)}"}} {count}'
                    )
                lines.append(
                    f'sentinel_service_operation_latency_seconds_bucket{{operation="{operation}",status="{status}",le="+Inf"}} {hist.count}'
                )
                lines.append(
                    f'sentinel_service_operation_latency_seconds_sum{{operation="{operation}",status="{status}"}} {_fmt(hist.sum)}'
                )
                lines.append(
                    f'sentinel_service_operation_latency_seconds_count{{operation="{operation}",status="{status}"}} {hist.count}'
                )

        return "\n".join(lines) + "\n"


_collector = SentinelMetricsCollector()


def get_metrics_collector() -> SentinelMetricsCollector:
    return _collector


def reset_metrics_collector() -> SentinelMetricsCollector:
    _collector.reset()
    return _collector


class _MetricsHandler(BaseHTTPRequestHandler):
    collector = None

    def log_message(self, format, *args):  # pragma: no cover
        _ = format, args

    def _handle(self, include_body: bool) -> None:
        if self.path == "/healthz":
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.end_headers()
            if include_body:
                self.wfile.write(b"ok")
            return
        if self.path == "/metrics":
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
            self.end_headers()
            if include_body:
                self.wfile.write(self.collector.render_prometheus().encode("utf-8"))
            return
        self.send_response(404)
        self.end_headers()

    def do_GET(self):  # noqa: N802
        self._handle(include_body=True)

    def do_HEAD(self):  # noqa: N802
        self._handle(include_body=False)

    def _reject_method(self) -> None:
        self.send_response(405)
        self.send_header("Allow", "GET, HEAD")
        self.end_headers()

    def do_POST(self):  # noqa: N802
        self._reject_method()

    def do_PUT(self):  # noqa: N802
        self._reject_method()

    def do_DELETE(self):  # noqa: N802
        self._reject_method()

    def do_PATCH(self):  # noqa: N802
        self._reject_method()


def start_metrics_server(addr: str, logger=None):
    if not addr:
        return None

    _MetricsHandler.collector = get_metrics_collector()
    try:
        server = ThreadingHTTPServer(_split_addr(addr), _MetricsHandler)
    except Exception as exc:  # pylint: disable=broad-except
        if logger is not None:
            logger.warning("sentinel metrics server disabled", extra={"addr": addr, "error": str(exc)})
        return None
    thread = threading.Thread(target=server.serve_forever, name="sentinel-metrics", daemon=True)
    thread.start()
    if logger is not None:
        logger.info("sentinel metrics server listening", extra={"addr": addr})
    return server


def _split_addr(addr: str) -> Tuple[str, int]:
    host, _, port = addr.rpartition(":")
    if not port:
        raise ValueError(f"invalid metrics address: {addr}")
    if not host:
        host = "0.0.0.0"
    return host, int(port)


def _fmt(value: float) -> str:
    return f"{value:.6f}".rstrip("0").rstrip(".")
