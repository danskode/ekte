package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitChunks(t *testing.T) {
	page := Page{Path: "concepts/x.md", Content: "---\ntype: concept\n---\n# Titel\nintro-tekst\n\n## Definition\ndef-tekst\n\n## Detaljer\ndetalje-tekst\n"}
	chunks := splitChunks(page)
	if len(chunks) != 3 {
		t.Fatalf("forventede 3 chunks (preamble + 2 sektioner), fik %d", len(chunks))
	}
	if chunks[0].Heading != "" || !strings.Contains(chunks[0].Body, "intro-tekst") {
		t.Errorf("preamble forkert: %+v", chunks[0])
	}
	if chunks[1].Heading != "## Definition" || !strings.Contains(chunks[1].Body, "def-tekst") {
		t.Errorf("sektion 1 forkert: %+v", chunks[1])
	}
	// Frontmatter må ikke lække ind i nogen chunk.
	for _, c := range chunks {
		if strings.Contains(c.Body, "type: concept") {
			t.Errorf("frontmatter lækkede ind i chunk: %+v", c)
		}
	}
}

func TestScoreChunkHeadingWeight(t *testing.T) {
	withHeading := Chunk{Heading: "## Mutex", Body: "tekst"}
	bodyOnly := Chunk{Heading: "## Andet", Body: "mutex"}
	kw := []string{"mutex"}
	if scoreChunk(withHeading, kw) <= scoreChunk(bodyOnly, kw) {
		t.Errorf("overskrift-match bør veje tungere: head=%d body=%d",
			scoreChunk(withHeading, kw), scoreChunk(bodyOnly, kw))
	}
}

func TestBuildBudgetedContextPicksRelevantSection(t *testing.T) {
	// Relevant sektion ligger til sidst — head-trunkering ville misse den.
	page := Page{Path: "concepts/x.md", Content: "# Titel\n## Irrelevant\n" +
		strings.Repeat("blah ", 50) + "\n## Mutex\nmutex og rwmutex forklaret her\n"}
	body, paths := BuildBudgetedContext("mutex rwmutex", []Page{page}, 100)
	if body == "" {
		t.Fatal("forventede ikke-tom kontekst")
	}
	if !strings.Contains(body, "Mutex") || !strings.Contains(body, "rwmutex forklaret") {
		t.Errorf("forventede at den relevante Mutex-sektion blev valgt, fik:\n%s", body)
	}
	if len(paths) != 1 || paths[0] != "concepts/x.md" {
		t.Errorf("forventede proveniens concepts/x.md, fik %v", paths)
	}
	// Proveniens-header skal vise side › sektion.
	if !strings.Contains(body, "concepts/x.md › Mutex") {
		t.Errorf("forventede proveniens-header, fik:\n%s", body)
	}
}

func TestBuildBudgetedContextEmpty(t *testing.T) {
	if body, paths := BuildBudgetedContext("noget", nil, 100); body != "" || paths != nil {
		t.Errorf("tom input bør give tomt resultat, fik body=%q paths=%v", body, paths)
	}
	if body, _ := BuildBudgetedContext("noget", []Page{{Path: "a.md", Content: "x"}}, 0); body != "" {
		t.Errorf("nul-budget bør give tom kontekst, fik %q", body)
	}
}

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
