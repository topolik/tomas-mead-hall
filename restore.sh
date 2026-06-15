#!/usr/bin/env bash
# restore.sh — restore all project state from encrypted backups.
# Finds the latest backup for each project, or accepts explicit paths.
#
# Usage:
#   ./restore.sh                            # restore latest from default dirs
#   ./restore.sh --dir /path/to/backups     # restore latest from shared directory
#   ./restore.sh --dsh <f> --gml <f> --mnd <f>  # restore specific files
#
# Passphrase: $BACKUP_PASSPHRASE env var, or prompted interactively.
set -euo pipefail
cd "$(dirname "$0")"

DSH_BACKUP="" GML_BACKUP="" MND_BACKUP="" BACKUP_DIR=""

while [ $# -gt 0 ]; do
  case "$1" in
    --dir) BACKUP_DIR="$2"; shift 2 ;;
    --dsh) DSH_BACKUP="$2"; shift 2 ;;
    --gml) GML_BACKUP="$2"; shift 2 ;;
    --mnd) MND_BACKUP="$2"; shift 2 ;;
    *)     echo "Usage: $0 [--dir <backup-directory>] [--dsh <file>] [--gml <file>] [--mnd <file>]" >&2; exit 1 ;;
  esac
done

latest_backup() {
  local dir="$1" pattern="${2:-*.enc}"
  if [ -d "$dir" ]; then
    find "$dir" -maxdepth 1 -name "$pattern" -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -1 | cut -d' ' -f2-
  fi
}

if [ -n "$BACKUP_DIR" ]; then
  [ -z "$DSH_BACKUP" ] && DSH_BACKUP=$(latest_backup "$BACKUP_DIR" 'dsh-*.enc') || true
  [ -z "$GML_BACKUP" ] && GML_BACKUP=$(latest_backup "$BACKUP_DIR" 'gml-*.enc') || true
  [ -z "$MND_BACKUP" ] && MND_BACKUP=$(latest_backup "$BACKUP_DIR" 'mnd-*.enc') || true
else
  [ -z "$DSH_BACKUP" ] && DSH_BACKUP=$(latest_backup "$HOME/.local/share/dsh/backups" '*.enc') || true
  [ -z "$GML_BACKUP" ] && GML_BACKUP=$(latest_backup "$HOME/.local/share/gml/backups" '*.enc') || true
  [ -z "$MND_BACKUP" ] && MND_BACKUP=$(latest_backup "$HOME/.local/share/mnd/backups" '*.enc') || true
fi

FOUND=0
[ -n "$DSH_BACKUP" ] && FOUND=$((FOUND + 1)) || true
[ -n "$GML_BACKUP" ] && FOUND=$((FOUND + 1)) || true
[ -n "$MND_BACKUP" ] && FOUND=$((FOUND + 1)) || true

if [ "$FOUND" -eq 0 ]; then
  echo "❌ No backups found. Run ./backup.sh first or pass paths with --dsh/--gml/--mnd." >&2
  exit 1
fi

echo "📋 Backups to restore:"
[ -n "$DSH_BACKUP" ] && echo "   DSH: $DSH_BACKUP" || true
[ -n "$GML_BACKUP" ] && echo "   GML: $GML_BACKUP" || true
[ -n "$MND_BACKUP" ] && echo "   MND: $MND_BACKUP" || true
echo ""

if [ -n "${BACKUP_PASSPHRASE:-}" ]; then
  PASS="$BACKUP_PASSPHRASE"
else
  read -rsp "🔑 Backup passphrase (used for all projects): " PASS; echo
  [ -n "$PASS" ] || { echo "❌ Passphrase required." >&2; exit 1; }
fi
export BACKUP_PASSPHRASE="$PASS"

FAILED=()

if [ -n "$DSH_BACKUP" ]; then
  echo ""
  echo "━━━ 📦 DSH ━━━"
  if ! projects/DSH-dashboard/restore.sh "$DSH_BACKUP"; then
    FAILED+=("DSH")
  fi
fi

if [ -n "$GML_BACKUP" ]; then
  echo ""
  echo "━━━ 📧 GML ━━━"
  if ! projects/GML-gmail-agent/restore.sh "$GML_BACKUP"; then
    FAILED+=("GML")
  fi
fi

if [ -n "$MND_BACKUP" ]; then
  echo ""
  echo "━━━ 🧠 MND ━━━"
  if ! projects/MND-mind-model/restore.sh "$MND_BACKUP"; then
    FAILED+=("MND")
  fi
fi

echo ""
if [ ${#FAILED[@]} -eq 0 ]; then
  echo "✅ All restores complete."
else
  echo "⚠️  Failed: ${FAILED[*]}"
  exit 1
fi
