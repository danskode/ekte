package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/text/unicode/norm"

	"github.com/danskode/ekte/internal/agent"
	"github.com/danskode/ekte/internal/consent"
	"github.com/danskode/ekte/internal/ektelog"
	"github.com/danskode/ekte/internal/git"
	"github.com/danskode/ekte/internal/obs"
	"github.com/danskode/ekte/internal/onboarding"
	"github.com/danskode/ekte/internal/provider"
	"github.com/danskode/ekte/internal/review"
	"github.com/danskode/ekte/internal/secret"
	"github.com/danskode/ekte/internal/sensor"
	"github.com/danskode/ekte/internal/session"
	"github.com/danskode/ekte/internal/skill"
	"github.com/danskode/ekte/internal/springcheck"
	"github.com/danskode/ekte/internal/tools"
	"github.com/danskode/ekte/internal/tui"
	"github.com/danskode/ekte/internal/wiki"
	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "init" {
		runInit()
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "springcheck" {
		runSpringCheck()
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "review" {
		runReview()
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "verify" {
		runVerify()
		return
	}
	autoApprove := false
	sessionArg := ""
	goalText := ""
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-y" || args[i] == "--yes":
			autoApprove = true
		case args[i] == "goal":
			// Headless goal-mode: `ekte -y goal "<beskrivelse>"` kører mål-loopet
			// uden TUI — til CI, scripts og autonome kørsler.
			goalText = strings.Join(args[i+1:], " ")
			i = len(args)
		case sessionArg == "":
			sessionArg = args[i]
		}
	}
	if goalText != "" {
		runTUI("", autoApprove, goalText)
		return
	}
	runTUI(sessionArg, autoApprove, "")
}

func globalEkteDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ekte")
}

