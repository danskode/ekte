package wiki

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Init kører interaktiv opsætning af wiki og returnerer den færdige Config.
func Init() (*Config, error) {
	r := bufio.NewReader(os.Stdin)

	fmt.Println("\nekte wiki-opsætning")
	fmt.Println("-------------------")

	if !ask(r, "Vil du bruge en personlig wiki?") {
		return &Config{Enabled: false}, nil
	}

	fmt.Println("\nHvor skal wikien ligge?")
	fmt.Println("  1. Global — delt på tværs af projekter (~/.ekte/wiki)")
	fmt.Println("  2. Lokal  — kun dette projekt (.ekte/wiki)")
	fmt.Print("Valg [1/2]: ")

	choice := readLine(r)
	var wikiPath string
	switch strings.TrimSpace(choice) {
	case "2":
		cwd, _ := os.Getwd()
		wikiPath = filepath.Join(cwd, ".ekte", "wiki")
	default:
		home, _ := os.UserHomeDir()
		wikiPath = filepath.Join(home, ".ekte", "wiki")
	}

	if _, err := os.Stat(wikiPath); err == nil {
		fmt.Printf("\nFandt eksisterende wiki på: %s\n", wikiPath)
		return &Config{Enabled: true, Path: wikiPath}, nil
	}

	var cloneURL string
	if ask(r, "\nHar du et eksisterende wiki-repo?") {
		fmt.Print("Git URL: ")
		cloneURL = strings.TrimSpace(readLine(r))
	} else {
		cloneURL = templateRepo
		fmt.Printf("\nKloner wiki-template fra %s...\n", cloneURL)
	}

	if err := cloneWiki(cloneURL, wikiPath); err != nil {
		return nil, fmt.Errorf("kunne ikke klone wiki: %w", err)
	}

	fmt.Printf("✓ Wiki oprettet på: %s\n", wikiPath)

	return &Config{Enabled: true, Path: wikiPath}, nil
}

func cloneWiki(url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	cmd := exec.Command("git", "clone", url, dest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ask(r *bufio.Reader, question string) bool {
	fmt.Printf("%s [j/n]: ", question)
	answer := strings.ToLower(strings.TrimSpace(readLine(r)))
	return answer == "j" || answer == "ja" || answer == "y" || answer == "yes"
}

func readLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}
