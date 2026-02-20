# syntax=docker/dockerfile:1

FROM python:3.11-slim-bookworm

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY docker/telemetry/feature-transformer/requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir --upgrade pip && \
    pip install --no-cache-dir -r /tmp/requirements.txt && \
    rm -f /tmp/requirements.txt

COPY telemetry-layer/feature-transformer_python /app/feature_transformer
COPY telemetry-layer/proto/gen/python /app/proto_gen
COPY telemetry-layer/schemas /app/schemas

RUN addgroup --system --gid 10001 telemetry && \
    adduser --system --uid 10001 --ingroup telemetry telemetry && \
    chown -R telemetry:telemetry /app

USER telemetry:telemetry

ENV PYTHONUNBUFFERED=1
ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONPATH=/app/feature_transformer:/app/proto_gen
WORKDIR /app/feature_transformer

ENTRYPOINT ["python", "main.py"]
