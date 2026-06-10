package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gyldigSkill = `---
name: tdd
version: 1.0.0
description: Test-drevet udvikling
tools: []
tags: [testing, go]
hooks:
  pre: go vet ./...
---

# TDD

Brødtekst her.

## System Prompt Addition

Skriv altid tests før implementering.
Brug Go's testing-pakke.

## Andet afsnit

Dette hører ikke med i system-prompten.
`

func writeSkill(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestParseFrontmatter(t *testing.T) {
	front, body, err := parseFrontmatter("---\nname: x\n---\nbrødtekst")
	if err != nil {
		t.Fatalf("parseFrontmatter: %v", err)
	}
	if front != "name: x" || body != "brødtekst" {
		t.Errorf("front=%q body=%q", front, body)
	}

	// Uden frontmatter: hele indholdet er body.
	front, body, err = parseFrontmatter("bare tekst")
	if err != nil || front != "" || body != "bare tekst" {
		t.Errorf("uden frontmatter: front=%q body=%q err=%v", front, body, err)
	}

	// Manglende afsluttende --- er en fejl.
	if _, _, err := parseFrontmatter("---\nname: x\nuden slut"); err == nil {
		t.Error("manglende afsluttende --- burde give fejl")
	}
}

func TestExtractSection(t *testing.T) {
	body := "# Titel\n\n## System Prompt Addition\n\nlinje1\nlinje2\n\n## Næste\n\nikke med"
	got := extractSection(body, "System Prompt Addition")
	if got != "linje1\nlinje2" {
		t.Errorf("extractSection = %q", got)
	}
	if got := extractSection(body, "Findes Ikke"); got != "" {
		t.Errorf("ukendt sektion burde give tom streng, fik %q", got)
	}
}

func TestLoadAll(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "tdd.md", gyldigSkill)
	writeSkill(t, dir, "ugyldig.md", "---\nname: [defekt yaml\n---\nkrop")
	writeSkill(t, dir, "ikke-skill.txt", "ignoreres")

	skills, errs := LoadAll(dir)
	if len(errs) != 1 {
		t.Errorf("forventede 1 parsefejl (ugyldig.md), fik %d: %v", len(errs), errs)
	}
	if len(skills) != 1 {
		t.Fatalf("forventede 1 gyldig skill, fik %d", len(skills))
	}
	s := skills[0]
	if s.Name != "tdd" || s.Version != "1.0.0" {
		t.Errorf("frontmatter ikke parset: %+v", s)
	}
	if len(s.Tags) != 2 || s.Tags[0] != "testing" {
		t.Errorf("tags = %v", s.Tags)
	}
	if s.Hooks.Pre != "go vet ./..." {
		t.Errorf("hooks.pre = %q", s.Hooks.Pre)
	}
	if !strings.Contains(s.SystemPromptAddition, "tests før implementering") {
		t.Errorf("SystemPromptAddition = %q", s.SystemPromptAddition)
	}
	if strings.Contains(s.SystemPromptAddition, "hører ikke med") {
		t.Error("SystemPromptAddition indeholder tekst fra næste sektion")
	}
}

func TestLoadAllNavnFraFilnavn(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "min-skill.md", "---\ndescription: uden navn\n---\nkrop")
	skills, errs := LoadAll(dir)
	if len(errs) != 0 || len(skills) != 1 {
		t.Fatalf("skills=%d errs=%v", len(skills), errs)
	}
	if skills[0].Name != "min-skill" {
		t.Errorf("Name = %q, forventet filnavn-fallback 'min-skill'", skills[0].Name)
	}
}

func TestLoadAllManglendeMappe(t *testing.T) {
	skills, errs := LoadAll(filepath.Join(t.TempDir(), "findes-ikke"))
	if skills != nil || errs != nil {
		t.Error("manglende mappe burde give (nil, nil)")
	}
}

func TestLoadAllFromDirsLokalOverskriverGlobal(t *testing.T) {
	global := t.TempDir()
	local := t.TempDir()
	writeSkill(t, global, "tdd.md", "---\nname: tdd\ndescription: global udgave\n---\nkrop")
	writeSkill(t, global, "kun-global.md", "---\nname: kun-global\n---\nkrop")
	writeSkill(t, local, "tdd.md", "---\nname: tdd\ndescription: lokal udgave\n---\nkrop")

	skills, errs := LoadAllFromDirs(global, local)
	if len(errs) != 0 {
		t.Fatalf("uventede fejl: %v", errs)
	}
	if len(skills) != 2 {
		t.Fatalf("forventede 2 skills (dedupliceret), fik %d", len(skills))
	}
	for _, s := range skills {
		if s.Name == "tdd" && s.Description != "lokal udgave" {
			t.Errorf("lokal skill burde overskrive global, fik %q", s.Description)
		}
	}
}

// TestDreamerSkillParses verificerer at repoets egen dreamer-skill kan indlæses.
func TestDreamerSkillParses(t *testing.T) {
	path := filepath.Join("..", "..", ".ekte", "skills", "dreamer.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("dreamer.md ikke til stede")
	}
	dir := filepath.Dir(path)
	skills, errs := LoadAll(dir)
	if len(errs) != 0 {
		t.Fatalf("parsefejl i .ekte/skills/: %v", errs)
	}
	for _, s := range skills {
		if s.Name == "dreamer" {
			if s.SystemPromptAddition == "" {
				t.Error("dreamer mangler System Prompt Addition-sektion")
			}
			return
		}
	}
	t.Error("dreamer-skill ikke fundet i .ekte/skills/")
}
