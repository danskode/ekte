package consent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsPrivateURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"http://localhost:11434/v1", true},
		{"http://ip6-localhost:1234", true},
		{"http://127.0.0.1:11434/v1", true},
		{"http://[::1]:11434/v1", true},
		{"http://10.0.0.5:8080/v1", true},
		{"http://192.168.1.10:1234/v1", true},
		{"http://172.16.0.1/v1", true},
		{"http://169.254.1.1/v1", true}, // link-local
		{"https://api.openai.com/v1", false},
		{"https://api.anthropic.com", false},
		{"http://8.8.8.8/v1", false},
		{"", false},
		{"::ikke en url::", false},
		// Hostnavn der ikke er en IP regnes ikke som privat her —
		// DNS-resolution håndteres af DialContext-tjekket i provider-laget.
		{"http://min-interne-server.lan:1234", false},
	}
	for _, c := range cases {
		if got := IsPrivateURL(c.url); got != c.want {
			t.Errorf("IsPrivateURL(%q) = %v, forventet %v", c.url, got, c.want)
		}
	}
}

func TestGrantOgGranted(t *testing.T) {
	dir := t.TempDir()
	url := "http://localhost:11434/v1"

	if Granted(dir, url) {
		t.Fatal("ingen samtykkefil — Granted burde være false")
	}
	if err := Grant(dir, url); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if !Granted(dir, url) {
		t.Error("samtykke blev ikke fundet efter Grant")
	}

	// Præcis-match: anden port, sti eller skema er IKKE dækket.
	for _, other := range []string{
		"http://localhost:11435/v1",
		"http://localhost:11434/v2",
		"https://localhost:11434/v1",
		"http://127.0.0.1:11434/v1",
	} {
		if Granted(dir, other) {
			t.Errorf("Granted(%q) burde være false — samtykke er pr. præcis URL", other)
		}
	}

	// Trimning: omkringliggende whitespace ændrer ikke identiteten.
	if !Granted(dir, "  "+url+"  ") {
		t.Error("trimmet URL burde matche gemt samtykke")
	}
}

func TestGrantIdempotent(t *testing.T) {
	dir := t.TempDir()
	url := "http://localhost:1234/v1"
	for i := 0; i < 3; i++ {
		if err := Grant(dir, url); err != nil {
			t.Fatal(err)
		}
	}
	f := load(dir)
	if len(f.LocalProviders) != 1 {
		t.Errorf("gentaget Grant gav %d poster, forventet 1", len(f.LocalProviders))
	}
}

func TestGrantTomURLSkriverIkke(t *testing.T) {
	dir := t.TempDir()
	if err := Grant(dir, "   "); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); !os.IsNotExist(err) {
		t.Error("tom URL burde ikke oprette samtykkefil")
	}
	if Granted(dir, "") {
		t.Error("tom URL burde aldrig være granted")
	}
}

func TestFilrettigheder(t *testing.T) {
	dir := t.TempDir()
	if err := Grant(dir, "http://localhost:11434/v1"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("consent.yaml har mode %o, forventet 0600", perm)
	}
}

// TestKorruptFilFailerClosed: en ulæselig samtykkefil må aldrig give samtykke.
func TestKorruptFilFailerClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{::ikke yaml::"), 0600); err != nil {
		t.Fatal(err)
	}
	if Granted(dir, "http://localhost:11434/v1") {
		t.Error("korrupt fil burde give Granted=false (fail closed)")
	}
	// Grant ovenpå korrupt fil må stadig fungere.
	if err := Grant(dir, "http://localhost:11434/v1"); err != nil {
		t.Fatalf("Grant ovenpå korrupt fil: %v", err)
	}
	if !Granted(dir, "http://localhost:11434/v1") {
		t.Error("Grant burde reparere/overskrive korrupt fil")
	}
}

