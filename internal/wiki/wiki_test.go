package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasSubstantiveQuery(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"hej", false},
		{"hvad er en mutex", false}, // kun ét keyword tilbage efter stopord
		{"mutex rwmutex forskel", true},
		{"hvad er forskellen på mutex og rwmutex", true},
		{"", false},
		{"og er i af til", false}, // kun stopord
	}
	for _, c := range cases {
		if got := HasSubstantiveQuery(c.in); got != c.want {
			t.Errorf("HasSubstantiveQuery(%q) = %v, forventet %v", c.in, got, c.want)
		}
	}
}

func TestExtractKeywords(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"stopord fjernes", "hvad er en mutex i Go?", "mutex"},
		{"korte ord fjernes", "er og af xy mutex", "mutex"},
		{"tegnsætning trimmes", `"trådsikkerhed?" goroutines!`, "trådsikkerhed goroutines"},
		{"lowercases", "MUTEX RWMutex", "mutex rwmutex"},
		{"tom", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractKeywords(c.in); got != c.want {
				t.Errorf("extractKeywords(%q) = %q, forventet %q", c.in, got, c.want)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Mutex vs RWMutex", "mutex-vs-rwmutex"},
		{"Hej Verden!", "hej-verden"},
		{"  -kanter-  ", "kanter"},
		// Dokumenteret adfærd: ikke-ASCII (æøå) droppes af slugify.
		{"Næste skridt", "nste-skridt"},
	}
	for _, c := range cases {
		if got := slugify(c.in); got != c.want {
			t.Errorf("slugify(%q) = %q, forventet %q", c.in, got, c.want)
		}
	}
}

func TestExtractWikiLink(t *testing.T) {
	cases := []struct{ in, want string }{
		{"- [[pages/mutex]] — om låse", "wiki/pages/mutex"},
		{"- [[pages/mutex|Mutex-siden]] — alias", "wiki/pages/mutex"},
		{"ingen link her", ""},
		{"defekt [[link uden slut", ""},
	}
	for _, c := range cases {
		if got := extractWikiLink(c.in); got != c.want {
			t.Errorf("extractWikiLink(%q) = %q, forventet %q", c.in, got, c.want)
		}
	}
}

func TestPagesFromIndex(t *testing.T) {
	index := `# Indeks
## Pages
- [[pages/mutex]] — alt om mutex og låse
- [[pages/kanaler]] — Go channels
`
	got := pagesFromIndex(index, "mutex låse")
	if len(got) != 1 || got[0] != "wiki/pages/mutex.md" {
		t.Errorf("pagesFromIndex gav %v", got)
	}
	if got := pagesFromIndex(index, "docker kubernetes"); len(got) != 0 {
		t.Errorf("ingen match burde give tom liste, fik %v", got)
	}
}

func TestDedupe(t *testing.T) {
	got := dedupe([]string{"a", "b", "a", "c", "b"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("dedupe gav %v", got)
	}
}

// testWiki bygger en minimal wiki-mappe med indeks og én side.
func testWiki(t *testing.T) *Wiki {
	t.Helper()
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki", "pages")
	if err := os.MkdirAll(wikiDir, 0755); err != nil {
		t.Fatal(err)
	}
	index := "# Indeks\n## Pages\n- [[pages/mutex]] — om mutex og trådsikkerhed\n"
	if err := os.WriteFile(filepath.Join(root, "wiki", "index.md"), []byte(index), 0644); err != nil {
		t.Fatal(err)
	}
	page := "# Mutex\n\nEn mutex beskytter delt tilstand mod samtidige skrivninger.\n"
	if err := os.WriteFile(filepath.Join(wikiDir, "mutex.md"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}
	return &Wiki{root: root}
}

func TestGrepSearch(t *testing.T) {
	w := testWiki(t)
	results := w.grepSearch("MUTEX") // case-insensitiv
	found := false
	for _, r := range results {
		if strings.HasSuffix(r, "mutex.md") {
			found = true
		}
	}
	if !found {
		t.Errorf("grepSearch fandt ikke mutex-siden, gav %v", results)
	}
	if got := w.grepSearch(""); got != nil {
		t.Errorf("tomme keywords burde give nil, fik %v", got)
	}
}

func TestQueryFinderSide(t *testing.T) {
	w := testWiki(t)
	ctx, pages, err := w.Query("mutex trådsikkerhed forklaring")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("Query fandt ingen sider")
	}
	if !strings.Contains(ctx, "beskytter delt tilstand") {
		t.Error("konteksten indeholder ikke sidens indhold")
	}
}

func TestSavePage(t *testing.T) {
	w := testWiki(t)
	rel, err := w.SavePage("page", "Channels i Go", "Kanaler kommunikerer.")
	if err != nil {
		t.Fatalf("SavePage: %v", err)
	}
	full := filepath.Join(w.root, rel)
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("gemt side ikke fundet: %v", err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\ntype: page\n") {
		t.Error("frontmatter mangler eller er forkert")
	}
	if !strings.Contains(content, "# Channels i Go") || !strings.Contains(content, "Kanaler kommunikerer.") {
		t.Error("titel eller indhold mangler i gemt side")
	}
	// Indekset skal have fået en ny post.
	idx, _ := os.ReadFile(filepath.Join(w.root, "wiki", "index.md"))
	if !strings.Contains(string(idx), "channels-i-go") {
		t.Error("indekset blev ikke opdateret med den nye side")
	}
}

func TestNewDisabled(t *testing.T) {
	w, err := New(&Config{Enabled: false, Path: "/x"})
	if w != nil || err != nil {
		t.Error("deaktiveret config burde give (nil, nil)")
	}
	w, err = New(nil)
	if w != nil || err != nil {
		t.Error("nil config burde give (nil, nil)")
	}
	if _, err := New(&Config{Enabled: true, Path: filepath.Join(t.TempDir(), "mangler")}); err == nil {
		t.Error("manglende wiki-mappe burde give fejl")
	}
}
