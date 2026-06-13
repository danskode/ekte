package onboarding

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/danskode/ekte/internal/skill"
)

type Result struct {
	ProjectName string
	Ok          bool
}

// IsFirstRun returnerer true hvis global config (~/.ekte/config.yaml) ikke eksisterer endnu.
func IsFirstRun(dir string) bool {
	home, _ := os.UserHomeDir()
	_, err := os.Stat(filepath.Join(home, ".ekte", "config.yaml"))
	return os.IsNotExist(err)
}

// Run kører det interaktive onboarding-flow.
func Run(dir string) (Result, error) {
	r := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("Velkommen til ekte")
	fmt.Println("──────────────────")

	// 1. Trust-check
	fmt.Println()
	if !ask(r, "Stoler du på koden i denne mappe?") {
		fmt.Println("\nAfslutter — kør ekte igen i en mappe du stoler på.")
		return Result{Ok: false}, nil
	}
	fmt.Println("✓ Godt.")

	// 2. Initialiser mappestruktur
	for _, d := range []string{
		filepath.Join(dir, ".ekte"),
		filepath.Join(dir, ".ekte", "skills"),
		filepath.Join(dir, ".ekte", "hooks"),
		filepath.Join(dir, ".ekte", "sessions"),
		filepath.Join(dir, "specs"),
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return Result{}, fmt.Errorf("opret mappe %s: %w", d, err)
		}
	}

	// 2b. Beskyt private filer mod commit — config.yaml kan indeholde
	// base_url med privat IP, sessions/ den fulde samtalehistorik.
	if err := EnsureGitignore(dir); err != nil {
		fmt.Printf("⚠ Kunne ikke opdatere .gitignore: %v\n", err)
	}

	// 3. ekte.md
	var projectName string
	ekteMdPath := filepath.Join(dir, "ekte.md")
	if _, err := os.Stat(ekteMdPath); os.IsNotExist(err) {
		fmt.Println()
		fmt.Println("Der er ingen ekte.md endnu.")
		fmt.Println("Det er din projektkontekst — loades automatisk som baggrundsviden i hver session.")
		fmt.Println()
		if ask(r, "Vil du oprette den nu?") {
			var err error
			projectName, err = runPRDGuide(r, ekteMdPath)
			if err != nil {
				return Result{}, err
			}
		}
	} else {
		projectName = readProjectName(ekteMdPath)
	}

	// 4. LLM-opsætning
	fmt.Println()
	fmt.Println("LLM-opsætning")
	fmt.Println("─────────────")
	configPath := filepath.Join(dir, ".ekte", "config.yaml")
	if err := runLLMSetup(r, configPath); err != nil {
		return Result{}, err
	}

	// 5. Wiki
	fmt.Println()
	fmt.Println("simple-minded — lokalt videnslager")
	fmt.Println("──────────────────────────────────")
	fmt.Println("simple-minded samler din viden på tværs af projekter.")
	fmt.Println("Tilgå den manuelt med /wiki — aldrig automatisk injiceret.")
	skillsDir := filepath.Join(dir, ".ekte", "skills")
	if ask(r, "Vil du sætte en wiki op?") {
		wikiPath := runWikiSetup(r, dir)
		if wikiPath != "" {
			appendWikiConfig(configPath, wikiPath)
			fmt.Printf("✓ Wiki konfigureret: %s\n", wikiPath)
			// Wikien kræver vedligeholdelses-skills (gap-analyse m.m.) for at fungere.
			installRequired(skillsDir, "wiki")
		}
	} else {
		fmt.Println("  Du kan altid sætte det op senere med 'ekte init'.")
	}

	// 6. AIDD-skills — obligatoriske, da AIDD er præmissen for ekte.
	fmt.Println()
	fmt.Println("AIDD-skills (obligatoriske — præmissen for ekte)")
	fmt.Println("────────────────────────────────────────────────")
	installRequired(skillsDir, "harness")

	// 7. SKILLeton — øvrige, valgfrie skills
	fmt.Println()
	fmt.Println("Skills — SKILLeton")
	fmt.Println("──────────────────")
	fmt.Println("SKILLeton er et åbent bibliotek af skills til ekte.")
	fmt.Println("Valgte skills installeres permanent i .ekte/skills/ og kan bruges fremover.")
	fmt.Println("(Aktivér en installeret skill pr. prompt med /skills <navn> — den nulstilles bagefter.)")
	if ask(r, "Vil du vælge flere skills fra SKILLeton?") {
		runSkillCatalog(r, skillsDir)
	} else {
		fmt.Println("  Du kan tilføje skills senere med '/skills catalog' i ekte.")
	}

	fmt.Println()
	fmt.Println("✓ Alt klar!")
	fmt.Println()
	fmt.Print("Tryk Enter for at starte ekte...")
	readLine(r)
	return Result{Ok: true, ProjectName: projectName}, nil
}

