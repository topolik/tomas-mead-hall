#!/usr/bin/env bash
# setup.sh — build DSH, start containers, optionally initialize Tailscale.
# Called by the top-level setup.sh, or run standalone.
set -euo pipefail
cd "$(dirname "$0")"

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BOLD='\033[1m'; NC='\033[0m'
ok()     { echo -e "  ${GREEN}✓${NC} $*"; }
warn()   { echo -e "  ${YELLOW}⚠${NC} $*"; }
ask_yn() { local ans; read -rp "  $1 [y/N] " ans; [[ "$ans" =~ ^[Yy]$ ]]; }

# ── Build & start ─────────────────────────────────────────────────────────

echo "  Building and starting DSH containers..."
docker compose up -d --build 2>&1 | sed 's/^/  /'

printf "  Waiting for DSH..."
until curl -sf http://localhost:9090/api/v1/health >/dev/null 2>&1; do
  printf "."
  sleep 1
done
echo " ready."
ok "DSH running on http://localhost:9090"

# ── Tailscale (optional) ─────────────────────────────────────────────────

echo ""
echo "  Tailscale creates a private HTTPS network so you can access"
echo "  DSH from your phone (passkeys, push notifications)."
echo ""
echo "  Before continuing, install the Tailscale app on your phone:"
echo "    iOS:     https://apps.apple.com/app/tailscale/id1470499037"
echo "    Android: https://play.google.com/store/apps/details?id=com.tailscale.ipn"
echo ""
echo "  Sign in on your phone with the same account you'll use here."
echo ""

if ask_yn "Initialize Tailscale now?"; then
  printf "  Waiting for Tailscale daemon..."
  until docker compose exec -T tailscale tailscale status --json >/dev/null 2>&1; do
    printf "."
    sleep 1
  done
  echo " ready."

  state=$(docker compose exec -T tailscale tailscale status --json 2>/dev/null | jq -r .BackendState) || true

  if [ "$state" = "Running" ]; then
    ok "Tailscale already authenticated"
  else
    echo ""
    echo "  ═══════════════════════════════════════════════"
    echo "  Open the URL below in your browser to authorize"
    echo "  this machine on your Tailscale network:"
    echo "  ═══════════════════════════════════════════════"
    echo ""
    docker compose exec tailscale tailscale up
    echo ""
    ok "Tailscale authenticated"
  fi

  docker compose exec -T tailscale tailscale serve --bg --https=443 http://localhost:9090 >/dev/null 2>&1

  dns=$(docker compose exec -T tailscale tailscale status --json | jq -r '.Self.DNSName' | sed 's/\.$//')

  ts_origin="http://localhost:9090,https://${dns}"
  echo "DSH_ORIGIN=${ts_origin}" > .env

  echo ""
  ok "Tailscale HTTPS proxy: https://${dns}"
  echo ""
  echo "  DSH is now reachable from any device on your tailnet."
  echo "  Open https://${dns} on your phone to verify."
  ok "Tailscale initialized (auth state persists in Docker volume)"
else
  ok "Tailscale setup skipped (will happen on first ./run.sh)"
fi
