package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danskode/ekte/internal/provider"
)

// Definitions returnerer tool-definitioner til brug i LLM-kald.
// canWrite styrer om write_file, edit_file og create_dir er inkluderet.
func Definitions(canRead, canWrite bool) []provider.ToolDefinition {
	var defs []provider.ToolDefinition
	if canRead {
		defs = append(defs,
			provider.ToolDefinition{
				Name:        "read_file",
				Description: "Læs indholdet af en fil. Sti er relativ til projektmappen.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Filsti relativ til projektmappen",
						},
					},
					"required": []string{"path"},
				},
			},
			provider.ToolDefinition{
				Name:        "search_files",
				Description: "Søg efter filer der matcher et mønster eller indeholder en streng.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"pattern": map[string]any{
							"type":        "string",
							"description": "Glob-mønster (fx '**/*.go') eller tekststreng at søge efter i filnavne",
						},
						"contains": map[string]any{
							"type":        "string",
							"description": "Valgfri: tekst der skal forekomme i filindholdet",
						},
					},
					"required": []string{"pattern"},
				},
			},
		)
	}
	if canWrite {
		defs = append(defs,
			provider.ToolDefinition{
				Name:        "write_file",
				Description: "Skriv eller overskriv en fil. Opretter automatisk manglende mapper. Sti er relativ til projektmappen.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Filsti relativ til projektmappen",
						},
						"content": map[string]any{
							"type":        "string",
							"description": "Filindholdet der skal skrives",
						},
					},
					"required": []string{"path", "content"},
				},
			},
			provider.ToolDefinition{
				Name: "edit_file",
				Description: "Rediger en eksisterende fil. To tilstande: " +
					"(1) old_string + new_string: erstat præcis tekst — old_string skal være unik i filen. " +
					"(2) insert_after + new_string: indsæt ny tekst lige efter en markør-linje uden at erstatte noget. " +
					"Brug insert_after når du tilføjer nyt indhold (fx en ny tag) frem for at ændre eksisterende.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Filsti relativ til projektmappen",
						},
						"old_string": map[string]any{
							"type":        "string",
							"description": "Tilstand 1: den eksakte tekst der erstattes — skal forekomme præcis én gang i filen",
						},
						"insert_after": map[string]any{
							"type":        "string",
							"description": "Tilstand 2: markør-tekst at indsætte new_string efter — intet slettes",
						},
						"new_string": map[string]any{
							"type":        "string",
							"description": "Teksten der indsættes (erstatter old_string, eller tilføjes efter insert_after)",
						},
					},
					"required": []string{"path", "new_string"},
				},
			},
			provider.ToolDefinition{
				Name:        "create_dir",
				Description: "Opret en mappe (og eventuelle parent-mapper). Sti er relativ til projektmappen.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Mappesti relativ til projektmappen",
						},
					},
					"required": []string{"path"},
				},
			},
		)
	}
	return defs
}

// Execute udfører et tool call og returnerer resultatet som streng.
// root er den absolutte projektmappe — alle stier er relative til den.
func Execute(call provider.ToolCall, root string, canRead, canWrite bool) (string, error) {
	var args map[string]any
	if err := json.Unmarshal(call.Input, &args); err != nil {
		return "", fmt.Errorf("ugyldige argumenter: %w", err)
	}

	switch call.Name {
	case "read_file":
		if !canRead {
			return "", fmt.Errorf("file_read er ikke tilladt i whitelist")
		}
		return readFile(args, root)

	case "search_files":
		if !canRead {
			return "", fmt.Errorf("file_read er ikke tilladt i whitelist")
		}
		return searchFiles(args, root)

	case "write_file":
		if !canWrite {
			return "", fmt.Errorf("file_write er ikke tilladt i whitelist")
		}
		return writeFile(args, root)

	case "edit_file":
		if !canWrite {
			return "", fmt.Errorf("file_write er ikke tilladt i whitelist")
		}
		return editFile(args, root)

	case "create_dir":
		if !canWrite {
			return "", fmt.Errorf("file_write er ikke tilladt i whitelist")
		}
		return createDir(args, root)

	default:
		return "", fmt.Errorf("ukendt tool: %s", call.Name)
	}
}

// maxSearchFileBytes begrænser hvilke filer searchFiles vil indlæse for indholdssøgning.
const maxSearchFileBytes = 64 * 1024

