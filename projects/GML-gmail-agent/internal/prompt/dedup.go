package prompt

import (
	"strings"

	"github.com/topolik/gml-gmail-agent/internal/notify"
)

func BuildDedup(analysisJSON string, dismissed []notify.PreviousNotification) string {
	var sb strings.Builder

	sb.WriteString(`You are a strict deduplication filter. Your job is to REMOVE concerns that the user already dismissed.

TASK: For each concern in NEW ANALYSIS, check if it matches ANY dismissed notification. If it matches, REMOVE it. When in doubt, REMOVE.

WHAT COUNTS AS A MATCH:
- Same sender (from: field)
- Same topic or subject matter (even if worded differently)
- Same type of alert/notification (e.g., "Grafana ETD alert" matches "Grafana ETD Bucket alerts")
- Same vulnerability or security item (e.g., "BIRD vulnerability" matches "BIRD/BIRD2 stack buffer overflow")
- "New replies", "update", "thread update", "verdict changed" on a dismissed topic is STILL a match — the user dismissed the TOPIC, not just one message

WHAT IS NOT A MATCH:
- Completely different sender AND completely different topic
- A fundamentally different security incident (not just rewording of the same one)

OUTPUT FORMAT:
Output ONLY a JSON array containing the concerns to KEEP (the ones that do NOT match any dismissed notification).
- Same schema as the input — copy kept concerns exactly, do not modify them.
- If all concerns match dismissed items, output: []
- Output valid JSON only. No markdown, no explanations, no preamble.

`)

	sb.WriteString("DISMISSED NOTIFICATIONS:\n\n")
	sb.WriteString("<dismissed_notifications>\n")
	sb.WriteString(notify.FormatDismissedNotifications(dismissed))
	sb.WriteString("</dismissed_notifications>\n\n")

	sb.WriteString("NEW ANALYSIS (remove matches, keep only genuinely new concerns):\n\n")
	sb.WriteString("<analysis>\n")
	sb.WriteString(analysisJSON)
	sb.WriteString("\n</analysis>\n")

	return sb.String()
}

// BuildInsightDedup is the learn-path counterpart to BuildDedup. The strict
// BuildDedup removes anything touching a dismissed topic ("when in doubt,
// REMOVE") — right for one-off security concerns, wrong for behavioral insights
// where the user WANTS to be re-surfaced when something genuinely changed about
// a sender they already addressed. This variant keeps a candidate that adds a
// new observation/refinement even on a dismissed topic, and marks it as an
// update so the re-posted insight reads as a clarification rather than a repeat.
func BuildInsightDedup(analysisJSON string, dismissed []notify.PreviousNotification) string {
	var sb strings.Builder

	sb.WriteString(`You are a deduplication filter for behavioral email INSIGHTS. The user has already SEEN and DISMISSED the insights below (often with a comment saying how they handled them). Your job: drop insights that merely REPEAT a dismissed one, but KEEP insights that genuinely ADD something the user has not yet seen.

TASK: For each insight in NEW INSIGHTS, compare it to the DISMISSED INSIGHTS.

DROP an insight when it is a REWORDED DUPLICATE of a dismissed one:
- same sender(s) AND same topic/behavior, with no new information — only a different gmail_search wording, a reordered query, or an added/removed time window.

KEEP an insight when it adds GENUINELY NEW information about a dismissed sender/topic:
- a new sub-pattern, a materially different behavior, a changed signal strength, a new sender joining the group, or a refinement the dismissed insight did not capture.
- When you KEEP such an insight (one that overlaps a dismissed sender/topic but adds something new), PREFIX its "pattern" field with "Update: " so the re-posted insight reads as a clarification of what changed. Do not modify any other field.

KEEP unchanged any insight whose sender(s) AND topic do not appear in the dismissed list at all (copy it exactly).

When the only difference is wording → DROP. When there is real new substance → KEEP (prefixed). If unsure whether something is new, prefer KEEP (the user explicitly wants to be told when something changed).

OUTPUT FORMAT:
Output ONLY a JSON array of the insights to keep, in the SAME schema as the input (fields: pattern, evidence, signal_strength, category, affected_senders, suggested_action, gmail_search). Apply the "Update: " prefix only to the pattern field of kept-but-overlapping insights. If every insight is a pure duplicate, output: []. Output valid JSON only — no markdown, no explanations, no preamble.

`)

	sb.WriteString("DISMISSED INSIGHTS (already seen by the user):\n\n")
	sb.WriteString("<dismissed_insights>\n")
	sb.WriteString(notify.FormatDismissedNotifications(dismissed))
	sb.WriteString("</dismissed_insights>\n\n")

	sb.WriteString("NEW INSIGHTS (drop pure rewordings, keep genuinely new — prefix overlapping-but-new with \"Update: \"):\n\n")
	sb.WriteString("<insights>\n")
	sb.WriteString(analysisJSON)
	sb.WriteString("\n</insights>\n")

	return sb.String()
}
