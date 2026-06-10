package dep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseGoMod(t *testing.T) {
	gomod := `module github.com/danskode/eksempel

go 1.21

require github.com/enkelt/modul v1.2.3

require (
	github.com/foo/bar v0.5.0
	// kommentar springes over
	github.com/baz/qux v2.0.0+incompatible // indirect
)
`
	path := filepath.Join(t.TempDir(), "go.mod")
	if err := os.WriteFile(path, []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	mods, err := ParseGoMod(path)
	if err != nil {
		t.Fatalf("ParseGoMod: %v", err)
	}
	want := []Module{
		{Path: "github.com/enkelt/modul", Version: "v1.2.3"},
		{Path: "github.com/foo/bar", Version: "v0.5.0"},
		{Path: "github.com/baz/qux", Version: "v2.0.0+incompatible"},
	}
	if len(mods) != len(want) {
		t.Fatalf("fik %d moduler, forventet %d: %v", len(mods), len(want), mods)
	}
	for i, m := range mods {
		if m != want[i] {
			t.Errorf("modul[%d] = %v, forventet %v", i, m, want[i])
		}
	}
}

func TestParseGoModManglendeFil(t *testing.T) {
	if _, err := ParseGoMod(filepath.Join(t.TempDir(), "findes-ikke")); err == nil {
		t.Error("manglende go.mod burde give fejl")
	}
}

func TestEncodePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"github.com/danskode/ekte", "github.com/danskode/ekte"},
		{"github.com/Azure/azure-sdk", "github.com/!azure/azure-sdk"},
		{"github.com/BurntSushi/toml", "github.com/!burnt!sushi/toml"},
	}
	for _, c := range cases {
		if got := encodePath(c.in); got != c.want {
			t.Errorf("encodePath(%q) = %q, forventet %q", c.in, got, c.want)
		}
	}
}

func TestShortPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"github.com/charmbracelet/bubbletea", "charmbracelet/bubbletea"},
		{"kortnavn", "kortnavn"},
	}
	for _, c := range cases {
		if got := shortPath(c.in); got != c.want {
			t.Errorf("shortPath(%q) = %q, forventet %q", c.in, got, c.want)
		}
	}
}

func TestRating(t *testing.T) {
	old := time.Now().Add(-365 * 24 * time.Hour)
	fresh := time.Now().Add(-5 * 24 * time.Hour)
	cases := []struct {
		name string
		s    Score
		want int
	}{
		{"moden uden CVE", Score{Released: old}, 5},
		{"helt ny udgivelse", Score{Released: fresh}, 4},
		{"ukendt udgivelsesdato", Score{}, 4},
		{"én CVE", Score{Released: old, VulnCount: 1}, 3},
		{"mange CVE", Score{Released: old, VulnCount: 4}, 2},
		{"værst tænkelig bunder i 1", Score{VulnCount: 10}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.s.rating(); got != c.want {
				t.Errorf("rating() = %d, forventet %d", got, c.want)
			}
		})
	}
}

func TestVerdict(t *testing.T) {
	old := time.Now().Add(-365 * 24 * time.Hour)
	if v := (Score{Released: old}).verdict(); !strings.HasPrefix(v, "✓") {
		t.Errorf("ren score burde være ✓, fik %q", v)
	}
	if v := (Score{Released: old, VulnCount: 2}).verdict(); !strings.HasPrefix(v, "⚠") {
		t.Errorf("score med CVE burde være ⚠, fik %q", v)
	}
	// Bemærk: "~"-dommen (rating 3 uden CVE'er) kan ikke nås med den nuværende
	// rating-formel — en helt ny udgivelse lander på 4 og er stadig "✓".
	if v := (Score{Released: time.Now()}).verdict(); !strings.HasPrefix(v, "✓") {
		t.Errorf("helt ny udgivelse rater 4 og burde være ✓, fik %q", v)
	}
}

func TestStars(t *testing.T) {
	old := time.Now().Add(-365 * 24 * time.Hour)
	if got := (Score{Released: old}).stars(); got != "★★★★★" {
		t.Errorf("stars() = %q", got)
	}
	if got := (Score{VulnCount: 10}).stars(); got != "★☆☆☆☆" {
		t.Errorf("stars() = %q", got)
	}
}

func TestRenderReport(t *testing.T) {
	old := time.Now().Add(-365 * 24 * time.Hour)
	scores := []Score{
		{Module: "github.com/a/ren", Version: "v1.0.0", Released: old},
		{Module: "github.com/b/sårbar", Version: "v0.1.0", Released: old, VulnCount: 1, Vulns: []string{"GO-2026-0001: testbeskrivelse"}},
		{Module: "github.com/c/fejl", Version: "v0.0.1", Err: "timeout"},
	}
	out := RenderReport("Projekt", scores)
	for _, frag := range []string{"a/ren", "b/sårbar", "GO-2026-0001", "c/fejl"} {
		if !strings.Contains(out, frag) {
			t.Errorf("rapport mangler %q:\n%s", frag, out)
		}
	}
}
