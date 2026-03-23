#!/usr/bin/env sh
set -eu

SOURCE_PATH=""
INSTALL_DIR=""
INSTALL_COMPLETION="false"
UPDATE_SHELL_PROFILE="false"
PROFILE_FILE=""
PUBLIC_KEY_PATH=""
SIGNATURE_PATH=""
REQUIRE_SIGNATURE="false"
MANAGED_BLOCK_BEGIN="# >>> kubectl-cybermesh >>>"
MANAGED_BLOCK_END="# <<< kubectl-cybermesh <<<"
MANIFEST_NAME=".kubectl-cybermesh-install.env"

log() {
  printf '[kubectl-cybermesh] %s\n' "$1"
}

fail() {
  printf '[kubectl-cybermesh] %s\n' "$1" >&2
  exit 1
}

default_install_dir() {
  if [ -n "${HOME:-}" ]; then
    printf '%s/.local/bin' "$HOME"
  else
    printf '/usr/local/bin'
  fi
}

default_completion_dir() {
  if [ -n "${HOME:-}" ]; then
    printf '%s/.local/share/bash-completion/completions' "$HOME"
  else
    printf '/tmp'
  fi
}

default_profile_file() {
  shell_name=$(basename "${SHELL:-}")
  case "$shell_name" in
    zsh)
      printf '%s/.zshrc' "$HOME"
      ;;
    bash)
      printf '%s/.bashrc' "$HOME"
      ;;
    *)
      printf '%s/.profile' "$HOME"
      ;;
  esac
}

checksum_tool() {
  if command -v sha256sum >/dev/null 2>&1; then
    printf 'sha256sum'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    printf 'shasum'
    return
  fi
  printf ''
}

compute_sha256() {
  file_path="$1"
  tool=$(checksum_tool)
  case "$tool" in
    sha256sum)
      sha256sum "$file_path" | awk '{print $1}'
      ;;
    shasum)
      shasum -a 256 "$file_path" | awk '{print $1}'
      ;;
    *)
      fail "No SHA-256 tool available (expected sha256sum or shasum)"
      ;;
  esac
}

verify_checksum() {
  source_path="$1"
  checksum_file="$2"
  artifact_name=$(basename "$source_path")
  [ -f "$checksum_file" ] || fail "Missing checksum file: $checksum_file"
  expected=$(awk -v name="$artifact_name" '$2 == name {print $1}' "$checksum_file")
  [ -n "$expected" ] || fail "No checksum entry for $artifact_name in $checksum_file"
  actual=$(compute_sha256 "$source_path")
  [ "$expected" = "$actual" ] || fail "Checksum mismatch for $artifact_name"
}

python_tool() {
  if command -v python3 >/dev/null 2>&1; then
    printf 'python3'
    return
  fi
  if command -v python >/dev/null 2>&1; then
    printf 'python'
    return
  fi
  printf ''
}

verify_signature() {
  checksum_file="$1"
  public_key_path="$2"
  signature_path="$3"
  [ -f "$public_key_path" ] || fail "Missing public key file: $public_key_path"
  [ -f "$signature_path" ] || fail "Missing signature file: $signature_path"

  py=$(python_tool)
  [ -n "$py" ] || fail "Signature verification requires python3 or python on PATH"

  script_path="$(dirname "$checksum_file")/.kubectl-cybermesh-verify-signature.$$"
  cat > "$script_path" <<'PY'
import base64
import pathlib
import sys

try:
    from cryptography.exceptions import InvalidSignature
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
except ImportError as exc:
    raise SystemExit("python cryptography package is required for signature verification") from exc

checksums_path = pathlib.Path(sys.argv[1])
public_key_path = pathlib.Path(sys.argv[2])
signature_path = pathlib.Path(sys.argv[3])

public_key = serialization.load_pem_public_key(public_key_path.read_bytes())
if not isinstance(public_key, Ed25519PublicKey):
    raise SystemExit("public key is not Ed25519")

payload = checksums_path.read_bytes()
signature = base64.b64decode(signature_path.read_text(encoding="utf-8").strip())
try:
    public_key.verify(signature, payload)
except InvalidSignature as exc:
    raise SystemExit("signature verification failed") from exc
PY

  if ! "$py" "$script_path" "$checksum_file" "$public_key_path" "$signature_path"; then
    rm -f "$script_path"
    fail "Signature verification failed"
  fi
  rm -f "$script_path"
}

update_profile_file() {
  profile_path="$1"
  path_line="export PATH=\"$INSTALL_DIR:\$PATH\""

  mkdir -p "$(dirname "$profile_path")"
  touch "$profile_path"

  if grep -Fq "$MANAGED_BLOCK_BEGIN" "$profile_path"; then
    log "Shell profile already contains a managed kubectl-cybermesh PATH block"
    return
  fi

  {
    printf '\n%s\n' "$MANAGED_BLOCK_BEGIN"
    printf '%s\n' "$path_line"
    printf '%s\n' "$MANAGED_BLOCK_END"
  } >> "$profile_path"
  log "Updated shell profile $profile_path"
}

