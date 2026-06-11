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
// extraRoots nævnes i sti-beskrivelserne, så modellen ved hvor absolutte
// stier er tilladt — uden den viden kan den ikke følge regler om at
// oprette filer uden for projektmappen (fx en playground-mappe).
func Definitions(canRead, canWrite bool, extraRoots []string) []provider.ToolDefinition {
	extraDesc := ""
	if len(extraRoots) > 0 {
		extraDesc = " — eller en absolut sti under: " + strings.Join(extraRoots, ", ")
	}
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
							"description": "Filsti relativ til projektmappen" + extraDesc,
						},
					},
					"required": []string{"path"},
				},
			},
			provider.ToolDefinition{
				Name: "search_files",
				Description: "Søg rekursivt efter filer der matcher et mønster eller indeholder en streng. " +
					"Brug '*.go' for filtype (søger i hele projektet inkl. .ekte/), " +
					"'**/*.go' virker identisk. Brug delvis sti som 'internal/foo' for sti-søgning. " +
					"Brug list_dir for at se indholdet af én konkret mappe.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"pattern": map[string]any{
							"type":        "string",
							"description": "Glob-mønster ('*.md', '*.go') eller tekststreng ('main', 'internal/agent') at søge efter i filstier",
						},
						"contains": map[string]any{
							"type":        "string",
							"description": "Valgfri: tekst der skal forekomme i filindholdet",
						},
					},
					"required": []string{"pattern"},
				},
			},
			provider.ToolDefinition{
				Name:        "list_dir",
				Description: "List indholdet af en mappe ét niveau dybere — viser filer og undermapper. Brug '.' for projektmappen.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Mappesti relativ til projektmappen ('.' = rod, '.ekte/skills' = skills-mappe)" + extraDesc,
						},
					},
					"required": []string{"path"},
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
							"description": "Filsti relativ til projektmappen" + extraDesc,
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
							"description": "Filsti relativ til projektmappen" + extraDesc,
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
							"description": "Mappesti relativ til projektmappen" + extraDesc,
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
// extraRoots er yderligere absolutte rødder (fra config'ens extra_roots,
// normaliseret via NormalizeExtraRoots) hvor filoperationer også er tilladt.
func Execute(call provider.ToolCall, root string, canRead, canWrite bool, extraRoots []string) (string, error) {
	var args map[string]any
	if err := json.Unmarshal(call.Input, &args); err != nil {
		return "", fmt.Errorf("ugyldige argumenter: %w", err)
	}

	switch call.Name {
	case "read_file":
		if !canRead {
			return "", fmt.Errorf("file_read er ikke tilladt i whitelist")
		}
		return readFile(args, root, extraRoots)

	case "search_files":
		if !canRead {
			return "", fmt.Errorf("file_read er ikke tilladt i whitelist")
		}
		return searchFiles(args, root)

	case "list_dir":
		if !canRead {
			return "", fmt.Errorf("file_read er ikke tilladt i whitelist")
		}
		return listDir(args, root, extraRoots)

	case "write_file":
		if !canWrite {
			return "", fmt.Errorf("file_write er ikke tilladt i whitelist")
		}
		return writeFile(args, root, extraRoots)

	case "edit_file":
		if !canWrite {
			return "", fmt.Errorf("file_write er ikke tilladt i whitelist")
		}
		return editFile(args, root, extraRoots)

	case "create_dir":
		if !canWrite {
			return "", fmt.Errorf("file_write er ikke tilladt i whitelist")
		}
		return createDir(args, root, extraRoots)

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
	".ekte/config", // forhindrer LLM i at læse provider-konfiguration (API-nøgler)
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
// (1) den ligger under root eller en af extraRoots (safePath), (2) symlinks følges
// og resultatet skal STADIG ligge under en tilladt rod (sandkasse-grænsen kan ellers
// omgås via et internt symlink),
// (3) den matcher ikke en kendt følsom mønster (blokliste — forsvarslag, ikke garanti).
// Returnerer den endelige sti der skal læses fra (undgår TOCTOU mellem tjek og læsning).
// Brug ALTID denne for ethvert værktøj der lader LLM'en læse filindhold — read_file og
// search_files deler denne kontrol netop for ikke at glemme den ét sted.
func resolveSafeFile(root, path string, extraRoots []string) (string, error) {
	abs, err := safePath(root, path, extraRoots)
	if err != nil {
		return "", err
	}
	resolved := abs
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		resolved = real
	}
	allowed := underRoot(resolved, root)
	for _, er := range extraRoots {
		if allowed {
			break
		}
		allowed = underRoot(resolved, er)
	}
	if !allowed {
		return "", fmt.Errorf("sti ikke tilladt: %s (symlink peger uden for projektmappen)", path)
	}
	if isSensitivePath(resolved) {
		return "", fmt.Errorf("læsning af %s er ikke tilladt", path)
	}
	return resolved, nil
}

func readFile(args map[string]any, root string, extraRoots []string) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path mangler")
	}
	resolved, err := resolveSafeFile(root, path, extraRoots)
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
		// Spring interne kataloger over. .ekte/sessions/ og .ekte/hooks/ indeholder
		// session-historik og hook-scripts der ikke er relevante for LLM-søgning.
		// .ekte/skills/ og .ekte/memory/ er søgbare (agentoprettede filer).
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules":
				return filepath.SkipDir
			case "sessions", "hooks", "memory":
				// Spring .ekte/sessions/, .ekte/hooks/ og .ekte/memory/ over.
				// Memory injiceres allerede som system-beskeder ved opstart —
				// direkte søgeadgang er overflødig og eksponerer private noter.
				rel2, _ := filepath.Rel(root, path)
				if strings.HasPrefix(rel2, ".ekte"+string(filepath.Separator)) {
					return filepath.SkipDir
				}
			}
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)

		// Normalisér **-mønstre: Go's filepath.Match forstår ikke rekursive **.
		// "**/*.go" → match *.go mod filnavnet; "**" alene → match alt.
		effectivePattern := pattern
		if strings.Contains(pattern, "**") {
			if idx := strings.LastIndex(pattern, "**/"); idx >= 0 {
				effectivePattern = pattern[idx+3:]
			} else {
				effectivePattern = "*"
			}
		}

		matched, _ := filepath.Match(effectivePattern, d.Name())
		// Prøv også med fuld relativ sti for glob-mønstre med /
		if !matched && strings.Contains(effectivePattern, "/") {
			matched, _ = filepath.Match(effectivePattern, rel)
		}
		// Simpel substring-match på fil/sti hvis ingen glob-match
		if !matched && !strings.Contains(effectivePattern, "*") {
			matched = strings.Contains(rel, effectivePattern)
		}
		if !matched {
			return nil
		}
		if contains != "" {
			// Samme sikre-læsning-kontrol som read_file — undgår at search_files
			// bliver en bagdør til at eksfiltrere følsomme filer via 'contains'.
			resolved, err := resolveSafeFile(root, rel, nil)
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

func listDir(args map[string]any, root string, extraRoots []string) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}
	abs, err := safePath(root, path, extraRoots)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", fmt.Errorf("kan ikke læse mappe %s: %w", path, err)
	}
	if len(entries) == 0 {
		return fmt.Sprintf("%s/ (tom)", path), nil
	}
	var lines []string
	for _, e := range entries {
		if e.IsDir() {
			lines = append(lines, e.Name()+"/")
		} else {
			info, err := e.Info()
			if err == nil {
				lines = append(lines, fmt.Sprintf("%s  (%d bytes)", e.Name(), info.Size()))
			} else {
				lines = append(lines, e.Name())
			}
		}
	}
	return fmt.Sprintf("%s/\n%s", path, strings.Join(lines, "\n")), nil
}

