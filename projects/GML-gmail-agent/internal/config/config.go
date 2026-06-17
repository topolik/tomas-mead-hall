package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Mode     string         `yaml:"mode"` // "readonly" (default) or "readwrite"
	Schedule ScheduleConfig `yaml:"schedule"`
	Rules    []Rule         `yaml:"rules"`
	Analysis AnalysisConfig `yaml:"analysis"`
}

type ScheduleConfig struct {
	IntervalMinutes int `yaml:"interval_minutes"`
	// LookbackHours bounds how far back the rules daemon archives each run. It is
	// decoupled from the tick interval on purpose: the interval is about not
	// missing mail between ticks, while the look-back must cover how long mail can
	// sit before a freshly-approved rule goes live (review + approval latency,
	// including weekends). Default 72h (3 days). The effective window never drops
	// below 2× the interval (the gap-avoidance floor).
	LookbackHours int `yaml:"lookback_hours"`
}

// EffectiveLookbackHours returns the configured rules look-back (default 72h /
// 3 days), floored at 2× the tick interval so a long interval can't create a
// gap between runs. intervalMinutes is the resolved daemon interval.
func (s ScheduleConfig) EffectiveLookbackHours(intervalMinutes int) int {
	lookback := s.LookbackHours
	if lookback <= 0 {
		lookback = 72
	}
	floor := (intervalMinutes*2 + 59) / 60
	if floor > lookback {
		return floor
	}
	return lookback
}

type AnalysisConfig struct {
	Days            int         `yaml:"days"`
	MaxDays         int         `yaml:"max_days"`
	ScheduleMinutes int         `yaml:"schedule_minutes"`
	DSH             DSHConfig   `yaml:"dsh"`
	Learn           LearnConfig `yaml:"learn"`
}

type LearnConfig struct {
	Days                     int `yaml:"days"`
	MaxDays                  int `yaml:"max_days"`
	TopSenders               int `yaml:"top_senders"`
	MinEmails                int `yaml:"min_emails"`
	KnowledgeIntervalMinutes int `yaml:"knowledge_interval_minutes"`
}

func (l *LearnConfig) EffectiveDays() int {
	if l.Days <= 0 {
		return 30
	}
	return l.Days
}

func (l *LearnConfig) EffectiveMaxDays() int {
	if l.MaxDays <= 0 {
		return 90
	}
	return l.MaxDays
}

func (l *LearnConfig) EffectiveTopSenders() int {
	if l.TopSenders <= 0 {
		return 30
	}
	return l.TopSenders
}

func (l *LearnConfig) EffectiveMinEmails() int {
	if l.MinEmails <= 0 {
		return 3
	}
	return l.MinEmails
}

func (l *LearnConfig) EffectiveKnowledgeIntervalMinutes() int {
	if l.KnowledgeIntervalMinutes <= 0 {
		return 5
	}
	return l.KnowledgeIntervalMinutes
}

type DSHConfig struct {
	URL          string `yaml:"url"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

func (a *AnalysisConfig) EffectiveDays() int {
	if a.Days <= 0 {
		return 3
	}
	return a.Days
}

func (a *AnalysisConfig) EffectiveMaxDays() int {
	if a.MaxDays <= 0 {
		return 14
	}
	return a.MaxDays
}

func (a *AnalysisConfig) EffectiveScheduleMinutes() int {
	if a.ScheduleMinutes <= 0 {
		return 360
	}
	return a.ScheduleMinutes
}

func (c *Config) ReadOnly() bool {
	return c.Mode != "readwrite"
}

type Rule struct {
	Name   string     `yaml:"name" json:"name"`
	Type   string     `yaml:"type" json:"type"`
	Params RuleParams `yaml:"params" json:"params"`
}

type RuleParams struct {
	// archive_by_age
	Days  int    `yaml:"days" json:"days,omitempty"`
	State string `yaml:"state" json:"state,omitempty"` // "read", "unread", "any"

	// archive_by_sender
	Patterns     []string `yaml:"patterns" json:"patterns,omitempty"`
	Filter       string   `yaml:"filter,omitempty" json:"filter,omitempty"`
	RequireReply bool     `yaml:"require_reply,omitempty" json:"require_reply,omitempty"`

	// archive_by_label
	Label string `yaml:"label" json:"label,omitempty"`
}

var validRuleTypes = map[string]bool{
	"archive_by_age":    true,
	"archive_by_sender": true,
	"archive_by_label":  true,
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (a *AnalysisConfig) Validate() error {
	if a.DSH.URL == "" {
		return fmt.Errorf("analysis.dsh.url is required")
	}
	if a.DSH.ClientID == "" {
		return fmt.Errorf("analysis.dsh.client_id is required")
	}
	if a.DSH.ClientSecret == "" {
		return fmt.Errorf("analysis.dsh.client_secret is required")
	}
	return nil
}

func LoadDSH(path string) (*DSHConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading DSH config: %w", err)
	}
	var cfg DSHConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing DSH config: %w", err)
	}
	return &cfg, nil
}

func validate(cfg *Config) error {
	for i, r := range cfg.Rules {
		if !validRuleTypes[r.Type] {
			return fmt.Errorf("rule[%d] %q: unknown type %q (valid: archive_by_age, archive_by_sender, archive_by_label)", i, r.Name, r.Type)
		}
		switch r.Type {
		case "archive_by_age":
			if r.Params.Days <= 0 {
				return fmt.Errorf("rule[%d] %q: archive_by_age requires days > 0", i, r.Name)
			}
			if r.Params.State == "" {
				cfg.Rules[i].Params.State = "read"
			}
			if s := cfg.Rules[i].Params.State; s != "read" && s != "unread" && s != "any" {
				return fmt.Errorf("rule[%d] %q: state must be read|unread|any, got %q", i, r.Name, s)
			}
		case "archive_by_sender":
			if len(r.Params.Patterns) == 0 {
				return fmt.Errorf("rule[%d] %q: archive_by_sender requires at least one pattern", i, r.Name)
			}
			for _, p := range r.Params.Patterns {
				if _, err := regexp.Compile(p); err != nil {
					return fmt.Errorf("rule[%d] %q: invalid sender pattern %q: %w", i, r.Name, p, err)
				}
			}
		case "archive_by_label":
			if r.Params.Label == "" {
				return fmt.Errorf("rule[%d] %q: archive_by_label requires label", i, r.Name)
			}
		}
	}
	return nil
}
