package wiki

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const templateRepo = "https://github.com/danskode/simple-wiki.git"

type Config struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

type Page struct {
	Path    string
	Content string
}

type Wiki struct {
	root string
}

// Root returnerer wikiens rodmappe — til visning i fx /context.
func (w *Wiki) Root() string { return w.root }

func New(cfg *Config) (*Wiki, error) {
	if cfg == nil || !cfg.Enabled || cfg.Path == "" {
		return nil, nil
	}
	path := expandHome(cfg.Path)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("wiki-mappe ikke fundet: %s — kør 'ekte init' for at sætte den op", path)
	}
	return &Wiki{root: path}, nil
}

// Query finder relevante wiki-sider for et spørgsmål og returnerer dem som kontekst.
func (w *Wiki) Query(question string) (string, []Page, error) {
	index, err := w.readIndex()
	if err != nil {
		return "", nil, fmt.Errorf("index: %w", err)
	}

	keywords := extractKeywords(question)
	searchResults, _ := w.search(keywords)

	candidates := dedupe(append(
		pagesFromIndex(index, keywords),
		searchResults...,
	))

	var pages []Page
	for _, rel := range candidates {
		full := filepath.Join(w.root, rel)
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		pages = append(pages, Page{Path: rel, Content: string(data)})
	}

	ctx := buildContext(question, index, pages)
	return ctx, pages, nil
}

// SavePage gemmer en ny side i wikien med korrekt frontmatter.
func (w *Wiki) SavePage(pageType, title, content string) (string, error) {
	dir := filepath.Join(w.root, "wiki", pageType+"s")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	slug := slugify(title)
	path := filepath.Join(dir, slug+".md")

	today := todayISO()
	full := fmt.Sprintf("---\ntype: %s\ntags: []\ncreated: %s\nupdated: %s\nsource_count: 0\n---\n# %s\n\n%s\n",
		pageType, today, today, title, content)

	if err := os.WriteFile(path, []byte(full), 0644); err != nil {
		return "", err
	}

	rel := strings.TrimPrefix(path, w.root+"/")
	_ = w.updateIndex(pageType, title, slug, rel)
	_ = w.appendLog("save", title)

	return rel, nil
}

// SaveRaw gemmer markdown-indhold direkte på den angivne relative sti under wiki-roden.
func (w *Wiki) SaveRaw(relPath, content string) (string, error) {
	full := filepath.Join(w.root, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		return "", err
	}
	return relPath, nil
}

func (w *Wiki) readIndex() (string, error) {
	data, err := os.ReadFile(filepath.Join(w.root, "wiki", "index.md"))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (w *Wiki) search(keywords string) ([]string, error) {
	script := filepath.Join(w.root, "tools", "search.sh")
	if _, err := os.Stat(script); os.IsNotExist(err) {
		return w.grepSearch(keywords), nil
	}
	cmd := exec.Command("/bin/sh", script, keywords)
	cmd.Dir = w.root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			paths = append(paths, strings.TrimSpace(line))
		}
	}
	return paths, nil
}

// grepSearch er fallback hvis search.sh ikke eksisterer.
func (w *Wiki) grepSearch(keywords string) []string {
	wikiDir := w.root
	var results []string
	words := strings.Fields(keywords)
	if len(words) == 0 {
		return nil
	}

	_ = filepath.Walk(wikiDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lower := strings.ToLower(string(data))
		for _, kw := range words {
			if strings.Contains(lower, strings.ToLower(kw)) {
				rel := strings.TrimPrefix(path, w.root+"/")
				results = append(results, rel)
				break
			}
		}
		return nil
	})
	return results
}

func (w *Wiki) updateIndex(pageType, title, slug, rel string) error {
	indexPath := filepath.Join(w.root, "wiki", "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	content := string(data)
	section := "## " + capitalize(pageType) + "s"
	entry := fmt.Sprintf("- [[%s]] — %s", strings.TrimSuffix(rel, ".md"), title)

	if idx := strings.Index(content, section); idx != -1 {
		insertAt := idx + len(section) + 1
		content = content[:insertAt] + entry + "\n" + content[insertAt:]
		return os.WriteFile(indexPath, []byte(content), 0644)
	}
	return nil
}

func (w *Wiki) appendLog(op, detail string) error {
	logPath := filepath.Join(w.root, "wiki", "log.md")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n## [%s] %s | %s\n", todayISO(), op, detail)
	return err
}

func pagesFromIndex(index string, keywords string) []string {
	var results []string
	words := strings.Fields(strings.ToLower(keywords))
	scanner := bufio.NewScanner(strings.NewReader(index))
	for scanner.Scan() {
		line := scanner.Text()
		lower := strings.ToLower(line)
		for _, w := range words {
			if strings.Contains(lower, w) && strings.Contains(line, "[[") {
				if path := extractWikiLink(line); path != "" {
					results = append(results, path+".md")
				}
				break
			}
		}
	}
	return results
}

func extractWikiLink(line string) string {
	start := strings.Index(line, "[[")
	end := strings.Index(line, "]]")
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	link := line[start+2 : end]
	if strings.Contains(link, "|") {
		link = strings.SplitN(link, "|", 2)[0]
	}
	return "wiki/" + link
}

func buildContext(question string, index string, pages []Page) string {
	if len(pages) == 0 {
		return fmt.Sprintf("Wiki-indeks:\n%s\n\nIngen specifikke sider fundet for: %s", index, question)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Relevante wiki-sider for '%s':\n\n", question))
	for _, p := range pages {
		sb.WriteString(fmt.Sprintf("--- %s ---\n%s\n\n", p.Path, p.Content))
	}
	return sb.String()
}

// HasSubstantiveQuery returnerer true hvis input indeholder mindst 2
// ikke-trivielle keywords. Forhindrer wiki-opslag for hilsener og korte fraser.
func HasSubstantiveQuery(input string) bool {
	return len(strings.Fields(extractKeywords(input))) >= 2
}

func extractKeywords(question string) string {
	stopwords := map[string]bool{
		"hvad": true, "er": true, "en": true, "et": true, "og": true,
		"i": true, "af": true, "til": true, "for": true, "med": true,
		"what": true, "is": true, "a": true, "an": true, "the": true,
		"and": true, "or": true, "in": true, "of": true, "to": true,
		"how": true, "does": true, "can": true, "jeg": true, "vil": true,
	}
	words := strings.Fields(strings.ToLower(question))
	var keywords []string
	for _, w := range words {
		w = strings.Trim(w, "\"'?.,!")
		if !stopwords[w] && len(w) > 2 {
			keywords = append(keywords, w)
		}
	}
	return strings.Join(keywords, " ")
}

func dedupe(paths []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func slugify(title string) string {
	title = strings.ToLower(title)
	title = strings.ReplaceAll(title, " ", "-")
	var out strings.Builder
	for _, r := range title {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out.WriteRune(r)
		}
	}
	return strings.Trim(out.String(), "-")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func todayISO() string {
	t, _ := exec.Command("date", "+%Y-%m-%d").Output()
	return strings.TrimSpace(string(t))
}
