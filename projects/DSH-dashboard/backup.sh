#!/usr/bin/env bash
# backup.sh — encrypted backup of DSH's SQLite database and .env.
#
# Passphrase: $BACKUP_PASSPHRASE env var, or prompted interactively.
# Output:     $BACKUP_DIR/dsh-<timestamp>.db.gz.enc (default ~/.local/share/dsh/backups/)
set -euo pipefail

SRC="${REPO_ROOT:-$(git worktree list --porcelain | head -1 | sed 's/^worktree //')}/projects/DSH-dashboard"
cd "$SRC"

BACKUP_DIR="${BACKUP_DIR:-$HOME/.local/share/dsh/backups}"
KEEP_DAYS=30

VOLUME=$(docker volume ls --format '{{.Name}}' | grep -E '^dsh.*dsh-data$' | head -1)
if [ -z "$VOLUME" ]; then
  echo "❌ DSH data volume not found. Has DSH ever been started?" >&2
  exit 1
fi

if [ -n "${BACKUP_PASSPHRASE:-}" ]; then
  PASS="$BACKUP_PASSPHRASE"
else
  read -rsp "🔑 Backup passphrase: " PASS; echo
  [ -n "$PASS" ] || { echo "❌ Passphrase required." >&2; exit 1; }
fi

mkdir -p "$BACKUP_DIR"

TMPFILE=$(mktemp --suffix=.db)
trap 'shred -u "$TMPFILE" 2>/dev/null || rm -f "$TMPFILE"' EXIT

CONTAINER=$(docker compose ps -q dsh 2>/dev/null || true)

if [ -n "$CONTAINER" ]; then
  echo "📦 Backing up DSH database (hot backup):"
  echo "     /data/dsh.db"
  docker exec "$CONTAINER" sqlite3 /data/dsh.db ".backup /tmp/dsh-backup.db"
  docker cp "$CONTAINER:/tmp/dsh-backup.db" "$TMPFILE"
  docker exec "$CONTAINER" rm -f /tmp/dsh-backup.db
else
  echo "📦 Backing up DSH database (cold copy):"
  echo "     /data/dsh.db"
  docker run --rm -v "$VOLUME":/data:ro \
    alpine cat /data/dsh.db > "$TMPFILE"
fi

TIMESTAMP=$(date +%Y%m%d-%H%M%S)
OUTFILE="$BACKUP_DIR/dsh-$TIMESTAMP.db.gz.enc"

gzip -c "$TMPFILE" | openssl enc -aes-256-cbc -pbkdf2 -iter 600000 \
  -pass pass:"$PASS" -out "$OUTFILE"

find "$BACKUP_DIR" -name 'dsh-*.db.gz.enc' -mtime "+$KEEP_DAYS" -delete

echo "✅ DB backup: $OUTFILE"

if [ -f .env ]; then
  ENV_OUT="$BACKUP_DIR/dsh-$TIMESTAMP.env.enc"
  echo "📦 Backing up DSH .env:"
  echo "     .env"
  openssl enc -aes-256-cbc -pbkdf2 -iter 600000 \
    -pass pass:"$PASS" -in .env -out "$ENV_OUT"
  find "$BACKUP_DIR" -name 'dsh-*.env.enc' -mtime "+$KEEP_DAYS" -delete
  echo "✅ Env backup: $ENV_OUT"
fi
