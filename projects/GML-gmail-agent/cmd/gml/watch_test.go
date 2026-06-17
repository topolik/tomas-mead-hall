package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/topolik/gml-gmail-agent/internal/config"
)

func TestAnalysisWindow_FirstRun(t *testing.T) {
	cfg := &config.Config{
		Analysis: config.AnalysisConfig{Days: 3, MaxDays: 14},
	}
	stateFile := filepath.Join(t.TempDir(), "state")

	window := analysisWindow(cfg, 30, stateFile)

	want := 3 * 24 * 60
	if window != want {
		t.Errorf("first run window = %d, want %d (3 days in minutes)", window, want)
	}
}

func TestAnalysisWindow_RecentRun(t *testing.T) {
	cfg := &config.Config{
		Analysis: config.AnalysisConfig{Days: 3, MaxDays: 14},
	}
	stateFile := filepath.Join(t.TempDir(), "state")
	os.WriteFile(stateFile, []byte(fmt.Sprintf("%d", time.Now().Add(-10*time.Minute).Unix())), 0644)

	window := analysisWindow(cfg, 30, stateFile)

	if window != 30 {
		t.Errorf("recent run window = %d, want 30 (interval)", window)
	}
}

func TestAnalysisWindow_CatchUp(t *testing.T) {
	cfg := &config.Config{
		Analysis: config.AnalysisConfig{Days: 3, MaxDays: 14},
	}
	stateFile := filepath.Join(t.TempDir(), "state")
	os.WriteFile(stateFile, []byte(fmt.Sprintf("%d", time.Now().Add(-120*time.Minute).Unix())), 0644)

	window := analysisWindow(cfg, 30, stateFile)

	if window < 119 || window > 121 {
		t.Errorf("catch-up window = %d, want ~120 (elapsed minutes)", window)
	}
}

func TestAnalysisWindow_ClampsToMax(t *testing.T) {
	cfg := &config.Config{
		Analysis: config.AnalysisConfig{Days: 3, MaxDays: 7},
	}
	stateFile := filepath.Join(t.TempDir(), "state")
	// Last run 30 days ago — exceeds max
	os.WriteFile(stateFile, []byte(fmt.Sprintf("%d", time.Now().Add(-30*24*time.Hour).Unix())), 0644)

	window := analysisWindow(cfg, 30, stateFile)

	maxWindow := 7 * 24 * 60
	if window != maxWindow {
		t.Errorf("clamped window = %d, want %d (7 days max)", window, maxWindow)
	}
}

func TestAnalysisWindow_InvalidStateFile(t *testing.T) {
	cfg := &config.Config{
		Analysis: config.AnalysisConfig{Days: 5, MaxDays: 14},
	}
	stateFile := filepath.Join(t.TempDir(), "state")
	os.WriteFile(stateFile, []byte("garbage"), 0644)

	window := analysisWindow(cfg, 30, stateFile)

	want := 5 * 24 * 60
	if window != want {
		t.Errorf("invalid state window = %d, want %d (5 days fallback)", window, want)
	}
}

func TestAnalysisWindow_DefaultDays(t *testing.T) {
	cfg := &config.Config{
		Analysis: config.AnalysisConfig{MaxDays: 14},
	}
	stateFile := filepath.Join(t.TempDir(), "state")

	window := analysisWindow(cfg, 30, stateFile)

	want := 3 * 24 * 60
	if window != want {
		t.Errorf("default days window = %d, want %d (default 3 days)", window, want)
	}
}
