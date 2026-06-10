package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danskode/ekte/internal/provider"
)

func TestSanitizeDisplay(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"almindelig tekst", "stille-ravn", "stille-ravn"},
		{"danske tegn bevares", "blå-bjørn æøå", "blå-bjørn æøå"},
		{"ANSI-escape fjernes", "evil\x1b[31mrød\x1b[0m", "evil[31mrød[0m"},
		{"linjeskift fjernes", "linje1\nlinje2\rlinje3", "linje1linje2linje3"},
		{"nulbyte og DEL fjernes", "a\x00b\x7fc", "abc"},
		{"OSC-titel-injektion fjernes", "x\x1b]0;evil\x07y", "x]0;evily"},
		{"tom streng", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SanitizeDisplay(c.in); got != c.want {
				t.Errorf("SanitizeDisplay(%q) = %q, forventet %q", c.in, got, c.want)
			}
		})
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	msgs := []provider.Message{
		{Role: "user", Content: "hej ekte, hvad er en mutex?"},
		{Role: "assistant", Content: "En mutex er en lås..."},
	}

	s, err := Save(dir, msgs, "")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if s.Name == "" {
		t.Error("Save burde generere et navn når name er tom")
	}
	if s.Title != "hej ekte, hvad er en mutex?" {
		t.Errorf("Title = %q, forventet første user-besked", s.Title)
	}

	// Sessionsfiler indeholder fuld historik — skal være 0600.
	info, err := os.Stat(filepath.Join(dir, s.ID+".json"))
	if err != nil {
		t.Fatalf("sessionsfil ikke fundet: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("sessionsfil har mode %o, forventet 0600", perm)
	}

	loaded, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("LoadAll gav %d sessioner, forventet 1", len(loaded))
	}
	if len(loaded[0].Messages) != 2 || loaded[0].Messages[0].Content != msgs[0].Content {
		t.Error("beskeder overlevede ikke gem/indlæs-rundturen")
	}
}

func TestSaveSanitizesNameAndTitle(t *testing.T) {
	dir := t.TempDir()
	msgs := []provider.Message{
		{Role: "user", Content: "titel\x1b[2Jmed\nstyretegn"},
	}
	s, err := Save(dir, msgs, "navn\x1b]0;evil\x07her")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	for _, field := range []string{s.Name, s.Title} {
		if strings.ContainsAny(field, "\x1b\n\r\x07") {
			t.Errorf("felt indeholder stadig styretegn: %q", field)
		}
	}
}

// TestLoadAllSanitizesHostileFile bruger en statisk JSON-fixture der efterligner
// en manipuleret sessionsfil (CWE-150). Intet i fixturen eksekveres — testen
// verificerer kun at styretegn er strippet fra felter der vises i terminalen.
// \u001b (ESC) og \u0007 (BEL) er gyldige JSON-escapes og dekodes til rå
// styretegn af json.Unmarshal.
func TestLoadAllSanitizesHostileFile(t *testing.T) {
	dir := t.TempDir()
	hostile := `{
  "id": "20260101-120000",
  "name": "evil\u001b]0;pwned\u0007name",
  "title": "titel\u001b[31m\nmed injektion",
  "saved_at": "2026-01-01T12:00:00Z",
  "messages": []
}`
	if err := os.WriteFile(filepath.Join(dir, "20260101-120000.json"), []byte(hostile), 0600); err != nil {
		t.Fatal(err)
	}

	sessions, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("forventede 1 session, fik %d", len(sessions))
	}
	s := sessions[0]
	if strings.ContainsAny(s.Name, "\x1b\x07\n\r") {
		t.Errorf("Name indeholder stadig styretegn efter LoadAll: %q", s.Name)
	}
	if strings.ContainsAny(s.Title, "\x1b\x07\n\r") {
		t.Errorf("Title indeholder stadig styretegn efter LoadAll: %q", s.Title)
	}
	// RenderList viser Name/Title råt — heller ikke dén vej må styretegn slippe ud.
	if out := RenderList(sessions); strings.ContainsAny(out, "\x1b\x07") {
		t.Error("RenderList-output indeholder styretegn")
	}
}

func TestLoadAllSkipsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "korrupt.json"), []byte("{ikke json"), 0600); err != nil {
		t.Fatal(err)
	}
	sessions, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll burde ikke fejle på korrupt fil: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("korrupt fil burde springes over, fik %d sessioner", len(sessions))
	}
}

func TestLoadAllMissingDir(t *testing.T) {
	sessions, err := LoadAll(filepath.Join(t.TempDir(), "findes-ikke"))
	if err != nil || sessions != nil {
		t.Errorf("manglende mappe burde give (nil, nil), fik (%v, %v)", sessions, err)
	}
}

func TestPruneOldBeholderNyeste(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, time.Now().Format("20060102-1504")+string(rune('0'+i))+".json")
		if err := os.WriteFile(name, []byte(`{"id":"x","messages":[]}`), 0600); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(name, mt, mt); err != nil {
			t.Fatal(err)
		}
	}

	if err := pruneOld(dir); err != nil {
		t.Fatalf("pruneOld: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != maxSessions {
		t.Errorf("pruneOld efterlod %d filer, forventet %d", len(entries), maxSessions)
	}
}

func TestFindByName(t *testing.T) {
	dir := t.TempDir()
	s, err := Save(dir, []provider.Message{{Role: "user", Content: "x"}}, "stille-ravn")
	if err != nil {
		t.Fatal(err)
	}

	found, err := FindByName(dir, "  STILLE-RAVN  ")
	if err != nil {
		t.Fatalf("FindByName: %v", err)
	}
	if found == nil || found.ID != s.ID {
		t.Error("FindByName burde finde session case-insensitivt og trimmet")
	}

	missing, err := FindByName(dir, "findes-ikke")
	if err != nil || missing != nil {
		t.Errorf("ukendt navn burde give (nil, nil), fik (%v, %v)", missing, err)
	}

	empty, err := FindByName(dir, "   ")
	if err != nil || empty != nil {
		t.Error("tomt navn burde give (nil, nil)")
	}
}

func TestDeriveTitle(t *testing.T) {
	long := strings.Repeat("a", 80)
	cases := []struct {
		name string
		msgs []provider.Message
		want string
	}{
		{"første user-besked", []provider.Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "min titel"},
		}, "min titel"},
		{"lang titel afkortes", []provider.Message{
			{Role: "user", Content: long},
		}, long[:57] + "..."},
		{"ingen user-beskeder", []provider.Message{
			{Role: "assistant", Content: "kun svar"},
		}, "Tom session"},
		{"tom historik", nil, "Tom session"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := deriveTitle(c.msgs); got != c.want {
				t.Errorf("deriveTitle = %q, forventet %q", got, c.want)
			}
		})
	}
}
