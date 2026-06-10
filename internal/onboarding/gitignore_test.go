package onboarding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitignoreOpretterFil(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureGitignore(dir); err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf(".gitignore ikke oprettet: %v", err)
	}
	for _, e := range gitignoreEntries {
		if !strings.Contains(string(data), e+"\n") {
			t.Errorf(".gitignore mangler %q", e)
		}
	}
}

func TestEnsureGitignoreBevarerEksisterende(t *testing.T) {
	dir := t.TempDir()
	orig := "node_modules/\n*.log"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(orig), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGitignore(dir); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.HasPrefix(string(data), "node_modules/\n*.log\n") {
		t.Errorf("eksisterende indhold ændret:\n%s", data)
	}
	if !strings.Contains(string(data), ".ekte/config.yaml\n") {
		t.Error("ekte-poster ikke tilføjet")
	}
}

func TestEnsureGitignoreIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureGitignore(dir); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err := EnsureGitignore(dir); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(first) != string(second) {
		t.Errorf("gentaget kald ændrede filen:\nfør:\n%s\nefter:\n%s", first, second)
	}
}

func TestEnsureGitignoreKunManglende(t *testing.T) {
	dir := t.TempDir()
	// To poster findes allerede — kun resten må tilføjes, uden dubletter.
	orig := ".ekte/config.yaml\n.ekte/sessions/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(orig), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGitignore(dir); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if strings.Count(string(data), ".ekte/config.yaml") != 1 {
		t.Error(".ekte/config.yaml duplikeret")
	}
	if !strings.Contains(string(data), ".ekte/memory/") {
		t.Error("manglende post .ekte/memory/ ikke tilføjet")
	}
}
