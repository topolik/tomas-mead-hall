#!/usr/bin/env bash
# run-task.sh — MND Mind Model: run a single pipeline step
# Pipeline: extract → distill → profile → ask
#
# Go steps run in Docker (sessions mounted read-only); LLM calls run
# host-side via gemini-cli/claude with prompt files (MND-003, GML pattern).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
export MND_UID="$(id -u)" MND_GID="$(id -g)"

if [[ $# -eq 0 ]]; then
  echo "Usage: ./run-task.sh <command> [args...]" >&2
  echo "" >&2
  echo "Pipeline: extract → distill → profile → ask" >&2
  echo "" >&2
  echo "  extract                          Mine Claude+Gemini sessions into data/moments.jsonl" >&2
  echo "  distill [--limit N] [--batch-size N] [--model gemini|claude]" >&2
  echo "                                   Distill moments into data/insights.yaml" >&2
  echo "  profile [--model gemini|claude]  Generate data/profiles/*.md from insights" >&2
  echo "  ask [--json] [--model M] \"question\"" >&2
  echo "                                   Answer as Tomas would, with evidence citations" >&2
  echo "  pipeline [--limit N] [--model M] extract + distill + profile" >&2
  echo "  retrain [--limit N] [--model M]  Incremental: new moments + DSH feedback, regen profiles if changed" >&2
  echo "  watch-retrain [--model M]        Retrain daemon (MND_RETRAIN_INTERVAL, default 86400s)" >&2
  echo "  learn [--model M]                Ingest Tomas's DSH comments as corrective insights" >&2
  echo "  contradictions [--model M]       Retire stale insights newer corrections supersede" >&2
  echo "  eval [--model M]                 Fidelity: clone's blind answers vs Tomas's real decisions (MND_EVAL_N)" >&2
  echo "  feedback-post                    Escalate the last unanswerable ask to DSH" >&2
  echo "  stats                            Corpus statistics" >&2
  echo "" >&2
  echo "LLM routing: LLP gateway when up (highest models, quota-aware failover —" >&2
  echo "  MND_LLP=off to disable; MND_LLP_CHAIN, MND_GEMINI_MODEL, MND_CLAUDE_MODEL," >&2
  echo "  MND_LLP_URL, MND_LLP_SOCKET to tune), direct gemini/claude CLIs otherwise." >&2
  exit 1
fi

mnd() {
  docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T mnd "$@"
}

# --- LLP gateway (MND-020/021) ------------------------------------------------
# When the LLP proxy is up, LLM calls route through it: per-impl queueing,
# quota cooldown, failover, and usage accounting live in ONE place (LLP), and
# MND pins the highest models per side. LLP down or MND_LLP=off -> the direct
# CLI paths below, unchanged.

llp_up() {
  [[ "${MND_LLP:-on}" != "off" ]] || return 1
  [[ -S "${MND_LLP_SOCKET:-$HOME/.llp/control.sock}" ]] || return 1
  curl -s --max-time 3 "${MND_LLP_URL:-http://localhost:4000}/healthz" \
    | jq -e '.status == "ok"' >/dev/null 2>&1
}

# llp_complete <llp-model> <prompt_file> <response_file>
# Per-session token from the control-socket handshake, held in a shell var and
# passed to curl via a process-substituted config — never argv, env, or disk
# (service-secret rule).
llp_complete() {
  local lmodel="$1" prompt_file="$2" response_file="$3"
  local url="${MND_LLP_URL:-http://localhost:4000}" sock="${MND_LLP_SOCKET:-$HOME/.llp/control.sock}"
  local token
  token="$(curl -s --max-time 5 --unix-socket "$sock" -X POST http://unix/register \
    -H 'Content-Type: application/json' -d '{"agent":"mnd"}' | jq -r '.token // empty')"
  [[ -n "$token" ]] || { echo "  [llp] handshake failed at $sock" >&2; return 1; }
  local body="${SCRIPT_DIR}/data/.llp-body.json" resp="${SCRIPT_DIR}/data/.llp-resp.json" http
  jq -Rs --arg m "$lmodel" '{model: $m, messages: [{role: "user", content: .}]}' \
    < "$prompt_file" > "$body"
  http="$(curl -s -o "$resp" -w '%{http_code}' --max-time 300 \
    -K <(printf 'header = "Authorization: Bearer %s"\n' "$token") \
    -H 'Content-Type: application/json' --data-binary @"$body" \
    "$url/v1/chat/completions")" || { rm -f "$body"; return 1; }
  rm -f "$body"
  if [[ "$http" != "200" ]]; then
    echo "  [llp:$lmodel] HTTP $http: $(head -c 200 "$resp" | tr -d '\n')" >&2
    return 1
  fi
  jq -r '.choices[0].message.content // empty' "$resp" > "$response_file"
  rm -f "$resp"
  [[ -s "$response_file" ]] || { echo "  [llp:$lmodel] empty completion" >&2; return 1; }
  echo "  [llp:$lmodel] ok prompt_bytes=$(wc -c < "$prompt_file") response_bytes=$(wc -c < "$response_file")" >&2
}

# llm <model> <prompt_file> <response_file> — host-side LLM call (GML pattern)
llm() {
  local model="$1" prompt_file="$2" response_file="$3"
  if llp_up; then
    # LLP owns the capability ladder (2026-06-12): its `auto` chain walks
    # highest -> less capable with per-tier quota cooldowns, so one request is
    # enough. A claude model preference pins the top claude tier first, then
    # falls into the ladder. MND_LLP_CHAIN overrides (space-separated LLP
    # model names, e.g. "gemini/gemini-3-pro-preview claude/claude-opus-4-8").
    local chain="${MND_LLP_CHAIN:-}"
    if [[ -z "$chain" ]]; then
      case "$model" in
        claude) chain="claude auto" ;;
        *)      chain="auto" ;;
      esac
    fi
    local entry
    for entry in $chain; do
      llp_complete "$entry" "$prompt_file" "$response_file" && return 0
    done
    echo "  [llp] chain exhausted — falling back to direct CLI" >&2
  fi
  case "$model" in
    claude)
      # claude-opus-4-8: top available claude after claude-fable-5 was pulled
      # (Anthropic, 2026-06-13 — fable now exits non-zero "currently unavailable").
      claude -p --model "${MND_CLAUDE_MODEL:-claude-opus-4-8}" --output-format text < "$prompt_file" > "$response_file"
      ;;
    gemini)
      local gerr grc=0; gerr="$(mktemp)"
      # NOTE: --approval-mode plan is gated behind experimental.plan in
      # gemini-cli 0.29.5, so we use "default": in non-interactive (-p) mode
      # it cannot prompt and therefore auto-denies ALL tool calls. This flag
      # is mandatory — `-e none` only disables extensions, NOT built-in tools,
      # and the user-level defaultApprovalMode=auto_edit once let a large
      # prompt push the model into writing files into this repo by itself
      # (found live 2026-06-12).
      # MND_GEMINI_MODEL pins the model (-m) — the auto-routing classifier
      # chokes on very large prompts (>~300KB).
      local model_args=()
      [[ -n "${MND_GEMINI_MODEL:-}" ]] && model_args=(-m "$MND_GEMINI_MODEL")
      GOOGLE_CLOUD_PROJECT="${GOOGLE_CLOUD_PROJECT:?Set GOOGLE_CLOUD_PROJECT or run ./setup.sh}" timeout 300 npx @google/gemini-cli -e none --approval-mode default "${model_args[@]}" -p "" \
        < "$prompt_file" > "$response_file" 2>"$gerr" || grc=$?
      echo "  [gemini] exit=$grc prompt_bytes=$(wc -c < "$prompt_file") response_bytes=$(wc -c < "$response_file")" >&2
      if [[ $grc -ne 0 || ! -s "$response_file" ]]; then
        [[ $grc -eq 124 ]] && echo "  [gemini] TIMED OUT after 300s" >&2
        echo "  [gemini stderr]:" >&2
        sed 's/^/    /' "$gerr" >&2
        rm -f "$gerr"
        return 1
      fi
      rm -f "$gerr"
      ;;
    *)
      echo "error: unknown model '$model' (use claude or gemini)" >&2
      return 1
      ;;
  esac
}