// runTUI bygger agenten og starter TUI'en — eller, hvis headlessGoal er sat,
// kører goal-loopet uden TUI med events på stdout (CI/autonome kørsler).
func runTUI(sessionArg string, autoApprove bool, headlessGoal string) {
	cwd, _ := os.Getwd()
	globalDir := globalEkteDir()

	var welcomeName string
	var onboardProfile *ekteProfile
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
		// Navne blev spurgt i onboardingen (Del 2) — brug dem, så vi ikke spørger igen.
		if result.UserName != "" || result.AgentName != "" {
			onboardProfile = &ekteProfile{UserName: result.UserName, AgentName: result.AgentName}
		}
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
	if onboardProfile != nil {
		// Navne kom fra onboardingen — gem dem uden at spørge igen.
		profile = *onboardProfile
		saveProfile(profile)
	} else if profile.UserName == "" || profile.AgentName == "" {
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

	// Privat base_url kræver samtykke: gemt i ~/.ekte/consent.yaml, givet
	// interaktivt her, eller EKTE_ALLOW_LOCAL_PROVIDER (headless override).
	// Projekt-config kan ikke selv give samtykke — filen bor kun globalt.
	if cfg != nil && consent.IsPrivateURL(cfg.BaseURL) {
		switch {
		case consent.EnvOverride() || consent.Granted(globalDir, cfg.BaseURL):
			cfg.AllowLocal = true
		case promptLocalProviderConsent(cfg.BaseURL):
			if err := consent.Grant(globalDir, cfg.BaseURL); err != nil {
				fmt.Fprintf(os.Stderr, "advarsel: kunne ikke gemme samtykke: %v\n", err)
			}
			cfg.AllowLocal = true
		default:
			fmt.Println("\nAfvist. Ret base_url i .ekte/config.yaml, eller genstart og svar 'j'.")
			os.Exit(0)
		}
	}

	if cfg != nil {
		p, _ = provider.NewFromConfig(cfg)
		wCfg := &wiki.Config{Enabled: cfg.Wiki.Enabled, Path: cfg.Wiki.Path}
		wikiInst, _ = wiki.New(wCfg)
	}

	globalSkillsDir := filepath.Join(globalDir, "skills")
	skills, skillErrs := skill.LoadAllFromDirs(globalSkillsDir, skillsDir)
	for _, e := range skillErrs {
		fmt.Fprintf(os.Stderr, "skill advarsel: %v\n", e)
	}

	repoRoot := ""
	if root, err := git.RepoRoot(cwd); err == nil {
		repoRoot = root
	}

	// Sørg for at .ekte's private filer er gitignored — også i projekter
	// onboardet før denne beskyttelse fandtes. Idempotent.
	if repoRoot != "" {
		if _, err := os.Stat(filepath.Join(cwd, ".ekte")); err == nil {
			if err := onboarding.EnsureGitignore(cwd); err != nil {
				fmt.Fprintf(os.Stderr, "advarsel: kunne ikke opdatere .gitignore: %v\n", err)
			}
		}
	}

	var whitelist provider.WhitelistConfig
	var hooks map[string]provider.HookConfig
	var containers provider.ContainerConfig
	var goal provider.GoalConfig
	var extraRoots []string
	if cfg != nil {
		whitelist = cfg.Whitelist
		hooks = cfg.Hooks
		containers = cfg.Containers
		goal = cfg.Goal
		// extra_roots fra den GLOBALE config er betroet (brugerens egen maskine).
		// extra_roots fra den projekt-lokale config kan komme fra et klonet,
		// ondsindet repo (CWE-668) — hver ny rod kræver derfor eksplicit
		// interaktivt samtykke, gemt globalt (analogt med private base_url'er).
		extraRoots = resolveExtraRoots(globalCfg, localCfg, globalDir)
	}
	if autoApprove {
		whitelist.AutoApprove = true
		fmt.Fprintln(os.Stderr, "⚠  -y/--yes: fil-bekræftelser er deaktiveret — LLM kan skrive filer uden godkendelse")
	}

	// hookTrusted afgør om et hook må køres uden videre samtykke. Tillid
	// bestemmes af OPRINDELSE, ikke kommando-streng: definerer projekt-configen
	// egne hooks, erstatter de den globale (MergeConfigs), og alle aktive hooks
	// er da projekt-lokale — de kan stamme fra et klonet repo og kræver eksplicit
	// samtykke (consent.yaml) eller EKTE_ALLOW_LOCAL_HOOKS. Kun når de aktive
	// hooks kommer fra den globale config (brugerens egen maskine) er de betroede
	// som standard.
	//
	// Tillid må IKKE arves på streng-match mod global config: build-kommandoer
	// som 'mvn compile', './mvnw' eller 'npm test' kører kode bestemt af repo-
	// filer (pom.xml, package.json), så samme kommando-streng = vidt forskellig
	// risiko i et klonet repo (CWE-829). Et fjendtligt repo kunne ellers efterabe
	// en global kommando og få sin egen build-/plugin-kode kørt autonomt.
	// Edge case: en tom-men-ikke-nil lokal hooks-nøgle (`hooks: {}`) gør
	// hooksFromGlobal falsk → alle hooks (ingen) regnes lokale. Det er fail-safe
	// (mere restriktivt, aldrig mindre), så vi skelner bevidst ikke mellem
	// fraværende og tom nøgle her.
	hooksFromGlobal := localCfg == nil || localCfg.Hooks == nil
	hookTrusted := func(cmd string) bool {
		return hooksFromGlobal || consent.AllowLocalHooks() || consent.GrantedHook(globalDir, cmd)
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
		} else if globalSessions := filepath.Join(globalDir, "sessions"); globalSessions != sessionDir {
			// Sessioner gemt FØR projektet fik .ekte/ ligger i den globale
			// mappe — uden fallback ser en navngiven session ud som "tom chat".
			if found, err := session.FindByName(globalSessions, sessionArg); err == nil && found != nil {
				resumeSession = found
			}
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
	baseURL := ""
	if cfg != nil {
		providerName = cfg.Provider
		modelName = cfg.Model
		baseURL = cfg.BaseURL
	}

	contextSize := 0
	if cfg != nil && cfg.ContextSize > 0 {
		contextSize = cfg.ContextSize
	}
	contextSize = clampToLoadedContext(cfg, contextSize, true)

	memory := loadMemory(globalDir, cwd)

	a := agent.New(agent.Config{
		Provider:         p,
		Skills:           skills,
		Wiki:             wikiInst,
		RepoRoot:         repoRoot,
		WorkDir:          cwd,
		SessionDir:       sessionDir,
		Whitelist:        whitelist,
		ExtraRoots:       extraRoots,
		Hooks:            hooks,
		Containers:       containers,
		Goal:             goal,
		Obs:              recorder,
		Log:              logger,
		ResumeSession:    resumeSession,
		AgentName:        profile.AgentName,
		ContextSize:      contextSize,
		ProviderName:     providerName,
		ModelName:        modelName,
		BaseURL:          baseURL,
		Memory:           memory,
		WorkDirForMemory: cwd,
		GlobalConfigPath: globalConfigPath,
		LocalConfigPath:  localConfigPath,
		GrantLocalURL: func(u string) error {
			// Kaldes kun af agenten efter eksplicit 'j' i model-wizardens
			// bekræftelsestrin — samtykket gemmes globalt som ved opstart.
			return consent.Grant(globalDir, u)
		},
		HookTrusted: hookTrusted,
		GrantHookConsent: func(cmd string) error {
			// Kaldes kun efter eksplicit 'j' på en run_hook-bekræftelse i
			// TUI'en — samtykket gemmes globalt så hooket fremover kan køre
			// i headless `-y goal`.
			return consent.GrantHook(globalDir, cmd)
		},
		ProbeContext: func() (string, int, bool) {
			if cfg == nil {
				return "", 0, false
			}
			return provider.ProbeLoadedContext(cfg)
		},
		OnProviderReload: func() (*agent.ReloadResult, error) {
			newGlobal, _ := provider.LoadConfig(globalConfigPath)
			newLocal, _ := provider.LoadConfig(localConfigPath)
			newCfg := provider.MergeConfigs(newGlobal, newLocal)
			if newCfg == nil {
				return nil, fmt.Errorf("ingen config fundet")
			}
			// Genvalidér disk-config'ens URL mod samtykkelisten: er filen
			// ændret udenom wizarden til en ikke-godkendt privat URL, afvises
			// reload — den nye URL kræver bekræftelse ved næste opstart.
			if consent.IsPrivateURL(newCfg.BaseURL) {
				if consent.EnvOverride() || consent.Granted(globalDir, newCfg.BaseURL) {
					newCfg.AllowLocal = true
				} else {
					return nil, fmt.Errorf(
						"base_url %s er privat og ikke godkendt — genstart ekte for at bekræfte", newCfg.BaseURL)
				}
			}
			newProv, err := provider.NewFromConfig(newCfg)
			if err != nil {
				return nil, err
			}
			newCtxSize := clampToLoadedContext(newCfg, newCfg.ContextSize, false)
			note := ""
			if newCfg.ContextSize > 0 && newCtxSize < newCfg.ContextSize {
				note = fmt.Sprintf(
					"Modellen er loadet med %d tokens context i LM Studio — config'en siger %d. "+
						"ekte retter sig efter %d; genindlæs modellen i LM Studio med større context for at hæve den.",
					newCtxSize, newCfg.ContextSize, newCtxSize)
			}
			return &agent.ReloadResult{
				Provider:     newProv,
				ProviderName: newCfg.Provider,
				ModelName:    newCfg.Model,
				ContextSize:  newCtxSize,
				BaseURL:      newCfg.BaseURL,
				CtxNote:      note,
			}, nil
		},
	})

	if headlessGoal != "" {
		runGoalLoop(a, headlessGoal, autoApprove, hookTrusted)
		return
	}

	m := tui.New(a)
	m.SetNames(profile.UserName, profile.AgentName)
	m.SetMaxTokens(contextSize)
	m.SetModelName(modelName)
	m.SetWorkDir(cwd)

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
	} else if sessionArg != "" {
		// Navngiven session ikke fundet: sig det højt — ellers ligner det bare
		// en tom chat, og brugeren tror historikken er væk.
		m.AddWarning(fmt.Sprintf("⚠  Session '%s' blev ikke fundet (søgt i %s og den globale mappe) — startede en ny session.", sessionArg, sessionDir))
	}
	// Uden fil-rettigheder får LLM'en slet ingen tools tilbudt — den svarer så
	// bare "jeg kan ikke skrive til mappen" uden at brugeren kan se hvorfor.
	if !whitelist.FileRead && !whitelist.FileWrite {
		m.AddInfo("ℹ  Fil-tools er slået fra — modellen kan hverken læse eller skrive filer.\n" +
			"   Slå til i .ekte/config.yaml (whitelist.file_read / file_write) eller kør /init.\n" +
			"   Guide: https://github.com/danskode/ekte#konfiguration")
	}
	if isFirstRun {
		m.SetWelcome(welcomeName) // første besked efter install
	} else if resumeSession == nil {
		// Senere åbning (ikke en genoptaget session): kort velkommen-tilbage.
		m.SetWelcomeBack(profile.UserName, recentSessionNames(sessionDir))
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

// promptLocalProviderConsent viser opstartsdialogen for en privat provider-URL
// — i stil med onboardingens tillidstrin. Returnerer true ved 'j'.
// resolveExtraRoots samler de tilladte fil-rødder uden for projektmappen.
// Globale rødder er betroede; lokale (fra projekt-config) gates med samtykke
// — env-override (EKTE_ALLOW_LOCAL_PROVIDER) eller tidligere/ny godkendelse.
// Ikke-godkendte lokale rødder udelades stille, så ekte stadig kan starte.
func resolveExtraRoots(globalCfg, localCfg *provider.Config, globalDir string) []string {
	var roots []string
	seen := map[string]bool{}
	add := func(r string) {
		if !seen[r] {
			seen[r] = true
			roots = append(roots, r)
		}
	}
	if globalCfg != nil {
		for _, r := range tools.NormalizeExtraRoots(globalCfg.ExtraRoots) {
			add(r)
		}
	}
	if localCfg == nil {
		return roots
	}
	for _, r := range tools.NormalizeExtraRoots(localCfg.ExtraRoots) {
		if seen[r] {
			continue // allerede betroet via global config
		}
		switch {
		case consent.EnvOverride() || consent.GrantedRoot(globalDir, r):
			add(r)
		case promptExtraRootConsent(r):
			if err := consent.GrantRoot(globalDir, r); err != nil {
				fmt.Fprintf(os.Stderr, "advarsel: kunne ikke gemme samtykke for %s: %v\n", r, err)
			}
			add(r)
		default:
			fmt.Fprintf(os.Stderr, "ℹ extra_root %s ikke godkendt — udeladt (fil-adgang forbliver i projektmappen).\n", r)
		}
	}
	return roots
}

// promptExtraRootConsent beder om bekræftelse på en projekt-lokal extra_root —
// den udvider LLM'ens fil-sandkasse uden for projektmappen, så en ubetroet
// config ikke må give sig selv adgangen.
func promptExtraRootConsent(root string) bool {
	bold := "\033[1m"
	reset := "\033[0m"
	dim := "\033[2m"

	fmt.Println()
	fmt.Printf("%s⚠  Udvidet fil-adgang%s\n", bold, reset)
	fmt.Println(strings.Repeat("─", 48))
	fmt.Println()
	fmt.Printf("Projektets config beder om læse/skrive-adgang uden for projektmappen:\n  %s%s%s\n", bold, root, reset)
	fmt.Printf("%sGodkend kun hvis du stoler på dette projekt og selv ønsker adgangen.%s\n", dim, reset)
	fmt.Println()
	fmt.Printf("Tillad? [j/n] > ")

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "j" || answer == "ja" || answer == "y" || answer == "yes"
}

func promptLocalProviderConsent(baseURL string) bool {
	bold := "\033[1m"
	reset := "\033[0m"
	dim := "\033[2m"

	fmt.Println()
	fmt.Printf("%s⚠  Lokal provider%s\n", bold, reset)
	fmt.Println(strings.Repeat("─", 48))
	fmt.Println()
	fmt.Printf("config peger på %s%s%s (privat adresse).\n", bold, baseURL, reset)
	fmt.Printf("%sTypisk Ollama eller LM Studio — bekræft kun hvis du selv har sat den.%s\n", dim, reset)
	fmt.Println()
	fmt.Printf("Tillad? [j/n] > ")

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "j" || answer == "ja" || answer == "y" || answer == "yes"
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
	// Brugeren har selv indtastet/bekræftet URL'en her — gem samtykket med det
	// samme, så opstartsdialogen ikke spørger igen om to sekunder.
	if consent.IsPrivateURL(baseURL) {
		if err := consent.Grant(globalEkteDir(), baseURL); err != nil {
			fmt.Fprintf(os.Stderr, "advarsel: kunne ikke gemme samtykke: %v\n", err)
		}
	}
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

// recentSessionNames returnerer navnene på de gemte sessioner (nyeste først),
// brugt af velkommen-tilbage-beskeden til at vise hvordan man genoptager.
func recentSessionNames(sessionDir string) []string {
	sessions, err := session.LoadAll(sessionDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, s := range sessions {
		if s.Name != "" {
			names = append(names, s.Name)
		}
	}
	return names
}

func loadEkteMd(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "ekte.md"))
	if err != nil {
		return ""
	}
	// Saniter den auto-genererede (LLM-skrevne) sektion mod persisteret
	// prompt injection — brugerens egen tekst røres ikke.
	return strings.TrimSpace(agent.SanitizeEkteMd(string(data)))
}

// runGoalLoop kører /goal headless: events skrives til stdout, og tool-
// bekræftelser besvares programmatisk — godkendt med -y, ellers afvist.
// hookTrusted gater dog run_hook: projekt-lokale, ikke-betroede hooks afvises
// selv med -y, så et klonet repo ikke kan auto-eksekvere vilkårlige kommandoer
// (CWE-78/829). Exitkode 0 hvis målet blev nået, ellers 1.
func runGoalLoop(a *agent.Agent, goalText string, autoApprove bool, hookTrusted func(cmd string) bool) {
	if !autoApprove {
		fmt.Fprintln(os.Stderr, "⚠  headless goal uden -y: alle skriveoperationer afvises — kør med -y for at godkende automatisk")
	} else {
		// -y goal auto-godkender fil-skrivninger. Hooks auto-godkendes KUN hvis
		// de er betroede (global config, EKTE_ALLOW_LOCAL_HOOKS, eller godkendt
		// før); ikke-betroede projekt-lokale hooks afvises uanset -y.
		fmt.Fprintln(os.Stderr, "⚠  -y goal: skriveoperationer auto-godkendes uden bekræftelse — kør kun i et repo du stoler på.")
	}
	ch := a.ProcessStream(context.Background(), "/goal "+goalText)
	reached := false
	for ev := range ch {
		switch ev.Type {
		case agent.EventToolConfirm:
			switch {
			case !autoApprove:
				fmt.Println("✗ afvist (mangler -y): " + ev.Content)
				ev.ConfirmCh <- agent.ConfirmResponse{Approved: false}
			case ev.HookName != "" && hookTrusted != nil && !hookTrusted(ev.HookCmd):
				// Projekt-lokal hook der ikke er betroet — afvis selv med -y.
				fmt.Printf("✗ afvist: hook '%s' er ikke betroet i headless (%s)\n"+
					"  Kør ekte interaktivt og godkend hooket én gang, eller sæt EKTE_ALLOW_LOCAL_HOOKS=1.\n",
					ev.HookName, ev.HookCmd)
				ev.ConfirmCh <- agent.ConfirmResponse{Approved: false}
			default:
				fmt.Println("⚙ auto-godkendt: " + ev.Content)
				ev.ConfirmCh <- agent.ConfirmResponse{Approved: true}
			}
		case agent.EventStreamToken, agent.EventReasoningToken, agent.EventThinking,
			agent.EventTokenCount, agent.EventModelInfo:
			// Stille i headless — det fulde svar kommer i EventStreamDone.
		default:
			if ev.Content != "" {
				fmt.Println(ev.Content)
				if strings.Contains(ev.Content, "✓ Mål nået") {
					reached = true
				}
			}
		}
	}
	if !reached {
		os.Exit(1)
	}
}

// clampToLoadedContext sænker contextSize til modellens faktisk loadede
// context-længde, hvis LM Studio rapporterer en mindre (ProbeLoadedContext).
// Uden dette trimmer ekte historikken efter config'ens context_size, sender
// prompts modellen ikke kan rumme, og LM Studio afviser dem med en SSE-fejl
// der ender som kryptisk "unexpected end of JSON input". warn styrer om der
// skrives en advarsel til stderr — kun ved opstart, før TUI'en tager skærmen.
func clampToLoadedContext(cfg *provider.Config, contextSize int, warn bool) int {
	id, loaded, ok := provider.ProbeLoadedContext(cfg)
	if !ok {
		return contextSize
	}
	if contextSize == 0 || loaded < contextSize {
		if warn && contextSize > 0 {
			fmt.Fprintf(os.Stderr,
				"⚠  %s er loadet med %d tokens context i LM Studio — config'ens context_size er %d.\n"+
					"   ekte retter sig efter %d; genindlæs modellen med større context for at få mere.\n",
				id, loaded, contextSize, loaded)
		}
		return loaded
	}
	return contextSize
}

// loadMemory læser hukommelsesfiler fra global (~/.ekte/memory/) og
// projekt-lokal (.ekte/memory/) mappe og returnerer dem som system-beskeder.
// Global læses først (lavere prioritet), lokal herefter (højere prioritet).
// Indhold saniteres mod prompt injection inden injection.
func loadMemory(globalDir, workDir string) []provider.Message {
	var msgs []provider.Message

	dirs := []struct {
		path  string
		label string
	}{
		{filepath.Join(globalDir, "memory"), "global"},
		{filepath.Join(workDir, ".ekte", "memory"), "lokal"},
	}

	for _, d := range dirs {
		entries, err := os.ReadDir(d.path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(d.path, entry.Name()))
			if err != nil {
				continue
			}
			content := strings.TrimSpace(string(data))
			if content == "" {
				continue
			}
			// Fjern YAML frontmatter (--- ... ---) inden sanitering
			cleaned := stripFrontmatter(content)
			sanitized := sanitizeMemoryContent(cleaned)
			if sanitized == "" {
				continue
			}
			// Sanitér filnavnet inden det skrives til system-beskeden —
			// en angriber med adgang til ~/.ekte/memory/ kan ellers injicere
			// via filnavnet (fx "ignore all instructions.md").
			safeLabel := sanitizeMemoryContent(entry.Name())
			msgs = append(msgs, provider.Message{
				Role:    "system",
				Content: "[Hukommelse — " + d.label + "/" + safeLabel + "]\n" + sanitized,
			})
		}
	}
	return msgs
}

// stripFrontmatter fjerner YAML frontmatter (--- ... ---) fra en markdown-fil.
func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	rest := strings.TrimPrefix(content, "---")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return content
	}
	return strings.TrimSpace(rest[idx+4:])
}

