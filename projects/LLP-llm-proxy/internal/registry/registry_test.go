package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/topolik/llp-llm-proxy/internal/provider"
)

const sampleYAML = `
port: 4000
db_path: ./data/llp.db
impls:
  gemini:
    type: cli
    command: ["echo", "hi"]
    timeout: 180s
    cooldown: 60s
    price: { in: 0, out: 0 }
  claude:
    type: cli
    command: ["echo", "yo"]
    timeout: 90s
    cooldown: 30s
  openllm:
    type: http
    base_url: ""
    api_key_env: OPENLLM_API_KEY
models:
  auto:        { chain: [gemini, claude, openllm] }
  gml-analyze: { chain: [gemini, claude] }
default_model: auto
`

// LLP-017: a config whose command execs gemini-cli without an explicit
// --approval-mode must refuse to build (regeneration-from-stale-example guard).
func TestBuildRejectsGeminiWithoutApprovalMode(t *testing.T) {
	bad := `
impls:
  gemini:
    type: cli
    command: ["npx", "@google/gemini-cli", "-e", "none", "-p", ""]
models:
  auto: { chain: [gemini] }
default_model: auto
`
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := Build(cfg); err == nil || !strings.Contains(err.Error(), "--approval-mode") {
		t.Fatalf("want approval-mode build error, got %v", err)
	}

	// Same command WITH the flag (or a bare `gemini` binary with the = form) builds.
	for _, good := range []string{
		`command: ["npx", "@google/gemini-cli", "-e", "none", "--approval-mode", "default", "-p", ""]`,
		`command: ["/usr/local/bin/gemini", "--approval-mode=default", "-p", ""]`,
	} {
		buildFromYAML(t, `
impls:
  gemini:
    type: cli
    `+good+`
models:
  auto: { chain: [gemini] }
default_model: auto
`)
	}
}

func buildFromYAML(t *testing.T, y string) (*Config, *Registry) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	reg, err := Build(cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return cfg, reg
}

// T2: durations parse, and a known model resolves to its exact chain.
func TestResolveKnownModel(t *testing.T) {
	cfg, reg := buildFromYAML(t, sampleYAML)
	if cfg.Impls["gemini"].Timeout.D().String() != "3m0s" {
		t.Fatalf("duration parse: %v", cfg.Impls["gemini"].Timeout.D())
	}
	chain, override := reg.Resolve("gml-analyze")
	if len(chain) != 2 || chain[0].Name != "gemini" || chain[1].Name != "claude" || override != "" {
		t.Fatalf("bad chain: %+v override=%q", names(chain), override)
	}
}

// T2: unknown model falls back to the default chain.
func TestResolveUnknownFallsBackToDefault(t *testing.T) {
	_, reg := buildFromYAML(t, sampleYAML)
	chain, _ := reg.Resolve("does-not-exist")
	if got := names(chain); len(got) != 3 || got[0] != "gemini" || got[2] != "openllm" {
		t.Fatalf("expected default chain, got %v", got)
	}
}

// T2: an impl name pins to a single-element chain (no failover, no override).
func TestResolveImplNamePins(t *testing.T) {
	_, reg := buildFromYAML(t, sampleYAML)
	chain, override := reg.Resolve("claude")
	if len(chain) != 1 || chain[0].Name != "claude" || override != "" {
		t.Fatalf("expected [claude] no override, got %v %q", names(chain), override)
	}
}

// T2: "impl/<model>" pins the impl and carries the model override.
func TestResolveImplWithModelOverride(t *testing.T) {
	_, reg := buildFromYAML(t, sampleYAML)
	chain, override := reg.Resolve("gemini/gemini-2.5-pro")
	if len(chain) != 1 || chain[0].Name != "gemini" || override != "gemini-2.5-pro" {
		t.Fatalf("expected [gemini] override gemini-2.5-pro, got %v %q", names(chain), override)
	}
}

// T2: a model override on a logical model is ignored (chains span impls).
func TestResolveLogicalModelIgnoresOverride(t *testing.T) {
	_, reg := buildFromYAML(t, sampleYAML)
	chain, override := reg.Resolve("auto/whatever")
	if len(chain) != 3 || override != "" {
		t.Fatalf("expected full auto chain, no override; got %v %q", names(chain), override)
	}
}

// T2: an http impl with empty base_url is present but reports unavailable.
func TestOpenLLMStubbedUnavailable(t *testing.T) {
	_, reg := buildFromYAML(t, sampleYAML)
	chain, _ := reg.Resolve("auto")
	var openllm *Impl
	for _, im := range chain {
		if im.Name == "openllm" {
			openllm = im
		}
	}
	if openllm == nil {
		t.Fatal("openllm missing from chain")
	}
	a, ok := openllm.Provider.(provider.Availabler)
	if !ok || a.Available() {
		t.Fatalf("stubbed openllm should be unavailable")
	}
}

// T2: a chain referencing an unknown impl is rejected at Build.
func TestBuildRejectsUnknownImplInChain(t *testing.T) {
	bad := `
impls:
  gemini: { type: cli, command: ["echo"] }
models:
  auto: { chain: [gemini, ghost] }
default_model: auto
`
	p := filepath.Join(t.TempDir(), "c.yaml")
	os.WriteFile(p, []byte(bad), 0o644)
	cfg, _ := Load(p)
	if _, err := Build(cfg); err == nil {
		t.Fatal("expected error for unknown impl in chain")
	}
}

// T2: default_model must exist.
func TestBuildRejectsMissingDefaultModel(t *testing.T) {
	bad := `
impls:
  gemini: { type: cli, command: ["echo"] }
models:
  auto: { chain: [gemini] }
default_model: nope
`
	p := filepath.Join(t.TempDir(), "c.yaml")
	os.WriteFile(p, []byte(bad), 0o644)
	cfg, _ := Load(p)
	if _, err := Build(cfg); err == nil {
		t.Fatal("expected error for missing default_model")
	}
}

func names(impls []*Impl) []string {
	out := make([]string, len(impls))
	for i, im := range impls {
		out[i] = im.Name
	}
	return out
}
