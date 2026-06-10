package agent

import (
	"context"
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
