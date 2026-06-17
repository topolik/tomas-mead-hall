package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/topolik/gml-gmail-agent/internal/behavior"
	"github.com/topolik/gml-gmail-agent/internal/config"
	"github.com/topolik/gml-gmail-agent/internal/creds"
	"github.com/topolik/gml-gmail-agent/internal/fetch"
	"github.com/topolik/gml-gmail-agent/internal/knowledge"
	"github.com/topolik/gml-gmail-agent/internal/llm"
	"github.com/topolik/gml-gmail-agent/internal/notify"
	"github.com/topolik/gml-gmail-agent/internal/prompt"
	"github.com/topolik/gml-gmail-agent/internal/propose"
	"github.com/topolik/gml-gmail-agent/internal/rules"
)

func logf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func pipelineLoadCreds() *creds.Creds {
	logf("fetching credentials from 1Password (one-time)...")
	cr, err := creds.LoadFromOP(creds.OPItemReadOnly, creds.OPField)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	logf("credentials loaded")
	return cr
}

func knowledgePath() string { return "data/knowledge.yaml" }
func dataDir() string       { return "data" }
func rulesYAMLPath() string {
	if p := os.Getenv("GML_RULES"); p != "" {
		return p
	}
	return "data/rules.yaml"
}

// --- dedup helper (shared by analyze + learn) ---

func pipelineDedup(lc *llm.Client, cfg *config.Config, model, analysisJSON string,
	buildFn func(string, []notify.PreviousNotification) string) (string, error) {

	analysisJSON = strings.TrimSpace(analysisJSON)
	if analysisJSON == "" || analysisJSON == "[]" {
		return "[]", nil
	}

	if err := cfg.Analysis.Validate(); err != nil {
		return analysisJSON, nil
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)
	all, err := dsh.GetDismissedNotifications("GML", 200, false)
	if err != nil {
		logf("warning: could not fetch dismissed, passing through: %v", err)
		return analysisJSON, nil
	}

	var dismissed []notify.PreviousNotification
	for _, n := range all {
		if n.DismissedAt != "" {
			dismissed = append(dismissed, n)
		}
	}

	if len(dismissed) == 0 {
		logf("  no dismissed notifications — skipping LLM dedup")
		return analysisJSON, nil
	}

	logf("  building dedup prompt with %d dismissed notifications", len(dismissed))
	dedupPrompt := buildFn(analysisJSON, dismissed)

	result, err := lc.Call(model, dedupPrompt)
	if err != nil {
		return "", err
	}
	logf("  dedup filtering applied")
	return result, nil
}

// --- notify helper (post analysis results to DSH) ---

func pipelineNotify(cfg *config.Config, analysisJSON string) error {
	results, err := notify.ParseAndValidate([]byte(analysisJSON))
	if err != nil {
		return fmt.Errorf("validation: %w", err)
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)
	notifs := notify.ToNotifications(results)

	existingLinks := make(map[string]bool)
	existing, err := dsh.GetDismissedNotifications("GML", 200, false)
	if err != nil {
		logf("warning: could not fetch existing notifications for dedup: %v", err)
	} else {
		for _, n := range existing {
			if n.Link != "" {
				existingLinks[n.Link] = true
			}
		}
	}

	posted, skipped := 0, 0
	for _, n := range notifs {
		if n.Link != "" && existingLinks[n.Link] {
			skipped++
			logf("  skipped (duplicate): %s", n.Message)
			continue
		}
		if err := dsh.PostNotification(n); err != nil {
			return fmt.Errorf("posting notification: %w", err)
		}
		posted++
		logf("  posted: [%s] %s", n.Type, n.Message)
	}
	logf("done: %d notifications posted, %d skipped (duplicate)", posted, skipped)
	return nil
}

// --- insights helper (post insight results to DSH) ---

