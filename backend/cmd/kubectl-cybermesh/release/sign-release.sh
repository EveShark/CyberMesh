#!/usr/bin/env sh
set -eu

ARTIFACTS_DIR="${1:-./bin}"
PRIVATE_KEY="${2:-./release-private-key.pem}"
PUBLIC_KEY="${3:-./release-public-key.pem}"

go run ./cmd/kubectl-cybermesh/release/sigtool --mode sign --artifacts-dir "$ARTIFACTS_DIR" --private-key "$PRIVATE_KEY" --write-public-key "$PUBLIC_KEY"
