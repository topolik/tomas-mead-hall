#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

docker compose up -d --build "$@"

# Wait for tailscaled to respond
printf "Waiting for Tailscale daemon..."
until docker compose exec -T tailscale tailscale status --json >/dev/null 2>&1; do
  printf "."
  sleep 1
done
echo " ready."

# Wait for Tailscale to settle (may be reconnecting from saved state)
printf "Waiting for Tailscale..."
for i in $(seq 1 15); do
  state=$(docker compose exec -T tailscale tailscale status --json 2>/dev/null | jq -r .BackendState) || true
  if [ "$state" = "Running" ]; then break; fi
  printf "."
  sleep 1
done
echo ""

# Authenticate if needed (first run only — state persists in volume)
if [ "$state" != "Running" ]; then
  echo ""
  echo "=========================================="
  echo "  First-time Tailscale setup"
  echo "=========================================="
  echo ""
  echo "Tailscale creates a private network so you can access DSH"
  echo "from your phone and other devices over HTTPS."
  echo ""
  echo "Before continuing:"
  echo "  1. Create a free account at https://tailscale.com if you don't have one"
  echo "  2. Install the Tailscale app on your phone (App Store / Play Store)"
  echo "  3. Sign in to Tailscale on your phone with the same account"
  echo ""
  echo "Then open the URL below to authorize this machine:"
  echo ""
  docker compose exec tailscale tailscale up
  echo ""
  echo "Once authorized, your phone and this machine are on the same"
  echo "private network — DSH will be reachable over HTTPS from any"
  echo "device on your tailnet."
  echo ""
fi

# Ensure HTTPS proxy is configured (idempotent)
docker compose exec -T tailscale tailscale serve --bg --https=443 http://localhost:9090 >/dev/null 2>&1

dns=$(docker compose exec -T tailscale tailscale status --json | jq -r '.Self.DNSName' | sed 's/\.$//')

# Write .env with the discovered Tailscale origin so docker-compose picks it up.
# If DSH_ORIGIN doesn't already include the Tailscale domain, restart DSH to apply it.
ts_origin="http://localhost:9090,https://${dns}"
current_origin="${DSH_ORIGIN:-}"
if [ "$current_origin" != "$ts_origin" ]; then
  echo "DSH_ORIGIN=${ts_origin}" > .env
  export DSH_ORIGIN="$ts_origin"
  echo "  Restarting DSH with Tailscale origin..."
  docker compose up -d --no-deps dsh
fi

echo ""
echo "DSH is running:"
echo "  Local:     http://localhost:9090"
echo "  Tailscale: https://${dns}"
echo ""
echo "First run (no passkey yet): open http://localhost:9090/setup to register one."
echo ""
echo "Add a phone or another device later: log in, then"
echo "  Passkeys -> \"Add a new device\" — it shows a QR to scan that mints a"
echo "  fresh 10-min enrollment link on demand (the passkey binds to ${dns})."
