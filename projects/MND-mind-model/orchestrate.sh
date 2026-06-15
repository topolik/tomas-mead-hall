#!/usr/bin/env bash
# orchestrate.sh — answer a herdr agent's pending question the way Tomas would.
#
# Reads the tail of the agent's terminal, asks the mind model for direction,
# and (with --send) delivers the answer back into the agent's pane.
#
# usage: ./orchestrate.sh <herdr-target> [--dry-run] [--escalate] [--lines N] [--model gemini|claude]
# Delivery is the DEFAULT (Tomas, 2026-06-12 — the orchestrator's job is
# continuous feedback, not previews); --dry-run is the shared preview flag
# here and in orchestrate-watch.sh.
#
# Safety valve (iter 10): a COMPETENCE GATE keyed on the question's category.
# The clone is measured-reliable on some categories and not on judgment calls
# (fidelity eval), and its self-reported confidence can't tell the two apart
# (uniformly "high"). So reliable categories auto-answer; the rest escalate to
# the real Tomas. MND_ROUTE_AUTO tunes the auto set; MND_ROUTE=off disables it.
#
# exit codes: 0 = sent (or dry-run) · 2 = escalated to DSH · 3 = no pending question
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Step narration -> stderr (stdout carries the direction/result block).
say() { echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] $*" >&2; }

usage() {
  cat <<'EOF'
orchestrate.sh — answer a herdr agent's pending question the way Tomas would

usage: ./orchestrate.sh <herdr-target> [--dry-run] [--escalate] [--lines N] [--model gemini|claude]
       ./orchestrate.sh list
       ./orchestrate.sh --help

  <herdr-target>  terminal id, unique agent name, or pane id (see: ./orchestrate.sh list)
  list            show every herdr agent (the valid targets) with status and cwd
  --dry-run       preview only: print the proposed direction, deliver nothing
                  (default: deliver — send the direction into the agent's pane;
                  confidence: low refuses to send and escalates to DSH instead)
  --escalate      don't send; escalate the pending question to DSH regardless of
                  confidence (watch mode uses this when a question survives an answer)
  --lines N       terminal-tail lines to read (default 40)
  --model M       LLM preference: gemini (default) or claude — or env MND_ASK_MODEL

competence gate (iter 10): the question is classified by category; reliable
categories auto-answer, the rest escalate to Tomas (the real safety valve, since
confidence is non-discriminating). Tune with:
  MND_ROUTE=off              disable the gate (legacy: answer everything)
  MND_ROUTE_AUTO=c1,c2       auto-answer categories (default correction_pattern,direction_pattern)
                             categories: tech_preference decision_heuristic direction_pattern correction_pattern

exit codes: 0 = sent (or dry-run) · 2 = escalated to DSH · 3 = no pending question

The answer comes from the mind model (data/ profiles + insights); every delivered
direction is ledgered so retraining never learns from the orchestrator's own words.
LLM routing follows run-task.sh (LLP gateway when up; MND_LLP, MND_LLP_CHAIN, ...).
EOF
}

# list_agents — every herdr agent = every valid target. The orchestrator's own
# worktree is marked: directing yourself is recursion, not orchestration.
list_agents() {
  {
    printf 'PANE\tSTATUS\tAGENT\tCWD\n'
    herdr agent list | jq -r '.result.agents[] | [.pane_id, .agent_status, .agent, .cwd] | @tsv' \
      | while IFS=$'\t' read -r pane status agent cwd; do
          mark=""
          case "$cwd" in "$REPO_ROOT"*) mark=" (self — do not orchestrate)" ;; esac
          printf '%s\t%s\t%s\t%s%s\n' "$pane" "$status" "$agent" "$cwd" "$mark"
        done
  } | column -t -s $'\t'
}

case "${1:-}" in
  "")               usage >&2; exit 64 ;;
  -h|--help|help)   usage; exit 0 ;;
  list)             list_agents; exit 0 ;;
