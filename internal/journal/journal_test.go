package journal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r1 := Record{
		Goal: "byg login", Criteria: []string{"bruger kan logge ind"},
		Outcome: OutcomeAccepted, Iterations: 2, HumanLabel: LabelAccepted,
		Verdicts: []VerdictLite{{Sensor: "intent", Pass: true, Severity: "low"}},
		Lesson:   "Genbrug eksisterende session-middleware.",
	}
	r2 := Record{Goal: "tilføj export", Outcome: OutcomeBackstop, Iterations: 3}

	if err := Append(dir, r1); err != nil {
		t.Fatalf("append r1: %v", err)
	}
	if err := Append(dir, r2); err != nil {
		t.Fatalf("append r2: %v", err)
	}

	recs, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("forventede 2 poster, fik %d", len(recs))
	}
	if recs[0].Goal != "byg login" || recs[0].Outcome != OutcomeAccepted || len(recs[0].Verdicts) != 1 {
		t.Errorf("post 1 forkert: %+v", recs[0])
	}
	if recs[1].Outcome != OutcomeBackstop {
		t.Errorf("post 2 forkert: %+v", recs[1])
	}
	if recs[0].Time.IsZero() {
		t.Error("Append burde sætte Time når den er nul")
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	recs, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("manglende journal bør ikke være en fejl: %v", err)
	}
	if recs != nil {
		t.Errorf("forventede nil, fik %v", recs)
	}
}

func TestLoadSkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, Record{Goal: "ok", Outcome: OutcomeAccepted}); err != nil {
		t.Fatal(err)
	}
	// Indsæt en korrupt linje manuelt.
	f, _ := os.OpenFile(filepath.Join(dir, fileName), os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("{ikke gyldig json\n")
	f.Close()
	if err := Append(dir, Record{Goal: "ok2", Outcome: OutcomeMaxIter}); err != nil {
		t.Fatal(err)
	}

	recs, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("korrupt linje skulle springes over → 2 gyldige, fik %d", len(recs))
	}
}
