#!/usr/bin/env bash
# orchestrate-watch.sh — hands-off orchestration: poll herdr and answer every
# agent that goes blocked/idle, the way Tomas would (MND iteration 4).
#
# Per agent+question: answer ONCE; if the same question survives our answer,
# escalate to DSH instead of resending (MND-022 loop protection). Agents whose
# cwd is this worktree are skipped — the orchestrator never orchestrates itself.
#
# usage: ./orchestrate-watch.sh [--once] [--dry-run] | list | --help
# Run ONE instance — concurrent watchers would race on data/ask.* and the ledger.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
LEDGER="${SCRIPT_DIR}/data/watch-ledger.jsonl"

interval="${MND_WATCH_INTERVAL:-30}"
# done is included by design (Tomas's spec: "when they are done/idle/blocked"):
# herdr can report `done` while the claude REPL is alive and showing a pending
# question (seen live 2026-06-12, DXP audit). The pending:none gate protects
# truly-finished agents from noise.
statuses="${MND_WATCH_STATUSES:-blocked,idle,done}"
exclude="${MND_WATCH_EXCLUDE:-}"
cooldown="${MND_WATCH_COOLDOWN:-60}"

usage() {
  cat <<'EOF'
orchestrate-watch.sh — continuous Tomas-style feedback to herdr agents

usage: ./orchestrate-watch.sh (all | pane ...) [--once] [--dry-run]
       ./orchestrate-watch.sh list [pane ...]
       ./orchestrate-watch.sh --help

  (no args)   this help screen
  all         continuous feedback to ALL agents: poll herdr every
              MND_WATCH_INTERVAL seconds, deliver direction to every agent
              whose status is in MND_WATCH_STATUSES (MND_WATCH_EXCLUDE applies)
  pane ...    only these panes/terminals (one or a selection); explicit
              targets override MND_WATCH_EXCLUDE — only the self-pane rule
              still applies
  --once      single pass, then exit (testing, cron)
  --dry-run   preview only: print proposed directions — no sends, no
              escalations, no ledger writes
  list        show every herdr agent and the verdict the watch would reach
              for it RIGHT NOW (orchestrate / escalate / skip + why)
  --help      this text

per-question behavior: answer ONCE per (pane, question); the same question
reappearing after our answer means the direction failed -> DSH escalation;
'pending: none' (the agent asks nothing) -> leave it alone. Ledger:
data/watch-ledger.jsonl

env:
  MND_WATCH_INTERVAL  poll seconds (default 30)
  MND_WATCH_STATUSES  statuses to react to (default "blocked,idle,done";
                      done agents often still show a live REPL with a pending
                      question — the pending gate protects the finished ones)
  MND_WATCH_EXCLUDE   comma-separated pane ids to skip
  MND_WATCH_LINES     terminal-tail lines to read & hash (default 40)
  MND_WATCH_COOLDOWN  min seconds between actions on one pane (default 60)
  MND_ASK_MODEL       model preference for the underlying ask (gemini|claude)
  MND_ROUTE           competence gate (iter 10): off = answer everything;
                      on (default) = auto-answer reliable categories, escalate
                      judgment calls to Tomas (the real safety valve)
  MND_ROUTE_AUTO      auto-answer categories (default correction_pattern,direction_pattern)

Run ONE instance — concurrent watchers race on data/ask.* and the ledger.
EOF
}

once=false dry=false list_only=false all_mode=false
targets=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --once)         once=true; shift ;;
    --dry-run)      dry=true; shift ;;
    all)            all_mode=true; shift ;;
    list|--list)    list_only=true; shift ;;
    -h|--help|help) usage; exit 0 ;;
    -*) echo "error: unknown arg '$1' (see --help)" >&2; exit 64 ;;
    *)  targets+=("$1"); shift ;;
  esac
done

