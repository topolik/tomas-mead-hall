#!/usr/bin/env bash
# setup.sh — first-run setup for LLP (LLM Proxy).
#
# Provisions config.yaml, detects/disables unavailable providers,
# builds the binary via Docker, and optionally starts Ollama.
#
# Env vars (set by the global setup.sh, or detected here):
#   GCP_PROJECT   — Google Cloud project ID for gemini-cli
#
# Safe to re-run — each step is idempotent.
set -euo pipefail
cd "$(dirname "$0")"

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
ok()   { echo -e "  ${GREEN}✅${NC} $*"; }
warn() { echo -e "  ${YELLOW}⚠️${NC}  $*"; }
ask_yn() { local ans; read -rp "  $1 [y/N] " ans; [[ "$ans" =~ ^[Yy]$ ]]; }

# ── 1. config.yaml ────────────────────────────────────────────────────────

if [ -f config.yaml ]; then
  ok "config.yaml already exists"
else
  cp config.example.yaml config.yaml
  ok "config.yaml created from template"
fi

# ── 2. GCP project ID (gemini-cli needs it) ───────────────────────────────

GCP_PROJECT="${GCP_PROJECT:-}"

if [[ -z "$GCP_PROJECT" ]] && grep -q 'your-gcp-project-id' config.yaml 2>/dev/null; then
  if command -v npx >/dev/null 2>&1; then
    echo ""
    echo "  Gemini CLI needs a Google Cloud project ID."
    echo "  Create one at: https://console.cloud.google.com/projectcreate"
    read -rp "  GCP project ID (or Enter to skip): " GCP_PROJECT
  fi
fi

if [[ -n "$GCP_PROJECT" ]]; then
  sed -i "s/your-gcp-project-id/${GCP_PROJECT}/g" config.yaml
  ok "GCP project ID set in config.yaml"
fi

# ── 3. Disable unavailable providers ──────────────────────────────────────

if ! command -v npx >/dev/null 2>&1; then
  sed -i '/^  gemini:/,/^  [a-z]/{/^  gemini/s/^/# /; /^    /s/^/#   /}' config.yaml 2>/dev/null || true
  sed -i '/^  gemini2:/,/^  [a-z]/{/^  gemini2/s/^/# /; /^    /s/^/#   /}' config.yaml 2>/dev/null || true
  warn "Gemini impls disabled in config.yaml (no npx)"
fi

if ! command -v claude >/dev/null 2>&1; then
  sed -i '/^  claude:/,/^  [a-z]/{/^  claude:/s/^/# /; /^    /s/^/#   /}' config.yaml 2>/dev/null || true
  sed -i '/^  claude2:/,/^  [a-z]/{/^  claude2/s/^/# /; /^    /s/^/#   /}' config.yaml 2>/dev/null || true
  warn "Claude impls disabled in config.yaml (no claude CLI)"
fi

# ── 4. Build LLP binary ──────────────────────────────────────────────────

echo ""
echo "  Building LLP binary (via Docker — no Go needed on host)..."
if docker build -f Dockerfile.build -o . . 2>&1; then
  ok "LLP binary built"
else
  warn "LLP build failed — will retry on first ./run.sh"
fi

# ── 5. Ollama (optional — needs NVIDIA GPU) ──────────────────────────────

if command -v nvidia-smi >/dev/null 2>&1; then
  echo ""
  echo "  NVIDIA GPU detected. Ollama provides a free local LLM backend (failover backstop)."
  if ask_yn "Start Ollama now? (pulls ~5GB model on first run)"; then
    MODEL="${1:-dolphin3:8b}"

    echo "  Starting Ollama container…"
    docker compose up -d ollama

    echo "  Waiting for Ollama to be ready…"
    for i in $(seq 1 30); do
      if curl -sf http://127.0.0.1:11434/api/tags >/dev/null 2>&1; then
        break
      fi
      [ "$i" -eq 30 ] && { warn "Ollama did not start in 30s"; return 0 2>/dev/null || exit 0; }
      sleep 1
    done

    echo "  Pulling model $MODEL (this may take a few minutes on first run)…"
    docker compose exec ollama ollama pull "$MODEL"
    ok "Ollama ready ($MODEL)"
  else
    ok "Ollama skipped (re-run ./setup.sh or: docker compose up -d ollama)"
  fi
else
  ok "No NVIDIA GPU — Ollama skipped (LLP will use CLI providers only)"
fi
