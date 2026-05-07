package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/danskode/ekte/internal/agent"
	"github.com/danskode/ekte/internal/provider"
)

const (
	maxTokens      = 200000
	warnThreshold  = 0.75
	critThreshold  = 0.90
	toolPanelWidth = 40
)

type Model struct {
	width, height int

	conversation viewport.Model
	toolPanel    viewport.Model
	input        textarea.Model

	messages   []provider.Message
	toolOutput string

	history           []string
	historyIdx        int
	savedDraft        string
	pendingShiftEnter bool

	streaming bool
	streamBuf string
	streamCh  <-chan agent.Event

	tokenCount int
	agent      *agent.Agent
	ready      bool
}

func New(a *agent.Agent) Model {
	ta := textarea.New()
	ta.Placeholder = "Skriv her... (Enter sender, Shift+Enter / Ctrl+J = ny linje, /hjælp)"
	ta.Focus()
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(false)
	ta.CharLimit = 0

	return Model{
		agent:      a,
		input:      ta,
		historyIdx: -1,
	}
}

func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

func (m *Model) SetProjectContext(context string) {
	m.agent.AddContext("system", "Projektkontekst (ekte.md):\n\n"+context)
}

func (m *Model) AddWarning(msg string) {
	m.messages = append(m.messages, provider.Message{
		Role:    "system",
		Content: styleError.Render(msg),
	})
}

func (m *Model) SetWelcome(projectName string) {
	name := projectName
	if name == "" {
		name = "dit projekt"
	}
	welcome := fmt.Sprintf(
		"Hej! Du er nu klar til at spec'e %s.\n\n"+
			"Vil du spec'e din første funktion, eller vil du først tilføje noget viden "+
			"til din wiki, som vi kan bruge til at bygge efter?\n\n"+
			"Du kan også se dine muligheder med /hjælp",
		name,
	)
	m.messages = append(m.messages, provider.Message{Role: "assistant", Content: welcome})
}

func (m Model) conversationContent() string {
	var sb strings.Builder
	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			sb.WriteString(styleUser.Render("Du") + "\n" + msg.Content + "\n\n")
		case "assistant":
			sb.WriteString(styleAssistant.Render("ekte") + "\n" + msg.Content + "\n\n")
		case "system":
			sb.WriteString(styleSystem.Render("● "+msg.Content) + "\n\n")
		}
	}
	if m.streaming {
		content := m.streamBuf + "▌"
		sb.WriteString(styleAssistant.Render("ekte") + "\n" + content + "\n\n")
	}
	return sb.String()
}

func (m Model) contextStyle() lipgloss.Style {
	ratio := float64(m.tokenCount) / float64(maxTokens)
	switch {
	case ratio >= critThreshold:
		return styleContextCrit
	case ratio >= warnThreshold:
		return styleContextWarn
	default:
		return styleContextOk
	}
}

func (m Model) statusBar() string {
	ctx := fmt.Sprintf("kontekst: %d/%d", m.tokenCount, maxTokens)
	ctxStyled := m.contextStyle().Render(ctx)

	skillIndicator := ""
	if m.agent != nil && m.agent.ActiveSkill() != nil {
		skillIndicator = "  " + styleSlashCmd.Render("skill:"+m.agent.ActiveSkill().Name)
	}

	hint := styleSystem.Render("/hjælp")
	left := styleStatusBar.Render(ctxStyled + skillIndicator)
	right := styleStatusBar.Render(hint)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	return left + styleStatusBar.Render(strings.Repeat(" ", gap)) + right
}

func (m Model) View() string {
	if !m.ready {
		return "Starter ekte...\n"
	}

	showTool := m.toolOutput != ""
	inputH := m.input.Height() + 2
	panelH := m.height - 1 - inputH - 2

	var convView, toolView string
	if showTool {
		convW := m.width - toolPanelWidth - 1
		m.conversation.Width = convW - 2
		m.conversation.Height = panelH - 2
		convView = styleBorder.Width(convW - 2).Height(panelH - 2).Render(m.conversation.View())
		m.toolPanel.Width = toolPanelWidth - 2
		m.toolPanel.Height = panelH - 2
		toolView = styleBorder.Width(toolPanelWidth - 2).Height(panelH - 2).Render(m.toolPanel.View())
	} else {
		m.conversation.Width = m.width - 2
		m.conversation.Height = panelH - 2
		convView = styleBorder.Width(m.width - 2).Height(panelH - 2).Render(m.conversation.View())
	}

	var panels string
	if showTool {
		panels = lipgloss.JoinHorizontal(lipgloss.Top, convView, toolView)
	} else {
		panels = convView
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		panels,
		styleActiveBorder.Width(m.width-2).Render(m.input.View()),
		m.statusBar(),
	)
}

func (m *Model) appendSystem(content string) {
	m.messages = append(m.messages, provider.Message{Role: "system", Content: content})
	m.conversation.SetContent(m.conversationContent())
	m.conversation.GotoBottom()
}
