#!/usr/bin/env bash
# backup.sh — encrypted backup of MND's brain and working data.
# Backs up: data/ (insights, profiles, feedback ledger, dsh.yaml)
#
# Passphrase: $BACKUP_PASSPHRASE env var, or prompted interactively.
# Output:     $BACKUP_DIR/mnd-<timestamp>.tar.gz.enc (default ~/.local/share/mnd/backups/)
set -euo pipefail
cd "$(dirname "$0")"

BACKUP_DIR="${BACKUP_DIR:-$HOME/.local/share/mnd/backups}"
KEEP_DAYS=30

if [ -n "${BACKUP_PASSPHRASE:-}" ]; then
  PASS="$BACKUP_PASSPHRASE"
else
  read -rsp "🔑 Backup passphrase: " PASS; echo
  [ -n "$PASS" ] || { echo "❌ Passphrase required." >&2; exit 1; }
fi

mkdir -p "$BACKUP_DIR"

FILES=()
for f in data/insights.yaml data/processed.yaml data/feedback-ledger.yaml data/sent-ledger.jsonl data/profiles.json data/profiles/*.md data/dsh.yaml; do
  [ -e "$f" ] && FILES+=("$f")
done

if [ ${#FILES[@]} -eq 0 ]; then
  echo "❌ Nothing to back up — no MND state files found." >&2
  exit 1
fi

TIMESTAMP=$(date +%Y%m%d-%H%M%S)
OUTFILE="$BACKUP_DIR/mnd-$TIMESTAMP.tar.gz.enc"

echo "🧠 Backing up MND brain (${#FILES[@]} files):"
printf '     %s\n' "${FILES[@]}"
tar czf - "${FILES[@]}" | openssl enc -aes-256-cbc -pbkdf2 -iter 600000 \
  -pass pass:"$PASS" -out "$OUTFILE"

find "$BACKUP_DIR" -name 'mnd-*.enc' -mtime "+$KEEP_DAYS" -delete

echo "✅ Backup: $OUTFILE"
echo "   Restore: ./restore.sh $OUTFILE"
