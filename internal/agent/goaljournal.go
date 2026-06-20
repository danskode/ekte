package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danskode/ekte/internal/journal"
	"github.com/danskode/ekte/internal/provider"
	"github.com/danskode/ekte/internal/sensor"
)

// toVerdictLites mapper sensor-verdikter til journalens uafhængige form.
func toVerdictLites(vs []sensor.Verdict) []journal.VerdictLite {
	var out []journal.VerdictLite
	for _, v := range vs {
		out = append(out, journal.VerdictLite{
			Sensor: v.Sensor, Pass: v.Pass, Severity: v.Severity, Critique: v.Critique,
		})
	}
	return out
}

// recordGoal bygger en journal.Record for et terminalt /goal-udfald og opsamler den.
func (a *Agent) recordGoal(ctx context.Context, ch chan<- Event, goalDesc, outcome, humanLabel string, iterations int, verdicts []sensor.Verdict) {
	a.captureGoalOutcome(ctx, journal.Record{
		Goal:       goalDesc,
		Criteria:   a.cfg.Goal.SuccessCriteria,
		Outcome:    outcome,
		HumanLabel: humanLabel,
		Iterations: iterations,
		Verdicts:   toVerdictLites(verdicts),
	}, ch)
}

// maxStoredLessons begrænser hvor mange lektioner goal-lessons.md beholder, så
// den loadede memory ikke vokser uafgrænset (mod context-anxiety). Fuld historik
// bevares i journalen.
const maxStoredLessons = 15

// captureEnabled afgør om automatisk opsamling kører: kræver harness_write
// (skrivning af harness-filer) og at goal.capture ikke er slået fra.
func (a *Agent) captureEnabled() bool {
	if !a.cfg.Whitelist.HarnessWrite {
		return false
	}
	return a.cfg.Goal.Capture == nil || *a.cfg.Goal.Capture
}

// memoryBaseDir er .ekte/memory/ (projekt-lokalt), samme rod som /remember bruger.
func (a *Agent) memoryBaseDir() string {
	if a.cfg.WorkDirForMemory != "" {
		return filepath.Join(a.cfg.WorkDirForMemory, ".ekte", "memory")
	}
	return filepath.Join(".ekte", "memory")
}

// captureGoalOutcome destillerer en genbrugelig lektion fra et terminalt /goal-
// udfald, appender en struktureret (eval-case-klar) post til den IKKE-loadede
// journal, og tilbyder at promovere lektionen til det loadede memory. Journalen
// skrives stille; memory-promoveringen kræver bekræftelse.
func (a *Agent) captureGoalOutcome(ctx context.Context, rec journal.Record, ch chan<- Event) {
	if !a.captureEnabled() {
		return
	}
	base := a.memoryBaseDir()
	rec.Time = time.Now()
	rec.Lesson = a.distillLesson(ctx, rec)

	// 1) Stille append til journalen (telemetri, ikke betroet kontekst).
	if err := journal.Append(filepath.Join(base, "goals"), rec); err != nil {
		a.log().Warn("kunne ikke skrive goal-journal", "error", err)
	} else {
		ch <- Event{Type: EventSystem, Content: "🗒 Udfald journalført (" + rec.Outcome + ")."}
	}

	if strings.TrimSpace(rec.Lesson) == "" {
		return
	}

	// 2) Tilbyd promovering til loadet memory — kræver bekræftelse, fordi memory
	// loades som betroet kontekst i fremtidige sessioner (persisteret-injection-forsvar).
	confirmCh := make(chan ConfirmResponse, 1)
	ch <- Event{Type: EventToolConfirm,
		Content:   "Gem denne lektion i hukommelsen (loades i fremtidige sessioner)?\n\n" + rec.Lesson,
		ConfirmCh: confirmCh}
	select {
	case r := <-confirmCh:
		if !r.Approved {
			ch <- Event{Type: EventSystem, Content: "↩ Lektion kun journalført — ikke gemt i hukommelsen."}
			return
		}
	case <-ctx.Done():
		return
	}

	if err := a.appendGoalLesson(base, rec); err != nil {
		ch <- Event{Type: EventError, Content: "Kunne ikke gemme lektion: " + err.Error()}
		return
	}
	ch <- Event{Type: EventSystem, Content: "📝 Lektion gemt i goal-lessons.md — fremtidige sessioner starter med den."}
}

