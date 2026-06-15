#!/usr/bin/env bash
# restore.sh — restore GML state from an encrypted backup.
# Usage: ./restore.sh <backup.tar.gz.enc>
set -euo pipefail
cd "$(dirname "$0")"

if [ $# -ne 1 ]; then
  echo "Usage: $0 <backup.tar.gz.enc>" >&2
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

echo "📧 Restoring GML state from $BACKUP_FILE..."
mkdir -p data
openssl enc -d -aes-256-cbc -pbkdf2 -iter 600000 \
  -pass pass:"$PASS" -in "$BACKUP_FILE" | tar xzf -

chmod 600 data/dsh.yaml 2>/dev/null || true

echo "✅ Restore complete."
echo "   Restored files:"
openssl enc -d -aes-256-cbc -pbkdf2 -iter 600000 \
  -pass pass:"$PASS" -in "$BACKUP_FILE" | tar tzf - | sed 's/^/     /'