// memoryInjectionPattern spejler agent.injectionPattern — holdes synkroniseret.
var memoryInjectionPattern = regexp.MustCompile(`(?i)(` +
	`ignore\s+(all\s+|the\s+)?(previous|prior|above)|` +
	`disregard\s+(all\s+|the\s+)?(previous|prior|above)|` +
	`forget\s+(all\s+|your\s+|the\s+)?(previous|prior|instructions)|` +
	`new\s+instructions?\s*[:.]|` +
	`system\s*[:]|<\|?(im_start|im_end|system|assistant|user)\|?>|` +
	`\[(system|inst)\]|<(human|assistant|system)>|` +
	`you\s+(are\s+now|must\s+now)|act\s+as\s+(a|an)|` +
	`reveal\s+(your|the)\s+(prompt|instructions|system)|` +
	`print\s+your\s+(prompt|instructions|system)` +
	`)`)

func sanitizeMemoryContent(content string) string {
	// NFKC-normaliser inden regex-match for at fange Unicode-homoglyf-omgåelser.
	normalized := norm.NFKC.String(content)
	// Tjek også flattened version for at fange sætninger splittet over linjeskift (CWE-74).
	flattened := strings.ReplaceAll(normalized, "\n", " ")
	if memoryInjectionPattern.MatchString(flattened) {
		return "[indhold fjernet: mulig prompt injection]"
	}
	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		if memoryInjectionPattern.MatchString(line) {
			lines[i] = "[linje fjernet: mulig prompt injection]"
		}
	}
	return strings.Join(lines, "\n")
}

