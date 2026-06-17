#!/usr/bin/env bash
# setup-ollama.sh — start the Ollama container and pull the default model.
# GPU-accelerated via NVIDIA runtime (GTX 1080 / 8GB VRAM).
set -euo pipefail
cd "$(dirname "$0")"

MODEL="${1:-qwen2.5:7b}"

echo "Starting Ollama container…"
docker compose up -d ollama

echo "Waiting for Ollama to be ready…"
for i in $(seq 1 30); do
  if curl -sf http://127.0.0.1:11434/api/tags >/dev/null 2>&1; then
    break
  fi
  [ "$i" -eq 30 ] && { echo "Ollama did not start in 30s" >&2; exit 1; }
  sleep 1
done
echo "Ollama is up."

echo "Pulling model $MODEL (this may take a few minutes on first run)…"
docker compose exec ollama ollama pull "$MODEL"
echo "Done. Model $MODEL is ready."
echo
echo "Verify: curl -s http://127.0.0.1:11434/api/tags | jq '.models[].name'"
