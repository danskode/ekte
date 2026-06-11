package provider

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeLoadedContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/models" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"data":[
			{"id":"stor-model","type":"llm","state":"loaded","loaded_context_length":2000,"max_context_length":262144},
			{"id":"embed","type":"embeddings","state":"loaded","loaded_context_length":2048},
			{"id":"anden-model","type":"llm","state":"not-loaded","max_context_length":40960}
		]}`))
	}))
	defer srv.Close()

	// AllowLocal: httptest kører på 127.0.0.1, som dial-guarden ellers afviser.
	cfg := &Config{BaseURL: srv.URL + "/v1", Model: "helt-andet-navn", AllowLocal: true}

	// Ingen id-match, men præcis én loadet LLM → brug den (embedding-modellen
	// er også loadet, men skal filtreres fra på type).
	id, ctx, ok := ProbeLoadedContext(cfg)
	if !ok || id != "stor-model" || ctx != 2000 {
		t.Errorf("forventet (stor-model, 2000, true), fik (%s, %d, %v)", id, ctx, ok)
	}

	// Id-match foretrækkes.
	cfg.Model = "stor-model"
	if id, ctx, ok = ProbeLoadedContext(cfg); !ok || id != "stor-model" || ctx != 2000 {
		t.Errorf("id-match: forventet (stor-model, 2000, true), fik (%s, %d, %v)", id, ctx, ok)
	}

	// Endpoint findes ikke (Ollama/cloud) → best-effort false, ingen fejl.
	cfg.BaseURL = srv.URL + "/forkert/v1"
	if _, _, ok = ProbeLoadedContext(cfg); ok {
		t.Error("manglende endpoint burde give ok=false")
	}

	// Tom config → false.
	if _, _, ok = ProbeLoadedContext(nil); ok {
		t.Error("nil-config burde give ok=false")
	}
}
