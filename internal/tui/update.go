package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/danskode/ekte/internal/agent"
	"github.com/danskode/ekte/internal/provider"
)

type msgAgentEvents struct {
	events []agent.Event
	err    error
}

func processCmd(a *agent.Agent, input string) tea.Cmd {
	return func() tea.Msg {
		events := a.Process(nil, input)
		return msgAgentEvents{events: events}
	}
}

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
		if msg.String() == "shift+enter" {
			m.input.InsertString("\n")
			return m, nil
		}
		if msg.String() == "alt+O" {
			m.pendingShiftEnter = true
			return m, nil
		}
		if m.pendingShiftEnter {
			m.pendingShiftEnter = false
			if len(msg.Runes) == 1 && msg.Runes[0] == 'M' {
				m.input.InsertString("\n")
				return m, nil
			}
			m.input.InsertString("O")
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyCtrlJ:
			m.input.InsertString("\n")
			return m, nil

		case tea.KeyUp:
			if m.input.Focused() && m.input.Line() == 0 {
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
			if len(m.history) == 0 || m.history[len(m.history)-1] != val {
				m.history = append(m.history, val)
			}
			if val != "/clear" {
				m.syncFromAgent()
				m.messages = append(m.messages, provider.Message{Role: "user", Content: val})
				m.conversation.SetContent(m.conversationContent())
				m.conversation.GotoBottom()
			}
			return m, processCmd(m.agent, val)
		}

	case msgAgentEvents:
		if msg.err != nil {
			m.appendSystem(styleError.Render(msg.err.Error()))
			break
		}
		for _, ev := range msg.events {
			switch ev.Type {
			case agent.EventAssistant:
				m.messages = append(m.messages, provider.Message{Role: "assistant", Content: ev.Content})
				m.conversation.SetContent(m.conversationContent())
				m.conversation.GotoBottom()
			case agent.EventSystem:
				if ev.Content == "" {
					m.messages = nil
					m.conversation.SetContent("")
				} else {
					m.appendSystem(ev.Content)
				}
			case agent.EventError:
				m.appendSystem(styleError.Render(ev.Content))
			case agent.EventTokenCount:
				m.tokenCount = ev.Tokens
			case agent.EventToolOutput:
				m.toolOutput = ev.Content
				m.toolPanel.SetContent(ev.Content)
			case agent.EventQuit:
				return m, tea.Quit
			}
		}
		m.syncFromAgent()
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

// syncFromAgent synkroniserer token-count og aktiv skill fra agent til TUI.
func (m *Model) syncFromAgent() {
	if m.agent == nil {
		return
	}
	m.tokenCount = m.agent.TokenCount()
}
