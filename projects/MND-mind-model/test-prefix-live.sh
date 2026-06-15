#!/usr/bin/env bash
# test-prefix-live.sh — live acceptance test for the [MND orchestrator]
# attribution prefix (iteration 5, MND-024). Two independent live proofs:
#
#   Phase 1 (real herdr agent + real LLM): spin a scratch agent that asks a
#   question, let orchestrate.sh answer it for real, and confirm the direction
#   that lands in the agent's pane is prefixed "[MND orchestrator]" and was
#   ledgered. This is the visible-attribution round-trip.
#
#   Phase 2 (real mnd extract binary): the payoff — a prefixed direction sitting
#   in a session as a "user" turn must be DROPPED from retraining (self-
#   exclusion), while a normal Tomas turn beside it survives. Proves the brain
#   can never relearn its own delivered directions.
#
# Scratch only: a throwaway worktree (removed after) and data/ptest/ (gitignored).
# The real sent-ledger is snapshotted and restored — the test send does not persist.
set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
export MND_UID="$(id -u)" MND_GID="$(id -g)"
PARENT="${PARENT:-$(cd "$SCRIPT_DIR/../.." && pwd)}"
LEDGER="${SCRIPT_DIR}/data/sent-ledger.jsonl"
PT="${SCRIPT_DIR}/data/ptest"; REL="data/ptest"
pass=0 fail=0
ok(){ echo "  ✓ $1"; pass=$((pass+1)); }
no(){ echo "  ✗ $1"; fail=$((fail+1)); }
mnd(){ docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T mnd "$@"; }

WS=""   # scratch workspace id, for cleanup
cleanup(){
  [[ -n "$WS" ]] && herdr worktree remove --workspace "$WS" --force --json >/dev/null 2>&1
  git -C "$PARENT" branch -D prefix-live-test >/dev/null 2>&1 || true
  [[ -f "${LEDGER}.snap" ]] && mv "${LEDGER}.snap" "$LEDGER"   # restore real ledger
  rm -rf "$PT"
}
trap cleanup EXIT

# =========================================================================
echo "═══ Phase 1: prefixed delivery into a real herdr agent (live LLM) ═══"
cp "$LEDGER" "${LEDGER}.snap" 2>/dev/null || : > "${LEDGER}.snap"
before=$(wc -l < "$LEDGER" 2>/dev/null || echo 0)

CREATE="$(herdr worktree create --cwd "$PARENT" --branch prefix-live-test --label "MND prefix live test" --no-focus --json 2>/dev/null)"
WS="$(jq -r '.result.workspace.workspace_id // .result.root_pane.workspace_id' <<<"$CREATE")"
PANE="$(jq -r '.result.root_pane.pane_id' <<<"$CREATE")"
echo "  scratch agent pane: $PANE (workspace $WS)"
[[ -n "$PANE" && "$PANE" != null ]] || { no "could not create scratch agent"; exit 1; }

herdr pane send-text "$PANE" 'claude "You are a test agent. Ask me exactly one question: should the new service store config in YAML or JSON? Ask it, then wait. When I answer, reply: Acknowledged. and stop."'
herdr pane send-keys "$PANE" Enter
echo "  waiting for the agent to ask + go idle..."
herdr wait agent-status "$PANE" --status idle --timeout 150000 >/dev/null 2>&1 || true

echo "  orchestrating (real LLM, real --send)..."
"${SCRIPT_DIR}/orchestrate.sh" "$PANE" --send >/dev/null 2>&1 || true

# Same normalization orchestrate.sh uses to write the ledger (== exclude.NormHash).
norm_hash(){ tr '[:upper:]' '[:lower:]' | tr -s '[:space:]' ' ' | sed 's/^ //;s/ $//' | sha256sum | cut -d' ' -f1; }

after=$(wc -l < "$LEDGER" 2>/dev/null || echo 0)
ans="$(mnd ask-parse --response data/ask.response --json 2>/dev/null | jq -r '.answer' 2>/dev/null || true)"
if [[ "$after" -le "$before" ]]; then
  no "no send happened (ledger unchanged) — LLM low-confidence/escalated; rerun (gemini may be cooling)"
elif [[ -z "$ans" || "$ans" == null ]]; then
  no "could not recover the delivered answer from $REL/ask.response"
else
  # The ledger hash is written AFTER `herdr pane send-text "$sent"`, so its
  # existence proves the send ran; matching it to the PREFIXED answer proves
  # the prefix was what got delivered (not the bare answer).
  exp="$(printf '%s' "[MND orchestrator] ${ans}" | norm_hash)"
  bare="$(printf '%s' "${ans}" | norm_hash)"
  last="$(tail -1 "$LEDGER" | jq -r '.hash' 2>/dev/null)"
  ltarget="$(tail -1 "$LEDGER" | jq -r '.target' 2>/dev/null)"
  echo "     delivered: [MND orchestrator] ${ans:0:80}…"
  if [[ "$last" == "$exp" && "$ltarget" == "$PANE" ]]; then
    ok "the direction delivered into the pane was prefixed [MND orchestrator] and ledgered (hash matches the PREFIXED text)"
  elif [[ "$last" == "$bare" ]]; then
    no "delivered text was NOT prefixed — ledger hash matches the bare answer"
  else
    no "ledger hash ($last) matches neither prefixed nor bare answer for $PANE"
  fi
fi
# informational: show the agent's own reaction (best-effort; TUI may have redrawn)
herdr wait agent-status "$PANE" --status idle --timeout 90000 >/dev/null 2>&1 || true
react="$(herdr pane read "$PANE" --source recent-unwrapped --lines 200 2>/dev/null | grep -iF "acknowledg" | head -1 || true)"
[[ -n "$react" ]] && echo "     agent reacted: ${react:0:80}"

# =========================================================================
echo "═══ Phase 2: self-exclusion through the real 'mnd extract' binary ═══"
mkdir -p "$PT/claude/scratchproj"
cat > "$PT/claude/scratchproj/sess.jsonl" <<'JSONL'
{"type":"user","userType":"external","timestamp":"2026-06-13T20:55:00Z","sessionId":"ptest-sess","message":{"role":"user","content":"Use Postgres for the new service, not SQLite."}}
{"type":"user","userType":"external","timestamp":"2026-06-13T20:56:00Z","sessionId":"ptest-sess","message":{"role":"user","content":"[MND orchestrator] Use flat files. KISS — no DB until the data outgrows it."}}
JSONL

: > "$PT/empty-ledger.jsonl"
STATS="$(mnd extract --claude-dir "$REL/claude" --gemini-dir "" --out "$REL/moments.jsonl" --ledger "$REL/empty-ledger.jsonl" 2>&1 || true)"
echo "$STATS" | grep -iE "dropped|kept|moment" | sed 's/^/     /' | tail -4

if grep -qF "Postgres" "$PT/moments.jsonl" 2>/dev/null; then
  ok "a normal Tomas turn survives extraction"
else
  no "the normal Tomas turn was lost (extraction issue)"
fi
if grep -qF "[MND orchestrator]" "$PT/moments.jsonl" 2>/dev/null; then
  no "LEAK: the prefixed direction reached the moments corpus — retraining could relearn it"
else
  ok "the prefixed direction was DROPPED — retraining can never relearn the brain's own output"
fi

echo "════════════════════════════════════════"
echo "RESULT: $pass passed, $fail failed"
[[ $fail -eq 0 ]] && echo "✅ LIVE TEST PASSED" || { echo "❌ LIVE TEST FAILED"; exit 1; }
