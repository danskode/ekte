package onboarding

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsFirstRun returnerer true hvis .ekte/ ikke eksisterer i den givne mappe.
func IsFirstRun(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".ekte"))
	return os.IsNotExist(err)
}

// Run kører det interaktive onboarding-flow.
// Returnerer false hvis brugeren afviser trust-check.
func Run(dir string) (bool, error) {
	r := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("Velkommen til ekte")
	fmt.Println("──────────────────")

	// 1. Trust-check
	fmt.Println()
	if !ask(r, "Stoler du på koden i denne mappe?") {
		fmt.Println("\nAfslutter — kør ekte igen i en mappe du stoler på.")
		return false, nil
	}

	// 2. Initialiser .ekte/
	dirs := []string{
		filepath.Join(dir, ".ekte"),
		filepath.Join(dir, ".ekte", "skills"),
		filepath.Join(dir, ".ekte", "hooks"),
		filepath.Join(dir, ".ekte", "sessions"),
		filepath.Join(dir, "specs"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return false, fmt.Errorf("opret mappe %s: %w", d, err)
		}
	}

	// kopier config-eksempel hvis ingen config findes
	configPath := filepath.Join(dir, ".ekte", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		examplePath := filepath.Join(dir, ".ekte", "config.yaml.example")
		if data, err := os.ReadFile(examplePath); err == nil {
			_ = os.WriteFile(configPath, data, 0600)
		} else {
			writeDefaultConfig(configPath)
		}
	}

	// 3. ekte.md
	ekteMdPath := filepath.Join(dir, "ekte.md")
	if _, err := os.Stat(ekteMdPath); os.IsNotExist(err) {
		fmt.Println()
		fmt.Println("Der er ingen ekte.md endnu.")
		fmt.Println("Det er din projektkontekst — svarende til CLAUDE.md i Claude Code.")
		fmt.Println("Den loades automatisk som baggrundsviden i hver session.")
		fmt.Println()
		if ask(r, "Vil du oprette den nu?") {
			if err := runPRDGuide(r, ekteMdPath); err != nil {
				return false, err
			}
		}
	}

	// 4. Provider-check
	checkProvider(configPath)

	fmt.Println()
	fmt.Println("✓ Klar — starter ekte...")
	fmt.Println()
	return true, nil
}

func runPRDGuide(r *bufio.Reader, path string) error {
	fmt.Println()
	fmt.Println("Jeg stiller dig fem korte spørgsmål.")
	fmt.Println()

	name := prompt(r, "1. Hvad hedder projektet?")
	problem := prompt(r, "2. Hvilket problem løser det? (én sætning)")
	users := prompt(r, "3. Hvem er brugerne?")
	features := prompt(r, "4. Hvad er de tre vigtigste features i v1? (adskil med komma)")
	stack := prompt(r, "5. Hvilken teknisk stack bruger du?")

	featureList := formatList(features)

	content := fmt.Sprintf(`# %s

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

## Kom i gang

Skriv '/spec <feature-navn>' for at starte på din første feature.
`, name, problem, users, stack, featureList)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("skriv ekte.md: %w", err)
	}

	fmt.Printf("\n✓ ekte.md oprettet\n")
	fmt.Printf("  Tip: skriv '/spec %s' for at starte på din første feature.\n",
		toSlug(strings.Split(features, ",")[0]))
	return nil
}

func checkProvider(configPath string) {
	hasKey := os.Getenv("OPENAI_API_KEY") != "" ||
		os.Getenv("ANTHROPIC_API_KEY") != ""

	if hasKey {
		return
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	if strings.Contains(string(data), "api_key: \"\"") || !strings.Contains(string(data), "api_key:") {
		fmt.Println()
		fmt.Println("⚠  Ingen API-nøgle fundet.")
		fmt.Println("   Sæt din nøgle i .ekte/config.yaml eller som env-variabel:")
		fmt.Println("   export OPENAI_API_KEY=...  eller  export ANTHROPIC_API_KEY=...")
	}
}

func writeDefaultConfig(path string) {
	content := `provider: openai
model: gpt-4o
base_url: ""
api_key: ""
`
	_ = os.WriteFile(path, []byte(content), 0600)
}

func formatList(csv string) string {
	items := strings.Split(csv, ",")
	var sb strings.Builder
	for _, item := range items {
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
