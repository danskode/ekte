package provider

import (
	"context"
	"encoding/json"
)

type Message struct {
	Role       string
	Content    string
	ToolCallID string      // bruges ved role:"tool" svar
	ToolCalls  []ToolCall  // bruges ved role:"assistant" med tool calls
}

type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type Response struct {
	Content    string
	ToolCalls  []ToolCall
	StopReason string
}

type Provider interface {
	Chat(ctx context.Context, messages []Message) (*Response, error)
	ChatWithTools(ctx context.Context, messages []Message, tools []ToolDefinition) (*Response, error)
	Stream(ctx context.Context, messages []Message) (<-chan string, error)
	Name() string
}