func pipelineInsights(cfg *config.Config, analysisJSON string) error {
	results, err := notify.ParseAndValidateInsights([]byte(analysisJSON))
	if err != nil {
		return fmt.Errorf("insight validation: %w", err)
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)

	existing, err := dsh.GetDismissedNotifications("GML", 100, false)
	if err != nil {
		logf("warning: could not fetch existing notifications for dedup: %v", err)
	}

	posts, updates, skips := notify.ClassifyInsights(results, existing)

	for _, c := range skips {
		logf("  skipped (already seen/dismissed): %s", c.Pattern)
	}

	updated := 0
	for _, u := range updates {
		n := notify.InsightToNotification(u.Candidate)
		if err := dsh.UpdateNotification(u.ID, n.Message, n.Link, n.Priority); err != nil {
			logf("  update #%d failed (%v) — posting as new", u.ID, err)
			posts = append(posts, u.Candidate)
			continue
		}
		updated++
		logf("  updated #%d in place: %s", u.ID, n.Message)
	}

	posted := 0
	for _, c := range posts {
		n := notify.InsightToNotification(c)
		if err := dsh.PostNotification(n); err != nil {
			return fmt.Errorf("posting insight: %w", err)
		}
		posted++
		logf("  posted: [%s] %s", n.Type, n.Message)
	}
	logf("done: %d posted, %d updated in place, %d skipped (duplicate)", posted, updated, len(skips))
	return nil
}

// ==========================================================================
// Pipeline commands
// ==========================================================================