// sensitivePatterns er sti-fragmenter der altid afvises for read_file — selv inden for projektmappen.
// Tjekkes på den opløste, absolutte sti (efter safePath+symlink-resolve) for at undgå omgåelse.
// Dette er en blokliste — et forsvarslag, ikke en garanti.
var sensitivePatterns = []string{
	".ssh", ".aws", ".gnupg", ".netrc", ".git-credentials",
	"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa", "id_xmss",
	"authorized_keys", "known_hosts",
	".npmrc", ".docker", ".pypirc",
	".bash_history", ".zsh_history", ".sh_history", ".bashrc", ".profile",
	"credentials", ".config/gh", ".kube", ".azure", ".gcloud",
	".env", ".pem", ".key", ".p12", ".pfx", ".crt", ".cer",
	"terraform.tfstate",
	"passwd", "shadow",
	"secret", "password", "token",
}

func isSensitivePath(abs string) bool {
	lower := strings.ToLower(abs)
	for _, pat := range sensitivePatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

// resolveSafeFile validerer at en sti er sikker at læse for LLM-formål:
// (1) den ligger under root (safePath), (2) symlinks følges og resultatet
// skal STADIG ligge under root (sandkasse-grænsen kan ellers omgås via et internt symlink),
// (3) den matcher ikke en kendt følsom mønster (blokliste — forsvarslag, ikke garanti).
// Returnerer den endelige sti der skal læses fra (undgår TOCTOU mellem tjek og læsning).
// Brug ALTID denne for ethvert værktøj der lader LLM'en læse filindhold — read_file og
// search_files deler denne kontrol netop for ikke at glemme den ét sted.
func resolveSafeFile(root, path string) (string, error) {
	abs, err := safePath(root, path)
	if err != nil {
		return "", err
	}
	resolved := abs
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		resolved = real
	}
	cleanRoot := filepath.Clean(root)
	if resolved != cleanRoot && !strings.HasPrefix(resolved, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("sti ikke tilladt: %s (symlink peger uden for projektmappen)", path)
	}
	if isSensitivePath(resolved) {
		return "", fmt.Errorf("læsning af %s er ikke tilladt", path)
	}
	return resolved, nil
}

func readFile(args map[string]any, root string) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path mangler")
	}
	resolved, err := resolveSafeFile(root, path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("kan ikke læse %s: %w", path, err)
	}
	// Begræns til 64 KB for at undgå enorme LLM-kontekster.
	// (Tidligere blev output også afkortet til 200 linjer — det skar filer som index.html
	// midt i markup'en, så modellen aldrig kunne se den del den skulle redigere og endte
	// i en løkke af identiske gen-læsninger. Byte-grænsen er den relevante kontekst-grænse.)
	const maxBytes = 64 * 1024
	out := string(data)
	if len(data) > maxBytes {
		out = out[:maxBytes] + "\n\n[... fil afkortet]"
	}
	return out, nil
}

