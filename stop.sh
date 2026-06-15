#!/usr/bin/env bash
# stop.sh — stop all projects (reverse of run.sh)
set -uo pipefail

REPO="$(cd "$(dirname "$0")" && pwd)"

step() { echo "🛑 $* ────────────────────────────────────────────"; }

# ── 1/4  MND — mind model ──────────────────────────────────────────────────
step "Stopping 🧠 MND"
"${REPO}/projects/MND-mind-model/watch.sh" stop 2>/dev/null || echo "  mnd: not running"

# ── 2/4  GML — gmail agent ─────────────────────────────────────────────────
step "Stopping 📧 GML"
"${REPO}/projects/GML-gmail-agent/watch.sh" stop 2>/dev/null || echo "  gml: not running"

# ── 3/4  LLP — llm proxy ──────────────────────────────────────────────────
step "Stopping 🔀 LLP"
"${REPO}/projects/LLP-llm-proxy/watch.sh" stop 2>/dev/null || echo "  llp: not running"

# ── 4/4  DSH — dashboard (docker compose) ─────────────────────────────────
step "Stopping 📦 DSH"
(cd "${REPO}/projects/DSH-dashboard" && docker compose down)

echo ""
echo "✅ All services stopped."
