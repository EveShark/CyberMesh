#!/usr/bin/env sh
set -eu

PRIVATE_KEY="${1:-./release-private-key.pem}"
PUBLIC_KEY="${2:-./release-public-key.pem}"

go run ./cmd/kubectl-cybermesh/release/sigtool --mode keygen --private-key "$PRIVATE_KEY" --public-key "$PUBLIC_KEY"
