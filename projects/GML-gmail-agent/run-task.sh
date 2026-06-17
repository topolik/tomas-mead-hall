#!/usr/bin/env bash
# run-task.sh — GML Gmail Agent: run a single task
# Cycle: Analyze → Knowledge → Rules → (repeat, analysis excludes rule-matched emails)
#
# For daemon management use ./watch.sh instead.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OP_ITEM_TOKEN="GML Gmail Read-Only Credentials"
OP_ITEM_TOKEN_RULES="GML Gmail Read-Write Credentials"
OP_FIELD_TOKEN="credential"

# --- LLP proxy integration (non-destructive) ---------------------------------
# When LLP_URL is set, route an LLM completion through the LLP proxy
# (OpenAI-compatible /v1/chat/completions) instead of invoking the gemini/claude
# CLI directly. The prompt file becomes a single user message; the assistant
# content is written to OUT_FILE. With LLP_URL unset, GML uses the CLIs as before.
#   LLP_URL     e.g. http://localhost:4000
#   LLP_SOCKET  control socket for the token handshake (default ~/.llp/control.sock)
#   LLP_MODEL   logical model to request (default: gml-analyze; proxy fails over gemini->claude)
# A session token is auto-provisioned over the control socket and kept only in
# this shell (never an env var / file). run-task.sh runs on the host, which owns
# ~/.llp, so it can reach the socket directly.
llp_complete() {
  local prompt_file="$1" out_file="$2"
  local llp_model="${LLP_MODEL:-gml-analyze}"
  local socket="${LLP_SOCKET:-$HOME/.llp/control.sock}"
  local token
  token="$(curl -sS --unix-socket "$socket" -X POST http://unix/register \
    -H 'Content-Type: application/json' -d '{"agent":"gml"}' | jq -r '.token // empty')"
  [ -n "$token" ] || { echo "  [LLP] handshake failed via $socket — is llp running?" >&2; return 1; }
  jq -Rs --arg model "$llp_model" '{model:$model, messages:[{role:"user", content:.}]}' "$prompt_file" \
    | curl -sS --max-time 200 "${LLP_URL%/}/v1/chat/completions" \
        -H "Authorization: Bearer $token" \
        -H "Content-Type: application/json" --data @- \
    | jq -r '.choices[0].message.content // empty' > "$out_file"
}

# llm_call model prompt_file out_file — route through LLP when available, else CLI
llm_call() {
  local model="$1" prompt_file="$2" out_file="$3"
  if [[ -n "${LLP_URL:-}" ]]; then
    echo "  [LLP] routing via proxy at ${LLP_URL} (model=${LLP_MODEL:-gml-analyze})" >&2
    llp_complete "$prompt_file" "$out_file"
  else
    case "$model" in
      claude)
        claude -p --model claude-opus-4-6 --output-format text < "$prompt_file" > "$out_file"
        ;;
      gemini)
        local gerr grc=0; gerr="$(mktemp)"
        GOOGLE_CLOUD_PROJECT="${GOOGLE_CLOUD_PROJECT:?Set GOOGLE_CLOUD_PROJECT or run ./setup.sh}" \
          timeout 180 npx @google/gemini-cli -e none --approval-mode plan -p "" \
          < "$prompt_file" > "$out_file" 2>"$gerr" || grc=$?
        echo "  [gemini] exit=$grc prompt=$(wc -c < "$prompt_file")B response=$(wc -c < "$out_file")B" >&2
        if [[ $grc -ne 0 || ! -s "$out_file" ]]; then
          [[ $grc -eq 124 ]] && echo "  [gemini] TIMED OUT after 180s" >&2
          echo "  [gemini stderr]:" >&2; sed 's/^/    /' "$gerr" >&2
        fi
        rm -f "$gerr"
        ;;
      *)
        echo "error: unknown model '$model' (use claude or gemini)" >&2
        return 1
        ;;
    esac
  fi
}

