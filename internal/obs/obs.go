package obs

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TurnStat indeholder observability-data for ét turn med LLM.
type TurnStat struct {
	TurnNum      int       `json:"turn"`
	Timestamp    time.Time `json:"ts"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	UserChars    int       `json:"user_chars"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	CacheRead    int       `json:"cache_read"`
	CacheWrite   int       `json:"cache_write"`
	MsgCount     int       `json:"msg_count"`
	SysTokens    int       `json:"sys_tokens"`
	WikiTokens   int       `json:"wiki_tokens"`
	HistTokens   int       `json:"hist_tokens"`
	UserTokens   int       `json:"user_tokens"`
	ToolTokens   int       `json:"tool_tokens"`
	IsRepeat     bool      `json:"is_repeat"`
}

// SessionSummary aggregerer data på tværs af turns i én session.
type SessionSummary struct {
	SessionID       string         `json:"session_id"`
	Date            time.Time      `json:"date"`
	TurnCount       int            `json:"turns"`
	ByProvider      map[string]int `json:"by_provider"`
	TotalInput      int            `json:"total_input"`
	TotalOutput     int            `json:"total_output"`
	TotalCache      int            `json:"total_cache"`
	AvgInputPerTurn int            `json:"avg_input"`
	PeakInput       int            `json:"peak_input"`
	RepeatRate      float64        `json:"repeat_rate"`
}

// Recorder skriver TurnStats til en JSONL-fil og holder dem i hukommelsen.
type Recorder struct {
	path       string
	sessionDir string
	turns      []TurnStat
}

func NewRecorder(path, sessionDir string) *Recorder {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	return &Recorder{path: path, sessionDir: sessionDir}
}

func (r *Recorder) Record(t TurnStat) {
	t.TurnNum = len(r.turns) + 1
	if t.Timestamp.IsZero() {
		t.Timestamp = time.Now()
	}
	r.turns = append(r.turns, t)
	_ = r.appendLine(t)
}

func (r *Recorder) Turns() []TurnStat { return r.turns }

func (r *Recorder) SessionDir() string { return r.sessionDir }

func (r *Recorder) appendLine(t TurnStat) error {
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

// Load læser en JSONL-fil og returnerer alle turns.
func Load(path string) ([]TurnStat, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var turns []TurnStat
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var t TurnStat
		if err := json.Unmarshal(scanner.Bytes(), &t); err == nil {
			turns = append(turns, t)
		}
	}
	return turns, nil
}

// LoadAll finder alle *_obs.jsonl-filer i sessionDir og returnerer per-session summaries.
func LoadAll(sessionDir string) ([]SessionSummary, error) {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return nil, err
	}
	var summaries []SessionSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_obs.jsonl") {
			continue
		}
		turns, err := Load(filepath.Join(sessionDir, e.Name()))
		if err != nil || len(turns) == 0 {
			continue
		}
		summaries = append(summaries, summarize(e.Name(), turns))
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Date.Before(summaries[j].Date)
	})
	return summaries, nil
}

func summarize(filename string, turns []TurnStat) SessionSummary {
	id := strings.TrimSuffix(filename, "_obs.jsonl")
	s := SessionSummary{
		SessionID:  id,
		Date:       turns[0].Timestamp,
		TurnCount:  len(turns),
		ByProvider: make(map[string]int),
	}
	var repeats int
	for _, t := range turns {
		s.TotalInput += t.InputTokens
		s.TotalOutput += t.OutputTokens
		s.TotalCache += t.CacheRead
		key := t.Provider + "/" + t.Model
		s.ByProvider[key] += t.InputTokens
		if t.InputTokens > s.PeakInput {
			s.PeakInput = t.InputTokens
		}
		if t.IsRepeat {
			repeats++
		}
	}
	if len(turns) > 0 {
		s.AvgInputPerTurn = s.TotalInput / len(turns)
		s.RepeatRate = float64(repeats) / float64(len(turns))
	}
	return s
}

// --- TUI-formatering ---

const barWidth = 16

func bar(val, max int) string {
	if max == 0 {
		return strings.Repeat("░", barWidth)
	}
	filled := val * barWidth / max
	if filled > barWidth {
		filled = barWidth
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
}

func miniBar(val, max, width int) string {
	if max == 0 || width == 0 {
		return strings.Repeat("□", width)
	}
	filled := val * width / max
	if filled > width {
		filled = width
	}
	return strings.Repeat("■", filled) + strings.Repeat("□", width-filled)
}

func fmtK(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%s %03d", fmtK(n/1000), n%1000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%d %03d", n/1000, n%1000)
	}
	return fmt.Sprintf("%d", n)
}

