package agent

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/danskode/ekte/internal/provider"
)

// modelWizardState holder tilstand for /model setup-wizarden.
type modelWizardState struct {
	step        int    // 0=provider 1=baseURL 2=model 3=context 4=confirm
	provider    string // "anthropic" | "openai"
	model       string
	baseURL     string
	contextSize int
	// needsURL angiver at den valgte provider kræver en base URL (ollama, lmstudio)
	needsURL bool
}

const wizardHint = "  (skriv 'annuller' for at afbryde)"

func (a *Agent) handleModel(arg string) []Event {
	parts := strings.Fields(arg)
	if len(parts) == 0 {
		var sb strings.Builder
		sb.WriteString("Aktuel model-konfiguration:\n\n")
		prov := a.cfg.ProviderName
		if prov == "" {
			prov = "(ikke sat)"
		}
		model := a.cfg.ModelName
		if model == "" {
			model = "(ikke sat)"
		}
		sb.WriteString(fmt.Sprintf("  Provider:    %s\n", prov))
		if a.cfg.BaseURL != "" {
			sb.WriteString(fmt.Sprintf("  URL:         %s\n", a.cfg.BaseURL))
		}
		sb.WriteString(fmt.Sprintf("  Model:       %s\n", model))
		if a.cfg.ContextSize > 0 {
			sb.WriteString(fmt.Sprintf("  Kontekst:    %d tokens\n", a.cfg.ContextSize))
		} else {
			sb.WriteString("  Kontekst:    (bruger model-default)\n")
		}
		sb.WriteString("\nBrug '/model setup' for guided opsætning.")
		return []Event{{Type: EventSystem, Content: sb.String()}}
	}

	switch strings.ToLower(parts[0]) {
	case "setup":
		return a.startModelWizard()

	case "context":
		if len(parts) < 2 {
			return []Event{{Type: EventSystem, Content: "Brug: /model context <antal-tokens>  — fx /model context 128000"}}
		}
		n := 0
		if _, err := fmt.Sscanf(parts[1], "%d", &n); err != nil || n < 1000 || n > 2_000_000 {
			return []Event{{Type: EventSystem, Content: "Ugyldigt antal tokens — angiv et tal mellem 1000 og 2000000."}}
		}
		a.modelWizard = &modelWizardState{step: 4, contextSize: n,
			provider: a.cfg.ProviderName, model: a.cfg.ModelName, baseURL: a.cfg.BaseURL}
		return []Event{{Type: EventSystem, Content: fmt.Sprintf(
			"Sæt kontekststørrelse til %d tokens.\nSkriv 'j' for at bekræfte eller 'n' for at annullere.", n)}}

	case "anthropic":
		if len(parts) < 2 {
			return []Event{{Type: EventSystem, Content: "Brug: /model anthropic <modelnavn>  — fx /model anthropic claude-sonnet-4-6"}}
		}
		if err := provider.ValidateModelName(parts[1]); err != nil {
			return []Event{{Type: EventSystem, Content: "Ugyldigt modelnavn: " + err.Error()}}
		}
		a.modelWizard = &modelWizardState{step: 4, provider: "anthropic", model: parts[1]}
		return []Event{{Type: EventSystem, Content: fmt.Sprintf(
			"Skift til anthropic / %s.\nSkriv 'j' for at bekræfte eller 'n' for at annullere.", parts[1])}}

	case "openai":
		if len(parts) < 2 {
			return []Event{{Type: EventSystem, Content: "Brug: /model openai <modelnavn>  — fx /model openai gpt-4o"}}
		}
		if err := provider.ValidateModelName(parts[1]); err != nil {
			return []Event{{Type: EventSystem, Content: "Ugyldigt modelnavn: " + err.Error()}}
		}
		a.modelWizard = &modelWizardState{step: 4, provider: "openai", model: parts[1]}
		return []Event{{Type: EventSystem, Content: fmt.Sprintf(
			"Skift til openai / %s.\nSkriv 'j' for at bekræfte eller 'n' for at annullere.", parts[1])}}

	case "ollama", "lmstudio":
		if len(parts) < 3 {
			return []Event{{Type: EventSystem, Content: "Brug: /model ollama <url> <modelnavn>  — fx /model ollama http://localhost:11434/v1 llama3.2"}}
		}
		baseURL, modelName := parts[1], parts[2]
		if err := validateBaseURL(baseURL); err != nil {
			return []Event{{Type: EventSystem, Content: "Ugyldig URL: " + err.Error()}}
		}
		if err := provider.ValidateModelName(modelName); err != nil {
			return []Event{{Type: EventSystem, Content: "Ugyldigt modelnavn: " + err.Error()}}
		}
		a.modelWizard = &modelWizardState{step: 4, provider: "openai", model: modelName, baseURL: baseURL}
		privateWarn := ""
		if baseURLIsPrivate(baseURL) {
			privateWarn = "\n⚠ URL peger på lokal/privat adresse — 'j' gemmer permanent samtykke for præcis denne URL."
		}
		return []Event{{Type: EventSystem, Content: fmt.Sprintf(
			"Skift til %s / %s via %s.%s\nSkriv 'j' for at bekræfte eller 'n' for at annullere.", parts[0], modelName, baseURL, privateWarn)}}

	default:
		return []Event{{Type: EventSystem, Content: "Ukendt /model-argument. Brug '/model setup' eller '/model' for hjælp."}}
	}
}

