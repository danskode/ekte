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
	GitWorktree   bool `yaml:"git_worktree"`   // /spec opret/merge/fjern
	WikiWrite     bool `yaml:"wiki_write"`     // /wiki gem
	WikiFetch     bool `yaml:"wiki_fetch"`     // /wiki-get hent URL-indhold
	HookRun       bool `yaml:"hook_run"`       // /hook <navn>
	HookContainer bool `yaml:"hook_container"` // /hook med container-isolation (kræver desuden hook_run)
	FileRead      bool `yaml:"file_read"`      // LLM må læse filer
	FileWrite     bool `yaml:"file_write"`     // LLM må skrive/oprette filer
}

// ContainerSpec beskriver hvordan en hook køres i en isoleret container.
type ContainerSpec struct {
	Image   string   `yaml:"image"`
	Network bool     `yaml:"network,omitempty"`  // default false = --network none
	Memory  string   `yaml:"memory,omitempty"`   // default "512m"
	CPUs    string   `yaml:"cpus,omitempty"`     // default "1"
	Workdir string   `yaml:"workdir,omitempty"`  // default "/work"
	Ports   []string `yaml:"ports,omitempty"`    // ["8080:8080"]
	Env     []string `yaml:"env,omitempty"`      // eksplicitte KEY=VALUE — arves ikke fra host
}

// HookConfig beskriver én hook — enten en simpel shell-kommando eller en
// kommando der køres i en isoleret container.
// Bagudkompatibel: en streng-værdi i YAML ("test: go test ./...") parses
// automatisk til HookConfig{Cmd: "go test ./..."}.
type HookConfig struct {
	Cmd       string         `yaml:"cmd"`
	Container *ContainerSpec `yaml:"container,omitempty"`
}

func (h *HookConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		h.Cmd = value.Value
		return nil
	}
	type raw HookConfig
	return value.Decode((*raw)(h))
}

// ContainerConfig indeholder globale defaults og runtime-valg for container-hooks.
type ContainerConfig struct {
	Runtime        string `yaml:"runtime"`         // "docker"|"podman"|"" = autodetect
	DefaultMemory  string `yaml:"default_memory"`  // default "512m"
	DefaultCPUs    string `yaml:"default_cpus"`    // default "1"
	TimeoutSeconds int    `yaml:"timeout_seconds"` // default 300; 0 = ingen timeout
}

// GoalConfig styrer adfærden for /goal-kommandoen.
type GoalConfig struct {
	CheckHook     string `yaml:"check_hook"`     // navn på hook der bruges som succes-tjek
	MaxIterations int    `yaml:"max_iterations"` // default 10
}

type Config struct {
	Provider    string                `yaml:"provider"`
	Model       string                `yaml:"model"`
	BaseURL     string                `yaml:"base_url"`
	APIKey      string                `yaml:"api_key"` // læses kun fra env — advarsel hvis sat i fil
	ContextSize int                   `yaml:"context_size"` // 0 = brug default (200000)
	Wiki        WikiConfig            `yaml:"wiki"`
	Whitelist   WhitelistConfig       `yaml:"whitelist"`
	Hooks       map[string]HookConfig `yaml:"hooks,omitempty"`
	Containers  ContainerConfig       `yaml:"containers,omitempty"`
	Goal        GoalConfig            `yaml:"goal,omitempty"`
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

// MergeConfigs kombinerer global og lokal config. Lokal overskriver provider-indstillinger
// hvis de er sat; whitelist og hooks er altid projekt-specifikke.
func MergeConfigs(global, local *Config) *Config {
	if global == nil && local == nil {
		return &Config{}
	}
	if global == nil {
		return local
	}
	if local == nil {
		return global
	}
	merged := *global
	if local.Provider != "" {
		merged.Provider = local.Provider
	}
	if local.Model != "" {
		merged.Model = local.Model
	}
	if local.BaseURL != "" {
		merged.BaseURL = local.BaseURL
	}
	if local.APIKey != "" {
		merged.APIKey = local.APIKey
	}
	if local.Wiki.Path != "" {
		merged.Wiki = local.Wiki
	}
	merged.Whitelist = local.Whitelist
	if local.Hooks != nil {
		merged.Hooks = local.Hooks
	}
	return &merged
}

func NewFromConfig(cfg *Config) (Provider, error) {
	switch cfg.Provider {
	case "anthropic":
		return NewAnthropicProvider(cfg), nil
	default:
		return NewOpenAIProvider(cfg), nil
	}
}
