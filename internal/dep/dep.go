package dep

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

type proxyLatest struct {
	Version string    `json:"Version"`
	Time    time.Time `json:"Time"`
}

type osvResponse struct {
	Vulns []struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
	} `json:"vulns"`
}

// Check henter version fra Go-proxy og kendte sårbarheder fra OSV.dev.
func Check(ctx context.Context, module string) Score {
	sc := Score{Module: module}
	client := &http.Client{Timeout: 10 * time.Second}

	// Go module proxy: seneste version
	proxyURL := "https://proxy.golang.org/" + encodePath(module) + "/@latest"
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
		sc.Err = fmt.Sprintf("modul ikke fundet (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		return sc
	}
	var pl proxyLatest
	if err := json.Unmarshal(body, &pl); err == nil {
		sc.Version = pl.Version
		sc.Released = pl.Time
	}

	// OSV.dev: kendte CVE'er
	osvBody, _ := json.Marshal(map[string]any{
		"package": map[string]string{"name": module, "ecosystem": "Go"},
	})
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
					if len(line) > 80 {
						line = line[:77] + "..."
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
		r-- // meget ny — ikke battle-tested
	}
	if r < 1 {
		r = 1
	}
	return r
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
