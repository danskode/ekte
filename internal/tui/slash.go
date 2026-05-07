package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/danskode/ekte/internal/skill"
)

type slashResult struct {
	handled bool
	cmd     tea.Cmd
	output  string
}

func (m *Model) handleSlash(input string) slashResult {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return slashResult{}
	}

	parts := strings.SplitN(input, " ", 2)
	command := strings.ToLower(parts[0])
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	switch command {
	case "/hjælp", "/help":
		return slashResult{handled: true, output: helpText()}

	case "/skills":
		if len(m.skills) == 0 {
			return slashResult{handled: true, output: "Ingen skills fundet i .ekte/skills/"}
		}
		if arg != "" {
			if m.ActivateSkill(arg) {
				return slashResult{handled: true, output: styleSuccess.Render("✓ Skill aktiveret: " + arg + " (gælder for næste prompt)")}
			}
			return slashResult{handled: true, output: "Skill ikke fundet: " + arg}
		}
		return slashResult{handled: true, output: renderSkillsList(m.skills)}

	case "/spec":
		if m.repoRoot == "" {
			return slashResult{handled: true, output: styleError.Render("Ikke i et git-repo. Kør 'git init' først.")}
		}
		if arg == "" || arg == "list" {
			return slashResult{
				handled: true,
				output:  styleSystem.Render("Henter worktrees..."),
				cmd:     worktreeListCmd(m.repoRoot),
			}
		}
		subparts := strings.SplitN(arg, " ", 2)
		switch subparts[0] {
		case "merge":
			if len(subparts) < 2 {
				return slashResult{handled: true, output: "Brug: /spec merge <navn>"}
			}
			return slashResult{
				handled: true,
				output:  styleSystem.Render("Kører hooks og merger: " + subparts[1] + "..."),
				cmd:     worktreeMergeCmd(m.repoRoot, subparts[1], nil),
			}
		case "remove":
			if len(subparts) < 2 {
				return slashResult{handled: true, output: "Brug: /spec remove <navn>"}
			}
			return slashResult{
				handled: true,
				output:  styleSystem.Render("Fjerner worktree: " + subparts[1] + "..."),
				cmd:     worktreeRemoveCmd(m.repoRoot, subparts[1]),
			}
		default:
			return slashResult{
				handled: true,
				output:  styleSystem.Render("Opretter spec og worktree: " + arg + "..."),
				cmd:     worktreeCreateCmd(m.repoRoot, arg),
			}
		}

	case "/compress":
		return slashResult{handled: true, output: "[kontekst-komprimering — ikke implementeret endnu]"}

	case "/wiki":
		if m.wiki == nil {
			return slashResult{handled: true, output: styleError.Render("Wiki ikke sat op. Kør 'ekte init' for at konfigurere.")}
		}
		if arg == "" {
			return slashResult{handled: true, output: "Brug: /wiki \"spørgsmål\" eller /wiki gem <titel>"}
		}
		subparts := strings.SplitN(arg, " ", 2)
		if subparts[0] == "gem" {
			if m.pendingWikiSave == "" {
				return slashResult{handled: true, output: "Intet at gemme endnu — brug /forresten først."}
			}
			title := "Notat"
			if len(subparts) > 1 {
				title = subparts[1]
			}
			return slashResult{
				handled: true,
				output:  styleSystem.Render("Gemmer i wiki: " + title + "..."),
				cmd:     wikiSaveCmd(m.wiki, "concept", title, m.pendingWikiSave),
			}
		}
		return slashResult{
			handled: true,
			output:  styleSystem.Render("Søger i wiki: " + arg + "..."),
			cmd:     wikiQueryCmd(m.wiki, m.provider, arg, m.messages),
		}

	case "/hook":
		if arg == "" {
			return slashResult{handled: true, output: "Brug: /hook <navn>"}
		}
		return slashResult{handled: true, output: "[kører hook: " + arg + " — ikke implementeret endnu]"}

	case "/forresten":
		if arg == "" {
			return slashResult{handled: true, output: "Brug: /forresten <dit spørgsmål>"}
		}
		return slashResult{
			handled: true,
			cmd:     forrestenCmd(m.provider, m.forrestenHist, arg),
			output:  styleSystem.Render("forresten: " + arg),
		}

	case "/clear":
		m.messages = nil
		m.tokenCount = 0
		m.conversation.SetContent("")
		return slashResult{handled: true}

	case "/exit":
		if m.sessionDir == "" || len(m.messages) == 0 {
			return slashResult{handled: true, cmd: tea.Quit}
		}
		return slashResult{
			handled: true,
			output:  styleSystem.Render("Gemmer session..."),
			cmd:     sessionSaveCmd(m.sessionDir, m.messages),
		}

	case "/resume":
		if m.sessionDir == "" {
			return slashResult{handled: true, output: "Ingen session-mappe konfigureret."}
		}
		if arg != "" {
			// indlæs session nr. <arg>
			idx := 0
			fmt.Sscanf(arg, "%d", &idx)
			if idx < 1 || idx > len(m.sessions) {
				return slashResult{handled: true, output: fmt.Sprintf("Ugyldigt nummer — vælg 1-%d.", len(m.sessions))}
			}
			s := m.sessions[idx-1]
			m.messages = s.Messages
			m.tokenCount = 0
			for _, msg := range m.messages {
				m.tokenCount += len(msg.Content) / 4
			}
			m.conversation.SetContent(m.conversationContent())
			m.conversation.GotoBottom()
			return slashResult{
				handled: true,
				output:  styleSuccess.Render(fmt.Sprintf("✓ Session indlæst: %s", s.Title)),
			}
		}
		return slashResult{
			handled: true,
			cmd:     sessionListCmd(m.sessionDir),
		}
	}

	return slashResult{handled: true, output: "Ukendt kommando: " + command + " (prøv /hjælp)"}
}

func renderSkillsList(skills []skill.Skill) string {
	var sb strings.Builder
	sb.WriteString(styleSlashCmd.Render("Skills") + " — brug '/skills <navn>' for at aktivere\n\n")
	for _, s := range skills {
		tags := ""
		if len(s.Tags) > 0 {
			tags = styleSystem.Render(" [" + strings.Join(s.Tags, ", ") + "]")
		}
		sb.WriteString(fmt.Sprintf("  %s%s\n  %s\n\n",
			styleSlashCmd.Render(s.Name),
			tags,
			s.Description,
		))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func helpText() string {
	cmds := []struct{ cmd, desc string }{
		{"/skills [navn]", "vis skills — angiv navn for at aktivere"},
		{"/spec <navn>", "opret spec + git worktree"},
		{"/compress", "komprimer kontekstvindue"},
		{"/wiki \"spørgsmål\"", "søg i din personlige wiki"},
		{"/hook <navn>", "kør hook manuelt"},
		{"/forresten <besked>", "side-chat med subagent (husker historik)"},
		{"/clear", "ryd samtalen"},
		{"/exit", "gem session og afslut"},
		{"/resume [nummer]", "vis eller indlæs tidligere sessioner"},
		{"/hjælp", "vis denne hjælp"},
	}
	var sb strings.Builder
	sb.WriteString(styleSlashCmd.Render("Slash commands:\n"))
	for _, c := range cmds {
		sb.WriteString("  " + styleSlashCmd.Render(c.cmd) + "  — " + c.desc + "\n")
	}
	return sb.String()
}
