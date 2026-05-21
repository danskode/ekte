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
)

type CatalogEntry struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
	File        string   `yaml:"file"`
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