func (a *Agent) startModelWizard() []Event {
	// Forudfyld med aktuelle værdier — Enter bevarer dem
	a.modelWizard = &modelWizardState{
		step:        0,
		provider:    a.cfg.ProviderName,
		model:       a.cfg.ModelName,
		baseURL:     a.cfg.BaseURL,
		contextSize: a.cfg.ContextSize,
	}
	cur := a.cfg.ProviderName
	if cur == "" {
		cur = "ikke sat"
	}
	return []Event{{Type: EventSystem, Content: fmt.Sprintf(
		"Model-wizard startet.\n\nVælg provider:\n  1. anthropic\n  2. openai\n  3. ollama\n  4. lmstudio (http://localhost:1234/v1)\n\n"+
			"Aktuel: %s — tryk Enter for at beholde, eller skriv nyt valg.\n%s",
		cur, wizardHint)}}
}

func (a *Agent) advanceModelWizard(input string) []Event {
	w := a.modelWizard
	low := strings.ToLower(strings.TrimSpace(input))
	val := strings.TrimSpace(input)

	if low == "annuller" || low == "cancel" || low == "q" || low == "quit" {
		a.modelWizard = nil
		return []Event{{Type: EventSystem, Content: "Model-wizard afbrudt — ingen ændringer."}}
	}

	switch w.step {
	case 0: // vælg provider
		if val == "" {
			// Enter = behold nuværende provider
			if w.needsURL || w.baseURL != "" {
				w.step = 1
				cur := w.baseURL
				if cur == "" {
					cur = "ikke sat"
				}
				return []Event{{Type: EventSystem, Content: fmt.Sprintf("URL til server (aktuel: %s — Enter for at beholde):\n%s", cur, wizardHint)}}
			}
			w.step = 2
			cur := w.model
			if cur == "" {
				cur = "ikke sat"
			}
			return []Event{{Type: EventSystem, Content: fmt.Sprintf("Modelnavn (aktuel: %s — Enter for at beholde):\n%s", cur, wizardHint)}}
		}
		switch low {
		case "1", "anthropic":
			w.provider = "anthropic"
			w.needsURL = false
			w.baseURL = ""
		case "2", "openai":
			w.provider = "openai"
			w.needsURL = false
		case "3", "ollama":
			w.provider = "openai"
			w.needsURL = true
			if w.baseURL == "" {
				w.baseURL = "http://localhost:11434/v1"
			}
			w.step = 1
			return []Event{{Type: EventSystem, Content: fmt.Sprintf("URL til Ollama-server (Enter = %s):\n%s", w.baseURL, wizardHint)}}
		case "4", "lmstudio":
			w.provider = "openai"
			w.needsURL = true
			if w.baseURL == "" {
				w.baseURL = "http://localhost:1234/v1"
			}
			w.step = 1
			return []Event{{Type: EventSystem, Content: fmt.Sprintf("URL til LM Studio (Enter = %s):\n%s", w.baseURL, wizardHint)}}
		default:
			return []Event{{Type: EventSystem, Content: "Skriv 1-4 eller providernavn (anthropic/openai/ollama/lmstudio):\n" + wizardHint}}
		}
		w.step = 2
		cur := w.model
		if cur == "" {
			cur = "ikke sat"
		}
		return []Event{{Type: EventSystem, Content: fmt.Sprintf("Modelnavn (aktuel: %s — Enter for at beholde):\n%s", cur, wizardHint)}}

	case 1: // baseURL
		if val != "" {
			if err := validateBaseURL(val); err != nil {
				return []Event{{Type: EventSystem, Content: "Ugyldig URL: " + err.Error() + "\nForsøg igen (fx http://localhost:1234/v1):\n" + wizardHint}}
			}
			w.baseURL = val
		}
		// val=="" → behold eksisterende w.baseURL
		w.step = 2
		cur := w.model
		if cur == "" {
			cur = "ikke sat"
		}
		return []Event{{Type: EventSystem, Content: fmt.Sprintf("Modelnavn (aktuel: %s — Enter for at beholde):\n%s", cur, wizardHint)}}

	case 2: // modelnavn
		if val != "" {
			if err := provider.ValidateModelName(val); err != nil {
				return []Event{{Type: EventSystem, Content: "Ugyldigt modelnavn: " + err.Error() + "\n" + wizardHint}}
			}
			w.model = val
		}
		// val=="" → behold eksisterende w.model
		w.step = 3
		cur := "(model-default)"
		if w.contextSize > 0 {
			cur = fmt.Sprintf("%d tokens", w.contextSize)
		}
		return []Event{{Type: EventSystem, Content: fmt.Sprintf("Kontekststørrelse i tokens (aktuel: %s — Enter for at beholde):\n%s", cur, wizardHint)}}

	case 3: // kontekststørrelse
		if val != "" {
			n := 0
			if _, err := fmt.Sscanf(val, "%d", &n); err != nil || n < 1000 || n > 2_000_000 {
				return []Event{{Type: EventSystem, Content: "Ugyldigt tal — angiv 1000–2000000 eller Enter for at beholde:\n" + wizardHint}}
			}
			w.contextSize = n
		}
		// val=="" → behold eksisterende w.contextSize
		w.step = 4
		urlLine := ""
		if w.baseURL != "" {
			urlLine = fmt.Sprintf("  URL:         %s\n", w.baseURL)
		}
		ctxLine := "  Kontekst:    (model-default)\n"
		if w.contextSize > 0 {
			ctxLine = fmt.Sprintf("  Kontekst:    %d tokens\n", w.contextSize)
		}
		privateWarn := ""
		if baseURLIsPrivate(w.baseURL) {
			privateWarn = "\n⚠ URL peger på lokal/privat adresse — 'j' gemmer permanent samtykke for præcis denne URL."
		}
		return []Event{{Type: EventSystem, Content: fmt.Sprintf(
			"Gem denne konfiguration?\n\n  Provider:    %s\n%s  Model:       %s\n%s\nj = gem · n/annuller = afbryd%s",
			w.provider, urlLine, w.model, ctxLine, privateWarn)}}

	case 4: // bekræft
		if low == "j" || low == "ja" || low == "y" || low == "yes" {
			return a.applyModelConfig()
		}
		a.modelWizard = nil
		return []Event{{Type: EventSystem, Content: "Annulleret — ingen ændringer gemt."}}
	}
	return nil
}

func validateBaseURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("URL må ikke være tom")
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return fmt.Errorf("URL skal starte med http:// eller https://")
	}
	if _, err := url.Parse(raw); err != nil {
		return fmt.Errorf("ugyldig URL: %w", err)
	}
	// Private/loopback-URL'er blokeres ikke længere her: bekræftelsestrinnet
	// viser en ⚠-advarsel, og 'j' gemmer samtykke via GrantLocalURL
	// (internal/consent). Runtime-håndhævelsen sker i provider-lagets
	// DialContext-tjek, som kun åbnes af netop dét samtykke eller env-varen.
	return nil
}

// baseURLIsPrivate returnerer true hvis URL peger på en lokal/privat IP.
// Bruges til at vise en advarsel i wizarden — lokale endpoints som LM Studio
// er legitime og blokeres ikke, men brugeren informeres.
func baseURLIsPrivate(raw string) bool {
	u, err := url.Parse(raw)
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

func (a *Agent) applyModelConfig() []Event {
	w := a.modelWizard
	if w == nil {
		return []Event{{Type: EventSystem, Content: "Ingen wizard aktiv."}}
	}
	a.modelWizard = nil

	if a.cfg.OnProviderReload == nil {
		return []Event{{Type: EventError, Content: "Kan ikke skifte provider: ingen reload-callback konfigureret."}}
	}

	// Bestem skrivesti: lokal foretrækkes
	targetPath := a.cfg.GlobalConfigPath
	if a.cfg.LocalConfigPath != "" {
		if _, err := os.Stat(a.cfg.LocalConfigPath); err == nil {
			targetPath = a.cfg.LocalConfigPath
		}
	}
	if targetPath == "" {
		return []Event{{Type: EventError, Content: "Ingen config-sti konfigureret — kan ikke gemme."}}
	}

	// Brugeren har netop bekræftet konfigurationen med 'j' — og blev advaret
	// hvis URL'en er privat. Gem samtykket (globalt, via cmd/ekte's callback)
	// inden reload, ellers afviser reload-valideringen den nye URL.
	if w.baseURL != "" && baseURLIsPrivate(w.baseURL) && a.cfg.GrantLocalURL != nil {
		if err := a.cfg.GrantLocalURL(w.baseURL); err != nil {
			return []Event{{Type: EventError, Content: "Kunne ikke gemme samtykke til lokal provider: " + err.Error()}}
		}
	}

	if w.provider != "" || w.model != "" {
		if err := provider.UpdateProviderConfig(targetPath, w.provider, w.model, w.baseURL); err != nil {
			return []Event{{Type: EventError, Content: "Fejl ved gem af provider-config: " + err.Error()}}
		}
	}
	if w.contextSize > 0 {
		if err := provider.UpdateContextSize(targetPath, w.contextSize); err != nil {
			return []Event{{Type: EventError, Content: "Fejl ved gem af kontekststørrelse: " + err.Error()}}
		}
	}

	newProv, provName, modelName, ctxSize, baseURL, err := a.cfg.OnProviderReload()
	if err != nil {
		return []Event{{Type: EventError, Content: "Config gemt, men reload fejlede: " + err.Error() + "\nGenstart ekte for at aktivere ændringerne."}}
	}
	a.cfg.Provider = newProv
	a.cfg.ProviderName = provName
	a.cfg.ModelName = modelName
	a.cfg.ContextSize = ctxSize
	a.cfg.BaseURL = baseURL

	urlPart := ""
	if baseURL != "" {
		urlPart = " via " + baseURL
	}
	return []Event{
		{Type: EventSystem, Content: fmt.Sprintf("✓ Provider skiftet til %s / %s%s — aktiv nu. Gemt i: %s", provName, modelName, urlPart, targetPath)},
		{Type: EventTokenCount, Tokens: a.tokenCount},
	}
}