# Ensure bind-mounted files exist (Docker creates directories for missing mounts)
[[ -f "${SCRIPT_DIR}/data/rules.yaml" ]] || { echo "error: rules.yaml not found — see README.md for setup" >&2; exit 1; }

if [[ $# -eq 0 ]]; then
  echo "Usage: ./run-task.sh <command> [args...]" >&2
  echo "" >&2
  echo "Cycle: Analyze → Knowledge → Rules → (repeat)" >&2
  echo "" >&2
  echo "Analyze:" >&2
  echo "  analyze [--days N|--hours N|--minutes N] [--model gemini|claude]" >&2
  echo "  watch-analysis [--model gemini|claude]" >&2
  echo "" >&2
  echo "Knowledge:" >&2
  echo "  learn [--days N] [--model gemini|claude]" >&2
  echo "  distill [--model gemini|claude]" >&2
  echo "  propose [--json]" >&2
  echo "  apply-rules" >&2
  echo "  watch-knowledge [--model gemini|claude]" >&2
  echo "" >&2
  echo "Rules:" >&2
  echo "  run [--dry-run] [--json] [--since H] [--pages N]" >&2
  echo "  watch-rules" >&2
  echo "" >&2
  echo "Utility:" >&2
  echo "  profile              Verify auth — shows Gmail account" >&2
  echo "  stats [--json]       Inbox statistics" >&2
  exit 1
fi

pipe_creds() {
  op item get "$OP_ITEM_TOKEN" --fields "$OP_FIELD_TOKEN" --reveal --format json \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['value'], end='')"
}

read_config() {
  local key="$1" default="$2"
  python3 -c "
import yaml, sys
try:
    c = yaml.safe_load(open('${SCRIPT_DIR}/data/rules.yaml'))
    keys = '${key}'.split('.')
    v = c
    for k in keys:
        v = v.get(k, {}) if isinstance(v, dict) else {}
    print(v if v and v != {} else '${default}')
except Exception:
    print('${default}')
"
}

pipe_rules_creds() {
  if ! op item get "$OP_ITEM_TOKEN_RULES" &>/dev/null 2>&1; then
    echo "error: modify-scoped credentials not found in 1Password ('$OP_ITEM_TOKEN_RULES')." >&2
    echo "Run: ./setup.sh --rules" >&2
    exit 1
  fi
  op item get "$OP_ITEM_TOKEN_RULES" --fields "$OP_FIELD_TOKEN" --reveal --format json \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['value'], end='')"
}

send_creds() {
  if [[ -n "${GML_CACHED_CREDS:-}" ]]; then
    echo -n "$GML_CACHED_CREDS"
  else
    pipe_creds
  fi
}

send_rules_creds() {
  if [[ -n "${GML_CACHED_RULES_CREDS:-}" ]]; then
    echo -n "$GML_CACHED_RULES_CREDS"
  else
    pipe_rules_creds
  fi
}

GML_PROMPT_FILE=""
GML_ANALYSIS_FILE=""
trap 'rm -f "$GML_PROMPT_FILE" "$GML_ANALYSIS_FILE"' EXIT

