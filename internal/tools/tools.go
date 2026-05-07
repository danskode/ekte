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
// canWrite styrer om write_file er inkluderet.
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
		defs = append(defs, provider.ToolDefinition{
			Name:        "write_file",
			Description: "Skriv eller overskriv en fil. Sti er relativ til projektmappen.",
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
		})
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

	default:
		return "", fmt.Errorf("ukendt tool: %s", call.Name)
	}
}

func readFile(args map[string]any, root string) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path mangler")
	}
	abs, err := safePath(root, path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("kan ikke læse %s: %w", path, err)
	}
	// Begræns output til 200 linjer for ikke at sprænge kontekstvinduet
	lines := strings.Split(string(data), "\n")
	const maxLines = 200
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	out := strings.Join(lines, "\n")
	if truncated {
		out += fmt.Sprintf("\n\n[... fil afkortet ved %d linjer]", maxLines)
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
			data, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(data), contains) {
				return nil
			}
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
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("kan ikke skrive %s: %w", path, err)
	}
	return fmt.Sprintf("✓ Skrevet: %s (%d bytes)", path, len(content)), nil
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
