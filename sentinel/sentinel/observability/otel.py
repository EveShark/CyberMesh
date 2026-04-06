from __future__ import annotations

import os
from contextlib import contextmanager
from typing import Any, Dict, Iterable, Optional, Tuple
from urllib.parse import urlparse

_OTEL_AVAILABLE = True
try:
    from opentelemetry import propagate, trace
    from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
    from opentelemetry.sdk.resources import Resource
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import BatchSpanProcessor
    from opentelemetry.sdk.trace.sampling import ParentBased, TraceIdRatioBased
except Exception:  # pragma: no cover - optional dependency path
    _OTEL_AVAILABLE = False
    propagate = None  # type: ignore
    trace = None  # type: ignore
    OTLPSpanExporter = None  # type: ignore
    Resource = None  # type: ignore
    TracerProvider = None  # type: ignore
    BatchSpanProcessor = None  # type: ignore
    ParentBased = None  # type: ignore
    TraceIdRatioBased = None  # type: ignore

_provider = None


def _env_bool(name: str, default: bool = False) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


def _normalize_http_trace_endpoint(endpoint: str) -> str:
    parsed = urlparse(endpoint)
    if not parsed.scheme:
        endpoint = f"http://{endpoint}"
        parsed = urlparse(endpoint)
    if parsed.path and parsed.path != "/":
        return endpoint.rstrip("/")
    return endpoint.rstrip("/") + "/v1/traces"


def init_tracing_from_env(default_service_name: str = "cybermesh-sentinel-gateway") -> bool:
    global _provider
    if not _OTEL_AVAILABLE:
        return False
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
    ratio = min(1.0, max(0.0, ratio))

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


def shutdown_tracing() -> None:
    global _provider
    if _provider is None:
        return
    _provider.shutdown()
    _provider = None


def inject_context_headers(existing_headers: Optional[Dict[str, str]] = None, context=None) -> Dict[str, str]:
    out: Dict[str, str] = {}
    if existing_headers:
        for key, value in existing_headers.items():
            if key is None:
                continue
            if isinstance(value, bytes):
                out[str(key).lower()] = value.decode("utf-8", errors="ignore")
            elif value is None:
                out[str(key).lower()] = ""
            else:
                out[str(key).lower()] = str(value)
    if not _OTEL_AVAILABLE:
        return out
    carrier: Dict[str, str] = {}
    propagate.inject(carrier, context=context)
    for key, value in carrier.items():
        out[str(key).lower()] = str(value)
    return out


def extract_context_from_headers(headers: Optional[Iterable[Tuple[Any, Any]]], parent_context=None):
    if not _OTEL_AVAILABLE:
        return parent_context
    carrier: Dict[str, str] = {}
    if headers:
        for key, value in headers:
            if key is None:
                continue
            k = str(key).lower()
            if isinstance(value, bytes):
                carrier[k] = value.decode("utf-8", errors="ignore")
            elif value is None:
                carrier[k] = ""
            else:
                carrier[k] = str(value)
    return propagate.extract(carrier, context=parent_context)


@contextmanager
def start_span(name: str, *, tracer_name: str, context=None, attributes: Optional[Dict[str, object]] = None):
    if not _OTEL_AVAILABLE:
        yield None
        return
    tracer = trace.get_tracer(tracer_name)
    with tracer.start_as_current_span(name, context=context) as span:
        if attributes:
            for key, value in attributes.items():
                span.set_attribute(key, value)
        yield span
