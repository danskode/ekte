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
	a.Process(context.Background(), "/mode plan")
	if a.WorkMode() != "plan" {
		t.Fatal("forventede plan mode aktiv")
	}
	a.Process(context.Background(), "/clear")
	if a.WorkMode() != "develop" {
		t.Error("/clear burde afslutte plan mode")
	}
}

func TestWorkModeSkift(t *testing.T) {
	a := New(Config{})
	if a.WorkMode() != "develop" {
		t.Fatalf("default arbejdsmode burde være develop, fik %s", a.WorkMode())
	}

	a.Process(context.Background(), "/mode plan")
	if a.WorkMode() != "plan" {
		t.Error("/mode plan aktiverede ikke plan mode")
	}
	// Plan-systemprompten skal være injiceret.
	found := false
	for _, m := range a.messages {
		if m.Role == "system" && strings.Contains(m.Content, "PLAN MODE") {
			found = true
		}
	}
	if !found {
		t.Error("planModeSystemPrompt ikke injiceret ved /mode plan")
	}

	a.Process(context.Background(), "/mode develop")
	if a.WorkMode() != "develop" {
		t.Error("/mode develop forlod ikke plan mode")
	}

	// toggle: develop → plan → develop
	a.Process(context.Background(), "/mode toggle")
	if a.WorkMode() != "plan" {
		t.Error("/mode toggle burde skifte til plan")
	}
	a.Process(context.Background(), "/mode toggle")
	if a.WorkMode() != "develop" {
		t.Error("/mode toggle burde skifte tilbage til develop")
	}
}
