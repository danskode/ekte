package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/danskode/ekte/internal/agent"
	"github.com/danskode/ekte/internal/provider"
)

const (
	defaultMaxTokens  = 200000
	warnThreshold     = 0.75
	critThreshold     = 0.90
	toolPanelMinWidth = 36
	toolPanelMaxWidth = 72
	toolPanelRatio    = 0.38 // 38% af terminalbredden
)

type Model struct {
	width, height int

	conversation viewport.Model
	toolPanel    viewport.Model
	input        textarea.Model

	messages   []provider.Message
	toolOutput string
	// toolLog er seneste tool-output uafhængigt af hvad panelet viser lige nu —
	// reasoning-streaming låner panelet (EventReasoningToken) og uden denne
	// kopi var tool-loggen tabt for altid når modellens tanker overskrev den.
	toolLog string
	workDir string // vises i statuslinjen så brugeren kan se hvor ekte arbejder

	history           []string
	historyIdx        int
	savedDraft        string
	pendingShiftEnter bool
	promptQueue       []string

	bannerContent    string
	forrestenPending bool
	userName         string
	agentName        string
	mdRenderer       *glamour.TermRenderer
	// mdWidth er den wrap-bredde mdRenderer senest blev bygget med. Samtalens
	// bredde ændrer sig når sidepanelet åbner/lukker — wrapper rendereren til
	// den gamle bredde, klippes linjerne midt i ANSI-koderne og ligner rå tekst.
	mdWidth int

	suggestions   []string
	suggestionIdx int

	streaming    bool
	streamBuf    string
	streamCh     <-chan agent.Event
	streamStart  time.Time
	cancelStream context.CancelFunc
	thinking     bool
	thinkPos     int
	reasoningBuf string // modellens live-streamede "tanker" — vises i sidepanelet, indtil tool-output overtager det

	pendingConfirm     bool
	confirmCh          chan agent.ConfirmResponse
	confirmDesc        string
	confirmRedirecting bool // bruger er i gang med at skrive en omdirigering ved afvisning

	tokenCount int
	maxTokens  int
	modelName  string // vises i statuslinjen; opdateres via EventModelInfo
	// pendingModeToggle: Shift+Tab trykket mens modellen streamede — skiftet
	// anvendes når svaret er færdigt (direkte mutation ville give data race).
	pendingModeToggle bool
	spinner           spinner.Model
	agent             *agent.Agent
	ready             bool

	// exitNote er den seneste system-besked der blev modtaget — ved /exit eller
	// Ctrl+C er det netop afslutnings-resuméet (sessionsnavn, resume-hint, log-sti).
	// Printes til den rigtige terminal efter alt-screen lukker, så brugeren kan se det.
	exitNote string
}

// ExitNote returnerer den seneste system-besked modtaget før programmet afsluttede —
// til brug uden for alt-screen, så afslutningsbeskeden ikke forsvinder med skærmen.
func (m Model) ExitNote() string {
	return m.exitNote
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

	m := Model{
		agent:         a,
		input:         ta,
		spinner:       sp,
		historyIdx:    -1,
		suggestionIdx: -1,
		maxTokens:     defaultMaxTokens,
	}
	m.syncFromAgent() // synkronisér evt. resumed session-historik
	return m
}

func (m *Model) SetMaxTokens(n int) {
	if n > 0 {
		m.maxTokens = n
	}
}

// SetModelName sætter modelnavnet til statuslinjen (sættes ved opstart;
// opdateres derefter via EventModelInfo når /model skifter model).
// restoreToolLog giver sidepanelet tilbage til seneste tool-output efter at
// reasoning-streaming har lånt det til modellens tanker. No-op uden tanker.
// Var tool-loggen tom lukker panelet — som før reasoning-visningen fandtes.
func (m *Model) restoreToolLog() {
	if m.reasoningBuf == "" {
		return
	}
	m.reasoningBuf = ""
	m.toolOutput = m.toolLog
	if m.toolLog != "" {
		m.toolPanel.SetContent(wordWrap(m.toolLog, m.toolPanelWidth()-4))
	}
}