run_analyze() {
  local model="$1"
  shift
  local pass_args=("$@")

  echo "=== GML Analyze (model: $model) ===" >&2

  GML_PROMPT_FILE="$(mktemp)"
  GML_ANALYSIS_FILE="$(mktemp)"
  local prompt_file="$GML_PROMPT_FILE"
  local analysis_file="$GML_ANALYSIS_FILE"

  echo "[1/4] Fetching and sanitizing emails..." >&2
  local knowledge_vol=""
  if [[ -f "${SCRIPT_DIR}/data/knowledge.yaml" ]]; then
    knowledge_vol="-v ${SCRIPT_DIR}/data/knowledge.yaml:/app/data/knowledge.yaml:ro"
  fi
  send_creds | docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T $knowledge_vol gml fetch "${pass_args[@]}" > "$prompt_file"
  local fetch_status=${PIPESTATUS[1]}

  if [[ $fetch_status -eq 2 ]]; then
    echo "  no emails to analyze — skipping" >&2
    return 0
  elif [[ $fetch_status -ne 0 ]]; then
    echo "error: fetch failed" >&2
    return 1
  fi

  if [[ ! -s "$prompt_file" ]]; then
    echo "error: fetch produced empty prompt" >&2
    return 1
  fi

  echo "[2/4] Analyzing with $model..." >&2
  llm_call "$model" "$prompt_file" "$analysis_file"

  if [[ ! -s "$analysis_file" ]]; then
    echo "error: $model returned empty response" >&2
    return 1
  fi

  echo "[3/4] Dedup review with $model..." >&2
  local dedup_output dedup_file
  dedup_output="$(docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T gml dedup < "$analysis_file")"

  # gml dedup outputs raw JSON (starts with [) when no dedup needed, or a natural-language prompt when LLM review is needed
  if [[ "$dedup_output" == "["* ]]; then
    echo "  no dismissed notifications — skipping LLM dedup" >&2
  else
    dedup_file="$(mktemp)"
    printf '%s' "$dedup_output" > "$dedup_file"
    llm_call "$model" "$dedup_file" "$analysis_file"
    rm -f "$dedup_file"
    if [[ ! -s "$analysis_file" ]]; then
      echo "  dedup returned empty — nothing to post" >&2
      echo "=== Analysis complete ===" >&2
      return 0
    fi
    echo "  dedup filtering applied" >&2
  fi

  echo "[4/4] Validating and posting to DSH..." >&2
  docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T gml notify < "$analysis_file"

  echo "=== Analysis complete ===" >&2
}

run_learn() {
  local model="$1"
  shift
  local pass_args=("$@")

  echo "=== GML Knowledge: Learn (model: $model) ===" >&2

  GML_PROMPT_FILE="$(mktemp)"
  GML_ANALYSIS_FILE="$(mktemp)"
  local prompt_file="$GML_PROMPT_FILE"
  local analysis_file="$GML_ANALYSIS_FILE"

  echo "[1/3] Collecting behavioral data..." >&2
  local knowledge_vol=""
  if [[ -f "${SCRIPT_DIR}/data/knowledge.yaml" ]]; then
    knowledge_vol="-v ${SCRIPT_DIR}/data/knowledge.yaml:/app/data/knowledge.yaml:ro"
  fi
  send_creds | docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T $knowledge_vol gml history "${pass_args[@]}" > "$prompt_file"
  local hist_status=${PIPESTATUS[1]}

  if [[ $hist_status -eq 2 ]]; then
    echo "  no behavioral data to analyze — skipping" >&2
    return 0
  elif [[ $hist_status -ne 0 ]]; then
    echo "error: history failed" >&2
    return 1
  fi

  if [[ ! -s "$prompt_file" ]]; then
    echo "error: history produced empty prompt" >&2
    return 1
  fi

  echo "[2/4] Analyzing patterns with $model..." >&2
  llm_call "$model" "$prompt_file" "$analysis_file"

  if [[ ! -s "$analysis_file" ]]; then
    echo "error: $model returned empty response" >&2
    return 1
  fi

  # Insight-dedup review: drop reworded duplicates of dismissed insights, keep
  # genuine refinements (prefixed "Update: "). Mirrors the analyze dedup stage,
  # but with the learn-tuned prompt that re-surfaces real changes.
  echo "[3/4] Insight-dedup review with $model..." >&2
  local dedup_output dedup_file
  dedup_output="$(docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T gml insight-dedup < "$analysis_file")"
  if [[ "$dedup_output" == "["* ]]; then
    echo "  no dismissed insights — skipping LLM dedup" >&2
  else
    dedup_file="$(mktemp)"
    printf '%s' "$dedup_output" > "$dedup_file"
    llm_call "$model" "$dedup_file" "$analysis_file"
    rm -f "$dedup_file"
    if [[ ! -s "$analysis_file" ]]; then
      echo "  insight-dedup returned empty — nothing to post" >&2
      echo "=== Learning complete ===" >&2
      return 0
    fi
    echo "  insight-dedup filtering applied" >&2
  fi

  echo "[4/4] Validating and posting insights to DSH..." >&2
  docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T gml insights < "$analysis_file"

  echo "=== Learning complete ===" >&2
}

