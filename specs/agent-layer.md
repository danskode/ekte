# Spec: Agent Layer

## Status: draft

## Intent

Udtrække forretningslogik fra TUI til et neutralt `internal/agent`-lag
så en GUI-app (Wails, Fyne) kan genbruge samme kerne uden at røre TUI-koden.

## Interface

```go
// agent/agent.go
type EventType int
const (
    EventAssistant EventType = iota  // LLM-svar
    EventSystem                       // system-besked (info, fejl, etc.)
    EventQuit                         // afslut
    EventTokenCount                   // opdateret token-antal
)

type Event struct {
    Type    EventType
    Content string
    Tokens  int
}

type Agent struct { ... }

func New(cfg Config) (*Agent, error)
func (a *Agent) Process(ctx context.Context, input string) ([]Event, error)
func (a *Agent) Messages() []provider.Message
func (a *Agent) ActiveSkill() *skill.Skill
```

## Hvad flyttes fra TUI til Agent

- Slash command dispatch (al logik i `slash.go`)
- Provider-kald (Chat/Stream)
- /forresten side-chat historik
- Token-estimering
- Skill-aktivering og injection
- /exit session-gem
- /resume session-load
- /wiki query
- /spec worktree-oprettelse

## Hvad bliver i TUI

- Rendering (View)
- Input-capture (textarea, key-bindings)
- Viewport-scrolling
- Størrelses-håndtering (WindowSizeMsg)

## GUI-portabilitet

En Wails/Fyne-app ville:
1. Importere `internal/agent`
2. Kalde `agent.Process(input)` ved bruger-input
3. Rendere `[]Event` i sit eget UI-framework

## Acceptkriterier

- [ ] `internal/agent` pakke med `Agent`, `Event`, `EventType`
- [ ] Al slash-logik i agent, ikke i TUI
- [ ] TUI er ren præsentationslag — kalder kun agent.Process()
- [ ] Eksisterende funktionalitet uændret
- [ ] Kompilerer og kører identisk som før
