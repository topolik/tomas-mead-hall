#!/usr/bin/env bash
# setup.sh — interactive first-run setup for the AI Team Workspace.
#
# Checks prerequisites, detects LLM providers, provisions config files,
# walks through GML OAuth, and initializes Tailscale for phone access.
#
# Safe to re-run — each step is idempotent and skips if already done.
set -uo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; BOLD='\033[1m'; NC='\033[0m'
OK=0 WARN=0 FAIL=0

ok()      { echo -e "  ${GREEN}✅${NC} $*"; OK=$((OK+1)); }
warn()    { echo -e "  ${YELLOW}⚠️${NC}  $*"; WARN=$((WARN+1)); }
fail()    { echo -e "  ${RED}❌${NC} $*"; FAIL=$((FAIL+1)); }
section() { echo ""; echo -e "${BOLD}🔧 $* ──────────────────────────────────────────${NC}"; }
ask_yn()  { local ans; read -rp "  $1 [y/N] " ans; [[ "$ans" =~ ^[Yy]$ ]]; }

HAS_GEMINI=false HAS_CLAUDE=false GCP_PROJECT=""

# ═══════════════════════════════════════════════════════════════════════════
# 1. Prerequisites
# ═══════════════════════════════════════════════════════════════════════════
section "1/6  🖥️  Host prerequisites"

BLOCKER=false

check_required() {
  local cmd="$1" purpose="$2" hint="$3"
  if command -v "$cmd" >/dev/null 2>&1; then
    ok "$cmd ($purpose)"
  else
    fail "$cmd — $purpose"
    echo "       Install: $hint"
    BLOCKER=true
  fi
}

check_required docker "containers (all projects build and run in Docker)" \
  "https://docs.docker.com/engine/install/"
check_required tmux   "background daemons for LLP, GML, MND" \
  "apt install tmux / brew install tmux"
check_required jq     "JSON parsing (Tailscale status, LLP healthz)" \
  "apt install jq / brew install jq"

if docker compose version >/dev/null 2>&1; then
  ok "docker compose (container orchestration)"
else
  fail "docker compose plugin"
  echo "       Install: https://docs.docker.com/compose/install/"
  BLOCKER=true
fi

if $BLOCKER; then
  echo ""
  echo -e "  ${RED}❌ Missing required tools above. Install them and re-run ./setup.sh${NC}"
  exit 1
fi

# ═══════════════════════════════════════════════════════════════════════════
# 2. LLM providers
# ═══════════════════════════════════════════════════════════════════════════
section "2/6  🤖 LLM providers"

echo "  GML, LLP, and MND use LLM CLIs on the host for AI reasoning."
echo "  You need at least one provider. Both is better (failover)."
echo ""

# Gemini
if command -v npx >/dev/null 2>&1; then
  ok "npx available (runs gemini-cli)"
  HAS_GEMINI=true
else
  warn "npx not found — Gemini provider will be unavailable"
  echo "       Install Node.js: https://nodejs.org/"
fi

# Claude
if command -v claude >/dev/null 2>&1; then
  ok "claude CLI available"
  HAS_CLAUDE=true
else
  warn "claude CLI not found — Claude provider will be unavailable"
  echo "       Install: npm install -g @anthropic-ai/claude-code"
fi

if ! $HAS_GEMINI && ! $HAS_CLAUDE; then
  echo ""
  echo -e "  ${RED}No LLM providers detected.${NC} Install at least one:"
  echo "    Gemini: npm install -g npx (then gemini-cli runs via npx)"
  echo "    Claude: npm install -g @anthropic-ai/claude-code"
  echo ""
  echo "  LLP, GML analysis, and MND distillation won't work without one."
  echo ""
fi

# GCP project (needed for gemini-cli) — passed to LLP's setup.sh
if $HAS_GEMINI && [ ! -f "${REPO}/projects/LLP-llm-proxy/config.yaml" ]; then
  echo ""
  echo "  Gemini CLI needs a Google Cloud project ID."
  echo "  Create one at: https://console.cloud.google.com/projectcreate"
  echo "  Then enable the Gemini API in that project."
  echo ""
  read -rp "  GCP project ID (or Enter to skip): " GCP_PROJECT
  if [[ -n "$GCP_PROJECT" ]]; then
    ok "GCP project: $GCP_PROJECT"
  else
    warn "No GCP project set — LLP setup will ask, or edit config manually"
  fi
