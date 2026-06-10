package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danskode/ekte/internal/provider"
)

func call(name, args string) provider.ToolCall {
	return provider.ToolCall{ID: "t1", Name: name, Input: json.RawMessage(args)}
}

// projRoot opretter en projektmappe med en fil udenfor — til traversal-tests.
func projRoot(t *testing.T) (root, outsideFile string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "proj")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	outsideFile = filepath.Join(base, "hemmelig.txt")
	if err := os.WriteFile(outsideFile, []byte("må ikke læses"), 0644); err != nil {
		t.Fatal(err)
	}
	return root, outsideFile
}

func TestSafePath(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name    string
		rel     string
		wantErr bool
	}{
		{"normal relativ sti", "src/main.go", false},
		{"punktum er roden", ".", false},
		{"traversal afvises", "../udenfor.txt", true},
		{"dyb traversal afvises", "a/../../../etc/passwd", true},
		{"ren parent afvises", "..", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := safePath(root, c.rel)
			if (err != nil) != c.wantErr {
				t.Errorf("safePath(%q): err=%v, forventet fejl=%v", c.rel, err, c.wantErr)
			}
		})
	}
}

func TestExecuteRespektererWhitelist(t *testing.T) {
	root := t.TempDir()
	if _, err := Execute(call("read_file", `{"path":"x.txt"}`), root, false, false); err == nil ||
		!strings.Contains(err.Error(), "whitelist") {
		t.Errorf("read_file uden canRead burde afvises med whitelist-fejl, fik: %v", err)
	}
	if _, err := Execute(call("write_file", `{"path":"x.txt","content":"y"}`), root, true, false); err == nil ||
		!strings.Contains(err.Error(), "whitelist") {
		t.Errorf("write_file uden canWrite burde afvises med whitelist-fejl, fik: %v", err)
	}
	if _, err := Execute(call("ukendt_tool", `{}`), root, true, true); err == nil {
		t.Error("ukendt tool burde give fejl")
	}
}

func TestReadFileTraversalAfvises(t *testing.T) {
	root, _ := projRoot(t)
	if _, err := Execute(call("read_file", `{"path":"../hemmelig.txt"}`), root, true, false); err == nil {
		t.Error("read_file med ../-traversal burde afvises")
	}
}

func TestReadFileSymlinkEscapeAfvises(t *testing.T) {
	root, outside := projRoot(t)
	link := filepath.Join(root, "genvej.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink ikke understøttet: %v", err)
	}
	_, err := Execute(call("read_file", `{"path":"genvej.txt"}`), root, true, false)
	if err == nil {
		t.Error("read_file via symlink ud af projektmappen burde afvises")
	}
}

func TestReadFileSensitivBlokliste(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".env", "min-token.txt", "id_rsa"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("hemmeligt"), 0644); err != nil {
			t.Fatal(err)
		}
		out, err := Execute(call("read_file", `{"path":"`+name+`"}`), root, true, false)
		if err == nil {
			t.Errorf("read_file(%q) burde afvises af bloklisten, fik output: %q", name, out)
		}
	}
}

func TestReadFileNormal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("indhold her"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := Execute(call("read_file", `{"path":"note.md"}`), root, true, false)
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if out != "indhold her" {
		t.Errorf("read_file gav %q", out)
	}
}

func TestWriteFileOgIdempotens(t *testing.T) {
	root := t.TempDir()
	out, err := Execute(call("write_file", `{"path":"ny/fil.txt","content":"hej"}`), root, false, true)
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if !strings.HasPrefix(out, "✓") {
		t.Errorf("første skrivning burde melde ✓, fik %q", out)
	}
	data, err := os.ReadFile(filepath.Join(root, "ny", "fil.txt"))
	if err != nil || string(data) != "hej" {
		t.Fatalf("filindhold = %q, err=%v", data, err)
	}

	// Samme indhold igen → "allerede gjort"-signal, ikke ny skrivning.
	out, err = Execute(call("write_file", `{"path":"ny/fil.txt","content":"hej"}`), root, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "↩") {
		t.Errorf("identisk genskrivning burde melde ↩, fik %q", out)
	}
}

