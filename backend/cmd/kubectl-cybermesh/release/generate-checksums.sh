#!/usr/bin/env sh
set -eu

ARTIFACTS_DIR="${1:-}"
if [ -z "$ARTIFACTS_DIR" ]; then
  SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
  ARTIFACTS_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../../../bin" && pwd)
fi

if command -v sha256sum >/dev/null 2>&1; then
  HASH_CMD="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  HASH_CMD="shasum -a 256"
else
  printf 'Missing sha256sum or shasum for checksum generation.\n' >&2
  exit 1
fi

artifacts="
kubectl-cybermesh.exe
kubectl-cybermesh
kubectl-cybermesh.bash
_kubectl-cybermesh
kubectl-cybermesh.ps1
install-kubectl-cybermesh.ps1
uninstall-kubectl-cybermesh.ps1
install-kubectl-cybermesh.sh
uninstall-kubectl-cybermesh.sh
"

output="$ARTIFACTS_DIR/SHA256SUMS"
: > "$output"
for name in $artifacts; do
  path="$ARTIFACTS_DIR/$name"
  if [ ! -f "$path" ]; then
    printf 'Missing artifact: %s\n' "$path" >&2
    exit 1
  fi
  if [ ! -s "$path" ]; then
    printf 'Artifact is empty: %s\n' "$path" >&2
    exit 1
  fi
  (cd "$ARTIFACTS_DIR" && $HASH_CMD "$name") >> "$output"
done

printf '%s\n' "$output"
