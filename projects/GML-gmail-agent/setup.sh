#!/usr/bin/env bash
# setup.sh — one-time auth setup for GML Gmail Agent
# Requires: op (1Password CLI) + docker.
# Run once on a machine with a browser. After this, use run-task.sh or watch.sh.

set -euo pipefail

OP_ITEM_CLIENT="GML Gmail Agent"
OP_ITEM_TOKEN="GML Gmail Read-Only Credentials"
OP_ITEM_TOKEN_RULES="GML Gmail Read-Write Credentials"
OP_FIELD_TOKEN="credential"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

info()    { echo -e "${GREEN}[setup]${NC} $*"; }
warn()    { echo -e "${YELLOW}[warn]${NC}  $*"; }
fail()    { echo -e "${RED}[error]${NC} $*" >&2; exit 1; }
heading() { echo -e "\n${GREEN}=== $* ===${NC}"; }

DSH_DIR="../DSH-dashboard"
DSH_ONLY=false
[[ "${1:-}" == "--dsh-only" ]] && DSH_ONLY=true

GWS_CONFIG=""
cleanup() {
  rm -rf "${GWS_CONFIG:-}" 2>/dev/null || true
}
trap cleanup EXIT

# ── DSH client provisioning ───────────────────────────────────────────────────

heading "DSH connection"

if [ -f "data/dsh.yaml" ] && grep -q 'client_id: "dsh_' "data/dsh.yaml" 2>/dev/null; then
  info "DSH client already provisioned"
else
  if curl -sf http://localhost:9090/api/v1/health >/dev/null 2>&1; then
    json=$(cd "$DSH_DIR" && docker compose exec -T dsh /app/dsh create-client GML 2>/dev/null) || true
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
        info "DSH client provisioned"
      else
        warn "Failed to parse DSH response — create client manually at /admin/clients"
      fi
    else
      warn "Failed to create DSH client — create manually at /admin/clients"
    fi
  else
    warn "DSH not running — provision DSH client later (run top-level ./setup.sh)"
  fi
fi

$DSH_ONLY && exit 0

# ── Prerequisites ─────────────────────────────────────────────────────────────

heading "Checking prerequisites"

if ! command -v op &>/dev/null; then
  fail "1Password CLI (op) not found. Install: https://developer.1password.com/docs/cli/get-started/"
fi
if ! op account list &>/dev/null 2>&1; then
  fail "1Password CLI is not signed in. Run: op signin"
fi
info "1Password CLI: OK"

command -v docker &>/dev/null || fail "Docker not found."
info "Docker: OK"

# ── Step 1: Build image ────────────────────────────────────────────────────────

heading "Building Docker image"
docker compose build
info "Image ready."

# ── Step 2: Read-only credentials (gmail.readonly) ───────────────────────────

