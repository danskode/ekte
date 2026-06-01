package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/danskode/ekte/internal/agent"
	"github.com/danskode/ekte/internal/provider"
)

const (
	defaultMaxTokens = 200000
	warnThreshold    = 0.75
	critThreshold    = 0.90
	toolPanelWidth   = 40
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

	bannerContent    string
	forrestenPending bool
	userName         string
	agentName        string
	mdRenderer       *glamour.TermRenderer

	suggestions   []string
	suggestionIdx int

	streaming bool
	streamBuf string
	streamCh  <-chan agent.Event

	tokenCount int
	maxTokens  int
	spinner    spinner.Model
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

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorAccent)

	return Model{
		agent:         a,
		input:         ta,
		spinner:       sp,
		historyIdx:    -1,
		suggestionIdx: -1,
		maxTokens:     defaultMaxTokens,
	}
}

func (m *Model) SetMaxTokens(n int) {
	if n > 0 {
		m.maxTokens = n
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

func newMdRenderer(width int) *glamour.TermRenderer {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	return r
}

func (m *Model) SetNames(userName, agentName string) {
	m.userName = userName
	m.agentName = agentName
}

func (m Model) msgHeader(rendered string, w int) string {
	labelW := lipgloss.Width(rendered)
	ruleW := w - labelW - 1
	if ruleW < 0 {
		ruleW = 0
	}
	rule := lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", ruleW))
	return rendered + " " + rule
}

func (m *Model) ShowBanner() {
	letterRows := [][]string{
		{"          ", "   █████  ", "  ████████", "  ██      ", "   ██████ "}, // e
		{"██        ", "██    ██  ", "███████   ", "██    ██  ", "██      ██"}, // k
		{"    ██    ", "████████  ", "    ██    ", "    ██    ", "    ██████"}, // t
		{"          ", "   █████  ", "  ████████", "  ██      ", "   ██████ "}, // e
	}
	// Ingen separator mellem k og t — tværstregen på t peger mod k
	separators := []string{" ", "", " "}
	colors := []lipgloss.Color{
		lipgloss.Color("219"),
		lipgloss.Color("213"),
		lipgloss.Color("177"),
		lipgloss.Color("135"),
	}

	var sb strings.Builder
	for row := 0; row < 5; row++ {
		for i, letter := range letterRows {
			style := lipgloss.NewStyle().Foreground(colors[i]).Bold(true)
			sb.WriteString(style.Render(letter[row]))
			if i < len(separators) {
				sb.WriteString(separators[i])
			}
		}
		sb.WriteString("\n")
	}
	subtitleStyle := lipgloss.NewStyle().Foreground(colorSubtle)
	subtitle := "et agent harness baseret på AIDD"
	logoWidth := 42
	padding := (logoWidth - lipgloss.Width(subtitle)) / 2
	if padding > 0 {
		sb.WriteString(strings.Repeat(" ", padding))
	}
	sb.WriteString(subtitleStyle.Render(subtitle))

	m.bannerContent = sb.String()
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
	w := m.conversation.Width
	if w <= 0 {
		w = 80
	}
	var sb strings.Builder
	if m.bannerContent != "" {
		sb.WriteString(m.bannerContent + "\n\n")
	}
	renderMd := func(s string) string {
		if m.mdRenderer == nil {
			return wordWrap(s, w)
		}
		out, err := m.mdRenderer.Render(s)
		if err != nil || strings.TrimSpace(out) == "" {
			return wordWrap(s, w)
		}
		return strings.TrimRight(out, "\n")
	}

	userName := m.userName
	if userName == "" {
		userName = "Dig"
	}
	agentName := m.agentName
	if agentName == "" {
		agentName = "Ekte"
	}

	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			header := m.msgHeader(styleUser.Render("👤 "+userName), w)
			body := styleUserBody.Render(wordWrap(msg.Content, w-2))
			sb.WriteString(header + "\n" + body + "\n\n")
		case "assistant":
			header := m.msgHeader(styleAssistantLabel.Render("🤖 "+agentName), w)
			sb.WriteString(header + "\n" + renderMd(msg.Content) + "\n\n")
		case "system":
			sb.WriteString(styleSystem.Render("◦ "+wordWrap(msg.Content, w)) + "\n\n")
		case "forresten":
			header := m.msgHeader(styleForrestenLabel.Render("💬 forresten"), w)
			sb.WriteString(header + "\n" + renderMd(msg.Content) + "\n\n")
		}
	}
	if m.forrestenPending {
		sb.WriteString(styleForrestenLabel.Render("💬 forresten") + " " +
			lipgloss.NewStyle().Foreground(colorBorder).Render("────") + "  " +
			styleSystem.Render("venter...") + "\n\n")
	}
	if m.streaming && m.streamBuf != "" {
		header := m.msgHeader(styleAssistantLabel.Render("🤖 "+agentName), w)
		body := styleAssistantBody.Render(wordWrap(m.streamBuf, w)) + "▌"
		sb.WriteString(header + "\n" + body + "\n\n")
	}
	return sb.String()
}

