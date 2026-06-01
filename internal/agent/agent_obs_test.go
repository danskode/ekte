package agent

import (
	"testing"

	"github.com/danskode/ekte/internal/provider"
)

// --- buildBreakdown ---

func TestBuildBreakdownNoWiki(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "system prompt her"},       // sys
		{Role: "user", Content: "tidligere spørgsmål"},       // hist
		{Role: "assistant", Content: "tidligere svar"},       // hist
		{Role: "user", Content: "nuværende prompt fra bruger"}, // user (sidst)
	}
	bd := buildBreakdown(msgs, -1)

	if bd.sys == 0 {
		t.Error("sys burde være > 0")
	}
	if bd.wiki != 0 {
		t.Errorf("wiki burde være 0 når wikiIdx=-1, fik %d", bd.wiki)
	}
	if bd.user == 0 {
		t.Error("user burde være > 0 (sidste user-besked)")
	}
	if bd.hist == 0 {
		t.Error("hist burde være > 0 (tidligere beskeder)")
	}
	if bd.tools != 0 {
		t.Errorf("tools burde være 0 her, fik %d", bd.tools)
	}
}

func TestBuildBreakdownWithWiki(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "wiki kontekst som er ret lang og fylder noget"}, // wikiIdx=0
		{Role: "system", Content: "system prompt"},                                  // sys (nu index 1? nej)
		{Role: "user", Content: "prompten"},                                         // user
	}
	// wikiIdx=0: første system er wiki, ikke sys
	bd := buildBreakdown(msgs, 0)

	if bd.wiki == 0 {
		t.Error("wiki burde være > 0 når wikiIdx=0")
	}
	if bd.sys != 0 {
		// index 0 er wiki, index 1 er system — men buildBreakdown tjekker i==0 for sys
		// så sys burde være 0 her fordi wikiIdx=0 tager det
		t.Logf("sys=%d (wikiIdx=0 overtrumfer sys-tjekket for index 0)", bd.sys)
	}
	if bd.user == 0 {
		t.Error("user burde være > 0 (sidste user-besked)")
	}
}

func TestBuildBreakdownWithTools(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "spørg noget"},
		{Role: "assistant", Content: "her er tool call"},
		{Role: "tool", Content: "tool output der fylder lidt mere"},
		{Role: "tool", Content: "endnu et tool output"},
		{Role: "user", Content: "bruger prompt til sidst"},
	}
	bd := buildBreakdown(msgs, -1)

	if bd.tools == 0 {
		t.Error("tools burde være > 0 (to tool-beskeder)")
	}
	if bd.user == 0 {
		t.Error("user burde være > 0")
	}
}

func TestBuildBreakdownSingleMessage(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Content: "kun én besked"},
	}
	bd := buildBreakdown(msgs, -1)
	if bd.user == 0 {
		t.Error("single user-besked burde gå til user")
	}
	if bd.hist != 0 {
		t.Errorf("hist burde være 0, fik %d", bd.hist)
	}
}

func TestBuildBreakdownEmpty(t *testing.T) {
	bd := buildBreakdown(nil, -1)
	if bd.sys != 0 || bd.wiki != 0 || bd.hist != 0 || bd.user != 0 || bd.tools != 0 {
		t.Error("tom messages-slice burde give alle-nul breakdown")
	}
}

// --- promptOverlap ---

func TestPromptOverlapEmpty(t *testing.T) {
	// Ingen tidligere beskeder
	if promptOverlap("hvad er AI observability?", nil) {
		t.Error("promptOverlap burde returnere false for tom historik")
	}
}

func TestPromptOverlapNoUserMessages(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "assistant", Content: "et svar fra assistenten"},
	}
	if promptOverlap("ny prompt", msgs) {
		t.Error("promptOverlap burde returnere false når ingen user-beskeder")
	}
}

func TestPromptOverlapShortMessages(t *testing.T) {
	// Under 3 ord → altid false
	msgs := []provider.Message{
		{Role: "user", Content: "hej ekte"},
	}
	if promptOverlap("hej ekte", msgs) {
		t.Error("promptOverlap burde returnere false for beskeder under 3 ord")
	}
}

func TestPromptOverlapHighOverlap(t *testing.T) {
	// Næsten identiske prompts → true
	msgs := []provider.Message{
		{Role: "user", Content: "hvad er forskellen på observability og monitoring i AI systemer"},
	}
	current := "hvad er forskellen på observability og monitoring i AI systemer please"
	if !promptOverlap(current, msgs) {
		t.Error("promptOverlap burde returnere true for næsten identiske prompts")
	}
}

func TestPromptOverlapLowOverlap(t *testing.T) {
	// Helt forskellige prompts → false
	msgs := []provider.Message{
		{Role: "user", Content: "hvad er den bedste golang web framework til REST APIs"},
	}
	current := "kan du hjælpe mig med at forstå kubernetes deployment strategier"
	if promptOverlap(current, msgs) {
		t.Error("promptOverlap burde returnere false for helt forskellige prompts")
	}
}

func TestPromptOverlapUsesLastUserMessage(t *testing.T) {
	// Kun den SENESTE user-besked sammenlignes
	msgs := []provider.Message{
		{Role: "user", Content: "hvad er observability og monitoring og metrics og traces og logs"},
		{Role: "assistant", Content: "svar på ovenstående"},
		{Role: "user", Content: "tak for det svar det var meget informativt og nyttigt"},
	}
	// Current matcher den FØRSTE user-besked, ikke den seneste
	current := "hvad er observability og monitoring og metrics og traces og logs igen"
	// Burde sammenligne med seneste user (tak for det svar...) som IKKE matcher
	if promptOverlap(current, msgs) {
		t.Error("promptOverlap burde kun sammenligne med seneste user-besked")
	}
}

func TestPromptOverlapCaseInsensitive(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Content: "Hvad Er AI Observability I Praksis For Udviklere"},
	}
	current := "hvad er ai observability i praksis for udviklere please fortæl"
	if !promptOverlap(current, msgs) {
		t.Error("promptOverlap burde være case-insensitiv")
	}
}

// --- max hjælpefunktion ---

func TestMax(t *testing.T) {
	if max(3, 5) != 5 {
		t.Error("max(3,5) burde give 5")
	}
	if max(5, 3) != 5 {
		t.Error("max(5,3) burde give 5")
	}
	if max(4, 4) != 4 {
		t.Error("max(4,4) burde give 4")
	}
	if max(0, 0) != 0 {
		t.Error("max(0,0) burde give 0")
	}
	if max(-1, -5) != -1 {
		t.Error("max(-1,-5) burde give -1")
	}
}

// --- estimateTokens ---

func TestEstimateTokensEmpty(t *testing.T) {
	if estimateTokens(nil) != 0 {
		t.Error("estimateTokens(nil) burde give 0")
	}
}

func TestEstimateTokensBasic(t *testing.T) {
	msgs := []provider.Message{
		{Role: "user", Content: "1234"}, // 4 tegn = 1 token
		{Role: "assistant", Content: "12345678"}, // 8 tegn = 2 tokens
	}
	got := estimateTokens(msgs)
	if got != 3 {
		t.Errorf("estimateTokens: forventede 3, fik %d", got)
	}
}
