package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/danskode/ekte/internal/agent"
	"github.com/danskode/ekte/internal/git"
	"github.com/danskode/ekte/internal/onboarding"
	"github.com/danskode/ekte/internal/provider"
	"github.com/danskode/ekte/internal/skill"
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
	cwd, _ := os.Getwd()

	var welcomeName string
	isFirstRun := onboarding.IsFirstRun(cwd)
	if isFirstRun {
		result, err := onboarding.Run(cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "onboarding fejl: %v\n", err)
			os.Exit(1)
		}
		if !result.Ok {
			os.Exit(0)
		}
		welcomeName = result.ProjectName
	}

	configPath := filepath.Join(".ekte", "config.yaml")
	skillsDir := filepath.Join(".ekte", "skills")

	var p provider.Provider
	var wikiInst *wiki.Wiki

	cfg, err := provider.LoadConfig(configPath)
	if err == nil {
		if provider.MissingKey(cfg) {
			fmt.Fprintf(os.Stderr, "⚠  Ingen API-nøgle fundet. Sæt env-variablen:\n")
			if cfg.Provider == "anthropic" {
				fmt.Fprintf(os.Stderr, "   export ANTHROPIC_API_KEY=\"din-nøgle\"\n\n")
			} else {
				fmt.Fprintf(os.Stderr, "   export OPENAI_API_KEY=\"din-nøgle\"\n\n")
			}
		}
		p, _ = provider.NewFromConfig(cfg)
		wCfg := &wiki.Config{Enabled: cfg.Wiki.Enabled, Path: cfg.Wiki.Path}
		wikiInst, _ = wiki.New(wCfg)
	}

	skills, skillErrs := skill.LoadAll(skillsDir)
	for _, e := range skillErrs {
		fmt.Fprintf(os.Stderr, "skill advarsel: %v\n", e)
	}

	repoRoot := ""
	if root, err := git.RepoRoot(cwd); err == nil {
		repoRoot = root
	}

	a := agent.New(agent.Config{
		Provider:   p,
		Skills:     skills,
		Wiki:       wikiInst,
		RepoRoot:   repoRoot,
		SessionDir: filepath.Join(".ekte", "sessions"),
	})

	m := tui.New(a)

	if provider.KeyInFile(configPath) {
		m.AddWarning("⚠  API-nøgle fundet i .ekte/config.yaml — flyt den til env-variabel:\nexport ANTHROPIC_API_KEY=\"din-nøgle\"  (tilføj til ~/.bashrc)")
	}

	if context := loadEkteMd(cwd); context != "" {
		m.SetProjectContext(context)
		if welcomeName == "" {
			welcomeName = onboarding.ReadProjectName(filepath.Join(cwd, "ekte.md"))
		}
	}

	if isFirstRun {
		m.SetWelcome(welcomeName)
	}

	prog := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "fejl: %v\n", err)
		os.Exit(1)
	}
}

func loadEkteMd(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "ekte.md"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func runInit() {
	configPath := filepath.Join(".ekte", "config.yaml")

	wikiCfg, err := wiki.Init()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wiki init fejl: %v\n", err)
		os.Exit(1)
	}

	type fullConfig struct {
		Provider string       `yaml:"provider"`
		Model    string       `yaml:"model"`
		BaseURL  string       `yaml:"base_url,omitempty"`
		Wiki     *wiki.Config `yaml:"wiki,omitempty"`
	}

	cfg := fullConfig{Provider: "openai", Model: "gpt-4o", Wiki: wikiCfg}
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
