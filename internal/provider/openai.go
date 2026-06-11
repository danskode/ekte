package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"

	"github.com/sashabaranov/go-openai"

	"github.com/danskode/ekte/internal/netsafe"
)

type OpenAIProvider struct {
	client *openai.Client
	model  string
}

func NewOpenAIProvider(cfg *Config) *OpenAIProvider {
	clientCfg := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		clientCfg.BaseURL = cfg.BaseURL
	}
	// AllowLocal sættes af cmd/ekte efter interaktivt samtykke (internal/consent);
	// env-varen er headless-override. Begge åbner for private IP'er i dial-tjekket.
	clientCfg.HTTPClient = &http.Client{Transport: newTransport(allowLocalProvider(cfg))}
	return &OpenAIProvider{
		client: openai.NewClientWithConfig(clientCfg),
		model:  cfg.Model,
	}
}

// allowLocalProvider afgør om private/loopback provider-IP'er er tilladt —
// via interaktivt samtykke (cfg.AllowLocal) eller headless env-override.
func allowLocalProvider(cfg *Config) bool {
	return cfg.AllowLocal || os.Getenv("EKTE_ALLOW_LOCAL_PROVIDER") != ""
}

// newTransport bygger HTTP-transporten til provider-kald — deles af chat-klienten
// og ProbeLoadedContext, så IP-valideringen ikke kan glemmes ét sted.
//
// Lokale LLM-servere (LM Studio m.fl.) lukker ofte holdte-i-live-forbindelser
// hurtigere end Go's standardtransport (IdleConnTimeout: 90s) forventer.
// Når Go genbruger en sådan "død" forbindelse, fejler det første byte-læs med
// "unexpected end of JSON input" — selvom modellen er fuldt indlæst og klar
// (et øjeblikkeligt retry virker altid). DisableKeepAlives fjerner hele
// problemklassen ved at tvinge en frisk forbindelse pr. kald — uden mærkbar
// pris på et lokalt loopback/LAN-link.
//
// Vi kloner DefaultTransport i stedet for at oprette en helt ny — det bevarer
// Proxy-fra-miljø, DialContext-timeout (30s) og TLS-handshake-timeout (10s),
// så forbindelsesopsætning stadig er tidsbegrænset. Vi sætter BEVIDST INGEN
// http.Client.Timeout her: den ville afbryde selve respons-læsningen, og lokale
// modeller streamer ofte i flere minutter — en klient-timeout ville kappe
// helt normale, lange genereringer midt i strømmen.
func newTransport(allowLocal bool) *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DisableKeepAlives = true
	// Tilføj DialContext-hook der validerer resolved IP mod private ranges
	// for at forhindre DNS rebinding-angreb (CWE-918). Resolver DNS præcis
	// ét sted og dialer til den validerede IP — så kan et senere, uafhængigt
	// opslag ikke pege forbindelsen om til en privat adresse (TOCTOU).
	// LookupHost-fejl afvises (fail closed).
	baseDial := tr.DialContext
	if baseDial == nil {
		baseDial = (&net.Dialer{}).DialContext
	}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if allowLocal {
			return baseDial(ctx, network, addr)
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("DNS-opslag for provider-URL fejlede: %w", err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("DNS-opslag gav ingen resultater for %s", host)
		}
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip == nil {
				return nil, fmt.Errorf("DNS-opslag for %s gav uparserbar adresse %q", host, ipStr)
			}
			if netsafe.IsPrivateIP(ip) {
				return nil, fmt.Errorf("provider-URL peger på privat/intern IP %s — bekræft lokal provider ved opstart, eller sæt EKTE_ALLOW_LOCAL_PROVIDER=1 til headless brug", ipStr)
			}
		}
		return baseDial(ctx, network, net.JoinHostPort(ips[0], port))
	}
	return tr
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
	return p.ChatWithTools(ctx, messages, nil)
}

