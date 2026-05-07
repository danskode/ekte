package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/danskode/ekte/internal/git"
	"github.com/danskode/ekte/internal/provider"
	"github.com/danskode/ekte/internal/session"
	"github.com/danskode/ekte/internal/wiki"
)

type msgResponse struct {
	content    string
	err        error
	forresten  bool
}

type msgStream string
type msgStreamDone struct{}
type msgToolOutput string

type msgWorktreeCreated struct {
	wt  *git.Worktree
	err error
}

type msgWorktreeList struct {
	wts []git.Worktree
	err error
}

type msgWorktreeMerged struct {
	name string
	err  error
}

type msgWorktreeRemoved struct {
	name string
	err  error
}

func chatCmd(p provider.Provider, messages []provider.Message) tea.Cmd {
	return func() tea.Msg {
		resp, err := p.Chat(context.Background(), messages)
		if err != nil {
			return msgResponse{err: err}
		}
		return msgResponse{content: resp.Content}
	}
}

func streamCmd(p provider.Provider, messages []provider.Message) tea.Cmd {
	return func() tea.Msg {
		ch, err := p.Stream(context.Background(), messages)
		if err != nil {
			return msgResponse{err: err}
		}
		var sb strings.Builder
		for chunk := range ch {
			sb.WriteString(chunk)
		}
		return msgResponse{content: sb.String()}
	}
}

func forrestenCmd(p provider.Provider, hist []provider.Message, input string) tea.Cmd {
	msgs := append(hist, provider.Message{Role: "user", Content: input})
	return func() tea.Msg {
		resp, err := p.Chat(context.Background(), msgs)
		if err != nil {
			return msgResponse{err: err, forresten: true}
		}
		return msgResponse{content: resp.Content, forresten: true}
	}
}

func worktreeCreateCmd(repoRoot, name string) tea.Cmd {
	return func() tea.Msg {
		wt, err := git.Create(repoRoot, name)
		return msgWorktreeCreated{wt: wt, err: err}
	}
}

func worktreeListCmd(repoRoot string) tea.Cmd {
	return func() tea.Msg {
		wts, err := git.List(repoRoot)
		return msgWorktreeList{wts: wts, err: err}
	}
}

func worktreeMergeCmd(repoRoot, name string, hooks []string) tea.Cmd {
	return func() tea.Msg {
		err := git.Merge(repoRoot, name, hooks)
		return msgWorktreeMerged{name: name, err: err}
	}
}

func worktreeRemoveCmd(repoRoot, name string) tea.Cmd {
	return func() tea.Msg {
		err := git.Remove(repoRoot, name)
		return msgWorktreeRemoved{name: name, err: err}
	}
}

type msgSessionSaved struct {
	s   *session.Session
	err error
}

type msgSessionList struct {
	sessions []session.Session
	err      error
}

func sessionSaveCmd(dir string, messages []provider.Message) tea.Cmd {
	return func() tea.Msg {
		s, err := session.Save(dir, messages)
		return msgSessionSaved{s: s, err: err}
	}
}

func sessionListCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		sessions, err := session.LoadAll(dir)
		return msgSessionList{sessions: sessions, err: err}
	}
}

type msgWikiResult struct {
	context string
	pages   []wiki.Page
	err     error
}

type msgWikiSaved struct {
	path string
	err  error
}

func wikiQueryCmd(w *wiki.Wiki, p provider.Provider, question string, hist []provider.Message) tea.Cmd {
	return func() tea.Msg {
		ctx, pages, err := w.Query(question)
		if err != nil {
			return msgWikiResult{err: err}
		}
		// send til LLM med wiki-kontekst
		msgs := append([]provider.Message{
			{Role: "system", Content: ctx},
		}, hist...)
		msgs = append(msgs, provider.Message{Role: "user", Content: question})

		resp, err := p.Chat(context.Background(), msgs)
		if err != nil {
			return msgWikiResult{err: err, pages: pages}
		}
		return msgWikiResult{context: resp.Content, pages: pages}
	}
}

func wikiSaveCmd(w *wiki.Wiki, pageType, title, content string) tea.Cmd {
	return func() tea.Msg {
		path, err := w.SavePage(pageType, title, content)
		return msgWikiSaved{path: path, err: err}
	}
}

func renderWorktreeList(wts []git.Worktree) string {
	if len(wts) == 0 {
		return "Ingen aktive worktrees. Brug '/spec <navn>' for at oprette en."
	}
	var sb strings.Builder
	sb.WriteString(styleSlashCmd.Render("Aktive worktrees:\n\n"))
	for _, wt := range wts {
		sb.WriteString(fmt.Sprintf("  %s\n  branch: %s\n  sti: %s\n\n",
			styleSlashCmd.Render(wt.Name),
			styleSystem.Render(wt.Branch),
			styleSystem.Render(wt.Path),
		))
	}
	return strings.TrimRight(sb.String(), "\n")
}
