#!/usr/bin/env bash
# smoke.sh — exercise a RUNNING LLP instance end-to-end: handshake, health,
# models, completions, model selection, the bad-model error path, auth, usage.
# Obtains a session token via the control-socket handshake (no keys needed).
#
#   LLP_URL     base url (default http://localhost:4000)
#   LLP_SOCKET  control socket (default ~/.llp/control.sock)
set -euo pipefail
cd "$(dirname "$0")"

URL="${LLP_URL:-http://localhost:4000}"
SOCKET="${LLP_SOCKET:-$HOME/.llp/control.sock}"

[ -S "$SOCKET" ] || { echo "control socket $SOCKET not found — is llp running? (./run.sh)" >&2; exit 1; }

say() { printf '\n\033[1m== %s ==\033[0m\n' "$1"; }

say "handshake (register over the control socket)"
KEY="$(curl -s --unix-socket "$SOCKET" -X POST http://unix/register \
  -H 'Content-Type: application/json' -d '{"agent":"smoke"}' | jq -r .token)"
[ -n "$KEY" ] && [ "$KEY" != "null" ] || { echo "handshake failed" >&2; exit 1; }
printf '  got session token %s… (in memory only)\n' "${KEY:0:12}"

comp() { # $1 = model value
  local body resp
  body=$(jq -n --arg m "$1" '{model:$m, messages:[{role:"user",content:"Reply with exactly one word: PONG"}]}')
  resp=$(curl -s --max-time 120 "$URL/v1/chat/completions" \
    -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" -d "$body")
  printf '  %-28s -> %s\n' "$1" \
    "$(echo "$resp" | jq -r 'if .error then "ERROR: "+(.error.message[0:60]) else "served_by="+.model+"  "+(.choices[0].message.content|tojson) end')"
}

say "health";  curl -s "$URL/healthz" | jq -c '{status, impls:[.impls[]|{(.name):(if .cooling_down then "cooldown" elif .available then "up" else "disabled" end)}]}'
say "models";  curl -s "$URL/v1/models" -H "Authorization: Bearer $KEY" | jq -c '[.data[].id]'

say "model selection (real LLM calls — takes ~1 min)"
comp auto
comp gemini
comp claude
comp gemini/gemini-2.5-flash
comp claude/haiku
comp gemini/not-a-real-model

say "auth (missing token must be 401)"
curl -s -o /dev/null -w '  no-token status=%{http_code}\n' "$URL/v1/chat/completions" \
  -H "Content-Type: application/json" -d '{"model":"auto","messages":[{"role":"user","content":"x"}]}'

say "usage so far"; curl -s "$URL/admin/usage" -H "Authorization: Bearer $KEY" | jq -c '.usage[]'
echo
