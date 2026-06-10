package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"mellemrum bliver bindestreg", "min nye feature", "min-nye-feature"},
		{"store bogstaver sænkes", "MinFeature", "minfeature"},
		{"specialtegn droppes", "fix: panik! (v2)", "fix-panik-v2"},
		// Sikkerhedsegenskab: '/' og '.' overlever ikke — et navn kan aldrig
		// blive en sti-traversal i .ekte/worktrees/ eller specs/.
		{"traversal neutraliseres", "../../etc/passwd", "etcpasswd"},
		{"skjult sti neutraliseres", ".git/hooks", "githooks"},
		{"kanter trimmes", "--kant--", "kant"},
		{"kun ugyldige tegn", "!!!", ""},
		{"danske tegn droppes", "blåbær", "blbr"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitize(c.in); got != c.want {
				t.Errorf("sanitize(%q) = %q, forventet %q", c.in, got, c.want)
			}
		})
	}
}

func TestEnsureSpec(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "specs", "min-feature.md")

	if err := ensureSpec(path, "min-feature"); err != nil {
		t.Fatalf("ensureSpec: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spec ikke oprettet: %v", err)
	}
	if !strings.Contains(string(data), "# Spec: Min Feature") {
		t.Errorf("spec-skabelon mangler titel:\n%s", data)
	}

	// Eksisterende spec må ikke overskrives.
	if err := os.WriteFile(path, []byte("mit eget indhold"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureSpec(path, "min-feature"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "mit eget indhold" {
		t.Error("ensureSpec overskrev en eksisterende spec")
	}
}

func TestRepoRoot(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(dir, "git", "init", "-q"); err != nil {
		t.Skipf("git ikke tilgængelig: %v", err)
	}
	root, err := RepoRoot(dir)
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	// macOS/symlink-tolerant sammenligning via EvalSymlinks.
	wantResolved, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(root)
	if gotResolved != wantResolved {
		t.Errorf("RepoRoot = %q, forventet %q", root, dir)
	}
}

func TestRepoRootUdenforRepo(t *testing.T) {
	// /proc er aldrig et git-repo og kan ikke indeholde et.
	if _, err := RepoRoot(os.TempDir()); err == nil {
		t.Skip("os.TempDir() ligger tilsyneladende i et git-repo — springer over")
	}
}

func TestCreateAfviserUgyldigtNavn(t *testing.T) {
	if _, err := Create(t.TempDir(), "!!!"); err == nil {
		t.Error("Create burde afvise et navn der saniteres til tom streng")
	}
}
