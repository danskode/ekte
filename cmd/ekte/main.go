package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"time"

	"github.com/danskode/ekte/internal/agent"
	"github.com/danskode/ekte/internal/ektelog"
	"github.com/danskode/ekte/internal/git"
	"github.com/danskode/ekte/internal/obs"
	"github.com/danskode/ekte/internal/onboarding"
	"github.com/danskode/ekte/internal/provider"
	"github.com/danskode/ekte/internal/session"
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
	sessionArg := ""
	if len(os.Args) > 1 {
		sessionArg = os.Args[1]
	}
	runTUI(sessionArg)
}

func globalEkteDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ekte")
}

func runTUI(sessionArg string) {
	cwd, _ := os.Getwd()
	globalDir := globalEkteDir()

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

	globalConfigPath := filepath.Join(globalDir, "config.yaml")
	localConfigPath := filepath.Join(".ekte", "config.yaml")
	skillsDir := filepath.Join(".ekte", "skills")

	var p provider.Provider
	var wikiInst *wiki.Wiki

	globalCfg, _ := provider.LoadConfig(globalConfigPath)
	localCfg, _ := provider.LoadConfig(localConfigPath)
	cfg := provider.MergeConfigs(globalCfg, localCfg)

	profile := loadProfile()
	if profile.UserName == "" || profile.AgentName == "" {
		profile = promptProfile()
		saveProfile(profile)
	}

	if cfg == nil || provider.MissingKey(cfg) {
		if !promptAPISetup(globalConfigPath) {
			os.Exit(0)
		}
		globalCfg, _ = provider.LoadConfig(globalConfigPath)
		cfg = provider.MergeConfigs(globalCfg, localCfg)
	}

	if cfg != nil {
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

	var whitelist provider.WhitelistConfig
	var hooks map[string]string
	if cfg != nil {
		whitelist = cfg.Whitelist
		hooks = cfg.Hooks
	}

	// Brug lokal session-mappe hvis .ekte/ eksisterer, ellers global fallback
	sessionDir := filepath.Join(globalDir, "sessions")
	if _, err := os.Stat(".ekte"); err == nil {
		sessionDir = filepath.Join(".ekte", "sessions")
	}
	_ = os.MkdirAll(sessionDir, 0755)

	var resumeSession *session.Session
	if sessionArg != "" {
		if found, err := session.FindByName(sessionDir, sessionArg); err == nil && found != nil {
			resumeSession = found
		}
	}

	sessionID := time.Now().Format("20060102-150405")
	obsPath := filepath.Join(sessionDir, sessionID+"_obs.jsonl")
	recorder := obs.NewRecorder(obsPath, sessionDir)

	logPath := filepath.Join(sessionDir, sessionID+".log")
	logger, err := ektelog.New(logPath, ektelog.DEBUG)
	if err != nil {
		logger = ektelog.Discard()
	}
	defer logger.Close()

	providerName := ""
	modelName := ""
	if cfg != nil {
		providerName = cfg.Provider
		modelName = cfg.Model
	}

	contextSize := 0
	if cfg != nil && cfg.ContextSize > 0 {
		contextSize = cfg.ContextSize
	}

	a := agent.New(agent.Config{
		Provider:      p,
		Skills:        skills,
		Wiki:          wikiInst,
		RepoRoot:      repoRoot,
		WorkDir:       cwd,
		SessionDir:    sessionDir,
		Whitelist:     whitelist,
		Hooks:         hooks,
		Obs:           recorder,
		Log:           logger,
		ResumeSession: resumeSession,
		AgentName:     profile.AgentName,
		ContextSize:   contextSize,
		ProviderName:  providerName,
		ModelName:     modelName,
	})

	m := tui.New(a)
	m.SetNames(profile.UserName, profile.AgentName)
	m.SetMaxTokens(contextSize)

	if provider.KeyInFile(globalConfigPath) || provider.KeyInFile(localConfigPath) {
		m.AddWarning("⚠  API-nøgle fundet i config-fil — flyt den til env-variabel:\nexport ANTHROPIC_API_KEY=\"din-nøgle\"  (tilføj til ~/.bashrc)")
	}

	if context := loadEkteMd(cwd); context != "" {
		m.SetProjectContext(context)
		if welcomeName == "" {
			welcomeName = onboarding.ReadProjectName(filepath.Join(cwd, "ekte.md"))
		}
	}

	m.ShowBanner()
	if resumeSession != nil {
		m.AddInfo(fmt.Sprintf("✓ Session genoptaget: %s (%s)", resumeSession.Title, resumeSession.Name))
	} else if hint := recentSessionsHint(sessionDir); hint != "" {
		m.AddInfo(hint)
	}
	if isFirstRun {
		m.SetWelcome(welcomeName)
	}

	// MouseCellMotion gør det muligt at scrolle samtaleruden med musehjulet —
	// viewport.Model håndterer tea.MouseMsg automatisk når den modtager dem.
	prog := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := prog.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fejl: %v\n", err)
		os.Exit(1)
	}

	// Vis afslutnings-noten (sessionsnavn, resume-hint, log-sti) i den rigtige
	// terminal — alt-screen er nu lukket, så den forsvinder ikke fra skærmen.
	if fm, ok := final.(tui.Model); ok {
		if note := fm.ExitNote(); note != "" {
			fmt.Println()
			fmt.Println(note)
		}
	}
}

