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

type Config struct {
	Provider string     `yaml:"provider"`
	Model    string     `yaml:"model"`
	BaseURL  string     `yaml:"base_url"`
	APIKey   string     `yaml:"api_key"`
	Wiki     WikiConfig `yaml:"wiki"`
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
	if cfg.APIKey == "" {
		switch cfg.Provider {
		case "anthropic":
			cfg.APIKey = os.Getenv("ANTHROPIC_API_KEY")
		default:
			cfg.APIKey = os.Getenv("OPENAI_API_KEY")
		}
	}
	return &cfg, nil
}

func NewFromConfig(cfg *Config) (Provider, error) {
	switch cfg.Provider {
	case "anthropic":
		return NewAnthropicProvider(cfg), nil
	default:
		return NewOpenAIProvider(cfg), nil
	}
}