run_distill() {
  local model="$1"

  echo "=== GML Knowledge: Distill (model: $model) ===" >&2

  GML_PROMPT_FILE="$(mktemp)"
  GML_ANALYSIS_FILE="$(mktemp)"
  local prompt_file="$GML_PROMPT_FILE"
  local analysis_file="$GML_ANALYSIS_FILE"

  echo "[1/3] Gathering dismissed insights from DSH..." >&2
  local knowledge_vol=""
  if [[ -f "${SCRIPT_DIR}/data/knowledge.yaml" ]]; then
    knowledge_vol="-v ${SCRIPT_DIR}/data/knowledge.yaml:/app/data/knowledge.yaml:ro"
  fi
  docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T $knowledge_vol gml distill-gather > "$prompt_file"

  if [[ ! -s "$prompt_file" ]]; then
    echo "no dismissed insights with comments — nothing to distill" >&2
    return 0
  fi

  echo "[2/3] Distilling with $model..." >&2
  llm_call "$model" "$prompt_file" "$analysis_file"

  if [[ ! -s "$analysis_file" ]]; then
    echo "error: $model returned empty response" >&2
    return 1
  fi

  echo "[3/3] Applying distilled knowledge..." >&2
  touch "${SCRIPT_DIR}/data/knowledge.yaml"
  chmod 666 "${SCRIPT_DIR}/data/knowledge.yaml"
  docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T \
    -v "${SCRIPT_DIR}/data/knowledge.yaml:/app/data/knowledge.yaml" \
    gml distill-apply < "$analysis_file"

  echo "=== Distillation complete ===" >&2
}

# run_propose: LLM-gated propose (gather → semantic dedup LLM → apply).
# Mirrors the apply-rules merge flow. Returns 0 even when there's no work.
run_propose() {
  local model="$1"

  echo "=== GML Knowledge: Propose (model: $model) ===" >&2

  GML_PROMPT_FILE="$(mktemp)"
  GML_ANALYSIS_FILE="$(mktemp)"
  local prompt_file="$GML_PROMPT_FILE"
  local analysis_file="$GML_ANALYSIS_FILE"

  local knowledge_vol=""
  if [[ -f "${SCRIPT_DIR}/data/knowledge.yaml" ]]; then
    knowledge_vol="-v ${SCRIPT_DIR}/data/knowledge.yaml:/app/data/knowledge.yaml:ro"
  fi

  echo "[1/3] Generating proposals + structural dedup..." >&2
  docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T $knowledge_vol gml propose-gather > "$prompt_file"

  if [[ ! -s "$prompt_file" ]]; then
    echo "  no candidates for the semantic gate — nothing to propose" >&2
    return 0
  fi

  echo "[2/3] Semantic dedup with $model..." >&2
  llm_call "$model" "$prompt_file" "$analysis_file"

  if [[ ! -s "$analysis_file" ]]; then
    echo "error: $model returned empty response" >&2
    return 1
  fi

  echo "[3/3] Posting surviving plans to DSH..." >&2
  docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T gml propose-apply < "$analysis_file"

  echo "=== Propose complete ===" >&2
}

# run_apply_rules: DETERMINISTIC (no-LLM) merge — approved DSH plans → rules.yaml.
# No conflict-detection LLM: propose folds one-rule-per-sender upstream, and the
# in-binary same-sender guard withholds any residual OR-union footgun. Returns 0
# even when there's nothing to apply. Used by the apply-rules command (--no-llm)
# and as step 4/4 of the knowledge cycle.
run_apply_rules() {
  echo "=== GML Apply-Rules (deterministic, no LLM) ===" >&2

  local out_file
  out_file="$(mktemp)"

  docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T gml apply-rules > "$out_file"

  if [[ -s "$out_file" ]]; then
    cp "$out_file" "${SCRIPT_DIR}/data/rules.yaml"
    chmod 644 "${SCRIPT_DIR}/data/rules.yaml"
    echo "[apply-rules] rules.yaml updated" >&2
  else
    echo "[apply-rules] no approved rules to apply — rules.yaml unchanged" >&2
  fi
  rm -f "$out_file"

  echo "=== Apply-Rules complete ===" >&2
}

