#!/usr/bin/env bash
# backup.sh — encrypted backup of all project state.
# Runs each project's backup.sh with a shared passphrase.
#
# Usage: ./backup.sh [--dir <path>]
# Passphrase: $BACKUP_PASSPHRASE env var, or prompted interactively.
set -euo pipefail
cd "$(dirname "$0")"

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

FAILED=()

echo ""
echo "━━━ 📦 DSH ━━━"
if ! projects/DSH-dashboard/backup.sh; then
  FAILED+=("DSH")
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
