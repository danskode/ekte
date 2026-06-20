package sensor

import (
	"context"
	"errors"
	"testing"

	"github.com/danskode/ekte/internal/provider"
)

// fakeProvider returnerer et fast Chat-svar — nok til at drive sensorerne uden
// netværk eller en rigtig model.
type fakeProvider struct {
	resp  string
	err   error
	calls int
}

func (f *fakeProvider) Chat(ctx context.Context, msgs []provider.Message) (*provider.Response, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &provider.Response{Content: f.resp}, nil
}
func (f *fakeProvider) ChatWithTools(ctx context.Context, msgs []provider.Message, _ []provider.ToolDefinition) (*provider.Response, error) {
	return f.Chat(ctx, msgs)
}
func (f *fakeProvider) Stream(context.Context, []provider.Message) (<-chan string, error) {
	return nil, nil
}
func (f *fakeProvider) StreamWithTools(context.Context, []provider.Message, []provider.ToolDefinition) (<-chan provider.StreamEvent, error) {
	return nil, nil
}
func (f *fakeProvider) Name() string { return "fake" }

func TestIntentSensorPass(t *testing.T) {
	p := &fakeProvider{resp: `{"conformance":"pass","critique":"opfylder alle kriterier","unmet":[]}`}
	v, err := IntentSensor{P: p}.Check(context.Background(), Input{
		Goal: "tilføj login", Criteria: []string{"bruger kan logge ind"}, Diff: "+func Login() {}",
	})
	if err != nil {
		t.Fatalf("uventet fejl: %v", err)
	}
	if !v.Pass || v.NeedsClarification {
		t.Errorf("forventede pass uden afklaring, fik %+v", v)
	}
}

func TestIntentSensorFail(t *testing.T) {
	p := &fakeProvider{resp: `{"conformance":"fail","critique":"mangler validering","unmet":["input valideres"]}`}
	v, _ := IntentSensor{P: p}.Check(context.Background(), Input{
		Goal: "x", Criteria: []string{"input valideres"}, Diff: "+code",
	})
	if v.Pass {
		t.Error("forventede ikke-pass ved fail")
	}
	if len(v.Findings) != 1 {
		t.Errorf("forventede 1 finding, fik %d", len(v.Findings))
	}
}

func TestIntentSensorUnclear(t *testing.T) {
	p := &fakeProvider{resp: `{"conformance":"unclear","clarify_question":"hvad menes med hurtig?"}`}
	v, _ := IntentSensor{P: p}.Check(context.Background(), Input{
		Goal: "gør det hurtigt", Criteria: []string{"hurtigt"}, Diff: "+code",
	})
	if v.Pass || !v.NeedsClarification {
		t.Errorf("forventede afklaring, fik %+v", v)
	}
	if v.ClarifyQuestion == "" {
		t.Error("forventede et afklarende spørgsmål")
	}
}

func TestIntentSensorNoCriteriaNeedsClarificationWithoutCall(t *testing.T) {
	p := &fakeProvider{resp: `{"conformance":"pass"}`}
	v, _ := IntentSensor{P: p}.Check(context.Background(), Input{Goal: "x", Diff: "+code"})
	if !v.NeedsClarification {
		t.Error("uden kriterier forventes afklaring")
	}
	if p.calls != 0 {
		t.Errorf("modellen bør ikke kaldes uden kriterier, men blev kaldt %d gange", p.calls)
	}
}

func TestIntentSensorUnparseableFailsClosed(t *testing.T) {
	p := &fakeProvider{resp: "det her er ikke JSON"}
	v, _ := IntentSensor{P: p}.Check(context.Background(), Input{
		Goal: "x", Criteria: []string{"y"}, Diff: "+code",
	})
	if v.Pass || v.NeedsClarification {
		t.Errorf("ufortolkeligt svar skal fejle lukket uden afklaring, fik %+v", v)
	}
}

func TestIntentSensorProviderError(t *testing.T) {
	p := &fakeProvider{err: errors.New("netværk nede")}
	_, err := IntentSensor{P: p}.Check(context.Background(), Input{
		Goal: "x", Criteria: []string{"y"}, Diff: "+code",
	})
	if err == nil {
		t.Error("transport-fejl skal returneres som error")
	}
}

func TestSecuritySensorBlocksOnMedium(t *testing.T) {
	p := &fakeProvider{resp: `{"risk_level":"medium","summary":"sårbar","findings":[{"severity":"medium","file":"a.go","issue":"SQL injection","recommendation":"brug parametre"}]}`}
	v, err := SecuritySensor{P: p}.Check(context.Background(), Input{Diff: "+code"})
	if err != nil {
		t.Fatalf("uventet fejl: %v", err)
	}
	if v.Pass {
		t.Error("medium-fund skal blokere")
	}
	if len(v.Findings) != 1 {
		t.Errorf("forventede 1 finding, fik %d", len(v.Findings))
	}
}

func TestSecuritySensorPassesOnLow(t *testing.T) {
	p := &fakeProvider{resp: `{"risk_level":"low","summary":"ok","findings":[]}`}
	v, _ := SecuritySensor{P: p}.Check(context.Background(), Input{Diff: "+code"})
	if !v.Pass {
		t.Error("low uden fund skal passe")
	}
}

func TestSecuritySensorFailsClosedOnGarbage(t *testing.T) {
	p := &fakeProvider{resp: "ikke json"}
	v, _ := SecuritySensor{P: p}.Check(context.Background(), Input{Diff: "+code"})
	if v.Pass {
		t.Error("ufortolkeligt review skal fejle lukket (blokere)")
	}
}

func TestSecuritySensorEmptyDiffPasses(t *testing.T) {
	p := &fakeProvider{resp: "(bør ikke kaldes)"}
	v, _ := SecuritySensor{P: p}.Check(context.Background(), Input{Diff: "   "})
	if !v.Pass || p.calls != 0 {
		t.Errorf("tom diff skal passe uden modelkald, fik pass=%v calls=%d", v.Pass, p.calls)
	}
}

func TestAggregate(t *testing.T) {
	vs := []Verdict{
		{Sensor: "sikkerhed", Pass: true, Severity: "low"},
		{Sensor: "intent", Pass: false, Severity: "medium", NeedsClarification: true, ClarifyQuestion: "q"},
	}
	s := Aggregate(vs)
	if s.Pass {
		t.Error("én ikke-pass skal give samlet ikke-pass")
	}
	if !s.NeedsClarification || s.ClarifyQuestion != "q" {
		t.Error("afklaring skal propagere til summary")
	}
	if s.WorstSeverity != "medium" {
		t.Errorf("worst severity forkert: %s", s.WorstSeverity)
	}
}
