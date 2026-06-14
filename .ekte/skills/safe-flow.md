---
name: safe-flow
version: 1.0.0
description: Sætter sikre, deterministiske guardrails op — feature-branch-flow, pre-commit/pre-push-tjek og CI-gate — så arbejde ikke ødelægges ved en fejl
tools: [read_file, write_file]
tags: [ci, security, workflow, guardrails]
---

# Safe Flow

## Intent

Hjælper brugeren med at sætte **deterministiske guardrails** op, så det er svært
at ødelægge sit eget arbejde: ingen commits direkte på main, automatiske
feature-branches, lokale tjek før push, og en CI-gate. Målet er at gøre det rigtige
til standardvejen — uden at brugeren skal huske noget.

Princippet er fail-closed automatisering frem for disciplin: en hook der blokerer
er stærkere end en regel man skal huske. Komplementerer [[ci-hardening]] (selve
GitHub Actions) og [[secure-harness]] (sikre CLI-pipelines).

## Steps

1. **Branch-guardrail** — installer en `pre-commit`-hook der afviser commits på
   default-branchen (main/master) og minder om at lave en feature-branch.
2. **Pre-push-gate** — installer en `pre-push`-hook der kører projektets tjek
   (tests + lint/format) og fejler lukket, så intet ubekræftet rammer remote.
3. **CI-gate** — referér [[ci-hardening]] for GitHub Actions (SHA-pinning,
   Dependabot) + anbefal branch protection: krav om PR + grønne checks før merge.
4. **Bekræft før skrivning** — vis hver hook for brugeren, forklar hvad den gør,
   og skriv den kun efter accept. Hooks er ikke-destruktive: de blokerer, de
   ændrer aldrig kode og force-pusher aldrig.

## System Prompt Addition

Du er i "safe-flow" mode. Hjælp brugeren med at etablere deterministiske
guardrails, så arbejde ikke kan gå tabt ved en fejl. Vær konkret, gør det nemt,
og forklar hvert trin kort så brugeren forstår beskyttelsen.

**Ufravigelige principper:**
- **Fail-closed:** hooks/CI blokerer ved tvivl. Et grønt resultat skal være et
  bevidst, deterministisk ja — ikke fravær af nej.
- **Ikke-destruktivt:** hooks må kun *blokere*, aldrig ændre, slette eller
  force-pushe. Foreslå aldrig `git push --force`, `git reset --hard` på delt
  arbejde, eller at omgå en hook med `--no-verify` uden brugerens eksplicitte ja.
- **Idempotent:** at køre opsætningen igen må ikke ødelægge eksisterende hooks —
  tjek først, og gem en eksisterende hook som `*.bak`.
- **Bekræft før skrivning:** vis indholdet, forklar det, skriv efter accept.

**1. Feature-branch som standard.** Arbejd aldrig direkte på default-branchen.
Hvis brugeren er på `main`/`master`, så foreslå en branch (`git switch -c
<beskrivende-navn>`) før ændringer. Installer denne `pre-commit`-hook der gør det
deterministisk:

```sh
#!/bin/sh
# Bloker commits direkte på default-branchen.
branch=$(git rev-parse --abbrev-ref HEAD)
default=$(git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null | sed 's@^origin/@@')
default=${default:-main}
if [ "$branch" = "$default" ]; then
  echo "✗ Commit på '$branch' er blokeret. Lav en feature-branch:" >&2
  echo "    git switch -c min-feature" >&2
  exit 1
fi
```

**2. Pre-push-gate med deterministiske tjek.** Installer en `pre-push`-hook der
kører projektets tests + lint/format og fejler lukket. Tilpas kommandoerne til
projektets sprog (spørg eller detektér: Go → `go vet ./... && go test ./...`,
Node → `npm test`, Python → `pytest` osv.):

```sh
#!/bin/sh
set -e
echo "→ kører tjek før push..."
# Tilpas til projektet:
# go vet ./... && go test ./...
<projektets test- og lint-kommando>
echo "✓ tjek grønne"
```

Tilbyd at tilføje et **agnostisk** sikkerhedsreview-trin der blokerer ved medium+
fund. Hvis brugeren bruger ekte, så brug `ekte review` — den kører reviewet via
den LLM brugeren allerede har valgt (også en lokal model som LM Studio/Ollama),
så der ikke kræves nogen ekstern API-nøgle på maskinen eller i CI:

```sh
# i pre-push, efter tests:
ekte review || { echo "✗ sikkerhedsreview fandt medium+ fund — push afbrudt" >&2; exit 1; }
```

`ekte review` exit'er 0 ved lav risiko, 1 ved medium+ fund, og 2 hvis reviewet
ikke kan køres/fortolkes (**fail-closed** — blokerer, så et uforståeligt svar
aldrig stilles lig grønt lys; opt-out med `--allow-failopen` for upålidelige
lokale modeller). For andre setups: se [[security-review]].

**3. CI-gate + branch protection.** Henvis til [[ci-hardening]] for en
SHA-pinnet GitHub Actions-workflow + Dependabot. Anbefal derefter branch
protection på default-branchen: kræv pull request, kræv at status-checks
(CI + tests) er grønne, og forbyd force-push. Det gør guardrailen gældende også
serverside, ikke kun lokalt.

**4. Gør det nemt.** Saml det til ét guidet flow. Efter opsætning: vis brugeren
hvordan en typisk cyklus ser ud (branch → commit → push → PR → grøn CI → merge),
og bekræft at hver hook er eksekverbar (`chmod +x`). Mind om at hooks er lokale
(deles ikke via git) — så enten committes de til en `hooks/`-mappe med et
`git config core.hooksPath hooks`-skift, eller geninstalleres pr. klon.
