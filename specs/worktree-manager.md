# Spec: Git Worktree Manager

## Status: draft

## Intent

Automatiser git worktree-workflow så udvikleren aldrig behøver køre
`git checkout -b` manuelt. `/spec <navn>` opretter spec-fil og worktree
i ét. Merge-flow kører hooks som gates.

## Kommandoer

| Kommando | Handling |
|---|---|
| `/spec <navn>` | Opret spec + worktree + branch |
| `/spec list` | Vis aktive worktrees og deres status |
| `/spec merge <navn>` | Kør hooks → merge til main → ryd op |
| `/spec remove <navn>` | Slet worktree og branch uden merge |

## Impl

```go
type Worktree struct {
    Name   string  // = spec-filnavn uden .md
    Branch string  // = "feature/<navn>"
    Path   string  // absolut sti til worktree-mappe
    Spec   string  // absolut sti til spec-fil
}

func Create(repoRoot, name string) (*Worktree, error)
func List(repoRoot string) ([]Worktree, error)
func Merge(repoRoot, name string, hooks []string) error
func Remove(repoRoot, name string) error
```

## Navnekonvention

- Spec-fil: `specs/<navn>.md`
- Branch: `feature/<navn>`
- Worktree-mappe: `.ekte/worktrees/<navn>`

## Sikkerhed

- Merge kører altid hooks i `.ekte/hooks/` som gates (afvises hvis hook fejler)
- `remove` kræver eksplicit bekræftelse fra bruger (ikke automatisk)
- Ingen force-push, ingen `--no-verify`

## Acceptkriterier

- [ ] `Create` opretter spec-fil, branch og worktree
- [ ] `List` viser navn, branch og sti
- [ ] `Merge` kører hooks og merger til main
- [ ] `Remove` sletter worktree og branch
- [ ] Graceful fejl hvis ikke i git-repo
- [ ] `/spec` i TUI kalder manager korrekt
