# Spec: Wiki Submodule Setup

## Status: draft

## Intent

Gør det nemt for enhver udvikler at sætte en personlig wiki op uden git-ekspertviden.
`ekte init` stiller tre spørgsmål og kloner template-repo'et til den rigtige sted.
`/wiki` giver query-on-demand mod wikien under en session.

## Opsætningsflow (`ekte init`)

```
Vil du bruge en personlig wiki? [j/n]
Skal wikien være global (delt på tværs af projekter) eller lokal (kun dette projekt)?
  1. Global (~/.ekte/wiki)
  2. Lokal (.ekte/wiki)
Har du et eksisterende wiki-repo? [j/n]
  → Ja: indtast git URL
  → Nej: kloner danskode/simple-wiki som template
```

Config gemmes i `.ekte/config.yaml`:
```yaml
wiki:
  enabled: true
  path: ~/.ekte/wiki
```

## Query-flow (`/wiki "spørgsmål"`)

1. Læs `wiki/index.md` → identificér relevante kategorier
2. Kør `tools/search.sh "<keywords>"` → find relevante filer
3. Læs de matchende sider
4. Send til LLM med spørgsmålet som kontekst
5. Svar vises i samtalen

## Gem ny viden

- Efter `/forresten`-svar: harness foreslår at gemme som wiki-side
- Bruger bekræfter → LLM genererer wiki-side med korrekt frontmatter
- Siden gemmes i korrekt undermappe efter type

## Datastrukturer

```go
type WikiConfig struct {
    Enabled bool   `yaml:"enabled"`
    Path    string `yaml:"path"`
}

type QueryResult struct {
    Pages   []WikiPage
    Context string
}

type WikiPage struct {
    Path    string
    Content string
}
```

## Acceptkriterier

- [ ] `ekte init` stiller spørgsmål og kloner template
- [ ] Config gemmes korrekt med expanderet sti
- [ ] `/wiki` kører index → search → load → LLM
- [ ] `search.sh` bruges til keyword-søgning
- [ ] Forslag om at gemme viden efter `/forresten`
- [ ] Fungerer uden wiki (graceful degradation)
