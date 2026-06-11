package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/danskode/ekte/internal/provider"
)

// TestClearBevarerBaseline: /clear må rydde samtalen, men ALDRIG efterlade
// modellen uden systemprompt, hukommelse og projektkontekst (regression:
// /clear satte messages=nil, hvorefter /context viste systemprompt 0 og
// resten af sessionen kørte helt uden system-instruktioner).
func TestClearBevarerBaseline(t *testing.T) {
	a := New(Config{
		Memory: []provider.Message{{Role: "system", Content: "[Hukommelse — global/note.md]\nvigtig note"}},
	})
	a.AddContext("system", "Projektkontekst (ekte.md):\n\nTestprojektet")
	a.messages = append(a.messages,
		provider.Message{Role: "user", Content: "hej"},
		provider.Message{Role: "assistant", Content: "hej med dig"},
	)

	a.Process(context.Background(), "/clear")

	var hasSys, hasMem, hasCtx, hasChat bool
	for _, m := range a.messages {
		switch {
		case m.Content == baseSystemPrompt:
			hasSys = true
		case strings.HasPrefix(m.Content, "[Hukommelse"):
			hasMem = true
		case strings.Contains(m.Content, "Testprojektet"):
			hasCtx = true
		}
		if m.Role == "user" || m.Role == "assistant" {
			hasChat = true
		}
	}
	if !hasSys {
		t.Error("baseSystemPrompt mangler efter /clear")
	}
	if !hasMem {
		t.Error("hukommelse mangler efter /clear")
	}
	if !hasCtx {
		t.Error("projektkontekst (ekte.md) mangler efter /clear")
	}
	if hasChat {
		t.Error("samtalehistorik burde være væk efter /clear")
	}
	if a.tokenCount == 0 {
		t.Error("tokenCount burde afspejle baseline efter /clear, ikke 0")
	}
}

func TestClearAfslutterPlanMode(t *testing.T) {
	a := New(Config{})
	a.ToggleWorkMode()
	if a.WorkMode() != "plan" {
		t.Fatal("forventede plan mode aktiv")
	}
	a.Process(context.Background(), "/clear")
	if a.WorkMode() != "develop" {
		t.Error("/clear burde afslutte plan mode")
	}
}

func TestWorkModeToggle(t *testing.T) {
	a := New(Config{})
	if a.WorkMode() != "develop" {
		t.Fatalf("default arbejdsmode burde være develop, fik %s", a.WorkMode())
	}

	// develop → plan: systemprompten skal injiceres.
	a.ToggleWorkMode()
	if a.WorkMode() != "plan" {
		t.Error("ToggleWorkMode aktiverede ikke plan mode")
	}
	found := false
	for _, m := range a.messages {
		if m.Role == "system" && strings.Contains(m.Content, "PLAN MODE") {
			found = true
		}
	}
	if !found {
		t.Error("planModeSystemPrompt ikke injiceret ved skift til plan")
	}

	// plan → develop
	a.ToggleWorkMode()
	if a.WorkMode() != "develop" {
		t.Error("ToggleWorkMode forlod ikke plan mode")
	}
}

// TestModeRørerIkkeArbejdsmode: /mode styrer kun verbositet (beginner/expert).
// Arbejdsmode (plan/develop) er en uafhængig akse — man kan være beginner og
// i plan mode samtidig.
func TestModeRørerIkkeArbejdsmode(t *testing.T) {
	a := New(Config{})
	a.ToggleWorkMode() // plan mode aktiv

	a.Process(context.Background(), "/mode beginner")
	if a.WorkMode() != "plan" {
		t.Error("/mode beginner må ikke ændre arbejdsmode")
	}
	a.Process(context.Background(), "/mode expert")
	if a.WorkMode() != "plan" {
		t.Error("/mode expert må ikke ændre arbejdsmode")
	}
	// plan/develop er IKKE gyldige /mode-argumenter længere
	a.Process(context.Background(), "/mode develop")
	if a.WorkMode() != "plan" {
		t.Error("/mode develop burde være ukendt og ikke ændre arbejdsmode")
	}
}

