package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/danskode/ekte/internal/git"
	"github.com/danskode/ekte/internal/provider"
	"github.com/danskode/ekte/internal/tui"
	"github.com/danskode/ekte/internal/wiki"
	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "init" {
		runInit()
		return
	}
	runTUI()
}

func runTUI() {
	configPath := filepath.Join(".ekte", "config.yaml")
	skillsDir := filepath.Join(".ekte", "skills")

	var p provider.Provider
	var wikiInst *wiki.Wiki

	cfg, err := provider.LoadConfig(configPath)
	if err == nil {
		p, err = provider.NewFromConfig(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "provider fejl: %v\n", err)
		}
		wCfg := &wiki.Config{
			Enabled: cfg.Wiki.Enabled,
			Path:    cfg.Wiki.Path,
		}
		wikiInst, err = wiki.New(wCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wiki advarsel: %v\n", err)
		}
	}

	m := tui.New(p)

	if errs := m.LoadSkills(skillsDir); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "skill advarsel: %v\n", e)
		}
	}

	cwd, _ := os.Getwd()
	if root, err := git.RepoRoot(cwd); err == nil {
		m.SetRepoRoot(root)
	}

	if wikiInst != nil {
		m.SetWiki(wikiInst)
	}

	m.SetSessionDir(filepath.Join(".ekte", "sessions"))

	prog := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "fejl: %v\n", err)
		os.Exit(1)
	}
}

func runInit() {
	configPath := filepath.Join(".ekte", "config.yaml")

	wikiCfg, err := wiki.Init()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wiki init fejl: %v\n", err)
		os.Exit(1)
	}

	// læs eksisterende config eller opret ny
	type fullConfig struct {
		Provider string          `yaml:"provider"`
		Model    string          `yaml:"model"`
		BaseURL  string          `yaml:"base_url,omitempty"`
		APIKey   string          `yaml:"api_key,omitempty"`
		Wiki     *wiki.Config    `yaml:"wiki,omitempty"`
	}

	cfg := fullConfig{
		Provider: "openai",
		Model:    "llama3.2",
		BaseURL:  "http://localhost:11434/v1",
		Wiki:     wikiCfg,
	}

	if data, err := os.ReadFile(configPath); err == nil {
		_ = yaml.Unmarshal(data, &cfg)
		cfg.Wiki = wikiCfg
	}

	if err := os.MkdirAll(".ekte", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "fejl: %v\n", err)
		os.Exit(1)
	}

	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "gem config fejl: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Config gemt: %s\n", configPath)
	fmt.Println("\nKør 'ekte' for at starte.")
}
