package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/danskode/ekte/internal/provider"
	"github.com/danskode/ekte/internal/skill"
	"github.com/danskode/ekte/internal/wiki"
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

	history    []string
	historyIdx int
	savedDraft string

	tokenCount int

	provider      provider.Provider
	forrestenHist []provider.Message

	skills      []skill.Skill
	activeSkill *skill.Skill

	repoRoot string
	wiki     *wiki.Wiki

	pendingWikiSave string // forresten-svar afventer gem-bekræftelse

	ready bool
	err   error
}

func New(p provider.Provider) Model {
	ta := textarea.New()
	ta.Placeholder = "Skriv her... (Enter sender, Shift+Enter = ny linje, /hjælp for kommandoer)"
	ta.Focus()
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetKeys("shift+enter")
	ta.CharLimit = 0

	return Model{
		provider:   p,
		input:      ta,
		historyIdx: -1,
	}
}

func (m *Model) LoadSkills(dir string) []error {
	skills, errs := skill.LoadAll(dir)
	m.skills = skills
	return errs
}

func (m *Model) ActivateSkill(name string) bool {
	for i := range m.skills {
		if m.skills[i].Name == name {
			m.activeSkill = &m.skills[i]
			return true
		}
	}
	return false
}

func (m *Model) SetRepoRoot(root string) {
	m.repoRoot = root
}

func (m *Model) SetWiki(w *wiki.Wiki) {
	m.wiki = w
}

func (m *Model) ClearActiveSkill() {
	m.activeSkill = nil
}

func (m *Model) appendSystem(content string) {
	m.messages = append(m.messages, provider.Message{Role: "system", Content: content})
	m.conversation.SetContent(m.conversationContent())
	m.conversation.GotoBottom()
}

// messagesWithActiveSkill returnerer messages med skill-injection forrest hvis en skill er aktiv.
func (m Model) messagesWithActiveSkill() []provider.Message {
	if m.activeSkill == nil || m.activeSkill.SystemPromptAddition == "" {
		return m.messages
	}
	injection := provider.Message{
		Role:    "system",
		Content: m.activeSkill.SystemPromptAddition,
	}
	out := make([]provider.Message, 0, len(m.messages)+1)
	out = append(out, injection)
	out = append(out, m.messages...)
	return out
}

func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

func (m Model) conversationContent() string {
	var sb strings.Builder
	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			sb.WriteString(styleUser.Render("Du") + "\n")
			sb.WriteString(msg.Content + "\n\n")
		case "assistant":
			sb.WriteString(styleAssistant.Render("ekte") + "\n")
			sb.WriteString(msg.Content + "\n\n")
		case "system":
			sb.WriteString(styleSystem.Render("● "+msg.Content) + "\n\n")
		}
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

	provider := ""
	if m.provider != nil {
		provider = styleSystem.Render(m.provider.Name())
	}

	skillIndicator := ""
	if m.activeSkill != nil {
		skillIndicator = "  " + styleSlashCmd.Render("skill:"+m.activeSkill.Name)
	}

	hint := styleSystem.Render("/hjælp")

	left := styleStatusBar.Render(ctxStyled + "  " + provider + skillIndicator)
	right := styleStatusBar.Render(hint)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	middle := styleStatusBar.Render(strings.Repeat(" ", gap))
	return left + middle + right
}

func (m Model) View() string {
	if !m.ready {
		return "Starter ekte...\n"
	}

	showTool := m.toolOutput != ""
	statusH := 1
	inputH := m.input.Height() + 2
	panelH := m.height - statusH - inputH - 2

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

	inputBox := styleActiveBorder.Width(m.width - 2).Render(m.input.View())
	status := m.statusBar()

	return lipgloss.JoinVertical(lipgloss.Left, panels, inputBox, status)
}
