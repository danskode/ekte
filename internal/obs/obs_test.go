package obs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Recorder og Load ---

func TestRecordAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_obs.jsonl")
	r := NewRecorder(path, dir)

	r.Record(TurnStat{Provider: "anthropic", Model: "claude", InputTokens: 100, OutputTokens: 50})
	r.Record(TurnStat{Provider: "anthropic", Model: "claude", InputTokens: 200, OutputTokens: 80})

	turns, err := Load(path)
	if err != nil {
		t.Fatalf("Load fejlede: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("forventede 2 turns, fik %d", len(turns))
	}
	if turns[0].TurnNum != 1 || turns[1].TurnNum != 2 {
		t.Errorf("forkerte turn-numre: %d, %d", turns[0].TurnNum, turns[1].TurnNum)
	}
	if turns[0].InputTokens != 100 || turns[1].InputTokens != 200 {
		t.Errorf("forkerte token-værdier")
	}
}

func TestRecordTurnsInMemory(t *testing.T) {
	dir := t.TempDir()
	r := NewRecorder(filepath.Join(dir, "x.jsonl"), dir)
	r.Record(TurnStat{InputTokens: 42})
	r.Record(TurnStat{InputTokens: 99})
	turns := r.Turns()
	if len(turns) != 2 {
		t.Fatalf("forventede 2 turns i memory, fik %d", len(turns))
	}
	if turns[1].InputTokens != 99 {
		t.Errorf("forkert InputTokens: %d", turns[1].InputTokens)
	}
}

func TestLoadEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	_ = os.WriteFile(path, []byte{}, 0644)
	turns, err := Load(path)
	if err != nil {
		t.Fatalf("Load på tom fil fejlede: %v", err)
	}
	if len(turns) != 0 {
		t.Errorf("forventede 0 turns fra tom fil, fik %d", len(turns))
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	_, err := Load("/tmp/does-not-exist-abc123.jsonl")
	if err == nil {
		t.Error("forventede fejl for ikke-eksisterende fil")
	}
}

func TestLoadSkipsInvalidLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.jsonl")
	content := `{"turn":1,"input_tokens":100}
ikke-json-linje
{"turn":2,"input_tokens":200}
`
	_ = os.WriteFile(path, []byte(content), 0644)
	turns, err := Load(path)
	if err != nil {
		t.Fatalf("Load fejlede: %v", err)
	}
	if len(turns) != 2 {
		t.Errorf("forventede 2 gyldige turns, fik %d", len(turns))
	}
}

// --- LoadAll ---

func TestLoadAllEmptyDir(t *testing.T) {
	dir := t.TempDir()
	summaries, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll på tom mappe fejlede: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("forventede 0 summaries, fik %d", len(summaries))
	}
}

func TestLoadAllIgnoresNonJSONL(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "session.json"), []byte(`{}`), 0644)
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hej"), 0644)
	summaries, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll fejlede: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("forventede 0 summaries (ingen _obs.jsonl filer), fik %d", len(summaries))
	}
}

func TestLoadAllMultipleSessions(t *testing.T) {
	dir := t.TempDir()

	writeSession := func(name string, turns []TurnStat) {
		r := NewRecorder(filepath.Join(dir, name+"_obs.jsonl"), dir)
		for _, turn := range turns {
			r.Record(turn)
		}
	}

	writeSession("20260501-100000", []TurnStat{
		{Provider: "anthropic", Model: "claude", InputTokens: 1000, OutputTokens: 200, Timestamp: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)},
	})
	writeSession("20260601-100000", []TurnStat{
		{Provider: "lmstudio", Model: "qwen", InputTokens: 500, OutputTokens: 100, Timestamp: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)},
		{Provider: "lmstudio", Model: "qwen", InputTokens: 700, OutputTokens: 150, Timestamp: time.Date(2026, 6, 1, 10, 5, 0, 0, time.UTC)},
	})

	summaries, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll fejlede: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("forventede 2 sessioner, fik %d", len(summaries))
	}
	// Tjek sortering (ældste først)
	if summaries[0].TotalInput != 1000 {
		t.Errorf("første session burde have 1000 input tokens")
	}
	if summaries[1].TurnCount != 2 {
		t.Errorf("anden session burde have 2 turns")
	}
}

// --- summarize ---