// cmdPipelineAnalyze: fetch → LLM → dedup → LLM → notify
func cmdPipelineAnalyze(lc *llm.Client, cr *creds.Creds, cfg *config.Config, args []string) {
	model := stringFlag(args, "--model", "gemini")
	days := intFlag(args, "--days", 0)
	hours := intFlag(args, "--hours", 0)
	minutes := intFlag(args, "--minutes", 0)

	flagCount := 0
	if days > 0 {
		flagCount++
	}
	if hours > 0 {
		flagCount++
	}
	if minutes > 0 {
		flagCount++
	}
	if flagCount == 0 {
		fmt.Fprintln(os.Stderr, "error: specify a time window: --days N, --hours N, or --minutes N")
		os.Exit(1)
	}
	if flagCount > 1 {
		fmt.Fprintln(os.Stderr, "error: use only one of --days, --hours, or --minutes")
		os.Exit(1)
	}

	var timeFilter string
	switch {
	case minutes > 0:
		h := (minutes + 59) / 60
		timeFilter = fmt.Sprintf("newer_than:%dh", h)
	case hours > 0:
		timeFilter = fmt.Sprintf("newer_than:%dh", hours)
	default:
		timeFilter = fmt.Sprintf("newer_than:%dd", days)
	}

	if err := runAnalyze(lc, cr, cfg, model, timeFilter); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runAnalyze(lc *llm.Client, cr *creds.Creds, cfg *config.Config, model, timeFilter string) error {
	logf("=== GML Analyze (model: %s) ===", model)

	// [1/4] Fetch emails
	logf("[1/4] Fetching and sanitizing emails...")
	var knowledgePatterns []knowledge.Pattern
	kf, err := knowledge.Load(knowledgePath())
	if err != nil {
		logf("warning: could not load knowledge.yaml: %v", err)
	} else {
		knowledgePatterns = kf.Patterns
	}

	exclusions := rules.BuildExclusions(cfg.Rules)
	if exclusions != "" {
		logf("excluding rule-matched emails: %s", exclusions)
	}

	boxes, err := fetch.FetchAllWithFilter(cr, timeFilter, exclusions)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	totalEmails := 0
	for _, b := range boxes {
		logf("  Box %d (%s): %d emails", b.Box.Number, b.Box.Name, len(b.Emails))
		totalEmails += len(b.Emails)
	}
	logf("  total: %d emails", totalEmails)

	if totalEmails == 0 {
		logf("  no emails to analyze — skipping")
		return nil
	}

	// Fetch DSH context
	var prevNotifs, dismissedNotifs string
	if cfg.Analysis.DSH.URL != "" && cfg.Analysis.DSH.ClientID != "" {
		dsh := notify.NewDSHClient(cfg.Analysis.DSH)
		notifs, err := dsh.GetNotifications("GML", 20)
		if err != nil {
			logf("warning: DSH unreachable, skipping previous notifications: %v", err)
		} else {
			prevNotifs = notify.FormatPreviousNotifications(notifs)
			if len(notifs) > 0 {
				logf("  included %d previous notifications", len(notifs))
			}
		}
		dismissed, err := dsh.GetDismissedNotifications("GML", 50, false)
		if err != nil {
			logf("warning: could not fetch dismissed notifications: %v", err)
		} else {
			dismissedNotifs = notify.FormatDismissedNotifications(dismissed)
			if len(dismissed) > 0 {
				logf("  included %d dismissed notifications", len(dismissed))
			}
		}
	}

	promptText := prompt.Build(boxes, prevNotifs, dismissedNotifs, knowledgePatterns)

	// [2/4] LLM analysis
	logf("[2/4] Analyzing with %s...", model)
	analysisText, err := lc.Call(model, promptText)
	if err != nil {
		return fmt.Errorf("LLM analysis: %w", err)
	}
	if strings.TrimSpace(analysisText) == "" {
		return fmt.Errorf("%s returned empty response", model)
	}

	// [3/4] Dedup
	logf("[3/4] Dedup review with %s...", model)
	analysisText, err = pipelineDedup(lc, cfg, model, analysisText, prompt.BuildDedup)
	if err != nil {
		return fmt.Errorf("dedup: %w", err)
	}
	if strings.TrimSpace(analysisText) == "" {
		logf("  dedup returned empty — nothing to post")
		logf("=== Analysis complete ===")
		return nil
	}

	// [4/4] Post to DSH
	logf("[4/4] Validating and posting to DSH...")
	if err := pipelineNotify(cfg, analysisText); err != nil {
		return fmt.Errorf("notify: %w", err)
	}

	logf("=== Analysis complete ===")
	return nil
}

// cmdPipelineLearn: history → LLM → insight-dedup → LLM → insights
func cmdPipelineLearn(lc *llm.Client, cr *creds.Creds, cfg *config.Config, args []string) {
	model := stringFlag(args, "--model", "gemini")
	days := intFlag(args, "--days", cfg.Analysis.Learn.EffectiveDays())

	if err := runLearn(lc, cr, cfg, model, days); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runLearn(lc *llm.Client, cr *creds.Creds, cfg *config.Config, model string, days int) error {
	logf("=== GML Knowledge: Learn (model: %s) ===", model)

	if err := cfg.Analysis.Validate(); err != nil {
		return fmt.Errorf("config: %w", err)
	}

	topSenders := cfg.Analysis.Learn.EffectiveTopSenders()
	minEmails := cfg.Analysis.Learn.EffectiveMinEmails()

	// [1/4] Collect behavioral data
	logf("[1/3] Collecting behavioral data...")
	logf("collecting behavioral data: %d-day window, top %d senders (min %d emails)...", days, topSenders, minEmails)

	client := &gwsClient{cr: cr}
	senders, err := behavior.CollectSenderBehavior(client, days, topSenders, minEmails)
	if err != nil {
		return fmt.Errorf("history: %w", err)
	}
	logf("  found %d senders above threshold", len(senders))

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)

	var dismissedNotifs string
	dismissed, err := dsh.GetDismissedNotifications("GML", 100, true)
	if err != nil {
		logf("warning: could not fetch dismissed notifications: %v", err)
	} else {
		dismissedNotifs = notify.FormatDismissedNotifications(dismissed)
		logf("  included %d dismissed notifications with comments", len(dismissed))
	}

	if len(senders) == 0 && len(dismissed) == 0 {
		logf("no behavioral data or dismissed notifications to analyze — skipping")
		return nil
	}

	var activeRules string
	if len(cfg.Rules) > 0 {
		var sb strings.Builder
		for _, r := range cfg.Rules {
			fmt.Fprintf(&sb, "- %s (type: %s)\n", r.Name, r.Type)
			switch r.Type {
			case "archive_by_sender":
				for _, p := range r.Params.Patterns {
					fmt.Fprintf(&sb, "    pattern: %s\n", p)
				}
			case "archive_by_age":
				fmt.Fprintf(&sb, "    days: %d, state: %s\n", r.Params.Days, r.Params.State)
			case "archive_by_label":
				fmt.Fprintf(&sb, "    label: %s\n", r.Params.Label)
			}
		}
		activeRules = sb.String()
	}

	var previousInsights string
	prevActive, err := dsh.GetNotifications("GML", 50)
	if err == nil {
		var insightNotifs []notify.PreviousNotification
		for _, n := range prevActive {
			if strings.Contains(n.Message, "[Insight:") {
				insightNotifs = append(insightNotifs, n)
			}
		}
		if len(insightNotifs) > 0 {
			previousInsights = notify.FormatPreviousNotifications(insightNotifs)
		}
	}
	prevDismissed, err := dsh.GetDismissedNotifications("GML", 50, false)
	if err == nil {
		for _, n := range prevDismissed {
			if strings.Contains(n.Message, "[Insight:") {
				ts := n.CreatedAt
				if len(ts) > 16 {
					ts = ts[:16]
				}
				previousInsights += fmt.Sprintf("[%s] %s\n", ts, n.Message)
			}
		}
	}

	var knowledgeText string
	kf, err := knowledge.Load(knowledgePath())
	if err == nil && len(kf.Patterns) > 0 {
		knowledgeText = knowledge.Format(kf)
		logf("  included %d knowledge patterns", len(kf.Patterns))
	}

	promptText := prompt.BuildHistory(senders, dismissedNotifs, activeRules, previousInsights, knowledgeText)

	// [2/4] LLM analysis
	logf("[2/4] Analyzing patterns with %s...", model)
	analysisText, err := lc.Call(model, promptText)
	if err != nil {
		return fmt.Errorf("LLM analysis: %w", err)
	}
	if strings.TrimSpace(analysisText) == "" {
		return fmt.Errorf("%s returned empty response", model)
	}

	// [3/4] Insight dedup
	logf("[3/4] Insight-dedup review with %s...", model)
	analysisText, err = pipelineDedup(lc, cfg, model, analysisText, prompt.BuildInsightDedup)
	if err != nil {
		return fmt.Errorf("insight-dedup: %w", err)
	}
	if strings.TrimSpace(analysisText) == "" {
		logf("  insight-dedup returned empty — nothing to post")
		logf("=== Learning complete ===")
		return nil
	}

	// [4/4] Post insights to DSH
	logf("[4/4] Validating and posting insights to DSH...")
	if err := pipelineInsights(cfg, analysisText); err != nil {
		return fmt.Errorf("insights: %w", err)
	}

	logf("=== Learning complete ===")
	return nil
}

// cmdPipelineDistill: distill-gather → LLM → distill-apply
func cmdPipelineDistill(lc *llm.Client, cfg *config.Config, args []string) {
	model := stringFlag(args, "--model", "gemini")

	if err := runDistill(lc, cfg, model); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runDistill(lc *llm.Client, cfg *config.Config, model string) error {
	logf("=== GML Knowledge: Distill (model: %s) ===", model)

	if err := cfg.Analysis.Validate(); err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// [1/3] Gather dismissed insights
	logf("[1/3] Gathering dismissed insights from DSH...")
	dsh := notify.NewDSHClient(cfg.Analysis.DSH)
	dismissed, err := dsh.GetDismissedNotifications("GML", 200, true)
	if err != nil {
		return fmt.Errorf("fetching dismissed: %w", err)
	}

	kf, _ := knowledge.Load(knowledgePath())
	insightNotifs, regularNotifs, skipped := selectUndistilled(dismissed, kf)

	if len(insightNotifs) == 0 && len(regularNotifs) == 0 {
		logf("no new dismissed notifications to distill (%d already distilled)", skipped)
		return nil
	}
	logf("  found %d dismissed insights + %d dismissed notifications with comments (%d skipped as already distilled)",
		len(insightNotifs), len(regularNotifs), skipped)
	insightNotifs = append(insightNotifs, regularNotifs...)

	dismissedText := notify.FormatDismissedNotifications(insightNotifs)

	var existingKnowledge string
	if kf != nil && len(kf.Patterns) > 0 {
		existingKnowledge = knowledge.Format(kf)
		logf("  included %d existing knowledge patterns", len(kf.Patterns))
	}

	promptText := prompt.BuildDistill(dismissedText, existingKnowledge)

	// [2/3] LLM distillation
	logf("[2/3] Distilling with %s...", model)
	analysisText, err := lc.Call(model, promptText)
	if err != nil {
		return fmt.Errorf("LLM distill: %w", err)
	}
	if strings.TrimSpace(analysisText) == "" {
		return fmt.Errorf("%s returned empty response", model)
	}

	// [3/3] Apply distilled knowledge
	logf("[3/3] Applying distilled knowledge...")

	result, warnings, err := notify.ParseAndValidateDistilled([]byte(analysisText))
	if err != nil {
		return fmt.Errorf("distill validation: %w", err)
	}
	for _, w := range warnings {
		logf("  warning: %s", w)
	}

	insightByKey := map[string][]int64{}
	for _, n := range dismissed {
		if n.Link == "" {
			continue
		}
		k := notify.InsightDedupKey(n.Link)
		insightByKey[k] = append(insightByKey[k], n.ID)
	}

	if kf == nil {
		kf = &knowledge.KnowledgeFile{}
	}
	changed := false

	if len(result.Patterns) > 0 {
		patterns := notify.DistilledToKnowledge(result.Patterns)
		for _, p := range patterns {
			p.SourceInsights = insightByKey[notify.NormalizeSearchKey(p.GmailSearch)]
			kf.Upsert(p)
			src := ""
			if len(p.SourceInsights) > 0 {
				src = fmt.Sprintf(" [insights %v]", p.SourceInsights)
			}
			logf("  [pattern:%s] %s (gmail: %s)%s", p.Status, p.Pattern, p.GmailSearch, src)
		}
		kf.LastDistilledAt = knowledge.Now()
		changed = true
	}

	if added := appendDistilledLedger(kf, dismissed); len(added) > 0 {
		for _, id := range added {
			logf("  [ledger] recorded insight #%d distilled (residual-gap)", id)
		}
		changed = true
	}

	kPath := knowledgePath()
	if changed {
		if err := knowledge.Save(kPath, kf); err != nil {
			return fmt.Errorf("saving knowledge.yaml: %w", err)
		}
	}
	if len(result.Patterns) > 0 {
		logf("  %d patterns written to knowledge.yaml", len(result.Patterns))
	}

	if len(result.Todos) > 0 {
		for _, td := range result.Todos {
			priority := td.Priority
			if priority == "" {
				priority = "Q2"
			}
			text := td.Text
			if len(td.SourceInsights) > 0 {
				text += "  " + formatInsightSuffix(td.SourceInsights)
			}
			if err := dsh.PostTodo(text, priority, td.ProjectCode); err != nil {
				return fmt.Errorf("posting todo: %w", err)
			}
			logf("  [todo:%s] %s", priority, text)
		}
		logf("  %d todos posted to DSH", len(result.Todos))
	}

	logf("done: %d patterns + %d todos", len(result.Patterns), len(result.Todos))
	logf("=== Distillation complete ===")
	return nil
}

// cmdPipelinePropose: propose-gather → LLM semantic dedup → propose-apply
func cmdPipelinePropose(lc *llm.Client, cfg *config.Config, args []string) {
	model := stringFlag(args, "--model", "gemini")
	noLLM := hasFlag(args, "--no-llm")
	jsonOut := hasFlag(args, "--json")

	if noLLM || jsonOut {
		cmdPropose(args)
		return
	}

	if err := runPropose(lc, cfg, model); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runPropose(lc *llm.Client, cfg *config.Config, model string) error {
	logf("=== GML Knowledge: Propose (model: %s) ===", model)

	if err := cfg.Analysis.Validate(); err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// [1/3] Generate proposals + structural dedup
	logf("[1/3] Generating proposals + structural dedup...")
	kf, err := knowledge.Load(knowledgePath())
	if err != nil {
		return fmt.Errorf("loading knowledge: %w", err)
	}
	if len(kf.Patterns) == 0 {
		logf("no patterns in knowledge.yaml — nothing to propose")
		return nil
	}

	result := propose.Generate(kf, cfg)
	if len(result.Proposals) == 0 {
		logf("no new proposals (all patterns already covered or skipped)")
		return nil
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)
	existingPlans, err := dsh.GetPlans("")
	if err != nil {
		logf("warning: could not fetch existing plans for dedup: %v", err)
	}

	survivors, structSkipped := structuralDedup(result.Proposals, existingPlans)
	logf("  %d candidate(s) after structural dedup (%d skipped as exact duplicates)", len(survivors), structSkipped)
	if len(survivors) == 0 {
		logf("no candidates survive structural dedup — nothing for the semantic gate")
		return nil
	}

	candidatesJSON, _ := json.MarshalIndent(survivors, "", "  ")
	promptText := prompt.BuildProposeReconcile(string(candidatesJSON), formatExistingPlans(existingPlans), formatExistingRules(cfg.Rules))

	// [2/3] Semantic dedup LLM
	logf("[2/3] Semantic dedup with %s...", model)
	responseText, err := lc.Call(model, promptText)
	if err != nil {
		return fmt.Errorf("LLM semantic dedup: %w", err)
	}
	if strings.TrimSpace(responseText) == "" {
		return fmt.Errorf("%s returned empty response", model)
	}

	// [3/3] Post surviving proposals
	logf("[3/3] Posting surviving plans to DSH...")
	kept, err := propose.ParseProposals([]byte(responseText))
	if err != nil {
		return fmt.Errorf("parsing proposals: %w", err)
	}

	survivors, structSkipped = structuralDedup(kept, existingPlans)

	posted := 0
	for _, p := range survivors {
		detail, _ := json.Marshal(p)
		id, err := dsh.PostPlan("GML", p.ProposedRule.Name+" ("+p.ProposedRule.Type+")", string(detail))
		if err != nil {
			return fmt.Errorf("posting plan: %w", err)
		}
		posted++
		if supersedes := parseConflictPlanIDs(p.KnowledgeRef); len(supersedes) > 0 {
			logf("  posted plan #%d: %s (folded — supersedes %v)", id, p.ProposedRule.Name, supersedes)
		} else {
			logf("  posted plan #%d: %s", id, p.ProposedRule.Name)
		}
	}
	logf("done: %d plans posted, %d skipped (structural re-check)", posted, structSkipped)

	logf("=== Propose complete ===")
	return nil
}

// cmdPipelineApplyRules: deterministic apply-rules (no LLM) or LLM merge
func cmdPipelineApplyRules(lc *llm.Client, cfg *config.Config, args []string) {
	model := stringFlag(args, "--model", "gemini")
	noLLM := hasFlag(args, "--no-llm")

	if noLLM {
		if err := runApplyRulesDeterministic(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := runApplyRulesLLM(lc, cfg, model); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runApplyRulesDeterministic(cfg *config.Config) error {
	logf("=== GML Apply-Rules (deterministic, no LLM) ===")

	if err := cfg.Analysis.Validate(); err != nil {
		return fmt.Errorf("config: %w", err)
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)
	plans, err := dsh.GetPlans("approved")
	if err != nil {
		return fmt.Errorf("fetching approved plans: %w", err)
	}

	if len(plans) == 0 {
		logf("no approved plans in DSH — nothing to apply")
		return nil
	}

	superseded := make(map[int64]bool)
	for _, plan := range plans {
		var p propose.Proposal
		if json.Unmarshal([]byte(plan.Detail), &p) == nil {
			for _, id := range parseConflictPlanIDs(p.KnowledgeRef) {
				superseded[id] = true
			}
		}
	}

	var newRules []propose.AnnotatedRule
	for _, plan := range plans {
		if superseded[plan.ID] {
			logf("  skipping plan #%d (superseded by conflict resolution)", plan.ID)
			continue
		}
		var p propose.Proposal
		if err := json.Unmarshal([]byte(plan.Detail), &p); err != nil {
			logf("  skipping plan #%d: invalid detail JSON: %v", plan.ID, err)
			continue
		}
		if p.ProposedRule.Name == "" {
			logf("  skipping plan #%d: no proposed rule", plan.ID)
			continue
		}
		newRules = append(newRules, propose.AnnotatedRule{
			Rule:       p.ProposedRule,
			Pattern:    p.Pattern,
			Constraint: p.Constraint,
			PlanIDs:    []int64{plan.ID},
			InsightIDs: p.SourceInsights,
		})
		constraint := ""
		if p.Constraint != "" {
			constraint = " [constraint: " + p.Constraint + "]"
		}
		logf("  approved: %s (%s)%s", p.ProposedRule.Name, p.ProposedRule.Type, constraint)
	}

	if len(newRules) == 0 {
		logf("no valid rules to apply")
		return nil
	}

	newRules = guardSameSender(newRules)
	if len(newRules) == 0 {
		logf("no rules left after same-sender guard")
		return nil
	}

	rPath := rulesYAMLPath()
	data, err := os.ReadFile(rPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", rPath, err)
	}

	output := propose.BuildGeneratedRules(string(data), newRules)
	if err := os.WriteFile(rPath, []byte(output), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", rPath, err)
	}
	logf("[apply-rules] rules.yaml updated")
	logf("=== Apply-Rules complete ===")
	return nil
}

func runApplyRulesLLM(lc *llm.Client, cfg *config.Config, model string) error {
	logf("=== GML Apply-Rules (LLM merge, model: %s) ===", model)

	if err := cfg.Analysis.Validate(); err != nil {
		return fmt.Errorf("config: %w", err)
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)

	// [1/3] Gather approved plans
	logf("[1/3] Gathering approved plans...")
	plans, err := dsh.GetPlans("approved")
	if err != nil {
		return fmt.Errorf("fetching plans: %w", err)
	}
	if len(plans) == 0 {
		logf("no approved plans — nothing to apply")
		return nil
	}

	superseded := make(map[int64]bool)
	for _, plan := range plans {
		var p propose.Proposal
		if err := json.Unmarshal([]byte(plan.Detail), &p); err != nil {
			continue
		}
		for _, id := range parseConflictPlanIDs(p.KnowledgeRef) {
			superseded[id] = true
		}
	}

	var mergePlans []prompt.MergePlan
	for _, plan := range plans {
		if superseded[plan.ID] {
			logf("  skipping plan #%d (superseded by conflict resolution)", plan.ID)
			continue
		}
		var p propose.Proposal
		if err := json.Unmarshal([]byte(plan.Detail), &p); err != nil {
			logf("  skipping plan #%d: invalid detail JSON: %v", plan.ID, err)
			continue
		}
		if p.ProposedRule.Name == "" {
			logf("  skipping plan #%d: no rule name (unresolved conflict?)", plan.ID)
			continue
		}
		mergePlans = append(mergePlans, prompt.MergePlan{
			PlanID:       plan.ID,
			Title:        plan.Title,
			Pattern:      p.Pattern,
			Constraint:   p.Constraint,
			RuleName:     p.ProposedRule.Name,
			RuleType:     p.ProposedRule.Type,
			Senders:      p.ProposedRule.Params.Patterns,
			Filter:       p.ProposedRule.Params.Filter,
			RequireReply: p.ProposedRule.Params.RequireReply,
			Reason:       p.Reason,
		})
		logf("  plan #%d: %s", plan.ID, plan.Title)
	}

	if len(mergePlans) == 0 {
		logf("no valid plans to merge")
		return nil
	}

	var existingRules []prompt.ExistingRule
	for _, r := range cfg.Rules {
		er := prompt.ExistingRule{
			Name:         r.Name,
			Type:         r.Type,
			Filter:       r.Params.Filter,
			RequireReply: r.Params.RequireReply,
		}
		if r.Type == "archive_by_sender" {
			er.Senders = r.Params.Patterns
		}
		existingRules = append(existingRules, er)
	}

	kf, _ := knowledge.Load(knowledgePath())
	knowledgeCtx := ""
	if kf != nil {
		knowledgeCtx = knowledge.Format(kf)
	}

	promptText := prompt.BuildMergePlans(mergePlans, existingRules, knowledgeCtx)

	// [2/3] LLM merge
	logf("[2/3] Merging with %s (conflict detection)...", model)
	responseText, err := lc.Call(model, promptText)
	if err != nil {
		return fmt.Errorf("LLM merge: %w", err)
	}
	if strings.TrimSpace(responseText) == "" {
		return fmt.Errorf("%s returned empty response", model)
	}

	// [3/3] Apply merged rules
	logf("[3/3] Validating merge and applying rules...")
	mergeResult, err := propose.ParseAndValidateMerge([]byte(responseText))
	if err != nil {
		return fmt.Errorf("merge validation: %w", err)
	}

	// Completeness check
	var planIDs []int64
	for _, p := range plans {
		if !superseded[p.ID] {
			planIDs = append(planIDs, p.ID)
		}
	}
	if err := propose.ValidateMergeCompleteness(mergeResult, planIDs); err != nil {
		logf("warning: %v", err)
	}

	// Post conflicts
	existingConflictKeys := make(map[string]bool)
	allPlans, err := dsh.GetPlans("")
	if err != nil {
		logf("warning: could not fetch plans for conflict dedup: %v", err)
	} else {
		for _, plan := range allPlans {
			if plan.Status == "rejected" {
				continue
			}
			var detail struct {
				KnowledgeRef string  `json:"knowledge_ref"`
				SourcePlans  []int64 `json:"source_plan_ids"`
			}
			if err := json.Unmarshal([]byte(plan.Detail), &detail); err != nil {
				continue
			}
			if strings.HasPrefix(detail.KnowledgeRef, "merge_conflict:") {
				existingConflictKeys[detail.KnowledgeRef] = true
			}
			if len(detail.SourcePlans) > 0 {
				existingConflictKeys[conflictPlanKey(detail.SourcePlans)] = true
			}
		}
	}

	conflictsPosted, conflictsSkipped := 0, 0
	for _, conflict := range mergeResult.Conflicts {
		key := conflictPlanKey(conflict.AffectedPlanIDs)
		if existingConflictKeys[key] {
			conflictsSkipped++
			logf("  skipped (conflict plan exists): %s", truncate(conflict.Description, 80))
			continue
		}
		title := fmt.Sprintf("merge conflict: %s", truncate(conflict.Description, 80))
		detail := propose.FormatConflictPlanDetail(conflict)
		id, err := dsh.PostPlan("GML", title, detail)
		if err != nil {
			return fmt.Errorf("posting conflict plan: %w", err)
		}
		conflictsPosted++
		logf("  conflict → plan #%d: %s", id, conflict.Description)
	}

	// Build rules
	annotated := propose.MergeResultToAnnotatedRules(mergeResult)
	annotated = guardSameSender(annotated)

	if len(annotated) == 0 {
		logf("no non-conflicting rules to apply")
		if len(mergeResult.Conflicts) > 0 {
			logf("  %d conflicts posted to DSH as plans for review", conflictsPosted)
		}
		return nil
	}

	rPath := rulesYAMLPath()
	data, err := os.ReadFile(rPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", rPath, err)
	}

	output := propose.BuildGeneratedRules(string(data), annotated)
	if err := os.WriteFile(rPath, []byte(output), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", rPath, err)
	}

	logf("done: %d rules merged, %d conflicts posted, %d skipped (dedup)",
		len(annotated), conflictsPosted, conflictsSkipped)
	logf("=== Apply-Rules complete ===")
	return nil
}

func stringFlag(args []string, flag, def string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return def
}