# --- propose (data/knowledge.yaml → DSH plans) ---
#   default: LLM-gated (structural dedup + semantic dedup gate)
#   --no-llm / --json: structural-only direct path (no semantic gate)
if [[ "$1" == "propose" ]]; then
  shift
  MODEL="gemini"
  NO_LLM=false
  pass_args=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --model) MODEL="$2"; shift 2 ;;
      --no-llm) NO_LLM=true; shift ;;
      --json) NO_LLM=true; pass_args+=("--json"); shift ;;
      *) pass_args+=("$1"); shift ;;
    esac
  done

  if [[ "$NO_LLM" == "true" ]]; then
    knowledge_vol=""
    if [[ -f "${SCRIPT_DIR}/data/knowledge.yaml" ]]; then
      knowledge_vol="-v ${SCRIPT_DIR}/data/knowledge.yaml:/app/data/knowledge.yaml:ro"
    fi
    docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T $knowledge_vol gml propose "${pass_args[@]}"
    exit 0
  fi

  run_propose "$MODEL"
  exit 0
fi

# --- apply-rules (LLM-based merge: gather → LLM → apply) ---
if [[ "$1" == "apply-rules" ]]; then
  shift
  MODEL="gemini"
  NO_LLM=false
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --model) MODEL="$2"; shift 2 ;;
      --no-llm) NO_LLM=true; shift ;;
      *)       shift ;;
    esac
  done

  if [[ "$NO_LLM" == "true" ]]; then
    run_apply_rules
    exit 0
  fi

  echo "=== GML Apply-Rules (LLM merge, model: $MODEL) ===" >&2

  GML_PROMPT_FILE="$(mktemp)"
  GML_ANALYSIS_FILE="$(mktemp)"
  trap 'rm -f "$GML_PROMPT_FILE" "$GML_ANALYSIS_FILE"' EXIT

  knowledge_vol=""
  if [[ -f "${SCRIPT_DIR}/data/knowledge.yaml" ]]; then
    knowledge_vol="-v ${SCRIPT_DIR}/data/knowledge.yaml:/app/data/knowledge.yaml:ro"
  fi

  echo "[1/3] Gathering approved plans..." >&2
  docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T $knowledge_vol gml merge-plans-gather > "$GML_PROMPT_FILE"

  if [[ ! -s "$GML_PROMPT_FILE" ]]; then
    echo "no approved plans — nothing to apply" >&2
    exit 0
  fi

  echo "[2/3] Merging with $MODEL (conflict detection)..." >&2
  llm_call "$MODEL" "$GML_PROMPT_FILE" "$GML_ANALYSIS_FILE"

  if [[ ! -s "$GML_ANALYSIS_FILE" ]]; then
    echo "error: $MODEL returned empty response" >&2
    exit 1
  fi

  echo "[3/3] Validating merge and applying rules..." >&2
  RULES_OUTPUT="$(mktemp)"
  docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T gml merge-plans-apply < "$GML_ANALYSIS_FILE" > "$RULES_OUTPUT"

  if [[ -s "$RULES_OUTPUT" ]]; then
    cp "$RULES_OUTPUT" "${SCRIPT_DIR}/data/rules.yaml"
    chmod 644 "${SCRIPT_DIR}/data/rules.yaml"
    echo "[apply-rules] rules.yaml updated" >&2
  fi
  rm -f "$RULES_OUTPUT"

  echo "=== Apply-Rules complete ===" >&2
  exit 0
fi

