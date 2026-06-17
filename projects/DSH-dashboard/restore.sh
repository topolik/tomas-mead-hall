#!/usr/bin/env bash
# restore.sh — restore DSH database and .env from encrypted backups.
# If DSH is running, stops it before restore and restarts after.
#
# Usage: ./restore.sh <backup.db.gz.enc>
set -euo pipefail

SRC="${REPO_ROOT:-$(git worktree list --porcelain | head -1 | sed 's/^worktree //')}/projects/DSH-dashboard"
cd "$SRC"

if [ $# -ne 1 ]; then
  echo "Usage: $0 <backup.db.gz.enc>" >&2
  exit 1
fi

BACKUP_FILE="$1"
if [ ! -f "$BACKUP_FILE" ]; then
  echo "❌ $BACKUP_FILE not found." >&2
  exit 1
fi

if [ -n "${BACKUP_PASSPHRASE:-}" ]; then
  PASS="$BACKUP_PASSPHRASE"
else
  read -rsp "🔑 Backup passphrase: " PASS; echo
  [ -n "$PASS" ] || { echo "❌ Passphrase required." >&2; exit 1; }
fi

TMPFILE=$(mktemp --suffix=.db)
trap 'shred -u "$TMPFILE" 2>/dev/null || rm -f "$TMPFILE"' EXIT

echo "📦 Decrypting $BACKUP_FILE..."
if [[ "$BACKUP_FILE" == *.gz.enc ]]; then
  openssl enc -d -aes-256-cbc -pbkdf2 -iter 600000 \
    -pass pass:"$PASS" -in "$BACKUP_FILE" | gunzip > "$TMPFILE"
else
  openssl enc -d -aes-256-cbc -pbkdf2 -iter 600000 \
    -pass pass:"$PASS" -in "$BACKUP_FILE" -out "$TMPFILE"
fi

WAS_RUNNING=false
if docker compose ps -q dsh 2>/dev/null | grep -q .; then
  WAS_RUNNING=true
  echo "🛑 Stopping DSH..."
  docker compose stop dsh
fi

VOLUME=$(docker volume ls --format '{{.Name}}' | grep -E '^dsh.*dsh-data$' | head -1)
if [ -z "$VOLUME" ]; then
  VOLUME="dsh_dsh-data"
  echo "📦 Creating volume $VOLUME..."
  docker volume create "$VOLUME" >/dev/null
fi

echo "📦 Replacing database..."
docker run --rm -v "$VOLUME":/data -v "$TMPFILE":/tmp/restore.db \
  alpine sh -c 'cp /tmp/restore.db /data/dsh.db && rm -f /data/dsh.db-wal /data/dsh.db-shm'

if [ "$WAS_RUNNING" = true ]; then
  echo "🚀 Restarting DSH..."
  docker compose start dsh
fi

echo "✅ DB restore complete."
echo "   Restored files:"
echo "     /data/dsh.db"

ENV_BACKUP="${BACKUP_FILE%.db.gz.enc}.env.enc"
if [ -f "$ENV_BACKUP" ]; then
  echo "📦 Restoring .env from $ENV_BACKUP..."
  openssl enc -d -aes-256-cbc -pbkdf2 -iter 600000 \
    -pass pass:"$PASS" -in "$ENV_BACKUP" -out .env
  chmod 600 .env
  echo "     .env"
fi
