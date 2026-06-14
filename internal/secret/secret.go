// Package secret redakterer almindelige hemmeligheds-mønstre i tekst, før den
// sendes til en LLM-provider. Det bruges af ekte review/orchestrator, så en diff
// med fx en API-nøgle ikke lækker til en ekstern provider. Heuristisk og
// best-effort — ikke en garanti; suppler med et dedikeret secret-scan.
package secret

import "regexp"

// Mask erstatter den fundne hemmelighed.
const Mask = "[REDAKTERET]"

type pattern struct {
	re    *regexp.Regexp
	group int // capture-gruppe der er selve værdien (0 = hele matchet)
}

var patterns = []pattern{
	{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), 0},                            // AWS access key id
	{regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`), 0},                  // GitHub tokens
	{regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`), 0},                // Slack tokens
	{regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]{20,}`), 0},           // Bearer-tokens
	{regexp.MustCompile(`-----BEGIN[A-Z ]*PRIVATE KEY-----`), 0},           // privat nøgle-header
	{regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`), 0}, // JWT
	// nøgle = værdi: bevar nøgle+separator, maskér værdien.
	{regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|passwd|pwd|access[_-]?key)(["']?\s*[:=]\s*["']?)([A-Za-z0-9_\-./+]{12,})`), 3},
}

// Redact maskerer almindelige secret-mønstre i text og returnerer den maskerede
// tekst plus antal redaktioner.
func Redact(text string) (string, int) {
	count := 0
	for _, p := range patterns {
		text = p.re.ReplaceAllStringFunc(text, func(m string) string {
			count++
			if p.group == 0 {
				return Mask
			}
			sub := p.re.FindStringSubmatch(m)
			if len(sub) > p.group {
				return sub[1] + sub[2] + Mask
			}
			return Mask
		})
	}
	return text, count
}
