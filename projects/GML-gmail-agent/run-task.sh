#!/usr/bin/env bash
# run-task.sh — GML Gmail Agent: run a single task inside Docker
# All commands run via docker compose — the image bundles the Go binary and gws CLI.
#
# For daemon management use ./watch.sh instead.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.yml"
export UID="$(id -u)"
export GID="$(id -g)"

OP_ITEM_TOKEN="GML Gmail Read-Only Credentials"
OP_ITEM_TOKEN_RULES="GML Gmail Read-Write Credentials"
OP_FIELD_TOKEN="credential"

[[ -f "${SCRIPT_DIR}/data/rules.yaml" ]] || { echo "error: rules.yaml not found — see README.md for setup" >&2; exit 1; }

if [[ $# -eq 0 ]]; then
  docker compose -f "$COMPOSE_FILE" run --rm -T gml
  exit 0
fi

# --- Credential helpers ---

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

# LLP proxy: default URL + socket, mount socket into container for handshake
export LLP_URL="${LLP_URL:-http://localhost:4000}"
export LLP_SOCKET="${LLP_SOCKET:-${HOME}/.llp/control.sock}"
extra_args=()
if [[ -S "$LLP_SOCKET" ]]; then
  extra_args+=(-v "${LLP_SOCKET}:${LLP_SOCKET}" -e "LLP_SOCKET=${LLP_SOCKET}")
fi

docker_run() {
  docker compose -f "$COMPOSE_FILE" run --rm -T "${extra_args[@]}" gml "$@"
}

# --- Route commands ---

case "${1:-}" in
  # Pipeline commands that need read-only credentials
  analyze|learn|watch-analysis|watch-knowledge)
    send_creds | docker_run "$@"
    ;;

  # Pipeline commands that don't need credentials
  distill|propose|apply-rules)
    docker_run "$@"
    ;;

  # Archive rules — uses modify-scoped credentials
  run|watch-rules)
    send_rules_creds | docker_run "$@"
    ;;

  # All other commands: pipe read-only credentials
  *)
    send_creds | docker_run "$@"
    ;;
esac
