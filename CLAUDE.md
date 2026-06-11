# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Kommandoer

```bash
# Byg
go build ./cmd/ekte/

# Installér til $GOPATH/bin
go install ./cmd/ekte/

# Test (med race detector)
go test -v -race ./...

# Vet
go vet ./...

# Kør én test
go test -v -run TestNavn ./internal/agent/

# Sikkerhedsreview (kræver scripts/security-review.sh)
make security

# Sæt dev-hooks op (pre-push kører sikkerhedsreview automatisk)
make setup
```

## Arkitektur

`ekte` er et Go TUI-program — en personlig AI-assistent der kører direkte i terminalen. Entrypointet er `cmd/ekte/main.go`, som samler config → agent → TUI og starter Bubbletea-event-loopet.

### Kerneprincip: agent som præsentations-agnostisk kerne

Al forretningslogik lever i `internal/agent`. TUI'en (`internal/tui`) er et tyndt præsentationslag der kun modtager `[]Event` fra agenten og renderer dem. Dette gør det muligt at bygge alternative frontends ved at tilføje et nyt `cmd/`-entrypoint der importerer `internal/agent` — TUI-laget røres ikke.

### Dataflow

```
bruger-input → agent.ProcessStream(ctx, input) → <-chan Event → tui.Update()
```

`ProcessStream` returnerer en kanal der lukkes når svaret er færdigt. Slash-commands sendes stadig som én batch. Chat-beskeder streames token-for-token (`EventStreamToken`) og afsluttes med `EventStreamDone`.

### Tool call-loopet

Når LLM'en returnerer tool calls kører agenten et uendeligt loop (`streamChat` i `agent.go`) indtil ingen flere tool calls. Sikkerhedsmekanismer:
- Løkke-detektion: direkte gentagelse og 2-cyklisk oscillation stoppes
- Absolut rundeloft: 60 runder
- Absolut tidsloft: 2 timer
- Skriveoperationer (`write_file`, `edit_file`, `create_dir`) kræver brugerbekræftelse via `EventToolConfirm` — medmindre `whitelist.auto_approve` er sat
- Filindhold saniteres mod prompt injection via `sanitizeFileContent` før det lægges i tool-resultater

### Provider-interfacet

`internal/provider/provider.go` definerer `Provider`-interfacet med fire metoder: `Chat`, `ChatWithTools`, `Stream`, `StreamWithTools`. Anthropic (`anthropic.go`) og OpenAI/Ollama (`openai.go`) implementerer begge. Ny provider: implementér interfacet og tilføj den til `cmd/ekte/main.go`'s provider-valg.

### Config og whitelist

Config læses fra `.ekte/config.yaml` i projektmappen (ikke i ekte-repo'et selv). `WhitelistConfig` styrer hvilke operationer der er tilladt: `git_worktree`, `wiki_write`, `hook_run`, `file_read`, `file_write`, `wiki_fetch`, `auto_approve`. Alt er `false` som standard.

Fil-tools er sandboxet til projektmappen (`safePath` i `internal/tools`): `~` ekspanderes til hjemmemappen, og absolutte stier er kun tilladt under rødder angivet i `extra_roots` i config (normaliseret via `tools.NormalizeExtraRoots` — `/` og hjemmemappen selv frasorteres). De tilladte rødder nævnes i tool-beskrivelserne, så LLM'en kender dem.

### Skills

En skill er en markdown-fil med YAML-frontmatter (`name`, `description`, `tags`, `system_prompt_addition`). Filer hentes fra `.ekte/skills/` i projektmappen eller fra SKILLeton-kataloget via `/skills catalog` / `/skills install`. Aktiv skill injiceres som første system-besked i næste prompt, derefter nulstilles den.

### Wiki

`internal/wiki` håndterer søgning og lagring i en lokal wiki-mappe (default `~/.ekte/wiki`). Wikien injiceres automatisk i streamen når `HasSubstantiveQuery` vurderer at input er substantielt nok — wiki-kontekst præfikses som `"system"`-besked med "kilde til sandhed"-instruktion.

### Sessions

Gemmes som JSON i `.ekte/sessions/` (maks 3, ældste slettes automatisk). Ved genindlæsning saniteres alle beskeder uanset rolle mod injection-forsøg.

### Observability

`internal/obs` optager per-tur statistik (tokens, tok/s, cache-hits, token-fordeling) til JSON-filer i `~/.ekte/obs/`. Vises med `/observ`, `/observ all` og `/observ html`.