elif $HAS_GEMINI; then
  ok "GCP project already configured in LLP config.yaml"
fi

# ═══════════════════════════════════════════════════════════════════════════
# 3. LLP — LLM Proxy config
# ═══════════════════════════════════════════════════════════════════════════
section "3/6  🔀 LLP — LLM Proxy"

LLP_DIR="${REPO}/projects/LLP-llm-proxy"

(cd "$LLP_DIR" && GCP_PROJECT="$GCP_PROJECT" ./setup.sh)

# ═══════════════════════════════════════════════════════════════════════════
# 4. DSH — Dashboard & Tailscale
# ═══════════════════════════════════════════════════════════════════════════
section "4/6  📦 DSH — Dashboard & Tailscale"

DSH_DIR="${REPO}/projects/DSH-dashboard"

(cd "$DSH_DIR" && ./setup.sh)

# ═══════════════════════════════════════════════════════════════════════════
# 5. GML — Gmail Agent
# ═══════════════════════════════════════════════════════════════════════════
section "5/6  📧 GML — Gmail Agent"

GML_DIR="${REPO}/projects/GML-gmail-agent"

echo "  GML automates email triage. It needs:"
echo "    • A GCP project with the Gmail API enabled"
echo "    • OAuth credentials (Desktop app) for Gmail access"
echo "    • 1Password CLI (op) to store credentials securely"
echo ""

if ! command -v op >/dev/null 2>&1; then
  warn "1Password CLI (op) not found — skipping GML Gmail setup"
  echo "       Install: https://developer.1password.com/docs/cli/get-started/"
  echo "       Then re-run ./setup.sh to complete GML setup."
  # Still provision DSH client even without op
  (cd "${GML_DIR}" && ./setup.sh --dsh-only) 2>/dev/null || true
elif ! op account list >/dev/null 2>&1; then
  warn "1Password CLI not signed in — skipping GML Gmail setup"
  echo "       Run: op signin"
  echo "       Then re-run ./setup.sh to complete GML setup."
  (cd "${GML_DIR}" && ./setup.sh --dsh-only) 2>/dev/null || true
elif ask_yn "Set up GML Gmail credentials now? (needs a browser)"; then
  echo ""
  (cd "${GML_DIR}" && ./setup.sh)
  ok "GML setup complete"
else
  # Provision DSH client even if skipping Gmail OAuth
  (cd "${GML_DIR}" && ./setup.sh --dsh-only) 2>/dev/null || true
  ok "GML Gmail setup skipped (run projects/GML-gmail-agent/setup.sh later)"
fi

# ═══════════════════════════════════════════════════════════════════════════
# 6. MND — Mind Model
# ═══════════════════════════════════════════════════════════════════════════
section "6/6  🧠 MND — Mind Model"

MND_DIR="${REPO}/projects/MND-mind-model"

(cd "$MND_DIR" && ./setup.sh)

# ── Stop DSH (run.sh will start everything in order) ─────────────────────

echo ""
echo "  Stopping DSH containers (./run.sh will start them properly)..."
(cd "$DSH_DIR" && docker compose down) 2>&1 | sed 's/^/  /'

# ═══════════════════════════════════════════════════════════════════════════
# Summary
# ═══════════════════════════════════════════════════════════════════════════
echo ""
echo "════════════════════════════════════════════════════════"
if [ "$FAIL" -gt 0 ]; then
  echo -e "  ❌ ${RED}${FAIL} failed${NC}, ${WARN} warnings, ${OK} ready"
  echo "  Fix the failures above and re-run ./setup.sh"
else
  echo -e "  ✅ ${GREEN}Setup complete${NC} (${WARN} warnings, ${OK} checks passed)"
  echo ""
  echo "  Next:"
  echo "    🚀 ./run.sh     — start all services"
  echo "    🛑 ./stop.sh    — stop all services"
  echo ""
  echo "  📦 DSH:"
  echo "    Local:     http://localhost:9090"
  if [ -f "${DSH_DIR}/.env" ]; then
    ts_host=$(grep DSH_ORIGIN "${DSH_DIR}/.env" | sed 's/.*https:\/\///' | tr -d ',')
    echo "    Tailscale: https://${ts_host}"
  fi
  echo "    👉 First run: open http://localhost:9090/setup to register a passkey"
fi
echo "════════════════════════════════════════════════════════"
