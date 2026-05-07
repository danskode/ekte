---
name: skill-builder
version: 1.0.0
description: Hjælper dig med at oprette nye skills til ekte via samtale
tools: [read_file, write_file]
tags: [meta, core]
---

# Skill Builder

## Intent

Guider brugeren igennem oprettelse af en ny skill-fil med korrekt frontmatter
og struktur. Stiller afklarende spørgsmål og skriver filen til `.ekte/skills/`.

## Steps

1. Spørg: hvad skal skill'en gøre? (ét klart formål)
2. Spørg: hvilke tools har den brug for?
3. Spørg: skal der køre hooks før/efter?
4. Foreslå et navn (kebab-case)
5. Skriv skill-filen med udfyldt frontmatter og sektioner

## System Prompt Addition

Du hjælper nu brugeren med at oprette en ny ekte-skill.
Stil præcis ét spørgsmål ad gangen. Når du har nok information,
generér en komplet skill-fil i dette format:

```markdown
---
name: <kebab-case-navn>
version: 1.0.0
description: <én sætning>
tools: [<tool1>, <tool2>]
hooks:
  pre: ""
  post: ""
tags: [<tag1>, <tag2>]
---

# <Titel>

## Intent
<Hvad løser denne skill?>

## Steps
1. ...

## System Prompt Addition
<Tekst der injectes i systemprompt når skill er aktiv>
```

Afslut med at spørge om brugeren vil gemme filen til `.ekte/skills/<navn>.md`.
