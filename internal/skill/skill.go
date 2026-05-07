package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Hooks struct {
	Pre  string `yaml:"pre"`
	Post string `yaml:"post"`
}

type Skill struct {
	Name        string   `yaml:"name"`
	Version     string   `yaml:"version"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools"`
	Hooks       Hooks    `yaml:"hooks"`
	Tags        []string `yaml:"tags"`

	Body                 string
	SystemPromptAddition string
	Path                 string
}

func LoadAll(dir string) ([]Skill, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{fmt.Errorf("læs skills-mappe: %w", err)}
	}

	var skills []Skill
	var errs []error

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		s, err := load(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		skills = append(skills, *s)
	}
	return skills, errs
}

func load(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	front, body, err := parseFrontmatter(string(data))
	if err != nil {
		return nil, err
	}

	var s Skill
	if err := yaml.Unmarshal([]byte(front), &s); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}

	s.Body = body
	s.SystemPromptAddition = extractSection(body, "System Prompt Addition")
	s.Path = path

	if s.Name == "" {
		s.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	return &s, nil
}

func parseFrontmatter(content string) (front, body string, err error) {
	if !strings.HasPrefix(content, "---") {
		return "", content, nil
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		return "", content, fmt.Errorf("mangler afsluttende ---")
	}
	front = strings.TrimSpace(rest[:idx])
	body = strings.TrimSpace(rest[idx+4:])
	return front, body, nil
}

// extractSection finder indholdet under en ## Overskrift og returnerer
// teksten frem til næste ## eller slutningen af filen.
func extractSection(body, heading string) string {
	lines := strings.Split(body, "\n")
	target := "## " + heading
	var collecting bool
	var out []string

	for _, line := range lines {
		if strings.TrimSpace(line) == target {
			collecting = true
			continue
		}
		if collecting {
			if strings.HasPrefix(line, "## ") {
				break
			}
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