// wordWrap bryder lange linjer ved whitespace uden at ødelægge eksisterende linjeskift.
func wordWrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out strings.Builder
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		if len(line) <= width {
			out.WriteString(line)
			continue
		}
		col := 0
		for j, word := range strings.Fields(line) {
			wl := len(word)
			if j == 0 {
				out.WriteString(word)
				col = wl
			} else if col+1+wl <= width {
				out.WriteByte(' ')
				out.WriteString(word)
				col += 1 + wl
			} else {
				out.WriteByte('\n')
				out.WriteString(word)
				col = wl
			}
		}
	}
	return out.String()
}

func (m Model) contextStyle() lipgloss.Style {
	ratio := float64(m.tokenCount) / float64(m.maxTokens)
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
	ctx := fmt.Sprintf("kontekst: %d/%d", m.tokenCount, m.maxTokens)
	ctxStyled := m.contextStyle().Render(ctx)

	skillIndicator := ""
	if m.agent != nil && m.agent.ActiveSkill() != nil {
		skillIndicator = "  " + styleSlashCmd.Render("skill:"+m.agent.ActiveSkill().Name)
	}

	var right string
	if m.streaming {
		right = styleStatusBar.Render(m.spinner.View() + " arbejder...")
	} else {
		right = styleStatusBar.Render(styleSystem.Render("/hjælp"))
	}

	left := styleStatusBar.Render(ctxStyled + skillIndicator)
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
		hint := styleSystem.Render("PgUp/PgDn: scroll · Esc: luk")
		toolView = styleBorder.Width(toolPanelWidth - 2).Height(panelH - 2).
			BorderTopForeground(colorSubtle).
			Render(m.toolPanel.View() + "\n\n" + hint)
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

	parts := []string{
		panels,
		styleActiveBorder.Width(m.width - 2).Render(m.input.View()),
	}
	if sugg := m.renderSuggestions(); sugg != "" {
		parts = append(parts, sugg)
	}
	parts = append(parts, m.statusBar())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) updateSuggestions() {
	val := m.input.Value()
	if !strings.HasPrefix(val, "/") || strings.ContainsRune(val, ' ') {
		m.suggestions = nil
		m.suggestionIdx = -1
		return
	}
	query := strings.ToLower(val)
	var matches []string
	if m.agent != nil {
		for _, cmd := range m.agent.Commands() {
			if strings.HasPrefix(strings.ToLower(cmd), query) && cmd != val {
				matches = append(matches, cmd)
			}
		}
	}
	m.suggestions = matches
	if m.suggestionIdx >= len(matches) {
		m.suggestionIdx = -1
	}
}

func (m Model) renderSuggestions() string {
	if len(m.suggestions) == 0 {
		return ""
	}
	dimStyle := lipgloss.NewStyle().Foreground(colorSubtle)
	activeStyle := lipgloss.NewStyle().Foreground(colorAccent)
	hintStyle := lipgloss.NewStyle().Foreground(colorBorder)

	var sb strings.Builder
	sb.WriteString("  ")
	for i, s := range m.suggestions {
		if i == m.suggestionIdx {
			sb.WriteString(activeStyle.Render(s))
		} else {
			sb.WriteString(dimStyle.Render(s))
		}
		if i < len(m.suggestions)-1 {
			sb.WriteString("  ")
		}
	}
	sb.WriteString("  " + hintStyle.Render("tab · ↑↓"))
	return sb.String()
}

func (m *Model) appendSystem(content string) {
	m.messages = append(m.messages, provider.Message{Role: "system", Content: content})
	m.conversation.SetContent(m.conversationContent())
	m.conversation.GotoBottom()
}
