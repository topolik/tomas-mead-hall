#!/usr/bin/env bash
# run.sh — build and launch LLP on the host (see ASSUMPTIONS LLP-006).
#
# No keys to set up: the data API binds to loopback, and agents obtain a session
# token at startup over the control socket (~/.llp/control.sock by default), held
# only in memory. Provider auth (gemini/claude) uses the host CLIs' own sessions.
set -euo pipefail
cd "$(dirname "$0")"

CONFIG="${LLP_CONFIG:-config.yaml}"
if [ ! -f "$CONFIG" ]; then
  echo "No $CONFIG found — copying from config.example.yaml"
  cp config.example.yaml "$CONFIG"
fi

# SECURITY GUARD (LLP-017): never start a gemini-cli impl without an explicit
# --approval-mode. Headless gemini-cli inherits the user-level approval mode
# (auto_edit on this host => it can WRITE FILES, incident MND-011). This catches
# a config.yaml regenerated from a stale example — the exact silent failure of
# 2026-06-12 20:10. Line-level: every `command:` line that execs gemini-cli must
# carry the flag on that line. The llp binary enforces the same invariant on the
# parsed argv (registry.Build), which also covers exotic YAML layouts.
if grep -E '^[[:space:]]*command:.*gemini-cli' "$CONFIG" | grep -qv -- '--approval-mode'; then
  echo "FATAL: $CONFIG defines a gemini-cli command without --approval-mode." >&2
  echo "  Headless gemini-cli inherits user-level defaultApprovalMode (auto_edit => can write files)." >&2
  echo "  Add \"--approval-mode\", \"default\" to the gemini command (see ASSUMPTIONS LLP-014/LLP-017)." >&2
  exit 1
fi

echo "Building llp…"
if command -v go >/dev/null 2>&1; then
  go build -o llp ./cmd/llp
else
  docker build -f Dockerfile.build -o . .
fi

exec ./llp --config "$CONFIG" "$@"