# parse --model out of args, collect the rest
model="gemini"
pass_args=()
cmd="$1"; shift
while [[ $# -gt 0 ]]; do
  case "$1" in
    --model) model="$2"; shift 2 ;;
    *) pass_args+=("$1"); shift ;;
  esac
done

mkdir -p "${SCRIPT_DIR}/data" "${SCRIPT_DIR}/brain"

case "$cmd" in
  extract)
    mnd extract --claude-dir /sessions/claude --gemini-dir /sessions/gemini --out data/moments.jsonl
    ;;

  stats)
    mnd stats --moments data/moments.jsonl
    ;;

  distill)
    [[ -s "${SCRIPT_DIR}/data/moments.jsonl" ]] || { echo "error: no moments — run ./run-task.sh extract first" >&2; exit 1; }
    # Fresh batch set each run: already-distilled moments are excluded via
    # --skip-insights, so re-running only processes what's new.
    rm -rf "${SCRIPT_DIR}/data/batches" "${SCRIPT_DIR}/data/responses"
    mkdir -p "${SCRIPT_DIR}/data/batches" "${SCRIPT_DIR}/data/responses"
    echo "[1/3] Building batch prompts..." >&2
    mnd distill-prompts --moments data/moments.jsonl --out-dir data/batches \
      --skip-insights data/insights.yaml --processed data/processed.yaml "${pass_args[@]}"

    shopt -s nullglob
    prompts=("${SCRIPT_DIR}"/data/batches/batch-*.prompt)
    [[ ${#prompts[@]} -gt 0 ]] || { echo "nothing new to distill" >&2; exit 0; }
    echo "[2/3] Distilling ${#prompts[@]} batches with $model..." >&2
    failed=0
    for p in "${prompts[@]}"; do
      name="$(basename "$p" .prompt)"
      echo "  $name..." >&2
      if ! llm "$model" "$p" "${SCRIPT_DIR}/data/responses/${name}.response"; then
        echo "  $name FAILED — continuing" >&2
        failed=$((failed+1))
      fi
    done
    echo "[3/3] Merging into data/insights.yaml..." >&2
    mnd distill-merge --responses-dir data/responses --moments data/moments.jsonl \
      --insights data/insights.yaml --batches-dir data/batches --processed data/processed.yaml
    [[ $failed -eq 0 ]] || echo "warning: $failed batch(es) failed — re-run distill to retry them" >&2
    ;;

  profile)
    # data/ by default; MND_PROFILE_INSIGHTS / MND_PROFILE_DIR override (dev copies).
    pins="${MND_PROFILE_INSIGHTS:-data/insights.yaml}"
    pdir="${MND_PROFILE_DIR:-data/profiles}"
    echo "[1/2] Building profile prompt ($pins)..." >&2
    mnd profile-prompt --insights "$pins" --out data/profile.prompt
    echo "[2/2] Generating profiles with $model..." >&2
    # Direct CLI only (MND_LLP=off): the ~300KB profile prompt is the exact
    # input that once pushed gemini into writing files (MND-011), and LLP's
    # gemini impl doesn't carry --approval-mode default yet (todo.txt, Q1).
    # Route profiles through LLP only after that lands.
    MND_LLP=off llm "$model" "${SCRIPT_DIR}/data/profile.prompt" "${SCRIPT_DIR}/data/profile.response"
    mnd profile-write --response data/profile.response --out-dir data/profiles
    ;;

  ask)
    # remaining args: [--json] [--tail-file F] "question"
    json_flag=""
    question=""
    tail_file=""
    i=0
    while [[ $i -lt ${#pass_args[@]} ]]; do
      case "${pass_args[$i]}" in
        --json) json_flag="--json" ;;
        --tail-file) i=$((i+1)); tail_file="${pass_args[$i]}" ;;
        *) question="${pass_args[$i]}" ;;
      esac
      i=$((i+1))
    done
    if [[ -n "$tail_file" ]]; then
      mnd ask-prompt --tail-file "$tail_file" --brain-dir data --out data/ask.prompt --question-out data/ask.question >&2
    elif [[ -n "$question" ]]; then
      mnd ask-prompt --question "$question" --brain-dir data --out data/ask.prompt --question-out data/ask.question >&2
    else
      echo "usage: ./run-task.sh ask [--json] (--tail-file F | \"question\")" >&2; exit 1
    fi
    llm "$model" "${SCRIPT_DIR}/data/ask.prompt" "${SCRIPT_DIR}/data/ask.response"
    mnd ask-parse --response data/ask.response $json_flag
    ;;

  feedback-post)
    # Escalate the last unanswerable ask to DSH (called by orchestrate.sh on
    # confidence: low; also usable manually after a plain ask).
    mnd feedback-post --config data/dsh.yaml \
      --question-file data/ask.question --answer-file data/ask.response
    ;;

  learn)
    # Tomas's dismissal comments on MND escalations -> corrective insights.
    echo "[1/3] Gathering commented escalations from DSH..." >&2
    mnd learn-gather --config data/dsh.yaml --ledger data/feedback-ledger.yaml \
      --out data/learn.prompt --notifs-out data/learn.notifs.json
    [[ -s "${SCRIPT_DIR}/data/learn.prompt" ]] || exit 0
    echo "[2/3] Distilling corrective insights with $model..." >&2
    llm "$model" "${SCRIPT_DIR}/data/learn.prompt" "${SCRIPT_DIR}/data/learn.response"
    echo "[3/3] Merging into brain..." >&2
    mnd learn-merge --response data/learn.response --notifs data/learn.notifs.json \
      --insights data/insights.yaml --ledger data/feedback-ledger.yaml
    ;;

  contradictions)
    # Retire stale insights that newer corrections supersede (MND-025). The LLM
    # finds genuine conflicts; Go picks the winner by provenance. Idempotent —
    # only active insights are swept, so resolved losers never reappear.
    #
    # Loop-until-dry (MND-029): a single sweep's coverage is non-deterministic —
    # the LLM flags different conflict sets each pass. Repeat until DRY_NEEDED
    # consecutive passes resolve nothing (insights.yaml unchanged), capped at
    # MND_SWEEP_MAX passes so token cost stays bounded.
    max="${MND_SWEEP_MAX:-4}"; dry_needed="${MND_SWEEP_DRY:-2}"
    dry=0; pass=0
    while (( pass < max && dry < dry_needed )); do
      pass=$((pass+1))
      mnd contradiction-prompt --insights data/insights.yaml --out data/contradiction.prompt
      [[ -s "${SCRIPT_DIR}/data/contradiction.prompt" ]] || { echo "  [sweep] nothing to sweep" >&2; break; }
      before="$(sha256sum "${SCRIPT_DIR}/data/insights.yaml" | cut -d' ' -f1)"
      echo "  [sweep pass $pass/$max] sweeping with $model..." >&2
      llm "$model" "${SCRIPT_DIR}/data/contradiction.prompt" "${SCRIPT_DIR}/data/contradiction.response"
      mnd contradiction-merge --response data/contradiction.response --insights data/insights.yaml
      after="$(sha256sum "${SCRIPT_DIR}/data/insights.yaml" | cut -d' ' -f1)"
      if [[ "$before" == "$after" ]]; then
        dry=$((dry+1)); echo "  [sweep pass $pass] no change ($dry/$dry_needed dry)" >&2
      else
        dry=0; echo "  [sweep pass $pass] resolved changes — continuing" >&2
      fi
    done
    echo "  [sweep] done after $pass pass(es)" >&2
    ;;

  pipeline)
    "$0" extract
    "$0" distill --model "$model" "${pass_args[@]}"
    "$0" profile --model "$model"
    ;;

  eval)
    # Fidelity eval (iteration 8): how often does the clone's BLIND answer match
    # what Tomas actually decided? build cases → ask (clone) → judge → report.
    # in-sample by default (label says so); MND_EVAL_N candidate moments (def 40).
    n="${MND_EVAL_N:-40}"
    [[ -s "${SCRIPT_DIR}/data/moments.jsonl" ]] || "$0" extract
    rm -rf "${SCRIPT_DIR}/data/eval"; mkdir -p "${SCRIPT_DIR}/data/eval/asks"
    echo "[1/4] building eval cases (sampling n=$n)..." >&2
    mnd eval-build-prompt --moments data/moments.jsonl --n "$n" \
      --out data/eval/build.prompt --candidates-out data/eval/candidates.jsonl
    llm "$model" "${SCRIPT_DIR}/data/eval/build.prompt" "${SCRIPT_DIR}/data/eval/build.response"
    mnd eval-build-merge --response data/eval/build.response \
      --candidates data/eval/candidates.jsonl --out data/eval/cases.jsonl
    [[ -s "${SCRIPT_DIR}/data/eval/cases.jsonl" ]] || { echo "eval: no decision cases built from $n candidates — raise MND_EVAL_N" >&2; exit 1; }
    echo "[2/4] clone answering each case blind..." >&2
    mnd eval-ask-prompts --cases data/eval/cases.jsonl --brain-dir data --out-dir data/eval/asks
    shopt -s nullglob
    for p in "${SCRIPT_DIR}"/data/eval/asks/case-*.prompt; do
      llm "$model" "$p" "${p%.prompt}.response" || echo "  $(basename "$p") ask failed — continuing" >&2
    done
    mnd eval-ask-merge --cases data/eval/cases.jsonl --responses-dir data/eval/asks --out data/eval/answered.jsonl
    echo "[3/4] judging clone vs Tomas..." >&2
    mnd eval-judge-prompt --answered data/eval/answered.jsonl --out data/eval/judge.prompt
    llm "$model" "${SCRIPT_DIR}/data/eval/judge.prompt" "${SCRIPT_DIR}/data/eval/judge.response"
    mnd eval-judge-merge --response data/eval/judge.response \
      --answered data/eval/answered.jsonl --out data/eval/scored.jsonl
    echo "[4/4] report..." >&2
    mnd eval-report --scored data/eval/scored.jsonl --provenance in-sample \
      --out-md data/eval/report.md --out-json data/eval/report.json \
      --at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "── report: ${SCRIPT_DIR}/data/eval/report.md" >&2
    ;;

  dedup)
    # Merge semantically-duplicate insights (iter 9 lever B). Per category:
    # prompt → LLM → merge. data/insights.yaml by default; MND_DEDUP_INSIGHTS
    # overrides (used to dedup a dev copy for A/B measurement).
    ins="${MND_DEDUP_INSIGHTS:-data/insights.yaml}"
    for cat in tech_preference decision_heuristic direction_pattern correction_pattern; do
      mnd dedup-prompt --insights "$ins" --category "$cat" --out data/dedup.prompt
      [[ -s "${SCRIPT_DIR}/data/dedup.prompt" ]] || continue
      echo "  dedup $cat..." >&2
      llm "$model" "${SCRIPT_DIR}/data/dedup.prompt" "${SCRIPT_DIR}/data/dedup.response"
      mnd dedup-merge --response data/dedup.response --insights "$ins"
    done
    ;;

  eval-rerun)
    # Re-ask + re-judge the EXISTING data/eval/cases.jsonl against the current
    # data/prompt — a controlled A/B (same cases) to measure whether a change
    # moved fidelity, without rebuilding the case set. MND_EVAL_BRAIN points at a
    # dev brain copy (iteration 9 tooling).
    [[ -s "${SCRIPT_DIR}/data/eval/cases.jsonl" ]] || { echo "no data/eval/cases.jsonl — run eval first" >&2; exit 1; }
    bdir="${MND_EVAL_BRAIN:-data}"
    rm -rf "${SCRIPT_DIR}/data/eval/asks"; mkdir -p "${SCRIPT_DIR}/data/eval/asks"
    echo "[1/3] re-asking $(wc -l < "${SCRIPT_DIR}/data/eval/cases.jsonl") cases blind (brain=$bdir)..." >&2
    mnd eval-ask-prompts --cases data/eval/cases.jsonl --brain-dir "$bdir" --out-dir data/eval/asks
    shopt -s nullglob
    for p in "${SCRIPT_DIR}"/data/eval/asks/case-*.prompt; do
      llm "$model" "$p" "${p%.prompt}.response" || echo "  $(basename "$p") failed" >&2
    done
    mnd eval-ask-merge --cases data/eval/cases.jsonl --responses-dir data/eval/asks --out data/eval/answered.jsonl
    echo "[2/3] judging..." >&2
    mnd eval-judge-prompt --answered data/eval/answered.jsonl --out data/eval/judge.prompt
    llm "$model" "${SCRIPT_DIR}/data/eval/judge.prompt" "${SCRIPT_DIR}/data/eval/judge.response"
    mnd eval-judge-merge --response data/eval/judge.response --answered data/eval/answered.jsonl --out data/eval/scored.jsonl
    echo "[3/3] report..." >&2
    mnd eval-report --scored data/eval/scored.jsonl --provenance in-sample \
      --out-md data/eval/report.md --out-json data/eval/report.json --at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    ;;

  classify)
    # Classify ONE incoming question into a routing category (iter 10): prints
    # one word to stdout — tech_preference|decision_heuristic|direction_pattern|
    # correction_pattern|other. Fails safe to "other" (escalate-by-default).
    # The orchestrator's competence gate auto-answers reliable categories and
    # escalates the rest to Tomas. Keyed on the QUESTION, never on self-confidence.
    q=""
    i=0
    while [[ $i -lt ${#pass_args[@]} ]]; do
      case "${pass_args[$i]}" in
        --question-file) i=$((i+1)); q="$(cat "${pass_args[$i]}")" ;;
        *) q="${pass_args[$i]}" ;;
      esac
      i=$((i+1))
    done
    [[ -n "$q" ]] || { echo "usage: ./run-task.sh classify (--question-file F | \"question\")" >&2; exit 1; }
    mkdir -p "${SCRIPT_DIR}/data/route"
    mnd route-classify-prompt --question "$q" --out data/route/one.prompt >&2
    if ! llm "$model" "${SCRIPT_DIR}/data/route/one.prompt" "${SCRIPT_DIR}/data/route/one.response" >&2; then
      echo other; exit 0   # fail safe: escalate
    fi
    mnd route-classify-merge --question --response data/route/one.response
    ;;

  route-eval)
    # Competence-routing measurement (iter 10): classify each existing eval case
    # by CATEGORY (the orchestrator's router signal), then simulate routing
    # against the judge's verdicts — does auto-answering high-fidelity categories
    # and escalating the rest beat answering everything? No self-confidence used.
    [[ -s "${SCRIPT_DIR}/data/eval/cases.jsonl" ]]  || { echo "no data/eval/cases.jsonl — run eval first" >&2; exit 1; }
    # Route against the PRODUCTION-brain verdicts. baseline-scored.jsonl is the
    # iter-8 production baseline; scored.jsonl may be a rejected dev-brain A/B.
    sc="${MND_ROUTE_SCORED:-}"
    if [[ -z "$sc" ]]; then
      if [[ -s "${SCRIPT_DIR}/data/eval/baseline-scored.jsonl" ]]; then sc="data/eval/baseline-scored.jsonl"
      else sc="data/eval/scored.jsonl"; fi
    fi
    [[ -s "${SCRIPT_DIR}/$sc" ]] || { echo "no scored set ($sc) — run eval first" >&2; exit 1; }
    mkdir -p "${SCRIPT_DIR}/data/route"
    echo "[1/3] classifying $(wc -l < "${SCRIPT_DIR}/data/eval/cases.jsonl") cases by category..." >&2
    mnd route-classify-prompt --cases data/eval/cases.jsonl --out data/route/classify.prompt
    llm "$model" "${SCRIPT_DIR}/data/route/classify.prompt" "${SCRIPT_DIR}/data/route/classify.response"
    mnd route-classify-merge --response data/route/classify.response \
      --cases data/eval/cases.jsonl --out data/route/labels.jsonl
    echo "[2/3] simulating routing against verdicts ($sc)..." >&2
    auto_cats="${MND_ROUTE_AUTO:-correction_pattern,direction_pattern}"
    mnd route-sim --scored "$sc" --labels data/route/labels.jsonl \
      --auto-cats "$auto_cats" \
      --out-md data/route/report.md --out-json data/route/sweep.json \
      --at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "[3/3] report: ${SCRIPT_DIR}/data/route/report.md" >&2
    ;;

  # --- Embedding (iter 11) ----------------------------------------------------

  embed-start)
    # Start Ollama with GPU, pull model if needed. Idempotent.
    docker compose -f "${SCRIPT_DIR}/docker-compose.yml" --profile embed up -d ollama
    echo "waiting for Ollama..." >&2
    for i in $(seq 1 30); do
      curl -s --max-time 2 http://localhost:11434/api/tags >/dev/null 2>&1 && break
      sleep 1
    done
    curl -s --max-time 2 http://localhost:11434/api/tags >/dev/null 2>&1 || { echo "Ollama failed to start" >&2; exit 1; }
    # Pull model if not present
    embed_model="${MND_EMBED_MODEL:-nomic-embed-text}"
    if ! curl -s http://localhost:11434/api/tags | grep -q "\"$embed_model\""; then
      echo "pulling $embed_model..." >&2
      curl -s http://localhost:11434/api/pull -d "{\"name\":\"$embed_model\"}" | tail -1
    fi
    echo "Ollama ready (model: $embed_model)" >&2
    ;;

  embed-batch)
    # Embed all active insights → data/embeddings.json. Delta: only new/changed.
    embed_model="${MND_EMBED_MODEL:-nomic-embed-text}"
    embed_url="${MND_EMBED_URL:-http://localhost:11434}"
    # Ensure Ollama is up
    curl -s --max-time 2 "$embed_url/api/tags" >/dev/null 2>&1 || "$0" embed-start
    echo "[1/3] loading insights and existing embeddings..." >&2
    mnd embed-plan --insights data/insights.yaml --embeddings data/embeddings.json \
      --model "$embed_model" --texts-out data/embed-batch.jsonl
    n="$(wc -l < "${SCRIPT_DIR}/data/embed-batch.jsonl" 2>/dev/null || echo 0)"
    if [[ "$n" -eq 0 ]]; then
      echo "embed-batch: all insights already embedded — nothing to do" >&2
      exit 0
    fi
    echo "[2/3] embedding $n insights via $embed_model..." >&2
    # Batch in chunks of 100 (Ollama handles large batches but be safe)
    > "${SCRIPT_DIR}/data/embed-responses.jsonl"
    while IFS= read -r batch; do
      curl -s "$embed_url/api/embed" -d "$batch" >> "${SCRIPT_DIR}/data/embed-responses.jsonl"
      echo "" >> "${SCRIPT_DIR}/data/embed-responses.jsonl"
    done < "${SCRIPT_DIR}/data/embed-batch.jsonl"
    echo "[3/3] merging into embeddings store..." >&2
    mnd embed-merge --responses data/embed-responses.jsonl --plan data/embed-batch.jsonl \
      --embeddings data/embeddings.json --model "$embed_model"
    echo "embed-batch: done ($(grep -c '"id"' "${SCRIPT_DIR}/data/embeddings.json" 2>/dev/null || echo 0) total vectors)" >&2
    ;;

  embed-query)
    # Embed a single question → stdout JSON vector.
    embed_model="${MND_EMBED_MODEL:-nomic-embed-text}"
    embed_url="${MND_EMBED_URL:-http://localhost:11434}"
    q=""
    i=0
    while [[ $i -lt ${#pass_args[@]} ]]; do
      case "${pass_args[$i]}" in
        --question-file) i=$((i+1)); q="$(cat "${pass_args[$i]}")" ;;
        *) q="${pass_args[$i]}" ;;
      esac
      i=$((i+1))
    done
    [[ -n "$q" ]] || { echo "usage: ./run-task.sh embed-query (--question-file F | \"question\")" >&2; exit 1; }
    resp="$(curl -s "$embed_url/api/embed" -d "$(jq -n --arg m "$embed_model" --arg t "$q" '{model:$m, input:[$t]}')")"
    echo "$resp" | jq -c '.embeddings[0]'
    ;;

  retrain)
    before="$(sha256sum "${SCRIPT_DIR}/data/insights.yaml" 2>/dev/null | cut -d' ' -f1 || true)"
    "$0" extract
    "$0" distill --model "$model" "${pass_args[@]}"
    if [[ -f "${SCRIPT_DIR}/data/dsh.yaml" ]]; then
      "$0" learn --model "$model" || echo "learn failed — continuing retrain" >&2
    fi
    "$0" contradictions --model "$model" || echo "contradiction sweep failed — continuing retrain" >&2
    after="$(sha256sum "${SCRIPT_DIR}/data/insights.yaml" 2>/dev/null | cut -d' ' -f1 || true)"
    if [[ "$before" == "$after" ]]; then
      echo "retrain: no brain changes — profiles unchanged" >&2
    else
      MND_GEMINI_MODEL="${MND_GEMINI_MODEL:-gemini-2.5-pro}" "$0" profile --model "$model"
      echo "retrain: brain updated — profiles regenerated" >&2
    fi
    # mandatory fidelity eval after every retrain
    echo "retrain: running fidelity eval..." >&2
    "$0" eval --model "$model"
    "$0" route-eval --model "$model"
    auto_cats="${MND_ROUTE_AUTO:-correction_pattern,direction_pattern}"
    min_fidelity="${MND_FIDELITY_MIN_AUTO:-75}"
    if mnd fidelity-check --auto-cats "$auto_cats" --min-auto "$min_fidelity" \
         --eval-json data/eval/report.json --sweep-json data/route/sweep.json; then
      echo "retrain: fidelity check passed" >&2
    else
      echo "retrain: ⚠ FIDELITY BELOW THRESHOLD — escalating to DSH" >&2
      if [[ -f "${SCRIPT_DIR}/data/dsh.yaml" ]]; then
        alert_msg="[MND fidelity-alert] Post-retrain fidelity dropped below ${min_fidelity}% for auto-set (${auto_cats}). Review data/eval/report.md and data/route/report.md."
        mnd fidelity-alert --config data/dsh.yaml --message "$alert_msg" || echo "DSH alert failed" >&2
      fi
    fi
    echo "retrain: done — run ./backup.sh to persist" >&2
    ;;

  watch-retrain)
    interval="${MND_RETRAIN_INTERVAL:-86400}"
    echo "watch-retrain: every ${interval}s (set MND_RETRAIN_INTERVAL to change)" >&2
    while true; do
      echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] retrain run starting" >&2
      "$0" retrain --model "$model" "${pass_args[@]}" || echo "retrain run failed — will retry next interval" >&2
      sleep "$interval"
    done
    ;;

  *)
    echo "error: unknown command '$cmd'" >&2
    exit 1
    ;;
esac
