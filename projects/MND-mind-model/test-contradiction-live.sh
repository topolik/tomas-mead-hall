#!/usr/bin/env bash
# test-contradiction-live.sh — live-LLM acceptance test for iteration 5B.
#
# A controlled "does the brain change its mind?" experiment: plant a scratch
# brain with ONE deliberate genuine contradiction (an old distilled belief vs a
# newer Tomas correction) and ONE deliberate context-split (two insights that
# look like a conflict but are scope-different), plus two unrelated insights as
# a control. Then run the REAL contradiction sweep and a REAL ask through the
# live LLM and assert the known-correct outcome:
#
#   1. sweep flags the VM/k8s pair as a genuine contradiction
#   2. provenance retires the stale VM belief (feedback+newer k8s wins)
#   3. the Rust/Python pair is NEVER wrongly retired (both stay active) — the
#      anti-over-retirement invariant; if flagged at all it must be context_split
#   4. the two unrelated insights are untouched
#   5. ask "deploy a new prod service?" answers Kubernetes, NOT bare VMs
#      (proving the retired insight no longer leaks into answers)
#   6. ask "language for a quick script?" answers Python, "for a backend?" Rust
#      (proving scope-different insights each apply in their own situation)
#
# Everything runs in data/ctest/ (gitignored) — the real brain is never touched.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
export MND_UID="$(id -u)" MND_GID="$(id -g)"
CT="${SCRIPT_DIR}/data/ctest"
REL="data/ctest"   # path as the mnd container sees it
pass=0 fail=0
ok(){ echo "  ✓ $1"; pass=$((pass+1)); }
no(){ echo "  ✗ $1"; fail=$((fail+1)); }

mnd(){ docker compose -f "${SCRIPT_DIR}/docker-compose.yml" run --rm -T mnd "$@"; }

# LLM via the LLP gateway (model:auto → highest available), prompt-file -> stdout.
llp(){
  local pf="$1" tok body
  tok="$(curl -s --max-time 5 --unix-socket "${HOME}/.llp/control.sock" -X POST http://unix/register \
    -H 'Content-Type: application/json' -d '{"agent":"ctest"}' | jq -r '.token // empty')"
  [[ -n "$tok" ]] || { echo "FATAL: LLP handshake failed (is it up on :4000?)" >&2; exit 1; }
  body="$(mktemp)"
  jq -Rs '{model:"auto",messages:[{role:"user",content:.}]}' < "$pf" > "$body"
  curl -s --max-time 300 -H "Authorization: Bearer $tok" -H 'Content-Type: application/json' \
    --data-binary @"$body" http://localhost:4000/v1/chat/completions \
    | jq -r '.choices[0].message.content // empty'
  rm -f "$body"
}

rm -rf "$CT"; mkdir -p "$CT/profiles"

# --- plant the scratch brain ----------------------------------------------
cat > "$CT/insights.yaml" <<'YAML'
updated: "2026-06-13T00:00:00Z"
insights:
  - id: vm-deploy
    category: tech_preference
    statement: "Deploy all production services directly on bare virtual machines; never use Kubernetes or container orchestration."
    strength: strong
    source: distill
    occurrences: 3
    evidence:
      - {moment: m1, ts: "2025-01-15T10:00:00Z", quote: "just put it on a VM"}
  - id: k8s-deploy
    category: tech_preference
    statement: "Deploy all production services on Kubernetes; do not provision bare virtual machines for services."
    strength: strong
    source: feedback
    occurrences: 1
    evidence:
      - {moment: "dsh:9001", ts: "2026-06-10T09:00:00Z", quote: "we're standardizing on k8s now, stop spinning up VMs"}
  - id: rust-backend
    category: tech_preference
    statement: "Write performance-critical backend services in Rust."
    strength: strong
    source: distill
    occurrences: 4
    evidence:
      - {moment: m2, ts: "2026-03-01T10:00:00Z", quote: "the service should be in rust"}
  - id: python-scripts
    category: tech_preference
    statement: "Write quick automation and glue scripts in Python."
    strength: strong
    source: distill
    occurrences: 5
    evidence:
      - {moment: m3, ts: "2026-03-02T10:00:00Z", quote: "just bang out a python script"}
  - id: ctrl-darkmode
    category: tech_preference
    statement: "Prefer dark-mode user interfaces."
    strength: weak
    source: distill
    occurrences: 1
    evidence:
      - {moment: m4, ts: "2026-02-01T10:00:00Z", quote: "dark mode please"}
  - id: ctrl-commitlen
    category: direction_pattern
    statement: "Keep git commit subject lines under 72 characters."
    strength: moderate
    source: distill
    occurrences: 2
    evidence:
      - {moment: m5, ts: "2026-02-02T10:00:00Z", quote: "shorter commit subjects"}
