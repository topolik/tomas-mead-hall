#!/usr/bin/env bash
# backup.sh — encrypted backup of LLP's data/ directory and config.yaml.
#
# Passphrase: $BACKUP_PASSPHRASE env var, or prompted interactively.
# Output:     $BACKUP_DIR/llp-<timestamp>.tar.gz.enc (default ~/.local/share/llp/backups/)
set -euo pipefail

SRC="${REPO_ROOT:-$(git worktree list --porcelain | head -1 | sed 's/^worktree //')}/projects/LLP-llm-proxy"
cd "$SRC"

BACKUP_DIR="${BACKUP_DIR:-$HOME/.local/share/llp/backups}"
KEEP_DAYS=30

if [ -n "${BACKUP_PASSPHRASE:-}" ]; then
  PASS="$BACKUP_PASSPHRASE"
else
  read -rsp "🔑 Backup passphrase: " PASS; echo
  [ -n "$PASS" ] || { echo "❌ Passphrase required." >&2; exit 1; }
fi

mkdir -p "$BACKUP_DIR"

FILES=()
[ -d data ] && FILES+=(data)
[ -f config.yaml ] && FILES+=(config.yaml)

if [ ${#FILES[@]} -eq 0 ]; then
  echo "❌ Nothing to back up — no LLP state files found." >&2
  exit 1
fi

TIMESTAMP=$(date +%Y%m%d-%H%M%S)
OUTFILE="$BACKUP_DIR/llp-$TIMESTAMP.tar.gz.enc"

echo "🔀 Backing up LLP state:"
find "${FILES[@]}" -type f | sort | sed 's/^/     /'
tar czf - "${FILES[@]}" | openssl enc -aes-256-cbc -pbkdf2 -iter 600000 \
  -pass pass:"$PASS" -out "$OUTFILE"

find "$BACKUP_DIR" -name 'llp-*.enc' -mtime "+$KEEP_DAYS" -delete

echo "✅ Backup: $OUTFILE"
echo "   Restore: ./restore.sh $OUTFILE"
