#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROTO_DIR="$ROOT"
OUT_GO="$ROOT/gen/go"
OUT_PY="$ROOT/gen/python"

command -v protoc >/dev/null 2>&1 || { echo "protoc not found in PATH"; exit 1; }

protoc \
  --proto_path="$PROTO_DIR" \
  --go_out="$OUT_GO" \
  --go_opt=paths=source_relative \
  --python_out="$OUT_PY" \
  telemetry_flow_v1.proto \
  telemetry_feature_v1.proto \
  telemetry_dlq_v1.proto \
  telemetry_deepflow_v1.proto \
  telemetry_pcap_request_v1.proto \
  telemetry_pcap_result_v1.proto

echo "Generated Go + Python protobufs into gen/"