YAML

for k in decision-making technical-preferences direction-style; do
  echo "Tomas's $k profile is defined entirely by the evidence insights below." > "$CT/profiles/$k.md"
done

echo "── planted scratch brain (6 insights) at $REL/insights.yaml"

# === Phase 1: live contradiction sweep ====================================
echo "── Phase 1: contradiction sweep (live LLM)"
mnd contradiction-prompt --insights "$REL/insights.yaml" --out "$REL/sweep.prompt" >/dev/null
llp "$CT/sweep.prompt" > "$CT/sweep.response"
echo "── LLM verdicts:"; sed -n 's/.*\("verdict"[^}]*\).*/     \1/p' "$CT/sweep.response" | head -5 || true
mnd contradiction-merge --response "$REL/sweep.response" --insights "$REL/insights.yaml" | sed 's/^/     /'

# --- assertions on the resolved scratch brain (in an `if` so set -e tolerates a fail) ---
if python3 - "$CT/insights.yaml" <<'PY'
import sys, yaml
ins = {i['id']: i for i in yaml.safe_load(open(sys.argv[1]))['insights']}
P=F=0
def chk(c,m):
    global P,F
    print(("  ✓ " if c else "  ✗ ")+m);
    P+=1 if c else 0; F+=0 if c else 1

vm, k8s = ins['vm-deploy'], ins['k8s-deploy']
chk(vm.get('status')=='superseded', "genuine contradiction: VM belief retired")
chk(vm.get('superseded_by')=='k8s-deploy', "provenance winner = k8s (feedback + newer beats old distill)")
chk(k8s.get('status','active')=='active', "k8s belief stays active")
# anti-over-retirement: scope-different insights must NEVER be retired
chk(ins['rust-backend'].get('status','active')=='active' and ins['python-scripts'].get('status','active')=='active',
    "Rust AND Python both stay active (no wrong retirement)")
chk(ins['ctrl-darkmode'].get('status','active')=='active' and ins['ctrl-commitlen'].get('status','active')=='active',
    "control: unrelated insights untouched")
# bonus (informational, not a hard fail): note if either got a sharpened context
sc=[i for i in ('rust-backend','python-scripts') if ins[i].get('context')]
print(f"  · (info) context_split scoping applied to: {sc or 'none — LLM judged them already unambiguous'}")
print(f"PHASE1 {P} pass / {F} fail")
sys.exit(1 if F else 0)
PY
then pass=$((pass+5)); else fail=$((fail+1)); fi

# === Phase 2: live ask — does the retirement reach the answers? ===========
echo "── Phase 2: ask (live LLM) over the resolved brain"

ask(){ # question -> answer text (lowercased)
  mnd ask-prompt --brain-dir "$REL" --question "$1" --out "$REL/ask.prompt" >/dev/null
  llp "$CT/ask.prompt" > "$CT/ask.response"
  mnd ask-parse --response "$REL/ask.response" --json | jq -r '.answer' | tr '[:upper:]' '[:lower:]'
}

a1="$(ask "I'm standing up a brand-new production service. Should I put it on a bare VM or on Kubernetes?")"
echo "     Q(deploy): ${a1:0:160}"
# The retired belief leaks only if the answer RECOMMENDS VMs. A VM mention
# inside a "stop/avoid/don't" clause is the winning k8s insight talking — not a
# leak — so require k8s AND, if VMs are named, a negation cue alongside them.
if ! grep -Eq "kubernetes|k8s" <<<"$a1"; then
  no "ask did not recommend Kubernetes — possible VM-belief leak: $a1"
elif grep -Eq "bare vm|virtual machine" <<<"$a1" && ! grep -Eq "stop|avoid|not |n't|no longer|instead|rather than|never|don" <<<"$a1"; then
  no "ask recommends k8s but appears to also endorse VMs: $a1"
else
  ok "ask recommends Kubernetes; the retired VM belief did not leak into the answer"
fi

a2="$(ask "What language should I use for a quick one-off automation script?")"
echo "     Q(script): ${a2:0:160}"
grep -q "python" <<<"$a2" && ok "ask picks Python for quick scripts (context-split)" || no "expected Python: $a2"

a3="$(ask "What language should I write a new performance-critical backend service in?")"
echo "     Q(backend): ${a3:0:160}"
grep -q "rust" <<<"$a3" && ok "ask picks Rust for backend services (context-split)" || no "expected Rust: $a3"

echo "════════════════════════════════════════"
echo "RESULT: $pass passed, $fail failed"
[[ $fail -eq 0 ]] && echo "✅ LIVE TEST PASSED" || { echo "❌ LIVE TEST FAILED"; exit 1; }
