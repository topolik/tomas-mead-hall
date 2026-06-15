package scheduler

import (
	"log"
	"time"

	"github.com/topolik/gml-gmail-agent/internal/config"
	"github.com/topolik/gml-gmail-agent/internal/creds"
	"github.com/topolik/gml-gmail-agent/internal/gws"
	"github.com/topolik/gml-gmail-agent/internal/rules"
)

// Run starts the scheduler and blocks until the process exits.
// Rules are run once immediately on startup, then on the configured interval.
// When rulesPath is non-empty, rules.yaml is reloaded at the start of each tick
// so a regenerated file (e.g. by the knowledge cycle's apply-rules step) takes
// effect without restarting the daemon; a read error keeps the last good config.
func Run(cfg *config.Config, cr *creds.Creds, intervalOverride int, rulesPath string) {
	interval := intervalOverride
	if interval <= 0 {
		interval = cfg.Schedule.IntervalMinutes
	}
	if interval <= 0 {
		interval = 5
	}

	dryRun := cfg.ReadOnly()
	if dryRun {
		log.Printf("[scheduler] readonly mode — rules will run but no messages will be archived")
	}

	var tracingLabelID string
	if !dryRun {
		labelID, err := gws.EnsureTracingLabel(cr, rules.TracingLabelName)
		if err != nil {
			log.Printf("[scheduler] warning: tracing labels unavailable: %v", err)
		} else {
			tracingLabelID = labelID
			log.Printf("[scheduler] tracing label %q ready (id: %s)", rules.TracingLabelName, labelID)
		}
	}

	log.Printf("[scheduler] starting — interval %d min, rules: %d", interval, len(cfg.Rules))

	runOnce(cfg, cr, dryRun, tracingLabelID, interval)

	ticker := time.NewTicker(time.Duration(interval) * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		if rulesPath != "" {
			if fresh, err := config.Load(rulesPath); err != nil {
				log.Printf("[scheduler] rules reload failed, keeping previous config: %v", err)
			} else {
				if len(fresh.Rules) != len(cfg.Rules) {
					log.Printf("[scheduler] rules reloaded: %d → %d rules", len(cfg.Rules), len(fresh.Rules))
				}
				cfg = fresh
				dryRun = cfg.ReadOnly()
			}
		}
		runOnce(cfg, cr, dryRun, tracingLabelID, interval)
	}
}

func runOnce(cfg *config.Config, cr *creds.Creds, dryRun bool, tracingLabelID string, interval int) {
	log.Printf("[scheduler] running rules")
	sinceHours := cfg.Schedule.EffectiveLookbackHours(interval)
	result, err := rules.RunWithSenderFilter(cfg, cr, dryRun, 5, sinceHours, tracingLabelID)
	if err != nil {
		log.Printf("[scheduler] error: %v", err)
		return
	}
	for _, a := range result.Actions {
		verb := "archived"
		if !a.Archived {
			verb = "would archive"
		}
		log.Printf("[scheduler] [%s] [%s] %s — %s (%s)", verb, a.RuleName, a.From, a.Subject, a.Reason)
	}
	for _, e := range result.Errors {
		log.Printf("[scheduler] rule error: %s", e)
	}
	log.Printf("[scheduler] done — %d actions, %d errors", len(result.Actions), len(result.Errors))
}
