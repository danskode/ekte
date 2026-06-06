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

type Usage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
}

type Response struct {
	Content    string
	ToolCalls  []ToolCall
	StopReason string
	Usage      Usage
}

// StreamEvent er enten et tekst-token eller et afsluttende event med eventuelle tool calls.
type StreamEvent struct {
	Token     string     // tekst-fragment; tomt på afsluttende event
	ToolCalls []ToolCall // kun sat på afsluttende event, hvis LLM kaldte tools
	Done      bool       // true for det afsluttende event
}

type Provider interface {
	Chat(ctx context.Context, messages []Message) (*Response, error)
	ChatWithTools(ctx context.Context, messages []Message, tools []ToolDefinition) (*Response, error)
	Stream(ctx context.Context, messages []Message) (<-chan string, error)
	// StreamWithTools streamer tekst-tokens løbende og returnerer tool calls på det afsluttende event.
	StreamWithTools(ctx context.Context, messages []Message, tools []ToolDefinition) (<-chan StreamEvent, error)
	Name() string
}
