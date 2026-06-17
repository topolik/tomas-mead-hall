#!/usr/bin/env bash
# backup.sh — encrypted backup of all project state.
# Runs each project's backup.sh with a shared passphrase.
#
# Usage: ./backup.sh [--dir <path>]
# Passphrase: $BACKUP_PASSPHRASE env var, or prompted interactively.
set -euo pipefail
cd "$(dirname "$0")"

REPO_ROOT=$(git worktree list --porcelain | head -1 | sed 's/^worktree //')
export REPO_ROOT

BACKUP_DIR=""
while [ $# -gt 0 ]; do
  case "$1" in
    --dir) BACKUP_DIR="$2"; shift 2 ;;
    *)     echo "Usage: $0 [--dir <backup-directory>]" >&2; exit 1 ;;
  esac
done

if [ -n "${BACKUP_PASSPHRASE:-}" ]; then
  PASS="$BACKUP_PASSPHRASE"
else
  read -rsp "🔑 Backup passphrase (used for all projects): " PASS; echo
  [ -n "$PASS" ] || { echo "❌ Passphrase required." >&2; exit 1; }
fi
export BACKUP_PASSPHRASE="$PASS"

if [ -n "$BACKUP_DIR" ]; then
  export BACKUP_DIR
else
  unset BACKUP_DIR
fi

TODO_DIR="${BACKUP_DIR:-$HOME/.local/share/mead-hall/backups}"

FAILED=()

echo ""
echo "━━━ 📝 TODO ━━━"
TODO_FILE="$REPO_ROOT/todo.txt"
if [ -f "$TODO_FILE" ]; then
  mkdir -p "$TODO_DIR"
  TIMESTAMP=$(date +%Y%m%d-%H%M%S)
  TODO_OUT="$TODO_DIR/todo-$TIMESTAMP.txt.gz.enc"
  echo "📝 Backing up todo.txt:"
  echo "     $TODO_FILE"
  gzip -c "$TODO_FILE" | openssl enc -aes-256-cbc -pbkdf2 -iter 600000 \
    -pass pass:"$PASS" -out "$TODO_OUT"
  find "$TODO_DIR" -name 'todo-*.enc' -mtime +30 -delete
  echo "✅ Backup: $TODO_OUT"
else
  echo "⚠️  $TODO_FILE not found, skipping."
fi

echo ""
echo "━━━ 📦 DSH ━━━"
if ! projects/DSH-dashboard/backup.sh; then
  FAILED+=("DSH")
fi

echo ""
echo "━━━ 🔀 LLP ━━━"
if ! projects/LLP-llm-proxy/backup.sh; then
  FAILED+=("LLP")
fi

echo ""
echo "━━━ 📧 GML ━━━"
if ! projects/GML-gmail-agent/backup.sh; then
  FAILED+=("GML")
fi

echo ""
echo "━━━ 🧠 MND ━━━"
if ! projects/MND-mind-model/backup.sh; then
  FAILED+=("MND")
fi

echo ""
if [ ${#FAILED[@]} -eq 0 ]; then
  echo "✅ All backups complete."
else
  echo "⚠️  Failed: ${FAILED[*]}"
  exit 1
fi
