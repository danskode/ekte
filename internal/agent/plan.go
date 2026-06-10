package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danskode/ekte/internal/provider"
)

const planModeSystemPrompt = `Du er i PLAN MODE — din rolle er Architect of Intent, ikke implementor.
Dit formål er at definere hvad der tæller som succes INDEN generation begynder.

Opgaver:
1. Eksternaliser brugerens transitive tilstande (det de ved men ikke har sagt eksplicit)
2. Kvalificér intent via Ahujas fem komponenter:
   - Description:       hvad ønskes præcist? (propositionelt indhold)
   - Constraints:       hvad skal være sandt? (forberedende betingelse)
   - Failure scenarios: hvad må IKKE ske? (negativ prop. indhold — gør abuse synlig)
   - Success scenarios: hvad tæller som done? (essentiel betingelse)
   - Connections:       hvad else kan påvirkes? (pragmatisk baggrundsviden)
3. Navngiv transitive tilstande eksplicit: "Jeg bemærker du antager X — er det en constraint?"
4. Brug misfire/abuse-distinktionen: abuse er den usynlige fejl — det der gennemføres plausibelt men matcher ikke intentionen. Gør den synlig via failure scenarios.

Regler:
- Skriv INGEN kode og udfør INGEN filoperationer
- Stil maksimalt ét spørgsmål ad gangen
- Afslut hvert svar med et kort resumé af hvad du allerede har forstået
- Brug brugerens egne AIDD-definitioner fra hukommelsen — ikke generiske begreber
- Forklar hvornår du mener intent er tilstrækkeligt kvalificeret til at starte implementering

Afslutning:
Når /plan godkend køres: opsummér planen, skriv den til .ekte/plans/ og bekræft klar til implementering.`

// enterPlanMode aktiverer plan mode uden startbeskrivelse — bruges af
// /mode plan og Shift+Tab. /plan <beskrivelse> starter desuden samtalen.
func (a *Agent) enterPlanMode() []Event {
	if a.planMode {
		return []Event{{Type: EventSystem, Content: "Plan mode er allerede aktiv.\nBrug: /plan godkend · /plan vis · /plan afvis"}}
	}
	a.planMode = true
	a.planFile = ""
	a.messages = append(a.messages, provider.Message{
		Role:    "system",
		Content: planModeSystemPrompt,
	})
	return []Event{{Type: EventSystem, Content: "📋 Plan mode — jeg er Architect of Intent og skriver ingen kode.\nBeskriv hvad du vil bygge; /plan godkend når planen er klar. (Shift+Tab skifter tilbage til develop)"}}
}

// exitPlanMode skifter tilbage til develop uden at gemme en plan.
func (a *Agent) exitPlanMode() []Event {
	if !a.planMode {
		return []Event{{Type: EventSystem, Content: "Develop mode er allerede aktiv."}}
	}
	a.planMode = false
	a.planFile = ""
	return []Event{{Type: EventSystem, Content: "✓ Develop mode — plan mode afsluttet uden gemt plan (brug /plan godkend næste gang for at gemme)."}}
}

// handlePlanGodkend kører bekræftelse via j/n/tab i chat-inputfeltet (EventToolConfirm).
// Blocking — kalder direkte på ch i stedet for at returnere []Event.
func (a *Agent) handlePlanGodkend(ctx context.Context, ch chan<- Event) {
	if !a.planMode {
		ch <- Event{Type: EventSystem, Content: "Ingen aktiv plan mode — start med /plan <beskrivelse>"}
		return
	}

	planContent := a.buildPlanSummary()
	ch <- Event{Type: EventSystem, Content: "📋 Plan klar til godkendelse:\n\n" + planContent + "\n"}

	confirmCh := make(chan ConfirmResponse, 1)
	ch <- Event{
		Type:      EventToolConfirm,
		Content:   "Godkend planen og start implementering? (j/n — Tab for at tilføje kommentar)",
		ConfirmCh: confirmCh,
	}

	resp := <-confirmCh
	if !resp.Approved {
		if resp.Redirect != "" {
			ch <- Event{Type: EventSystem, Content: "Revision tilføjet — fortsæt samtalen eller skriv /plan godkend igen når klar."}
			a.messages = append(a.messages, provider.Message{
				Role:    "user",
				Content: "[Revision af plan]: " + resp.Redirect,
			})
			a.streamChat(ctx, resp.Redirect, ch)
		} else {
			ch <- Event{Type: EventSystem, Content: "Plan ikke godkendt — fortsæt samtalen eller brug /plan afvis."}
		}
		return
	}

	planPath, err := a.savePlanFile(planContent)
	if err != nil {
		ch <- Event{Type: EventError, Content: "Fejl ved gem af plan: " + err.Error()}
		return
	}
	a.planMode = false
	a.messages = append(a.messages, provider.Message{
		Role:    "system",
		Content: "[Plan godkendt — implementering kan starte]\n" + planContent,
	})
	ch <- Event{Type: EventSystem, Content: fmt.Sprintf("✓ Plan godkendt og gemt: %s\nPlan mode afsluttet — implementering kan starte.", planPath)}
	ch <- Event{Type: EventSystem, Content: "Vil du sætte et succeskriterie for opgaven? Skriv '/goal <hook-navn>' eller fortsæt manuelt."}
}

