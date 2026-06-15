#!/usr/bin/env bash
# setup.sh — provision MND's DSH connection and detect LLM providers.
# Called by the top-level setup.sh, or run standalone.
set -euo pipefail
cd "$(dirname "$0")"

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
ok()   { echo -e "  ${GREEN}✓${NC} $*"; }
warn() { echo -e "  ${YELLOW}⚠${NC} $*"; }

DSH_DIR="../DSH-dashboard"

# ── DSH client provisioning ──────────────────────────────────────────────

if [ -f "data/dsh.yaml" ] && grep -q 'client_id: "dsh_' "data/dsh.yaml" 2>/dev/null; then
  ok "DSH client already provisioned"
else
  if curl -sf http://localhost:9090/api/v1/health >/dev/null 2>&1; then
    json=$(cd "$DSH_DIR" && docker compose exec -T dsh /app/dsh create-client MND 2>/dev/null) || true
    if [ -n "$json" ]; then
      cid=$(echo "$json" | jq -r .client_id)
      csec=$(echo "$json" | jq -r .client_secret)
      if [ -n "$cid" ] && [ "$cid" != "null" ]; then
        mkdir -p data
        cat > data/dsh.yaml <<CREDS
url: "http://localhost:9090"
client_id: "${cid}"
client_secret: "${csec}"
CREDS
        chmod 600 data/dsh.yaml
        ok "DSH client provisioned"
      else
        warn "Failed to parse DSH response — create client manually at /admin/clients"
      fi
    else
      warn "Failed to create DSH client — create manually at /admin/clients"
    fi
  else
    mkdir -p data
    cp dsh.yaml.example data/dsh.yaml
    chmod 600 data/dsh.yaml
    warn "DSH not running — created empty dsh.yaml (fill credentials from /admin/clients)"
  fi
fi

# ── LLM provider detection ──────────────────────────────────────────────

HAS_GEMINI=false
HAS_CLAUDE=false
command -v npx >/dev/null 2>&1 && HAS_GEMINI=true
command -v claude >/dev/null 2>&1 && HAS_CLAUDE=true

DEFAULT_MODEL="gemini"
if ! $HAS_GEMINI && $HAS_CLAUDE; then
  DEFAULT_MODEL="claude"
fi

echo ""
echo "  MND will use '$DEFAULT_MODEL' as the default LLM provider."
echo "  Override per-command: ./run-task.sh distill --model claude"
echo "  Or set MND_LLP=on (default) to route through LLP with automatic failover."
ok "MND configured (default model: $DEFAULT_MODEL)"
