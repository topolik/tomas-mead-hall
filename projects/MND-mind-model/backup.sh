#!/usr/bin/env bash
# backup.sh — encrypted backup of MND's data/ directory.
#
# Passphrase: $BACKUP_PASSPHRASE env var, or prompted interactively.
# Output:     $BACKUP_DIR/mnd-<timestamp>.tar.gz.enc (default ~/.local/share/mnd/backups/)
set -euo pipefail

SRC="${REPO_ROOT:-$(git worktree list --porcelain | head -1 | sed 's/^worktree //')}/projects/MND-mind-model"
cd "$SRC"

BACKUP_DIR="${BACKUP_DIR:-$HOME/.local/share/mnd/backups}"
KEEP_DAYS=30

if [ -n "${BACKUP_PASSPHRASE:-}" ]; then
  PASS="$BACKUP_PASSPHRASE"
else
  read -rsp "🔑 Backup passphrase: " PASS; echo
  [ -n "$PASS" ] || { echo "❌ Passphrase required." >&2; exit 1; }
fi

mkdir -p "$BACKUP_DIR"

if [ ! -d data ] || [ -z "$(ls -A data 2>/dev/null)" ]; then
  echo "❌ Nothing to back up — data/ is missing or empty." >&2
  exit 1
fi

TIMESTAMP=$(date +%Y%m%d-%H%M%S)
OUTFILE="$BACKUP_DIR/mnd-$TIMESTAMP.tar.gz.enc"

echo "🧠 Backing up MND data/:"
find data -type f | sort | sed 's/^/     /'
tar czf - data | openssl enc -aes-256-cbc -pbkdf2 -iter 600000 \
  -pass pass:"$PASS" -out "$OUTFILE"

find "$BACKUP_DIR" -name 'mnd-*.enc' -mtime "+$KEEP_DAYS" -delete

echo "✅ Backup: $OUTFILE"
echo "   Restore: ./restore.sh $OUTFILE"
