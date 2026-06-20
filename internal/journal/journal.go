// Package journal er en append-only log over terminale /goal-udfald. Hver post er
// eval-case-klar (mål, kriterier, sensor-verdikter, udfald, menneske-label), så
// harnesset kompounderer på erfaring frem for at smide hver kørsels signal væk —
// og så en fremtidig eval-runner kan replaye journalen mod kriterierne.
//
// Journalen ligger i .ekte/memory/goals/ og auto-loades IKKE ind i kontekst
// (loadMemory springer undermapper og ikke-.md-filer over). Den er telemetri,
// ikke betroet kontekst — derfor JSONL frem for markdown.
package journal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// VerdictLite er et sensor-verdict reduceret til det journalen gemmer. Holder
// pakken uafhængig af internal/sensor (kalderen mapper sensor.Verdict → VerdictLite).
type VerdictLite struct {
	Sensor   string `json:"sensor"`
	Pass     bool   `json:"pass"`
	Severity string `json:"severity"`
	Critique string `json:"critique,omitempty"`
}

// Outcome-værdier for et terminalt /goal-udfald.
const (
	OutcomeAccepted    = "accepted"           // mennesket godkendte
	OutcomeRejected    = "rejected"           // mennesket afviste trods grønne sensorer
	OutcomeBackstop    = "backstop"           // evaluator underkendte gentagne gange
	OutcomeMaxIter     = "max_iterations"     // loopet udtømt uden succes
	OutcomeClarify     = "clarification"      // intent kunne ikke afgøres
	OutcomeVerifyError = "verification_error" // sensorerne kunne ikke gennemføres
)

// HumanLabel-værdier — rejected_despite_pass er højeste signal til judge-kalibrering.
const (
	LabelAccepted            = "accepted"
	LabelRejectedDespitePass = "rejected_despite_pass"
)

// Record er én terminal /goal-post.
type Record struct {
	Time       time.Time     `json:"time"`
	Goal       string        `json:"goal"`
	Criteria   []string      `json:"criteria,omitempty"`
	Outcome    string        `json:"outcome"`
	Iterations int           `json:"iterations"`
	Verdicts   []VerdictLite `json:"verdicts,omitempty"`
	HumanLabel string        `json:"human_label,omitempty"`
	Lesson     string        `json:"lesson,omitempty"`
}

const fileName = "journal.jsonl"

// Append tilføjer én post som en JSON-linje. Opretter dir hvis nødvendigt.
func Append(dir string, r Record) error {
	if r.Time.IsZero() {
		r.Time = time.Now()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, fileName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// Load læser alle poster tilbage. Korrupte/ufuldstændige linjer springes over,
// så én dårlig skrivning ikke gør hele journalen ulæselig.
func Load(dir string) ([]Record, error) {
	f, err := os.Open(filepath.Join(dir, fileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // store poster (verdikter/lektioner)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r Record
		if json.Unmarshal(line, &r) != nil {
			continue // spring korrupt linje over
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}
