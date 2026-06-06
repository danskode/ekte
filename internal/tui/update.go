package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/danskode/ekte/internal/agent"
	"github.com/danskode/ekte/internal/provider"
)

// msgStreamStarted returneres når kanalen er klar — TUI begynder at læse fra den.
type msgStreamStarted struct {
	ch     <-chan agent.Event
	cancel context.CancelFunc
}

// msgStreamEnd markerer at kanalen er lukket og streaming er færdig.
type msgStreamEnd struct{}

// msgMdReady sendes når glamour-rendereren er klar i baggrunden.
type msgMdReady struct{ r *glamour.TermRenderer }

func forrestenCmd(a *agent.Agent, input string) tea.Cmd {
	return func() tea.Msg {
		ch := a.ProcessStream(context.Background(), input)
		for ev := range ch {
			if ev.Type == agent.EventForresten || ev.Type == agent.EventError {
				return ev
			}
		}
		return agent.Event{Type: agent.EventForresten, Content: ""}
	}
}

func initMdCmd(width int) tea.Cmd {
	return func() tea.Msg {
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return msgMdReady{}
		}
		return msgMdReady{r: r}
	}
}

func startStreamCmd(a *agent.Agent, input string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch := a.ProcessStream(ctx, input)
		return msgStreamStarted{ch: ch, cancel: cancel}
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
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()
			return m, initMdCmd(msg.Width - 6)
		} else {
			m.conversation.Width = msg.Width - 4
			m.conversation.Height = msg.Height - 10
			m.input.SetWidth(msg.Width - 4)
			return m, initMdCmd(msg.Width - 6)
		}

	case msgMdReady:
		m.mdRenderer = msg.r
		m.conversation.SetContent(m.conversationContent())

	case tea.KeyMsg:
		// Bekræftelsestilstand: kun j/y bekræfter, n/esc afviser, alt andet ignoreres
		if m.pendingConfirm {
			switch strings.ToLower(msg.String()) {
			case "j", "y":
				confirmCh := m.confirmCh
				m.pendingConfirm = false
				m.confirmCh = nil
				m.confirmDesc = ""
				confirmCh <- true
				m.conversation.SetContent(m.conversationContent())
				m.conversation.GotoBottom()
				return m, nil
			case "n", "esc", "ctrl+c":
				confirmCh := m.confirmCh
				m.pendingConfirm = false
				m.confirmCh = nil
				m.confirmDesc = ""
				confirmCh <- false
				m.conversation.SetContent(m.conversationContent())
				m.conversation.GotoBottom()
				return m, nil
			default:
				// Scroll-taster og andre taster passerer igennem til viewport
			}
		}
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
			if m.streaming {
				// Afbryd i stedet for at afslutte
				if m.cancelStream != nil {
					m.cancelStream()
					m.cancelStream = nil
				}
				if m.pendingConfirm && m.confirmCh != nil {
					m.confirmCh <- false
				}
				m.streaming = false
				m.streamBuf = ""
				m.streamCh = nil
				m.pendingConfirm = false
				m.confirmCh = nil
				m.confirmDesc = ""
				m.appendSystem(styleError.Render("Afbrudt."))
				return m, nil
			}
			return m, tea.Quit

		case tea.KeyPgUp:
			if m.toolOutput != "" {
				m.toolPanel.HalfViewUp()
			} else {
				m.conversation.HalfViewUp()
			}
			return m, nil

		case tea.KeyPgDown:
			if m.toolOutput != "" {
				m.toolPanel.HalfViewDown()
			} else {
				m.conversation.HalfViewDown()
			}
			return m, nil

		case tea.KeyCtrlJ:
			m.input.InsertString("\n")
			return m, nil

		case tea.KeyTab:
			if len(m.suggestions) > 0 {
				m.suggestionIdx = (m.suggestionIdx + 1) % len(m.suggestions)
				m.input.SetValue(m.suggestions[m.suggestionIdx])
				m.input.CursorEnd()
				return m, nil
			}

		case tea.KeyEsc:
			if len(m.suggestions) > 0 {
				m.suggestions = nil
				m.suggestionIdx = -1
				return m, nil
			}
			if m.toolOutput != "" {
				m.toolOutput = ""
				return m, nil
			}

		case tea.KeyUp:
			if len(m.suggestions) > 0 {
				if m.suggestionIdx <= 0 {
					m.suggestionIdx = len(m.suggestions) - 1
				} else {
					m.suggestionIdx--
				}
				return m, nil
			}
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
			if len(m.suggestions) > 0 {
				m.suggestionIdx = (m.suggestionIdx + 1) % len(m.suggestions)
				return m, nil
			}
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
			if len(m.suggestions) > 0 && m.suggestionIdx >= 0 {
				m.input.SetValue(m.suggestions[m.suggestionIdx] + " ")
				m.input.CursorEnd()
				m.suggestions = nil
				m.suggestionIdx = -1
				return m, nil
			}
			val := strings.TrimSpace(m.input.Value())
			if val == "" {
				return m, nil
			}
			if strings.HasPrefix(val, "/forresten") {
				m.input.Reset()
				m.historyIdx = -1
				m.savedDraft = ""
				if len(m.history) == 0 || m.history[len(m.history)-1] != val {
					m.history = append(m.history, val)
				}
				m.messages = append(m.messages, provider.Message{Role: "user", Content: val})
				m.forrestenPending = true
				m.conversation.SetContent(m.conversationContent())
				m.conversation.GotoBottom()
				return m, forrestenCmd(m.agent, val)
			}
			if m.streaming {
				return m, nil // ignorer input under streaming
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
		m.cancelStream = msg.cancel
		m.streamStart = time.Now()
		return m, tea.Batch(readStreamCmd(msg.ch), m.spinner.Tick)

	case spinner.TickMsg:
		if m.streaming {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		if m.streaming && m.streamBuf == "" {
			m.thinkPos++
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()
		}

	case msgStreamEnd:
		m.streaming = false
		m.streamCh = nil
		m.conversation.SetContent(m.conversationContent())
		m.conversation.GotoBottom()

	case agent.Event:
		switch msg.Type {

		case agent.EventToolConfirm:
			m.pendingConfirm = true
			m.confirmCh = msg.ConfirmCh
			m.confirmDesc = msg.Content
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()

		case agent.EventThinking:
			m.thinking = true
			m.thinkPos = 0
			m.streamBuf = "" // ryd buffer → hjerneanimation vises igen
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()

		case agent.EventStreamToken:
			m.thinking = false
			m.streamBuf += msg.Content
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()
			return m, readStreamCmd(m.streamCh)

		case agent.EventStreamDone:
			m.streaming = false
			m.thinking = false
			m.streamBuf = ""
			m.streamCh = nil
			if msg.Content != "" {
				m.messages = append(m.messages, provider.Message{Role: "assistant", Content: msg.Content})
			}
			if msg.Source != "" {
				m.messages = append(m.messages, provider.Message{
					Role:    "system",
					Content: "Information fra 📚 wiki · " + msg.Source,
				})
			}
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()

		case agent.EventAssistant:
			m.messages = append(m.messages, provider.Message{Role: "assistant", Content: msg.Content})
			if msg.Source != "" {
				m.messages = append(m.messages, provider.Message{
					Role:    "system",
					Content: "Information fra 📚 wiki · " + msg.Source,
				})
			}
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()

		case agent.EventForresten:
			m.forrestenPending = false
			if msg.Content != "" {
				m.messages = append(m.messages, provider.Message{Role: "forresten", Content: msg.Content})
			}
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()

		case agent.EventSystem:
			if msg.Prefill != "" {
				m.input.SetValue(msg.Prefill)
				m.input.CursorEnd()
			}
			if msg.Content == "" {
				m.streaming = false
				m.streamBuf = ""
				m.streamCh = nil
				m.messages = nil
				m.conversation.SetContent(m.conversationContent())
			} else {
				m.appendSystem(msg.Content)
			}

		case agent.EventError:
			m.streaming = false
			m.thinking = false
			m.streamBuf = ""
			m.streamCh = nil
			m.appendSystem(styleError.Render(msg.Content))

		case agent.EventTokenCount:
			m.tokenCount = msg.Tokens

		case agent.EventToolOutput:
			m.streamBuf = ""
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
	m.updateSuggestions()

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