// TestProjektmappeIsolation dokumenterer kerne-garantien: samtykke læses kun
// fra den mappe kalderen angiver. En consent.yaml plantet i et projekts
// .ekte/ har ingen effekt, fordi cmd/ekte altid spørger med ~/.ekte.
func TestProjektmappeIsolation(t *testing.T) {
	globalDir := t.TempDir()
	projektDir := t.TempDir() // simulerer et klonet repos .ekte/

	// Angriber planter samtykke i projektmappen (statisk fixture).
	planted := "local_providers:\n  - url: http://localhost:11434/v1\n    granted: \"2026-01-01\"\n"
	if err := os.WriteFile(filepath.Join(projektDir, fileName), []byte(planted), 0644); err != nil {
		t.Fatal(err)
	}

	if Granted(globalDir, "http://localhost:11434/v1") {
		t.Error("samtykke plantet i projektmappe påvirkede global Granted")
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("EKTE_ALLOW_LOCAL_PROVIDER", "")
	if EnvOverride() {
		t.Error("tom env-var burde ikke være override")
	}
	t.Setenv("EKTE_ALLOW_LOCAL_PROVIDER", "1")
	if !EnvOverride() {
		t.Error("EKTE_ALLOW_LOCAL_PROVIDER=1 burde være override")
	}
}

func TestAllowLocalHooks(t *testing.T) {
	t.Setenv("EKTE_ALLOW_LOCAL_HOOKS", "")
	if AllowLocalHooks() {
		t.Error("tom env-var burde ikke tillade lokale hooks")
	}
	t.Setenv("EKTE_ALLOW_LOCAL_HOOKS", "1")
	if !AllowLocalHooks() {
		t.Error("EKTE_ALLOW_LOCAL_HOOKS=1 burde tillade lokale hooks")
	}
	// Provider-overriden må IKKE samtidig åbne for hooks (ingen scope-overload).
	t.Setenv("EKTE_ALLOW_LOCAL_HOOKS", "")
	t.Setenv("EKTE_ALLOW_LOCAL_PROVIDER", "1")
	if AllowLocalHooks() {
		t.Error("EKTE_ALLOW_LOCAL_PROVIDER må ikke tillade lokale hooks")
	}
}

func TestGrantHookOgGrantedHook(t *testing.T) {
	dir := t.TempDir()
	cmd := "mvn -q compile"

	if GrantedHook(dir, cmd) {
		t.Fatal("ingen samtykkefil — GrantedHook burde være false")
	}
	if err := GrantHook(dir, cmd); err != nil {
		t.Fatalf("GrantHook: %v", err)
	}
	if !GrantedHook(dir, cmd) {
		t.Error("hook-samtykke blev ikke fundet efter GrantHook")
	}

	// Match er pr. præcis KOMMANDO: en ændret kommando (selv let) er IKKE
	// dækket — så en klonet config ikke kan genbruge et betroet hook-navn til
	// en anden kommando. (Strengene her sammenlignes kun, køres aldrig.)
	for _, other := range []string{
		"mvn -q compile ; echo overskrevet",
		"mvn compile",
		"npm run build",
	} {
		if GrantedHook(dir, other) {
			t.Errorf("GrantedHook(%q) burde være false — samtykke er pr. præcis kommando", other)
		}
	}

	// Trimning ændrer ikke identiteten.
	if !GrantedHook(dir, "  "+cmd+"  ") {
		t.Error("trimmet kommando burde matche gemt hook-samtykke")
	}
}

func TestGrantHookIdempotent(t *testing.T) {
	dir := t.TempDir()
	cmd := "go test ./..."
	for i := 0; i < 3; i++ {
		if err := GrantHook(dir, cmd); err != nil {
			t.Fatal(err)
		}
	}
	f := load(dir)
	if len(f.Hooks) != 1 {
		t.Errorf("gentaget GrantHook gav %d poster, forventet 1", len(f.Hooks))
	}
}

func TestGrantHookTomKommandoSkriverIkke(t *testing.T) {
	dir := t.TempDir()
	if err := GrantHook(dir, "   "); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); !os.IsNotExist(err) {
		t.Error("tom kommando burde ikke oprette samtykkefil")
	}
	if GrantedHook(dir, "") {
		t.Error("tom kommando burde aldrig være granted")
	}
}

// TestHookProjektmappeIsolation: en hook-samtykkefil plantet i et klonet repos
// .ekte/ må ikke påvirke global GrantedHook — samme garanti som for providere.
// Den plantede kommando er en ufarlig markør; den sammenlignes kun som streng.
func TestHookProjektmappeIsolation(t *testing.T) {
	globalDir := t.TempDir()
	projektDir := t.TempDir()

	planted := "hooks:\n  - url: utroet-hook-markør\n    granted: \"2026-01-01\"\n"
	if err := os.WriteFile(filepath.Join(projektDir, fileName), []byte(planted), 0644); err != nil {
		t.Fatal(err)
	}
	if GrantedHook(globalDir, "utroet-hook-markør") {
		t.Error("hook-samtykke plantet i projektmappe påvirkede global GrantedHook")
	}
}