// distillLesson beder modellen om én konkret, genbrugelig lektion fra udfaldet.
func (a *Agent) distillLesson(ctx context.Context, rec journal.Record) string {
	if a.cfg.Provider == nil {
		return ""
	}
	var vb strings.Builder
	for _, v := range rec.Verdicts {
		mark := "bestået"
		if !v.Pass {
			mark = "underkendt"
		}
		fmt.Fprintf(&vb, "- %s: %s (%s) %s\n", v.Sensor, mark, v.Severity, v.Critique)
	}
	const sys = `Du destillerer ÉN genbrugelig lektion (1-3 sætninger på dansk) fra udfaldet af en autonom kodeopgave, så fremtidige opgaver i samme projekt kan undgå samme fejl eller genbruge samme tilgang. Skriv KONKRET og handlingsorienteret — ikke generelt. Ingen indledning, kun lektionen. Er der ingen meningsfuld lektion, svar med en tom streng.`
	user := fmt.Sprintf("Mål: %s\nUdfald: %s (efter %d iterationer)\nSucceskriterier: %s\nSensor-verdikter:\n%s",
		rec.Goal, rec.Outcome, rec.Iterations, strings.Join(rec.Criteria, "; "), vb.String())
	resp, err := a.cfg.Provider.Chat(ctx, []provider.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: user},
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(stripThinkTags(resp.Content))
}

// appendGoalLesson tilføjer lektionen til top-niveau goal-lessons.md (loadet som
// memory) og holder filen til de seneste maxStoredLessons poster.
func (a *Agent) appendGoalLesson(base string, rec journal.Record) error {
	if err := os.MkdirAll(base, 0700); err != nil {
		return err
	}
	path := filepath.Join(base, "goal-lessons.md")
	entry := fmt.Sprintf("## %s — %s [%s]\n%s",
		time.Now().Format("2006-01-02"), goalSlug(rec.Goal), rec.Outcome,
		sanitizeFileContent(rec.Lesson))

	var entries []string
	if data, err := os.ReadFile(path); err == nil {
		body := stripMemoryFrontmatter(string(data))
		for _, e := range strings.Split(body, "\n## ") {
			e = strings.TrimSpace(strings.TrimPrefix(e, "## "))
			if e != "" {
				entries = append(entries, "## "+e)
			}
		}
	}
	entries = append(entries, entry)
	if len(entries) > maxStoredLessons {
		entries = entries[len(entries)-maxStoredLessons:]
	}
	content := "---\ntype: memory\ntitle: goal-lektioner\n---\n\n" + strings.Join(entries, "\n\n") + "\n"
	return os.WriteFile(path, []byte(content), 0600)
}

// goalSlug laver en kort, fil-/visningsvenlig etiket af målbeskrivelsen.
func goalSlug(goal string) string {
	fields := strings.Fields(strings.ToLower(goal))
	if len(fields) > 6 {
		fields = fields[:6]
	}
	var b strings.Builder
	for _, f := range fields {
		for _, r := range f {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == 'æ' || r == 'ø' || r == 'å' {
				b.WriteRune(r)
			}
		}
		b.WriteByte('-')
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "mål"
	}
	return s
}

// stripMemoryFrontmatter fjerner en YAML-frontmatter (--- ... ---) fra toppen,
// så goal-lessons.md kan genskrives uden at duplikere headeren.
func stripMemoryFrontmatter(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return content
	}
	rest := strings.TrimPrefix(content, "---")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return content
	}
	return strings.TrimSpace(rest[idx+4:])
}
