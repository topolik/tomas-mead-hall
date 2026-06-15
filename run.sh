#!/usr/bin/env bash
# run.sh — start or restart all projects
#
# Order: DSH → LLP → GML → MND
#   DSH first: GML and MND post notifications to it.
#   LLP before GML: GML can route LLM calls through the proxy when LLP_URL is set.
#
# Usage: ./run.sh
set -uo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"

if [ ! -f "${REPO}/projects/LLP-llm-proxy/config.yaml" ]; then
  echo "❌ Setup has not been run yet. Run ./setup.sh first." >&2
  exit 1
fi

FAILURES=()

step() { echo ""; echo "🚀 $* ────────────────────────────────────────────"; }

run_step() {
  local name="$1"; shift
  if "$@"; then
    return 0
  else
    echo "  ❌ $name: FAILED (exit $?)" >&2
    FAILURES+=("$name")
    return 0
  fi
}

# ── 1/4  DSH — dashboard (docker compose) ──────────────────────────────────
step "1/4  DSH — dashboard"
run_step DSH "${REPO}/projects/DSH-dashboard/run.sh"

# ── 2/4  LLP — llm proxy (tmux daemon) ─────────────────────────────────────
step "2/4  LLP — llm proxy"
run_step LLP "${REPO}/projects/LLP-llm-proxy/watch.sh" restart

# ── 3/4  GML — gmail agent (tmux daemons: analysis, knowledge, rules) ───────
step "3/4  GML — gmail agent"
echo ""
echo "  🔑 GML's analysis and knowledge daemons will request Gmail credentials"
echo "     from 1Password (op) inside their tmux sessions."
echo "     You may see a biometric/password prompt from 1Password — that's expected."
echo ""
run_step GML "${REPO}/projects/GML-gmail-agent/watch.sh" restart

# ── 4/4  MND — mind model (tmux daemon: watch-retrain) ──────────────────────
step "4/4  MND — mind model"
run_step MND "${REPO}/projects/MND-mind-model/watch.sh" restart

# ── summary ─────────────────────────────────────────────────────────────────
echo ""
if [[ ${#FAILURES[@]} -eq 0 ]]; then
  echo "✅ All services started."
else
  echo "⚠️  Started with failures: ${FAILURES[*]}"
fi
echo ""
echo "── 📊 Status ─────────────────────────────────────────"
echo ""
echo "📦 DSH:"
echo "  http://localhost:9090"
if [[ -f "${REPO}/projects/DSH-dashboard/.env" ]]; then
  grep DSH_ORIGIN "${REPO}/projects/DSH-dashboard/.env" 2>/dev/null \
    | sed 's/.*https/  https/' | tr ',' '\n' | grep https || true
fi
echo ""
echo "🔀 LLP:"
"${REPO}/projects/LLP-llm-proxy/watch.sh" status
echo ""
echo "📧 GML:"
"${REPO}/projects/GML-gmail-agent/watch.sh" status
echo ""
echo "🧠 MND:"
"${REPO}/projects/MND-mind-model/watch.sh" status
