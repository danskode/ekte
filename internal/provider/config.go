package provider

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type WikiConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

// WhitelistConfig styrer hvilke operationer agenten må udføre uden bekræftelse.
// Alt er forbudt som standard — tilføj eksplicit til .ekte/config.yaml.
type WhitelistConfig struct {
	GitWorktree bool `yaml:"git_worktree"` // /spec opret/merge/fjern
	WikiWrite   bool `yaml:"wiki_write"`   // /wiki gem
	HookRun     bool `yaml:"hook_run"`     // /hook <navn>
}

type Config struct {
	Provider  string            `yaml:"provider"`
	Model     string            `yaml:"model"`
	BaseURL   string            `yaml:"base_url"`
	APIKey    string            `yaml:"api_key"` // læses kun fra env — advarsel hvis sat i fil
	Wiki      WikiConfig        `yaml:"wiki"`
	Whitelist WhitelistConfig   `yaml:"whitelist"`
	Hooks     map[string]string `yaml:"hooks,omitempty"` // navn → shell-kommando
}

// KeyInFile returnerer true hvis api_key er sat direkte i config-filen.
// Bruges til at vise sikkerhedsadvarsel i TUI.
func KeyInFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return false
	}
	v, ok := raw["api_key"]
	if !ok {
		return false
	}
	s, _ := v.(string)
	return s != ""
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	// env-variabel har forrang over fil
	switch cfg.Provider {
	case "anthropic":
		if env := os.Getenv("ANTHROPIC_API_KEY"); env != "" {
			cfg.APIKey = env
		}
	default:
		if env := os.Getenv("OPENAI_API_KEY"); env != "" {
			cfg.APIKey = env
		}
	}
	return &cfg, nil
}

// MissingKey returnerer true hvis ingen API-nøgle er tilgængelig
// (hverken env-variabel eller config-fil) for en cloud-provider.
func MissingKey(cfg *Config) bool {
	if cfg.BaseURL != "" {
		return false // lokal provider — ingen nøgle nødvendig
	}
	return cfg.APIKey == ""
}

func NewFromConfig(cfg *Config) (Provider, error) {
	switch cfg.Provider {
	case "anthropic":
		return NewAnthropicProvider(cfg), nil
	default:
		return NewOpenAIProvider(cfg), nil
	}
}