func (a *Agent) handlePlan(ctx context.Context, arg string) []Event {
	subCmd := strings.ToLower(strings.TrimSpace(arg))

	switch {
	case subCmd == "vis" || subCmd == "show":
		if !a.planMode {
			return []Event{{Type: EventSystem, Content: "Ingen aktiv plan mode."}}
		}
		if a.planFile != "" {
			data, err := os.ReadFile(a.planFile)
			if err == nil {
				return []Event{{Type: EventSystem, Content: string(data)}}
			}
		}
		return []Event{{Type: EventSystem, Content: "Ingen plan-fil gemt endnu — samtalen er i gang."}}

	case subCmd == "afvis" || subCmd == "discard":
		if !a.planMode {
			return []Event{{Type: EventSystem, Content: "Ingen aktiv plan mode."}}
		}
		a.planMode = false
		a.planFile = ""
		return []Event{{Type: EventSystem, Content: "Plan forkastet. Plan mode afsluttet."}}

	case subCmd == "":
		if a.planMode {
			return []Event{{Type: EventSystem, Content: "Plan mode er allerede aktiv.\nBrug: /plan godkend · /plan vis · /plan afvis"}}
		}
		return []Event{{Type: EventSystem, Content: "Brug: /plan <beskrivelse af hvad du vil bygge>"}}

	default:
		// /plan <beskrivelse> — aktiver plan mode og start med at stille spørgsmål
		if a.planMode {
			// Allerede i plan mode — tilføj som ekstra kontekst
			a.messages = append(a.messages, provider.Message{
				Role:    "user",
				Content: "[Tilføjet kontekst til plan]: " + arg,
			})
		} else {
			a.planMode = true
			a.planFile = ""
			// Injicér plan mode systempromt
			a.messages = append(a.messages, provider.Message{
				Role:    "system",
				Content: planModeSystemPrompt,
			})
		}
		// Kør en streaming chat med plan-konteksten
		bufCh := make(chan Event, 256)
		go func() {
			a.streamChat(ctx, arg, bufCh)
			close(bufCh)
		}()
		var evs []Event
		for ev := range bufCh {
			evs = append(evs, ev)
		}
		return append([]Event{{Type: EventSystem, Content: "📋 Plan mode aktiv — jeg er nu Architect of Intent. Brug /plan godkend når vi er klar.\n"}}, evs...)
	}
}

func (a *Agent) buildPlanSummary() string {
	// Saml plan-indhold fra samtalehistorikken
	var sb strings.Builder
	sb.WriteString("# Plan\n\n")
	sb.WriteString("*Genereret af ekte plan mode*\n\n")

	// Find plan mode beskeder fra historikken
	inPlan := false
	for _, m := range a.messages {
		if m.Role == "system" && strings.Contains(m.Content, "PLAN MODE") {
			inPlan = true
			continue
		}
		if !inPlan {
			continue
		}
		if m.Role == "assistant" {
			sb.WriteString("## Agent\n\n")
			sb.WriteString(m.Content + "\n\n")
		} else if m.Role == "user" && !strings.HasPrefix(m.Content, "[") {
			sb.WriteString("## Bruger\n\n")
			sb.WriteString(m.Content + "\n\n")
		}
	}
	return sb.String()
}

func (a *Agent) savePlanFile(content string) (string, error) {
	// Bestem plan-mappe: projekt-lokal foretrækkes
	planDir := filepath.Join(".ekte", "plans")
	if a.cfg.WorkDirForMemory != "" {
		planDir = filepath.Join(a.cfg.WorkDirForMemory, ".ekte", "plans")
	}
	if err := os.MkdirAll(planDir, 0700); err != nil {
		return "", err
	}
	slug := time.Now().Format("20060102-150405")
	path := filepath.Join(planDir, slug+".md")
	if err := os.WriteFile(path, []byte(sanitizeFileContent(content)), 0600); err != nil {
		return "", err
	}
	a.planFile = path
	return path, nil
}
