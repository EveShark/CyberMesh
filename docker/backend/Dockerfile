# Multi-stage build for CyberMesh Consensus Backend
# Optimized for size and security

# ============================================================
# Stage 1: Builder (COMMENTED OUT - Using pre-built binary for speed)
# ============================================================
# FROM mirror.gcr.io/library/golang:1.25.1 AS builder
#
# WORKDIR /src
#
# # Add CA certificates and configure git for better network handling
# RUN apt-get update && apt-get install -y ca-certificates git curl && \
#     git config --global http.timeout 60 && \
#     git config --global http.lowSpeedLimit 0 && \
#     git config --global http.lowSpeedTime 999999 && \
#     rm -rf /var/lib/apt/lists/*
#
# # Cache go modules first with fallback proxies and network timeout
# COPY backend/go.mod backend/go.sum ./
# ENV GOPROXY=https://proxy.golang.org,https://goproxy.io,https://goproxy.cn,direct
# ENV GOSUMDB=sum.golang.org
# ENV GONOPROXY=none
# ENV GONOSUMDB=none
# ENV GOPRIVATE=none
# RUN timeout 600 go mod download || \
#     (echo "Retrying with direct proxy..." && \
#      GOPROXY=direct GOSUMDB=off timeout 300 go mod download)
#
# # Copy only necessary directories
# COPY backend/cmd/ ./cmd/
# COPY backend/pkg/ ./pkg/
# COPY backend/proto/ ./proto/
#
# # Update dependencies to ensure compatibility
# RUN go mod tidy
#
# # Build static binary for Linux AMD64
# ENV CGO_ENABLED=0
# ENV GOOS=linux
# ENV GOARCH=amd64
# RUN go build \
#     -ldflags="-s -w -X main.version=1.0.0 -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
#     -trimpath \
#     -o /out/cybermesh \
#     ./cmd/cybermesh

# ============================================================
# Stage 2: Runtime
# ============================================================
FROM mirror.gcr.io/library/debian:12-slim AS runtime

# Install curl for health checks and ca-certificates
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl ca-certificates && \
    rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd -g 1000 cybermesh && \
    useradd -u 1000 -g cybermesh -s /bin/bash -m cybermesh

WORKDIR /app

# Copy pre-built binary directly (Using local binary for faster builds)
# COPY --from=builder --chown=cybermesh:cybermesh /out/cybermesh ./cybermesh
COPY --chown=cybermesh:cybermesh backend/bin/cybermesh ./cybermesh
RUN chmod +x ./cybermesh

# Create directories for runtime mounts
RUN mkdir -p /app/keys /app/logs && chown -R cybermesh:cybermesh /app

# Use non-root user
USER cybermesh

# Set garbage collection optimization
ENV GOGC=50

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:9441/api/v1/health || exit 1

# Expose API port and metrics
EXPOSE 9441 9100

# Run the binary
CMD ["./cybermesh"]
