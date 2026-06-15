// Package extract walks the Claude and Gemini session trees, filters noise,
// dedups overlapping checkpoints, redacts secrets, and produces the
// moments.jsonl corpus.
package extract

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/topolik/mnd-mind-model/internal/claude"
	"github.com/topolik/mnd-mind-model/internal/exclude"
	"github.com/topolik/mnd-mind-model/internal/gemini"
	"github.com/topolik/mnd-mind-model/internal/moment"
	"github.com/topolik/mnd-mind-model/internal/redact"
)

const (
	ContextBudget = 700  // chars of assistant context per moment
	maxTextLen    = 4000 // chars of user text per moment
)

// Stats reports what a run saw, for the run log.
type Stats struct {
	ClaudeFiles, GeminiChatFiles, GeminiLogFiles int
	Kept, DroppedNoise, DroppedDup, DroppedSelf  int
}

// Options tunes a Run beyond the source dirs.
type Options struct {
	LedgerPath            string   // orchestrator send-ledger (exclude.Ledger)
	ExcludeGeminiProjects []string // pipeline working dirs in ~/.gemini/tmp
}

// Run extracts from both trees. Either dir may be "" to skip that source.
func Run(claudeDir, geminiDir string, opts Options) ([]moment.Moment, Stats, error) {
	var all []moment.Moment
	var st Stats

	ledger, err := exclude.LoadLedger(opts.LedgerPath)
	if err != nil {
		return nil, st, fmt.Errorf("ledger: %w", err)
	}

	if claudeDir != "" {
		if err := walkClaude(claudeDir, &all, &st); err != nil {
			return nil, st, fmt.Errorf("claude: %w", err)
		}
	}
	if geminiDir != "" {
		if err := walkGemini(geminiDir, opts.ExcludeGeminiProjects, &all, &st); err != nil {
			return nil, st, fmt.Errorf("gemini: %w", err)
		}
	}

	all = finalize(all, ledger, &st)
	return all, st, nil
}

func walkClaude(root string, all *[]moment.Moment, st *Stats) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return err
		}
		project := filepath.Base(filepath.Dir(path))
		ms, perr := claude.ParseFile(path, project, ContextBudget)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "warn: %s: %v\n", path, perr)
			return nil
		}
		st.ClaudeFiles++
		*all = append(*all, ms...)
		return nil
	})
}

func walkGemini(root string, excluded []string, all *[]moment.Moment, st *Stats) error {
	projects, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	skip := make(map[string]bool, len(excluded))
	for _, e := range excluded {
		skip[strings.ToLower(strings.TrimSpace(e))] = true
	}
	for _, p := range projects {
		if !p.IsDir() || skip[strings.ToLower(p.Name())] {
			continue
		}
		projDir := filepath.Join(root, p.Name())
		chatsDir := filepath.Join(projDir, "chats")

		if entries, err := os.ReadDir(chatsDir); err == nil && len(entries) > 0 {
			for _, e := range entries {
				if e.IsDir() || !strings.HasPrefix(e.Name(), "session-") || !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				ms, perr := gemini.ParseChatFile(filepath.Join(chatsDir, e.Name()), p.Name(), ContextBudget)
				if perr != nil {
					fmt.Fprintf(os.Stderr, "warn: %s: %v\n", filepath.Join(chatsDir, e.Name()), perr)
					continue
				}
				st.GeminiChatFiles++
				*all = append(*all, ms...)
			}
			continue // chats are authoritative; logs.json would only duplicate
		}

		logsPath := filepath.Join(projDir, "logs.json")
		if _, err := os.Stat(logsPath); err == nil {
			ms, perr := gemini.ParseLogsFile(logsPath, p.Name())
			if perr != nil {
				fmt.Fprintf(os.Stderr, "warn: %s: %v\n", logsPath, perr)
				continue
			}
			st.GeminiLogFiles++
			*all = append(*all, ms...)
		}
	}
	return nil
}

// finalize self-filters (pipeline content, orchestrator-sent directions —
// T22-T24), noise-filters, dedups (checkpoint files repeat messages — T8;
// templated loops repeat texts with fresh timestamps — T8b), redacts, caps
// lengths, and orders the corpus chronologically.
func finalize(in []moment.Moment, ledger *exclude.Ledger, st *Stats) []moment.Moment {
	seen := make(map[string]bool, len(in))
	seenText := make(map[string]bool, len(in))
	out := make([]moment.Moment, 0, len(in))
	for _, m := range in {
		if exclude.IsPipelineContent(m.Text) || ledger.Contains(m.Text) {
			st.DroppedSelf++
			continue
		}
		if seen[m.ID] || seenText[nearDupKey(m.Text)] {
			st.DroppedDup++
			continue
		}
		seen[m.ID] = true
		seenText[nearDupKey(m.Text)] = true
		if IsNoise(m.Text) {
			st.DroppedNoise++
			continue
		}
		m.Text = redact.String(truncate(m.Text, maxTextLen))
		m.Context = redact.String(m.Context)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	st.Kept = len(out)
	return out
}

// decision words that rescue short messages — a terse "no, use yaml" is
// direction signal; a bare "continue" is not.
var decisionWords = []string{
	"no", "not", "don't", "dont", "wrong", "instead", "use", "prefer",
	"keep", "must", "should", "need", "want", "fix", "stop", "remove",
	"simple", "kiss", "but", "why", "priorit", "important", "q1", "q2", "q3", "q4",
}

// machine-fed payloads that arrive as "user" turns (T7b): piped file dumps
// from batch scripts, harness limit notices. Real-run findings 2026-06-12.
var machinePrefixes = []string{
	"--- FILE:",
	"You have exceeded the maximum number of turns",
}

// IsNoise drops moments with no reusable decision signal (T7, T7b).
func IsNoise(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || strings.HasPrefix(t, "/") {
		return true
	}
	for _, p := range machinePrefixes {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	if len(t) >= 40 {
		return false
	}
	for _, w := range decisionWords {
		for _, tok := range strings.FieldsFunc(t, func(r rune) bool {
			return !('a' <= r && r <= 'z' || '0' <= r && r <= '9' || r == '\'')
		}) {
			if tok == w || (len(w) > 4 && strings.HasPrefix(tok, w)) {
				return false
			}
		}
	}
	return true
}

// nearDupKey collapses templated repeats: normalized first 200 chars (T8b).
func nearDupKey(s string) string {
	norm := strings.Join(strings.Fields(strings.ToLower(s)), " ")
	if len(norm) > 200 {
		norm = norm[:200]
	}
	return norm
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && (cut[len(cut)-1]&0xC0) == 0x80 {
		cut = cut[:len(cut)-1]
	}
	return cut + "…[truncated]"
}