esac
target="$1"
shift
# MND_ASK_MODEL: model preference when --model isn't given (watch mode runs
# non-interactively and steers via env; LLP-routed calls override per-chain).
dry=false escalate=false lines=40 model="${MND_ASK_MODEL:-gemini}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)  dry=true; shift ;;
    --send)     dry=false; shift ;;   # legacy alias — delivery is the default now
    --escalate) escalate=true; shift ;;
    --lines)    lines="$2"; shift 2 ;;
    --model)    model="$2"; shift 2 ;;
    *) echo "error: unknown arg '$1' (see --help)" >&2; exit 64 ;;
  esac
done

# 0. Resolve the pane behind the target (needed for read and send;
#    `pane read` yields raw text while `agent read` wraps it in JSON).
pane_id="$(herdr agent get "$target" | jq -r '.result.pane_id // .result.agent.pane_id // empty')"
if [[ -z "$pane_id" ]]; then
  echo "error: cannot resolve pane id for '$target' (see \`herdr agent list\`)" >&2
  exit 1
fi
say "target '$target' resolved to pane $pane_id"

# 1. What is the agent asking? (tail of its terminal; visible buffer —
#    recent-unwrapped can be empty right after a status change)
context="$(herdr pane read "$pane_id" --source visible --lines "$lines")"
if [[ -z "${context//[[:space:]]/}" ]]; then
  echo "error: no output readable from '$target' ($pane_id)" >&2
  exit 1
fi
say "read terminal tail: $lines lines, ${#context} chars"

# 2. Ask the mind model. The tail goes through mnd ask-prompt --tail-file,
#    which frames AND datamarks it (MND-013) — untrusted content can't pose
#    as instructions, and the datamark self-marks the prompt for retraining
#    exclusion.
printf '%s' "$context" > "${SCRIPT_DIR}/data/orchestrate.tail"
say "asking the mind model (model preference: $model; LLP routing when up) — this is the slow step..."
ask_started=$SECONDS
response="$("${SCRIPT_DIR}/run-task.sh" ask --json --model "$model" --tail-file data/orchestrate.tail)"
answer="$(jq -r '.answer' <<<"$response")"
confidence="$(jq -r '.confidence' <<<"$response")"
citations="$(jq -r '.citations | join(", ")' <<<"$response")"
pending="$(jq -r '.pending // "question"' <<<"$response")"
say "answer in $((SECONDS - ask_started))s: confidence=$confidence pending=$pending citations=$(jq -r '.citations | length' <<<"$response")"

# No question waiting (MND-023) — the agent finished cleanly or is mid-work;
# sending fabricated direction would inject noise into a healthy agent.
if [[ "$pending" == "none" ]]; then
  echo "── no pending question detected — nothing to send ($answer)"
  exit 3
fi

# Attribution prefix (iter 5, Tomas): every delivered direction is visibly
# marked as the MND orchestrator's voice — so the receiving agent and anyone
# reading the pane can tell it's not Tomas typing. It also doubles as a
# self-exclusion phrase-marker (internal/exclude phraseMarkers) so retraining
# never relearns MND's own output. Keep the default in sync with that marker.
prefix="${MND_SEND_PREFIX:-[MND orchestrator]}"
sent="${prefix} ${answer}"

echo "── proposed direction ─────────────────────────── [confidence: $confidence]"
echo "$answer"
[[ -n "$citations" ]] && echo "── evidence: $citations"

# Forced escalation (watch mode, MND-022): the question reappeared after a
# delivered answer — the mind model's direction didn't unblock the agent, so
# this is Tomas's by definition, whatever the confidence says.
if $escalate; then
  echo "── forced escalation requested" >&2
  if "${SCRIPT_DIR}/run-task.sh" feedback-post 2>&1 | sed 's/^/── /'; then
    echo "── escalated to DSH — answer it there; 'learn' ingests your comment" >&2
  else
    echo "── DSH escalation failed (no data/dsh.yaml? see dsh.yaml.example)" >&2
  fi
  exit 2
