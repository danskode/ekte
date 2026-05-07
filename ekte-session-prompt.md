# Projekt: ekte — Go TUI Harness

Vi skal planlægge og begynde implementering af `ekte`: et personligt
developer harness bygget i Go med TUI, spec-drevet workflow og
multi-provider LLM-support. Projektet er spec-drevet — dvs. vi skriver
specs før implementation, og harness selv bruger det workflow vi bygger.

---

## Hvad ekte er

En CLI-binary (`ekte`) der starter en TUI fra den mappe man befinder sig i.
Harness opererer kun inden for den mappe. Konceptuelt svarer det til
Claude Code, men det er vores eget, fuldt kontrollerbart system.

---

## Repo-struktur (template på GitHub)

ekte-projektet hentes som GitHub template og gøres til eget repo.
Wiki'en er et separat GitHub template-repo der trækkes ind som git submodule.

```
mit-projekt/
├── .ekte/
│   ├── config.yaml       ← provider, model, api-nøgler
│   ├── system.md         ← systemprompt (markdown)
│   ├── skills/           ← skill-filer (markdown + frontmatter)
│   └── hooks/            ← deterministiske scripts (test, lint, build)
├── specs/                ← feature-specs (markdown, én per feature)
├── wiki/                 ← git submodule → udviklerens globale wiki-repo
└── [projektkode]
```

---

## Tekniske valg (besluttet)

- **Sprog**: Go
- **TUI**: Bubbletea (Elm-arkitektur: Model/Update/View)
- **Split-layout**: Venstre = conversation, højre = tools/status, bund = input
- **Providers**:
  - `OpenAIProvider` dækker: OpenAI API, LM Studio (localhost:1234), Ollama
  - `AnthropicProvider` til native Anthropic API (separat pga. tool use-format)
  - Skiftes i `.ekte/config.yaml` — ét sted

---

## Skill-format (markdown + frontmatter)

```markdown
---
name: skill-builder
version: 1.0.0
description: Opretter nye skills interaktivt
tools: [read_file, write_file, bash]
hooks:
  pre: hooks/validate-skill.sh   # kører FØR skill aktiveres
  post: hooks/test-skill.sh      # kører EFTER skill er færdig
tags: [meta, core]
---

# Skill Builder

## Intent
[hvad denne skill løser]

## Steps
1. ...

## System Prompt Addition
[tekst der injectes i systemprompt når skill er aktiv]
```

Harness loader alle `.md`-filer i `.ekte/skills/` ved opstart.
Man kan se dem i TUI uden at aktivere agenten (sidebar/panel).

---

## Git-workflow (worktrees)

Spec-drevet feature-workflow:
1. Bruger opretter en spec i `specs/min-feature.md`
2. `ekte` opretter automatisk en **git worktree** med en branch
   navngivet efter spec-filen — bruger behøver aldrig køre `git checkout -b`
3. Al arbejde sker i worktree'et
4. Deterministiske hooks (test, lint) kører som gates før merge
5. Ved godkendelse: worktree merges til main, branch slettes, worktree ryddes op

Sikkerhed: hooks er scripts i `.ekte/hooks/` — versioneres med projektet.
Hooks kan ikke bypasses af agenten (kun af bruger eksplicit).

---

## Memory & Wiki

Tre lag:

| Lag | Hvad | Levetid |
|---|---|---|
| Session log | Append-only log af denne session | Sæsion |
| Context compression | Komprimér gamle messages til summary | Automatisk ved token-grænse |
| Wiki (submodule) | Global vidensbase på tværs af projekter | Permanent |

Wiki'en bruger samme schema og operationer som dette projekts wiki
(se CLAUDE.md i wiki-repo'et for detaljer: ingest, query, gap-analysis,
frontmatter-format, taksonomi osv.).

---

## Subagents (v1-scope)

To use cases:
- **Parallelisering**: spawn flere agents til uafhængige deltasks
- **Baggrunds-tasks**: long-running agent kører mens bruger arbejder

Subagents arver provider-config men får eget kontekstvindue.

---

## TUI — synlighed uden agent

Fra TUI skal man kunne se (uden at sende noget til LLM):
- Aktive skills (liste med navn + description)
- Systemprompt (`.ekte/system.md`)
- Tilgængelige tools
- Hooks (hvilke scripts der kører hvornår)
- Aktive worktrees / åbne specs

Genveje: fx `Ctrl+S` = skills-panel, `Ctrl+P` = systemprompt-panel.

---

## V1 Scope (hvad vi bygger først)

1. **Core agentic loop** — provider-interface, OpenAI + Anthropic impl.
2. **Bubbletea TUI** — split-panel, input, tool-output
3. **Skill loader** — læs `.ekte/skills/*.md`, vis i panel
4. **Skill: skill-builder** — første og vigtigste skill; lader bruger
   oprette nye skills via samtale
5. **Git worktree manager** — auto-opret/merge worktrees fra specs
6. **Wiki submodule setup** — init-kommando der kloner wiki-template
   og tilføjer som submodule

Udskydes til v2: subagents, avanceret context compression,
spec-runner skill, fuld hook-pipeline.

---

## Start her

1. Lav repo-struktur og Go-modul (`go mod init github.com/[user]/ekte`)
2. Skriv provider-interface + OpenAI-impl (test mod Ollama lokalt)
3. Byg minimal Bubbletea loop (input → API → output)
4. Tilføj skill-loader
5. Skriv skill-builder skill i markdown

Vi bruger spec-drevet workflow på ekte selv: skriv en spec for hver
komponent i `specs/` inden implementation.
