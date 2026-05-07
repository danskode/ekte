package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/danskode/ekte/internal/agent"
	"github.com/danskode/ekte/internal/provider"
)

// msgStreamStarted returneres når kanalen er klar — TUI begynder at læse fra den.
type msgStreamStarted struct{ ch <-chan agent.Event }

// msgStreamEnd markerer at kanalen er lukket og streaming er færdig.
type msgStreamEnd struct{}

func startStreamCmd(a *agent.Agent, input string) tea.Cmd {
	return func() tea.Msg {
		ch := a.ProcessStream(context.Background(), input)
		return msgStreamStarted{ch: ch}
	}
}

// readStreamCmd venter på næste event fra kanalen og returnerer det som tea.Msg.
// agent.Event implementerer tea.Msg da alle typer gør det.
func readStreamCmd(ch <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return msgStreamEnd{}
		}
		return ev
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
			if m.streaming {
				return m, nil // ignorer input under streaming
			}
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
			return m, startStreamCmd(m.agent, val)
		}

	case msgStreamStarted:
		m.streaming = true
		m.streamBuf = ""
		m.streamCh = msg.ch
		return m, readStreamCmd(msg.ch)

	case msgStreamEnd:
		m.streaming = false
		m.streamCh = nil

	case agent.Event:
		switch msg.Type {

		case agent.EventStreamToken:
			m.streamBuf += msg.Content
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()
			return m, readStreamCmd(m.streamCh)

		case agent.EventStreamDone:
			m.streaming = false
			m.streamBuf = ""
			m.streamCh = nil
			m.messages = append(m.messages, provider.Message{Role: "assistant", Content: msg.Content})
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()

		case agent.EventAssistant:
			// Bruges af /forresten og /wiki — ikke streaming
			m.messages = append(m.messages, provider.Message{Role: "assistant", Content: msg.Content})
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()

		case agent.EventSystem:
			if msg.Content == "" {
				m.messages = nil
				m.conversation.SetContent("")
			} else {
				m.appendSystem(msg.Content)
			}

		case agent.EventError:
			m.streaming = false
			m.streamBuf = ""
			m.streamCh = nil
			m.appendSystem(styleError.Render(msg.Content))

		case agent.EventTokenCount:
			m.tokenCount = msg.Tokens

		case agent.EventToolOutput:
			m.toolOutput = msg.Content
			m.toolPanel.SetContent(msg.Content)

		case agent.EventQuit:
			return m, tea.Quit
		}

		// Hvis vi stadig streamer og næste event ikke er token (fx EventSystem fra slash),
		// fortsæt med at læse kanalen
		if m.streaming && m.streamCh != nil &&
			msg.Type != agent.EventStreamToken {
			return m, readStreamCmd(m.streamCh)
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

func (m *Model) syncFromAgent() {
	if m.agent == nil {
		return
	}
	m.tokenCount = m.agent.TokenCount()
}
