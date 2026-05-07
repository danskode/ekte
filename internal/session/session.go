package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danskode/ekte/internal/provider"
)

const maxSessions = 3

type Session struct {
	ID       string             `json:"id"`
	Title    string             `json:"title"`
	SavedAt  time.Time          `json:"saved_at"`
	Messages []provider.Message `json:"messages"`
}

func Save(dir string, messages []provider.Message) (*Session, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	now := time.Now()
	s := &Session{
		ID:       now.Format("20060102-150405"),
		Title:    deriveTitle(messages),
		SavedAt:  now,
		Messages: messages,
	}

	path := filepath.Join(dir, s.ID+".json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
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

	for _, f := range files[maxSessions:] {
		_ = os.Remove(filepath.Join(dir, f.Name()))
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
	sb.WriteString("Gemte sessioner — skriv '/resume <nummer>' for at indlæse:\n\n")
	for i, s := range sessions {
		sb.WriteString(fmt.Sprintf("  %d. %s\n     %s · %d beskeder\n\n",
			i+1,
			s.Title,
			s.SavedAt.Format("02 Jan 2006 15:04"),
			len(s.Messages),
		))
	}
	return strings.TrimRight(sb.String(), "\n")
}
