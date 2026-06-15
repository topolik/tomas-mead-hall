// Package registry loads LLP's config.yaml and turns it into runtime providers,
// the logical-model -> failover-chain mapping, and the client auth key map.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/topolik/llp-llm-proxy/internal/provider"
	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that unmarshals from a YAML string like "180s".
type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	if s == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) D() time.Duration { return time.Duration(d) }

// Price is the per-token cost (USD) for an impl. CLI impls are free (0).
type Price struct {
	In  float64 `yaml:"in"`
	Out float64 `yaml:"out"`
}

type ImplConfig struct {
	Type      string            `yaml:"type"` // "cli" | "http"
	Command   []string          `yaml:"command"`
	Env       map[string]string `yaml:"env"`
	ModelFlag string            `yaml:"model_flag"`
	ModelID   string            `yaml:"model_id"`
	Timeout   Duration          `yaml:"timeout"`
	Cooldown  Duration          `yaml:"cooldown"`
	// QuotaCooldown is the longer cooldown applied when the impl reports its
	// quota exhausted for a long window (e.g. gemini's TerminalQuotaError, a
	// daily limit) rather than a momentary throttle. 0 = use Cooldown.
	QuotaCooldown Duration `yaml:"quota_cooldown"`
	BaseURL       string   `yaml:"base_url"`
	APIKeyEnv     string   `yaml:"api_key_env"`
	StripFence    bool     `yaml:"strip_fence"`
	Concurrency   int      `yaml:"concurrency"`
	Price         Price    `yaml:"price"`
}

type ModelConfig struct {
	Chain []string `yaml:"chain"`
}

type Config struct {
	Port           int                    `yaml:"port"`
	BindHost       string                 `yaml:"bind_host"`      // default 127.0.0.1 (loopback, off-host unreachable)
	ControlSocket  string                 `yaml:"control_socket"` // Unix socket for the token handshake
	RequireSameUID bool                   `yaml:"control_require_same_uid"`
	DBPath         string                 `yaml:"db_path"`
	MaxPromptBytes int                    `yaml:"max_prompt_bytes"`
	ContentPreview *bool                  `yaml:"content_preview"`     // store truncated prompt/response per request (default true)
	PreviewMaxLen  int                    `yaml:"content_preview_max"` // chars kept per preview (default 4096)
	Impls          map[string]ImplConfig  `yaml:"impls"`
	Models         map[string]ModelConfig `yaml:"models"`
	DefaultModel   string                 `yaml:"default_model"`
}

// PreviewMax returns the per-preview char cap, or 0 if content preview is
// disabled (in which case no prompt/response text is stored).
func (c *Config) PreviewMax() int {
	if c.ContentPreview != nil && !*c.ContentPreview {
		return 0
	}
	return c.PreviewMaxLen
}

// Impl is a runtime-resolved backend: its provider plus routing metadata.
type Impl struct {
	Name          string
	Provider      provider.Provider
	Cooldown      time.Duration
	QuotaCooldown time.Duration // applied instead of Cooldown on quota-exhausted errors; 0 = use Cooldown
	Concurrency   int
	Price         Price
}

// Registry resolves logical model names to ordered impl chains.
type Registry struct {
	impls  map[string]*Impl
	models map[string][]string
	def    string
}

// NewRegistry builds a Registry directly (used by tests with fake providers).
func NewRegistry(impls map[string]*Impl, models map[string][]string, def string) *Registry {
	return &Registry{impls: impls, models: models, def: def}
}

// Resolve maps a requested model name to an ordered failover chain and an
// optional per-request model-id override. It accepts three forms:
//
//   - "auto" / "gml-analyze"   a defined logical model -> its chain, no override
//   - "gemini" / "claude"      an impl name            -> [that impl], no override
//   - "gemini/gemini-2.5-pro"  impl + model override   -> [that impl], override="gemini-2.5-pro"
//
// A defined logical model always wins (exact match), and an override on a
// logical model is ignored (overrides only make sense for a single impl).
// Anything unrecognized falls back to the default model's chain.
func (r *Registry) Resolve(model string) (chain []*Impl, override string) {
	// 1. exact logical model name -> its chain
	if c, ok := r.models[model]; ok {
		return r.implChain(c), ""
	}
	// 2. "impl" or "impl/<override>"
	name := model
	if i := strings.IndexByte(model, '/'); i >= 0 {
		name, override = model[:i], model[i+1:]
	}
	if im, ok := r.impls[name]; ok {
		return []*Impl{im}, override
	}
	// 3. logical model carrying a trailing "/..." (override not meaningful) -> chain
	if c, ok := r.models[name]; ok {
		return r.implChain(c), ""
	}
	// 4. unknown -> default chain
	return r.implChain(r.models[r.def]), ""
}

func (r *Registry) implChain(names []string) []*Impl {
	out := make([]*Impl, 0, len(names))
	for _, n := range names {
		if im, ok := r.impls[n]; ok {
			out = append(out, im)
		}
	}
	return out
}