# --- distill (knowledge distillation) ---
if [[ "$1" == "distill" ]]; then
  shift
  MODEL="gemini"
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --model) MODEL="$2"; shift 2 ;;
      *)       shift ;;
    esac
  done
  run_distill "$MODEL"
  exit 0
fi

# --- learn (behavioral learning) ---
if [[ "$1" == "learn" ]]; then
  shift
  MODEL="gemini"
  PASS_ARGS=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --model) MODEL="$2"; shift 2 ;;
      *)       PASS_ARGS+=("$1"); shift ;;
    esac
  done
  run_learn "$MODEL" "${PASS_ARGS[@]}"
  exit 0
fi

# --- analyze (one-shot) ---
if [[ "$1" == "analyze" ]]; then
  shift
  MODEL="gemini"
  PASS_ARGS=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --model) MODEL="$2"; shift 2 ;;
      *)       PASS_ARGS+=("$1"); shift ;;
    esac
  done
  run_analyze "$MODEL" "${PASS_ARGS[@]}"
  exit 0
fi

# --- watch-analysis (scheduled analysis daemon) ---
if [[ "$1" == "watch-analysis" ]]; then
  shift
  MODEL="gemini"
  INTERVAL_OVERRIDE=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --model) MODEL="$2"; shift 2 ;;
      --interval) INTERVAL_OVERRIDE="$2"; shift 2 ;;
      *)       shift ;;
    esac
  done

  INTERVAL="${INTERVAL_OVERRIDE:-$(read_config "analysis.schedule_minutes" "5")}"
  STATE_FILE="${SCRIPT_DIR}/data/.gml-last-analysis"

  echo "[watch-analysis] fetching credentials from 1Password (one-time)..." >&2
  GML_CACHED_CREDS=$(pipe_creds)
  echo "[watch-analysis] credentials cached in memory for this session" >&2

  echo "=== GML Watch-Analysis: analyzing every ${INTERVAL} minutes (model: $MODEL) ===" >&2
  echo "[watch-analysis] Ctrl+C to stop, or use ./watch.sh to manage daemons" >&2

  # Migrate old state file if present
  if [[ -f "${SCRIPT_DIR}/data/.gml-last-run" && ! -f "$STATE_FILE" ]]; then
    mv "${SCRIPT_DIR}/data/.gml-last-run" "$STATE_FILE"
    echo "[watch-analysis] migrated data/.gml-last-run → data/.gml-last-analysis" >&2
  fi

  analysis_window() {
    local window="$INTERVAL"
    local max_days
    max_days=$(read_config "analysis.max_days" "14")
    local max_window=$(( max_days * 24 * 60 ))
    if [[ -f "$STATE_FILE" && -s "$STATE_FILE" ]]; then
      local last_ts now_ts elapsed
      last_ts=$(cat "$STATE_FILE")
      if [[ "$last_ts" =~ ^[0-9]+$ && "$last_ts" -gt 0 ]]; then
        now_ts=$(date +%s)
        elapsed=$(( (now_ts - last_ts) / 60 ))
        if [[ "$elapsed" -gt "$window" ]]; then
          window="$elapsed"
          echo "[watch-analysis] catching up: ${elapsed} minutes since last success (interval: ${INTERVAL}m)" >&2
        fi
      else
        echo "[watch-analysis] invalid state file — treating as first run" >&2
        local days
        days=$(read_config "analysis.days" "3")
        window=$(( days * 24 * 60 ))
      fi
    else
      local days
      days=$(read_config "analysis.days" "3")
      window=$(( days * 24 * 60 ))
      echo "[watch-analysis] first run — using ${days}-day initial window" >&2
    fi
    if [[ "$window" -gt "$max_window" ]]; then
      echo "[watch-analysis] clamping window to ${max_days}-day max (${max_window} minutes)" >&2
      window="$max_window"
    fi
    echo "$window"
  }

  while true; do
    echo "" >&2
    echo "[watch-analysis] $(date '+%Y-%m-%d %H:%M:%S') starting analysis..." >&2
    WINDOW=$(analysis_window)
    if run_analyze "$MODEL" --minutes "$WINDOW"; then
      date +%s > "$STATE_FILE"
      echo "[watch-analysis] $(date '+%Y-%m-%d %H:%M:%S') success — next run in ${INTERVAL} minutes" >&2
    else
      echo "[watch-analysis] $(date '+%Y-%m-%d %H:%M:%S') failed — will retry in ${INTERVAL} minutes (window will expand to catch up)" >&2
    fi
    sleep "${INTERVAL}m"
  done
