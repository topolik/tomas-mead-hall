package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/topolik/gml-gmail-agent/internal/config"
	"github.com/topolik/gml-gmail-agent/internal/creds"
	"github.com/topolik/gml-gmail-agent/internal/llm"
)

func cmdWatchAnalysis(lc *llm.Client, cr *creds.Creds, cfg *config.Config, args []string) {
	model := stringFlag(args, "--model", "gemini")
	intervalOverride := intFlag(args, "--interval", 0)

	interval := intervalOverride
	if interval <= 0 {
		interval = cfg.Analysis.EffectiveScheduleMinutes()
	}
	stateFile := dataDir() + "/.gml-last-analysis"

	fmt.Fprintf(os.Stderr, "=== GML Watch-Analysis: analyzing every %d minutes (model: %s) ===\n", interval, model)
	fmt.Fprintln(os.Stderr, "[watch-analysis] Ctrl+C to stop, or use ./watch.sh to manage daemons")

	for {
		fmt.Fprintf(os.Stderr, "\n[watch-analysis] %s starting analysis...\n", time.Now().Format("2006-01-02 15:04:05"))
		window := analysisWindow(cfg, interval, stateFile)
		timeFilter := fmt.Sprintf("newer_than:%dh", (window+59)/60)

		if err := runAnalyze(lc, cr, cfg, model, timeFilter); err != nil {
			fmt.Fprintf(os.Stderr, "[watch-analysis] %s failed: %v — will retry in %d minutes (window will expand to catch up)\n",
				time.Now().Format("2006-01-02 15:04:05"), err, interval)
		} else {
			os.WriteFile(stateFile, []byte(fmt.Sprintf("%d", time.Now().Unix())), 0644)
			fmt.Fprintf(os.Stderr, "[watch-analysis] %s success — next run in %d minutes\n",
				time.Now().Format("2006-01-02 15:04:05"), interval)
		}
		time.Sleep(time.Duration(interval) * time.Minute)
	}
}

func analysisWindow(cfg *config.Config, interval int, stateFile string) int {
	window := interval
	maxDays := cfg.Analysis.EffectiveMaxDays()
	maxWindow := maxDays * 24 * 60

	data, err := os.ReadFile(stateFile)
	if err != nil {
		days := cfg.Analysis.EffectiveDays()
		window = days * 24 * 60
		fmt.Fprintf(os.Stderr, "[watch-analysis] first run — using %d-day initial window\n", days)
	} else {
		ts := string(data)
		if lastTs, err := strconv.ParseInt(ts, 10, 64); err == nil && lastTs > 0 {
			elapsed := int((time.Now().Unix() - lastTs) / 60)
			if elapsed > window {
				window = elapsed
				fmt.Fprintf(os.Stderr, "[watch-analysis] catching up: %d minutes since last success (interval: %dm)\n", elapsed, interval)
			}
		} else {
			days := cfg.Analysis.EffectiveDays()
			window = days * 24 * 60
			fmt.Fprintln(os.Stderr, "[watch-analysis] invalid state file — treating as first run")
		}
	}

	if window > maxWindow {
		fmt.Fprintf(os.Stderr, "[watch-analysis] clamping window to %d-day max (%d minutes)\n", maxDays, maxWindow)
		window = maxWindow
	}
	return window
}

func cmdWatchKnowledge(lc *llm.Client, cr *creds.Creds, cfg *config.Config, args []string) {
	model := stringFlag(args, "--model", "gemini")
	intervalOverride := intFlag(args, "--interval", 0)

	interval := intervalOverride
	if interval <= 0 {
		interval = cfg.Analysis.Learn.EffectiveKnowledgeIntervalMinutes()
	}
	stateFile := dataDir() + "/.gml-last-knowledge"

	fmt.Fprintf(os.Stderr, "=== GML Watch-Knowledge: learn+distill+propose+apply every %d minutes (model: %s) ===\n", interval, model)
	fmt.Fprintln(os.Stderr, "[watch-knowledge] Ctrl+C to stop, or use ./watch.sh to manage daemons")

	for {
		fmt.Fprintf(os.Stderr, "\n[watch-knowledge] %s starting knowledge pipeline...\n", time.Now().Format("2006-01-02 15:04:05"))

		fmt.Fprintln(os.Stderr, "[watch-knowledge] step 1/4: learn...")
		if err := runLearn(lc, cr, cfg, model, cfg.Analysis.Learn.EffectiveDays()); err != nil {
			fmt.Fprintf(os.Stderr, "[watch-knowledge] learn failed: %v — continuing with distill\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "[watch-knowledge] learn succeeded")
		}

		fmt.Fprintln(os.Stderr, "[watch-knowledge] step 2/4: distill...")
		if err := runDistill(lc, cfg, model); err != nil {
			fmt.Fprintf(os.Stderr, "[watch-knowledge] distill failed: %v — continuing with propose\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "[watch-knowledge] distill succeeded")
		}

		fmt.Fprintln(os.Stderr, "[watch-knowledge] step 3/4: propose (LLM-gated, folds per sender)...")
		if err := runPropose(lc, cfg, model); err != nil {
			fmt.Fprintf(os.Stderr, "[watch-knowledge] propose failed: %v — continuing with apply\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "[watch-knowledge] propose succeeded")
		}

		fmt.Fprintln(os.Stderr, "[watch-knowledge] step 4/4: apply-rules (deterministic: approved plans → rules.yaml)...")
		if err := runApplyRulesDeterministic(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "[watch-knowledge] apply-rules failed: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "[watch-knowledge] apply-rules succeeded")
		}

		os.WriteFile(stateFile, []byte(fmt.Sprintf("%d", time.Now().Unix())), 0644)
		fmt.Fprintf(os.Stderr, "[watch-knowledge] %s pipeline complete — next run in %d minutes\n",
			time.Now().Format("2006-01-02 15:04:05"), interval)
		time.Sleep(time.Duration(interval) * time.Minute)
	}
}
