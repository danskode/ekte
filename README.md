# ekte

En personlig AI-assistent til softwareudvikling, bygget som et Go TUI-program.
Køres direkte i terminalen — ingen browser, ingen cloud-dashboard.

```
┌─────────────────────────────────────────────────────────┐
│ ekte                                    /hjælp           │
│  Hvad vil du bygge i dag?                               │
│                                                         │
│ Du                                                      │
│  /dep github.com/gin-gonic/gin                          │
│                                                         │
│ ekte                                                    │
│  ✓ Score hentet — se tool-panelet                       │
├─────────────────────────────────────────────────────────┤
│ Skriv her... (Enter sender, Shift+Enter = ny linje)     │
├─────────────────────────────────────────────────────────┤
│ kontekst: 1240/200000                          /hjælp   │
└─────────────────────────────────────────────────────────┘
```

---

## Indhold

- [Forudsætninger](#forudsætninger)
- [Installation](#installation)
- [Første opsætning](#første-opsætning)
- [Konfiguration](#konfiguration)
- [Slash commands](#slash-commands)
- [Skills](#skills)
- [Wiki](#wiki)
- [Mappestruktur](#mappestruktur)

---

## Forudsætninger

- Go 1.21 eller nyere
- Git
- En API-nøgle til Anthropic eller OpenAI (eller en lokal Ollama-instans)

---

## Installation

```bash
git clone https://github.com/danskode/ekte.git
cd ekte
go build -o ekte ./cmd/ekte
sudo mv ekte /usr/local/bin/   # eller et andet sted i din PATH
```

---

## Første opsætning

Gå til mappen for det projekt du vil arbejde på, og kør:

```bash
cd ~/projekter/mit-projekt
ekte
```

Ved første kørsel guides du igennem en kort onboarding:

1. **Tillid** — bekræft at du stoler på mappen
2. **Projektbeskrivelse** — besvar et par spørgsmål om projektet; svaret gemmes som `ekte.md`
3. **Navn** — hvad vil du kaldes, og hvad skal din agent hedde?
4. **API-opsætning** — vælg provider interaktivt; ekte forklarer præcis hvad du skal gøre
5. **Wiki** — valgfrit: sæt en personlig wiki op til videndeling på tværs af projekter

### API-nøgle

ekte gemmer **aldrig** API-nøgler i config-filen — kun i miljøvariabler.
Nøgler i filer risikerer at lække via git-historik.

```bash
# Anthropic
export ANTHROPIC_API_KEY="sk-ant-..."

# OpenAI
export OPENAI_API_KEY="sk-..."

# Tilføj til ~/.bashrc eller ~/.zshrc så den huskes permanent
```

### Lokal model (Ollama)

```bash
ollama pull llama3.2
```

Vælg "Lokal Ollama" i API-guiden, eller konfigurer manuelt i `.ekte/config.yaml`:

```yaml
provider: openai
model: llama3.2
base_url: http://localhost:11434/v1
```

---

## Konfiguration

Konfigurationen ligger i `.ekte/config.yaml` i projektmappen. Filen oprettes automatisk ved onboarding, eller manuelt med `ekte init`.

### Fuld konfigurationsreference

```yaml
# LLM-provider: "anthropic" eller "openai" (bruges også til Ollama/LM Studio)
provider: anthropic
model: claude-sonnet-4-6

# Lokal model — udelad base_url for cloud-providers
base_url: ""

# Wiki — valgfrit
wiki:
  enabled: true
  path: ~/.ekte/wiki

# Whitelist — hvilke operationer ekte må udføre uden at spørge
# Alt er forbudt som standard
whitelist:
  git_worktree: true   # /spec opret/merge/fjern git worktrees
  wiki_write:   true   # /wiki gem — skriv til wiki
  hook_run:     true   # /hook <navn> — kør shell-kommandoer

# Hooks — navngivne shell-kommandoer der køres med /hook
hooks:
  test: go test ./...
  lint: golangci-lint run
  build: go build ./...
```

> **OBS:** Tilladelser er `false` som standard. Uden whitelist-konfiguration vil `/spec`, `/wiki gem` og `/hook` blive blokeret med en forklarende fejlbesked.

---

## Slash commands

Alle kommandoer skrives direkte i input-feltet.

| Kommando | Beskrivelse |
|---|---|
| `/hjælp` | Vis liste over alle kommandoer |
| `/skills [navn]` | Vis tilgængelige skills — angiv navn for at aktivere |
| `/spec <navn>` | Opret en spec og tilhørende git worktree |
| `/spec merge <navn>` | Merge worktree ind i main og ryd op |
| `/spec remove <navn>` | Slet worktree uden merge |
| `/compress` | Komprimer kontekstvinduet — LLM laver et resumé af samtalen |
| `/wiki "spørgsmål"` | Søg i din personlige wiki |
| `/wiki gem <titel>` | Gem seneste `/forresten`-svar i wikien |
| `/hook` | Vis tilgængelige hooks |
| `/hook <navn>` | Kør en hook — output vises i tool-panelet |
| `/dep <modul>` | Sikkerhedsscore for én Go-afhængighed |
| `/sec-check` | Scan alle afhængigheder i projektet + ekte-harness |
| `/forresten <besked>` | Side-chat med en isoleret subagent (husker sin egen historik) |
| `/clear` | Ryd samtalens historik |
| `/resume` | Vis tidligere gemte sessioner |
| `/resume <nummer>` | Indlæs en tidligere session |
| `/exit` | Gem session og afslut |

### Tastatur

| Tast | Handling |
|---|---|
| `Enter` | Send besked |
| `Shift+Enter` | Ny linje i input |
| `↑` / `↓` | Naviger i inputhistorik |
| `PgUp` / `PgDn` | Scroll i samtalevisning |

### `/dep` og `/sec-check` — sikkerhedsscore

`/dep <modul>` tjekker ét modul mod Go module proxy og OSV.dev (CVE-database):

```
/dep github.com/gin-gonic/gin
```

```
Afhængighed:  github.com/gin-gonic/gin
Version:      v1.10.0 (5 maj 2024)
Score:        ★★★★★
Kendte CVE:   0

✓ Trygt at bruge
```

`/sec-check` scanner alle afhængigheder på én gang — både projektets `go.mod`
og ekte-harness'ets egne moduler. Op til 8 tjek kører parallelt.

```
Projekt (3 moduler)

✓ gin-gonic/gin v1.10.0
✓ gorilla/mux v1.8.1
⚠ some/old-lib v1.0.0 [1 CVE]
  · GO-2023-1234: Remote code execution...

3 rene · 0 sårbar · 0 fejl

────────────────────────

ekte-harness (25 moduler)

✓ charmbracelet/bubbletea v1.3.10
✓ charmbracelet/lipgloss v1.1.0
...

25 rene · 0 sårbar · 0 fejl
```

---

## Skills

En skill er en markdown-fil med YAML-frontmatter der tilføjer et system-prompt til næste besked.
Læg dem i `.ekte/skills/` i projektmappen.

### Eksempel: `.ekte/skills/tdd.md`

```markdown
---
name: tdd
description: Test-drevet udvikling — skriv test først
tags: [testing, go]
---

## System Prompt Addition

Du hjælper med test-drevet udvikling. Skriv altid tests før implementering.
Brug Go's standard `testing`-pakke. Forklar din tankegang kort.
```

### Brug

```
/skills          # vis alle skills
/skills tdd      # aktiver — gælder for næste prompt
```

---

## Wiki

Wikien er et privat vidensbibliotek der deles på tværs af projekter.
Den er baseret på [danskode/simple-wiki](https://github.com/danskode/simple-wiki).

### Opsætning

```bash
ekte init   # følg guiden til wiki-opsætning
```

### Arbejdsflow

```
/forresten hvad er forskellen på mutex og rwmutex i Go?
```

ekte svarer via en isoleret subagent. Hvis svaret er nyttigt:

```
/wiki gem mutex-vs-rwmutex
```

Siden gemmes i din wiki og kan søges frem i fremtidige projekter:

```
/wiki "trådsikkerhed i Go"
```

---

## Mappestruktur

### Projektmappe (dit projekt)

```
dit-projekt/
├── ekte.md                    # projektbeskrivelse og kontekst til LLM
│
└── .ekte/                     # ekte's arbejdsmappe (gitignored)
    ├── config.yaml            # provider, whitelist, hooks, wiki
    ├── skills/                # egne skills til dette projekt
    │   └── min-skill.md
    ├── sessions/              # gemte samtaler (max 3, ældste slettes)
    │   └── 2026-05-07-...json
    ├── worktrees/             # git worktrees til specs (auto-styret)
    │   └── min-feature/
    └── wiki/ -> ~/.ekte/wiki  # symlink til global wiki (valgfrit)
```

### ekte-repo (kildekode)

```
ekte/
├── cmd/
│   └── ekte/
│       └── main.go            # entrypoint: onboarding → agent → TUI
│
├── internal/
│   ├── agent/
│   │   └── agent.go           # al forretningslogik; Process() → []Event
│   ├── dep/
│   │   └── dep.go             # sikkerhedsscore via proxy.golang.org + osv.dev
│   ├── git/
│   │   └── worktree.go        # Create, List, Merge, Remove worktrees
│   ├── onboarding/
│   │   └── onboarding.go      # første-kørsel guide
│   ├── provider/
│   │   ├── config.go          # Config, WhitelistConfig, LoadConfig
│   │   ├── provider.go        # Provider-interface
│   │   ├── anthropic.go       # Anthropic-implementering
│   │   └── openai.go          # OpenAI/Ollama-implementering
│   ├── session/
│   │   └── session.go         # gem og indlæs samtaler som JSON
│   ├── skill/
│   │   └── skill.go           # parser markdown-skills med YAML-frontmatter
│   ├── tui/
│   │   ├── model.go           # Bubbletea Model — præsentationslag
│   │   ├── update.go          # tastaturhåndtering og event-rendering
│   │   └── styles.go          # lipgloss-stilarter
│   └── wiki/
│       ├── wiki.go            # Query, SavePage, grepSearch
│       └── init.go            # wiki-opsætning ved onboarding
│
├── specs/                     # feature-specs (én per feature — driver worktree-workflow)
├── go.mod
└── README.md
```

### Arkitektur

```
┌──────────────────────────────────────────────┐
│  cmd/ekte/main.go                            │
│  Samler config → agent → TUI                 │
└────────────────┬─────────────────────────────┘
                 │
    ┌────────────▼────────────┐
    │  internal/agent         │
    │  Process(input) →Events │  ← al logik: slash, LLM, hooks, dep
    └──┬───────┬──────┬───────┘
       │       │      │
  Provider   Wiki   Git/Skills/Session/Dep
  (OpenAI/   (søg   (worktrees, skills,
  Anthropic)  gem)   sessioner, CVE-tjek)
       │
    ┌──▼──────────────────────┐
    │  internal/tui           │
    │  Modtager Events        │  ← ren præsentation, ingen logik
    │  Renderer til terminal  │
    └─────────────────────────┘
```

TUI'en er et tyndt præsentationslag — al logik lever i `internal/agent`.
Det gør det muligt at bygge alternative frontends (GUI, LSP, web) ved kun
at tilføje et nyt `cmd/`-entrypoint der importerer `internal/agent`.
