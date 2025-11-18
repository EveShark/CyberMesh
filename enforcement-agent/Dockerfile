# Multi-stage build for CyberMesh Enforcement Agent
# Optimized for size and security

# ============================================================
# Stage 1: Builder
# ============================================================
FROM golang:1.25 AS builder

WORKDIR /workspace

# Copy backend module for protobuf dependencies
COPY backend /backend

# Cache go modules first
COPY enforcement-agent/go.mod enforcement-agent/go.sum ./
RUN go mod download

# Copy enforcement agent sources
COPY enforcement-agent/. ./

# Build static binary
ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64
RUN go build \
    -ldflags="-s -w" \
    -trimpath \
    -o /out/enforcement-agent \
    ./cmd/agent

# ============================================================
# Stage 2: Runtime
# ============================================================
FROM debian:12-slim

# Install iptables for host-level enforcement
# Using iptables-nft (netfilter nftables backend) for modern kernel compatibility
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        iptables \
        ca-certificates && \
    rm -rf /var/lib/apt/lists/*

# Copy binary from builder
COPY --from=builder /out/enforcement-agent /usr/local/bin/enforcement-agent

# Create mount point for trusted keys
RUN mkdir -p /var/run/cybermesh/trust

# NOTE: Container must run as root for iptables access with hostNetwork
# DaemonSet grants privileged: true and NET_ADMIN capability

ENTRYPOINT ["/usr/local/bin/enforcement-agent"]
