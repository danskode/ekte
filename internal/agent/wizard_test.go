package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestWizardEnterBeholder: tom Enter i model-wizarden betyder "behold nuværende
// værdi" (regression: tom input blev afvist FØR wizard-routingen i både
// Process og ProcessStream, så Enter aldrig nåede frem).
func TestWizardEnterBeholder(t *testing.T) {
	a := New(Config{
		ProviderName:     "openai",
		ModelName:        "test-model",
		ContextSize:      8000,
		GlobalConfigPath: filepath.Join(t.TempDir(), "config.yaml"),
		OnProviderReload: func() (*ReloadResult, error) {
			return &ReloadResult{ProviderName: "openai", ModelName: "test-model", ContextSize: 16000}, nil
		},
	})
	ctx := context.Background()

	a.Process(ctx, "/model setup")
	if !a.InWizard() {
		t.Fatal("wizard burde være aktiv efter /model setup")
	}

	// Enter × 3: behold provider → behold model → behold kontekst → bekræftelse
	a.Process(ctx, "")
	a.Process(ctx, "")
	evs := a.Process(ctx, "")
	if len(evs) == 0 || !strings.Contains(evs[len(evs)-1].Content, "Gem denne konfiguration") {
		t.Fatalf("forventede bekræftelsestrin efter 3× Enter, fik: %+v", evs)
	}

	// 'j' gemmer og skal udsende EventModelInfo med ny kontekststørrelse,
	// så TUI'ens statuslinje opdateres uden genstart.
	evs = a.Process(ctx, "j")
	var info *Event
	for i := range evs {
		if evs[i].Type == EventModelInfo {
			info = &evs[i]
		}
	}
	if info == nil {
		t.Fatal("EventModelInfo ikke udsendt ved gem af konfiguration")
	}
	if info.Tokens != 16000 {
		t.Errorf("EventModelInfo.Tokens = %d, forventet 16000 (fra reload)", info.Tokens)
	}
	if info.Content != "test-model" {
		t.Errorf("EventModelInfo.Content = %q, forventet modelnavn", info.Content)
	}
	if a.InWizard() {
		t.Error("wizard burde være afsluttet efter gem")
	}
}

// TestWizardEnterViaStream: samme garanti for streaming-stien (TUI'ens vej).
func TestWizardEnterViaStream(t *testing.T) {
	a := New(Config{ProviderName: "openai", ModelName: "m"})
	a.Process(context.Background(), "/model setup")

	ch := a.ProcessStream(context.Background(), "")
	var got []Event
	for ev := range ch {
		got = append(got, ev)
	}
	if len(got) == 0 {
		t.Fatal("tom Enter via ProcessStream gav ingen events — wizard-routing mangler før tom-tjek")
	}
	a.Process(context.Background(), "annuller")
}

func TestWizardAnnuller(t *testing.T) {
	a := New(Config{})
	a.Process(context.Background(), "/model setup")
	a.Process(context.Background(), "annuller")
	if a.InWizard() {
		t.Error("'annuller' burde afslutte wizarden")
	}
}