// SetWorkDir sætter arbejdsmappen til statuslinjen — hjemmemappen forkortes til ~.
func (m *Model) SetWorkDir(dir string) {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(dir, home) {
		dir = "~" + strings.TrimPrefix(dir, home)
	}
	m.workDir = dir
}

func (m *Model) SetModelName(name string) {
	m.modelName = name
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

func (m *Model) AddInfo(msg string) {
	m.messages = append(m.messages, provider.Message{
		Role:    "system",
		Content: msg,
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

func (m Model) toolPanelWidth() int {
	w := int(float64(m.width) * toolPanelRatio)
	if w < toolPanelMinWidth {
		w = toolPanelMinWidth
	}
	if w > toolPanelMaxWidth {
		w = toolPanelMaxWidth
	}
	return w
}

func (m Model) conversationWidth() int {
	w := m.width - 4 // 2 til border + 2 padding
	if m.toolOutput != "" {
		w = m.width - m.toolPanelWidth() - 1 - 4
	}
	if w <= 0 {
		w = 80
	}
	return w
}

func (m Model) conversationContent() string {
	w := m.conversationWidth()
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
			body := renderMd(msg.Content)
			if msg.Source != "" {
				dimStyle := lipgloss.NewStyle().Foreground(colorSubtle)
				body += "\n" + dimStyle.Render("◦ Information fra 📚 wiki · "+wordWrap(msg.Source, w-2))
			}
			sb.WriteString(header + "\n" + body + "\n\n")
		case "system":
			sb.WriteString(styleSystem.Render("◦ "+wordWrap(msg.Content, w-2)) + "\n\n")
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
	if m.streaming && m.streamBuf == "" {
		header := m.msgHeader(styleAssistantLabel.Render("🤖 "+agentName), w)
		sb.WriteString(header + "\n" + m.thinkingLine() + "\n\n")
	} else if m.streaming && m.streamBuf != "" {
		header := m.msgHeader(styleAssistantLabel.Render("🤖 "+agentName), w)
		body := styleAssistantBody.Render(wordWrap(m.streamBuf, w)) + "▌"
		sb.WriteString(header + "\n" + body + "\n\n")
	}
	return sb.String()
}

func (m Model) thinkingLine() string {
	avail := m.conversationWidth() - 4
	if avail < 4 {
		return "🧠"
	}
	span := avail - 2 // 🧠 er 2 bred
	if span < 1 {
		span = 1
	}
	cycle := span * 2
	pos := m.thinkPos % cycle
	if pos > span {
		pos = cycle - pos
	}
	return strings.Repeat(" ", pos) + "🧠"
}

// wordWrap bryder lange linjer ved whitespace uden at ødelægge eksisterende linjeskift.
// wordWrap bryder linjer ved word boundaries. Bruger ansi.StringWidth for at
// ignorere ANSI escape-sekvenser (farver, OSC 8 hyperlinks) i breddeberegningen.
func wordWrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out strings.Builder
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		if ansi.StringWidth(line) <= width {
			out.WriteString(line)
			continue
		}
		col := 0
		for j, word := range strings.Fields(line) {
			wl := ansi.StringWidth(word)
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

	modelIndicator := ""
	if m.modelName != "" {
		name := []rune(m.modelName)
		if len(name) > 28 {
			name = append(name[:27], '…')
		}
		modelIndicator = "  " + styleSystem.Render(string(name))
	}

	modeIndicator := ""
	if m.agent != nil {
		modeIndicator = "  " + styleSlashCmd.Render("mode:"+m.agent.WorkMode())
	}

	dirIndicator := ""
	if m.workDir != "" {
		// Afkort forfra — det er stiens sidste led der fortæller hvor man arbejder.
		dir := []rune(m.workDir)
		if len(dir) > 32 {
			dir = append([]rune("…"), dir[len(dir)-31:]...)
		}
		dirIndicator = "  " + styleSystem.Render("📁 "+string(dir))
	}

	skillIndicator := ""
	if m.agent != nil && m.agent.ActiveSkill() != nil {
		skillIndicator = "  " + styleSlashCmd.Render("skill:"+m.agent.ActiveSkill().Name)
	}

	var right string
	if m.streaming {
		elapsed := time.Since(m.streamStart).Round(time.Second)
		right = styleStatusBar.Render(m.spinner.View() + fmt.Sprintf(" arbejder... %s", elapsed))
	} else {
		soundIcon := "🔇"
		if m.agent != nil && m.agent.SoundEnabled() {
			soundIcon = "🔊"
		}
		right = styleStatusBar.Render(styleSystem.Render("PgUp/PgDn: scrol · Ctrl+Y: kopiér " + soundIcon + " · /hjælp"))
	}

	left := styleStatusBar.Render(ctxStyled + modelIndicator + modeIndicator + dirIndicator + skillIndicator)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	return left + styleStatusBar.Render(strings.Repeat(" ", gap)) + right
}

func (m Model) renderConfirmPrompt() string {
	warnBold := lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	descStyle := lipgloss.NewStyle().Bold(true)
	keyStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(colorSubtle)

	if m.confirmRedirecting {
		line1 := warnBold.Render("⚠  ") + descStyle.Render(m.confirmDesc)
		hint := dimStyle.Render("Hvad skal agenten gøre i stedet? ") +
			keyStyle.Render("Enter") + dimStyle.Render(" sender · ") +
			keyStyle.Render("Esc") + dimStyle.Render(" annullerer")
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorWarn).
			Width(m.width-4).
			Padding(0, 1).
			Render(line1 + "\n" + hint + "\n" + m.input.View())
	}

	line1 := warnBold.Render("⚠  ") + descStyle.Render(m.confirmDesc)
	sep := dimStyle.Render("  ·  ")
	line2 := keyStyle.Render("j") + dimStyle.Render(" tillad") +
		sep + keyStyle.Render("n") + dimStyle.Render(" afvis") +
		sep + keyStyle.Render("Tab") + dimStyle.Render(" afvis + giv ny besked") +
		sep + keyStyle.Render("Ctrl+C") + dimStyle.Render(" afbryd")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorWarn).
		Width(m.width-4).
		Padding(0, 1).
		Render(line1 + "\n" + line2)
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
		tpw := m.toolPanelWidth()
		convW := m.width - tpw - 1
		m.conversation.Width = convW - 2
		m.conversation.Height = panelH - 2
		convView = styleBorder.Width(convW - 2).Height(panelH - 2).Render(m.conversation.View())
		m.toolPanel.Width = tpw - 2
		m.toolPanel.Height = panelH - 2
		hint := styleSystem.Render("PgUp/PgDn: scroll · Esc: luk")
		toolView = styleBorder.Width(tpw - 2).Height(panelH - 2).
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

	var inputArea string
	if m.pendingConfirm {
		inputArea = m.renderConfirmPrompt()
	} else {
		inputArea = styleActiveBorder.Width(m.width - 2).Render(m.input.View())
	}

	parts := []string{panels, inputArea}
	if !m.pendingConfirm {
		if sugg := m.renderSuggestions(); sugg != "" {
			parts = append(parts, sugg)
		}
		if q := m.renderQueue(); q != "" {
			parts = append(parts, q)
		}
	}
	parts = append(parts, m.statusBar())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) updateSuggestions() {
	val := m.input.Value()
	if !strings.HasPrefix(val, "/") || m.agent == nil {
		m.suggestions = nil
		m.suggestionIdx = -1
		return
	}
	query := strings.ToLower(val)
	var matches []string

	if strings.ContainsRune(val, ' ') {
		// Andet-ords-komplettering: tilbyd kun fraser med statiske subkommandoer
		// (fx "/mode beginner", "/sound on") — aldrig placeholder-poster som
		// "/spec <navn>", der ville indsætte "<navn>" bogstaveligt.
		for _, cmd := range m.agent.Commands() {
			if !strings.ContainsRune(cmd, ' ') || strings.ContainsAny(cmd, "<[") {
				continue
			}
			if strings.HasPrefix(strings.ToLower(cmd), query) && cmd != val {
				matches = append(matches, cmd)
			}
		}
		// Dynamiske hook-navne: "/hook te" → "/hook test"
		if strings.HasPrefix(query, "/hook ") {
			for _, h := range m.agent.HookNames() {
				full := "/hook " + h
				if strings.HasPrefix(strings.ToLower(full), query) && full != val {
					matches = append(matches, full)
				}
			}
		}
	} else {
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
	// Vis beskrivelsen for det fremhævede forslag (fx at /skills show tager nr|navn).
	if m.suggestionIdx >= 0 && m.suggestionIdx < len(m.suggestions) && m.agent != nil {
		if desc := m.agent.DescribeCommand(m.suggestions[m.suggestionIdx]); desc != "" {
			sb.WriteString("\n  " + dimStyle.Render(desc))
		}
	}
	return sb.String()
}

func (m Model) renderQueue() string {
	if len(m.promptQueue) == 0 {
		return ""
	}
	dimStyle := lipgloss.NewStyle().Foreground(colorSubtle)
	hintStyle := lipgloss.NewStyle().Foreground(colorBorder)

	var items []string
	for i, q := range m.promptQueue {
		preview := strings.ReplaceAll(q, "\n", " ")
		if len(preview) > 40 {
			preview = preview[:40] + "…"
		}
		items = append(items, fmt.Sprintf("%d. %s", i+1, preview))
	}
	return "  " + dimStyle.Render("⏳ i kø: "+strings.Join(items, "  ·  ")) +
		"  " + hintStyle.Render("(afvikles automatisk)")
}

func (m *Model) handleQueueCmd(arg string) tea.Cmd {
	parts := strings.Fields(arg)
	subCmd := ""
	if len(parts) > 0 {
		subCmd = strings.ToLower(parts[0])
	}

	switch subCmd {
	case "":
		// Vis kø
		if len(m.promptQueue) == 0 {
			m.appendSystem("Ingen prompts i kø.")
			return nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Prompt-kø (%d):\n\n", len(m.promptQueue)))
		for i, q := range m.promptQueue {
			preview := strings.ReplaceAll(q, "\n", " ")
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, preview))
		}
		sb.WriteString("\n/kø slet <n> fjerner en prompt — /kø ryd fjerner alle")
		m.appendSystem(sb.String())
		return nil

	case "slet":
		if len(parts) < 2 {
			m.appendSystem("Brug: /kø slet <nummer>  — fx /kø slet 1")
			return nil
		}
		n := 0
		if _, err := fmt.Sscanf(parts[1], "%d", &n); err != nil || n < 1 || n > len(m.promptQueue) {
			m.appendSystem(fmt.Sprintf("Ugyldigt nummer — køen har %d prompt(s).", len(m.promptQueue)))
			return nil
		}
		removed := m.promptQueue[n-1]
		m.promptQueue = append(m.promptQueue[:n-1], m.promptQueue[n:]...)
		preview := strings.ReplaceAll(removed, "\n", " ")
		if len(preview) > 60 {
			preview = preview[:60] + "…"
		}
		m.appendSystem(fmt.Sprintf("✓ Fjernet fra kø: %q", preview))
		return nil

	case "ryd":
		count := len(m.promptQueue)
		m.promptQueue = nil
		m.appendSystem(fmt.Sprintf("✓ Kø ryddet (%d prompt(s) fjernet).", count))
		return nil

	default:
		m.appendSystem("Ukendt kø-kommando.\n  /kø          — vis kø\n  /kø slet <n> — fjern prompt\n  /kø ryd      — ryd alle")
		return nil
	}
}

func (m *Model) appendSystem(content string) {
	m.messages = append(m.messages, provider.Message{Role: "system", Content: content})
	m.conversation.SetContent(m.conversationContent())
	m.conversation.GotoBottom()
}

// lastAssistantText returnerer indholdet af det seneste assistent-svar, eller "" hvis intet findes.
func (m *Model) lastAssistantText() string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == "assistant" && m.messages[i].Content != "" {
			return m.messages[i].Content
		}
	}
	return ""
}
