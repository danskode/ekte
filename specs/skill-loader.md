# Spec: Skill Loader

## Status: draft

## Intent

Læs alle `.md`-filer i `.ekte/skills/` ved opstart, parse YAML frontmatter
og markdown-body, og eksponér skills til TUI. `/skills` åbner et panel hvor
man kan browse og aktivere en skill for den næste prompt.

## Skill-format

```markdown
---
name: skill-builder
version: 1.0.0
description: Opretter nye skills interaktivt
tools: [read_file, write_file, bash]
hooks:
  pre: hooks/validate-skill.sh
  post: hooks/test-skill.sh
tags: [meta, core]
---

# Skill Builder

## Intent
## Steps
## System Prompt Addition
```

## Datastrukturer

```go
type SkillHooks struct {
    Pre  string
    Post string
}

type Skill struct {
    Name        string
    Version     string
    Description string
    Tools       []string
    Hooks       SkillHooks
    Tags        []string
    Body        string   // fuld markdown efter frontmatter
    SystemPromptAddition string // indhold af ## System Prompt Addition
    Path        string   // absolut filsti
}
```

## Loader

- `LoadAll(dir string) ([]Skill, error)` — læser alle `.md`-filer i dir
- Parser YAML frontmatter (mellem første og andet `---`)
- Ekstraherer `## System Prompt Addition`-sektionen fra body
- Fejltolerant: logger advarsel ved ugyldig fil, fortsætter med resten

## Aktivering i TUI

- Én skill kan være aktiv ad gangen
- Aktiv skill injecter `SystemPromptAddition` som system-besked øverst i næste request
- Efter svar: skill nulstilles automatisk
- `/skills` viser liste med navn + description + tags

## Acceptkriterier

- [ ] `Skill`-struct og `LoadAll` i `internal/skill/skill.go`
- [ ] Frontmatter parses korrekt
- [ ] `SystemPromptAddition` ekstraheres
- [ ] `/skills` i TUI viser loadede skills
- [ ] Aktivering injecter system prompt i næste request
- [ ] `skill-builder` skill skrevet i `.ekte/skills/skill-builder.md`
