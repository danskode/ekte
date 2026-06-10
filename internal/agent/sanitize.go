package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/danskode/ekte/internal/provider"
	"gopkg.in/yaml.v3"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string { return ansiEscape.ReplaceAllString(s, "") }

// logControlChars matcher kontroltegn (bl.a. \n, \r) i modelstyrede strenge.
// Stier fra LLM-genererede tool calls er ikke-betroet input — uden denne rensning
// kan de forfalske logposter ved at injicere nye linjer (CWE-117 / log injection).
var logControlChars = regexp.MustCompile(`[\x00-\x1f\x7f]`)

func sanitizeLogPath(s string) string { return logControlChars.ReplaceAllString(s, "") }

// logSafePath udtrækker kun sti-feltet fra tool-args til logning (undgår at logge indhold).
// toolCallPath udtrækker "path"-argumentet fra et tool call, eller "" hvis det
// mangler eller argumenterne er ugyldige. Bruges til at spore redigerings-streaks
// (se editStreak i streamChat) — adskilt fra logSafePath, der returnerer
// menneskelæsbare placeholder-strenge til logning.
func toolCallPath(input json.RawMessage) string {
	var args map[string]any
	if json.Unmarshal(input, &args) != nil {
		return ""
	}
	path, _ := args["path"].(string)
	return sanitizeLogPath(path)
}

func logSafePath(input json.RawMessage) string {
	var args map[string]any
	if json.Unmarshal(input, &args) != nil {
		return "[ugyldige args]"
	}
	if path, ok := args["path"].(string); ok {
		return sanitizeLogPath(path)
	}
	return "[ingen sti]"
}

// injectionPattern matcher kendte formuleringer for prompt injection — hvor som helst i linjen,
// ikke kun som præfiks, da angreb ofte gemmes midt i tekst ("Normal tekst. Ignore previous...").
// Dette er et ekstra forsvarslag (defense-in-depth) — IKKE en garanti. Den primære spærring er
// at filindhold altid sendes som en separat tool-rolle adskilt fra brugerens instruktioner,
// og at write/edit/create_dir altid kræver eksplicit brugerbekræftelse uanset hvad LLM'en foreslår.
var injectionPattern = regexp.MustCompile(`(?i)(` +
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

// sanitizeFileContent fjerner linjer der ligner prompt injection-forsøg.
// Svagt ekstra lag — den reelle beskyttelse er bekræftelsesdialogen på skriveoperationer.
// Linje-for-linje matching alene fangede ikke sætninger splittet over to linjer;
// vi matcher nu også på den sammenflettede enkeltlinje-version (CWE-74).
func sanitizeFileContent(content string) string {
	// Tjek om hele indholdet indeholder injection når linjeskift erstattes med mellemrum
	flattened := strings.ReplaceAll(content, "\n", " ")
	if injectionPattern.MatchString(flattened) {
		return "[indhold fjernet: mulig prompt injection]"
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if injectionPattern.MatchString(line) {
			lines[i] = "[linje fjernet: mulig prompt injection]"
		}
	}
	return strings.Join(lines, "\n")
}

// appendHookWarning tilføjer en advarsel til bekræftelsesbeskeden hvis
// ændringen berører hooks-sektionen i config.yaml. Bruger yaml.Unmarshal til
// at parse indholdet — block scalars ('cmd: |') ekspanderes automatisk, så
// skjulte kommandoer altid er synlige i advarslen (CWE-116).
func appendHookWarning(desc string, tc provider.ToolCall) string {
	var args map[string]any
	if json.Unmarshal(tc.Input, &args) != nil {
		return desc
	}
	content, _ := args["content"].(string)
	if content == "" {
		content, _ = args["new_string"].(string)
	}
	if content == "" || !strings.Contains(content, "hooks:") {
		return desc
	}

	// Parse YAML så block scalars ekspanderes korrekt (CWE-116).
	// yaml.Unmarshal løser 'cmd: |' og indenterede linjer til én streng —
	// skjulte kommandoer er dermed altid synlige i den viste advarsel.
	var cfg struct {
		Hooks map[string]provider.HookConfig `yaml:"hooks"`
	}
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil || len(cfg.Hooks) == 0 {
		// Kan ikke parses som fuld config — vis rå indhold som fallback.
		return desc + "\n⚠ HOOKS ÆNDRES (rå YAML — kan ikke parses struktureret):\n" + content
	}

	var sb strings.Builder
	sb.WriteString("\n⚠ HOOKS DER AKTIVERES (shell-kommandoer):\n")
	for name, hc := range cfg.Hooks {
		cmd := strings.TrimSpace(hc.Cmd)
		sb.WriteString(fmt.Sprintf("  • %s: %s\n", name, cmd))
	}
	return desc + sb.String()
}

func toolConfirmDesc(tc provider.ToolCall) string {
	var args map[string]any
	if json.Unmarshal(tc.Input, &args) != nil {
		return tc.Name
	}
	path, _ := args["path"].(string)
	path = stripANSI(path)
	if path == "" {
		return tc.Name
	}
	switch tc.Name {
	case "edit_file":
		if insertAfter, ok := args["insert_after"].(string); ok && insertAfter != "" {
			if len(insertAfter) > 40 {
				insertAfter = insertAfter[:40] + "…"
			}
			return fmt.Sprintf("edit_file → %s  (indsæt efter: %q)", path, insertAfter)
		}
		old, _ := args["old_string"].(string)
		if len(old) > 40 {
			old = old[:40] + "…"
		}
		return fmt.Sprintf("edit_file → %s  (erstatter: %q)", path, old)
	default:
		return tc.Name + " → " + path
	}
}
