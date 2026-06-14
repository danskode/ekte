package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/danskode/ekte/internal/provider"
)

// routingStub svarer forskelligt afhængigt af system-promptens fase.
type routingStub struct{ scoreReply string }

func (s routingStub) Chat(ctx context.Context, m []provider.Message) (*provider.Response, error) {
	sys := ""
	if len(m) > 0 {
		sys = m[0].Content
	}
	switch {
	case strings.Contains(sys, "Nedbryd"):
		return &provider.Response{Content: `[{"title":"A","brief":"do a","specialty":"backend"},{"title":"B","brief":"do b","specialty":"tests"}]`}, nil
	case strings.Contains(sys, "kvalitetssikrer"):
		return &provider.Response{Content: s.scoreReply}, nil
	case strings.Contains(sys, "Saml de godkendte"):
		return &provider.Response{Content: "SAMLET LØSNING"}, nil
	default:
		return &provider.Response{Content: "subagent output"}, nil
	}
}
func (s routingStub) ChatWithTools(ctx context.Context, m []provider.Message, t []provider.ToolDefinition) (*provider.Response, error) {
	return s.Chat(ctx, m)
}
func (s routingStub) Stream(ctx context.Context, m []provider.Message) (<-chan string, error) {
	return nil, nil
}
func (s routingStub) StreamWithTools(ctx context.Context, m []provider.Message, t []provider.ToolDefinition) (<-chan provider.StreamEvent, error) {
	return nil, nil
}
func (s routingStub) Name() string { return "stub" }

func TestRunAccepts(t *testing.T) {
	o := New(routingStub{`{"score":90,"critique":"fint"}`}, Options{}, nil)
	sol, err := o.Run(context.Background(), "byg en feature")
	if err != nil {
		t.Fatal(err)
	}
	if len(sol.SubResults) != 2 {
		t.Fatalf("forventede 2 delresultater, fik %d", len(sol.SubResults))
	}
	for _, r := range sol.SubResults {
		if !r.Accepted || r.Iterations != 1 {
			t.Errorf("score 90 burde accepteres i runde 1: %+v", r)
		}
	}
	if sol.Assembled != "SAMLET LØSNING" {
		t.Errorf("assembled = %q", sol.Assembled)
	}
}

func TestRunIteratesWhenLowScore(t *testing.T) {
	o := New(routingStub{`{"score":40,"critique":"mangler"}`}, Options{MaxIterations: 3, AcceptScore: 80}, nil)
	sol, err := o.Run(context.Background(), "byg")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range sol.SubResults {
		if r.Accepted || r.Iterations != 3 {
			t.Errorf("lav score burde iterere til max (3) uden accept: %+v", r)
		}
	}
}

func TestRunEmptySpec(t *testing.T) {
	o := New(routingStub{`{"score":90}`}, Options{}, nil)
	if _, err := o.Run(context.Background(), "   "); err == nil {
		t.Error("tom spec burde give fejl")
	}
}