setup_readonly_creds() {
  heading "Checking existing read-only credentials"

  if op item get "$OP_ITEM_TOKEN" &>/dev/null 2>&1; then
    warn "1Password item '$OP_ITEM_TOKEN' already exists."
    read -rp "  Re-run auth and overwrite? [y/N] " OVERWRITE
    if [[ ! "$OVERWRITE" =~ ^[Yy]$ ]]; then
      info "Keeping existing read-only credentials."
      return 0
    fi
  fi

  heading "OAuth client credentials"

  local CLIENT_ID="" CLIENT_SECRET=""

  if op item get "$OP_ITEM_CLIENT" &>/dev/null 2>&1; then
    CLIENT_ID=$(op item get "$OP_ITEM_CLIENT" --fields username --reveal 2>/dev/null || echo "")
    CLIENT_SECRET=$(op item get "$OP_ITEM_CLIENT" --fields password --reveal 2>/dev/null || echo "")
  fi

  if [[ -n "$CLIENT_ID" && -n "$CLIENT_SECRET" ]]; then
    info "Reusing OAuth client from 1Password."
  else
    echo ""
    echo "Create a Desktop OAuth client in the GCP Console:"
    echo ""
    echo "  1. Create or select a GCP project:"
    echo "     https://console.cloud.google.com/projectcreate"
    echo ""
    echo "  2. Enable the Gmail API:"
    echo "     https://console.cloud.google.com/apis/library/gmail.googleapis.com"
    echo ""
    echo "  3. Configure OAuth branding:"
    echo "     https://console.cloud.google.com/auth/branding"
    echo "     → User type: External"
    echo ""
    echo "  4. Add your email as a test user:"
    echo "     https://console.cloud.google.com/auth/audience"
    echo ""
    echo "  5. Create OAuth credentials:"
    echo "     https://console.cloud.google.com/apis/credentials"
    echo "     → Create credentials → OAuth client ID → Desktop app"
    echo "     → Name: GML Gmail Agent"
    echo "     → Copy Client ID and Client Secret from the confirmation screen"
    echo ""

    read -rp "  Client ID: " CLIENT_ID
    [[ -n "$CLIENT_ID" ]] || fail "Client ID is required."
    read -rp "  Client Secret: " CLIENT_SECRET
    [[ -n "$CLIENT_SECRET" ]] || fail "Client Secret is required."

    if op item get "$OP_ITEM_CLIENT" &>/dev/null 2>&1; then
      op item edit "$OP_ITEM_CLIENT" \
        "username=$CLIENT_ID" \
        "password=$CLIENT_SECRET" >/dev/null
    else
      op item create --category Login \
        --title "$OP_ITEM_CLIENT" \
        "username=$CLIENT_ID" \
        "password=$CLIENT_SECRET" >/dev/null
    fi
    info "Client credentials saved to 1Password."
  fi

  info "OAuth client: $CLIENT_ID"

  GWS_CONFIG=$(mktemp -d /tmp/gml-gws-config-XXXXXX)

  heading "Gmail login — read-only scope (browser will open)"
  echo ""
  warn "When the browser opens: select your Gmail account."
  warn "If prompted about unverified app, click 'Continue'."
  echo ""
  read -rp "Press Enter to open the browser..."
  echo ""

  docker run --rm -it \
    --network host \
    --user "$(id -u):$(id -g)" \
    --entrypoint gws \
    -e GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file \
    -e GOOGLE_WORKSPACE_CLI_CONFIG_DIR=/gws-config \
    -e "GOOGLE_WORKSPACE_CLI_CLIENT_ID=$CLIENT_ID" \
    -e "GOOGLE_WORKSPACE_CLI_CLIENT_SECRET=$CLIENT_SECRET" \
    -v "$GWS_CONFIG":/gws-config \
    gml-gmail-agent-gml auth login -s gmail --readonly

  echo ""
  info "Login complete."

  heading "Storing read-only tokens in 1Password"

  local TOKEN_JSON
  TOKEN_JSON=$(docker run --rm -i \
    --user "$(id -u):$(id -g)" \
    --entrypoint gws \
    -e GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file \
    -e GOOGLE_WORKSPACE_CLI_CONFIG_DIR=/gws-config \
    -e "GOOGLE_WORKSPACE_CLI_CLIENT_ID=$CLIENT_ID" \
    -e "GOOGLE_WORKSPACE_CLI_CLIENT_SECRET=$CLIENT_SECRET" \
    -v "$GWS_CONFIG":/gws-config \
    gml-gmail-agent-gml auth export --unmasked)

  [[ -n "$TOKEN_JSON" ]] || fail "Exported credentials are empty — did login succeed?"
  TOKEN_JSON=$(printf '%s' "$TOKEN_JSON" | python3 -c "import sys,json; json.dump(json.load(sys.stdin),sys.stdout)")

  if op item get "$OP_ITEM_TOKEN" &>/dev/null 2>&1; then
    op item edit "$OP_ITEM_TOKEN" "${OP_FIELD_TOKEN}[password]=${TOKEN_JSON}" >/dev/null
  else
    op item create --category Password --title "$OP_ITEM_TOKEN" \
      "${OP_FIELD_TOKEN}[password]=${TOKEN_JSON}" >/dev/null
  fi

  info "Tokens stored in 1Password: '$OP_ITEM_TOKEN'"
}

