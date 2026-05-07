package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sashabaranov/go-openai"
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
	return &OpenAIProvider{
		client: openai.NewClientWithConfig(clientCfg),
		model:  cfg.Model,
	}
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
