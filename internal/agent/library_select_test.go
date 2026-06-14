package agent

import (
	"testing"

	"github.com/danskode/ekte/internal/skill"
)

func TestResolveSkillSelection(t *testing.T) {
	lib := &skill.Library{Skills: []skill.LibraryEntry{
		{Name: "a"}, {Name: "b"}, {Name: "c"},
	}}

	entries, unknown := resolveSkillSelection(lib, "1, 3, b, 9, zzz")
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if len(names) != 3 || names[0] != "a" || names[1] != "c" || names[2] != "b" {
		t.Errorf("entries = %v, forventet [a c b]", names)
	}
	if len(unknown) != 2 { // "9" (uden for interval) og "zzz"
		t.Errorf("unknown = %v, forventet 2 (9, zzz)", unknown)
	}

	// Dubletter (navn + nummer der peger på samme) fjernes.
	entries2, _ := resolveSkillSelection(lib, "a a 1")
	if len(entries2) != 1 {
		t.Errorf("dedupe fejlede: %v", entries2)
	}
}
