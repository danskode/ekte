package agent

import (
	"testing"

	"github.com/danskode/ekte/internal/skill"
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
	if a.commandAvailable("/verify") {
		t.Error("/verify bør være skjult uden provider")
	}
	if a.commandAvailable("/plan godkend") {
		t.Error("/plan godkend bør kun vises i plan mode")
	}
	if !a.commandAvailable("/clear") {
		t.Error("/clear bør altid være tilgængelig")
	}
	// libraryUp er false som default (baggrunds-probe kører ikke i test) → remote
	// skills-kommandoer er skjult; /skills [navn] kræver installerede skills.
	if a.commandAvailable("/skills library") || a.commandAvailable("/skills install") {
		t.Error("remote skills-kommandoer bør være skjult når biblioteket ikke kan nås")
	}
	if a.commandAvailable("/skills [navn]") || a.commandAvailable("/skills update") {
		t.Error("/skills [navn] og /skills update bør være skjult uden installerede skills")
	}

	// Aktivér kontekst → kommandoerne dukker op.
	a.cfg.Wiki = &wiki.Wiki{}
	a.planMode = true
	a.cfg.Skills = []skill.Skill{{Name: "tdd"}}
	a.libraryUp.Store(true)
	if !a.commandAvailable("/skills library") {
		t.Error("/skills library bør vises når biblioteket kan nås")
	}
	if !a.commandAvailable(`/wiki "spørgsmål"`) {
		t.Error("wiki-kommando bør vises når wiki er sat op")
	}
	if !a.commandAvailable("/plan godkend") {
		t.Error("/plan godkend bør vises i plan mode")
	}
	if !a.commandAvailable("/skills update") {
		t.Error("/skills update bør vises med installerede skills")
	}
}