// FormatTUI formaterer den aktuelle sessions data til tool-panel.
func FormatTUI(turns []TurnStat) string {
	if len(turns) == 0 {
		return "Ingen observability-data endnu. Kør et par prompts og prøv igen."
	}

	var sb strings.Builder

	// --- Token-forbrug per turn ---
	sb.WriteString("━━━ Token-forbrug ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	maxIn := 1
	for _, t := range turns {
		if t.InputTokens > maxIn {
			maxIn = t.InputTokens
		}
	}
	var totalIn, totalOut, totalCache int
	for _, t := range turns {
		cache := ""
		if t.CacheRead > 0 {
			cache = fmt.Sprintf("  ⚡ cache %s", fmtK(t.CacheRead))
		}
		repeat := ""
		if t.IsRepeat {
			repeat = " ↩"
		}
		sb.WriteString(fmt.Sprintf(" #%-2d %s  %s ind / %s ud%s%s\n",
			t.TurnNum, bar(t.InputTokens, maxIn),
			fmtK(t.InputTokens), fmtK(t.OutputTokens),
			cache, repeat,
		))
		totalIn += t.InputTokens
		totalOut += t.OutputTokens
		totalCache += t.CacheRead
	}
	sb.WriteString(fmt.Sprintf("      %s\n", strings.Repeat("─", barWidth+36)))
	sb.WriteString(fmt.Sprintf(" I alt  %s ind / %s ud", fmtK(totalIn), fmtK(totalOut)))
	if totalCache > 0 {
		sb.WriteString(fmt.Sprintf("  ⚡ cache i alt %s", fmtK(totalCache)))
	}
	sb.WriteString("\n\n")

	// --- Kontekst-breakdown for seneste turn ---
	last := turns[len(turns)-1]
	total := last.SysTokens + last.WikiTokens + last.HistTokens + last.UserTokens + last.ToolTokens
	if total > 0 {
		sb.WriteString("━━━ Kontekst-sammensætning (seneste turn) ━━━━━━\n")
		type row struct {
			label string
			val   int
		}
		rows := []row{
			{"System  ", last.SysTokens},
			{"Wiki    ", last.WikiTokens},
			{"Historik", last.HistTokens},
			{"Prompt  ", last.UserTokens},
			{"Tools   ", last.ToolTokens},
		}
		const bw = 10
		for _, r := range rows {
			if r.val == 0 {
				continue
			}
			pct := r.val * 100 / total
			sb.WriteString(fmt.Sprintf(" %s %s  %6s tk  (%2d%%)\n",
				r.label, miniBar(r.val, total, bw), fmtK(r.val), pct,
			))
		}
		sb.WriteString("\n")
	}

	// --- Optimeringsråd ---
	sb.WriteString("━━━ Optimering ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	hints := 0
	if len(turns) > 1 {
		growth := turns[len(turns)-1].InputTokens - turns[0].InputTokens
		perTurn := growth / len(turns)
		if perTurn > 500 {
			sb.WriteString(fmt.Sprintf(" 📈 Historik vokser ~%s tk/turn\n", fmtK(perTurn)))
			hints++
		}
	}
	if total > 0 && last.WikiTokens*100/total > 20 {
		sb.WriteString(" 💡 Wiki-kontekst er >20% — overvej /compress\n")
		hints++
	}
	var repeats int
	for _, t := range turns {
		if t.IsRepeat {
			repeats++
		}
	}
	if repeats > 0 {
		sb.WriteString(fmt.Sprintf(" ↩  %d turn(s) gentog en lignende prompt\n", repeats))
		hints++
	}
	if hints == 0 {
		sb.WriteString(" ✓ Ingen åbenlyse optimeringsmuligheder\n")
	}

	return sb.String()
}

type monthKey struct {
	month    int
	provider string
}

// FormatAllTUI formaterer tværgående data fra alle sessioner (månedsopdelt, indeværende år).
func FormatAllTUI(summaries []SessionSummary) string {
	if len(summaries) == 0 {
		return "Ingen tidligere sessioner med observability-data fundet."
	}

	now := time.Now()
	year := now.Year()

	// Månedsopdelt forbrug per provider
	byMonth := make(map[monthKey]int)
	providerTotals := make(map[string]int)
	grandTotal := 0
	monthMax := 0

	for _, s := range summaries {
		if s.Date.Year() != year {
			continue
		}
		m := int(s.Date.Month())
		for prov, tokens := range s.ByProvider {
			byMonth[monthKey{m, prov}] += tokens
			providerTotals[prov] += tokens
			grandTotal += tokens
		}
	}

	// Månedssummer for bar-størrelse
	monthTotals := make(map[int]int)
	for k, v := range byMonth {
		monthTotals[k.month] += v
		if monthTotals[k.month] > monthMax {
			monthMax = monthTotals[k.month]
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("━━━ %d — Token-forbrug per måned ━━━━━━━━━━━━━━━━━\n", year))

	monthNames := []string{"", "Jan", "Feb", "Mar", "Apr", "Maj", "Jun", "Jul", "Aug", "Sep", "Okt", "Nov", "Dec"}
	currentMonth := int(now.Month())

	for m := 1; m <= currentMonth; m++ {
		total := monthTotals[m]
		if total == 0 {
			continue
		}
		providers := providerKeys(byMonth, m)
		if len(providers) == 1 {
			prov := providers[0]
			sb.WriteString(fmt.Sprintf(" %s  %s  %s tk  %s\n",
				monthNames[m], bar(total, monthMax), fmtK(total), shortProvider(prov),
			))
		} else {
			sb.WriteString(fmt.Sprintf(" %s  %s  %s tk\n",
				monthNames[m], bar(total, monthMax), fmtK(total),
			))
			for _, prov := range providers {
				v := byMonth[monthKey{m, prov}]
				sb.WriteString(fmt.Sprintf("       %s  %s (%s)\n",
					bar(v, monthMax), shortProvider(prov), fmtK(v),
				))
			}
		}
	}

	// Per-provider totaler
	sb.WriteString("\n━━━ Per leverandør (år til dato) ━━━━━━━━━━━━━━━━\n")
	type provRow struct {
		prov   string
		tokens int
	}
	var provRows []provRow
	for p, t := range providerTotals {
		provRows = append(provRows, provRow{p, t})
	}
	sort.Slice(provRows, func(i, j int) bool { return provRows[i].tokens > provRows[j].tokens })
	for _, r := range provRows {
		pct := 0
		if grandTotal > 0 {
			pct = r.tokens * 100 / grandTotal
		}
		sb.WriteString(fmt.Sprintf(" %-40s  %s tk  (%d%%)\n", r.prov, fmtK(r.tokens), pct))
	}

	// Leverandør-intelligens
	sb.WriteString("\n━━━ Leverandør-intelligens ━━━━━━━━━━━━━━━━━━━━━━\n")
	type provStats struct {
		totalIn, totalOut int
		turns             int
		repeats           int
		sessions          int
	}
	ps := make(map[string]*provStats)
	for _, s := range summaries {
		for prov := range s.ByProvider {
			if ps[prov] == nil {
				ps[prov] = &provStats{}
			}
			ps[prov].sessions++
		}
		// per-turn data kræver fuld load — brug session-summaries som proxy
		for prov, tokens := range s.ByProvider {
			ps[prov].totalIn += tokens
			ps[prov].turns += s.TurnCount
		}
	}
	for prov, stat := range ps {
		effPct := 0
		if stat.totalIn > 0 {
			effPct = stat.totalOut * 100 / stat.totalIn
		}
		avgIn := 0
		if stat.turns > 0 {
			avgIn = stat.totalIn / stat.turns
		}
		sb.WriteString(fmt.Sprintf(" %-25s  Effekt. %2d%%  ø %s tk/turn  %d sess.\n",
			shortProvider(prov), effPct, fmtK(avgIn), stat.sessions,
		))
	}

	sb.WriteString("\n /observ html for SVG-diagrammer og fuld analyse\n")
	return sb.String()
}

func providerKeys(byMonth map[monthKey]int, m int) []string {
	seen := make(map[string]bool)
	for k := range byMonth {
		if k.month == m {
			seen[k.provider] = true
		}
	}
	var out []string
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func shortProvider(prov string) string {
	parts := strings.SplitN(prov, "/", 2)
	if len(parts) == 2 {
		return parts[0] + "/" + truncate(parts[1], 20)
	}
	return prov
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// WriteHTML genererer en selvstændig HTML-rapport over alle sessioner.
func WriteHTML(summaries []SessionSummary, dest string) error {
	_ = os.MkdirAll(filepath.Dir(dest), 0755)

	now := time.Now()
	year := now.Year()

	// Byg månedsopdelt data til SVG
	type monthData struct {
		name       string
		byProvider map[string]int
		total      int
	}
	monthNames := []string{"", "Jan", "Feb", "Mar", "Apr", "Maj", "Jun", "Jul", "Aug", "Sep", "Okt", "Nov", "Dec"}
	months := make([]monthData, 13)
	for i := range months {
		months[i] = monthData{name: monthNames[i], byProvider: make(map[string]int)}
	}
	providerSet := make(map[string]bool)
	for _, s := range summaries {
		if s.Date.Year() != year {
			continue
		}
		m := int(s.Date.Month())
		for prov, tokens := range s.ByProvider {
			months[m].byProvider[prov] += tokens
			months[m].total += tokens
			providerSet[prov] = true
		}
	}

	maxMonth := 1
	for m := 1; m <= 12; m++ {
		if months[m].total > maxMonth {
			maxMonth = months[m].total
		}
	}

	var providers []string
	for p := range providerSet {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	colors := []string{"#4f8ef7", "#f7a24f", "#4ff7a2", "#f74f8e", "#a24ff7", "#f7f74f"}
	provColor := make(map[string]string)
	for i, p := range providers {
		provColor[p] = colors[i%len(colors)]
	}

	// SVG månedssøjlediagram
	svgW := 700
	svgH := 220
	barW := svgW / 13
	maxH := svgH - 40

	var svgBars strings.Builder
	for m := 1; m <= 12; m++ {
		x := (m-1)*barW + 10
		if months[m].total == 0 {
			continue
		}
		yOffset := svgH - 20
		for _, prov := range providers {
			v := months[m].byProvider[prov]
			if v == 0 {
				continue
			}
			h := v * maxH / maxMonth
			if h < 1 {
				h = 1
			}
			yOffset -= h
			svgBars.WriteString(fmt.Sprintf(
				`<rect x="%d" y="%d" width="%d" height="%d" fill="%s" title="%s: %d tk"/>`,
				x, yOffset, barW-4, h, provColor[prov], prov, v,
			))
		}
		svgBars.WriteString(fmt.Sprintf(
			`<text x="%d" y="%d" text-anchor="middle" font-size="10" fill="#888">%s</text>`,
			x+barW/2, svgH-5, monthNames[m],
		))
	}

	// Farveforklaring
	var legend strings.Builder
	for _, p := range providers {
		legend.WriteString(fmt.Sprintf(
			`<span style="display:inline-block;width:12px;height:12px;background:%s;margin-right:4px;vertical-align:middle"></span>%s &nbsp; `,
			provColor[p], p,
		))
	}

	// Per-session tabel
	var tableRows strings.Builder
	for _, s := range summaries {
		tableRows.WriteString(fmt.Sprintf(
			`<tr><td>%s</td><td>%s</td><td>%d</td><td>%s</td><td>%s</td><td>%.0f%%</td></tr>`,
			s.Date.Format("2006-01-02"), s.SessionID[:min(8, len(s.SessionID))],
			s.TurnCount, fmtK(s.TotalInput), fmtK(s.TotalOutput),
			s.RepeatRate*100,
		))
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="da">
<head><meta charset="UTF-8"><title>ekte — Observability Rapport</title>
<style>
body{font-family:system-ui,sans-serif;background:#0d1117;color:#e6edf3;margin:0;padding:24px}
h1{font-size:1.4rem;color:#79c0ff;margin-bottom:4px}
h2{font-size:1rem;color:#8b949e;margin-top:32px;margin-bottom:8px;border-bottom:1px solid #30363d;padding-bottom:4px}
svg{background:#161b22;border-radius:6px;display:block;margin-bottom:8px}
table{border-collapse:collapse;width:100%%;font-size:.85rem}
th{text-align:left;color:#8b949e;border-bottom:1px solid #30363d;padding:6px 12px}
td{padding:6px 12px;border-bottom:1px solid #21262d}
tr:hover td{background:#161b22}
.legend{font-size:.8rem;color:#8b949e;margin-bottom:16px}
</style>
</head>
<body>
<h1>ekte — Observability Rapport</h1>
<p style="color:#8b949e;font-size:.85rem">Genereret %s · %d sessioner analyseret</p>

<h2>Token-forbrug per måned (%d)</h2>
<svg width="%d" height="%d">%s</svg>
<div class="legend">%s</div>

<h2>Alle sessioner</h2>
<table>
<tr><th>Dato</th><th>Session</th><th>Turns</th><th>Input</th><th>Output</th><th>Gensvar%%</th></tr>
%s
</table>
</body></html>`,
		now.Format("2006-01-02 15:04"),
		len(summaries),
		year,
		svgW, svgH,
		svgBars.String(),
		legend.String(),
		tableRows.String(),
	)

	return os.WriteFile(dest, []byte(html), 0644)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
