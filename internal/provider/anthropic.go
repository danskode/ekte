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
	return p.ChatWithTools(ctx, messages, nil)
}

func (p *AnthropicProvider) ChatWithTools(ctx context.Context, messages []Message, tools []ToolDefinition) (*Response, error) {
	body, err := p.buildRequest(messages, false, tools)
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
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return nil, fmt.Errorf("anthropic decode: %w", err)
	}
	out := &Response{
		StopReason: ar.StopReason,
		Usage: Usage{
			InputTokens:      ar.Usage.InputTokens,
			OutputTokens:     ar.Usage.OutputTokens,
			CacheReadTokens:  ar.Usage.CacheReadInputTokens,
			CacheWriteTokens: ar.Usage.CacheCreationInputTokens,
		},
	}
	for _, c := range ar.Content {
		switch c.Type {
		case "text":
			out.Content += c.Text
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:    c.ID,
				Name:  c.Name,
				Input: c.Input,
			})
		}
	}
	return out, nil
}

func (p *AnthropicProvider) Stream(ctx context.Context, messages []Message) (<-chan string, error) {
	body, err := p.buildRequest(messages, true, nil)
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
func (p *AnthropicProvider) buildRequest(messages []Message, stream bool, tools []ToolDefinition) ([]byte, error) {
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
	if len(tools) > 0 {
		var at []map[string]any
		for _, t := range tools {
			at = append(at, map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.Parameters,
			})
		}
		payload["tools"] = at
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

// toAnthropicMessages konverterer messages til Anthropics format.
// Tool calls og tool-resultater kræver særlige content-blokke.
func toAnthropicMessages(msgs []Message) []map[string]any {
	var out []map[string]any
	for _, m := range msgs {
		switch {
		case m.Role == "tool":
			// Tool-resultat: sendes som user-besked med tool_result content
			out = append(out, map[string]any{
				"role": "user",
				"content": []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": m.ToolCallID,
					"content":     m.Content,
				}},
			})
		case len(m.ToolCalls) > 0:
			// Assistant-besked med tool calls
			var content []map[string]any
			if m.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var input any
				_ = json.Unmarshal(tc.Input, &input)
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": input,
				})
			}
			out = append(out, map[string]any{"role": "assistant", "content": content})
		default:
			out = append(out, map[string]any{"role": m.Role, "content": m.Content})
		}
	}
	return out
}
