#!/usr/bin/env sh
set -eu

ARTIFACTS_DIR="${1:-./bin}"
PUBLIC_KEY="${2:-./release-public-key.pem}"

go run ./cmd/kubectl-cybermesh/release/sigtool --mode verify --artifacts-dir "$ARTIFACTS_DIR" --public-key "$PUBLIC_KEY"
