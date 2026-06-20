package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danskode/ekte/internal/provider"
)

const planModeSystemPrompt = `Du er i PLAN MODE. Du er en hjælpsom AI-assistent i ekte, et developer harness.
Du hjælper brugeren med at eksternalisere transitive tilstande og kvalificere intent som en vellykket talehandling.

Rollefordeling:
- Brugeren er Architect of Intent — IKKE dig. Du hjælper brugeren med at blive en god Architect of Intent.
- Din rolle er AIDD-konsulent: du skaber betingelserne for at brugerens intent bliver præcis.
- Du hjælper med AIDD-tilgangen til naturligt sprog gennem ISL og ICE.
- AIDD er brugerens eget begreb, defineret i brugerens AI Engineering-opgave (dokumentet 00_samlet_del_1.md) — referér IKKE til wikiens definitioner af AIDD.

Kvalificér intent med ICE-strukturen, ét element ad gangen:
1. Intent — hvad skal resultatet opnå?
2. Context — hvilke teknologier og rammer er givne?
3. Expectations — hvad er vellykket output, og hvordan evalueres det?

Regler:
- Skriv INGEN kode og udfør INGEN filoperationer — i plan mode kan du kun læse og søge
- Stil maksimalt ét spørgsmål ad gangen
- Afslut hvert svar med et kort resumé af hvad du allerede har forstået
- Forklar hvornår du mener intent er tilstrækkeligt kvalificeret til at starte implementering

Afslutning:
Når /plan godkend køres: opsummér planen, skriv den til .ekte/plans/ og bekræft klar til implementering — implementeringen sker bagefter i develop mode.`

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
	return []Event{{Type: EventSystem, Content: "📋 Plan mode — jeg hjælper dig med at kvalificere din intent (ICE). Jeg kan kun læse og søge, ikke skrive.\nBeskriv hvad du vil bygge; /plan godkend når planen er klar. (Shift+Tab skifter tilbage til develop)"}}
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

	// Samme logning som fil-tools' confirm — så automation (og fejlsøgning)
	// kan se at der ventes på en bekræftelse.
	a.log().Info("tool confirm", "tool", "plan_godkend")
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

	// Destillér Expectations (ICE) til en maskinverificerbar rubrik — den tråd
	// der binder Definition (intent) til Verification (IntentSensor i sensor-loopet).
	criteria := a.extractSuccessCriteria(ctx, planContent)
	a.cfg.Goal.SuccessCriteria = criteria
	if len(criteria) > 0 {
		var b strings.Builder
		b.WriteString("\n\n## Succeskriterier (IntentSensor-rubrik)\n\n")
		for _, c := range criteria {
			b.WriteString("- " + c + "\n")
		}
		planContent += b.String()
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
	if len(criteria) > 0 {
		ch <- Event{Type: EventSystem, Content: fmt.Sprintf("🎯 %d succeskriterie(r) udtrukket som intent-rubrik — /goal og /verify måler imod dem.", len(criteria))}
	} else {
		ch <- Event{Type: EventSystem, Content: "⚠ Ingen klare succeskriterier kunne udtrækkes af planen — /goal vil bede om en præcisering. Kvalificér Expectations tydeligere i /plan."}
	}
	// Hjælp brugeren videre til udførelsesfasen — og opdag hvis der mangler et
	// goal/check_hook, så man ikke står stranded efter godkendelsen.
	if a.cfg.Goal.CheckHook != "" {
		if _, ok := a.cfg.Hooks[a.cfg.Goal.CheckHook]; ok {
			ch <- Event{Type: EventSystem, Content: fmt.Sprintf("Klar til autonom udførelse: skriv /goal <beskrivelse> — tjekkes med hooket '%s'.\nEller beskriv bare næste skridt, så bygger vi manuelt.", a.cfg.Goal.CheckHook)}
			return
		}
	}
	ch <- Event{Type: EventSystem, Content: "Vil du køre opgaven autonomt med /goal? Det kræver et succes-tjek (check_hook).\n" +
		"Sæt et hurtigt op:\n" +
		"  /hook add compile mvn -q compile     (eller: go build ./..., npm run build)\n" +
		"  /hook add goalcheck ekte springcheck (Java + Thymeleaf: tjekker sider/endpoints)\n" +
		"Tilføj så goal.check_hook i .ekte/config.yaml. Eller beskriv bare næste skridt, så bygger vi manuelt."}
}

// extractSuccessCriteria destillerer Expectations fra plan-samtalen til en kort
// liste konkrete, verificerbare succeskriterier — rubrikken IntentSensor måler
// diffen mod i sensor-loopet. Best-effort: fejler kaldet eller parse, returneres
// nil, og /goal vil bede om en præcisering frem for at gætte.
func (a *Agent) extractSuccessCriteria(ctx context.Context, planContent string) []string {
	if a.cfg.Provider == nil {
		return nil
	}
	const sys = `Du destillerer en plan til en kort liste KONKRETE, VERIFICERBARE succeskriterier (Expectations fra ICE). Hvert kriterium er en testbar tilstand der skal være sand for at opgaven tæller som løst — IKKE implementeringstrin eller hvordan. Returnér KUN valid JSON uden markdown: {"criteria": ["...", "..."]}. Maks 8 kriterier. Kan du ikke udlede klare kriterier, returnér {"criteria": []}.`
	resp, err := a.cfg.Provider.Chat(ctx, []provider.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: planContent},
	})
	if err != nil {
		return nil
	}
	clean := strings.TrimSpace(resp.Content)
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(clean, "```")
	var out struct {
		Criteria []string `json:"criteria"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(clean)), &out) != nil {
		return nil
	}
	var crit []string
	for _, c := range out.Criteria {
		if c = strings.TrimSpace(c); c != "" {
			crit = append(crit, c)
		}
		if len(crit) >= 8 {
			break
		}
	}
	return crit
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
