package provider

import (
	"context"
	"encoding/json"
)

type Message struct {
	Role    string
	Content string
}

type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

type Response struct {
	Content    string
	ToolCalls  []ToolCall
	StopReason string
}

type Provider interface {
	Chat(ctx context.Context, messages []Message) (*Response, error)
	Stream(ctx context.Context, messages []Message) (<-chan string, error)
	Name() string
}
