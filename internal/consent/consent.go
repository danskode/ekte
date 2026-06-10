// Pakke consent håndterer persistent brugersamtykke til lokale/private
// provider-URL'er (Ollama, LM Studio m.fl.).
//
// Samtykke gemmes i consent.yaml i brugerens GLOBALE ekte-mappe (~/.ekte/) —
// aldrig i projektets .ekte/. En klonet eller manipuleret projekt-config kan
// derfor ikke give sig selv samtykke; kun de interaktive flows i cmd/ekte
// (opstartsdialog, Ollama-guide, model-wizardens bekræftelse) skriver til filen.
//
// Matching sker på den præcise, trimmede URL-streng: ændres base_url — også
// bare port eller sti — kræves nyt samtykke.
package consent

import (
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const fileName = "consent.yaml"

type record struct {
	URL     string `yaml:"url"`
	Granted string `yaml:"granted"` // ISO-dato — kun til menneskelig læsning
}

type consentFile struct {
	LocalProviders []record `yaml:"local_providers"`
}

// EnvOverride returnerer true hvis EKTE_ALLOW_LOCAL_PROVIDER er sat —
// den globale override til headless/scriptet brug.
func EnvOverride() bool {
	return os.Getenv("EKTE_ALLOW_LOCAL_PROVIDER") != ""
}

// IsPrivateURL returnerer true hvis URL'en peger på en lokal/privat adresse
// (localhost, loopback, RFC1918, link-local). Hostnavne der først resolver
// til private IP'er ved opslag fanges ikke her — det håndteres af
// DialContext-tjekket i provider-laget (DNS rebinding, CWE-918).
func IsPrivateURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" || host == "ip6-localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func path(globalDir string) string {
	return filepath.Join(globalDir, fileName)
}

func load(globalDir string) consentFile {
	var f consentFile
	data, err := os.ReadFile(path(globalDir))
	if err != nil {
		return f
	}
	// Ulæselig fil → tomt samtykke (fail closed).
	_ = yaml.Unmarshal(data, &f)
	return f
}

// Granted returnerer true hvis brugeren tidligere har givet samtykke til
// præcis denne URL. globalDir SKAL være den globale ekte-mappe (~/.ekte) —
// giv aldrig en projektmappe her.
func Granted(globalDir, baseURL string) bool {
	target := strings.TrimSpace(baseURL)
	if target == "" {
		return false
	}
	for _, r := range load(globalDir).LocalProviders {
		if strings.TrimSpace(r.URL) == target {
			return true
		}
	}
	return false
}

// Grant gemmer samtykke for præcis denne URL. Idempotent — en URL optræder
// højst én gang. Filen skrives 0600: den er brugerens private tillidsliste.
func Grant(globalDir, baseURL string) error {
	target := strings.TrimSpace(baseURL)
	if target == "" {
		return nil
	}
	if Granted(globalDir, target) {
		return nil
	}
	f := load(globalDir)
	f.LocalProviders = append(f.LocalProviders, record{
		URL:     target,
		Granted: time.Now().Format("2006-01-02"),
	})
	data, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		return err
	}
	return os.WriteFile(path(globalDir), data, 0600)
}
