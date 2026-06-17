package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/topolik/gml-gmail-agent/internal/behavior"
	"github.com/topolik/gml-gmail-agent/internal/config"
	"github.com/topolik/gml-gmail-agent/internal/creds"
	"github.com/topolik/gml-gmail-agent/internal/fetch"
	"github.com/topolik/gml-gmail-agent/internal/gws"
	"github.com/topolik/gml-gmail-agent/internal/knowledge"
	"github.com/topolik/gml-gmail-agent/internal/llm"
	"github.com/topolik/gml-gmail-agent/internal/notify"
	"github.com/topolik/gml-gmail-agent/internal/prompt"
	"github.com/topolik/gml-gmail-agent/internal/propose"
	"github.com/topolik/gml-gmail-agent/internal/rules"
	"github.com/topolik/gml-gmail-agent/internal/scheduler"
	"github.com/topolik/gml-gmail-agent/internal/stats"
)

const usage = `gml — Gmail agent

Pipeline commands (credentials via stdin, LLM via LLP proxy):
  gml analyze [--days N|--hours N|--minutes N] [--model gemini|claude]
  gml learn [--days N] [--model gemini|claude]
  gml distill [--model gemini|claude]
  gml propose [--json] [--no-llm] [--model gemini|claude]
  gml apply-rules [--no-llm] [--model gemini|claude]
  gml watch-analysis [--model gemini|claude] [--interval N]
  gml watch-knowledge [--model gemini|claude] [--interval N]

Building-block commands (credentials via stdin, no LLM):
  gml profile                       Show authenticated Gmail account
  gml stats [--json] [--days N]     Inbox statistics
  gml run [--dry-run] [--json] [--since H] [--pages N]   Apply archive rules
  gml watch-rules [--interval N]    Start rules scheduler daemon (N minutes)
  gml fetch [--days N|--hours N|--minutes N]  Fetch emails, build LLM prompt (stdout)
  gml notify                        Read LLM analysis JSON (stdin), post to DSH
  gml dedup                         Read LLM analysis JSON (stdin), output dedup prompt
  gml insight-dedup                 Like dedup, for learn path
  gml history [--days N]            Collect behavioral data, build learning prompt (stdout)
  gml insights                      Read LLM insight JSON (stdin), post to DSH
  gml distill-gather                Gather dismissed insights, build distillation prompt (stdout)
  gml distill-apply                 Read LLM distillation JSON (stdin), update knowledge.yaml
  gml propose-gather                Build LLM semantic-dedup prompt for new proposals (stdout)
  gml propose-apply                 Read kept-proposals JSON (stdin), post survivors to DSH
  gml merge-plans-gather            Build LLM prompt for merging approved plans (stdout)
  gml merge-plans-apply             Read LLM merge JSON (stdin), apply rules + post conflicts to DSH

Environment:
  LLP_URL       LLP proxy base URL (e.g. http://localhost:4000)
  LLP_SOCKET    Control socket for token handshake (default ~/.llp/control.sock)
  LLP_MODEL     Logical model to request (default: gml-analyze)
  GML_RULES     Path to rules.yaml (default: data/rules.yaml)
  GML_DSH       Path to dsh.yaml (default: data/dsh.yaml)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		return
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	// Pipeline commands — load credentials from 1Password, route LLM via LLP proxy
	switch cmd {
	case "analyze":
		cfg := loadConfig()
		lc := llm.NewFromEnv()
		cr := pipelineLoadCreds()
		cmdPipelineAnalyze(lc, cr, cfg, args)
		return
	case "learn":
		cfg := loadConfig()
		lc := llm.NewFromEnv()
		cr := pipelineLoadCreds()
		cmdPipelineLearn(lc, cr, cfg, args)
		return
	case "distill":
		cfg := loadConfig()
		lc := llm.NewFromEnv()
		cmdPipelineDistill(lc, cfg, args)
		return
	case "propose":
		cfg := loadConfig()
		lc := llm.NewFromEnv()
		cmdPipelinePropose(lc, cfg, args)
		return
	case "apply-rules":
		cfg := loadConfig()
		lc := llm.NewFromEnv()
		cmdPipelineApplyRules(lc, cfg, args)
		return
	case "watch-analysis":
		cfg := loadConfig()
		lc := llm.NewFromEnv()
		cr := pipelineLoadCreds()
		cmdWatchAnalysis(lc, cr, cfg, args)
		return
	case "watch-knowledge":
		cfg := loadConfig()
		lc := llm.NewFromEnv()
		cr := pipelineLoadCreds()
		cmdWatchKnowledge(lc, cr, cfg, args)
		return
	}

	// Building-block commands: read LLM JSON from stdin or DSH — no Gmail credentials needed
	if cmd == "notify" {
		cmdNotify()
		return
	}
	if cmd == "dedup" {
		cmdDedup()
		return
	}
	if cmd == "insight-dedup" {
		cmdInsightDedup()
		return
	}
	if cmd == "insights" {
		cmdInsights()
		return
	}
	if cmd == "distill-gather" {
		cmdDistillGather()
		return
	}
	if cmd == "distill-apply" {
		cmdDistillApply()
		return
	}
	if cmd == "propose-gather" {
		cmdProposeGather()
		return
	}
	if cmd == "propose-apply" {
		cmdProposeApply()
		return
	}
	if cmd == "merge-plans-gather" {
		cmdMergePlansGather()
		return
	}
	if cmd == "merge-plans-apply" {
		cmdMergePlansApply()
		return
	}
	cr, err := creds.Load(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading credentials: %v\n", err)
		fmt.Fprintln(os.Stderr, "Credentials must be piped via stdin:")
		fmt.Fprintln(os.Stderr, `  op item get "GML Gmail Read-Only Credentials" --fields credential --reveal | ./run-task.sh <command>`)
		os.Exit(1)
	}

	switch cmd {
	case "profile":
		cmdProfile(cr)
	case "count":
		cmdCount(cr, args)
	case "stats":
		cmdStats(cr, args)
	case "run":
		cmdRun(cr, args)
	case "watch-rules":
		cmdServe(cr, args)
	case "fetch":
		cmdFetch(cr, args)
	case "history":
		cmdHistory(cr, args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(1)
	}
}

func cmdProfile(cr *creds.Creds) {
	p, err := gws.GetProfile(cr)
	fatal(err)
	fmt.Printf("Account:  %s\nMessages: %d\nThreads:  %d\n",
		p.EmailAddress, p.MessagesTotal, p.ThreadsTotal)
}

// cmdCount runs a raw Gmail search and prints how many messages match — a
// diagnostic for checking whether a candidate filter actually matches mail
// (Gmail tokenizes/ignores punctuation, so a filter that looks right may match 0).
func cmdCount(cr *creds.Creds, args []string) {
	if len(args) == 0 || strings.TrimSpace(strings.Join(args, " ")) == "" {
		fmt.Fprintln(os.Stderr, "usage: gml count <gmail-query>")
		os.Exit(1)
	}
	query := strings.Join(args, " ")
	n, err := gws.CountMessages(cr, query)
	fatal(err)
	fmt.Printf("%d\n", n)
	fmt.Fprintf(os.Stderr, "query %q → %d messages\n", query, n)
}

func cmdStats(cr *creds.Creds, args []string) {
	jsonOut := hasFlag(args, "--json")
	days := intFlag(args, "--days", 3)

	s, err := stats.Collect(cr, days)
	fatal(err)

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		fatal(enc.Encode(s))
		return
	}

	fmt.Printf("=== Gmail Stats (last %d days) ===\n\n", s.RecentDays)
	fmt.Printf("Total messages: %d\n", s.TotalMessages)
	fmt.Printf("Total threads:  %d\n", s.TotalThreads)
	fmt.Printf("Last %d days:   %d messages\n\n", s.RecentDays, s.RecentCount)

	if len(s.TopSenders) > 0 {
		fmt.Println("--- Top Senders ---")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, sc := range s.TopSenders {
			fmt.Fprintf(w, "  %d\t%s\n", sc.Count, sc.Sender)
		}
		w.Flush()
	}
}

func cmdRun(cr *creds.Creds, args []string) {
	dryRun := hasFlag(args, "--dry-run")
	jsonOut := hasFlag(args, "--json")
	pages := intFlag(args, "--pages", 5)
	sinceHours := intFlag(args, "--since", 0)

	cfg := loadConfig()

	if cfg.ReadOnly() && !dryRun {
		fmt.Fprintln(os.Stderr, "error: readonly mode — archiving is disabled")
		fmt.Fprintln(os.Stderr, "  To enable: set 'mode: readwrite' in rules.yaml and re-run setup.sh without --readonly")
		os.Exit(1)
	}

	if dryRun && !jsonOut {
		fmt.Println("[DRY RUN] No messages will be archived.")
	}

	var tracingLabelID string
	if !dryRun {
		labelID, err := gws.EnsureTracingLabel(cr, rules.TracingLabelName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: tracing labels unavailable, archiving without labels: %v\n", err)
		} else {
			tracingLabelID = labelID
			fmt.Fprintf(os.Stderr, "tracing label %q ready (id: %s)\n", rules.TracingLabelName, labelID)
		}
	}

	result, err := rules.RunWithSenderFilter(cfg, cr, dryRun, pages, sinceHours, tracingLabelID)
	fatal(err)

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		fatal(enc.Encode(result))
		return
	}

	if len(result.Actions) == 0 && len(result.Errors) == 0 {
		fmt.Println("No messages matched any rules.")
		return
	}

	archived := 0
	would := 0
	for _, a := range result.Actions {
		verb := "archived"
		if !a.Archived {
			verb = "would archive"
			would++
		} else {
			archived++
		}
		fmt.Printf("[%s] [%s] %s — %s\n", verb, a.RuleName, a.From, a.Subject)
		fmt.Printf("  id: %s\n", a.MessageID)
		if a.Date != "" {
			fmt.Printf("  date: %s\n", a.Date)
		}
		fmt.Printf("  reason: %s\n", a.Reason)
	}

	fmt.Println()
	if dryRun {
		fmt.Printf("Summary: %d messages would be archived\n", would)
	} else {
		fmt.Printf("Summary: %d messages archived\n", archived)
	}

	if len(result.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d errors:\n", len(result.Errors))
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		os.Exit(1)
	}
}

func cmdServe(cr *creds.Creds, args []string) {
	cfg := loadConfig()
	intervalOverride := intFlag(args, "--interval", 0)
	rulesPath := os.Getenv("GML_RULES")
	if rulesPath == "" {
		rulesPath = "data/rules.yaml"
	}
	scheduler.Run(cfg, cr, intervalOverride, rulesPath)
}

func cmdFetch(cr *creds.Creds, args []string) {
	cfg := loadConfig()
	days := intFlag(args, "--days", 0)
	hours := intFlag(args, "--hours", 0)
	minutes := intFlag(args, "--minutes", 0)

	// Count how many time flags were explicitly set
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
	var label string
	switch {
	case minutes > 0:
		if minutes > 14*24*60 {
			fmt.Fprintf(os.Stderr, "error: maximum is 14 days (%d minutes), got %d\n", 14*24*60, minutes)
			os.Exit(1)
		}
		// Gmail doesn't support minutes — convert to hours, minimum 1h
		h := (minutes + 59) / 60
		timeFilter = fmt.Sprintf("newer_than:%dh", h)
		label = fmt.Sprintf("%d minutes (→ %dh)", minutes, h)
	case hours > 0:
		if hours > 14*24 {
			fmt.Fprintf(os.Stderr, "error: maximum is 14 days (%d hours), got %d\n", 14*24, hours)
			os.Exit(1)
		}
		timeFilter = fmt.Sprintf("newer_than:%dh", hours)
		label = fmt.Sprintf("%d hours", hours)
	default:
		maxDays := cfg.Analysis.EffectiveMaxDays()
		if days > 14 {
			fmt.Fprintf(os.Stderr, "error: maximum --days is 14 (got %d)\n", days)
			os.Exit(1)
		}
		if days > maxDays {
			fmt.Fprintf(os.Stderr, "error: maximum --days is %d per config (got %d)\n", maxDays, days)
			os.Exit(1)
		}
		timeFilter = fmt.Sprintf("newer_than:%dd", days)
		label = fmt.Sprintf("%d days", days)
	}

	var knowledgePatterns []knowledge.Pattern
	kf, kErr := knowledge.Load("data/knowledge.yaml")
	if kErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not load knowledge.yaml: %v\n", kErr)
	} else {
		knowledgePatterns = kf.Patterns
	}

	exclusions := rules.BuildExclusions(cfg.Rules)
	if exclusions != "" {
		fmt.Fprintf(os.Stderr, "excluding rule-matched emails: %s\n", exclusions)
	}

	fmt.Fprintf(os.Stderr, "fetching emails from past %s across 6 boxes...\n", label)
	boxes, err := fetch.FetchAllWithFilter(cr, timeFilter, exclusions)
	fatal(err)

	totalEmails := 0
	for _, b := range boxes {
		fmt.Fprintf(os.Stderr, "  Box %d (%s): %d emails\n", b.Box.Number, b.Box.Name, len(b.Emails))
		totalEmails += len(b.Emails)
	}
	fmt.Fprintf(os.Stderr, "  total: %d emails\n", totalEmails)

	if totalEmails == 0 {
		os.Exit(2)
	}

	// Fetch previous notifications from DSH (non-fatal if unreachable)
	var prevNotifs, dismissedNotifs string
	if cfg.Analysis.DSH.URL != "" && cfg.Analysis.DSH.ClientID != "" {
		dsh := notify.NewDSHClient(cfg.Analysis.DSH)
		notifs, err := dsh.GetNotifications("GML", 20)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: DSH unreachable, skipping previous notifications: %v\n", err)
		} else {
			prevNotifs = notify.FormatPreviousNotifications(notifs)
			if len(notifs) > 0 {
				fmt.Fprintf(os.Stderr, "  included %d previous notifications\n", len(notifs))
			}
		}
		dismissed, err := dsh.GetDismissedNotifications("GML", 50, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not fetch dismissed notifications: %v\n", err)
		} else {
			dismissedNotifs = notify.FormatDismissedNotifications(dismissed)
			if len(dismissed) > 0 {
				fmt.Fprintf(os.Stderr, "  included %d dismissed notifications\n", len(dismissed))
			}
		}
	}

	promptText := prompt.Build(boxes, prevNotifs, dismissedNotifs, knowledgePatterns)
	fmt.Print(promptText)
}

func cmdDedup() { dedupCmd(prompt.BuildDedup) }

// cmdInsightDedup is the learn-path counterpart of cmdDedup: same gather/
// pass-through plumbing, but a prompt that re-surfaces genuine refinements of a
// dismissed insight instead of strictly removing anything that touches one.
func cmdInsightDedup() { dedupCmd(prompt.BuildInsightDedup) }

func dedupCmd(build func(string, []notify.PreviousNotification) string) {
	cfg := loadConfig()
	if err := cfg.Analysis.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	data, err := io.ReadAll(os.Stdin)
	fatal(err)

	analysisJSON := strings.TrimSpace(string(data))
	if analysisJSON == "" || analysisJSON == "[]" {
		fmt.Print("[]")
		return
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)
	all, err := dsh.GetDismissedNotifications("GML", 200, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch dismissed notifications, passing analysis through: %v\n", err)
		fmt.Print(analysisJSON)
		return
	}

	var dismissed []notify.PreviousNotification
	for _, n := range all {
		if n.DismissedAt != "" {
			dismissed = append(dismissed, n)
		}
	}

	if len(dismissed) == 0 {
		fmt.Fprintf(os.Stderr, "no dismissed notifications — skipping dedup\n")
		fmt.Print(analysisJSON)
		return
	}

	fmt.Fprintf(os.Stderr, "building dedup prompt with %d dismissed notifications\n", len(dismissed))
	fmt.Print(build(analysisJSON, dismissed))
}

func cmdNotify() {
	cfg := loadConfig()
	if err := cfg.Analysis.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	data, err := io.ReadAll(os.Stdin)
	fatal(err)

	results, err := notify.ParseAndValidate(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: LLM output validation failed: %v\n", err)
		os.Exit(1)
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)
	notifs := notify.ToNotifications(results)

	// Dedup: fetch all notifications (active + dismissed), skip if Link already exists
	existingLinks := make(map[string]bool)
	existing, err := dsh.GetDismissedNotifications("GML", 200, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch existing notifications for dedup: %v\n", err)
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
			fmt.Fprintf(os.Stderr, "  skipped (duplicate): %s\n", n.Message)
			continue
		}
		if err := dsh.PostNotification(n); err != nil {
			fmt.Fprintf(os.Stderr, "error posting notification: %v\n", err)
			os.Exit(1)
		}
		posted++
		fmt.Fprintf(os.Stderr, "  posted: [%s] %s\n", n.Type, n.Message)
	}
	fmt.Fprintf(os.Stderr, "done: %d notifications posted, %d skipped (duplicate)\n", posted, skipped)
}

type gwsClient struct {
	cr *creds.Creds
}

func (c *gwsClient) ListMessages(query string, maxPages int) ([]gws.MessageRef, error) {
	return gws.ListMessages(c.cr, query, maxPages)
}
func (c *gwsClient) GetMessage(id string) (*gws.Message, error) {
	return gws.GetMessage(c.cr, id)
}
func (c *gwsClient) ListThreads(query string, maxPages int) ([]gws.ThreadRef, error) {
	return gws.ListThreads(c.cr, query, maxPages)
}
func (c *gwsClient) GetThread(id string) (*gws.Thread, error) {
	return gws.GetThread(c.cr, id)
}

func cmdHistory(cr *creds.Creds, args []string) {
	cfg := loadConfig()
	if err := cfg.Analysis.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	days := intFlag(args, "--days", cfg.Analysis.Learn.EffectiveDays())
	maxDays := cfg.Analysis.Learn.EffectiveMaxDays()
	if days > maxDays {
		fmt.Fprintf(os.Stderr, "error: maximum --days is %d for learning (got %d)\n", maxDays, days)
		os.Exit(1)
	}

	topSenders := cfg.Analysis.Learn.EffectiveTopSenders()
	minEmails := cfg.Analysis.Learn.EffectiveMinEmails()

	fmt.Fprintf(os.Stderr, "collecting behavioral data: %d-day window, top %d senders (min %d emails)...\n", days, topSenders, minEmails)

	client := &gwsClient{cr: cr}
	senders, err := behavior.CollectSenderBehavior(client, days, topSenders, minEmails)
	fatal(err)
	fmt.Fprintf(os.Stderr, "  found %d senders above threshold\n", len(senders))

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)

	var dismissedNotifs string
	dismissed, err := dsh.GetDismissedNotifications("GML", 100, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch dismissed notifications: %v\n", err)
	} else {
		dismissedNotifs = notify.FormatDismissedNotifications(dismissed)
		fmt.Fprintf(os.Stderr, "  included %d dismissed notifications with comments\n", len(dismissed))
	}

	if len(senders) == 0 && len(dismissed) == 0 {
		fmt.Fprintln(os.Stderr, "no behavioral data or dismissed notifications to analyze — exiting")
		os.Exit(2)
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
	kf, err := knowledge.Load("data/knowledge.yaml")
	if err == nil && len(kf.Patterns) > 0 {
		knowledgeText = knowledge.Format(kf)
		fmt.Fprintf(os.Stderr, "  included %d knowledge patterns\n", len(kf.Patterns))
	}

	promptText := prompt.BuildHistory(senders, dismissedNotifs, activeRules, previousInsights, knowledgeText)
	fmt.Print(promptText)
}

func cmdInsights() {
	cfg := loadConfig()
	if err := cfg.Analysis.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	data, err := io.ReadAll(os.Stdin)
	fatal(err)

	results, err := notify.ParseAndValidateInsights(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: LLM insight validation failed: %v\n", err)
		os.Exit(1)
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)

	// Fetch existing notifications (active + dismissed) and classify each
	// candidate deterministically against them (no LLM):
	//   - structural floor: an exact-canonical-query repost (seen or dismissed)
	//     is skipped — dismissing is the strongest "I've seen this" signal.
	//   - identity match (sender-set + category) against an ACTIVE insight →
	//     update it in place rather than post a reworded duplicate. The learn LLM
	//     re-derives the same insight with a different query string every cycle,
	//     which the structural key alone cannot collapse.
	//   - everything else → post new (incl. a re-surfaced clarification of a
	//     dismissed insight, which the upstream insight-dedup stage let through).
	existing, err := dsh.GetDismissedNotifications("GML", 100, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch existing notifications for dedup: %v\n", err)
	}

	posts, updates, skips := notify.ClassifyInsights(results, existing)

	for _, c := range skips {
		fmt.Fprintf(os.Stderr, "  skipped (already seen/dismissed): %s\n", c.Pattern)
	}

	updated := 0
	for _, u := range updates {
		n := notify.InsightToNotification(u.Candidate)
		if err := dsh.UpdateNotification(u.ID, n.Message, n.Link, n.Priority); err != nil {
			// A 404 means the matched insight was dismissed between fetch and
			// PATCH — fall back to posting it as new rather than dropping it.
			fmt.Fprintf(os.Stderr, "  update #%d failed (%v) — posting as new\n", u.ID, err)
			posts = append(posts, u.Candidate)
			continue
		}
		updated++
		fmt.Fprintf(os.Stderr, "  updated #%d in place: %s\n", u.ID, n.Message)
	}

	posted := 0
	for _, c := range posts {
		n := notify.InsightToNotification(c)
		if err := dsh.PostNotification(n); err != nil {
			fmt.Fprintf(os.Stderr, "error posting insight: %v\n", err)
			os.Exit(1)
		}
		posted++
		fmt.Fprintf(os.Stderr, "  posted: [%s] %s\n", n.Type, n.Message)
	}
	fmt.Fprintf(os.Stderr, "done: %d posted, %d updated in place, %d skipped (duplicate)\n", posted, updated, len(skips))
}

func cmdDistillGather() {
	cfg := loadConfig()
	if err := cfg.Analysis.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)

	fmt.Fprintln(os.Stderr, "gathering dismissed notifications with comments...")
	dismissed, err := dsh.GetDismissedNotifications("GML", 200, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching dismissed notifications: %v\n", err)
		os.Exit(1)
	}

	// Skip-dedup: an insight already distilled must not be re-distilled, or the LLM
	// re-emits the same pattern/todo every cycle. "Already distilled" = provenance
	// (insight ID in a knowledge pattern's SourceInsights, iter 020) ∪ the local
	// distilled-ledger (iter 023 — covers the residual gap provenance can't:
	// todo-only / distilled-to-nothing / non-matching-query). Both live in
	// knowledge.yaml — no DSH dependency in the skip path.
	kf, _ := knowledge.Load("data/knowledge.yaml")
	insightNotifs, regularNotifs, skipped := selectUndistilled(dismissed, kf)

	if len(insightNotifs) == 0 && len(regularNotifs) == 0 {
		fmt.Fprintf(os.Stderr, "no new dismissed notifications to distill (%d already distilled)\n", skipped)
		os.Exit(0)
	}
	fmt.Fprintf(os.Stderr, "  found %d dismissed insights + %d dismissed notifications with comments (%d skipped as already distilled)\n", len(insightNotifs), len(regularNotifs), skipped)
	insightNotifs = append(insightNotifs, regularNotifs...)

	dismissedText := notify.FormatDismissedNotifications(insightNotifs)

	var existingKnowledge string
	if kf != nil && len(kf.Patterns) > 0 {
		existingKnowledge = knowledge.Format(kf)
		fmt.Fprintf(os.Stderr, "  included %d existing knowledge patterns\n", len(kf.Patterns))
	}

	promptText := prompt.BuildDistill(dismissedText, existingKnowledge)
	fmt.Print(promptText)
}

// provenanceDistilledSet is the set of insight IDs already recorded in a
// knowledge pattern's SourceInsights — the iter-020 deterministic repeat-killer
// for the common case (insight → matching pattern).
func provenanceDistilledSet(kf *knowledge.KnowledgeFile) map[int64]bool {
	distilled := map[int64]bool{}
	if kf != nil {
		for _, p := range kf.Patterns {
			for _, id := range p.SourceInsights {
				distilled[id] = true
			}
		}
	}
	return distilled
}

// isDistillable reports whether a dismissed notification feeds distillation —
// an insight-tagged row or any commented row. The union of selectUndistilled's
// two partition branches; the single source of truth for the marking step.
func isDistillable(n notify.PreviousNotification) bool {
	return strings.Contains(n.Message, "[Insight:") || n.Comment != ""
}

// distilledSet is the full "already distilled" skip-set: pattern provenance
// (iter 020) ∪ the local distilled-ledger (iter 023, the residual gap).
func distilledSet(kf *knowledge.KnowledgeFile) map[int64]bool {
	set := provenanceDistilledSet(kf)
	if kf != nil {
		for _, id := range kf.DistilledInsights {
			set[id] = true
		}
	}
	return set
}

// selectUndistilled partitions dismissed notifications for distillation, skipping
// any already distilled (provenance ∪ ledger). Returns insight-tagged
// notifications, regular commented notifications, and the count skipped.
func selectUndistilled(dismissed []notify.PreviousNotification, kf *knowledge.KnowledgeFile) (insights, regulars []notify.PreviousNotification, skipped int) {
	distilled := distilledSet(kf)
	for _, n := range dismissed {
		if distilled[n.ID] {
			skipped++
			continue
		}
		if strings.Contains(n.Message, "[Insight:") {
			insights = append(insights, n)
		} else if n.Comment != "" {
			regulars = append(regulars, n)
		}
	}
	return insights, regulars, skipped
}

// appendDistilledLedger records the residual-gap insights — distillable dismissed
// insights this cycle covered that pattern provenance can't (todo-only /
// distilled-to-nothing / non-matching query) and that aren't already ledgered —
// into kf.DistilledInsights, returning the IDs newly added. Callers upsert this
// cycle's patterns into kf FIRST, so pattern-producing insights are excluded
// (they're covered by provenance). This is the deliberate local "separate ledger"
// the gap requires: provenance alone cannot derive these (GML-087).
func appendDistilledLedger(kf *knowledge.KnowledgeFile, dismissed []notify.PreviousNotification) []int64 {
	covered := distilledSet(kf)
	var added []int64
	for _, n := range dismissed {
		if !isDistillable(n) || covered[n.ID] {
			continue
		}
		kf.DistilledInsights = append(kf.DistilledInsights, n.ID)
		covered[n.ID] = true // avoid dupes within this batch
		added = append(added, n.ID)
	}
	return added
}

func cmdDistillApply() {
	cfg := loadConfig()

	data, err := io.ReadAll(os.Stdin)
	fatal(err)

	result, warnings, err := notify.ParseAndValidateDistilled(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: LLM distillation validation failed: %v\n", err)
		os.Exit(1)
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
	}

	// Fetch dismissed insights once — for provenance attribution (the
	// Link↔gmail_search join: a pattern's gmail_search is, by the distill prompt's
	// contract, the insight's Link, so NormalizeSearchKey(Link) ==
	// NormalizeSearchKey(gmail_search) joins them deterministically, no LLM) and
	// for the iter-023 distilled-ledger. dsh stays nil when DSH isn't configured.
	var dsh *notify.DSHClient
	var dismissed []notify.PreviousNotification
	insightByKey := map[string][]int64{}
	if err := cfg.Analysis.Validate(); err == nil {
		dsh = notify.NewDSHClient(cfg.Analysis.DSH)
		if d, derr := dsh.GetDismissedNotifications("GML", 200, true); derr == nil {
			dismissed = d
			for _, n := range dismissed {
				if n.Link == "" {
					continue
				}
				k := notify.InsightDedupKey(n.Link)
				insightByKey[k] = append(insightByKey[k], n.ID)
			}
		} else {
			fmt.Fprintf(os.Stderr, "  warning: could not fetch insights for provenance: %v\n", derr)
		}
	}

	// Load knowledge once; update with this cycle's patterns and the distilled
	// ledger, then save once.
	kf, err := knowledge.Load("data/knowledge.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading knowledge.yaml: %v\n", err)
		os.Exit(1)
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
			fmt.Fprintf(os.Stderr, "  [pattern:%s] %s (gmail: %s)%s\n", p.Status, p.Pattern, p.GmailSearch, src)
		}
		kf.LastDistilledAt = knowledge.Now()
		changed = true
	}

	// Distilled-ledger (iter 023): record the residual-gap insights this cycle
	// covered that pattern provenance can't — runs even on an empty result, so a
	// distilled-to-nothing insight is still recorded and never re-fed. Patterns are
	// upserted above first, so pattern-producing insights are excluded.
	if added := appendDistilledLedger(kf, dismissed); len(added) > 0 {
		for _, id := range added {
			fmt.Fprintf(os.Stderr, "  [ledger] recorded insight #%d distilled (residual-gap)\n", id)
		}
		changed = true
	}

	if changed {
		if err := knowledge.Save("data/knowledge.yaml", kf); err != nil {
			fmt.Fprintf(os.Stderr, "error saving knowledge.yaml: %v\n", err)
			os.Exit(1)
		}
	}
	if len(result.Patterns) > 0 {
		fmt.Fprintf(os.Stderr, "  %d patterns written to knowledge.yaml\n", len(result.Patterns))
	}

	if len(result.Todos) > 0 {
		if dsh == nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", cfg.Analysis.Validate())
			os.Exit(1)
		}
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
				fmt.Fprintf(os.Stderr, "error posting todo: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "  [todo:%s] %s\n", priority, text)
		}
		fmt.Fprintf(os.Stderr, "  %d todos posted to DSH\n", len(result.Todos))
	}

	if len(result.Patterns) == 0 && len(result.Todos) == 0 {
		fmt.Fprintln(os.Stderr, "no patterns or todos to distill (empty result)")
	}
	fmt.Fprintf(os.Stderr, "done: %d patterns + %d todos\n", len(result.Patterns), len(result.Todos))
}

// formatInsightSuffix renders source insight IDs as a compact human+machine
// readable back-link appended to a todo's text, e.g. "(insight #12)" or
// "(insights #12, #15)".
func formatInsightSuffix(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("#%d", id)
	}
	label := "insight"
	if len(ids) > 1 {
		label = "insights"
	}
	return "(" + label + " " + strings.Join(parts, ", ") + ")"
}

func cmdPropose(args []string) {
	jsonOut := hasFlag(args, "--json")
	cfg := loadConfig()

	kf, err := knowledge.Load("data/knowledge.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading knowledge.yaml: %v\n", err)
		os.Exit(1)
	}
	if len(kf.Patterns) == 0 {
		fmt.Fprintln(os.Stderr, "no patterns in knowledge.yaml — nothing to propose")
		return
	}
	fmt.Fprintf(os.Stderr, "  loaded %d knowledge patterns, %d existing rules\n", len(kf.Patterns), len(cfg.Rules))

	result := propose.Generate(kf, cfg)

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		fatal(enc.Encode(result))
		fmt.Fprintf(os.Stderr, "done: %d proposals, %d skipped\n", len(result.Proposals), len(result.Skipped))
		return
	}

	if len(result.Proposals) == 0 {
		fmt.Fprintln(os.Stderr, "no new proposals (all patterns already covered or skipped)")
		for _, s := range result.Skipped {
			fmt.Fprintf(os.Stderr, "  skipped: %s — %s\n", s.Pattern, s.Reason)
		}
		return
	}

	if err := cfg.Analysis.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v (needed to post plans to DSH)\n", err)
		os.Exit(1)
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)

	existingPlans, err := dsh.GetPlans("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch existing plans for dedup: %v\n", err)
	}
	survivors, structSkipped := structuralDedup(result.Proposals, existingPlans)

	posted := 0
	for _, p := range survivors {
		detail, _ := json.Marshal(p)
		id, err := dsh.PostPlan("GML", p.ProposedRule.Name+" ("+p.ProposedRule.Type+")", string(detail))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error posting plan: %v\n", err)
			os.Exit(1)
		}
		posted++
		fmt.Fprintf(os.Stderr, "  posted plan #%d: %s\n", id, p.ProposedRule.Name)
	}
	fmt.Fprintf(os.Stderr, "done: %d plans posted, %d skipped (existing plan or rule)\n", posted, structSkipped+len(result.Skipped))
	if posted > 0 {
		fmt.Fprintln(os.Stderr, "  next: review in DSH → Plans, then run: ./run-task.sh apply-rules")
	}
}

type planDedupKey struct {
	sender       string
	filter       string
	requireReply bool
}

// structuralDedup drops proposals already covered by an active (non-rejected)
// DSH plan. It applies two deterministic keys: (1) the source-insight set — a
// candidate whose source insight #IDs are all already represented by an existing
// plan is a re-proposal of the same provenance (robust even when a folded plan's
// filter no longer matches the raw candidate's); (2) the structural floor —
// sender + CanonicalQuery(filter) + require_reply, for provenance-less artifacts.
// Rejected plans do not block re-proposals. Returns survivors and count skipped.
func structuralDedup(proposals []propose.Proposal, existingPlans []notify.Plan) ([]propose.Proposal, int) {
	plannedKeys := make(map[planDedupKey]bool)
	coveredInsights := make(map[int64]bool)
	for _, plan := range existingPlans {
		if plan.Status == "rejected" {
			continue
		}
		var p propose.Proposal
		if err := json.Unmarshal([]byte(plan.Detail), &p); err == nil {
			for _, s := range p.ProposedRule.Params.Patterns {
				plannedKeys[planDedupKey{
					sender:       strings.ToLower(s),
					filter:       propose.CanonicalQuery(p.ProposedRule.Params.Filter),
					requireReply: p.ProposedRule.Params.RequireReply,
				}] = true
			}
			for _, id := range p.SourceInsights {
				coveredInsights[id] = true
			}
		}
	}

	var survivors []propose.Proposal
	skipped := 0
	for _, p := range proposals {
		// (1) provenance: all source insights already represented by a live plan.
		if len(p.SourceInsights) > 0 {
			allCovered := true
			for _, id := range p.SourceInsights {
				if !coveredInsights[id] {
					allCovered = false
					break
				}
			}
			if allCovered {
				skipped++
				fmt.Fprintf(os.Stderr, "  skipped (insight provenance covered %v): %s\n", p.SourceInsights, p.ProposedRule.Name)
				continue
			}
		}

		// (2) structural floor: senders all covered with the same filter+rr.
		allCovered := true
		for _, s := range p.ProposedRule.Params.Patterns {
			key := planDedupKey{
				sender:       strings.ToLower(s),
				filter:       propose.CanonicalQuery(p.ProposedRule.Params.Filter),
				requireReply: p.ProposedRule.Params.RequireReply,
			}
			if !plannedKeys[key] {
				allCovered = false
				break
			}
		}
		if allCovered {
			skipped++
			fmt.Fprintf(os.Stderr, "  skipped (plan exists): %s\n", p.ProposedRule.Name)
			continue
		}
		survivors = append(survivors, p)
	}
	return survivors, skipped
}

// formatExistingPlans renders active (non-rejected) plans as compact JSON for
// the semantic dedup prompt's <existing_plans> context.
func formatExistingPlans(plans []notify.Plan) string {
	type planSummary struct {
		ID           int64    `json:"id"`
		Title        string   `json:"title"`
		Status       string   `json:"status"`
		RuleName     string   `json:"rule_name,omitempty"`
		RuleType     string   `json:"rule_type,omitempty"`
		Senders      []string `json:"senders,omitempty"`
		Filter       string   `json:"filter,omitempty"`
		RequireReply bool     `json:"require_reply,omitempty"`
	}
	var summaries []planSummary
	for _, plan := range plans {
		if plan.Status == "rejected" {
			continue
		}
		var p propose.Proposal
		_ = json.Unmarshal([]byte(plan.Detail), &p)
		summaries = append(summaries, planSummary{
			ID:           plan.ID,
			Title:        plan.Title,
			Status:       plan.Status,
			RuleName:     p.ProposedRule.Name,
			RuleType:     p.ProposedRule.Type,
			Senders:      p.ProposedRule.Params.Patterns,
			Filter:       p.ProposedRule.Params.Filter,
			RequireReply: p.ProposedRule.Params.RequireReply,
		})
	}
	if len(summaries) == 0 {
		return ""
	}
	b, _ := json.MarshalIndent(summaries, "", "  ")
	return string(b)
}

// cmdProposeGather is step 1 of the LLM-gated propose flow: Generate proposals,
// apply the deterministic structural dedup, and emit a semantic-dedup prompt
// for whatever survives. Emits nothing on stdout when there is no work, so the
// shell can skip the LLM and apply steps.
func cmdProposeGather() {
	cfg := loadConfig()

	kf, err := knowledge.Load("data/knowledge.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading knowledge.yaml: %v\n", err)
		os.Exit(1)
	}
	if len(kf.Patterns) == 0 {
		fmt.Fprintln(os.Stderr, "no patterns in knowledge.yaml — nothing to propose")
		return
	}

	result := propose.Generate(kf, cfg)
	if len(result.Proposals) == 0 {
		fmt.Fprintln(os.Stderr, "no new proposals (all patterns already covered or skipped)")
		return
	}

	if err := cfg.Analysis.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v (needed to reach DSH)\n", err)
		os.Exit(1)
	}
	dsh := notify.NewDSHClient(cfg.Analysis.DSH)
	existingPlans, err := dsh.GetPlans("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch existing plans for dedup: %v\n", err)
	}

	survivors, structSkipped := structuralDedup(result.Proposals, existingPlans)
	fmt.Fprintf(os.Stderr, "  %d candidate(s) after structural dedup (%d skipped as exact duplicates)\n", len(survivors), structSkipped)
	if len(survivors) == 0 {
		fmt.Fprintln(os.Stderr, "no candidates survive structural dedup — nothing for the semantic gate")
		return
	}

	candidatesJSON, _ := json.MarshalIndent(survivors, "", "  ")
	out := prompt.BuildProposeReconcile(string(candidatesJSON), formatExistingPlans(existingPlans), formatExistingRules(cfg.Rules))
	fmt.Print(out)
	fmt.Fprintf(os.Stderr, "done: reconcile prompt for %d candidate(s)\n", len(survivors))
}

// formatExistingRules renders the applied archive_by_sender rules from rules.yaml
// as compact JSON for the reconcile prompt's <existing_rules> context, so the
// gate folds candidates against the live archiving reality, not just DSH plans.
func formatExistingRules(rules []config.Rule) string {
	type ruleSummary struct {
		Name         string   `json:"name"`
		Type         string   `json:"type"`
		Senders      []string `json:"senders,omitempty"`
		Filter       string   `json:"filter,omitempty"`
		RequireReply bool     `json:"require_reply,omitempty"`
	}
	var summaries []ruleSummary
	for _, r := range rules {
		if r.Type != "archive_by_sender" {
			continue
		}
		summaries = append(summaries, ruleSummary{
			Name:         r.Name,
			Type:         r.Type,
			Senders:      r.Params.Patterns,
			Filter:       r.Params.Filter,
			RequireReply: r.Params.RequireReply,
		})
	}
	if len(summaries) == 0 {
		return ""
	}
	b, _ := json.MarshalIndent(summaries, "", "  ")
	return string(b)
}

// cmdProposeApply is step 3 of the LLM-gated propose flow: read the candidates
// the semantic gate chose to KEEP, re-run structural dedup as a safety net
// (guards against the LLM echoing an already-covered candidate), and post the
// survivors as DSH plans.
func cmdProposeApply() {
	cfg := loadConfig()
	if err := cfg.Analysis.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading stdin: %v\n", err)
		os.Exit(1)
	}
	if len(raw) == 0 {
		fmt.Fprintln(os.Stderr, "error: empty input — expected kept-proposals JSON on stdin")
		os.Exit(1)
	}

	kept, err := propose.ParseProposals(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)
	existingPlans, err := dsh.GetPlans("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch existing plans for safety re-check: %v\n", err)
	}
	survivors, structSkipped := structuralDedup(kept, existingPlans)

	posted := 0
	for _, p := range survivors {
		detail, _ := json.Marshal(p)
		id, err := dsh.PostPlan("GML", p.ProposedRule.Name+" ("+p.ProposedRule.Type+")", string(detail))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error posting plan: %v\n", err)
			os.Exit(1)
		}
		posted++
		if supersedes := parseConflictPlanIDs(p.KnowledgeRef); len(supersedes) > 0 {
			fmt.Fprintf(os.Stderr, "  posted plan #%d: %s (folded — supersedes %v)\n", id, p.ProposedRule.Name, supersedes)
		} else {
			fmt.Fprintf(os.Stderr, "  posted plan #%d: %s\n", id, p.ProposedRule.Name)
		}
	}
	fmt.Fprintf(os.Stderr, "done: %d plans posted, %d skipped (structural re-check)\n", posted, structSkipped)
	if posted > 0 {
		fmt.Fprintln(os.Stderr, "  next: review in DSH → Plans, then run: ./run-task.sh apply-rules")
	}
}

func cmdMergePlansGather() {
	cfg := loadConfig()
	if err := cfg.Analysis.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)
	plans, err := dsh.GetPlans("approved")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching approved plans: %v\n", err)
		os.Exit(1)
	}

	if len(plans) == 0 {
		fmt.Fprintln(os.Stderr, "no approved plans in DSH — nothing to merge")
		return
	}

	// First pass: find plans superseded by approved conflict resolutions.
	// A conflict resolution plan's KnowledgeRef encodes the IDs it replaces:
	//   "merge_conflict:[11 12]" means it supersedes plans 11 and 12.
	superseded := make(map[int64]bool)
	for _, plan := range plans {
		var p propose.Proposal
		if err := json.Unmarshal([]byte(plan.Detail), &p); err != nil {
			continue
		}
		ids := parseConflictPlanIDs(p.KnowledgeRef)
		for _, id := range ids {
			superseded[id] = true
		}
	}

	var mergePlans []prompt.MergePlan
	for _, plan := range plans {
		if superseded[plan.ID] {
			fmt.Fprintf(os.Stderr, "  skipping plan #%d (superseded by conflict resolution)\n", plan.ID)
			continue
		}
		var p propose.Proposal
		if err := json.Unmarshal([]byte(plan.Detail), &p); err != nil {
			fmt.Fprintf(os.Stderr, "  skipping plan #%d: invalid detail JSON: %v\n", plan.ID, err)
			continue
		}
		if p.ProposedRule.Name == "" {
			fmt.Fprintf(os.Stderr, "  skipping plan #%d: no rule name (unresolved conflict?)\n", plan.ID)
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
		fmt.Fprintf(os.Stderr, "  plan #%d: %s\n", plan.ID, plan.Title)
	}

	if len(mergePlans) == 0 {
		fmt.Fprintln(os.Stderr, "no valid plans to merge")
		return
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

	kf, _ := knowledge.Load("data/knowledge.yaml")
	knowledgeCtx := ""
	if kf != nil {
		knowledgeCtx = knowledge.Format(kf)
	}

	p := prompt.BuildMergePlans(mergePlans, existingRules, knowledgeCtx)
	fmt.Print(p)
	fmt.Fprintf(os.Stderr, "done: merge prompt for %d plans\n", len(mergePlans))
}

func cmdMergePlansApply() {
	cfg := loadConfig()
	if err := cfg.Analysis.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading stdin: %v\n", err)
		os.Exit(1)
	}
	if len(raw) == 0 {
		fmt.Fprintln(os.Stderr, "error: empty input — expected LLM merge JSON on stdin")
		os.Exit(1)
	}

	result, err := propose.ParseAndValidateMerge(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	dsh := notify.NewDSHClient(cfg.Analysis.DSH)

	// Fetch plan IDs for completeness check (excluding superseded plans)
	plans, err := dsh.GetPlans("approved")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch plans for completeness check: %v\n", err)
	} else {
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
		var planIDs []int64
		for _, p := range plans {
			if !superseded[p.ID] {
				planIDs = append(planIDs, p.ID)
			}
		}
		if err := propose.ValidateMergeCompleteness(result, planIDs); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
	}

	// Dedup: collect conflict keys already posted as pending/approved plans
	existingConflictKeys := make(map[string]bool)
	allPlans, err := dsh.GetPlans("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch plans for conflict dedup: %v\n", err)
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

	// Post conflict resolution plans to DSH (skip duplicates)
	posted, skipped := 0, 0
	for _, conflict := range result.Conflicts {
		key := conflictPlanKey(conflict.AffectedPlanIDs)
		if existingConflictKeys[key] {
			skipped++
			fmt.Fprintf(os.Stderr, "  skipped (conflict plan exists): %s\n", truncate(conflict.Description, 80))
			continue
		}

		title := fmt.Sprintf("merge conflict: %s", truncate(conflict.Description, 80))
		detail := propose.FormatConflictPlanDetail(conflict)
		id, err := dsh.PostPlan("GML", title, detail)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error posting conflict plan: %v\n", err)
			os.Exit(1)
		}
		posted++
		fmt.Fprintf(os.Stderr, "  conflict → plan #%d: %s\n", id, conflict.Description)
	}

	// Build rules from non-conflicting merges
	annotated := propose.MergeResultToAnnotatedRules(result)
	annotated = guardSameSender(annotated)

	if len(annotated) == 0 {
		fmt.Fprintln(os.Stderr, "no non-conflicting rules to apply")
		if len(result.Conflicts) > 0 {
			fmt.Fprintf(os.Stderr, "  %d conflicts posted to DSH as plans for review\n", len(result.Conflicts))
		}
		return
	}

	rulesPath := os.Getenv("GML_RULES")
	if rulesPath == "" {
		rulesPath = "data/rules.yaml"
	}
	data, err := os.ReadFile(rulesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", rulesPath, err)
		os.Exit(1)
	}

	output := propose.BuildGeneratedRules(string(data), annotated)
	fmt.Print(output)
	fmt.Fprintf(os.Stderr, "done: %d rules merged, %d conflicts posted, %d skipped (dedup)\n", len(annotated), posted, skipped)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func conflictPlanKey(ids []int64) string {
	sorted := make([]int64, len(ids))
	copy(sorted, ids)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	parts := make([]string, len(sorted))
	for i, id := range sorted {
		parts[i] = fmt.Sprintf("%d", id)
	}
	return "merge_conflict:[" + strings.Join(parts, " ") + "]"
}

func parseConflictPlanIDs(knowledgeRef string) []int64 {
	if !strings.HasPrefix(knowledgeRef, "merge_conflict:[") {
		return nil
	}
	inner := strings.TrimPrefix(knowledgeRef, "merge_conflict:[")
	inner = strings.TrimSuffix(inner, "]")
	var ids []int64
	for _, s := range strings.Fields(inner) {
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}


// guardSameSender withholds the OR-union footgun (≥2 archive_by_sender rules for
// one sender with different filters → archives ~everything) from rules.yaml and
// reports what it withheld.
func guardSameSender(rules []propose.AnnotatedRule) []propose.AnnotatedRule {
	safe, withheld := propose.GuardSameSender(rules)
	for sender, filters := range withheld {
		fmt.Fprintf(os.Stderr, "  WITHHELD %s — %d conflicting filters not applied (fold into one rule): %s\n",
			sender, len(filters), strings.Join(filters, " | "))
	}
	return safe
}

func loadConfig() *config.Config {
	rulesPath := os.Getenv("GML_RULES")
	if rulesPath == "" {
		rulesPath = "data/rules.yaml"
	}
	cfg, err := config.Load(rulesPath)
	fatal(err)

	dshPath := os.Getenv("GML_DSH")
	if dshPath == "" {
		dshPath = "data/dsh.yaml"
	}
	if dsh, err := config.LoadDSH(dshPath); err == nil {
		cfg.Analysis.DSH = *dsh
	} else if cfg.Analysis.DSH.URL != "" {
		fmt.Fprintf(os.Stderr, "warning: %s not found, using DSH config from rules.yaml\n", dshPath)
	}

	return cfg
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func intFlag(args []string, flag string, def int) int {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			var n int
			if _, err := fmt.Sscan(args[i+1], &n); err == nil {
				return n
			}
		}
	}
	return def
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