type ekteProfile struct {
	UserName  string `yaml:"user_name"`
	AgentName string `yaml:"agent_name"`
}

func loadProfile() ekteProfile {
	data, err := os.ReadFile(filepath.Join(globalEkteDir(), "profile.yaml"))
	if err != nil {
		return ekteProfile{}
	}
	var p ekteProfile
	_ = yaml.Unmarshal(data, &p)
	return p
}

func saveProfile(p ekteProfile) {
	dir := globalEkteDir()
	_ = os.MkdirAll(dir, 0755)
	data, _ := yaml.Marshal(p)
	_ = os.WriteFile(filepath.Join(dir, "profile.yaml"), data, 0644)
}

func promptProfile() ekteProfile {
	reader := bufio.NewReader(os.Stdin)
	bold := "\033[1m"
	reset := "\033[0m"

	fmt.Printf("\n%s👤 Hvad vil du kaldes?%s (Enter = Dig) > ", bold, reset)
	userName, _ := reader.ReadString('\n')
	userName = strings.TrimSpace(userName)
	if userName == "" {
		userName = "Dig"
	}

	fmt.Printf("%s🤖 Hvad skal din agent hedde?%s (Enter = Ekte) > ", bold, reset)
	agentName, _ := reader.ReadString('\n')
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		agentName = "Ekte"
	}

	fmt.Println()
	return ekteProfile{UserName: userName, AgentName: agentName}
}

func promptAPISetup(configPath string) bool {
	reader := bufio.NewReader(os.Stdin)
	bold := "\033[1m"
	reset := "\033[0m"

	fmt.Println()
	fmt.Printf("%s🔑 API-opsætning%s\n", bold, reset)
	fmt.Println(strings.Repeat("─", 48))
	fmt.Println()
	fmt.Println("Ekte har brug for adgang til en AI-model.")
	fmt.Println("Vælg hvilken provider du vil bruge:")
	fmt.Println()
	fmt.Println("  1. Anthropic Claude  (betalt cloud-API)")
	fmt.Println("  2. OpenAI            (betalt cloud-API)")
	fmt.Println("  3. Lokal Ollama      (gratis, kører på din maskine)")
	fmt.Println()
	fmt.Printf("Valg [1-3]: ")

	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1":
		setConfigProvider(configPath, "anthropic", "claude-sonnet-4-6", "")
		printCloudKeyGuide("ANTHROPIC_API_KEY", "console.anthropic.com → API Keys")
		_, _ = reader.ReadString('\n')
		return false
	case "2":
		setConfigProvider(configPath, "openai", "gpt-4o", "")
		printCloudKeyGuide("OPENAI_API_KEY", "platform.openai.com → API Keys")
		_, _ = reader.ReadString('\n')
		return false
	case "3":
		return setupOllama(reader, configPath)
	default:
		fmt.Println("\nUgyldigt valg — genstart ekte og prøv igen.")
		return false
	}
}

