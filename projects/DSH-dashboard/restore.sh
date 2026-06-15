#!/usr/bin/env bash
# restore.sh — restore DSH database from an encrypted backup.
# If DSH is running, stops it before restore and restarts after.
# Works with or without a running DSH container.
#
# Usage: ./restore.sh <backup.db.gz.enc>
set -euo pipefail
cd "$(dirname "$0")"

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

echo "✅ Restore complete."
