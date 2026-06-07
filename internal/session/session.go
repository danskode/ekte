package session

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/danskode/ekte/internal/provider"
)

const maxSessions = 3

type Session struct {
	ID string `json:"id"`
	// Name er et mindeværdigt visningsnavn (fx "stille-ravn"), genereret med
	// math/rand og brugt udelukkende til at lade brugeren genkende og genoptage
	// sine egne sessioner. Det er IKKE et adgangs- eller sikkerhedstoken — der
	// kræves ingen hemmelighed for at gætte eller kollidere med det, og det må
	// aldrig bruges til autentificering eller adgangskontrol.
	Name     string             `json:"name"`
	Title    string             `json:"title"`
	SavedAt  time.Time          `json:"saved_at"`
	Messages []provider.Message `json:"messages"`
}

// controlChars matcher styretegn (bl.a. \n, \r, ANSI-escapes via \x1b) i
// felter der stammer fra gemte sessionsfiler. Sådanne filer kan i teorien
// være plantet eller manipuleret (de kan ligge repo-lokalt), og Name/Title
// vises råt i terminalen flere steder (RenderList, "Session genoptaget: …",
// afslutningsnoter). Uden denne rensning kunne en ondsindet fil injicere
// terminal-escape-sekvenser eller forfalske visningsoutput (CWE-150).
var controlChars = regexp.MustCompile(`[\x00-\x1f\x7f]`)

// SanitizeDisplay fjerner styretegn fra strenge der senere vises i terminalen
// (Name/Title — i RenderList, "Session genoptaget"-besked, exit-noter osv.).
// Eksporteret så ALLE konstruktionsveje for en Session — frisk gemt via Save
// og genindlæst via LoadAll — bruger samme rensning og ingen kald glemmer det.
func SanitizeDisplay(s string) string {
	return controlChars.ReplaceAllString(s, "")
}

// nameAdjectives og nameNouns danner mindeværdige session-navne som "stille-ravn".
var nameAdjectives = []string{
	"stille", "rolig", "klar", "varm", "lys", "mørk", "blid", "glad",
	"modig", "snedig", "venlig", "frisk", "kvik", "sej", "skarp",
	"blå", "grøn", "gylden", "vild", "kold",
}

var nameNouns = []string{
	"ravn", "ulv", "ræv", "ørn", "bjørn", "fugl", "fisk", "sky",
	"sten", "skov", "flod", "bjerg", "stjerne", "måne", "sol",
	"vind", "bølge", "gnist", "due", "hare",
}

func randomName(taken map[string]bool) string {
	for i := 0; i < 30; i++ {
		adj := nameAdjectives[rand.Intn(len(nameAdjectives))]
		noun := nameNouns[rand.Intn(len(nameNouns))]
		name := adj + "-" + noun
		if !taken[name] {
			return name
		}
	}
	return fmt.Sprintf("session-%d", time.Now().Unix())
}

// Save gemmer en session. Hvis name er tom, genereres et mindeværdigt navn
// (fx "stille-ravn") der ikke kolliderer med eksisterende gemte sessioner.
func Save(dir string, messages []provider.Message, name string) (*Session, error) {
	// Sessionsfiler indeholder den fulde samtalehistorik og bruges desuden til
	// opslag via et lavt-entropi visningsnavn (FindByName, uden ejerskabstjek).
	// 0700/0600 sikrer at kun ejeren kan læse/liste dem på delte systemer —
	// så et forudsigeligt navn ikke i sig selv bliver en adgangsvej til en
	// anden brugers historik.
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	if name == "" {
		existing, _ := LoadAll(dir)
		taken := make(map[string]bool, len(existing))
		for _, s := range existing {
			taken[s.Name] = true
		}
		name = randomName(taken)
	}

	now := time.Now()
	s := &Session{
		ID: now.Format("20060102-150405"),
		// Saneres her — ikke kun ved LoadAll — så den FRISKE struct Save
		// returnerer (brugt direkte i exit-noter mv. uden om disk-turen)
		// aldrig kan bære rå styretegn videre til terminalen (CWE-150).
		// 'name' kan stamme fra brugerstyret '/navngiv'-input, og Title
		// afledes af brugerens første beskedindhold.
		Name:     SanitizeDisplay(name),
		Title:    SanitizeDisplay(deriveTitle(messages)),
		SavedAt:  now,
		Messages: messages,
	}

	path := filepath.Join(dir, s.ID+".json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}

	if err := pruneOld(dir); err != nil {
		return nil, err
	}
	return s, nil
}

func LoadAll(dir string) ([]Session, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var sessions []Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		s.Name = SanitizeDisplay(s.Name)
		s.Title = SanitizeDisplay(s.Title)
		sessions = append(sessions, s)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].SavedAt.After(sessions[j].SavedAt)
	})
	if len(sessions) > maxSessions {
		sessions = sessions[:maxSessions]
	}
	return sessions, nil
}

// FindByName slår en gemt session op via dens (case-insensitive) navn.
// Returnerer nil, nil hvis ingen session med det navn findes.
func FindByName(dir, name string) (*Session, error) {
	sessions, err := LoadAll(dir)
	if err != nil {
		return nil, err
	}
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return nil, nil
	}
	for i := range sessions {
		if strings.ToLower(sessions[i].Name) == target {
			return &sessions[i], nil
		}
	}
	return nil, nil
}

func pruneOld(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var files []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			files = append(files, e)
		}
	}

	sort.Slice(files, func(i, j int) bool {
		ii, _ := files[i].Info()
		jj, _ := files[j].Info()
		return ii.ModTime().After(jj.ModTime())
	})

	if len(files) > maxSessions {
		for _, f := range files[maxSessions:] {
			_ = os.Remove(filepath.Join(dir, f.Name()))
		}
	}
	return nil
}

func deriveTitle(messages []provider.Message) string {
	for _, m := range messages {
		if m.Role == "user" && m.Content != "" {
			title := m.Content
			if len(title) > 60 {
				title = title[:57] + "..."
			}
			return title
		}
	}
	return "Tom session"
}

func RenderList(sessions []Session) string {
	if len(sessions) == 0 {
		return "Ingen gemte sessioner."
	}
	var sb strings.Builder
	sb.WriteString("Gemte sessioner — skriv '/resume <nummer>' for at indlæse, eller 'ekte <navn>' fra terminalen:\n\n")
	for i, s := range sessions {
		sb.WriteString(fmt.Sprintf("  %d. %s  (%s)\n     %s · %d beskeder · fortsæt: ekte %s\n\n",
			i+1,
			s.Title,
			s.Name,
			s.SavedAt.Format("02 Jan 2006 15:04"),
			len(s.Messages),
			s.Name,
		))
	}
	return strings.TrimRight(sb.String(), "\n")
}
