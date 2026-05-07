package dep

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode"
)

type Score struct {
	Module    string
	Version   string
	Released  time.Time
	VulnCount int
	Vulns     []string
	Err       string
}

type Module struct {
	Path    string
	Version string
}

type proxyInfo struct {
	Version string    `json:"Version"`
	Time    time.Time `json:"Time"`
}

type osvResponse struct {
	Vulns []struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
	} `json:"vulns"`
}

// Check henter seneste version og tjekker alle kendte CVE'er for modulet.
func Check(ctx context.Context, module string) Score {
	return checkModule(ctx, module, "")
}

// CheckVersion tjekker en specifik version mod OSV — bruges ved go.mod-scanning.
func CheckVersion(ctx context.Context, module, version string) Score {
	return checkModule(ctx, module, version)
}

// CheckAll kører CheckVersion parallelt for alle moduler (max 8 ad gangen).
func CheckAll(ctx context.Context, mods []Module) []Score {
	scores := make([]Score, len(mods))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup

	for i, m := range mods {
		wg.Add(1)
		go func(i int, m Module) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			scores[i] = CheckVersion(ctx, m.Path, m.Version)
		}(i, m)
	}
	wg.Wait()
	return scores
}

// ParseGoMod læser alle require-linjer fra en go.mod fil.
func ParseGoMod(path string) ([]Module, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var mods []Module
	inRequire := false

	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)

		if trimmed == "require (" {
			inRequire = true
			continue
		}
		if inRequire && trimmed == ")" {
			inRequire = false
			continue
		}
		if strings.HasPrefix(trimmed, "require ") && !strings.Contains(trimmed, "(") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 3 {
				mods = append(mods, Module{Path: parts[1], Version: parts[2]})
			}
			continue
		}
		if inRequire && trimmed != "" && !strings.HasPrefix(trimmed, "//") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				mods = append(mods, Module{Path: parts[0], Version: parts[1]})
			}
		}
	}

	return mods, nil
}

// EkteDeps returnerer alle afhængigheder i det kørende ekte-binary.
func EkteDeps() []Module {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	var mods []Module
	for _, d := range info.Deps {
		m := Module{Path: d.Path, Version: d.Version}
		if d.Replace != nil {
			m.Path = d.Replace.Path
			m.Version = d.Replace.Version
		}
		mods = append(mods, m)
	}
	return mods
}

// RenderReport formaterer en sektion af scores kompakt til tool-panelet.
func RenderReport(title string, scores []Score) string {
	var sb strings.Builder
	var clean, vulnTotal, errCount int

	sb.WriteString(title + "\n\n")

	for _, sc := range scores {
		short := shortPath(sc.Module)
		switch {
		case sc.Err != "":
			errCount++
			sb.WriteString(fmt.Sprintf("? %s\n", short))
		case sc.VulnCount > 0:
			vulnTotal++
			sb.WriteString(fmt.Sprintf("⚠ %s %s [%d CVE]\n", short, sc.Version, sc.VulnCount))
			for _, v := range sc.Vulns {
				sb.WriteString(fmt.Sprintf("  · %s\n", v))
			}
		default:
			clean++
			sb.WriteString(fmt.Sprintf("✓ %s %s\n", short, sc.Version))
		}
	}

	sb.WriteString("\n")
	parts := []string{fmt.Sprintf("%d rene", clean)}
	if vulnTotal > 0 {
		parts = append(parts, fmt.Sprintf("%d sårbar ⚠", vulnTotal))
	}
	if errCount > 0 {
		parts = append(parts, fmt.Sprintf("%d fejl", errCount))
	}
	sb.WriteString(strings.Join(parts, " · "))

	return sb.String()
}

