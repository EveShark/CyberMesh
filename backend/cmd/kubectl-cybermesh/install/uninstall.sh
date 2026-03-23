#!/usr/bin/env sh
set -eu

INSTALL_DIR=""
PROFILE_FILE=""
MANAGED_BLOCK_BEGIN="# >>> kubectl-cybermesh >>>"
MANAGED_BLOCK_END="# <<< kubectl-cybermesh <<<"
MANIFEST_NAME=".kubectl-cybermesh-install.env"

log() {
  printf '[kubectl-cybermesh] %s\n' "$1"
}

default_install_dir() {
  if [ -n "${HOME:-}" ]; then
    printf '%s/.local/bin' "$HOME"
  else
    printf '/usr/local/bin'
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

remove_profile_block() {
  profile_path="$1"
  if [ ! -f "$profile_path" ]; then
    return
  fi
  tmp_dir=$(dirname "$profile_path")
  tmp_file="$tmp_dir/.kubectl-cybermesh.profile.tmp.$$"
  content_file="$tmp_dir/.kubectl-cybermesh.profile.content.$$"
  cleanup_profile_tmp() {
    rm -f "$tmp_file" "$content_file"
  }
  trap cleanup_profile_tmp EXIT INT TERM
  cp -p "$profile_path" "$tmp_file" 2>/dev/null || cp "$profile_path" "$tmp_file"
  awk -v begin="$MANAGED_BLOCK_BEGIN" -v end="$MANAGED_BLOCK_END" '
    $0 == begin { skip = 1; next }
    $0 == end { skip = 0; next }
    skip != 1 { print }
  ' "$profile_path" > "$content_file"
  cat "$content_file" > "$tmp_file"
  mv "$tmp_file" "$profile_path"
  rm -f "$content_file"
  trap - EXIT INT TERM
  log "Removed managed PATH block from $profile_path"
}

manifest_value() {
  manifest_path="$1"
  key="$2"
  if [ ! -f "$manifest_path" ]; then
    return
  fi
  awk -F= -v key="$key" '$1 == key {print substr($0, index($0, "=") + 1)}' "$manifest_path"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --install-dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    --profile-file)
      PROFILE_FILE="$2"
      shift 2
      ;;
    *)
      printf 'unsupported argument: %s\n' "$1" >&2
      exit 1
      ;;
  esac
done

if [ -z "$INSTALL_DIR" ]; then
  INSTALL_DIR=$(default_install_dir)
fi

MANIFEST_PATH="$INSTALL_DIR/$MANIFEST_NAME"
PATH_UPDATED=$(manifest_value "$MANIFEST_PATH" "PATH_UPDATED")
COMPLETION_INSTALLED=$(manifest_value "$MANIFEST_PATH" "COMPLETION_INSTALLED")
COMPLETION_PATH=$(manifest_value "$MANIFEST_PATH" "COMPLETION_PATH")
if [ -z "$PROFILE_FILE" ]; then
  PROFILE_FILE=$(manifest_value "$MANIFEST_PATH" "PROFILE_FILE")
fi
if [ -z "$PROFILE_FILE" ] && [ -n "${HOME:-}" ]; then
  PROFILE_FILE=$(default_profile_file)
fi

TARGET="$INSTALL_DIR/kubectl-cybermesh"
if [ -f "$TARGET" ]; then
  rm -f "$TARGET"
  log "Removed $TARGET"
fi

if [ "$COMPLETION_INSTALLED" = "true" ] && [ -n "$COMPLETION_PATH" ] && [ -f "$COMPLETION_PATH" ]; then
  rm -f "$COMPLETION_PATH"
  log "Removed $COMPLETION_PATH"
fi

if [ "$PATH_UPDATED" = "true" ] && [ -n "$PROFILE_FILE" ]; then
  remove_profile_block "$PROFILE_FILE"
fi

if [ -f "$MANIFEST_PATH" ]; then
  rm -f "$MANIFEST_PATH"
fi

log "Uninstall complete"