if $all_mode && [[ ${#targets[@]} -gt 0 ]]; then
  echo "error: use either 'all' or specific pane ids, not both" >&2; exit 64
fi
# No scope given -> help (running against the whole fleet must be explicit).
if ! $list_only && ! $all_mode && [[ ${#targets[@]} -eq 0 ]]; then
  usage; exit 0
fi

# target_match <pane> <terminal> — no targets = everything is in scope
target_match() {
  [[ ${#targets[@]} -eq 0 ]] && return 0
  local t
  for t in "${targets[@]}"; do
    [[ "$1" == "$t" || "$2" == "$t" ]] && return 0
  done
  return 1
}

mkdir -p "${SCRIPT_DIR}/data"

log() { echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] $*"; }

# Normalization identical to the sent-ledger (and internal/exclude.NormHash).
norm_hash() {
  tr '[:upper:]' '[:lower:]' | tr -s '[:space:]' ' ' | sed 's/^ //;s/ $//' \
    | sha256sum | cut -d' ' -f1
}

last_action() { # <pane> <hash> → most recent ledgered action ("" if none)
  [[ -f "$LEDGER" ]] || { echo ""; return; }
  jq -r --arg p "$1" --arg h "$2" \
    'select(.pane == $p and .hash == $h) | .action' "$LEDGER" 2>/dev/null | tail -1
}

pane_busy_until() { # <pane> → epoch seconds before which the pane is in cooldown
  [[ -f "$LEDGER" ]] || { echo 0; return; }
  local ts
  ts="$(jq -r --arg p "$1" 'select(.pane == $p) | .ts' "$LEDGER" 2>/dev/null | tail -1)"
  [[ -n "$ts" ]] || { echo 0; return; }
  echo $(( $(date -u -d "$ts" +%s) + cooldown ))
}

ledger_add() { # <pane> <hash> <action>
  printf '{"ts":"%s","pane":"%s","hash":"%s","action":"%s"}\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$2" "$3" >> "$LEDGER"
}

handle_pane() { # <pane> <status>
  local pane="$1" status="$2" tail_text hash last rc=0
  tail_text="$(herdr pane read "$pane" --source visible --lines "${MND_WATCH_LINES:-40}" 2>/dev/null || true)"
  [[ -n "${tail_text//[[:space:]]/}" ]] || return 0
  hash="$(printf '%s' "$tail_text" | norm_hash)"
  last="$(last_action "$pane" "$hash")"

  case "$last" in
    sent)
      # Same visible question after a delivered answer — direction failed;
      # this belongs to the real Tomas now.
      log "$pane [$status]: question survived our answer — escalating"
      $dry && return 0
      "${SCRIPT_DIR}/orchestrate.sh" "$pane" --escalate >/dev/null || rc=$?
      [[ $rc -eq 2 ]] && ledger_add "$pane" "$hash" escalated \
        || log "$pane: escalation attempt exited rc=$rc"
      ;;
    escalated|none|skip)
      log "$pane [$status]: skip — already handled ($last); waiting for the pane to change"
      ;;
    *)
      if (( $(date -u +%s) < $(pane_busy_until "$pane") )); then
        log "$pane [$status]: in cooldown — next pass"
        return 0
      fi
      log "$pane [$status]: orchestrating — asking the mind model (can take 30s-3min while a quota-cooled tier burns off)"
      if $dry; then
        "${SCRIPT_DIR}/orchestrate.sh" "$pane" --dry-run || rc=$?
        return 0
      fi
      "${SCRIPT_DIR}/orchestrate.sh" "$pane" || rc=$?
      case $rc in
        0) ledger_add "$pane" "$hash" sent;      log "$pane: direction sent" ;;
        2) ledger_add "$pane" "$hash" escalated; log "$pane: low confidence — escalated to DSH" ;;
        3) ledger_add "$pane" "$hash" none;      log "$pane: no pending question" ;;
        *) ledger_add "$pane" "$hash" skip;      log "$pane: orchestrate failed rc=$rc — skipping this state" ;;
      esac
      ;;
  esac
}

# verdict <pane> <terminal> <status> <cwd> — what would the watch do with this agent now?
verdict() {
  local pane="$1" term="$2" status="$3" cwd="$4" tail_text hash last now busy
  case "$cwd" in "$REPO_ROOT"*) echo "skip: self (the orchestrator's own worktree)"; return ;; esac
  target_match "$pane" "$term" || { echo "skip: not in selected targets"; return; }
  if [[ ${#targets[@]} -eq 0 ]]; then
    case ",$exclude," in *",$pane,"*) echo "skip: excluded (MND_WATCH_EXCLUDE)"; return ;; esac
  fi
  case ",$statuses," in *",$status,"*) ;; *) echo "skip: status not watched"; return ;; esac
  tail_text="$(herdr pane read "$pane" --source visible --lines "${MND_WATCH_LINES:-40}" 2>/dev/null || true)"
  [[ -n "${tail_text//[[:space:]]/}" ]] || { echo "skip: empty terminal tail"; return; }
  hash="$(printf '%s' "$tail_text" | norm_hash)"
  last="$(last_action "$pane" "$hash")"
  case "$last" in
    sent)       echo "would ESCALATE to DSH (question survived our answer)" ;;
    escalated)  echo "skip: already escalated to DSH (waiting for Tomas)" ;;
    none)       echo "skip: no pending question (remembered)" ;;
    skip)       echo "skip: orchestrate previously failed on this state" ;;
    *)
      now="$(date -u +%s)"; busy="$(pane_busy_until "$pane")"
      if (( now < busy )); then echo "skip: cooldown ($((busy - now))s left)"
      else echo "would ORCHESTRATE (answer or escalate by confidence)"; fi ;;
  esac
}