// gitignoreEntries er de .ekte-stier der aldrig bør committes i et
// brugerprojekt: config.yaml kan indeholde base_url med privat IP,
// sessions/ rummer fuld samtalehistorik, memory/ private noter.
var gitignoreEntries = []string{
	".ekte/config.yaml",
	".ekte/sessions/",
	".ekte/memory/",
	".ekte/worktrees/",
	".ekte/plans/",
}

// EnsureGitignore sikrer at projektets .gitignore dækker ekte's private filer.
// Idempotent: eksisterende indhold bevares, og kun manglende poster tilføjes.
// Kaldes både ved onboarding og ved opstart i eksisterende projekter.
func EnsureGitignore(dir string) error {
	path := filepath.Join(dir, ".gitignore")
	existing := map[string]bool{}
	data, err := os.ReadFile(path)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			existing[strings.TrimSpace(line)] = true
		}
	}

	var missing []string
	for _, e := range gitignoreEntries {
		if !existing[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.Write(data)
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("\n# ekte — private filer (tilføjet automatisk)\n")
	for _, e := range missing {
		sb.WriteString(e + "\n")
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func runPRDGuide(r *bufio.Reader, path string) (string, error) {
	fmt.Println()
	fmt.Println("Jeg stiller dig seks korte spørgsmål.")
	fmt.Println()

	name := prompt(r, "1. Hvad hedder projektet?")
	fmt.Printf("   ✓ %s\n\n", name)

	projectType := promptChoice(r, "2. Hvilken type projekt?", []string{"cli", "webapp", "library", "api", "andet"})
	fmt.Printf("   ✓ %s\n\n", projectType)

	stack := prompt(r, "3. Hvilken teknisk stack bruger du?")
	fmt.Printf("   ✓ %s\n\n", stack)

	problem := prompt(r, "4. Hvilket problem løser det? (én sætning)")
	fmt.Printf("   ✓ Noteret\n\n")

	users := prompt(r, "5. Hvem er brugerne?")
	fmt.Printf("   ✓ %s\n\n", users)

	features := prompt(r, "6. Hvad er de tre vigtigste features i v1? (adskil med komma)")
	fmt.Printf("   ✓ Noteret\n\n")

	content := fmt.Sprintf(`---
name: %s
type: %s
stack: [%s]
status: aktiv
created: %s
---

# %s

## Hvad er dette projekt?

%s

Målgruppe: %s

## Teknisk stack

%s

## V1 features

%s

## Konventioner

- Spec-drevet workflow: skriv spec i specs/ inden implementation
- Brug /spec <navn> for at oprette en ny feature-branch
- Kode skal være lean og sikker — ingen unødvendige dependencies
`,
		name, projectType, stack, todayISO(),
		name, problem, users,
		formatList(stack),
		formatList(features),
	)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("skriv ekte.md: %w", err)
	}
	fmt.Printf("✓ ekte.md oprettet\n")
	return name, nil
}

func runLLMSetup(r *bufio.Reader, configPath string) error {
	providerChoice := promptChoice(r, "Hvilken LLM-provider vil du bruge?", []string{
		"OpenAI (GPT-4o mv.)",
		"Anthropic (Claude)",
		"Lokal (Ollama / LM Studio)",
	})
	fmt.Printf("   ✓ %s\n\n", providerChoice)

	var providerKey, defaultModel, baseURL, envVar, keyURL string
	switch {
	case strings.HasPrefix(providerChoice, "Anthropic"):
		providerKey = "anthropic"
		defaultModel = "claude-sonnet-4-6"
		envVar = "ANTHROPIC_API_KEY"
		keyURL = "console.anthropic.com"
	case strings.HasPrefix(providerChoice, "Lokal"):
		providerKey = "openai"
		defaultModel = "llama3.2"
		baseURL = prompt(r, "Base URL (tryk Enter for http://localhost:11434/v1):")
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1"
		}
		fmt.Printf("   ✓ %s\n\n", baseURL)
	default:
		providerKey = "openai"
		defaultModel = "gpt-4o"
		envVar = "OPENAI_API_KEY"
		keyURL = "platform.openai.com/api-keys"
	}

	model := prompt(r, fmt.Sprintf("Model (tryk Enter for '%s'):", defaultModel))
	if model == "" {
		model = defaultModel
	}
	fmt.Printf("   ✓ %s\n\n", model)

	// API-nøgle: altid via env-variabel — aldrig gemt i fil
	if envVar != "" {
		fmt.Println("   API-nøgle:")
		fmt.Printf("   Din nøgle hentes fra: %s\n", keyURL)
		fmt.Println("   Sæt den som env-variabel — den gemmes IKKE i config-filen:")
		fmt.Printf("   export %s=\"din-nøgle-her\"\n", envVar)
		fmt.Println()
		fmt.Println("   Tilføj linjen til ~/.bashrc eller ~/.zshrc så den er klar ved næste opstart.")
		fmt.Println()
	}

	baseURLLine := ""
	if baseURL != "" {
		baseURLLine = fmt.Sprintf("base_url: %q\n", baseURL)
	}

	content := fmt.Sprintf(
		"# ekte konfiguration\n"+
			"# API-nøgler gemmes ALDRIG her — brug env-variabel i stedet\n"+
			"# Se onboarding-output for vejledning\n\n"+
			"provider: %s\nmodel: %s\n%s",
		providerKey, model, baseURLLine)

	return os.WriteFile(configPath, []byte(content), 0600)
}

func runWikiSetup(r *bufio.Reader, dir string) string {
	scope := promptChoice(r, "Skal simple-minded være lokal (ekte-starter) eller global?", []string{
		"Lokal — i dette projekt (./wiki — anbefalet med ekte-starter)",
		"Global — delt på tværs af projekter (~/.ekte/wiki)",
	})

	var wikiPath string
	if strings.HasPrefix(scope, "Global") {
		home, _ := os.UserHomeDir()
		wikiPath = filepath.Join(home, ".ekte", "wiki")
	} else {
		wikiPath = filepath.Join(dir, "wiki")
	}

	if _, err := os.Stat(wikiPath); err == nil {
		fmt.Printf("   ✓ Eksisterende simple-minded fundet: %s\n", wikiPath)
		return wikiPath
	}

	fmt.Println()
	if ask(r, "Har du et eksisterende wiki-repo?") {
		url := prompt(r, "Git URL:")
		if err := cloneWiki(url, wikiPath); err != nil {
			fmt.Printf("   ⚠  Kunne ikke klone: %v\n", err)
			return ""
		}
	} else {
		const repo = "https://github.com/danskode/simple-minded.git"
		if ask(r, "Vil du starte med standard AIDD-indhold (færdige wiki-sider om AIDD)?") {
			fmt.Println("   Kloner Simple Minded med AIDD-startindhold...")
			if err := cloneWikiBranch(repo, "aidd", wikiPath); err != nil {
				fmt.Printf("   ⚠  Kunne ikke klone AIDD-branch: %v\n", err)
				return ""
			}
		} else {
			fmt.Println("   Kloner tom Simple Minded wiki-template...")
			if err := cloneWiki(repo, wikiPath); err != nil {
				fmt.Printf("   ⚠  Kunne ikke klone template: %v\n", err)
				return ""
			}
		}
	}
	return wikiPath
}

// installRequired auto-installerer de skills i SKILLeton der er markeret som
// obligatoriske for en given funktion (fx "harness" eller "wiki").
func installRequired(skillsDir, feature string) {
	cat, err := skill.FetchCatalog()
	if err != nil {
		return
	}
	required := cat.RequiredFor(feature)
	if len(required) == 0 {
		return
	}
	_ = os.MkdirAll(skillsDir, 0755)
	installed := skill.InstalledNames(skillsDir)
	for _, entry := range required {
		if installed[entry.Name] {
			continue
		}
		if err := skill.DownloadSkill(entry, skillsDir); err != nil {
			fmt.Printf("   ⚠  obligatorisk skill %s: %v\n", entry.Name, err)
			continue
		}
		fmt.Printf("   ✓ obligatorisk skill installeret: %s\n", entry.Name)
	}
}

func runSkillCatalog(r *bufio.Reader, skillsDir string) {
	fmt.Println("   Henter katalog fra SKILLeton...")
	cat, err := skill.FetchCatalog()
	if err != nil {
		fmt.Printf("   ⚠  Kunne ikke hente katalog: %v\n", err)
		fmt.Println("   Prøv igen med '/skills catalog' i ekte.")
		return
	}

	installed := skill.InstalledNames(skillsDir)

	fmt.Println()
	for i, s := range cat.Skills {
		marker := "  "
		if installed[s.Name] {
			marker = "✓ "
		}
		fmt.Printf("   %s%d. %-20s %s\n", marker, i+1, s.Name, s.Description)
	}
	fmt.Println()
	fmt.Println("   Vælg med numre adskilt af komma, fx: 1,3")
	fmt.Println("   Enter = spring over")
	fmt.Print("   → ")
	input := strings.TrimSpace(readLine(r))
	if input == "" {
		fmt.Println("   Springer over. Brug '/skills catalog' i ekte for at tilføje senere.")
		return
	}

	count := 0
	for _, idx := range parseChoices(input, len(cat.Skills)) {
		entry := cat.Skills[idx]
		if installed[entry.Name] {
			fmt.Printf("   ✓ %s allerede installeret\n", entry.Name)
			continue
		}
		if err := skill.DownloadSkill(entry, skillsDir); err != nil {
			fmt.Printf("   ⚠  %s: %v\n", entry.Name, err)
			continue
		}
		fmt.Printf("   ✓ %s installeret\n", entry.Name)
		count++
	}
	if count > 0 {
		fmt.Printf("   %d skill(s) installeret i .ekte/skills/\n", count)
	}
}

func parseChoices(input string, max int) []int {
	var result []int
	seen := map[int]bool{}
	for _, part := range strings.Split(input, ",") {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &n); err != nil {
			continue
		}
		idx := n - 1
		if idx >= 0 && idx < max && !seen[idx] {
			seen[idx] = true
			result = append(result, idx)
		}
	}
	return result
}

