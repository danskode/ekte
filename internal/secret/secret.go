// Package secret redakterer almindelige hemmeligheds-mønstre i tekst, før den
// sendes til en LLM-provider. Det bruges af ekte review/orchestrator, så en diff
// med fx en API-nøgle ikke lækker til en ekstern provider.
//
// VIGTIGT: dette er en best-effort heuristik (defense-in-depth) — IKKE en garanti.
// Regex fanger langt fra alle hemmeligheder (generiske high-entropy-strenge,
// ukendte udbyder-formater m.m. slipper igennem). Suppler altid med et dedikeret
// secret-scan (fx gitleaks/trufflehog). Det rapporterede antal er antal
// mønster-træf, ikke nødvendigvis unikke hemmeligheder.
package secret

import "regexp"

// Mask erstatter den fundne hemmelighed.
const Mask = "[REDAKTERET]"

type pattern struct {
	re   *regexp.Regexp
	repl func(sub []string) string // nil = maskér hele matchet
}

var patterns = []pattern{
	{re: regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},                                   // AWS access key id
	{re: regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`)},                         // GitHub tokens
	{re: regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},                       // Slack tokens
	{re: regexp.MustCompile(`AIza[0-9A-Za-z_\-]{35}`)},                             // Google API key
	{re: regexp.MustCompile(`(?:sk|rk|pk)_(?:live|test)_[0-9a-zA-Z]{16,}`)},        // Stripe
	{re: regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]{20,}`)},                  // Bearer-tokens
	{re: regexp.MustCompile(`-----BEGIN[A-Z ]*PRIVATE KEY-----`)},                  // privat nøgle-header
	{re: regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`)}, // JWT
	// connection string med credentials: scheme://bruger:HEMMELIGHED@host
	{
		re:   regexp.MustCompile(`([a-zA-Z][\w+.\-]*://[^:@/\s]+:)([^@/\s]{3,})(@)`),
		repl: func(s []string) string { return s[1] + Mask + s[3] },
	},
	// nøgle = værdi: bevar nøgle+separator, maskér værdien.
	{
		re:   regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|passwd|pwd|access[_-]?key)(["']?\s*[:=]\s*["']?)([A-Za-z0-9_\-./+]{12,})`),
		repl: func(s []string) string { return s[1] + s[2] + Mask },
	},
}

// Redact maskerer almindelige secret-mønstre i text og returnerer den maskerede
// tekst plus antal mønster-træf (ikke nødvendigvis unikke hemmeligheder).
func Redact(text string) (string, int) {
	count := 0
	for _, p := range patterns {
		text = p.re.ReplaceAllStringFunc(text, func(m string) string {
			count++
			if p.repl == nil {
				return Mask
			}
			return p.repl(p.re.FindStringSubmatch(m))
		})
	}
	return text, count
}
