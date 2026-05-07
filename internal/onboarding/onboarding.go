package onboarding

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Result struct {
	ProjectName string
	Ok          bool
}

// IsFirstRun returnerer true hvis .ekte/ ikke eksisterer i den givne mappe.
func IsFirstRun(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".ekte"))
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
	fmt.Println("Wiki")
	fmt.Println("────")
	fmt.Println("En personlig wiki samler din viden på tværs af projekter.")
	if ask(r, "Vil du sætte en wiki op?") {
		wikiPath := runWikiSetup(r, dir)
		if wikiPath != "" {
			appendWikiConfig(configPath, wikiPath)
			fmt.Printf("✓ Wiki konfigureret: %s\n", wikiPath)
		}
	} else {
		fmt.Println("  Du kan altid sætte det op senere med 'ekte init'.")
	}

	fmt.Println()
	fmt.Println("✓ Alt klar!")
	fmt.Println()
	fmt.Print("Tryk Enter for at starte ekte...")
	readLine(r)
	return Result{Ok: true, ProjectName: projectName}, nil
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
	provider := promptChoice(r, "Hvilken LLM-provider vil du bruge?", []string{
		"OpenAI (GPT-4o mv.)",
		"Anthropic (Claude)",
		"Lokal (Ollama / LM Studio)",
	})
	fmt.Printf("   ✓ %s\n\n", provider)

	var providerKey, defaultModel, baseURL string
	switch {
	case strings.HasPrefix(provider, "Anthropic"):
		providerKey = "anthropic"
		defaultModel = "claude-sonnet-4-6"
	case strings.HasPrefix(provider, "Lokal"):
		providerKey = "openai"
		defaultModel = "llama3.2"
		baseURL = prompt(r, fmt.Sprintf("Base URL (tryk Enter for %s):", "http://localhost:11434/v1"))
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1"
		}
		fmt.Printf("   ✓ %s\n\n", baseURL)
	default:
		providerKey = "openai"
		defaultModel = "gpt-4o"
	}

	model := prompt(r, fmt.Sprintf("Model (tryk Enter for '%s'):", defaultModel))
	if model == "" {
		model = defaultModel
	}
	fmt.Printf("   ✓ %s\n\n", model)

	apiKey := ""
	if providerKey != "openai" || baseURL == "" {
		apiKey = prompt(r, "API-nøgle (lad stå tom for at bruge env-variabel):")
		if apiKey != "" {
			fmt.Println("   ✓ Nøgle gemt")
		} else {
			fmt.Println("   ✓ Bruger env-variabel")
		}
		fmt.Println()
	}

	baseURLLine := ""
	if baseURL != "" {
		baseURLLine = fmt.Sprintf("base_url: %q\n", baseURL)
	}

	content := fmt.Sprintf("provider: %s\nmodel: %s\n%sapi_key: %q\n",
		providerKey, model, baseURLLine, apiKey)

	return os.WriteFile(configPath, []byte(content), 0600)
}

func runWikiSetup(r *bufio.Reader, dir string) string {
	scope := promptChoice(r, "Skal wikien være global eller lokal?", []string{
		"Global — delt på tværs af projekter (~/.ekte/wiki)",
		"Lokal — kun dette projekt (.ekte/wiki)",
	})

	var wikiPath string
	if strings.HasPrefix(scope, "Lokal") {
		wikiPath = filepath.Join(dir, ".ekte", "wiki")
	} else {
		home, _ := os.UserHomeDir()
		wikiPath = filepath.Join(home, ".ekte", "wiki")
	}

	if _, err := os.Stat(wikiPath); err == nil {
		fmt.Printf("   ✓ Eksisterende wiki fundet: %s\n", wikiPath)
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
		fmt.Println("   Kloner wiki-template...")
		if err := cloneWiki("https://github.com/danskode/simple-wiki.git", wikiPath); err != nil {
			fmt.Printf("   ⚠  Kunne ikke klone template: %v\n", err)
			return ""
		}
	}
	return wikiPath
}

func cloneWiki(url, dest string) error {
	_ = os.MkdirAll(filepath.Dir(dest), 0755)
	out, err := runCmd("git", "clone", url, dest)
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
