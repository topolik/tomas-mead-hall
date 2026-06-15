package prompt

import "strings"

// BuildProposeReconcile builds the prompt for the reconcile gate that runs
// between propose-gather and propose-apply. It receives CANDIDATE proposals
// (already past the deterministic structural dedup), a readable summary of the
// EXISTING plans tracked in DSH (with their numeric ids), and the EXISTING
// applied rules from rules.yaml.
//
// Its job enforces a hard invariant: **at most ONE archive_by_sender rule per
// sender.** Two archive rules for the same sender apply independently and UNION
// at the mailbox, so e.g. "-Critical" plus "-VIP" archives everything that is
// not-Critical OR not-VIP — essentially all mail. So instead of merely dropping
// duplicates (the old behavior), the gate must FOLD a candidate that overlaps an
// existing rule/plan for the same sender into a single combined rule that
// supersedes the existing plan(s).
//
// Output contract: a JSON array of proposals to POST, same schema as the input.
// The LLM copies genuinely-new candidates verbatim, drops duplicates, and for a
// folding case outputs ONE combined proposal (folded filter + a knowledge_ref of
// "merge_conflict:[<ids>]" listing the existing plan ids it replaces). Direction
// is fold/keep-on-doubt: every output plan is reviewed by a human before it is
// applied, and the deterministic same-sender guard at apply is the backstop, so
// the gate should never knowingly emit two rules for one sender.
func BuildProposeReconcile(candidatesJSON, existingPlansText, existingRulesText string) string {
	var sb strings.Builder

	sb.WriteString(`You are a reconcile gate for proposed email-archive rules.

HARD INVARIANT: at most ONE archive_by_sender rule per sender address. Two archive rules for the same sender apply independently and UNION at the mailbox — e.g. a "-Critical" rule plus a "-VIP" rule archives every email that is "not Critical" OR "not VIP", i.e. essentially everything. You must never produce a second parallel rule for a sender that is already covered.

You receive CANDIDATE proposals, the EXISTING plans already tracked (each with a numeric id and status), and the EXISTING applied rules. A deterministic pass already removed exact duplicates. For EACH candidate, decide one of:

1. NEW — its sender is not covered by any existing plan or rule. Copy the candidate to the output UNCHANGED.

2. DUPLICATE — an existing plan/rule already targets that sender with the same effect (same filter, possibly worded differently). DROP it (omit from output).

3. FOLD — its sender is already covered, but the candidate adds a genuinely different constraint. Output exactly ONE combined proposal for that sender, and DROP the raw candidate. Combine the constraints into the single safest filter:
   - Exclusions (negative terms) AND together: existing "-Critical" + candidate "-VIP" => "-Critical -VIP" (archive socradar unless Critical OR VIP). This archives LESS — safe.
   - Positive subject filters OR together: existing subject:"A" + candidate subject:"B" => {subject:"A" subject:"B"}.
   - Mixed/ambiguous (a positive include vs a negative exclude): choose the MOST RESTRICTIVE result that archives the least, and keep it for human review.
   On the folded proposal set "knowledge_ref" to "merge_conflict:[<ids>]" listing the numeric ids of every EXISTING PLAN it replaces (space-separated, e.g. "merge_conflict:[57 68]"). Put the combined filter in proposed_rule.params.filter and the single sender in proposed_rule.params.patterns. If the sender is only covered by an applied rule that has no plan id, still fold but you may have no ids to list ("merge_conflict:[]").

RULES:
- NEVER output two proposals for the same sender — fold them into one.
- When unsure whether something is a real new constraint, prefer FOLD over a parallel rule; never widen what gets archived.
- Copy all fields you are not deliberately changing exactly as given.

OUTPUT FORMAT:
Output ONLY a JSON array of the proposals to post (NEW copied verbatim, FOLD combined). Same schema as the input candidates.
- If nothing should be posted, output: []
- Output valid JSON only. No markdown, no explanations, no preamble.

`)

	sb.WriteString("EXISTING PLANS (numeric id + sender + filter — fold against these and cite their ids):\n\n")
	sb.WriteString("<existing_plans>\n")
	if strings.TrimSpace(existingPlansText) != "" {
		sb.WriteString(existingPlansText)
	} else {
		sb.WriteString("No existing plans.\n")
	}
	sb.WriteString("\n</existing_plans>\n\n")

	sb.WriteString("EXISTING APPLIED RULES (current rules.yaml — the live archiving reality):\n\n")
	sb.WriteString("<existing_rules>\n")
	if strings.TrimSpace(existingRulesText) != "" {
		sb.WriteString(existingRulesText)
	} else {
		sb.WriteString("No applied rules.\n")
	}
	sb.WriteString("\n</existing_rules>\n\n")

	sb.WriteString("CANDIDATE PROPOSALS (reconcile each: NEW / DUPLICATE / FOLD):\n\n")
	sb.WriteString("<candidates>\n")
	sb.WriteString(candidatesJSON)
	sb.WriteString("\n</candidates>\n")

	return sb.String()
}
