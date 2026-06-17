#!/usr/bin/env bash
# backup.sh — encrypted backup of GML's learned state.
# Backs up: data/ (knowledge, dsh credentials, rules, state files)
#
# Passphrase: $BACKUP_PASSPHRASE env var, or prompted interactively.
# Output:     $BACKUP_DIR/gml-<timestamp>.tar.gz.enc (default ~/.local/share/gml/backups/)
set -euo pipefail
cd "$(dirname "$0")"

BACKUP_DIR="${BACKUP_DIR:-$HOME/.local/share/gml/backups}"
KEEP_DAYS=30

if [ -n "${BACKUP_PASSPHRASE:-}" ]; then
  PASS="$BACKUP_PASSPHRASE"
else
  read -rsp "🔑 Backup passphrase: " PASS; echo
  [ -n "$PASS" ] || { echo "❌ Passphrase required." >&2; exit 1; }
fi

mkdir -p "$BACKUP_DIR"

FILES=()
for f in data/knowledge.yaml data/dsh.yaml data/rules.yaml data/.gml-last-*; do
  [ -e "$f" ] && FILES+=("$f")
done

if [ ${#FILES[@]} -eq 0 ]; then
  echo "❌ Nothing to back up — no GML state files found." >&2
  exit 1
fi

TIMESTAMP=$(date +%Y%m%d-%H%M%S)
OUTFILE="$BACKUP_DIR/gml-$TIMESTAMP.tar.gz.enc"

echo "📧 Backing up GML state (${#FILES[@]} files):"
printf '     %s\n' "${FILES[@]}"
tar czf - "${FILES[@]}" | openssl enc -aes-256-cbc -pbkdf2 -iter 600000 \
  -pass pass:"$PASS" -out "$OUTFILE"

find "$BACKUP_DIR" -name 'gml-*.enc' -mtime "+$KEEP_DAYS" -delete

echo "✅ Backup: $OUTFILE"
echo "   Restore: ./restore.sh $OUTFILE"
