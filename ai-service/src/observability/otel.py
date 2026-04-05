from __future__ import annotations

import os
from contextlib import contextmanager
from urllib.parse import urlparse
from typing import Dict, Iterable, Optional, Tuple

from opentelemetry import propagate, trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.sampling import ParentBased, TraceIdRatioBased
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.trace import Span

_provider: Optional[TracerProvider] = None


def _env_bool(name: str, default: bool = False) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


def init_tracing_from_env(default_service_name: str = "cybermesh-ai-service") -> bool:
    global _provider
    if not _env_bool("OTEL_ENABLED", False):
        return False
    endpoint = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "").strip()
    if not endpoint:
        return False
    endpoint = _normalize_http_trace_endpoint(endpoint)

    service_name = os.getenv("OTEL_SERVICE_NAME", default_service_name).strip() or default_service_name
    sample_ratio = os.getenv("OTEL_TRACES_SAMPLER_ARG", "0.1").strip()
    try:
        ratio = float(sample_ratio)
    except ValueError:
        ratio = 0.1
    if ratio < 0:
        ratio = 0.0
    if ratio > 1:
        ratio = 1.0

    resource = Resource.create({"service.name": service_name})
    provider = TracerProvider(
        resource=resource,
        sampler=ParentBased(TraceIdRatioBased(ratio)),
    )
    exporter = OTLPSpanExporter(endpoint=endpoint)
    provider.add_span_processor(BatchSpanProcessor(exporter))
    trace.set_tracer_provider(provider)
    _provider = provider
    return True


def _normalize_http_trace_endpoint(endpoint: str) -> str:
    parsed = urlparse(endpoint)
    if not parsed.scheme:
        endpoint = f"http://{endpoint}"
        parsed = urlparse(endpoint)
    if parsed.path and parsed.path != "/":
        return endpoint.rstrip("/")
    return endpoint.rstrip("/") + "/v1/traces"


def shutdown_tracing() -> None:
    global _provider
    if _provider is None:
        return
    _provider.shutdown()
    _provider = None


def get_tracer(name: str):
    return trace.get_tracer(name)


def inject_context_headers(
    existing_headers: Optional[Iterable[Tuple[str, bytes]]] = None, context=None
) -> list[Tuple[str, bytes]]:
    carrier: Dict[str, str] = {}
    propagate.inject(carrier, context=context)

    out: Dict[str, bytes] = {}
    if existing_headers:
        for key, value in existing_headers:
            out[str(key).lower()] = value if isinstance(value, bytes) else str(value).encode("utf-8")
    for key, value in carrier.items():
        out[str(key).lower()] = value.encode("utf-8")
    return [(k, v) for k, v in out.items()]


def extract_context_from_headers(headers, parent_context=None):
    carrier: Dict[str, str] = {}
    if headers:
        for key, value in headers:
            if key is None:
                continue
            k = str(key).lower()
            if isinstance(value, bytes):
                carrier[k] = value.decode("utf-8", errors="ignore")
            else:
                carrier[k] = str(value)
    return propagate.extract(carrier, context=parent_context)


@contextmanager
def start_span(name: str, *, tracer_name: str, context=None, attributes: Optional[Dict[str, object]] = None):
    tracer = get_tracer(tracer_name)
    with tracer.start_as_current_span(name, context=context) as span:
        if isinstance(span, Span) and attributes:
            for key, value in attributes.items():
                span.set_attribute(key, value)
        yield span