func TestSummarizeProviderBreakdown(t *testing.T) {
	turns := []TurnStat{
		{Provider: "anthropic", Model: "claude", InputTokens: 500, OutputTokens: 100, CacheRead: 50},
		{Provider: "lmstudio", Model: "qwen", InputTokens: 300, OutputTokens: 80},
		{Provider: "anthropic", Model: "claude", InputTokens: 200, OutputTokens: 60, IsRepeat: true},
	}
	s := summarize("test_obs.jsonl", turns)

	if s.TotalInput != 1000 {
		t.Errorf("TotalInput: forventede 1000, fik %d", s.TotalInput)
	}
	if s.TotalOutput != 240 {
		t.Errorf("TotalOutput: forventede 240, fik %d", s.TotalOutput)
	}
	if s.TotalCache != 50 {
		t.Errorf("TotalCache: forventede 50, fik %d", s.TotalCache)
	}
	if s.TurnCount != 3 {
		t.Errorf("TurnCount: forventede 3, fik %d", s.TurnCount)
	}
	if s.PeakInput != 500 {
		t.Errorf("PeakInput: forventede 500, fik %d", s.PeakInput)
	}
	anthropicKey := "anthropic/claude"
	if s.ByProvider[anthropicKey] != 700 {
		t.Errorf("ByProvider[anthropic/claude]: forventede 700, fik %d", s.ByProvider[anthropicKey])
	}
	// RepeatRate: 1 ud af 3
	if s.RepeatRate < 0.33 || s.RepeatRate > 0.34 {
		t.Errorf("RepeatRate: forventede ~0.333, fik %f", s.RepeatRate)
	}
}

func TestSummarizeSingleTurn(t *testing.T) {
	turns := []TurnStat{
		{Provider: "anthropic", Model: "x", InputTokens: 42, OutputTokens: 10, Timestamp: time.Now()},
	}
	s := summarize("x_obs.jsonl", turns)
	if s.AvgInputPerTurn != 42 {
		t.Errorf("AvgInputPerTurn: forventede 42, fik %d", s.AvgInputPerTurn)
	}
}

// --- bar og miniBar (ingen panics ved zero) ---

func TestBarZeroMax(t *testing.T) {
	result := bar(100, 0)
	if len([]rune(result)) == 0 {
		t.Error("bar(100, 0) burde returnere ikke-tom streng")
	}
}

func TestBarFullAndEmpty(t *testing.T) {
	full := bar(100, 100)
	if !strings.Contains(full, "█") {
		t.Error("bar(100, 100) burde indeholde fyldte blokke")
	}
	empty := bar(0, 100)
	if strings.Contains(empty, "█") {
		t.Error("bar(0, 100) burde ikke indeholde fyldte blokke")
	}
}

func TestMiniBarZeroMax(t *testing.T) {
	result := miniBar(100, 0, 10)
	if len(result) == 0 {
		t.Error("miniBar(100, 0, 10) burde returnere ikke-tom streng")
	}
}

func TestMiniBarZeroWidth(t *testing.T) {
	result := miniBar(100, 100, 0)
	if result != "" {
		t.Errorf("miniBar med width=0 burde returnere tom streng, fik %q", result)
	}
}

// --- FormatTUI ---

func TestFormatTUIEmpty(t *testing.T) {
	result := FormatTUI(nil)
	if result == "" {
		t.Error("FormatTUI(nil) burde returnere en besked")
	}
}

func TestFormatTUIZeroTokens(t *testing.T) {
	turns := []TurnStat{
		{TurnNum: 1, InputTokens: 0, OutputTokens: 0},
	}
	// Må ikke panic
	result := FormatTUI(turns)
	if result == "" {
		t.Error("FormatTUI med zero tokens burde returnere indhold")
	}
}

func TestFormatTUIWithCache(t *testing.T) {
	turns := []TurnStat{
		{TurnNum: 1, InputTokens: 1000, OutputTokens: 200, CacheRead: 500},
	}
	result := FormatTUI(turns)
	if !strings.Contains(result, "cache") {
		t.Error("FormatTUI burde vise cache-information når CacheRead > 0")
	}
}

func TestFormatTUIWithRepeat(t *testing.T) {
	turns := []TurnStat{
		{TurnNum: 1, InputTokens: 100, OutputTokens: 50},
		{TurnNum: 2, InputTokens: 150, OutputTokens: 60, IsRepeat: true},
	}
	result := FormatTUI(turns)
	if !strings.Contains(result, "↩") {
		t.Error("FormatTUI burde markere repeat-turns med ↩")
	}
}

func TestFormatTUIContextBreakdown(t *testing.T) {
	turns := []TurnStat{
		{TurnNum: 1, InputTokens: 1000, OutputTokens: 200,
			SysTokens: 100, HistTokens: 800, UserTokens: 100},
	}
	result := FormatTUI(turns)
	if !strings.Contains(result, "System") || !strings.Contains(result, "Historik") {
		t.Error("FormatTUI burde vise kontekst-breakdown")
	}
}