func writeFile(args map[string]any, root string, extraRoots []string) (string, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return "", fmt.Errorf("path mangler")
	}
	abs, err := safePath(root, path, extraRoots)
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

func editFile(args map[string]any, root string, extraRoots []string) (string, error) {
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
	abs, err := safePath(root, path, extraRoots)
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

func createDir(args map[string]any, root string, extraRoots []string) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path mangler")
	}
	abs, err := safePath(root, path, extraRoots)
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

// underRoot rapporterer om den rensede, absolutte sti p ligger i eller under roden r.
func underRoot(p, r string) bool {
	r = filepath.Clean(r)
	return p == r || strings.HasPrefix(p, r+string(os.PathSeparator))
}

// safePath sikrer at stien ikke escaper sandkassen via path traversal.
// Relative stier opløses under root. Et førende "~" ekspanderes til brugerens
// hjemmemappe — uden dette blev "~/foo" til en bogstavelig mappe ved navn "~"
// i projektroden. Absolutte stier (inkl. ekspanderede ~-stier) er kun tilladt
// under en af extraRoots; fejlbeskeden nævner de tilladte rødder, så LLM'en
// kan rette sig selv i stedet for at gentage forsøget.
func safePath(root, rel string, extraRoots []string) (string, error) {
	p := rel
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("kan ikke opløse ~ i %s: %w", rel, err)
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if filepath.IsAbs(p) {
		abs := filepath.Clean(p)
		for _, er := range extraRoots {
			if underRoot(abs, er) {
				return abs, nil
			}
		}
		if len(extraRoots) > 0 {
			return "", fmt.Errorf("sti uden for projektmappen: %s — absolutte stier er kun tilladt under: %s", rel, strings.Join(extraRoots, ", "))
		}
		return "", fmt.Errorf("sti uden for projektmappen: %s — brug en sti relativ til projektroden", rel)
	}
	abs := filepath.Join(root, filepath.Clean(p))
	if !underRoot(abs, root) {
		return "", fmt.Errorf("sti ikke tilladt: %s", rel)
	}
	return abs, nil
}

// NormalizeExtraRoots forbereder config-værdien extra_roots til brug i safePath:
// ~ ekspanderes, stier renses, og farlige rødder frasorteres — "/" eller
// hjemmemappen selv ville reelt ophæve sandkassen.
func NormalizeExtraRoots(roots []string) []string {
	home, _ := os.UserHomeDir()
	var out []string
	for _, r := range roots {
		if r == "~" || strings.HasPrefix(r, "~/") {
			if home == "" {
				continue
			}
			r = filepath.Join(home, strings.TrimPrefix(r, "~"))
		}
		r = filepath.Clean(r)
		if !filepath.IsAbs(r) || r == "/" || (home != "" && r == filepath.Clean(home)) {
			continue
		}
		out = append(out, r)
	}
	return out
}