func TestWriteFileTraversalAfvises(t *testing.T) {
	root, outside := projRoot(t)
	if _, err := Execute(call("write_file", `{"path":"../hemmelig.txt","content":"overskrevet"}`), root, false, true); err == nil {
		t.Error("write_file med traversal burde afvises")
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "må ikke læses" {
		t.Error("filen udenfor projektmappen blev ændret")
	}
}

func TestEditFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "kode.go")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Tilstand 1: erstat unik streng.
	if _, err := Execute(call("edit_file", `{"path":"kode.go","old_string":"beta","new_string":"BETA"}`), root, false, true); err != nil {
		t.Fatalf("edit_file erstat: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "BETA") {
		t.Error("old_string blev ikke erstattet")
	}

	// old_string der ikke findes → fejl.
	if _, err := Execute(call("edit_file", `{"path":"kode.go","old_string":"findesikke","new_string":"x"}`), root, false, true); err == nil {
		t.Error("ukendt old_string burde give fejl")
	}

	// Flertydig old_string → fejl (filen har "a" mange steder — brug gentaget linje).
	if err := os.WriteFile(path, []byte("dup\ndup\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(call("edit_file", `{"path":"kode.go","old_string":"dup","new_string":"x"}`), root, false, true); err == nil {
		t.Error("flertydig old_string burde give fejl")
	}

	// Tilstand 2: insert_after.
	if err := os.WriteFile(path, []byte("linje1\nlinje2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(call("edit_file", `{"path":"kode.go","insert_after":"linje1\n","new_string":"indsat\n"}`), root, false, true); err != nil {
		t.Fatalf("edit_file insert_after: %v", err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "linje1\nindsat\nlinje2\n" {
		t.Errorf("insert_after gav %q", data)
	}

	// Hverken old_string eller insert_after → fejl.
	if _, err := Execute(call("edit_file", `{"path":"kode.go","new_string":"x"}`), root, false, true); err == nil {
		t.Error("edit_file uden old_string/insert_after burde give fejl")
	}
}

func TestCreateDirIdempotens(t *testing.T) {
	root := t.TempDir()
	out, err := Execute(call("create_dir", `{"path":"a/b"}`), root, false, true)
	if err != nil || !strings.HasPrefix(out, "✓") {
		t.Fatalf("create_dir: out=%q err=%v", out, err)
	}
	out, err = Execute(call("create_dir", `{"path":"a/b"}`), root, false, true)
	if err != nil || !strings.HasPrefix(out, "↩") {
		t.Errorf("eksisterende mappe burde melde ↩, fik out=%q err=%v", out, err)
	}
}

func TestSearchFiles(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"main.go":           "package main\nfunc main() {}\n",
		"intern/util.go":    "package intern\n// nøgleord her\n",
		"docs/læsmig.md":    "dokumentation",
		".ekte/skills/x.md": "skill-fil",
	}
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	out, err := Execute(call("search_files", `{"pattern":"*.go"}`), root, true, false)
	if err != nil {
		t.Fatalf("search_files: %v", err)
	}
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "util.go") {
		t.Errorf("glob-søgning manglede filer:\n%s", out)
	}
	if strings.Contains(out, "læsmig.md") {
		t.Error("*.go burde ikke matche .md-filer")
	}

	// Indholdssøgning returnerer linjenumre.
	out, err = Execute(call("search_files", `{"pattern":"*.go","contains":"nøgleord"}`), root, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "util.go") || !strings.Contains(out, "linje 2") {
		t.Errorf("indholdssøgning gav:\n%s", out)
	}

	// Ingen match.
	out, err = Execute(call("search_files", `{"pattern":"*.xyz"}`), root, true, false)
	if err != nil || out != "Ingen filer fundet." {
		t.Errorf("tom søgning gav out=%q err=%v", out, err)
	}
}

func TestSearchFilesSpringerSessionsOver(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, ".ekte", "sessions")
	if err := os.MkdirAll(private, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "historik.json"), []byte("privat samtale"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := Execute(call("search_files", `{"pattern":"historik"}`), root, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "historik.json") {
		t.Error(".ekte/sessions/ burde være udeladt fra søgning")
	}
}

func TestSearchFilesContainsLækkerIkkeSensitive(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "secret-config.txt"), []byte("API_KEY=abc123"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := Execute(call("search_files", `{"pattern":"secret","contains":"API_KEY"}`), root, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "abc123") {
		t.Error("contains-søgning lækkede indhold fra fil på bloklisten")
	}
}

func TestDefinitions(t *testing.T) {
	if defs := Definitions(false, false); len(defs) != 0 {
		t.Errorf("ingen rettigheder burde give 0 tools, fik %d", len(defs))
	}
	readOnly := Definitions(true, false)
	for _, d := range readOnly {
		if d.Name == "write_file" || d.Name == "edit_file" || d.Name == "create_dir" {
			t.Errorf("read-only definitioner indeholdt skrive-tool %s", d.Name)
		}
	}
	all := Definitions(true, true)
	if len(all) != 6 {
		t.Errorf("fuld adgang burde give 6 tools, fik %d", len(all))
	}
}