func cloneWiki(url, dest string) error {
	_ = os.MkdirAll(filepath.Dir(dest), 0755)
	// '--' sikrer at url/dest aldrig fortolkes som flag (argument injection).
	out, err := runCmd("git", "clone", "--", url, dest)
	if err != nil {
		return fmt.Errorf("%s", out)
	}
	return nil
}

// cloneWikiBranch kloner en specifik branch — bruges til AIDD-startindhold.
func cloneWikiBranch(url, branch, dest string) error {
	_ = os.MkdirAll(filepath.Dir(dest), 0755)
	out, err := runCmd("git", "clone", "--branch", branch, "--", url, dest)
	if err != nil {
		return fmt.Errorf("%s", out)
	}
	return nil
}

func appendWikiConfig(configPath, wikiPath string) {
	data, _ := os.ReadFile(configPath)
	content := string(data) + fmt.Sprintf("\nwiki:\n  enabled: true\n  path: %q\n", wikiPath)
	_ = os.WriteFile(configPath, []byte(content), 0600)
}

func ReadProjectName(ekteMdPath string) string {
	return readProjectName(ekteMdPath)
}

func readProjectName(ekteMdPath string) string {
	data, err := os.ReadFile(ekteMdPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		}
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}

func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func formatList(csv string) string {
	var sb strings.Builder
	for _, item := range strings.Split(csv, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			sb.WriteString("- " + item + "\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func toSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	var out strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out.WriteRune(r)
		}
	}
	return strings.Trim(out.String(), "-")
}

func promptChoice(r *bufio.Reader, question string, options []string) string {
	fmt.Printf("%s\n", question)
	for i, o := range options {
		fmt.Printf("  %d. %s\n", i+1, o)
	}
	fmt.Print("→ ")
	raw := strings.TrimSpace(readLine(r))
	for i, o := range options {
		if raw == fmt.Sprintf("%d", i+1) || strings.EqualFold(raw, o) {
			return o
		}
	}
	if raw == "" {
		return options[0]
	}
	return raw
}

func todayISO() string {
	info, err := os.Stat("/proc/self")
	if err != nil {
		return "ukendt"
	}
	mod := info.ModTime()
	return fmt.Sprintf("%d-%02d-%02d", mod.Year(), int(mod.Month()), mod.Day())
}

func ask(r *bufio.Reader, question string) bool {
	fmt.Printf("%s [j/n]: ", question)
	answer := strings.ToLower(strings.TrimSpace(readLine(r)))
	return answer == "j" || answer == "ja" || answer == "y" || answer == "yes"
}

func prompt(r *bufio.Reader, question string) string {
	fmt.Printf("%s\n→ ", question)
	return strings.TrimSpace(readLine(r))
}

func readLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}
