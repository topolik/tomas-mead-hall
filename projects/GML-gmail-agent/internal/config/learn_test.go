package config

import "testing"

func TestLearnConfig_EffectiveKnowledgeIntervalMinutes(t *testing.T) {
	cases := []struct {
		name     string
		interval int
		want     int
	}{
		{"default when zero", 0, 5},
		{"default when negative", -1, 5},
		{"explicit value", 30, 30},
		{"one minute", 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := &LearnConfig{KnowledgeIntervalMinutes: c.interval}
			if got := l.EffectiveKnowledgeIntervalMinutes(); got != c.want {
				t.Errorf("EffectiveKnowledgeIntervalMinutes(%d) = %d, want %d", c.interval, got, c.want)
			}
		})
	}
}

func TestLearnConfig_EffectiveDays(t *testing.T) {
	cases := []struct {
		days int
		want int
	}{
		{0, 30},
		{-1, 30},
		{14, 14},
	}
	for _, c := range cases {
		l := &LearnConfig{Days: c.days}
		if got := l.EffectiveDays(); got != c.want {
			t.Errorf("EffectiveDays(%d) = %d, want %d", c.days, got, c.want)
		}
	}
}

func TestLearnConfig_EffectiveTopSenders(t *testing.T) {
	cases := []struct {
		top  int
		want int
	}{
		{0, 30},
		{50, 50},
	}
	for _, c := range cases {
		l := &LearnConfig{TopSenders: c.top}
		if got := l.EffectiveTopSenders(); got != c.want {
			t.Errorf("EffectiveTopSenders(%d) = %d, want %d", c.top, got, c.want)
		}
	}
}

func TestLearnConfig_EffectiveMinEmails(t *testing.T) {
	cases := []struct {
		min  int
		want int
	}{
		{0, 3},
		{10, 10},
	}
	for _, c := range cases {
		l := &LearnConfig{MinEmails: c.min}
		if got := l.EffectiveMinEmails(); got != c.want {
			t.Errorf("EffectiveMinEmails(%d) = %d, want %d", c.min, got, c.want)
		}
	}
}
