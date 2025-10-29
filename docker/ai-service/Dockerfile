# CyberMesh AI Service - Detection & ML Pipeline
# Optimized for security and model isolation

# ============================================================
# Base: Python 3.11 on Debian 12 (matches backend)
# ============================================================
FROM python:3.11-slim-bookworm AS runtime

# Install system dependencies for ML libraries
# libgomp1: Required by LightGBM for OpenMP support
# ca-certificates: For HTTPS connections to Kafka/Redis/DB
# curl: For health checks
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgomp1 \
    ca-certificates \
    curl \
    netcat-openbsd && \
    rm -rf /var/lib/apt/lists/*

# Create non-root user (uid 1001 to differentiate from backend's 1000)
RUN groupadd -g 1001 aiservice && \
    useradd -u 1001 -g aiservice -s /bin/bash -m aiservice

WORKDIR /app

# ============================================================
# Layer 1: Python Dependencies (CACHED unless requirements.txt changes)
# ============================================================
COPY ai-service/requirements.txt ./requirements.txt
RUN pip install --no-cache-dir --upgrade pip && \
    pip install --no-cache-dir -r requirements.txt

# ============================================================
# Layer 2: Application Code
# ============================================================
# Copy source code
COPY --chown=aiservice:aiservice ai-service/src ./src
COPY --chown=aiservice:aiservice ai-service/config ./config
COPY --chown=aiservice:aiservice ai-service/proto ./proto
COPY --chown=aiservice:aiservice ai-service/cmd ./cmd

# Copy entrypoint script
COPY --chown=aiservice:aiservice docker/ai-service/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Create directories for runtime mounts
# /app/data/models - Will be mounted from OCI Object Storage (ReadOnly)
# /app/data/datasets - Will be mounted from OCI Object Storage (ReadOnly)
# /app/data - EmptyDir for nonce_state.json, service.pid
# /app/keys - Secret mount for Ed25519 signing keys
# /app/logs - EmptyDir for application logs
RUN mkdir -p /app/data/models /app/data/datasets /app/keys /app/logs && \
    chown -R aiservice:aiservice /app

# ============================================================
# Runtime Configuration
# ============================================================
# Use non-root user
USER aiservice

# Environment defaults (overridden by ConfigMap in K8s)
ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    DETECTION_INTERVAL=5 \
    METRICS_PORT=10000

# Health check (AI service runs on port 8000)
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD curl -f http://localhost:8000/health || exit 1

# Expose API port and Prometheus metrics
EXPOSE 8000 10000

# Use entrypoint script for pre-flight checks and secret injection
ENTRYPOINT ["/app/entrypoint.sh"]

# Default command (can be overridden with K8s args)
CMD []