list_verdicts() {
  {
    printf 'PANE\tSTATUS\tVERDICT\tCWD\n'
    herdr agent list \
      | jq -r '.result.agents[] | [.pane_id, .terminal_id, .agent_status, .cwd] | @tsv' \
      | while IFS=$'\t' read -r pane term status cwd; do
          printf '%s\t%s\t%s\t%s\n' "$pane" "$status" "$(verdict "$pane" "$term" "$status" "$cwd" </dev/null)" "$cwd"
        done
  } | column -t -s $'\t'
}

pass_n=0
pass() {
  local pane term status cwd row candidates=()
  pass_n=$((pass_n + 1))
  while IFS=$'\t' read -r pane term status cwd; do
    [[ -n "$pane" ]] || continue
    case "$cwd" in "$REPO_ROOT"*) continue ;; esac        # never orchestrate self
    target_match "$pane" "$term" || continue              # one / selected / all
    if [[ ${#targets[@]} -eq 0 ]]; then                   # explicit target wins over excludes
      case ",$exclude," in *",$pane,"*) continue ;; esac
    fi
    candidates+=("$pane"$'\t'"$status")
  done < <(herdr agent list \
    | jq -r --arg st "$statuses" '
        .result.agents[]
        | select(.agent_status as $s | $st | split(",") | index($s))
        | [.pane_id, .terminal_id, .agent_status, .cwd] | @tsv')
  log "pass $pass_n: ${#candidates[@]} candidate agent(s) in {$statuses}"
  for row in ${candidates[@]+"${candidates[@]}"}; do
    IFS=$'\t' read -r pane status <<<"$row"
    # </dev/null: children (docker compose run -T, curl) must not slurp any
    # inherited stdin — that silently drops work (found live in iteration 4).
    handle_pane "$pane" "$status" </dev/null
  done
}

if $list_only; then
  list_verdicts
  exit 0
fi

log "watch: targets={${targets[*]:-all}} statuses={$statuses} interval=${interval}s cooldown=${cooldown}s self=$REPO_ROOT ${exclude:+exclude={$exclude}}"
if $dry && ! $once; then
  log "note: --dry-run writes no ledger, so every pass re-asks the same panes (LLM cost each time);"
  log "      you probably want './orchestrate-watch.sh --once --dry-run' or the instant './orchestrate-watch.sh list'"
fi
while true; do
  pass || log "pass failed — retrying next interval"
  $once && { log "pass complete (--once)"; exit 0; }
  log "pass complete — next pass in ${interval}s (Ctrl+C to stop)"
  sleep "$interval"
done
