package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const anthropicAPIURL = "https://api.anthropic.com/v1/messages"
const anthropicVersion = "2023-06-01"

type AnthropicProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewAnthropicProvider(cfg *Config) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey: cfg.APIKey,
		model:  cfg.Model,
		client: &http.Client{},
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (p *AnthropicProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
	body, err := p.buildRequest(messages, false)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic error %d: %s", resp.StatusCode, b)
	}

	var ar struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return nil, fmt.Errorf("anthropic decode: %w", err)
	}
	var text strings.Builder
	for _, c := range ar.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return &Response{Content: text.String(), StopReason: ar.StopReason}, nil
}

func (p *AnthropicProvider) Stream(ctx context.Context, messages []Message) (<-chan string, error) {
	body, err := p.buildRequest(messages, true)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic error %d: %s", resp.StatusCode, b)
	}

	ch := make(chan string)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		parseAnthropicSSE(resp.Body, ch)
	}()
	return ch, nil
}

// parseAnthropicSSE læser Server-Sent Events og sender text_delta tokens på kanalen.
func parseAnthropicSSE(r io.Reader, ch chan<- string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]
		var ev struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		if ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
			ch <- ev.Delta.Text
		}
	}
}

// buildRequest bygger request-body. System-beskeder udtrækkes til Anthropics separate system-felt.
func (p *AnthropicProvider) buildRequest(messages []Message, stream bool) ([]byte, error) {
	system, filtered := separateSystemMessages(messages)

	payload := map[string]any{
		"model":      p.model,
		"max_tokens": 8096,
		"messages":   toAnthropicMessages(filtered),
	}
	if system != "" {
		payload["system"] = system
	}
	if stream {
		payload["stream"] = true
	}
	return json.Marshal(payload)
}

func (p *AnthropicProvider) setHeaders(r *http.Request) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("x-api-key", p.apiKey)
	r.Header.Set("anthropic-version", anthropicVersion)
}

// separateSystemMessages skiller system-beskeder fra user/assistant-beskeder.
// Anthropic API accepterer kun role:user og role:assistant i messages-arrayet.
func separateSystemMessages(messages []Message) (system string, filtered []Message) {
	var sb strings.Builder
	for _, m := range messages {
		if m.Role == "system" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(m.Content)
		} else {
			filtered = append(filtered, m)
		}
	}
	return sb.String(), filtered
}

func toAnthropicMessages(msgs []Message) []anthropicMessage {
	out := make([]anthropicMessage, len(msgs))
	for i, m := range msgs {
		out[i] = anthropicMessage{Role: m.Role, Content: m.Content}
	}
	return out
}