// runSpringCheck kører Java+Thymeleaf-goal-tjekket i den aktuelle mappe.
// Valgfrit argument: "bruger:kode" til det autentificerede flow (default
// admin:admin). Exitkode 0 = mål nået (hook-konventionen for goal.check_hook).
func runSpringCheck() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fejl: %v\n", err)
		os.Exit(1)
	}
	login := ""
	if len(os.Args) > 2 {
		login = os.Args[2]
	}
	rep := springcheck.Run(context.Background(), cwd, login)
	for _, line := range rep.Lines {
		fmt.Println(line)
	}
	if !rep.OK {
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

	type goalSection struct {
		CheckHook     string `yaml:"check_hook"`
		MaxIterations int    `yaml:"max_iterations"`
	}
	type fullConfig struct {
		Provider  string                   `yaml:"provider"`
		Model     string                   `yaml:"model"`
		BaseURL   string                   `yaml:"base_url,omitempty"`
		Wiki      *wiki.Config             `yaml:"wiki,omitempty"`
		Whitelist provider.WhitelistConfig `yaml:"whitelist"`
		Hooks     map[string]string        `yaml:"hooks,omitempty"`
		Goal      *goalSection             `yaml:"goal,omitempty"`
	}

	cfg := fullConfig{Provider: "openai", Model: "gpt-4o", Wiki: wikiCfg}
	if data, err := os.ReadFile(configPath); err == nil {
		_ = yaml.Unmarshal(data, &cfg)
		cfg.Wiki = wikiCfg
	}

	// Java + Thymeleaf (Spring Boot): sæt hooks og goal op automatisk, så
	// /goal virker fra start — succes = compile uden fejl + ingen Whitelabel-
	// fejl eller døde links/endpoints (tjekkes af `ekte springcheck`).
	springProject := false
	if pom, err := os.ReadFile("pom.xml"); err == nil && strings.Contains(string(pom), "thymeleaf") {
		springProject = true
		if cfg.Hooks == nil {
			cfg.Hooks = map[string]string{}
		}
		if _, ok := cfg.Hooks["compile"]; !ok {
			cfg.Hooks["compile"] = "mvn -q compile"
		}
		if _, ok := cfg.Hooks["test"]; !ok {
			cfg.Hooks["test"] = "mvn -q test"
		}
		if _, ok := cfg.Hooks["goalcheck"]; !ok {
			cfg.Hooks["goalcheck"] = "ekte springcheck"
		}
		if cfg.Goal == nil {
			cfg.Goal = &goalSection{CheckHook: "goalcheck", MaxIterations: 10}
		}
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
	if springProject {
		fmt.Println("✓ Java + Thymeleaf genkendt — hooks (compile/test/goalcheck) og goal er sat op.")
		fmt.Println("  /goal <beskrivelse> arbejder til compile er ren og alle sider/endpoints")
		fmt.Println("  svarer uden Whitelabel-fejl — og viser så projektets lokale adresse.")
	}
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

// runReview kører et provider-agnostisk sikkerhedsreview af git-diffen via den
// model brugeren har valgt (inkl. lokal LM Studio/Ollama). Bruges af 'ekte review'
// og af safe-flow's pre-push-hook. Exit-koder: 0 = lav risiko; 1 = medium+ fund
// (gate blokerer); 2 = kunne ikke køre/fortolke — FAIL-CLOSED (blokerer), så et
// uforståeligt svar aldrig stilles lig grønt lys. Opt-out til fail-open:
// --allow-failopen eller EKTE_REVIEW_ALLOW_FAILOPEN=1 (bevidst usikkert; sæt aldrig
// i CI/branch-protection-kontekster).
func runReview() {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	localCfg, _ := provider.LoadConfig(filepath.Join(cwd, ".ekte", "config.yaml"))
	globalCfg, _ := provider.LoadConfig(filepath.Join(home, ".ekte", "config.yaml"))
	cfg := provider.MergeConfigs(globalCfg, localCfg)
	if cfg == nil {
		fmt.Fprintln(os.Stderr, "ekte review: ingen config fundet (.ekte/config.yaml). Kør 'ekte init'.")
		os.Exit(2)
	}
	globalDir := filepath.Join(home, ".ekte")
	if consent.IsPrivateURL(cfg.BaseURL) {
		if consent.EnvOverride() || consent.Granted(globalDir, cfg.BaseURL) {
			cfg.AllowLocal = true
		} else {
			fmt.Fprintln(os.Stderr, "ekte review: lokal provider kræver samtykke. Start ekte normalt én gang og godkend, eller sæt EKTE_ALLOW_LOCAL_PROVIDER=1.")
			os.Exit(2)
		}
	}
	p, err := provider.NewFromConfig(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ekte review: kunne ikke oprette provider:", err)
		os.Exit(2)
	}

	diff, label, derr := gitDiffForReview(cwd)
	if derr != nil {
		fmt.Fprintln(os.Stderr, "ekte review: kunne ikke hente git-diff:", derr)
		os.Exit(2)
	}
	diff, redacted := secret.Redact(diff)
	if redacted > 0 {
		fmt.Fprintf(os.Stderr, "ekte review: %d potentielle secret(s) redakteret (best-effort heuristik — IKKE en garanti; kør et dedikeret secret-scan som gitleaks/trufflehog).\n", redacted)
	}
	if !consent.IsPrivateURL(cfg.BaseURL) {
		fmt.Fprintln(os.Stderr, "ekte review: diffen sendes til en ekstern provider (kun redakteret indhold).")
	}
	res, raw, err := review.Run(context.Background(), p, diff, label)
	if err != nil {
		// Fail-CLOSED som default (CWE-636): et uforståeligt svar er ikke grønt
		// lys. Opt-out for upålidelige lokale modeller via flag/env — men da
		// blokerer reviewet ikke længere, så det er bevidst usikkert.
		allowFailopen := os.Getenv("EKTE_REVIEW_ALLOW_FAILOPEN") == "1"
		for _, a := range os.Args {
			if a == "--allow-failopen" {
				allowFailopen = true
			}
		}
		fmt.Fprintln(os.Stderr, "ekte review: kunne ikke fortolke modellens svar.")
		fmt.Fprintln(os.Stderr, err)
		if raw != "" {
			fmt.Fprintln(os.Stderr, "rå svar:\n"+raw)
		}
		if allowFailopen {
			fmt.Fprintln(os.Stderr, "(--allow-failopen/EKTE_REVIEW_ALLOW_FAILOPEN sat — lader passere; usikkert)")
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "Gate fejl-lukket (blokerer). Sæt EKTE_REVIEW_ALLOW_FAILOPEN=1 eller --allow-failopen for at tillade.")
		os.Exit(2)
	}
	fmt.Println(review.Format(res))
	if res.Blocking() {
		os.Exit(1)
	}
}

// runVerify kører sensor-pakken (sikkerhed + intent-conformance) på git-diffen via
// den valgte provider — samme byggesten som /goal-loopets Validate-fase, eksponeret
// til pre-push og ad hoc-brug. Exit: 0 bestået · 1 blokeret · 2 afklaring/uafgjort.
func runVerify() {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	localCfg, _ := provider.LoadConfig(filepath.Join(cwd, ".ekte", "config.yaml"))
	globalCfg, _ := provider.LoadConfig(filepath.Join(home, ".ekte", "config.yaml"))
	cfg := provider.MergeConfigs(globalCfg, localCfg)
	if cfg == nil {
		fmt.Fprintln(os.Stderr, "ekte verify: ingen config fundet (.ekte/config.yaml). Kør 'ekte init'.")
		os.Exit(2)
	}
	globalDir := filepath.Join(home, ".ekte")
	if consent.IsPrivateURL(cfg.BaseURL) {
		if consent.EnvOverride() || consent.Granted(globalDir, cfg.BaseURL) {
			cfg.AllowLocal = true
		} else {
			fmt.Fprintln(os.Stderr, "ekte verify: lokal provider kræver samtykke. Start ekte normalt én gang og godkend, eller sæt EKTE_ALLOW_LOCAL_PROVIDER=1.")
			os.Exit(2)
		}
	}
	p, err := provider.NewFromConfig(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ekte verify: kunne ikke oprette provider:", err)
		os.Exit(2)
	}
	diff, _, derr := gitDiffForReview(cwd)
	if derr != nil {
		fmt.Fprintln(os.Stderr, "ekte verify: kunne ikke hente git-diff:", derr)
		os.Exit(2)
	}
	if strings.TrimSpace(diff) == "" {
		fmt.Println("Ingen ændringer at verificere.")
		return
	}
	if !consent.IsPrivateURL(cfg.BaseURL) {
		fmt.Fprintln(os.Stderr, "ekte verify: diffen sendes til en ekstern provider (kun redakteret indhold).")
	}
	if len(cfg.Goal.SuccessCriteria) == 0 {
		fmt.Fprintln(os.Stderr, "ekte verify: ingen succeskriterier i config (goal.success_criteria) — intent-conformance kan ikke vurderes (kun sikkerhed).")
	}

	sensors := []sensor.Sensor{
		sensor.SecuritySensor{P: p},
		sensor.IntentSensor{P: p},
	}
	verdicts, err := sensor.RunAll(context.Background(), sensors, sensor.Input{
		Criteria: cfg.Goal.SuccessCriteria,
		Diff:     diff,
	})
	if err != nil {
		// Fail-CLOSED (CWE-636): kan en sensor ikke nå sin provider, er det ikke grønt lys.
		fmt.Fprintln(os.Stderr, "ekte verify: kunne ikke gennemføre:", err)
		os.Exit(2)
	}
	fmt.Println(sensor.Format(verdicts))
	sum := sensor.Aggregate(verdicts)
	switch {
	case sum.NeedsClarification:
		fmt.Fprintln(os.Stderr, "Afklaring nødvendig — intentionen kan ikke vurderes uden præcisering.")
		os.Exit(2)
	case !sum.Pass:
		os.Exit(1)
	}
}

// gitDiffForReview vælger upushede commits (vs upstream) hvis muligt, ellers
// arbejdstræet mod HEAD. Returnerer fejl eksplicit, så en git-fejl ikke maskeres
// som en tom diff ("intet at reviewe").
func gitDiffForReview(dir string) (string, string, error) {
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "@{u}").Output(); err == nil {
		up := strings.TrimSpace(string(out))
		if up != "" {
			d, err := exec.Command("git", "-C", dir, "diff", up+"..HEAD").Output()
			if err != nil {
				return "", "", fmt.Errorf("git diff %s..HEAD: %w", up, err)
			}
			if len(strings.TrimSpace(string(d))) > 0 {
				return string(d), "upushede commits vs " + up, nil
			}
		}
	}
	d, err := exec.Command("git", "-C", dir, "diff", "HEAD").Output()
	if err != nil {
		return "", "", fmt.Errorf("git diff HEAD: %w", err)
	}
	return string(d), "arbejdstræ vs HEAD", nil
}
