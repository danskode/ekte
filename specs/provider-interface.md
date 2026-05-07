# Spec: Provider Interface

## Status: draft

## Intent

Definér et fælles Go-interface for LLM-providers, så resten af systemet
er uafhængigt af hvilken provider der bruges. Konkrete implementationer:
`OpenAIProvider` (OpenAI, LM Studio, Ollama) og `AnthropicProvider`.

## Interface

```go
type Message struct {
    Role    string // "user" | "assistant" | "tool"
    Content string
}

type ToolCall struct {
    ID       string
    Name     string
    Input    json.RawMessage
}

type Response struct {
    Content   string
    ToolCalls []ToolCall
    StopReason string
}

type Provider interface {
    Chat(ctx context.Context, messages []Message) (*Response, error)
    Stream(ctx context.Context, messages []Message) (<-chan string, error)
    Name() string
}
```

## Config (`.ekte/config.yaml`)

```yaml
provider: openai       # openai | anthropic
model: gpt-4o
base_url: http://localhost:1234/v1   # til LM Studio / Ollama
api_key: ""            # tomt = brug env var
```

## Implementationer

### OpenAIProvider
- Bruger `github.com/sashabaranov/go-openai`
- `base_url` sættes for LM Studio/Ollama-compat
- Tool use via OpenAI function calling format

### AnthropicProvider
- Bruger Anthropic HTTP API direkte (eller SDK)
- Separat impl pga. forskelligt tool use-format (input_schema)

## Acceptkriterier

- [ ] `Provider`-interface defineret i `internal/provider/provider.go`
- [ ] `OpenAIProvider` implementeret og testet mod Ollama
- [ ] `AnthropicProvider` implementeret med korrekt tool-format
- [ ] Config læses fra `.ekte/config.yaml` og env vars
- [ ] `go test ./internal/provider/...` passerer
