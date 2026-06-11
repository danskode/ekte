package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// lmsModel er ét element i LM Studios /api/v0/models-svar — kun de felter
// vi bruger, resten ignoreres ved unmarshal.
type lmsModel struct {
	ID                  string `json:"id"`
	Type                string `json:"type"` // "llm" | "vlm" | "embeddings"
	State               string `json:"state"`
	LoadedContextLength int    `json:"loaded_context_length"`
}

// ProbeLoadedContext spørger LM Studios REST-API om hvor mange context-tokens
// den loadede model FAKTISK kører med. LM Studio loader ofte modeller med
// langt mindre context end config'ens context_size — og afviser så store
// prompts med en SSE 'event: error', som go-openai kun kan gengive som
// "unexpected end of JSON input". Ved at kende den reelle grænse kan ekte
// trimme historikken derefter og advare brugeren ved opstart.
//
// Best-effort: returnerer (_, _, false) ved enhver fejl — Ollama og cloud-
// providers har ikke endpointet. Matcher på model-id; falder tilbage til den
// eneste loadede model, da LM Studios id'er ofte afviger fra config'ens navn.
func ProbeLoadedContext(cfg *Config) (modelID string, loadedCtx int, ok bool) {
	if cfg == nil || cfg.BaseURL == "" {
		return "", 0, false
	}
	base := strings.TrimSuffix(strings.TrimSuffix(cfg.BaseURL, "/"), "/v1")
	// Klient-timeout er fin her (modsat chat-streams): svaret er én lille JSON.
	client := &http.Client{
		Transport: newTransport(allowLocalProvider(cfg)),
		Timeout:   3 * time.Second,
	}
	resp, err := client.Get(base + "/api/v0/models")
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return "", 0, false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, false
	}
	var payload struct {
		Data []lmsModel `json:"data"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", 0, false
	}
	var loaded []lmsModel
	for _, m := range payload.Data {
		// Embedding-modeller auto-loades ofte af LM Studio (typisk 2048 ctx) —
		// uden type-filteret kunne en sådan blive "den eneste loadede model"
		// og klampe chatten helt i bund.
		if m.Type != "llm" && m.Type != "vlm" {
			continue
		}
		if m.State == "loaded" && m.LoadedContextLength > 0 {
			loaded = append(loaded, m)
		}
	}
	for _, m := range loaded {
		if m.ID == cfg.Model {
			return m.ID, m.LoadedContextLength, true
		}
	}
	if len(loaded) == 1 {
		return loaded[0].ID, loaded[0].LoadedContextLength, true
	}
	// Flere loadede modeller og ingen id-match: gæt ikke.
	return "", 0, false
}