write_manifest() {
  manifest_path="$1"
  completion_path="$2"
  {
    printf 'INSTALL_DIR=%s\n' "$INSTALL_DIR"
    printf 'PATH_UPDATED=%s\n' "$UPDATE_SHELL_PROFILE"
    printf 'PROFILE_FILE=%s\n' "$PROFILE_FILE"
    printf 'COMPLETION_INSTALLED=%s\n' "$INSTALL_COMPLETION"
    printf 'COMPLETION_PATH=%s\n' "$completion_path"
  } > "$manifest_path"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --source-path)
      SOURCE_PATH="$2"
      shift 2
      ;;
    --install-dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    --install-completion)
      INSTALL_COMPLETION="true"
      shift 1
      ;;
    --update-shell-profile)
      UPDATE_SHELL_PROFILE="true"
      shift 1
      ;;
    --profile-file)
      PROFILE_FILE="$2"
      shift 2
      ;;
    --public-key-path)
      PUBLIC_KEY_PATH="$2"
      shift 2
      ;;
    --signature-path)
      SIGNATURE_PATH="$2"
      shift 2
      ;;
    --require-signature)
      REQUIRE_SIGNATURE="true"
      shift 1
      ;;
    *)
      printf 'unsupported argument: %s\n' "$1" >&2
      exit 1
      ;;
  esac
done

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ -z "$SOURCE_PATH" ]; then
  REPO_BIN=$(CDPATH= cd -- "$SCRIPT_DIR/../../../bin" && pwd 2>/dev/null || true)
  if [ -n "$REPO_BIN" ] && [ -f "$REPO_BIN/kubectl-cybermesh" ]; then
    SOURCE_PATH="$REPO_BIN/kubectl-cybermesh"
  else
    fail "Could not find kubectl-cybermesh. Pass --source-path explicitly."
  fi
fi

if [ -z "$INSTALL_DIR" ]; then
  INSTALL_DIR=$(default_install_dir)
fi

if [ "$UPDATE_SHELL_PROFILE" = "true" ] && [ -z "${HOME:-}" ]; then
  fail "HOME must be set when using --update-shell-profile."
fi

if [ "$UPDATE_SHELL_PROFILE" = "true" ] && [ -z "$PROFILE_FILE" ]; then
  PROFILE_FILE=$(default_profile_file)
fi

SOURCE_DIR=$(CDPATH= cd -- "$(dirname -- "$SOURCE_PATH")" && pwd)
CHECKSUM_FILE="$SOURCE_DIR/SHA256SUMS"
if [ -z "$SIGNATURE_PATH" ]; then
  SIGNATURE_PATH="$CHECKSUM_FILE.sig"
fi
if [ "$REQUIRE_SIGNATURE" = "true" ] && [ -z "$PUBLIC_KEY_PATH" ]; then
  fail "--require-signature needs --public-key-path"
fi
if [ -n "$PUBLIC_KEY_PATH" ]; then
  verify_signature "$CHECKSUM_FILE" "$PUBLIC_KEY_PATH" "$SIGNATURE_PATH"
fi
verify_checksum "$SOURCE_PATH" "$CHECKSUM_FILE"

mkdir -p "$INSTALL_DIR"
TARGET="$INSTALL_DIR/kubectl-cybermesh"
MANIFEST_PATH="$INSTALL_DIR/$MANIFEST_NAME"
TMP_TARGET="$INSTALL_DIR/.kubectl-cybermesh.tmp.$$"
BACKUP_TARGET="$INSTALL_DIR/.kubectl-cybermesh.backup.$$"
cleanup() {
  rm -f "$TMP_TARGET" "$BACKUP_TARGET"
}
trap cleanup EXIT INT TERM

PREVIOUS_VERSION=""
if [ -x "$TARGET" ]; then
  PREVIOUS_VERSION=$("$TARGET" version 2>/dev/null || true)
  cp "$TARGET" "$BACKUP_TARGET"
fi

cp "$SOURCE_PATH" "$TMP_TARGET"
chmod +x "$TMP_TARGET"
TMP_VERSION=$("$TMP_TARGET" version 2>/dev/null || true)
[ -n "$TMP_VERSION" ] || fail "Installed binary failed validation: version command returned no output"

mv "$TMP_TARGET" "$TARGET"
log "Installed kubectl-cybermesh to $TARGET"

COMPLETION_PATH=""
if [ "$INSTALL_COMPLETION" = "true" ]; then
  COMPLETION_DIR=$(default_completion_dir)
  mkdir -p "$COMPLETION_DIR"
  COMPLETION_PATH="$COMPLETION_DIR/kubectl-cybermesh"
  "$TARGET" completion bash > "$COMPLETION_PATH"
  log "Wrote bash completion to $COMPLETION_PATH"
fi

if [ "$UPDATE_SHELL_PROFILE" = "true" ]; then
  update_profile_file "$PROFILE_FILE"
fi

CURRENT_VERSION=$("$TARGET" version 2>/dev/null || true)
if [ -z "$CURRENT_VERSION" ]; then
  if [ -f "$BACKUP_TARGET" ]; then
    mv "$BACKUP_TARGET" "$TARGET"
    fail "Install failed validation after replace; restored previous binary"
  fi
  fail "Install failed validation after replace"
fi

write_manifest "$MANIFEST_PATH" "$COMPLETION_PATH"

if [ -n "$PREVIOUS_VERSION" ]; then
  log "Upgrade complete"
  log "Previous version: $PREVIOUS_VERSION"
else
  log "Install complete"
fi
log "Current version: $CURRENT_VERSION"

case ":${PATH:-}:" in
  *:"$INSTALL_DIR":*)
    ;;
  *)
    if [ "$UPDATE_SHELL_PROFILE" = "true" ]; then
      log "Open a new shell, then run: kubectl-cybermesh version"
    else
      log "Add $INSTALL_DIR to PATH or rerun with --update-shell-profile, then run: kubectl-cybermesh version"
    fi
    ;;
esac
