package review

import (
	"context"
	"testing"

	"github.com/danskode/ekte/internal/provider"
)

type stubProvider struct{ reply string }

func (s stubProvider) Chat(ctx context.Context, m []provider.Message) (*provider.Response, error) {
	return &provider.Response{Content: s.reply}, nil
}
func (s stubProvider) ChatWithTools(ctx context.Context, m []provider.Message, t []provider.ToolDefinition) (*provider.Response, error) {
	return &provider.Response{Content: s.reply}, nil
}
func (s stubProvider) Stream(ctx context.Context, m []provider.Message) (<-chan string, error) {
	return nil, nil
}
func (s stubProvider) StreamWithTools(ctx context.Context, m []provider.Message, t []provider.ToolDefinition) (<-chan provider.StreamEvent, error) {
	return nil, nil
}
func (s stubProvider) Name() string { return "stub" }

func TestRunParsesFencedJSON(t *testing.T) {
	reply := "```json\n{\"risk_level\":\"medium\",\"summary\":\"x\",\"findings\":[{\"severity\":\"medium\",\"file\":\"a.go\",\"issue\":\"i\",\"recommendation\":\"r\"}]}\n```"
	r, _, err := Run(context.Background(), stubProvider{reply}, "diff", "test")
	if err != nil {
		t.Fatal(err)
	}
	if r.RiskLevel != "medium" || len(r.Findings) != 1 {
		t.Errorf("uventet resultat: %+v", r)
	}
	if !r.Blocking() {
		t.Error("medium burde blokere")
	}
}

func TestRunEmptyDiff(t *testing.T) {
	r, _, err := Run(context.Background(), stubProvider{""}, "", "test")
	if err != nil || r.Blocking() {
		t.Errorf("tom diff burde være low/non-blocking: %+v %v", r, err)
	}
}

func TestEffectiveRiskEscalates(t *testing.T) {
	r := &Result{RiskLevel: "low", Findings: []Finding{{Severity: "high"}}}
	if r.EffectiveRisk() != "high" || !r.Blocking() {
		t.Errorf("low risk_level + high finding burde eskalere/blokere; fik %s blocking=%v", r.EffectiveRisk(), r.Blocking())
	}
}

func TestRunRejectsInvalidRiskLevel(t *testing.T) {
	if _, _, err := Run(context.Background(), stubProvider{`{"risk_level":"banana","findings":[]}`}, "diff", "t"); err == nil {
		t.Error("ugyldigt risk_level burde give fejl (må ikke stilles lig low)")
	}
}

func TestRunParseErrorReturnsRaw(t *testing.T) {
	_, raw, err := Run(context.Background(), stubProvider{"ikke json"}, "diff", "test")
	if err == nil {
		t.Error("forventede parse-fejl")
	}
	if raw != "ikke json" {
		t.Errorf("rå svar skulle returneres, fik %q", raw)
	}
}