func printCloudKeyGuide(envVar, keyURL string) {
	bold := "\033[1m"
	reset := "\033[0m"
	dim := "\033[2m"

	fmt.Println()
	fmt.Println("Din API-nøgle skal leve i en miljøvariabel — aldrig i en fil.")
	fmt.Printf("%sNøgler i filer risikerer at lække via git-historik.%s\n", dim, reset)
	fmt.Println()
	fmt.Println("Trin 1 — Hent din nøgle:")
	fmt.Printf("         %s\n", keyURL)
	fmt.Println()
	fmt.Println("Trin 2 — Sæt den i din aktuelle terminal-session:")
	fmt.Printf("         %sexport %s=\"din-nøgle-her\"%s\n", bold, envVar, reset)
	fmt.Println()
	fmt.Println("Trin 3 — Gør den permanent (tilføj til din shell-profil):")
	fmt.Printf("         %secho 'export %s=\"din-nøgle\"' >> ~/.bashrc%s\n", bold, envVar, reset)
	fmt.Printf("         %s# Brug ~/.zshrc i stedet hvis du kører zsh%s\n", dim, reset)
	fmt.Println()
	fmt.Println("Genstart ekte i en ny terminal når nøglen er sat.")
	fmt.Println()
	fmt.Printf("Tryk Enter for at afslutte...")
}

func setupOllama(reader *bufio.Reader, configPath string) bool {
	bold := "\033[1m"
	reset := "\033[0m"

	fmt.Println()
	fmt.Printf("%sLokal Ollama%s\n", bold, reset)
	fmt.Println(strings.Repeat("─", 48))
	fmt.Println()
	fmt.Println("Ollama kræver ingen API-nøgle — kører helt lokalt.")
	fmt.Println("Sørg for at Ollama er installeret: ollama.com")
	fmt.Println()

	fmt.Printf("Base URL (Enter = http://localhost:11434/v1): ")
	baseURL, _ := reader.ReadString('\n')
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}

	fmt.Printf("Model    (Enter = llama3.2): ")
	model, _ := reader.ReadString('\n')
	model = strings.TrimSpace(model)
	if model == "" {
		model = "llama3.2"
	}

	setConfigProvider(configPath, "openai", model, baseURL)
	fmt.Printf("\n✓ Config gemt. Starter ekte...\n\n")
	return true
}

// setConfigProvider opdaterer provider/model/base_url i config-filen uden at røre andre felter.
// api_key slettes aktivt — nøgler hører ikke hjemme i filer.
func setConfigProvider(configPath, prov, model, baseURL string) {
	_ = os.MkdirAll(filepath.Dir(configPath), 0755)
	raw := map[string]any{}
	if data, err := os.ReadFile(configPath); err == nil {
		_ = yaml.Unmarshal(data, &raw)
	}
	raw["provider"] = prov
	raw["model"] = model
	if baseURL != "" {
		raw["base_url"] = baseURL
	} else {
		delete(raw, "base_url")
	}
	delete(raw, "api_key")
	data, _ := yaml.Marshal(raw)
	_ = os.WriteFile(configPath, data, 0600)
}

// recentSessionsHint viser navnene på de op til 3 seneste gemte sessioner,
// så brugeren kan se hvordan man genoptager dem med 'ekte <navn>'.
func recentSessionsHint(sessionDir string) string {
	sessions, err := session.LoadAll(sessionDir)
	if err != nil || len(sessions) == 0 {
		return ""
	}
	var names []string
	for _, s := range sessions {
		if s.Name == "" {
			continue
		}
		names = append(names, s.Name)
	}
	if len(names) == 0 {
		return ""
	}
	return "💾 Seneste sessioner: " + strings.Join(names, " · ") +
		"  —  skriv `ekte <navn>` for at fortsætte hvor du slap"
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
		Provider  string                   `yaml:"provider"`
		Model     string                   `yaml:"model"`
		BaseURL   string                   `yaml:"base_url,omitempty"`
		Wiki      *wiki.Config             `yaml:"wiki,omitempty"`
		Whitelist provider.WhitelistConfig `yaml:"whitelist"`
		Hooks     map[string]string        `yaml:"hooks,omitempty"`
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
	fmt.Println()
	fmt.Println("Whitelist (tilladelser) er sat til false som standard.")
	fmt.Println("Rediger .ekte/config.yaml for at tillade operationer:")
	fmt.Println()
	fmt.Println("  whitelist:")
	fmt.Println("    git_worktree: true   # /spec opret/merge/fjern")
	fmt.Println("    wiki_write:   true   # /wiki gem")
	fmt.Println("    hook_run:     true   # /hook <navn>")
	fmt.Println()
	fmt.Println("Tilføj hooks med navne og shell-kommandoer, fx:")
	fmt.Println()
	fmt.Println("  hooks:")
	fmt.Println("    test: go test ./...")
	fmt.Println("    lint: golangci-lint run")
	fmt.Println()
	fmt.Println("Kør 'ekte' for at starte.")
}
