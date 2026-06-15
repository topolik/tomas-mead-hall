package propose

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/topolik/gml-gmail-agent/internal/config"
	"github.com/topolik/gml-gmail-agent/internal/knowledge"
)

var (
	bareQuoted = regexp.MustCompile(`"([A-Za-z0-9_-]+)"`)
	dateValue  = regexp.MustCompile(`^(\d+)([dwmy])$`)
)

// CanonicalFilter strips redundant quotes around single-word tokens. Retained
// for callers that only need that minimal normalization; new dedup code should
// prefer CanonicalQuery.
func CanonicalFilter(s string) string {
	return strings.TrimSpace(bareQuoted.ReplaceAllString(s, "$1"))
}

// CanonicalQuery normalizes a Gmail search/filter string into a canonical form
// for dedup comparison ONLY. It must never be used to rewrite a stored or
// applied filter/link — an over-normalization bug should at worst cause a
// missed or false dedup match, never change what is applied to the mailbox.
//
// It lowercases operator names (operands are left untouched), strips redundant
// quotes around single-word tokens, canonicalizes relative date units to days
// (older_than:1w == older_than:7d), then dedupes and sorts the space-separated
// terms — space means AND, which is commutative, so order is irrelevant.
//
// It deliberately does not parse nested OR / {} / () grouping; the filters this
// system produces are flat. Promote to a real grammar parser if that changes.
func CanonicalQuery(s string) string {
	seen := map[string]bool{}
	var out []string
	for _, t := range splitGmailQuery(strings.TrimSpace(s)) {
		t = canonicalToken(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func canonicalToken(t string) string {
	neg := ""
	if strings.HasPrefix(t, "-") {
		neg = "-"
		t = t[1:]
	}
	t = bareQuoted.ReplaceAllString(t, "$1")
	if i := strings.Index(t, ":"); i > 0 {
		op := strings.ToLower(t[:i])
		val := t[i+1:]
		if op == "older_than" || op == "newer_than" {
			val = canonicalDate(val)
		}
		t = op + ":" + val
	}
	return neg + t
}

func canonicalDate(v string) string {
	m := dateValue.FindStringSubmatch(strings.ToLower(v))
	if m == nil {
		return v
	}
	n, _ := strconv.Atoi(m[1])
	mult := map[string]int{"d": 1, "w": 7, "m": 30, "y": 365}[m[2]]
	return strconv.Itoa(n*mult) + "d"
}

type Proposal struct {
	KnowledgeRef string      `json:"knowledge_ref"`
	Pattern      string      `json:"pattern"`
	Status       string      `json:"status"`
	Constraint   string      `json:"constraint,omitempty"`
	ProposedRule config.Rule `json:"proposed_rule"`
	Reason       string      `json:"reason"`
	// SourceInsights are the DSH insight #IDs this proposal traces back to,
	// copied from the knowledge pattern that produced it (back-tracking provenance).
	SourceInsights []int64 `json:"source_insights,omitempty"`
}

type Result struct {
	Proposals []Proposal `json:"proposals"`
	Skipped   []Skip     `json:"skipped,omitempty"`
}

type Skip struct {
	KnowledgeRef string `json:"knowledge_ref"`
	Pattern      string `json:"pattern"`
	Reason       string `json:"reason"`
}

// ParseProposals parses the JSON array of proposals returned by the semantic
// dedup gate (propose-apply reads this from the LLM via stdin). It tolerates a
// markdown code fence and validates that each kept proposal still carries the
// minimum it needs to be posted as a plan.
func ParseProposals(raw []byte) ([]Proposal, error) {
	var proposals []Proposal
	if err := json.Unmarshal(stripArrayFence(raw), &proposals); err != nil {
		return nil, fmt.Errorf("invalid proposals JSON: %w", err)
	}
	for i, p := range proposals {
		if p.ProposedRule.Name == "" {
			return nil, fmt.Errorf("proposals[%d]: proposed_rule.name is required", i)
		}
		if p.ProposedRule.Type == "archive_by_sender" && len(p.ProposedRule.Params.Patterns) == 0 {
			return nil, fmt.Errorf("proposals[%d] %q: archive_by_sender requires at least one pattern", i, p.ProposedRule.Name)
		}
	}
	return proposals, nil
}

// stripArrayFence removes a surrounding markdown code fence and any preamble
// before the opening '[' of a JSON array. (stripCodeFence in merge.go assumes a
// JSON object and would corrupt an array by trimming to the first '{'.)
func stripArrayFence(raw []byte) []byte {
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
	}
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "["); idx > 0 {
		s = s[idx:]
	}
	return []byte(s)
}

func Generate(kf *knowledge.KnowledgeFile, cfg *config.Config) *Result {
	result := &Result{Proposals: []Proposal{}}
	existing := existingPatterns(cfg)

	for _, p := range kf.Patterns {
		if p.Status == "rejected" {
			result.Skipped = append(result.Skipped, Skip{
				KnowledgeRef: p.GmailSearch,
				Pattern:      p.Pattern,
				Reason:       "rejected by user",
			})
			continue
		}

		if p.Category != "archive_candidate" {
			result.Skipped = append(result.Skipped, Skip{
				KnowledgeRef: p.GmailSearch,
				Pattern:      p.Pattern,
				Reason:       fmt.Sprintf("category %q not actionable yet", p.Category),
			})
			continue
		}

		senders := extractSenders(p)
		if len(senders) == 0 {
			result.Skipped = append(result.Skipped, Skip{
				KnowledgeRef: p.GmailSearch,
				Pattern:      p.Pattern,
				Reason:       "no sender pattern extractable",
			})
			continue
		}

		filter := CanonicalFilter(resolveFilter(p))

		var newSenders []string
		for _, s := range senders {
			if !existing[strings.ToLower(s)] {
				newSenders = append(newSenders, s)
			}
		}
		if len(newSenders) == 0 {
			if filter == "" && !p.RequireReply {
				result.Skipped = append(result.Skipped, Skip{
					KnowledgeRef: p.GmailSearch,
					Pattern:      p.Pattern,
					Reason:       "rule already exists for all senders",
				})
				continue
			}
			newSenders = senders
		}
		if p.Status == "refined" && p.RefinedAction != "" && filter == "" && !p.RequireReply {
			result.Skipped = append(result.Skipped, Skip{
				KnowledgeRef: p.GmailSearch,
				Pattern:      p.Pattern,
				Reason:       "refinement not expressible as Gmail filter",
			})
			continue
		}

		ruleName := buildRuleName(p)
		reason := fmt.Sprintf("knowledge pattern %q (%s)", p.Pattern, p.Status)
		if p.CommentSummary != "" {
			reason += " — " + p.CommentSummary
		}

		proposal := Proposal{
			KnowledgeRef: p.GmailSearch,
			Pattern:      p.Pattern,
			Status:       p.Status,
			Constraint:   p.RefinedAction,
			ProposedRule: config.Rule{
				Name: ruleName,
				Type: "archive_by_sender",
				Params: config.RuleParams{
					Patterns:     newSenders,
					Filter:       filter,
					RequireReply: p.RequireReply,
				},
			},
			Reason:         reason,
			SourceInsights: p.SourceInsights,
		}
		result.Proposals = append(result.Proposals, proposal)
	}

	return result
}

func resolveFilter(p knowledge.Pattern) string {
	if p.Filter != "" {
		return p.Filter
	}
	return extractFilter(p)
}

// extractFilter derives the content-filter portion of a knowledge pattern's
// gmail_search by dropping the sender expression — the senders are already
// captured in the rule's patterns. It works at the group level so that a
// multi-sender grouping like "{from:a from:b}" or "from:a OR from:b" is
// discarded entirely (yielding an empty filter), while a genuine content group
// such as {subject:"X" subject:"Y"} is preserved. Leaving the grouping glue
// behind is what produced the bogus filters "OR" and "{from:..." (plans #98/#100).
func extractFilter(p knowledge.Pattern) string {
	s := strings.TrimSpace(p.GmailSearch)
	if s == "" {
		return ""
	}
	var parts []string
	for _, unit := range splitTopLevel(s) {
		up := strings.ToUpper(unit)
		if up == "OR" || up == "AND" {
			continue // bare boolean connector (only joined senders)
		}
		if isSenderOnly(unit) {
			continue // a from: token, or a group containing only senders
		}
		parts = append(parts, unit)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// splitTopLevel splits a Gmail query on spaces while keeping {...}/(...) groups
// and "..." quoted phrases as single units.
func splitTopLevel(q string) []string {
	var tokens []string
	var cur strings.Builder
	depth := 0
	inQuote := false
	for _, r := range q {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case !inQuote && (r == '{' || r == '('):
			depth++
			cur.WriteRune(r)
		case !inQuote && (r == '}' || r == ')'):
			if depth > 0 {
				depth--
			}
			cur.WriteRune(r)
		case !inQuote && depth == 0 && r == ' ':
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// isSenderOnly reports whether a top-level unit consists solely of from: tokens
// and boolean connectors (optionally wrapped in one layer of {} or ()).
func isSenderOnly(unit string) bool {
	u := strings.TrimSpace(unit)
	if len(u) >= 2 && ((u[0] == '{' && u[len(u)-1] == '}') || (u[0] == '(' && u[len(u)-1] == ')')) {
		u = u[1 : len(u)-1]
	}
	inner := splitGmailQuery(u)
	if len(inner) == 0 {
		return false
	}
	for _, t := range inner {
		up := strings.ToUpper(t)
		if up == "OR" || up == "AND" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(t), "from:") {
			continue
		}
		return false
	}
	return true
}

func splitGmailQuery(q string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	for _, r := range q {
		if r == '"' {
			inQuote = !inQuote
			current.WriteRune(r)
		} else if !inQuote && r == ' ' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func extractSenders(p knowledge.Pattern) []string {
	if len(p.Senders) > 0 {
		return p.Senders
	}
	s := p.GmailSearch
	if strings.HasPrefix(s, "from:") {
		addr := strings.TrimPrefix(s, "from:")
		addr = strings.TrimSpace(addr)
		if addr != "" {
			return []string{addr}
		}
	}
	return nil
}

func existingPatterns(cfg *config.Config) map[string]bool {
	m := make(map[string]bool)
	for _, r := range cfg.Rules {
		if r.Type == "archive_by_sender" {
			for _, p := range r.Params.Patterns {
				m[strings.ToLower(p)] = true
			}
		}
	}
	return m
}

type AnnotatedRule struct {
	Rule       config.Rule
	Pattern    string  // human-readable knowledge pattern description
	Constraint string  // advisory constraint (not enforced by rule type)
	PlanIDs    []int64 // DSH plan IDs that produced this rule
	InsightIDs []int64 // DSH insight #IDs this rule traces back to (provenance)
}

// SameSenderConflicts detects the OR-union footgun: two or more
// archive_by_sender rules targeting the SAME sender with DIFFERENT filters.
// Such rules apply independently and union at the mailbox, so e.g.
// "-Critical" plus "-VIP" archives everything that is not-Critical OR not-VIP —
// i.e. essentially all mail. The only safe shape is a single rule per sender.
//
// It returns each offending sender (lowercased) mapped to the distinct raw
// filters seen for it. Senders with a single (canonical) filter are not
// reported. Filters are compared via CanonicalQuery so quoting/order/date-unit
// variants count as the same filter; require_reply is part of the distinctness
// key because a reply-gated rule archives a different set than an ungated one.
// This is pure detection — it never rewrites or folds; folding is the LLM/human's
// job at propose time.
func SameSenderConflicts(rules []config.Rule) map[string][]string {
	type variant struct {
		canon string
		raw   string
	}
	bySender := map[string][]variant{}
	for _, r := range rules {
		if r.Type != "archive_by_sender" {
			continue
		}
		canon := CanonicalQuery(r.Params.Filter)
		key := canon
		if r.Params.RequireReply {
			key += "\x00rr"
		}
		raw := r.Params.Filter
		if raw == "" {
			raw = "(no filter — archives all)"
		}
		if r.Params.RequireReply {
			raw += " [require_reply]"
		}
		for _, p := range r.Params.Patterns {
			s := strings.ToLower(strings.TrimSpace(p))
			if s == "" {
				continue
			}
			seen := false
			for _, v := range bySender[s] {
				if v.canon == key {
					seen = true
					break
				}
			}
			if !seen {
				bySender[s] = append(bySender[s], variant{canon: key, raw: raw})
			}
		}
	}

	conflicts := map[string][]string{}
	for s, vs := range bySender {
		if len(vs) < 2 {
			continue
		}
		filters := make([]string, len(vs))
		for i, v := range vs {
			filters[i] = v.raw
		}
		sort.Strings(filters)
		conflicts[s] = filters
	}
	return conflicts
}

// GuardSameSender withholds the OR-union footgun from rules.yaml. Any sender
// flagged by SameSenderConflicts is stripped from EVERY rule's patterns (so it
// receives no archiving rule until it is folded into one), and archive_by_sender
// rules left with no patterns are dropped. Other senders sharing those rules are
// preserved. Returns the safe rule set plus the report of withheld senders →
// distinct filters, which the caller logs.
func GuardSameSender(rules []AnnotatedRule) (safe []AnnotatedRule, withheld map[string][]string) {
	crs := make([]config.Rule, len(rules))
	for i, ar := range rules {
		crs[i] = ar.Rule
	}
	withheld = SameSenderConflicts(crs)
	if len(withheld) == 0 {
		return rules, withheld
	}
	for _, ar := range rules {
		if ar.Rule.Type == "archive_by_sender" {
			var kept []string
			for _, p := range ar.Rule.Params.Patterns {
				if _, bad := withheld[strings.ToLower(strings.TrimSpace(p))]; !bad {
					kept = append(kept, p)
				}
			}
			if len(kept) == 0 {
				continue // whole rule withheld
			}
			ar.Rule.Params.Patterns = kept
		}
		safe = append(safe, ar)
	}
	return safe, withheld
}

func BuildGeneratedRules(existing string, rules []AnnotatedRule) string {
	const marker = "# === gml-generated rules ==="
	const endMarker = "# === end gml-generated rules ==="

	content := existing
	if startIdx := strings.Index(content, marker); startIdx >= 0 {
		endIdx := strings.Index(content, endMarker)
		if endIdx >= 0 {
			after := content[endIdx+len(endMarker):]
			after = strings.TrimLeft(after, "\n")
			content = strings.TrimRight(content[:startIdx], "\n") + "\n\n" + after
		} else {
			content = strings.TrimRight(content[:startIdx], "\n") + "\n"
		}
	}

	merged := mergeAnnotatedRules(rules)

	var block strings.Builder
	block.WriteString("\n" + marker + "\n")
	for _, m := range merged {
		block.WriteString("  #\n")
		if len(m.planIDs) > 0 {
			ids := make([]string, len(m.planIDs))
			for i, id := range m.planIDs {
				ids[i] = fmt.Sprintf("#%d", id)
			}
			block.WriteString(fmt.Sprintf("  # plan %s\n", strings.Join(ids, ", ")))
		}
		if len(m.insightIDs) > 0 {
			ids := make([]string, len(m.insightIDs))
			for i, id := range m.insightIDs {
				ids[i] = fmt.Sprintf("#%d", id)
			}
			block.WriteString(fmt.Sprintf("  # insights %s\n", strings.Join(ids, ", ")))
		}
		for _, desc := range m.descriptions {
			block.WriteString(fmt.Sprintf("  # %s\n", desc))
		}
		for _, c := range m.constraints {
			block.WriteString(fmt.Sprintf("  # NOTE: %s\n", c))
		}
		block.WriteString(fmt.Sprintf("  - name: %q\n", m.rule.Name))
		block.WriteString(fmt.Sprintf("    type: %s\n", m.rule.Type))
		block.WriteString("    params:\n")
		switch m.rule.Type {
		case "archive_by_sender":
			block.WriteString("      patterns:\n")
			for _, p := range m.rule.Params.Patterns {
				block.WriteString(fmt.Sprintf("        - %q\n", p))
			}
			if m.rule.Params.Filter != "" {
				block.WriteString(fmt.Sprintf("      filter: %q\n", m.rule.Params.Filter))
			}
			if m.rule.Params.RequireReply {
				block.WriteString("      require_reply: true\n")
			}
		case "archive_by_age":
			block.WriteString(fmt.Sprintf("      days: %d\n", m.rule.Params.Days))
			if m.rule.Params.State != "" {
				block.WriteString(fmt.Sprintf("      state: %s\n", m.rule.Params.State))
			}
		case "archive_by_label":
			block.WriteString(fmt.Sprintf("      label: %q\n", m.rule.Params.Label))
		}
	}
	block.WriteString(endMarker + "\n")

	if idx := strings.Index(content, "\nanalysis:"); idx >= 0 {
		return content[:idx] + "\n" + block.String() + content[idx:]
	}
	return content + block.String()
}

type mergedRule struct {
	rule         config.Rule
	descriptions []string
	constraints  []string
	planIDs      []int64
	insightIDs   []int64
}

func mergeAnnotatedRules(rules []AnnotatedRule) []mergedRule {
	type key struct {
		name         string
		typ          string
		filter       string
		requireReply bool
	}
	order := []key{}
	byKey := map[key]*mergedRule{}

	for _, ar := range rules {
		k := key{ar.Rule.Name, ar.Rule.Type, ar.Rule.Params.Filter, ar.Rule.Params.RequireReply}
		m, ok := byKey[k]
		if !ok {
			m = &mergedRule{rule: ar.Rule}
			byKey[k] = m
			order = append(order, k)
		} else if ar.Rule.Type == "archive_by_sender" {
			seen := map[string]bool{}
			for _, p := range m.rule.Params.Patterns {
				seen[strings.ToLower(p)] = true
			}
			for _, p := range ar.Rule.Params.Patterns {
				if !seen[strings.ToLower(p)] {
					m.rule.Params.Patterns = append(m.rule.Params.Patterns, p)
				}
			}
		}
		m.planIDs = append(m.planIDs, ar.PlanIDs...)
		m.insightIDs = unionInsightIDs(m.insightIDs, ar.InsightIDs)
		if ar.Pattern != "" {
			m.descriptions = append(m.descriptions, ar.Pattern)
		}
		if ar.Constraint != "" {
			m.constraints = append(m.constraints, ar.Constraint)
		}
	}

	var result []mergedRule
	for _, k := range order {
		result = append(result, *byKey[k])
	}
	sort.Slice(result, func(i, j int) bool {
		mi, mj := minPlanID(result[i].planIDs), minPlanID(result[j].planIDs)
		return mi < mj
	})
	return result
}

func minPlanID(ids []int64) int64 {
	if len(ids) == 0 {
		return math.MaxInt64
	}
	m := ids[0]
	for _, id := range ids[1:] {
		if id < m {
			m = id
		}
	}
	return m
}

// unionInsightIDs merges insight-ID lists, deduped and ascending, so a merged
// rule carries the provenance of every plan folded into it.
func unionInsightIDs(a, b []int64) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, group := range [][]int64{a, b} {
		for _, id := range group {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func buildRuleName(p knowledge.Pattern) string {
	if len(p.Senders) == 1 {
		s := p.Senders[0]
		if at := strings.Index(s, "@"); at > 0 {
			domain := s[at+1:]
			parts := strings.Split(domain, ".")
			if len(parts) >= 2 {
				return parts[len(parts)-2] + " notifications"
			}
		}
		return s
	}
	words := strings.Fields(strings.ToLower(p.Pattern))
	if len(words) > 3 {
		words = words[:3]
	}
	return strings.Join(words, " ")
}
