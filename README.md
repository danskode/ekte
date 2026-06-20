# ekte

```
           ██            ██               
   █████   ██    ██  ████████      █████  
  ████████ ███████       ██       ████████
  ██       ██    ██      ██       ██      
   ██████  ██      ██    ██████    ██████ 
     et agent harness baseret på AIDD
```

🌐 **[danskode.github.io/ekte](https://danskode.github.io/ekte/)** — install-kommando, demo og økosystem.

**ekte** er et transparent agent-harness til softwareudvikling, bygget som et Go
TUI-program. Det kører direkte i terminalen — ingen browser, intet cloud-dashboard —
og er bygget op om **AIDD** (*Architecture of Intent-Driven Development*): du
kvalificerer din *intention*, agenten implementerer, og harnesset *verificerer*
resultatet mod intentionen før noget regnes som færdigt.

ekte er **provider-agnostisk** (Anthropic, OpenAI eller en lokal model via
Ollama/LM Studio), **lokalt** (din kode og dine nøgler forlader ikke maskinen ud
over kald til den provider *du* vælger) og **gennemsigtigt** (alle mekanismer er
synlige Go-primitiver, ikke skjult magi).

---

## Indhold

1. [Hvad er ekte?](#hvad-er-ekte)
2. [Installation](#installation)
3. [Kom i gang på 3 minutter](#kom-i-gang-på-3-minutter)
4. [Kerneflowet: AIDD i praksis](#kerneflowet-aidd-i-praksis)
5. [Konfiguration](#konfiguration)
6. [Sikkerhedsmodel](#sikkerhedsmodel)
7. [Kommandoreference](#kommandoreference)
8. [Skills & SKILLeton](#skills--skilleton)
9. [Wiki & hukommelse](#wiki--hukommelse)
10. [Afhængigheds-sikkerhed](#afhængigheds-sikkerhed)
11. [CI/CD & udvikling](#cicd--udvikling)
12. [Projektstruktur & arkitektur](#projektstruktur--arkitektur)

---

## Hvad er ekte?

De fleste AI-kodeværktøjer er en chat med værktøjer. ekte er et **harness**: en
kode-løkke der omslutter sprogmodellen med *guides* (føred-kontroller: system-prompt,
skills, plan-mode) og *sensors* (efter-kontroller: tests, sikkerheds- og
intent-verifikation). Det giver en disciplineret arbejdsgang frem for fri improvisation.

Den røde tråd er **intentionen**:

```
   DEFINITION              GENERATION             VERIFICATION
   (din intention)   →     (agenten koder)   →    (sensorer måler mod intentionen)
   /plan (ICE)             /goal-loopet           computationel + inferentiel sensor
                                                  + din endelige accept (HITL)
```

Det betyder at en `/goal`-kørsel ikke er "færdig", fordi koden *kompilerer* — den er
færdig, når den både består de tekniske tjek **og** beviseligt opfylder de
succeskriterier du opstillede, og **du** har godkendt det.

---

## Installation

**Hurtigst (anbefalet):**

```bash
curl -fsSL https://raw.githubusercontent.com/danskode/ekte/main/install.sh | sh
```

Installerer til `~/.local/bin` — ingen sudo, ingen pakkemanager. Kræver `git` og
`curl`/`wget`. Scriptet tilføjer `~/.local/bin` til din PATH (`.bashrc`/`.zshrc` + `.profile`).

> **Efter install:** åbn en **ny terminal** (eller `source ~/.bashrc`) så `ekte` er i din PATH.
> Vil du bruge den med det samme i den nuværende shell:
> `export PATH="$HOME/.local/bin:$PATH"`.

**Fra kildekode** (kræver Go 1.25+):

```bash
git clone https://github.com/danskode/ekte.git
cd ekte
go install ./cmd/ekte    # lægger binæren i $(go env GOPATH)/bin
```

Sørg for at install-mappen er i din `PATH`.

---

## Kom i gang på 3 minutter

**1 — Gå til dit projekt og start ekte:**

```bash
cd ~/projekter/mit-projekt
ekte
```

**2 — Følg onboarding** (kun første gang). Du bekræfter at du stoler på mappen,
beskriver projektet kort (gemmes som `ekte.md`), vælger navn, og sætter provider/model
op (skrives til `.ekte/config.yaml`). Wiki er valgfrit.

**3 — Sæt din API-nøgle** (kun i miljøvariabler — aldrig i config):

```bash
export ANTHROPIC_API_KEY="sk-ant-..."   # eller OPENAI_API_KEY
```

Bruger du en lokal model (Ollama/LM Studio) er ingen nøgle nødvendig.

**Så er du i gang.** Skriv almindeligt sprog, eller brug en slash-kommando. `/hjælp`
viser alt. For den fulde AIDD-arbejdsgang, læs næste afsnit.

> Vil du bare have et hurtigt build-loop op at køre? Tilføj et tjek-hook og kør
> `/goal` (se [Kerneflowet](#kerneflowet-aidd-i-praksis)).

---

## Kerneflowet: AIDD i praksis

Dette er det ekte er bygget til. Arbejdsgangen har tre faser.

### 1. Definér intentionen — `/plan`

`/plan` sætter dig i **Architect of Intent**-mode. Agenten stiller spørgsmål, ét ad
gangen, og hjælper dig med at kvalificere intentionen efter **ICE** (Intent, Context,
Expectations). Den skriver ikke kode her — den hjælper dig med at gøre *hvad* og
*hvornår det er lykkedes* præcist.

```
/plan byg et login-endpoint med rate limiting
…
/plan godkend
```

Ved `/plan godkend` destilleres dine **Expectations** til konkrete succeskriterier —
rubrikken resten af flowet måler imod.

### 2. Generér — `/goal`

`/goal` er den autonome bygge-løkke (den simpleste agentic PIV: Plan→Implement→Validate).
Den **kræver en godkendt plan**, så intentionen altid er eksternaliseret før der kodes.

```
/goal byg login-endpoint mod planen
```

Hver iteration: agenten skriver/retter kode → kører dit `check_hook` (computationelt
tjek) → og når det består, kører en **inferentiel Validate-fase**:

- **Sikkerheds-sensor** — review for CWE/OWASP-risici.
- **Intent-sensor** — en *separat, skeptisk* evaluator der afgør om ændringen faktisk
  opfylder dine succeskriterier (ikke bare om den kompilerer).

Begge skal bestå. Underkender en sensor, fødes kritikken tilbage og løkken itererer;
efter et par afvisninger stopper den og forelægger fundene for dig (backstop). Kan
intentionen ikke afgøres, *spørger* den i stedet for at gætte.

### 3. Verificér & accepter — du har sidste ord

Sensorerne **godkender ikke** — de anbefaler. Når begge består, forelægger løkken
resultatet og beder om **din** accept (HITL). Først dér regnes målet som nået. Hvert
udfald (godkendt, afvist, backstop, …) opsamles automatisk som en genbrugelig lektion
(se [Wiki & hukommelse](#wiki--hukommelse)).

### Sensorerne uden for løkken

Du kan køre de samme sensorer ad hoc på dine ændringer:

| Kommando | Hvad |
|---|---|
| `/verify` · `ekte verify` | Sensor-tjek af arbejdstræet: sikkerhed + intent-conformance |
| `/review` · `ekte review` | Provider-agnostisk sikkerhedsreview (CWE/OWASP) af en git-diff |
| `/orchestrate <opgave>` | Multi-agent: nedbryd opgave → subagenter løser → saml (Fase 1) |

`ekte verify` og `ekte review` er CLI-subkommandoer (exit ≠ 0 ved fund) og egner sig
til pre-push-hooks. Begge **redakterer hemmeligheder** før diffen sendes til provideren
og fejler **lukket**.

---

## Konfiguration

Konfigurationen ligger i `.ekte/config.yaml` i projektmappen. Filen oprettes ved
onboarding eller med `ekte init` / `/init`.

```yaml
# LLM-provider: "anthropic" eller "openai" (openai bruges også til Ollama/LM Studio)
provider: anthropic
model: claude-sonnet-4-6
base_url: ""            # sæt for lokal model, fx http://localhost:11434/v1

# Wiki — valgfrit
wiki:
  enabled: true
  path: ~/.ekte/wiki

# Whitelist — alt er forbudt som standard
whitelist:
  file_read:     true
  file_write:    true
  hook_run:      true   # /hook <navn> + run_hook-tool
  harness_write: true   # skriv harness-filer (memory, skills, ekte.md) — kræver bekræftelse
  git_worktree:  true   # /spec opret/merge/fjern
  wiki_write:    true   # /wiki gem

# Hooks — navngivne shell-kommandoer kørt med /hook eller af agenten via run_hook
hooks:
  test:  go test ./...
  build: go build ./...

# Autonom /goal: tjek-hook + intent-rubrik
goal:
  check_hook: build      # computationelt succes-tjek pr. iteration
  max_iterations: 10
  # success_criteria sættes typisk automatisk ved /plan godkend
  capture: true          # automatisk vidensopsamling fra /goal-udfald (default til)

# Ekstra rødder — mapper uden for projektet hvor fil-tools også må læse/skrive
extra_roots:
  - ~/projekter/playground
```

> **OBS:** Tilladelser er `false` som standard. Uden whitelist blokeres fil-, hook-,
> wiki- og worktree-operationer med en forklarende besked.

**API-nøgler gemmes aldrig i config** — kun i miljøvariabler (`ANTHROPIC_API_KEY` /
`OPENAI_API_KEY`), så de ikke lækker via git-historik.

**Lokale providers kræver samtykke:** peger `base_url` på en privat adresse, spørger
ekte første gang og gemmer "ja" pr. præcis URL i `~/.ekte/consent.yaml`. Til scriptet
brug: `EKTE_ALLOW_LOCAL_PROVIDER=1`.

---

## Sikkerhedsmodel

ekte er bygget med en bevidst trusselsmodel. Kort fortalt:

- **Fil-tools er sandkasse-låst** til projektmappen (+ evt. `extra_roots`); path
  traversal og symlink-flugt afvises.
- **Tillid bestemmes af oprindelse, ikke kommando-streng.** Et `run_hook` kræver altid
  bekræftelse i TUI'en. Headless `ekte -y goal` auto-godkender fil-skrivninger, men en
  hook fra et klonet, ubetroet repos `.ekte/config.yaml` gates særskilt — den regnes
  kun som betroet hvis den kommer fra din **globale** `~/.ekte/config.yaml`, er
  **godkendt før**, eller `EKTE_ALLOW_LOCAL_HOOKS=1` er sat (CWE-78/829).
- **`goal.check_hook`** kører programmatisk hver iteration — er kommandoen ikke betroet,
  nægter `/goal` at starte.
- **Hemmeligheder redakteres** (best-effort) før diffs sendes til en provider; sikkerheds-
  gates fejler **lukket**; ikke-betroet input afgrænses med tilfældige markører mod
  prompt-injection.
- **Persisteret kontekst bekræftes:** noter der senere loades som betroet kontekst
  (`ekte.md`, memory, goal-lektioner) kræver din godkendelse før skrivning.

> At betro et build-baseret check_hook (`mvn`, `gradle`, `npm`, `ekte springcheck`)
> betyder at betro projektets **build-logik** — en `pom.xml` kører vilkårlig kode via
> plugins. Gatingen styrer *hvornår* det starter autonomt; betro kun repos du stoler på.

Kør `/security` for at se den aktuelle whitelist og guardrail-status.

---

## Kommandoreference

Alle kommandoer skrives i input-feltet. `Tab` autocompleter (også 2. ord). Kontekst-
afhængige kommandoer skjules når de ikke giver mening (fx `/verify` uden provider).

**AIDD-flow**

| Kommando | Beskrivelse |
|---|---|
| `/plan <beskrivelse>` | Architect of Intent — kvalificér intent (ICE) inden implementering |
| `/plan godkend` · `vis` · `afvis` | Gem (→ succeskriterier), vis eller forkast planen |
| `/goal <beskrivelse>` | Autonom bygge-løkke med sensor-verifikation (kræver godkendt plan) |
| `/verify` | Sensor-tjek af ændringer: sikkerhed + intent-conformance |
| `/review` | Agnostisk sikkerhedsreview (CWE/OWASP) af ændringer |
| `/orchestrate <opgave>` | Multi-agent: nedbryd → subagenter → saml |

**Projekt & hooks**

| Kommando | Beskrivelse |
|---|---|
| `/init` | Opret `.ekte/config.yaml` + `ekte.md` (aktiverer fil-tools) |
| `/hook [navn]` | Vis hooks — angiv navn for at køre |
| `/hook add <navn> <kommando>` · `fjern <navn>` | Administrér hooks uden at redigere YAML |
| `/spec <navn>` · `merge` · `remove` | Spec + git worktree-arbejdsgang |

**Skills, wiki & hukommelse**

| Kommando | Beskrivelse |
|---|---|
| `/skills [navn]` | Vis/aktivér skills |
| `/skills library` · `bundle` · `show` · `install` · `update` | SKILLeton-biblioteket |
| `/wiki "spørgsmål"` · `/wiki-get <url>` · `/wiki-gem <titel>` | Søg/ingest/gem i wikien |
| `/forresten <besked>` | Side-chat med isoleret subagent |
| `/remember <tekst>` | Gem en note i hukommelsen (`.ekte/memory/`) |

**Sikkerhed, session & indstillinger**

| Kommando | Beskrivelse |
|---|---|
| `/dep <modul>` · `/sec-check` | Sikkerhedsscore for én/alle Go-afhængigheder |
| `/security` | Vis whitelist og guardrails |
| `/context` · `/compress` · `/observ` | Kontekst-lag · komprimér · ydelses-statistik |
| `/model` · `/mode beginner\|expert` · `/sound on\|off` | Provider/model · hints · lyd |
| `/kø` · `/navngiv` · `/resume` · `/clear` · `/exit` | Kø · navngiv · genoptag · ryd · afslut |

**Tastatur:** `Enter` send · `Shift+Enter` ny linje · `Shift+Tab` skift plan↔develop ·
`Tab` autocomplete · `↑`/`↓` inputhistorik · `PgUp`/`PgDn` scroll.

---

## Skills & SKILLeton

En **skill** er en markdown-fil med YAML-frontmatter, der tilføjer et system-prompt
(en *guide*) til arbejdet. Læg egne skills i `.ekte/skills/`:

```markdown
---
name: tdd
description: Test-drevet udvikling — skriv test først
tags: [testing, go]
---

## System Prompt Addition
Du hjælper med test-drevet udvikling. Skriv altid tests før implementering.
```

```
/skills          # vis alle
/skills tdd      # aktivér — gælder næste prompt
```

**SKILLeton** er det delte bibliotek af skills, du kan læse og installere fra:

```
/skills library         # se biblioteket (✓ = installeret)
/skills show 3          # læs en skill før install (vises i sidepanelet)
/skills install 1,3     # installér udvalgte
/skills bundle security # installér en hel pakke (security/ci/aidd/...)
```

Skills (*guides*) og sensorer (*harness-kode*) er bevidst adskilt: en skill former
*hvordan* agenten arbejder; sensorerne *måler* resultatet.

---

## Wiki & hukommelse

**Wiki** — et privat vidensbibliotek der deles på tværs af projekter. Spørg via en
isoleret subagent og fil gode svar tilbage:

```
/forresten forskellen på mutex og rwmutex i Go?
/wiki-gem mutex-vs-rwmutex
/wiki "trådsikkerhed i Go"
```

**Hukommelse** — noter i `.ekte/memory/` (lokalt) og `~/.ekte/memory/` (globalt) loades
som kontekst ved sessionsstart (saniteret mod injection). `/remember <tekst>` gemmer en
note. Derudover opsamler harnesset automatisk viden fra `/goal`:

- **Goal-journal** (`.ekte/memory/goals/journal.jsonl`) — en eval-case-klar, append-only
  log over hvert terminalt `/goal`-udfald (mål, kriterier, sensor-verdikter, dit
  ja/nej). Loades *ikke* i kontekst; den er telemetri der kan replayes senere.
- **Goal-lektioner** (`.ekte/memory/goal-lessons.md`) — en kort destilleret lektion pr.
  udfald, som du bekræfter før den promoveres til loadet hukommelse. Holdes til de
  seneste ~15, så kontekst-budgettet ikke vokser ukontrolleret.

---

## Afhængigheds-sikkerhed

`/dep <modul>` scorer ét Go-modul mod Go module proxy og OSV.dev (CVE-database):

```
/dep github.com/gin-gonic/gin
→ Version v1.10.0 · Score ★★★★★ · 0 kendte CVE · ✓ Trygt at bruge
```

`/sec-check` scanner alle afhængigheder på én gang — både projektets `go.mod` og
ekte-harnessets egne moduler (op til 8 parallelle tjek), og rapporterer CVE'er pr. modul.

---

## CI/CD & udvikling

ekte har en let, men reel pipeline. Status pr. nu:

**CI — `.github/workflows/ci.yml`** ✅ *på plads*
- Kører på **hver push og pull request**.
- `go vet ./...` + `go test -race ./...` på `ubuntu-latest`, Go-version fra `go.mod`.
- Actions er **SHA-pinnede**; Dependabot holder dem opdaterede (`.github/dependabot` + PR'er).

**Release — `.github/workflows/release-please.yml`** ✅ *automatiseret*
- **release-please** vedligeholder en **release-PR** på `main` med auto-genereret changelog +
  næste semver, udledt af **Conventional Commits** (`feat:` → minor, `fix:` → patch,
  `feat!:`/`BREAKING CHANGE` → major). Når du merger PR'en, oprettes tag + GitHub Release.
- **goreleaser** (`.goreleaser.yaml`) kører i samme workflow (betinget af release-please) og
  uploader cross-platform binærer + `checksums.txt` til releasen. ekte er en CLI, så "CD" =
  release-artefakter, ikke server-deploy.
- Releases er stadig **bevidste** (du merger release-PR'en), men kræver intet manuelt tagging.

**Deploy — `.github/workflows/pages.yml`** ✅ *på plads*
- Landingpagen i `site/` deployes til GitHub Pages (`danskode.github.io/ekte`) ved push til `main`.

**Maintainer-side gate** (ikke en del af CI)
- En lokal **pre-push hook** (`scripts/pre-push.sh` + `security-review.sh`) kører et
  LLM-baseret sikkerhedsreview af upushede commits og blokerer ved medium+ fund. Det er
  *ikke-deterministisk* og bevidst adskilt fra den agnostiske `ekte verify`/`ekte review`.

**Conventional Commits** — brug `feat:`, `fix:`, `docs:`, `chore:`, `refactor:` (+ `!`/
`BREAKING CHANGE` for major), så release-please kan udlede versioner og changelog automatisk.

**Udestående / mulige forbedringer**
- CI kører kun `go vet` — ingen `golangci-lint`-trin endnu (kan køres lokalt via hook).
- Ingen coverage-gate.

**Lokal udvikling**

```bash
go build ./...           # byg alt
go vet ./... && go test ./...   # samme tjek som CI (tilføj -race for fuld paritet)
go install ./cmd/ekte    # installér din lokale build
```

---

## Projektstruktur & arkitektur

**Projektmappe (dit projekt):**

```
dit-projekt/
├── ekte.md                    # projektbeskrivelse + kontekst til LLM (+ goal-byggeresumé)
└── .ekte/                     # ekte's arbejdsmappe (bør gitignores)
    ├── config.yaml            # provider, whitelist, hooks, goal, wiki
    ├── skills/                # egne skills
    ├── memory/                # noter loadet som kontekst
    │   ├── goal-lessons.md    # destillerede /goal-lektioner (bekræftede)
    │   └── goals/journal.jsonl# eval-case-klar udfaldslog (loades ikke)
    ├── plans/                 # godkendte /plan-artefakter
    ├── sessions/              # gemte samtaler (max 3)
    └── worktrees/             # git worktrees til specs
```

**Kildekode (udvalgte pakker):**

```
ekte/
├── cmd/ekte/main.go          # entrypoint: onboarding → agent → TUI; subkommandoer (init, review, verify, ...)
├── internal/
│   ├── agent/                # kerne: ProcessStream → <-chan Event, slash-handlers,
│   │                         #   /plan, /goal-loop (streamGoal), hooks, goaljournal
│   ├── sensor/               # inferentielle sensorer: SecuritySensor + skeptisk IntentSensor
│   ├── review/               # provider-agnostisk sikkerhedsreview (CWE/OWASP)
│   ├── journal/              # append-only, eval-case-klar /goal-udfaldslog
│   ├── orchestrator/         # multi-agent: nedbryd → subagenter → scor → saml
│   ├── provider/             # Provider-interface: anthropic, openai, lmstudio
│   ├── skill/                # markdown-skills + SKILLeton-bibliotek
│   ├── wiki/                 # query, gem, ingest (sandkasse + SSRF-værn)
│   ├── secret/ · consent/ · netsafe/ · container/   # sikkerheds-primitiver
│   └── tui/                  # Bubbletea-præsentationslag (ingen logik)
└── .github/workflows/        # ci.yml, pages.yml, release-please.yml
```

**Lagdeling:** al logik lever i `internal/agent`, der eksponerer
`ProcessStream(input) → <-chan Event`. TUI'en er et tyndt præsentationslag der kun
renderer events. Det gør det muligt at bygge alternative frontends (GUI, web, LSP) ved
at tilføje et nyt `cmd/`-entrypoint, der importerer `internal/agent`.
