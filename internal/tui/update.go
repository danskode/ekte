package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/atotto/clipboard"
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

// submitPrompt sender en prompt til agenten og opdaterer samtalevisningen — fælles
// for både direkte input og afvikling fra prompt-køen.
func (m *Model) submitPrompt(val string) tea.Cmd {
	// Tom prompt (wizard-Enter) vises ikke som user-besked i samtalen.
	if val != "/clear" && val != "" {
		m.syncFromAgent()
		m.messages = append(m.messages, provider.Message{Role: "user", Content: val})
		m.conversation.SetContent(m.conversationContent())
		m.conversation.GotoBottom()
	}
	return startStreamCmd(m.agent, val)
}

// dequeueNext starter den næste kø-prompt, hvis der er en — kaldes når streaming stopper.
func (m *Model) dequeueNext() tea.Cmd {
	if len(m.promptQueue) == 0 {
		return nil
	}
	val := m.promptQueue[0]
	m.promptQueue = m.promptQueue[1:]
	return m.submitPrompt(val)
}

func startStreamCmd(a *agent.Agent, input string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch := a.ProcessStream(ctx, input)
		return msgStreamStarted{ch: ch, cancel: cancel}
	}
}

// beepCmd afspiller en notifikationslyd. Prøver paplay/aplay med freedesktop-lyde
// og falder tilbage til terminal-bell hvis ingen lydafspiller er tilgængelig.
func beepCmd() tea.Msg {
	sounds := []string{
		"/usr/share/sounds/freedesktop/stereo/message.oga",
		"/usr/share/sounds/freedesktop/stereo/bell.oga",
		"/usr/share/sounds/freedesktop/stereo/complete.oga",
	}
	for _, s := range sounds {
		if _, err := os.Stat(s); err != nil {
			continue
		}
		if exec.Command("paplay", s).Run() == nil {
			return nil
		}
		if exec.Command("aplay", s).Run() == nil {
			return nil
		}
		break
	}
	fmt.Print("\a")
	return nil
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
	wasStreaming := m.streaming

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			m.conversation = viewport.New(msg.Width-4, msg.Height-10)
			m.toolPanel = viewport.New(m.toolPanelWidth()-4, msg.Height-10)
			m.input.SetWidth(msg.Width - 4)
			m.ready = true
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()
			m.mdWidth = m.conversationWidth() - 2
			return m, initMdCmd(m.mdWidth)
		} else {
			m.conversation.Width = msg.Width - 4
			m.conversation.Height = msg.Height - 10
			m.toolPanel.Width = m.toolPanelWidth() - 2
			m.toolPanel.Height = msg.Height - 10
			if m.toolOutput != "" {
				m.toolPanel.SetContent(wordWrap(m.toolOutput, m.toolPanelWidth()-4))
			}
			m.input.SetWidth(msg.Width - 4)
			m.mdWidth = m.conversationWidth() - 2
			return m, initMdCmd(m.mdWidth)
		}

	case msgMdReady:
		m.mdRenderer = msg.r
		m.conversation.SetContent(m.conversationContent())

	case tea.KeyMsg:
		// Bekræftelsestilstand: j/y bekræfter, n/esc afviser, Tab afviser + lader
		// brugeren skrive hvad agenten skal gøre i stedet for blot at vente på et nyt svar
		if m.pendingConfirm {
			if m.confirmRedirecting {
				switch msg.Type {
				case tea.KeyEnter:
					val := strings.TrimSpace(m.input.Value())
					if val == "" {
						return m, nil
					}
					confirmCh := m.confirmCh
					m.pendingConfirm = false
					m.confirmRedirecting = false
					m.confirmCh = nil
					m.confirmDesc = ""
					m.input.Reset()
					confirmCh <- agent.ConfirmResponse{Approved: false, Redirect: val}
					m.conversation.SetContent(m.conversationContent())
					m.conversation.GotoBottom()
					return m, nil
				case tea.KeyEsc:
					m.confirmRedirecting = false
					m.input.Reset()
					return m, nil
				}
				var inputCmd tea.Cmd
				m.input, inputCmd = m.input.Update(msg)
				return m, inputCmd
			}
			switch strings.ToLower(msg.String()) {
			case "j", "y":
				confirmCh := m.confirmCh
				m.pendingConfirm = false
				m.confirmCh = nil
				m.confirmDesc = ""
				confirmCh <- agent.ConfirmResponse{Approved: true}
				m.conversation.SetContent(m.conversationContent())
				m.conversation.GotoBottom()
				return m, nil
			case "n", "ctrl+c":
				confirmCh := m.confirmCh
				m.pendingConfirm = false
				m.confirmCh = nil
				m.confirmDesc = ""
				confirmCh <- agent.ConfirmResponse{Approved: false}
				m.conversation.SetContent(m.conversationContent())
				m.conversation.GotoBottom()
				return m, nil
			case "tab":
				m.confirmRedirecting = true
				m.input.Reset()
				m.input.Focus()
				return m, nil
			case "esc":
				confirmCh := m.confirmCh
				m.pendingConfirm = false
				m.confirmCh = nil
				m.confirmDesc = ""
				confirmCh <- agent.ConfirmResponse{Approved: false}
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
		// Shift+Tab skifter arbejdsmode (plan ↔ develop) — som i Claude Code.
		// Kaldes direkte på agenten (ikke som slash-kommando): /mode er
		// reserveret til verbositet (beginner/expert), en uafhængig akse.
		if msg.String() == "shift+tab" && !m.pendingConfirm && m.agent != nil {
			if m.streaming {
				// Agentens beskedliste muteres af stream-goroutinen lige nu —
				// skiftet anvendes derfor først når svaret er færdigt (data race
				// ellers). Tryk igen for at fortryde.
				m.pendingModeToggle = !m.pendingModeToggle
				if m.pendingModeToggle {
					m.appendSystem("⏳ Mode skiftes når svaret er færdigt — Shift+Tab igen fortryder.")
				} else {
					m.appendSystem("Mode-skift fortrudt.")
				}
				return m, nil
			}
			for _, ev := range m.agent.ToggleWorkMode() {
				if ev.Content != "" {
					m.appendSystem(ev.Content)
				}
			}
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
					m.confirmCh <- agent.ConfirmResponse{Approved: false}
				}
				m.streaming = false
				m.streamBuf = ""
				m.streamCh = nil
				m.pendingConfirm = false
				m.confirmRedirecting = false
				m.confirmCh = nil
				m.confirmDesc = ""
				m.appendSystem(styleError.Render("Afbrudt."))
				return m, nil
			}
			return m, startStreamCmd(m.agent, "/exit")

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

		case tea.KeyCtrlY:
			if text := m.lastAssistantText(); text != "" {
				if err := clipboard.WriteAll(text); err == nil {
					m.appendSystem("✓ Seneste svar kopieret til udklipsholder.")
				}
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
				m.toolLog = ""
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
				// I model-wizarden betyder tom Enter "behold nuværende værdi"
				// — send den videre i stedet for at sluge den.
				if m.agent != nil && m.agent.InWizard() && !m.streaming {
					m.input.Reset()
					return m, m.submitPrompt("")
				}
				return m, nil
			}
			// /kø-kommandoer håndteres lokalt i TUI — køen lever i model, ikke agenten
			if val == "/kø" || strings.HasPrefix(val, "/kø ") {
				m.input.Reset()
				m.historyIdx = -1
				m.savedDraft = ""
				arg := strings.TrimSpace(strings.TrimPrefix(val, "/kø"))
				m.conversation.SetContent(m.conversationContent())
				m.conversation.GotoBottom()
				return m, m.handleQueueCmd(arg)
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
			m.input.Reset()
			m.historyIdx = -1
			m.savedDraft = ""
			if len(m.history) == 0 || m.history[len(m.history)-1] != val {
				m.history = append(m.history, val)
			}
			if m.streaming {
				// Læg i kø i stedet for at blokere — afvikles automatisk når den aktuelle prompt er færdig
				m.promptQueue = append(m.promptQueue, val)
				return m, nil
			}
			return m, m.submitPrompt(val)
		}

	case msgStreamStarted:
		m.streaming = true
		m.streamBuf = ""
		m.reasoningBuf = ""
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
			if m.agent != nil && m.agent.SoundEnabled() {
				cmds = append(cmds, beepCmd)
			}

		case agent.EventThinking:
			m.thinking = true
			m.thinkPos = 0
			m.streamBuf = "" // ryd buffer → hjerneanimation vises igen
			m.reasoningBuf = ""
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()

		case agent.EventReasoningToken:
			// Stream modellens "tanker" live ind i sidepanelet — panelet gives
			// tilbage til tool-loggen (restoreToolLog) så snart svaret begynder.
			m.reasoningBuf += msg.Content
			m.toolOutput = "🧠 " + m.reasoningBuf
			m.toolPanel.SetContent(wordWrap(m.toolOutput, m.toolPanelWidth()-4))
			m.toolPanel.GotoBottom()
			return m, readStreamCmd(m.streamCh)

		case agent.EventStreamToken:
			m.thinking = false
			m.restoreToolLog() // tankerne er slut — giv panelet tilbage til tool-loggen
			m.streamBuf += msg.Content
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()
			return m, readStreamCmd(m.streamCh)

		case agent.EventStreamDone:
			m.streaming = false
			m.thinking = false
			m.restoreToolLog() // svar uden tokens (fx rene tool-runder) skal også rydde tankerne
			m.streamBuf = ""
			m.streamCh = nil
			if msg.Content != "" {
				m.messages = append(m.messages, provider.Message{Role: "assistant", Content: msg.Content, Source: msg.Source})
			}
			if msg.Stats != "" {
				m.messages = append(m.messages, provider.Message{
					Role:    "system",
					Content: msg.Stats,
				})
			}
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()
			if m.agent != nil && m.agent.SoundEnabled() {
				cmds = append(cmds, beepCmd)
			}

		case agent.EventAssistant:
			// Nulstil streaming-bufferen FØR vi tilføjer den færdige besked til m.messages —
			// ellers vises samme tekst dobbelt et øjeblik (én gang som fastlåst besked, én
			// gang som det "levende" streamBuf), indtil næste EventThinking rydder den.
			m.streamBuf = ""
			if msg.Content != "" {
				m.messages = append(m.messages, provider.Message{Role: "assistant", Content: msg.Content, Source: msg.Source})
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
				m.exitNote = msg.Content
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

		case agent.EventModelInfo:
			if msg.Tokens > 0 {
				m.maxTokens = msg.Tokens
			}
			if msg.Content != "" {
				m.modelName = msg.Content
			}

		case agent.EventToolOutput:
			m.streamBuf = ""
			m.toolOutput = msg.Content
			m.toolLog = msg.Content
			m.toolPanel.SetContent(wordWrap(msg.Content, m.toolPanelWidth()-4))

		case agent.EventQuit:
			return m, tea.Quit
		}

		// Hvis vi stadig streamer og næste event ikke er token (fx EventSystem fra slash),
		// fortsæt med at læse kanalen — men bevar evt. opsamlede cmds (fx beepCmd),
		// ellers går de tabt når vi returnerer tidligt her.
		if m.streaming && m.streamCh != nil &&
			msg.Type != agent.EventStreamToken {
			cmds = append(cmds, readStreamCmd(m.streamCh))
			return m, tea.Batch(cmds...)
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

	// Sidepanelet kan have åbnet/lukket sig siden sidste render — følg samtalens
	// aktuelle bredde, ellers wrapper glamour til den gamle og output klippes.
	if m.ready {
		if w := m.conversationWidth() - 2; w > 0 && w != m.mdWidth {
			m.mdWidth = w
			cmds = append(cmds, initMdCmd(w))
		}
	}

	if wasStreaming && !m.streaming {
		// Anvend et mode-skift bestilt under streaming — FØR næste kø-prompt,
		// så den afvikles i den mode brugeren bad om.
		if m.pendingModeToggle && m.agent != nil {
			m.pendingModeToggle = false
			for _, ev := range m.agent.ToggleWorkMode() {
				if ev.Content != "" {
					m.appendSystem(ev.Content)
				}
			}
		}
		if cmd := m.dequeueNext(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) syncFromAgent() {
	if m.agent == nil {
		return
	}
	m.tokenCount = m.agent.TokenCount()
	// Ved resume: kopiér kun user/assistant-beskeder til TUI.
	// System-beskeder (prompt, hukommelse, profil) hører ikke til i chat-visningen.
	if msgs := m.agent.Messages(); len(m.messages) == 0 {
		for _, msg := range msgs {
			if msg.Role == "user" || msg.Role == "assistant" {
				m.messages = append(m.messages, msg)
			}
		}
	}
}