func (p *OpenAIProvider) ChatWithTools(ctx context.Context, messages []Message, tools []ToolDefinition) (*Response, error) {
	req := openai.ChatCompletionRequest{
		Model:    p.model,
		Messages: toOpenAIMessages(messages),
	}
	for _, t := range tools {
		req.Tools = append(req.Tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	resp, err := p.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("openai chat: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai: no choices returned")
	}
	choice := resp.Choices[0]
	out := &Response{
		Content:    choice.Message.Content,
		StopReason: string(choice.FinishReason),
		Usage: Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
	for _, tc := range choice.Message.ToolCalls {
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &raw); err != nil {
			raw = json.RawMessage(`{}`)
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: raw,
		})
	}
	return out, nil
}

func (p *OpenAIProvider) Stream(ctx context.Context, messages []Message) (<-chan string, error) {
	req := openai.ChatCompletionRequest{
		Model:    p.model,
		Messages: toOpenAIMessages(messages),
		Stream:   true,
	}
	stream, err := p.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("openai stream: %w", err)
	}
	ch := make(chan string)
	go func() {
		defer close(ch)
		defer stream.Close()
		for {
			resp, err := stream.Recv()
			if err != nil {
				return
			}
			if len(resp.Choices) > 0 {
				ch <- resp.Choices[0].Delta.Content
			}
		}
	}()
	return ch, nil
}

func (p *OpenAIProvider) StreamWithTools(ctx context.Context, messages []Message, tools []ToolDefinition) (<-chan StreamEvent, error) {
	req := openai.ChatCompletionRequest{
		Model:    p.model,
		Messages: toOpenAIMessages(messages),
		Stream:   true,
	}
	for _, t := range tools {
		req.Tools = append(req.Tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	stream, err := p.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("openai stream: %w", err)
	}
	ch := make(chan StreamEvent, 64)
	go func() {
		defer close(ch)
		defer stream.Close()
		// Akkumulér tool call-fragmenter pr. indeks
		type accTC struct {
			id        string
			name      string
			arguments string
		}
		accumulated := map[int]*accTC{}
		var streamErr error
		for {
			if ctx.Err() != nil {
				streamErr = ctx.Err()
				break
			}
			resp, err := stream.Recv()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					streamErr = err
				}
				break
			}
			if len(resp.Choices) == 0 {
				continue
			}
			delta := resp.Choices[0].Delta
			if delta.Content != "" {
				ch <- StreamEvent{Token: delta.Content}
			}
			if delta.ReasoningContent != "" {
				ch <- StreamEvent{Reasoning: delta.ReasoningContent}
			}
			for _, tc := range delta.ToolCalls {
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}
				if _, ok := accumulated[idx]; !ok {
					accumulated[idx] = &accTC{}
				}
				acc := accumulated[idx]
				if tc.ID != "" {
					acc.id = tc.ID
				}
				acc.name += tc.Function.Name
				acc.arguments += tc.Function.Arguments
			}
		}
		if streamErr != nil {
			ch <- StreamEvent{Done: true, Err: streamErr}
			return
		}
		final := StreamEvent{Done: true}
		keys := make([]int, 0, len(accumulated))
		for k := range accumulated {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		for _, k := range keys {
			acc := accumulated[k]
			if acc == nil {
				continue
			}
			var raw json.RawMessage
			if err := json.Unmarshal([]byte(acc.arguments), &raw); err != nil {
				raw = json.RawMessage(`{}`)
			}
			final.ToolCalls = append(final.ToolCalls, ToolCall{
				ID:    acc.id,
				Name:  acc.name,
				Input: raw,
			})
		}
		ch <- final
	}()
	return ch, nil
}

func toOpenAIMessages(msgs []Message) []openai.ChatCompletionMessage {
	out := make([]openai.ChatCompletionMessage, 0, len(msgs))
	for _, m := range msgs {
		msg := openai.ChatCompletionMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, openai.ToolCall{
				ID:   tc.ID,
				Type: openai.ToolTypeFunction,
				Function: openai.FunctionCall{
					Name:      tc.Name,
					Arguments: string(tc.Input),
				},
			})
		}
		out = append(out, msg)
	}
	return out
}
