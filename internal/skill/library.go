package skill

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	CatalogURL = "https://raw.githubusercontent.com/danskode/SKILLeton/main/catalog.yaml"
	rawBase    = "https://raw.githubusercontent.com/danskode/SKILLeton/main/"
	// CatalogSchema er den katalog-skema-version ekte forventer. Multi-repo-
	// kontrakten mellem ekte og SKILLeton er løs YAML; ligger SKILLeton's
	// top-level version højere, kender denne ekte-version måske ikke alle felter.
	CatalogSchema = 1
)

type CatalogEntry struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Version     string   `yaml:"version"`
	Tags        []string `yaml:"tags"`
	File        string   `yaml:"file"`
	// Requires angiver hvilke harness-funktioner denne skill er obligatorisk for.
	// "harness" = altid (AIDD er præmissen), "wiki" = når wiki er tilvalgt.
	Requires []string `yaml:"requires"`
}

// RequiredFor returnerer de skills i kataloget der er obligatoriske for en
// given funktion (fx "harness" eller "wiki").
func (c *Catalog) RequiredFor(feature string) []CatalogEntry {
	var out []CatalogEntry
	for _, s := range c.Skills {
		for _, r := range s.Requires {
			if r == feature {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

type Catalog struct {
	Version int            `yaml:"version"`
	Skills  []CatalogEntry `yaml:"skills"`
}

func FetchCatalog() (*Catalog, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(CatalogURL)
	if err != nil {
		return nil, fmt.Errorf("hent katalog: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var cat Catalog
	if err := yaml.Unmarshal(data, &cat); err != nil {
		return nil, fmt.Errorf("parse katalog: %w", err)
	}
	return &cat, nil
}

func DownloadSkill(entry CatalogEntry, destDir string) error {
	url := rawBase + entry.File
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(destDir, entry.Name+".md"), data, 0644)
}

// InstalledNames returnerer navnene på skills der allerede ligger i skillsDir.
func InstalledNames(skillsDir string) map[string]bool {
	installed := map[string]bool{}
	entries, _ := os.ReadDir(skillsDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			installed[strings.TrimSuffix(e.Name(), ".md")] = true
		}
	}
	return installed
}

// InstalledVersions returnerer en map fra skill-navn til installeret version
// (læst fra frontmatterens version-felt; tom streng hvis ikke angivet).
func InstalledVersions(skillsDir string) map[string]string {
	versions := map[string]string{}
	skills, _ := LoadAll(skillsDir)
	for _, s := range skills {
		versions[s.Name] = s.Version
	}
	return versions
}

// FetchSkillContent henter den rå markdown for en skill fra SKILLeton uden at
// gemme den — bruges til at læse en skill igennem før installation.
func FetchSkillContent(entry CatalogEntry) (string, error) {
	url := rawBase + entry.File
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("hent skill: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// VersionNewer returnerer true hvis a er en nyere semver end b (simpel
// numerisk sammenligning af punktum-adskilte felter). Manglende felter = 0.
func VersionNewer(a, b string) bool {
	pa, pb := splitVersion(a), splitVersion(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func splitVersion(v string) []int {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}
