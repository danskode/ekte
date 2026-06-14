package agent

import (
	"testing"

	"github.com/danskode/ekte/internal/wiki"
)

func TestCommandAvailable(t *testing.T) {
	a := &Agent{cfg: Config{}} // ingen wiki/hooks/provider, ikke plan mode

	if a.commandAvailable(`/wiki "spørgsmål"`) {
		t.Error("wiki-kommando bør være skjult uden wiki")
	}
	if a.commandAvailable("/hook [navn]") {
		t.Error("/hook [navn] bør være skjult uden hooks")
	}
	if !a.commandAvailable("/hook add <navn> <kommando>") {
		t.Error("/hook add bør altid være tilgængelig (så man kan oprette første hook)")
	}
	if a.commandAvailable("/review") {
		t.Error("/review bør være skjult uden provider")
	}
	if a.commandAvailable("/plan godkend") {
		t.Error("/plan godkend bør kun vises i plan mode")
	}
	if !a.commandAvailable("/clear") {
		t.Error("/clear bør altid være tilgængelig")
	}

	// Aktivér kontekst → kommandoerne dukker op.
	a.cfg.Wiki = &wiki.Wiki{}
	a.planMode = true
	if !a.commandAvailable(`/wiki "spørgsmål"`) {
		t.Error("wiki-kommando bør vises når wiki er sat op")
	}
	if !a.commandAvailable("/plan godkend") {
		t.Error("/plan godkend bør vises i plan mode")
	}
}
