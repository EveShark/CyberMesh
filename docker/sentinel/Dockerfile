FROM python:3.11-slim-bookworm

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

# Copy only Sentinel runtime sources (no TestData or unrelated repo content).
COPY sentinel/pyproject.toml /app/sentinel/pyproject.toml
COPY sentinel/README.md /app/sentinel/README.md
COPY sentinel/sentinel /app/sentinel/sentinel
COPY sentinel/scripts/kafka_gateway.py /app/sentinel/scripts/kafka_gateway.py
COPY telemetry-layer/proto/gen/python /app/telemetry_proto

# Install Sentinel + Kafka extras needed for the in-cluster worker.
RUN pip install --upgrade pip && \
    pip install "/app/sentinel[kafka]"

CMD ["python", "/app/sentinel/scripts/kafka_gateway.py"]