setup_readonly_creds

# ── Step 3: Rules credentials (gmail.modify) ─────────────────────────────────

setup_rules_creds() {
  heading "Checking existing rules credentials"

  if op item get "$OP_ITEM_TOKEN_RULES" &>/dev/null 2>&1; then
    warn "1Password item '$OP_ITEM_TOKEN_RULES' already exists."
    read -rp "  Re-run auth and overwrite? [y/N] " OVERWRITE
    if [[ ! "$OVERWRITE" =~ ^[Yy]$ ]]; then
      info "Keeping existing rules credentials."
      return 0
    fi
  fi

  # Get client credentials (must exist from main setup)
  local CLIENT_ID CLIENT_SECRET
  CLIENT_ID=$(op item get "$OP_ITEM_CLIENT" --fields username --reveal 2>/dev/null || echo "")
  CLIENT_SECRET=$(op item get "$OP_ITEM_CLIENT" --fields password --reveal 2>/dev/null || echo "")

  if [[ -z "$CLIENT_ID" || -z "$CLIENT_SECRET" ]]; then
    fail "OAuth client not found in 1Password ('$OP_ITEM_CLIENT')."
  fi

  GWS_CONFIG=$(mktemp -d /tmp/gml-gws-config-XXXXXX)

  echo ""
  warn "When the browser opens: select the SAME Gmail account as before."
  warn "This login requests modify scope (read + archive). No delete access."
  echo ""
  read -rp "Press Enter to open the browser..."
  echo ""

  docker run --rm -it \
    --network host \
    --user "$(id -u):$(id -g)" \
    --entrypoint gws \
    -e GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file \
    -e GOOGLE_WORKSPACE_CLI_CONFIG_DIR=/gws-config \
    -e "GOOGLE_WORKSPACE_CLI_CLIENT_ID=$CLIENT_ID" \
    -e "GOOGLE_WORKSPACE_CLI_CLIENT_SECRET=$CLIENT_SECRET" \
    -v "$GWS_CONFIG":/gws-config \
    gml-gmail-agent-gml auth login --scopes https://www.googleapis.com/auth/gmail.modify

  info "Modify-scope login complete."

  heading "Storing rules tokens in 1Password"

  local TOKEN_JSON
  TOKEN_JSON=$(docker run --rm -i \
    --user "$(id -u):$(id -g)" \
    --entrypoint gws \
    -e GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file \
    -e GOOGLE_WORKSPACE_CLI_CONFIG_DIR=/gws-config \
    -e "GOOGLE_WORKSPACE_CLI_CLIENT_ID=$CLIENT_ID" \
    -e "GOOGLE_WORKSPACE_CLI_CLIENT_SECRET=$CLIENT_SECRET" \
    -v "$GWS_CONFIG":/gws-config \
    gml-gmail-agent-gml auth export --unmasked)

  [[ -n "$TOKEN_JSON" ]] || fail "Exported rules credentials are empty — did login succeed?"
  TOKEN_JSON=$(printf '%s' "$TOKEN_JSON" | python3 -c "import sys,json; json.dump(json.load(sys.stdin),sys.stdout)")

  if op item get "$OP_ITEM_TOKEN_RULES" &>/dev/null 2>&1; then
    op item edit "$OP_ITEM_TOKEN_RULES" "${OP_FIELD_TOKEN}[password]=${TOKEN_JSON}" >/dev/null
  else
    op item create --category Password --title "$OP_ITEM_TOKEN_RULES" \
      "${OP_FIELD_TOKEN}[password]=${TOKEN_JSON}" >/dev/null
  fi

  info "Rules tokens stored in 1Password: '$OP_ITEM_TOKEN_RULES'"
}

setup_rules_creds

echo ""
info "Setup complete. You can now use:"
echo ""
echo "  ./run-task.sh profile"
echo "  ./run-task.sh stats"
echo "  ./run-task.sh run --dry-run"
echo "  ./run-task.sh analyze"
