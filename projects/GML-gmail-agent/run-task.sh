#!/usr/bin/env bash
# run-task.sh — GML Gmail Agent: run a single task
# Cycle: Analyze → Knowledge → Rules → (repeat, analysis excludes rule-matched emails)
#
# Pipeline commands (analyze, learn, distill, propose, apply-rules, watch-*)
# are handled by the Go binary directly — no Docker, no bash orchestration.
# This script builds the binary on first use and delegates.
#
# Remaining bash commands (run, watch-rules, profile, stats, etc.) still pipe
# credentials through Docker for backward compatibility.
#
# For daemon management use ./watch.sh instead.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GML_BIN="${SCRIPT_DIR}/data/.gml"

OP_ITEM_TOKEN="GML Gmail Read-Only Credentials"
OP_ITEM_TOKEN_RULES="GML Gmail Read-Write Credentials"
OP_FIELD_TOKEN="credential"

# Build the Go binary if missing or stale (any .go file newer than the binary).
ensure_binary() {
  local needs_build=false
  if [[ ! -f "$GML_BIN" ]]; then
    needs_build=true
  else
    if find "${SCRIPT_DIR}" -name '*.go' -newer "$GML_BIN" -print -quit | grep -q .; then
      needs_build=true
    fi
  fi
  if $needs_build; then
    echo "[gml] building binary..." >&2
    (cd "$SCRIPT_DIR" && CGO_ENABLED=0 go build -o "$GML_BIN" ./cmd/gml/) || {
      echo "error: go build failed" >&2; exit 1
    }
    echo "[gml] binary ready: $GML_BIN" >&2
  fi
}

# --- Pipeline commands: delegate to Go binary ---
case "${1:-}" in
  analyze|learn|distill|propose|apply-rules|watch-analysis|watch-knowledge)
    ensure_binary
    exec "$GML_BIN" "$@"
    ;;
esac

# --- Credential helpers (for Docker-based commands) ---

[[ -f "${SCRIPT_DIR}/data/rules.yaml" ]] || { echo "error: rules.yaml not found — see README.md for setup" >&2; exit 1; }

if [[ $# -eq 0 ]]; then
  ensure_binary
  exec "$GML_BIN"
fi

pipe_creds() {
  op item get "$OP_ITEM_TOKEN" --fields "$OP_FIELD_TOKEN" --reveal --format json \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['value'], end='')"
}

pipe_rules_creds() {
  if ! op item get "$OP_ITEM_TOKEN_RULES" &>/dev/null 2>&1; then
    echo "error: modify-scoped credentials not found in 1Password ('$OP_ITEM_TOKEN_RULES')." >&2
    echo "Run: ./setup.sh --rules" >&2
    exit 1
  fi
  op item get "$OP_ITEM_TOKEN_RULES" --fields "$OP_FIELD_TOKEN" --reveal --format json \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['value'], end='')"
}

send_creds() {
  if [[ -n "${GML_CACHED_CREDS:-}" ]]; then
    echo -n "$GML_CACHED_CREDS"
  else
    pipe_creds
  fi
}

send_rules_creds() {
  if [[ -n "${GML_CACHED_RULES_CREDS:-}" ]]; then
    echo -n "$GML_CACHED_RULES_CREDS"
  else
    pipe_rules_creds
  fi
}

# --- run (apply archive rules — uses modify-scoped credentials) ---
if [[ "$1" == "run" ]]; then
  shift
  send_rules_creds | docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T gml run "$@"
  exit 0
fi

# --- watch-rules (scheduled rules daemon — uses modify-scoped credentials) ---
if [[ "$1" == "watch-rules" ]]; then
  shift
  send_rules_creds | docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T gml watch-rules "$@"
  exit 0
fi

# All other commands: pipe read-only credentials to container
knowledge_vol=""
if [[ -f "${SCRIPT_DIR}/data/knowledge.yaml" ]]; then
  knowledge_vol="-v ${SCRIPT_DIR}/data/knowledge.yaml:/app/data/knowledge.yaml:ro"
fi
send_creds | docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T $knowledge_vol gml "$@"