fi

# Competence gate (iter 10): the confidence signal can't separate good answers
# from bad (eval: uniformly "high"), so the real safety valve is the QUESTION
# CATEGORY. The clone is measured-reliable on some categories (correction ~86%,
# direction) and not on judgment calls (~38%); auto-answer the reliable ones,
# route the rest to the real Tomas. Keyed on the question, never on self-report.
# MND_ROUTE_AUTO = comma-list of auto-answer categories (default the measured-safe
# set: delivered ~78% vs 59% blanket, 0 judgment leaks — data/route/report.md).
# MND_ROUTE=off disables the gate (legacy answer-everything behavior).
route_action="auto"; route_cat="(gate off)"
if [[ "${MND_ROUTE:-on}" != "off" ]]; then
  auto_set="${MND_ROUTE_AUTO:-correction_pattern,direction_pattern}"
  say "competence gate: classifying the question (auto set: $auto_set)..."
  route_cat="$("${SCRIPT_DIR}/run-task.sh" classify --model "$model" --question-file data/ask.question 2>/dev/null || echo other)"
  if [[ ",$auto_set," == *",$route_cat,"* ]]; then
    route_action="auto"
    say "competence gate: '$route_cat' is a reliable category -> auto-answer"
  else
    route_action="escalate"
    say "competence gate: '$route_cat' is a judgment/low-fidelity category -> escalate to Tomas"
  fi
fi

# 3. Deliver — or refuse when the gate routes to Tomas / the brain is silent.
if $dry; then
  echo "── competence gate: category=$route_cat -> $route_action"
  if [[ "$route_action" == "escalate" ]]; then
    echo "── dry-run: would ESCALATE to Tomas (DSH), not deliver."
  else
    echo "── dry-run: not sent (drop --dry-run to deliver). Would deliver:"
    echo "   $sent"
  fi
  exit 0
fi
if [[ "$route_action" == "escalate" ]]; then
  echo "── competence gate: '$route_cat' is a category the clone isn't reliable on — routing to Tomas, not auto-answering" >&2
  if "${SCRIPT_DIR}/run-task.sh" feedback-post 2>&1 | sed 's/^/── /'; then
    echo "── escalated to DSH — answer it there; 'learn' ingests your comment" >&2
  else
    echo "── DSH escalation failed (no data/dsh.yaml? see dsh.yaml.example)" >&2
  fi
  exit 2
fi
if [[ "$confidence" == "low" ]]; then
  echo "── confidence low: NOT sending — this needs the real Tomas" >&2
  # Close the human loop (iteration 3): escalate to DSH. Tomas's dismissal
  # comment comes back as a corrective insight via `run-task.sh learn`.
  if "${SCRIPT_DIR}/run-task.sh" feedback-post 2>&1 | sed 's/^/── /'; then
    echo "── escalated to DSH — answer it there; 'learn' ingests your comment" >&2
  else
    echo "── DSH escalation failed (no data/dsh.yaml? see dsh.yaml.example)" >&2
  fi
  exit 2
fi

say "delivering direction into pane $pane_id (prefixed: ${prefix})"
herdr pane send-text "$pane_id" "$sent"
herdr pane send-keys "$pane_id" Enter

# Ledger every delivered direction (MND-015): retraining must never learn from
# text the orchestrator itself authored. Hash the ACTUAL sent text (prefix
# included) — that's what lands in the session, so that's what self-exclusion
# must match. Hash matches internal/exclude.NormHash.
norm="$(tr '[:upper:]' '[:lower:]' <<<"$sent" | tr -s '[:space:]' ' ' | sed 's/^ //;s/ $//')"
hash="$(printf '%s' "$norm" | sha256sum | cut -d' ' -f1)"
printf '{"ts":"%s","target":"%s","hash":"%s"}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$target" "$hash" >> "${SCRIPT_DIR}/data/sent-ledger.jsonl"
echo "── sent to $target ($pane_id); ledgered"