// TestTrimHistoryBevarerBaseline: regression — kun de første 2 system-beskeder
// blev bevaret, så hukommelse, hook-noter og projektkontekst forsvandt efter
// første tur (og ved resume, hvor baseline ligger sidst, næsten alt).
func TestTrimHistoryBevarerBaseline(t *testing.T) {
	var msgs []provider.Message
	for i := 0; i < 6; i++ {
		msgs = append(msgs, provider.Message{Role: "system", Content: fmt.Sprintf("viden-%d", i)})
	}
	// Dublet (fx plan-mode-prompt tilføjet to gange) skal dedupliceres.
	msgs = append(msgs, provider.Message{Role: "system", Content: "viden-0"})
	msgs = append(msgs,
		provider.Message{Role: "user", Content: "hej"},
		provider.Message{Role: "assistant", Content: "hej igen"},
	)
	out := trimHistory(msgs, 20)
	var sysCount int
	for _, m := range out {
		if m.Role == "system" {
			sysCount++
		}
	}
	if sysCount != 6 {
		t.Errorf("forventet 6 unikke system-beskeder bevaret, fik %d", sysCount)
	}
	if len(out) != 8 {
		t.Errorf("forventet 8 beskeder i alt (6 system + 2 samtale), fik %d", len(out))
	}

	// Loft: ved >16 unikke system-beskeder beholdes de første 4 og de nyeste 12.
	var mange []provider.Message
	for i := 0; i < 30; i++ {
		mange = append(mange, provider.Message{Role: "system", Content: fmt.Sprintf("s-%d", i)})
	}
	mange = append(mange, provider.Message{Role: "user", Content: "hej"})
	out = trimHistory(mange, 20)
	if len(out) != 17 { // 16 system + 1 user
		t.Fatalf("forventet 17 beskeder efter loft, fik %d", len(out))
	}
	if out[0].Content != "s-0" || out[3].Content != "s-3" || out[4].Content != "s-18" || out[15].Content != "s-29" {
		t.Errorf("loftet beholdt forkerte system-beskeder: først=%s, sidst=%s", out[0].Content, out[15].Content)
	}
}

// TestTrimToBudget: regression — ekte loggede ctx_pct over 100% men sendte
// prompten alligevel; LM Studio afviste så hele kaldet med en SSE-fejl.
func TestTrimToBudget(t *testing.T) {
	big := strings.Repeat("x", 4000) // ≈1000 tokens pr. besked
	msgs := []provider.Message{
		{Role: "system", Content: big},
		{Role: "user", Content: big},
		{Role: "assistant", Content: big, ToolCalls: []provider.ToolCall{{ID: "t1", Name: "read_file"}}},
		{Role: "tool", Content: big, ToolCallID: "t1"},
		{Role: "user", Content: big},
		{Role: "assistant", Content: big},
	}
	// Budget 4000 → ~3600 effektivt; 6 beskeder à ~1000 + 500 overhead ≈ 6500.
	out := trimToBudget(msgs, 4000)
	if estimateTokens(out) > 3600 {
		t.Errorf("budget ikke overholdt: est=%d", estimateTokens(out))
	}
	// System-beskeden skal bevares.
	if out[0].Role != "system" {
		t.Error("system-besked burde bevares først")
	}
	// Tool-blok må ikke splittes: ingen tool-besked uden sin assistant med ToolCalls.
	for i, m := range out {
		if m.Role == "tool" {
			if i == 0 || len(out[i-1].ToolCalls) == 0 {
				t.Error("forældreløst tool-svar efter beskæring")
			}
		}
	}
	// Mindst én user-besked skal overleve — selv ved umuligt lille budget.
	out = trimToBudget(msgs, 600)
	users := 0
	for _, m := range out {
		if m.Role == "user" {
			users++
		}
	}
	if users == 0 {
		t.Error("mindst én user-besked skal altid bevares")
	}
}

// TestReprobeContext: LM Studio JIT-genloader modeller med server-default
// context — re-proben skal kun kunne SKRUMPE og skal udsende EventModelInfo
// så statuslinjen følger med.
func TestReprobeContext(t *testing.T) {
	a := New(Config{ContextSize: 32768, ProbeContext: func() (string, int, bool) {
		return "m", 8192, true
	}})
	ch := make(chan Event, 4)
	if !a.reprobeContext(ch) {
		t.Fatal("skrumpet context burde give re-klampning")
	}
	if a.cfg.ContextSize != 8192 {
		t.Errorf("ContextSize burde være 8192, er %d", a.cfg.ContextSize)
	}
	var sawInfo bool
	close(ch)
	for ev := range ch {
		if ev.Type == EventModelInfo && ev.Tokens == 8192 {
			sawInfo = true
		}
	}
	if !sawInfo {
		t.Error("EventModelInfo med ny context ikke udsendt")
	}

	// Større loaded context må IKKE hæve over config-værdien.
	a = New(Config{ContextSize: 8192, ProbeContext: func() (string, int, bool) {
		return "m", 32768, true
	}})
	if a.reprobeContext(make(chan Event, 2)) {
		t.Error("voksende context burde ikke ændre noget")
	}
}

// TestUpsertAutoSection: auto-sektionen i ekte.md erstattes idempotent —
// resten af filen (brugerens egen PRD-tekst) må aldrig røres.
func TestUpsertAutoSection(t *testing.T) {
	første := upsertAutoSection("# Mit projekt\n\nPRD-tekst her.\n", "Notat v1")
	if !strings.Contains(første, "PRD-tekst her.") || !strings.Contains(første, "Notat v1") {
		t.Fatalf("første upsert mangler indhold: %q", første)
	}
	anden := upsertAutoSection(første, "Notat v2")
	if strings.Contains(anden, "Notat v1") {
		t.Error("gammel auto-sektion burde være erstattet")
	}
	if !strings.Contains(anden, "Notat v2") || !strings.Contains(anden, "PRD-tekst her.") {
		t.Errorf("anden upsert mangler indhold: %q", anden)
	}
	if strings.Count(anden, autoSectionStart) != 1 {
		t.Error("der må kun være én auto-sektion")
	}
}