func checkModule(ctx context.Context, module, version string) Score {
	sc := Score{Module: module, Version: version}
	client := &http.Client{Timeout: 10 * time.Second}

	// Proxy: hent version og udgivelsesdato
	var proxyURL string
	if version != "" && version != "v0.0.0" {
		proxyURL = "https://proxy.golang.org/" + encodePath(module) + "/@v/" + version + ".info"
	} else {
		proxyURL = "https://proxy.golang.org/" + encodePath(module) + "/@latest"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL, nil)
	if err != nil {
		sc.Err = err.Error()
		return sc
	}
	resp, err := client.Do(req)
	if err != nil {
		sc.Err = "netværksfejl: " + err.Error()
		return sc
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Ikke fatal ved version-opslag — vi kender allerede versionen fra go.mod
		if version == "" {
			sc.Err = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			return sc
		}
	} else {
		var info proxyInfo
		if err := json.Unmarshal(body, &info); err == nil {
			if sc.Version == "" {
				sc.Version = info.Version
			}
			sc.Released = info.Time
		}
	}

	// OSV: kendte CVE'er for denne version
	osvPayload := map[string]any{
		"package": map[string]string{"name": module, "ecosystem": "Go"},
	}
	if version != "" {
		osvPayload["version"] = version
	}
	osvBody, _ := json.Marshal(osvPayload)

	osvReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.osv.dev/v1/query", bytes.NewReader(osvBody))
	if err == nil {
		osvReq.Header.Set("Content-Type", "application/json")
		if osvResp, err := client.Do(osvReq); err == nil {
			var result osvResponse
			if err := json.NewDecoder(osvResp.Body).Decode(&result); err == nil {
				sc.VulnCount = len(result.Vulns)
				for _, v := range result.Vulns {
					line := v.ID
					if v.Summary != "" {
						line += ": " + v.Summary
					}
					if len(line) > 72 {
						line = line[:69] + "..."
					}
					sc.Vulns = append(sc.Vulns, line)
				}
			}
			osvResp.Body.Close()
		}
	}

	return sc
}

func (s Score) Render() string {
	if s.Err != "" {
		return fmt.Sprintf("⛔ %s\n%s", s.Module, s.Err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Afhængighed:  %s\n", s.Module))
	if s.Version != "" {
		age := ""
		if !s.Released.IsZero() {
			age = fmt.Sprintf(" (%s)", s.Released.Format("2 Jan 2006"))
		}
		sb.WriteString(fmt.Sprintf("Version:      %s%s\n", s.Version, age))
	}
	sb.WriteString(fmt.Sprintf("Score:        %s\n", s.stars()))
	sb.WriteString(fmt.Sprintf("Kendte CVE:   %d\n\n", s.VulnCount))
	sb.WriteString(s.verdict())

	if len(s.Vulns) > 0 {
		sb.WriteString("\n\nSårbarheder:\n")
		for _, v := range s.Vulns {
			sb.WriteString("  • " + v + "\n")
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

func (s Score) stars() string {
	r := s.rating()
	return strings.Repeat("★", r) + strings.Repeat("☆", 5-r)
}

func (s Score) verdict() string {
	if s.VulnCount > 0 {
		return fmt.Sprintf("⚠  %d kendte sårbarhed(er) — undersøg CVE'erne inden brug", s.VulnCount)
	}
	switch s.rating() {
	case 5, 4:
		return "✓ Trygt at bruge"
	case 3:
		return "~ Brug med omtanke (relativt ny)"
	default:
		return "✗ Anbefales ikke"
	}
}

func (s Score) rating() int {
	r := 5
	if s.VulnCount > 0 {
		r -= 2
	}
	if s.VulnCount > 3 {
		r--
	}
	if s.Released.IsZero() {
		r--
	} else if time.Since(s.Released) < 30*24*time.Hour {
		r--
	}
	if r < 1 {
		r = 1
	}
	return r
}

// shortPath returnerer de to sidste segmenter af en modulsti, fx "charmbracelet/bubbletea".
func shortPath(module string) string {
	parts := strings.Split(module, "/")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return module
}

// encodePath konverterer store bogstaver til !<lille> til brug i proxy-URL.
func encodePath(s string) string {
	var b strings.Builder
	for _, c := range s {
		if unicode.IsUpper(c) {
			b.WriteByte('!')
			b.WriteRune(unicode.ToLower(c))
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}
