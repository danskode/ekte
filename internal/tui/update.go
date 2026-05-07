package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/danskode/ekte/internal/provider"
	"github.com/danskode/ekte/internal/session"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		if !m.ready {
			m.conversation = viewport.New(msg.Width-4, msg.Height-10)
			m.toolPanel = viewport.New(toolPanelWidth-4, msg.Height-10)
			m.input.SetWidth(msg.Width - 4)
			m.ready = true
		} else {
			m.conversation.Width = msg.Width - 4
			m.conversation.Height = msg.Height - 10
			m.input.SetWidth(msg.Width - 4)
		}

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyUp:
			if !m.input.Focused() {
				break
			}
			// history navigation kun når cursor er på første linje
			if m.input.Line() == 0 {
				if m.historyIdx == -1 {
					m.savedDraft = m.input.Value()
				}
				if m.historyIdx < len(m.history)-1 {
					m.historyIdx++
					m.input.SetValue(m.history[len(m.history)-1-m.historyIdx])
					m.input.CursorEnd()
				}
				return m, nil
			}

		case tea.KeyDown:
			if m.historyIdx >= 0 {
				m.historyIdx--
				if m.historyIdx == -1 {
					m.input.SetValue(m.savedDraft)
				} else {
					m.input.SetValue(m.history[len(m.history)-1-m.historyIdx])
				}
				m.input.CursorEnd()
				return m, nil
			}

		case tea.KeyEnter:
			val := strings.TrimSpace(m.input.Value())
			if val == "" {
				return m, nil
			}

			m.input.Reset()
			m.historyIdx = -1
			m.savedDraft = ""

			// gem i history
			if len(m.history) == 0 || m.history[len(m.history)-1] != val {
				m.history = append(m.history, val)
			}

			// slash command?
			if strings.HasPrefix(val, "/") {
				result := m.handleSlash(val)
				if result.handled {
					if result.output != "" {
						m.messages = append(m.messages, provider.Message{
							Role:    "system",
							Content: result.output,
						})
						m.conversation.SetContent(m.conversationContent())
						m.conversation.GotoBottom()
					}
					return m, result.cmd
				}
			}

			// normal besked til provider
			m.messages = append(m.messages, provider.Message{
				Role:    "user",
				Content: val,
			})
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()

			var cmd tea.Cmd
			if m.provider != nil {
				msgs := m.messagesWithActiveSkill()
				cmd = streamCmd(m.provider, msgs)
			}
			m.ClearActiveSkill()
			return m, cmd
		}

	case msgResponse:
		if msg.err != nil {
			m.appendSystem("Fejl: " + msg.err.Error())
		} else {
			isForresten := msg.forresten
			if isForresten {
				m.forrestenHist = append(m.forrestenHist,
					provider.Message{Role: "assistant", Content: msg.content},
				)
				m.appendSystem(styleAssistant.Render("forresten → ") + msg.content)
				if m.wiki != nil {
					m.pendingWikiSave = msg.content
					m.appendSystem(styleSystem.Render("Vil du gemme dette i din wiki? Skriv '/wiki gem <titel>' eller ignorer."))
				}
			} else {
				m.messages = append(m.messages, provider.Message{Role: "assistant", Content: msg.content})
				total := 0
				for _, msg := range m.messages {
					total += len(msg.Content) / 4
				}
				m.tokenCount = total
				m.conversation.SetContent(m.conversationContent())
				m.conversation.GotoBottom()
			}
		}

	case msgWorktreeCreated:
		var content string
		if msg.err != nil {
			content = styleError.Render("Fejl: " + msg.err.Error())
		} else {
			content = styleSuccess.Render("✓ Worktree oprettet: "+msg.wt.Name) +
				"\n  branch: " + styleSystem.Render(msg.wt.Branch) +
				"\n  spec:   " + styleSystem.Render(msg.wt.Spec) +
				"\n  sti:    " + styleSystem.Render(msg.wt.Path)
		}
		m.appendSystem(content)

	case msgWorktreeList:
		var content string
		if msg.err != nil {
			content = styleError.Render("Fejl: " + msg.err.Error())
		} else {
			content = renderWorktreeList(msg.wts)
		}
		m.appendSystem(content)

	case msgWorktreeMerged:
		var content string
		if msg.err != nil {
			content = styleError.Render("Merge fejlede: " + msg.err.Error())
		} else {
			content = styleSuccess.Render("✓ Merget og ryddet op: " + msg.name)
		}
		m.appendSystem(content)

	case msgSessionSaved:
		if msg.err != nil {
			m.appendSystem(styleError.Render("Gem fejlede: " + msg.err.Error()))
		} else {
			m.appendSystem(styleSuccess.Render("✓ Session gemt: " + msg.s.Title))
			return m, tea.Quit
		}

	case msgSessionList:
		if msg.err != nil {
			m.appendSystem(styleError.Render("Fejl: " + msg.err.Error()))
		} else {
			m.sessions = msg.sessions
			m.appendSystem(session.RenderList(msg.sessions))
		}

	case msgWikiResult:
		if msg.err != nil {
			m.appendSystem(styleError.Render("Wiki-fejl: " + msg.err.Error()))
		} else {
			m.messages = append(m.messages, provider.Message{Role: "assistant", Content: msg.context})
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()
			if len(msg.pages) > 0 {
				refs := make([]string, len(msg.pages))
				for i, p := range msg.pages {
					refs[i] = p.Path
				}
				m.appendSystem(styleSystem.Render("Kilder: " + strings.Join(refs, ", ")))
			}
		}

	case msgWikiSaved:
		if msg.err != nil {
			m.appendSystem(styleError.Render("Gem fejlede: " + msg.err.Error()))
		} else {
			m.appendSystem(styleSuccess.Render("✓ Gemt i wiki: " + msg.path))
			m.pendingWikiSave = ""
		}

	case msgWorktreeRemoved:
		var content string
		if msg.err != nil {
			content = styleError.Render("Fejl: " + msg.err.Error())
		} else {
			content = styleSuccess.Render("✓ Worktree fjernet: " + msg.name)
		}
		m.appendSystem(content)

	case msgToolOutput:
		m.toolOutput = string(msg)
		m.toolPanel.SetContent(m.toolOutput)

	}

	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	cmds = append(cmds, inputCmd)

	var convCmd tea.Cmd
	m.conversation, convCmd = m.conversation.Update(msg)
	cmds = append(cmds, convCmd)

	if m.toolOutput != "" {
		var toolCmd tea.Cmd
		m.toolPanel, toolCmd = m.toolPanel.Update(msg)
		cmds = append(cmds, toolCmd)
	}

	return m, tea.Batch(cmds...)
}
