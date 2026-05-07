# Spec: Onboarding Flow

## Status: draft

## Intent

Når `ekte` køres i en mappe for første gang, guides brugeren igennem
tre trin inden TUI starter: trust-check, ekte.md-oprettelse og
provider-bekræftelse. Målet er at nye brugere er kørende på under
2 minutter uden at kende til YAML eller git.

## Flow

```
[ ekte startes ]
        ↓
Eksisterer .ekte/ ?
  Nej → Onboarding
  Ja  → Start TUI direkte

Onboarding:
  1. Trust-check
     "Stoler du på koden i denne mappe? [j/n]"
     → Nej: afslut uden at gøre noget

  2. Initialiser .ekte/ og config
     Kopier config.yaml.example → config.yaml hvis ikke findes

  3. ekte.md check
     "Der er ingen ekte.md endnu — det er din projektkontekst.
      Vil du oprette den nu? [j/n]"
     → Ja: kør PRD-guide interaktivt
     → Nej: fortsæt

  4. Start TUI
```

## ekte.md format

```markdown
# <projektnavn>

## Hvad er dette projekt?
<én paragraf>

## Teknisk stack
<liste>

## Mål
<liste>

## Konventioner
<liste>
```

Filen loades som system-kontekst ved opstart (ligesom CLAUDE.md i Claude Code).

## PRD-guide (interaktiv i terminal)

Fem spørgsmål stilles ét ad gangen:
1. Hvad hedder projektet?
2. Hvad løser det? (ét klart problem)
3. Hvem er brugerne?
4. Hvad er de tre vigtigste features i v1?
5. Hvilken teknisk stack bruger du?

Svarene samles til en `ekte.md` og brugeren får forslag om
at lave en spec for første feature med `/spec`.

## Provider-check

Hvis `config.yaml` ikke har en API-nøgle og `OPENAI_API_KEY` /
`ANTHROPIC_API_KEY` ikke er sat i env:
→ vis besked: "Husk at sætte din API-nøgle i .ekte/config.yaml"

## Acceptkriterier

- [ ] Onboarding kører kun ved første opstart (ingen `.ekte/`)
- [ ] Trust-check afslutter rent ved "nej"
- [ ] PRD-guide skriver korrekt `ekte.md`
- [ ] `ekte.md` loades som system-kontekst i TUI
- [ ] Provider-check advarer hvis ingen nøgle er sat
- [ ] Hele flowet tager under 2 minutter for en ny bruger