func searchFiles(args map[string]any, root string) (string, error) {
	pattern, _ := args["pattern"].(string)
	contains, _ := args["contains"].(string)
	if pattern == "" {
		return "", fmt.Errorf("pattern mangler")
	}

	var matches []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// Spring .ekte/ og .git/ over
		if d.IsDir() && (d.Name() == ".ekte" || d.Name() == ".git" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		matched, _ := filepath.Match(pattern, d.Name())
		// Prøv også med fuld relativ sti for glob-mønstre med /
		if !matched && strings.Contains(pattern, "/") {
			matched, _ = filepath.Match(pattern, rel)
		}
		// Simpel substring-match på filnavn hvis ingen glob-match
		if !matched && !strings.Contains(pattern, "*") {
			matched = strings.Contains(rel, pattern)
		}
		if !matched {
			return nil
		}
		if contains != "" {
			// Samme sikre-læsning-kontrol som read_file — undgår at search_files
			// bliver en bagdør til at eksfiltrere følsomme filer via 'contains'.
			resolved, err := resolveSafeFile(root, rel)
			if err != nil {
				return nil
			}
			info, err := os.Stat(resolved)
			if err != nil || info.Size() > maxSearchFileBytes {
				return nil
			}
			data, err := os.ReadFile(resolved)
			if err != nil {
				return nil
			}
			text := string(data)
			if !strings.Contains(text, contains) {
				return nil
			}
			// Returner matchende linjer med linjenummer (maks 20 pr. fil)
			const maxLinesPerFile = 20
			var lineMatches []string
			for i, line := range strings.Split(text, "\n") {
				if strings.Contains(line, contains) {
					if len(lineMatches) >= maxLinesPerFile {
						lineMatches = append(lineMatches, "  ... (yderligere forekomster ikke vist)")
						break
					}
					lineMatches = append(lineMatches, fmt.Sprintf("  linje %d: %s", i+1, strings.TrimSpace(line)))
				}
			}
			matches = append(matches, rel+"\n"+strings.Join(lineMatches, "\n"))
			if len(matches) >= 50 {
				return filepath.SkipAll
			}
			return nil
		}
		matches = append(matches, rel)
		if len(matches) >= 50 {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "Ingen filer fundet.", nil
	}
	return strings.Join(matches, "\n"), nil
}

func writeFile(args map[string]any, root string) (string, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return "", fmt.Errorf("path mangler")
	}
	abs, err := safePath(root, path)
	if err != nil {
		return "", err
	}
	// Genkend allerede-udført arbejde eksplicit i tool-resultatet — modellen har
	// kun samtalehistorikken at gå efter for at vide hvad den allerede har lavet
	// (især efter en genoptaget session eller en afbrudt opgave). Uden dette signal
	// skriver den filer igen fra bunden, selvom indholdet allerede er der.
	if existing, readErr := os.ReadFile(abs); readErr == nil {
		if string(existing) == content {
			return fmt.Sprintf("↩ Filen findes allerede med samme indhold — intet ændret: %s", path), nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("kan ikke skrive %s: %w", path, err)
	}
	return fmt.Sprintf("✓ Skrevet: %s (%d bytes)", path, len(content)), nil
}

func editFile(args map[string]any, root string) (string, error) {
	path, _ := args["path"].(string)
	oldStr, _ := args["old_string"].(string)
	insertAfter, _ := args["insert_after"].(string)
	newStr, _ := args["new_string"].(string)
	if path == "" {
		return "", fmt.Errorf("path mangler")
	}
	if oldStr == "" && insertAfter == "" {
		return "", fmt.Errorf("old_string eller insert_after mangler")
	}
	abs, err := safePath(root, path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("kan ikke læse %s: %w", path, err)
	}
	content := string(data)

	var updated string
	if insertAfter != "" {
		count := strings.Count(content, insertAfter)
		if count == 0 {
			return "", fmt.Errorf("insert_after-markør ikke fundet i %s", path)
		}
		if count > 1 {
			return "", fmt.Errorf("insert_after-markør forekommer %d gange i %s — gør den mere specifik", count, path)
		}
		idx := strings.Index(content, insertAfter)
		pos := idx + len(insertAfter)
		updated = content[:pos] + newStr + content[pos:]
	} else {
		count := strings.Count(content, oldStr)
		if count == 0 {
			return "", fmt.Errorf("old_string ikke fundet i %s", path)
		}
		if count > 1 {
			return "", fmt.Errorf("old_string forekommer %d gange i %s — gør den mere specifik", count, path)
		}
		updated = strings.Replace(content, oldStr, newStr, 1)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("kan ikke læse filinfo %s: %w", path, err)
	}
	if err := os.WriteFile(abs, []byte(updated), info.Mode().Perm()); err != nil {
		return "", fmt.Errorf("kan ikke skrive %s: %w", path, err)
	}
	return fmt.Sprintf("✓ Redigeret: %s", path), nil
}

func createDir(args map[string]any, root string) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path mangler")
	}
	abs, err := safePath(root, path)
	if err != nil {
		return "", err
	}
	// Se kommentar i writeFile — giv modellen et eksplicit "allerede gjort"-signal
	// i stedet for et identisk "✓ oprettet", som ser ud som nyt arbejde hver gang.
	if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
		return fmt.Sprintf("↩ Mappe findes allerede: %s", path), nil
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return "", fmt.Errorf("kan ikke oprette mappe %s: %w", path, err)
	}
	return fmt.Sprintf("✓ Mappe oprettet: %s", path), nil
}

// safePath sikrer at stien ikke escaper projektmappen via path traversal.
func safePath(root, rel string) (string, error) {
	abs := filepath.Join(root, filepath.Clean(rel))
	if !strings.HasPrefix(abs, filepath.Clean(root)+string(os.PathSeparator)) &&
		abs != filepath.Clean(root) {
		return "", fmt.Errorf("sti ikke tilladt: %s", rel)
	}
	return abs, nil
}