func TestFormatTUINoBreakdownWhenZero(t *testing.T) {
	// Ingen breakdown-sektion hvis alle breakdown-tokens er 0
	turns := []TurnStat{
		{TurnNum: 1, InputTokens: 100, OutputTokens: 50},
	}
	result := FormatTUI(turns)
	if strings.Contains(result, "Kontekst-sammensætning") {
		t.Error("FormatTUI burde ikke vise breakdown-sektion når alle tokens er 0")
	}
}

func TestFormatTUIOptimizingHint(t *testing.T) {
	// Wiki > 20% af total burde give hint
	turns := []TurnStat{
		{TurnNum: 1, InputTokens: 1000, OutputTokens: 200,
			SysTokens: 50, WikiTokens: 300, HistTokens: 600, UserTokens: 50},
	}
	result := FormatTUI(turns)
	if !strings.Contains(result, "Wiki") {
		t.Error("FormatTUI burde give wiki-optimeringsråd når wiki er >20%")
	}
}

// --- FormatAllTUI ---

func TestFormatAllTUIEmpty(t *testing.T) {
	result := FormatAllTUI(nil)
	if result == "" {
		t.Error("FormatAllTUI(nil) burde returnere en besked")
	}
}

func TestFormatAllTUIFiltersOldYears(t *testing.T) {
	summaries := []SessionSummary{
		{
			SessionID: "old",
			Date:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			ByProvider: map[string]int{"anthropic/claude": 5000},
		},
		{
			SessionID:  "current",
			Date:       time.Now(),
			ByProvider: map[string]int{"lmstudio/qwen": 1000},
		},
	}
	result := FormatAllTUI(summaries)
	// Gamle år bør ikke påvirke grafen (dog tjekker vi kun at den kører uden fejl)
	if result == "" {
		t.Error("FormatAllTUI burde returnere indhold")
	}
	if !strings.Contains(result, "lmstudio") {
		t.Error("FormatAllTUI burde vise den aktuelle providers data")
	}
}

func TestFormatAllTUIMultipleProviders(t *testing.T) {
	now := time.Now()
	summaries := []SessionSummary{
		{
			SessionID: "s1",
			Date:      now,
			ByProvider: map[string]int{
				"anthropic/claude": 3000,
				"lmstudio/qwen":    1000,
			},
			TotalInput: 4000,
		},
	}
	result := FormatAllTUI(summaries)
	if !strings.Contains(result, "anthropic") {
		t.Error("burde vise anthropic")
	}
	if !strings.Contains(result, "lmstudio") {
		t.Error("burde vise lmstudio")
	}
}

// --- WriteHTML ---

func TestWriteHTMLCreatesFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "report.html")
	err := WriteHTML(nil, dest)
	if err != nil {
		t.Fatalf("WriteHTML fejlede: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("kunne ikke læse genereret HTML: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "<!DOCTYPE html>") {
		t.Error("HTML-filen mangler DOCTYPE")
	}
	if !strings.Contains(content, "ekte") {
		t.Error("HTML-filen mangler 'ekte' i titlen")
	}
}

func TestWriteHTMLWithSessions(t *testing.T) {
	now := time.Now()
	summaries := []SessionSummary{
		{
			SessionID:  "20260601-120000",
			Date:       now,
			TurnCount:  3,
			TotalInput: 5000,
			TotalOutput: 800,
			ByProvider: map[string]int{"anthropic/claude": 5000},
		},
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "report.html")
	if err := WriteHTML(summaries, dest); err != nil {
		t.Fatalf("WriteHTML fejlede: %v", err)
	}
	data, _ := os.ReadFile(dest)
	content := string(data)
	if !strings.Contains(content, "anthropic") {
		t.Error("HTML-rapporten burde indeholde provider-navn")
	}
	if !strings.Contains(content, "<svg") {
		t.Error("HTML-rapporten burde indeholde SVG-diagram")
	}
}

func TestWriteHTMLCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "subdir", "deep", "report.html")
	if err := WriteHTML(nil, dest); err != nil {
		t.Fatalf("WriteHTML burde oprette parent-mapper: %v", err)
	}
}

// --- fmtK ---

func TestFmtK(t *testing.T) {
	cases := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1 000"},
		{12345, "12 345"},
		{1000000, "1 000 000"},
	}
	for _, c := range cases {
		got := fmtK(c.input)
		if got != c.expected {
			t.Errorf("fmtK(%d) = %q, forventede %q", c.input, got, c.expected)
		}
	}
}