fi

# --- watch-knowledge (scheduled learn+distill+propose daemon) ---
if [[ "$1" == "watch-knowledge" ]]; then
  shift
  MODEL="gemini"
  INTERVAL_OVERRIDE=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --model) MODEL="$2"; shift 2 ;;
      --interval) INTERVAL_OVERRIDE="$2"; shift 2 ;;
      *)       shift ;;
    esac
  done

  INTERVAL="${INTERVAL_OVERRIDE:-$(read_config "analysis.learn.knowledge_interval_minutes" "5")}"
  STATE_FILE="${SCRIPT_DIR}/data/.gml-last-knowledge"

  echo "[watch-knowledge] fetching credentials from 1Password (one-time)..." >&2
  GML_CACHED_CREDS=$(pipe_creds)
  export GML_CACHED_CREDS
  echo "[watch-knowledge] credentials cached in memory for this session" >&2

  echo "=== GML Watch-Knowledge: learn+distill+propose+apply every ${INTERVAL} minutes (model: $MODEL) ===" >&2
  echo "[watch-knowledge] Ctrl+C to stop, or use ./watch.sh to manage daemons" >&2

  while true; do
    echo "" >&2
    echo "[watch-knowledge] $(date '+%Y-%m-%d %H:%M:%S') starting knowledge pipeline..." >&2

    echo "[watch-knowledge] step 1/4: learn..." >&2
    if run_learn "$MODEL"; then
      echo "[watch-knowledge] learn succeeded" >&2
    else
      echo "[watch-knowledge] learn failed — continuing with distill" >&2
    fi

    echo "[watch-knowledge] step 2/4: distill..." >&2
    if run_distill "$MODEL"; then
      echo "[watch-knowledge] distill succeeded" >&2
    else
      echo "[watch-knowledge] distill failed — continuing with propose" >&2
    fi

    echo "[watch-knowledge] step 3/4: propose (LLM-gated, folds per sender)..." >&2
    if run_propose "$MODEL"; then
      echo "[watch-knowledge] propose succeeded" >&2
    else
      echo "[watch-knowledge] propose failed — continuing with apply" >&2
    fi

    echo "[watch-knowledge] step 4/4: apply-rules (deterministic: approved plans → rules.yaml)..." >&2
    if run_apply_rules; then
      echo "[watch-knowledge] apply-rules succeeded" >&2
    else
      echo "[watch-knowledge] apply-rules failed" >&2
    fi

    date +%s > "$STATE_FILE"
    echo "[watch-knowledge] $(date '+%Y-%m-%d %H:%M:%S') pipeline complete — next run in ${INTERVAL} minutes" >&2
    sleep "${INTERVAL}m"
  done
fi

# --- run (apply archive rules — uses modify-scoped credentials) ---
if [[ "$1" == "run" ]]; then
  shift
  send_rules_creds | docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T gml run "$@"
  exit 0
fi

# --- watch-rules (scheduled rules daemon — uses modify-scoped credentials) ---
if [[ "$1" == "watch-rules" ]]; then
  shift
  send_rules_creds | docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T gml watch-rules "$@"
  exit 0
fi

# All other commands: pipe read-only credentials to container
knowledge_vol=""
if [[ -f "${SCRIPT_DIR}/data/knowledge.yaml" ]]; then
  knowledge_vol="-v ${SCRIPT_DIR}/data/knowledge.yaml:/app/data/knowledge.yaml:ro"
fi
send_creds | docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T $knowledge_vol gml "$@"