// ImplByName returns a defined impl by name.
func (r *Registry) ImplByName(name string) (*Impl, bool) {
	im, ok := r.impls[name]
	return im, ok
}

// AllImpls returns every defined impl, sorted by name (stable for tests/health).
func (r *Registry) AllImpls() []*Impl {
	out := make([]*Impl, 0, len(r.impls))
	for _, im := range r.impls {
		out = append(out, im)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ModelNames returns the logical model names, sorted.
func (r *Registry) ModelNames() []string {
	out := make([]string, 0, len(r.models))
	for name := range r.models {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Load reads and parses config.yaml, applying defaults.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Port == 0 {
		cfg.Port = 4000
	}
	if cfg.BindHost == "" {
		cfg.BindHost = "127.0.0.1" // loopback by default: not reachable off-host
	}
	if cfg.ControlSocket == "" {
		cfg.ControlSocket = defaultControlSocket()
	} else if strings.HasPrefix(cfg.ControlSocket, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			cfg.ControlSocket = filepath.Join(home, cfg.ControlSocket[2:])
		}
	}
	if cfg.MaxPromptBytes == 0 {
		cfg.MaxPromptBytes = 1 << 20 // 1 MiB
	}
	if cfg.PreviewMaxLen == 0 {
		cfg.PreviewMaxLen = 4096
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "./data/llp.db"
	}
	return &cfg, nil
}

// defaultControlSocket returns ~/.llp/control.sock — a stable, owner-only (0700
// dir) path that agent containers can bind-mount predictably.
func defaultControlSocket() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".llp", "control.sock")
	}
	return "/tmp/llp-control.sock"
}

// checkGeminiApprovalMode enforces LLP-017: a command that execs gemini-cli must
// pin an explicit --approval-mode. Headless gemini-cli treats every workspace as
// trusted and inherits the user-level defaultApprovalMode — auto_edit on this
// host let a "completion" write files (incident MND-011). The mode must be
// chosen in config, never inherited; this refuses to start the exact insecure
// config a regeneration from a stale example would produce (2026-06-12 20:10).
func checkGeminiApprovalMode(name string, command []string) error {
	isGemini := false
	for i, arg := range command {
		if strings.Contains(arg, "gemini-cli") || (i == 0 && filepath.Base(arg) == "gemini") {
			isGemini = true
			break
		}
	}
	if !isGemini {
		return nil
	}
	for _, arg := range command {
		if arg == "--approval-mode" || strings.HasPrefix(arg, "--approval-mode=") {
			return nil
		}
	}
	return fmt.Errorf("impl %q: gemini-cli command must set an explicit --approval-mode (e.g. \"default\") — headless gemini-cli inherits the user-level approval mode and can WRITE FILES (LLP-014/LLP-017)", name)
}

// Build constructs the runtime Registry from a Config. Client auth is no longer
// static: agents obtain session tokens at startup via the control socket (see
// internal/control), so there are no keys to read here.
func Build(cfg *Config) (*Registry, error) {
	impls := make(map[string]*Impl, len(cfg.Impls))
	for name, ic := range cfg.Impls {
		var p provider.Provider
		switch ic.Type {
		case "cli":
			if err := checkGeminiApprovalMode(name, ic.Command); err != nil {
				return nil, err
			}
			p = provider.NewCli(provider.CliConfig{
				Name: name, Command: ic.Command, Env: ic.Env,
				ModelFlag: ic.ModelFlag, ModelID: ic.ModelID,
				Timeout: ic.Timeout.D(), StripFence: ic.StripFence,
			})
		case "http":
			key := ""
			if ic.APIKeyEnv != "" {
				key = os.Getenv(ic.APIKeyEnv)
			}
			p = provider.NewHttp(provider.HttpConfig{
				Name: name, BaseURL: ic.BaseURL, APIKey: key,
				ModelID: ic.ModelID, Timeout: ic.Timeout.D(),
			})
		default:
			return nil, fmt.Errorf("impl %q: unknown type %q (want cli|http)", name, ic.Type)
		}
		conc := ic.Concurrency
		if conc <= 0 {
			conc = 1
		}
		impls[name] = &Impl{Name: name, Provider: p, Cooldown: ic.Cooldown.D(), QuotaCooldown: ic.QuotaCooldown.D(), Concurrency: conc, Price: ic.Price}
	}

	models := make(map[string][]string, len(cfg.Models))
	for name, mc := range cfg.Models {
		models[name] = mc.Chain
	}

	if cfg.DefaultModel == "" {
		return nil, fmt.Errorf("default_model is required")
	}
	if _, ok := models[cfg.DefaultModel]; !ok {
		return nil, fmt.Errorf("default_model %q is not defined under models", cfg.DefaultModel)
	}
	for mname, chain := range models {
		if len(chain) == 0 {
			return nil, fmt.Errorf("model %q has an empty chain", mname)
		}
		for _, in := range chain {
			if _, ok := impls[in]; !ok {
				return nil, fmt.Errorf("model %q references unknown impl %q", mname, in)
			}
		}
	}

	return NewRegistry(impls, models, cfg.DefaultModel), nil
}
