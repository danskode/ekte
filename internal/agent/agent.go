package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/danskode/ekte/internal/dep"
	"github.com/danskode/ekte/internal/git"
	"github.com/danskode/ekte/internal/provider"
	"github.com/danskode/ekte/internal/session"
	"github.com/danskode/ekte/internal/skill"
	"github.com/danskode/ekte/internal/tools"
	"github.com/danskode/ekte/internal/wiki"
)

type EventType int

const (
	EventAssistant  EventType = iota // svar fra LLM (ikke-streaming)
	EventSystem                      // info/status besked
	EventError                       // fejlbesked
	EventQuit                        // afslut applikation
	EventTokenCount                  // opdateret token-estimat
	EventToolOutput                  // output til tool-panel
	EventStreamToken                 // streaming: et token fra LLM
	EventStreamDone                  // streaming: fuldt svar klar (Content = hele teksten)
	EventForresten                   // svar fra /forresten subagent
)

type Event struct {
	Type    EventType
	Content string
	Tokens  int
	Prefill string // hvis sat, pre-udfyld inputfeltet i TUI
	Source  string // wiki-kilde, vises efter svaret
}

type Config struct {
	Provider   provider.Provider
	Wiki       *wiki.Wiki
	RepoRoot   string
	WorkDir    string // rod for filoperationer — altid cwd ved opstart
	SessionDir string
	Skills     []skill.Skill
	Whitelist  provider.WhitelistConfig
	Hooks      map[string]string
}

type Agent struct {
	cfg             Config
	messages        []provider.Message
	forrestenHist   []provider.Message
	activeSkill     *skill.Skill
	sessions        []session.Session
	pendingWikiSave string
	pendingWikiFetch string // indhold fra /wiki-get, klar til /wiki-gem
	pendingWikiPath  string // foreslået sti fra /wiki-get
	tokenCount      int
}

func New(cfg Config) *Agent {
	return &Agent{cfg: cfg}
}

func (a *Agent) Messages() []provider.Message        { return a.messages }
func (a *Agent) Skills() []skill.Skill               { return a.cfg.Skills }
func (a *Agent) ActiveSkill() *skill.Skill           { return a.activeSkill }
func (a *Agent) TokenCount() int                     { return a.tokenCount }
func (a *Agent) Sessions() []session.Session         { return a.sessions }
func (a *Agent) PendingWikiSave() string             { return a.pendingWikiSave }

func (a *Agent) Commands() []string {
	builtin := []string{
		"/hjælp", "/clear", "/compress", "/spec", "/wiki", "/wiki-get", "/wiki-gem",
		"/forresten", "/hook", "/skills", "/dep", "/sec-check",
		"/resume", "/exit",
	}
	for _, s := range a.cfg.Skills {
		builtin = append(builtin, "/"+s.Name)
	}
	return builtin
}

func (a *Agent) AddContext(role, content string) {
	a.messages = append(a.messages, provider.Message{Role: role, Content: content})
}

// Process håndterer bruger-input og returnerer events til præsentationslaget.
func (a *Agent) Process(ctx context.Context, input string) []Event {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	if strings.HasPrefix(input, "/") {
		return a.handleSlash(ctx, input)
	}

	return a.handleChat(ctx, input)
}

func (a *Agent) handleChat(ctx context.Context, input string) []Event {
	a.messages = append(a.messages, provider.Message{Role: "user", Content: input})

	if a.cfg.Provider == nil {
		return []Event{{Type: EventError, Content: "Ingen LLM konfigureret. Sæt din API-nøgle og genstart ekte."}}
	}

	msgs := a.messagesWithSkill()
	a.clearSkill()

	resp, err := a.cfg.Provider.Chat(ctx, msgs)
	if err != nil {
		return []Event{{Type: EventError, Content: "LLM-fejl: " + err.Error()}}
	}

	a.messages = append(a.messages, provider.Message{Role: "assistant", Content: resp.Content})
	a.tokenCount = estimateTokens(a.messages)

	return []Event{
		{Type: EventAssistant, Content: resp.Content},
		{Type: EventTokenCount, Tokens: a.tokenCount},
	}
}

// ProcessStream kører input og sender events løbende via en kanal.
// Brug denne i stedet for Process til streaming-chat i TUI.
// Slash commands sendes stadig som en batch og kanalen lukkes derefter.
func (a *Agent) ProcessStream(ctx context.Context, input string) <-chan Event {
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		input = strings.TrimSpace(input)
		if input == "" {
			return
		}
		if strings.HasPrefix(input, "/wiki-get") {
			a.handleWikiGet(ctx, strings.TrimSpace(strings.TrimPrefix(input, "/wiki-get")), ch)
			return
		}
		if strings.HasPrefix(input, "/") {
			for _, ev := range a.handleSlash(ctx, input) {
				ch <- ev
			}
			return
		}
		a.streamChat(ctx, input, ch)
	}()
	return ch
}

func (a *Agent) streamChat(ctx context.Context, input string, ch chan<- Event) {
	a.messages = append(a.messages, provider.Message{Role: "user", Content: input})

	if a.cfg.Provider == nil {
		ch <- Event{Type: EventError, Content: "Ingen LLM konfigureret. Sæt din API-nøgle og genstart ekte."}
		return
	}

	msgs := a.messagesWithSkill()
	a.clearSkill()

	// Injicér wiki som autoritativ kilde — skal altid prioriteres over generel viden
	var wikiSource string
	if a.cfg.Wiki != nil {
		_, pages, err := a.cfg.Wiki.Query(input)
		if err == nil && len(pages) > 0 {
			var ctxBuilder strings.Builder
			var paths []string
			ctxBuilder.WriteString("VIGTIG INSTRUKTION: Følgende wiki-sider er projektets kilde til sandhed.\n")
			ctxBuilder.WriteString("Kodestandarder, arkitektur og ønsker herfra SKAL følges og prioriteres over generel viden.\n\n")
			for _, p := range pages {
				ctxBuilder.WriteString(fmt.Sprintf("=== %s ===\n%s\n\n", p.Path, p.Content))
				paths = append(paths, p.Path)
			}
			msgs = append([]provider.Message{{Role: "system", Content: ctxBuilder.String()}}, msgs...)
			wikiSource = "📚 " + strings.Join(paths, " · ")
		}
	}

	toolDefs := tools.Definitions(a.cfg.Whitelist.FileRead, a.cfg.Whitelist.FileWrite)
	workdir := a.cfg.WorkDir
	if workdir == "" {
		workdir, _ = os.Getwd()
	}

	// Tool-loop: kør til LLM stopper med at kalde tools
	for {
		if len(toolDefs) > 0 {
			// Brug ChatWithTools (ikke streaming) når tools er aktive —
			// streaming og tool calls kombineres ikke i denne version
			resp, err := a.cfg.Provider.ChatWithTools(ctx, msgs, toolDefs)
			if err != nil {
				ch <- Event{Type: EventError, Content: "LLM-fejl: " + err.Error()}
				return
			}

			if len(resp.ToolCalls) == 0 {
				// Intet tool call — vis svar og afslut
				a.messages = append(a.messages, provider.Message{Role: "assistant", Content: resp.Content})
				a.tokenCount = estimateTokens(a.messages)
				ch <- Event{Type: EventStreamDone, Content: resp.Content, Source: wikiSource}
				ch <- Event{Type: EventTokenCount, Tokens: a.tokenCount}
				return
			}

			// Eksekver tool calls og send output til tool-panel
			assistantMsg := provider.Message{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls}
			msgs = append(msgs, assistantMsg)

			var toolLog strings.Builder
			for _, tc := range resp.ToolCalls {
				result, err := tools.Execute(tc, workdir, a.cfg.Whitelist.FileRead, a.cfg.Whitelist.FileWrite)
				if err != nil {
					result = "Fejl: " + err.Error()
				}
				toolLog.WriteString(fmt.Sprintf("tool: %s\n%s\n\n", tc.Name, result))
				msgs = append(msgs, provider.Message{
					Role:       "tool",
					Content:    result,
					ToolCallID: tc.ID,
				})
			}
			ch <- Event{Type: EventToolOutput, Content: strings.TrimRight(toolLog.String(), "\n")}

			// Fortsæt loop — LLM skal svare på tool-resultater
			continue
		}

		// Ingen tools tilgængelige — stream direkte
		tokenCh, err := a.cfg.Provider.Stream(ctx, msgs)
		if err != nil {
			ch <- Event{Type: EventError, Content: "LLM-fejl: " + err.Error()}
			return
		}

		var sb strings.Builder
		for token := range tokenCh {
			sb.WriteString(token)
			ch <- Event{Type: EventStreamToken, Content: token}
		}

		full := sb.String()
		if full == "" {
			ch <- Event{Type: EventError, Content: "Tom respons fra LLM."}
			return
		}

		a.messages = append(a.messages, provider.Message{Role: "assistant", Content: full})
		a.tokenCount = estimateTokens(a.messages)
		ch <- Event{Type: EventStreamDone, Content: full, Source: wikiSource}
		ch <- Event{Type: EventTokenCount, Tokens: a.tokenCount}
		return
	}
}

func (a *Agent) handleSlash(ctx context.Context, input string) []Event {
	parts := strings.SplitN(input, " ", 2)
	cmd := strings.ToLower(parts[0])
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case "/hjælp", "/help":
		return []Event{{Type: EventSystem, Content: helpText()}}

	case "/clear":
		a.messages = nil
		a.tokenCount = 0
		return []Event{{Type: EventSystem, Content: ""}}

	case "/skills":
		return a.handleSkills(arg)

	case "/spec":
		return a.handleSpec(ctx, arg)

	case "/wiki":
		return a.handleWiki(ctx, arg)

	case "/wiki-gem":
		return a.handleWikiGem(arg)

	case "/forresten":
		if arg == "" {
			return []Event{{Type: EventSystem, Content: "Brug: /forresten <dit spørgsmål>"}}
		}
		return a.handleForresten(ctx, arg)

	case "/compress":
		return a.handleCompress(ctx)

	case "/hook":
		if arg == "" {
			return a.handleHookList()
		}
		if !a.cfg.Whitelist.HookRun {
			return []Event{{Type: EventSystem, Content: denyMsg("hook_run")}}
		}
		return a.handleHook(arg)

	case "/dep":
		if arg == "" {
			return []Event{{Type: EventSystem, Content: "Brug: /dep <go-modul-sti>  — fx /dep github.com/some/pkg"}}
		}
		return a.handleDep(ctx, arg)

	case "/sec-check":
		return a.handleDeps(ctx)

	case "/exit":
		return a.handleExit()

	case "/resume":
		return a.handleResume(arg)
	}

	return []Event{{Type: EventSystem, Content: "Ukendt kommando: " + cmd + " (prøv /hjælp)"}}
}

func (a *Agent) handleSkills(arg string) []Event {
	if len(a.cfg.Skills) == 0 {
		return []Event{{Type: EventSystem, Content: "Ingen skills fundet i .ekte/skills/"}}
	}
	if arg != "" {
		for i := range a.cfg.Skills {
			if a.cfg.Skills[i].Name == arg {
				a.activeSkill = &a.cfg.Skills[i]
				return []Event{{Type: EventSystem, Content: "✓ Skill aktiveret: " + arg + " (gælder for næste prompt)"}}
			}
		}
		return []Event{{Type: EventSystem, Content: "Skill ikke fundet: " + arg}}
	}
	return []Event{{Type: EventSystem, Content: renderSkillsList(a.cfg.Skills)}}
}

func (a *Agent) handleSpec(ctx context.Context, arg string) []Event {
	var initEvents []Event
	if a.cfg.RepoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return []Event{{Type: EventError, Content: "Kan ikke finde arbejdsmappe: " + err.Error()}}
		}
		if out, err := exec.Command("git", "-C", cwd, "init").CombinedOutput(); err != nil {
			return []Event{{Type: EventError, Content: "git init fejlede: " + strings.TrimSpace(string(out))}}
		}
		if out, err := exec.Command("git", "-C", cwd, "commit", "--allow-empty", "-m", "init").CombinedOutput(); err != nil {
			return []Event{{Type: EventError, Content: "git commit fejlede: " + strings.TrimSpace(string(out))}}
		}
		a.cfg.RepoRoot = cwd
		initEvents = []Event{{Type: EventSystem, Content: "✓ Git-repo initialiseret"}}
	}
	result := a.execSpec(arg)
	return append(initEvents, result...)
}

func (a *Agent) execSpec(arg string) []Event {
	if arg == "" || arg == "list" {
		wts, err := git.List(a.cfg.RepoRoot)
		if err != nil {
			return []Event{{Type: EventError, Content: err.Error()}}
		}
		return []Event{{Type: EventSystem, Content: renderWorktreeList(wts)}}
	}
	if !a.cfg.Whitelist.GitWorktree {
		return []Event{{Type: EventSystem, Content: denyMsg("git_worktree")}}
	}
	subparts := strings.SplitN(arg, " ", 2)
	switch subparts[0] {
	case "merge":
		if len(subparts) < 2 {
			return []Event{{Type: EventSystem, Content: "Brug: /spec merge <navn>"}}
		}
		if err := git.Merge(a.cfg.RepoRoot, subparts[1], nil); err != nil {
			return []Event{{Type: EventError, Content: "Merge fejlede: " + err.Error()}}
		}
		return []Event{{Type: EventSystem, Content: "✓ Merget og ryddet op: " + subparts[1]}}
	case "remove":
		if len(subparts) < 2 {
			return []Event{{Type: EventSystem, Content: "Brug: /spec remove <navn>"}}
		}
		if err := git.Remove(a.cfg.RepoRoot, subparts[1]); err != nil {
			return []Event{{Type: EventError, Content: err.Error()}}
		}
		return []Event{{Type: EventSystem, Content: "✓ Worktree fjernet: " + subparts[1]}}
	default:
		wt, err := git.Create(a.cfg.RepoRoot, arg)
		if err != nil {
			return []Event{{Type: EventError, Content: err.Error()}}
		}
		return []Event{{Type: EventSystem, Content: fmt.Sprintf(
			"✓ Worktree oprettet: %s\n  branch: %s\n  spec:   %s",
			wt.Name, wt.Branch, wt.Spec,
		)}}
	}
}

func (a *Agent) handleWiki(ctx context.Context, arg string) []Event {
	if a.cfg.Wiki == nil {
		return []Event{{Type: EventError, Content: "Wiki ikke sat op. Kør 'ekte init'."}}
	}
	if arg == "" {
		return []Event{{Type: EventSystem, Content: "Brug: /wiki \"spørgsmål\" eller /wiki gem <titel>"}}
	}
	subparts := strings.SplitN(arg, " ", 2)
	if subparts[0] == "gem" {
		if !a.cfg.Whitelist.WikiWrite {
			return []Event{{Type: EventSystem, Content: denyMsg("wiki_write")}}
		}
		if a.pendingWikiSave == "" {
			return []Event{{Type: EventSystem, Content: "Intet at gemme endnu — brug /forresten først."}}
		}
		title := "Notat"
		if len(subparts) > 1 {
			title = subparts[1]
		}
		path, err := a.cfg.Wiki.SavePage("concept", title, a.pendingWikiSave)
		if err != nil {
			return []Event{{Type: EventError, Content: "Gem fejlede: " + err.Error()}}
		}
		a.pendingWikiSave = ""
		return []Event{{Type: EventSystem, Content: "✓ Gemt i wiki: " + path}}
	}

	wikiCtx, pages, err := a.cfg.Wiki.Query(arg)
	if err != nil {
		return []Event{{Type: EventError, Content: "Wiki-fejl: " + err.Error()}}
	}
	msgs := append([]provider.Message{{Role: "system", Content: wikiCtx}}, a.messages...)
	msgs = append(msgs, provider.Message{Role: "user", Content: arg})
	resp, err := a.cfg.Provider.Chat(ctx, msgs)
	if err != nil {
		return []Event{{Type: EventError, Content: err.Error()}}
	}
	var source string
	if len(pages) > 0 {
		var paths []string
		for _, p := range pages {
			paths = append(paths, p.Path)
		}
		source = strings.Join(paths, " · ")
	}
	return []Event{
		{Type: EventAssistant, Content: resp.Content, Source: source},
	}
}

func (a *Agent) handleForresten(ctx context.Context, arg string) []Event {
	msgs := append(a.forrestenHist, provider.Message{Role: "user", Content: arg})
	resp, err := a.cfg.Provider.Chat(ctx, msgs)
	if err != nil {
		return []Event{{Type: EventError, Content: err.Error()}}
	}
	a.forrestenHist = append(msgs, provider.Message{Role: "assistant", Content: resp.Content})
	a.pendingWikiSave = resp.Content

	events := []Event{
		{Type: EventForresten, Content: resp.Content},
	}
	if a.cfg.Wiki != nil {
		events = append(events, Event{
			Type:    EventSystem,
			Content: "Vil du gemme dette i din wiki? Skriv '/wiki gem <titel>' eller ignorer.",
		})
	}
	return events
}

func (a *Agent) handleExit() []Event {
	if a.cfg.SessionDir == "" || len(a.messages) == 0 {
		return []Event{{Type: EventQuit}}
	}
	s, err := session.Save(a.cfg.SessionDir, a.messages)
	if err != nil {
		return []Event{
			{Type: EventError, Content: "Gem fejlede: " + err.Error()},
			{Type: EventQuit},
		}
	}
	return []Event{
		{Type: EventSystem, Content: "✓ Session gemt: " + s.Title},
		{Type: EventQuit},
	}
}

func (a *Agent) handleResume(arg string) []Event {
	sessions, err := session.LoadAll(a.cfg.SessionDir)
	if err != nil {
		return []Event{{Type: EventError, Content: err.Error()}}
	}
	a.sessions = sessions

	if arg == "" {
		return []Event{{Type: EventSystem, Content: session.RenderList(sessions)}}
	}

	var idx int
	fmt.Sscanf(arg, "%d", &idx)
	if idx < 1 || idx > len(sessions) {
		return []Event{{Type: EventSystem, Content: fmt.Sprintf("Vælg 1-%d.", len(sessions))}}
	}
	s := sessions[idx-1]
	a.messages = s.Messages
	a.tokenCount = estimateTokens(a.messages)
	return []Event{
		{Type: EventSystem, Content: "✓ Session indlæst: " + s.Title},
		{Type: EventTokenCount, Tokens: a.tokenCount},
	}
}

func (a *Agent) messagesWithSkill() []provider.Message {
	if a.activeSkill == nil || a.activeSkill.SystemPromptAddition == "" {
		return a.messages
	}
	out := make([]provider.Message, 0, len(a.messages)+1)
	out = append(out, provider.Message{Role: "system", Content: a.activeSkill.SystemPromptAddition})
	return append(out, a.messages...)
}

func (a *Agent) clearSkill() { a.activeSkill = nil }

func estimateTokens(messages []provider.Message) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4
	}
	return total
}

func renderSkillsList(skills []skill.Skill) string {
	var sb strings.Builder
	sb.WriteString("Skills — brug '/skills <navn>' for at aktivere:\n\n")
	for _, s := range skills {
		tags := ""
		if len(s.Tags) > 0 {
			tags = " [" + strings.Join(s.Tags, ", ") + "]"
		}
		sb.WriteString(fmt.Sprintf("  %s%s\n  %s\n\n", s.Name, tags, s.Description))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func renderWorktreeList(wts []git.Worktree) string {
	if len(wts) == 0 {
		return "Ingen aktive worktrees. Brug '/spec <navn>' for at oprette en."
	}
	var sb strings.Builder
	sb.WriteString("Aktive worktrees:\n\n")
	for _, wt := range wts {
		sb.WriteString(fmt.Sprintf("  %s\n  branch: %s\n  sti: %s\n\n", wt.Name, wt.Branch, wt.Path))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (a *Agent) handleWikiGem(customPath string) []Event {
	if a.pendingWikiFetch == "" {
		return []Event{{Type: EventSystem, Content: "Ingen wiki-indhold klar. Kør /wiki-get <url> først."}}
	}
	if !a.cfg.Whitelist.WikiWrite {
		return []Event{{Type: EventSystem, Content: denyMsg("wiki_write")}}
	}
	if a.cfg.Wiki == nil {
		return []Event{{Type: EventSystem, Content: "Wiki er ikke aktiveret i config."}}
	}

	targetPath := customPath
	if targetPath == "" {
		targetPath = a.pendingWikiPath
	}
	if targetPath == "" {
		return []Event{{Type: EventSystem, Content: "Angiv en sti: /wiki-gem concepts/emne.md"}}
	}

	savedPath, err := a.cfg.Wiki.SaveRaw(targetPath, a.pendingWikiFetch)
	if err != nil {
		return []Event{{Type: EventError, Content: "Kunne ikke gemme: " + err.Error()}}
	}

	a.pendingWikiFetch = ""
	a.pendingWikiPath = ""
	return []Event{{Type: EventSystem, Content: "✓ Gemt: " + savedPath}}
}

func (a *Agent) handleWikiGet(ctx context.Context, rawURL string, ch chan<- Event) {
	if !a.cfg.Whitelist.WikiFetch {
		ch <- Event{Type: EventSystem, Content: denyMsg("wiki_fetch")}
		return
	}
	if rawURL == "" {
		ch <- Event{Type: EventSystem, Content: "Brug: /wiki-get <url>"}
		return
	}

	ch <- Event{Type: EventSystem, Content: "↓ Henter " + rawURL + "..."}

	content, err := tools.FetchURL(rawURL)
	if err != nil {
		ch <- Event{Type: EventError, Content: "Kunne ikke hente URL: " + err.Error()}
		return
	}

	ch <- Event{Type: EventSystem, Content: fmt.Sprintf("✓ %d tegn hentet — analyserer...", len(content))}

	wikiCtx := ""
	if a.cfg.Wiki != nil {
		wikiCtx = "\nMin wiki er aktiveret."
	}

	prompt := fmt.Sprintf(`Analyser dette webindhold og hjælp mig med at tilføje det til min wiki.
URL: %s%s

Indhold:
%s

Svar i præcis dette format:
- Første linje: kun den foreslåede wiki-filsti, fx: concepts/emne.md
- Anden linje: tom
- Tredje linje og frem: opsummering (2-3 sætninger) efterfulgt af det komplette wiki-indlæg i markdown på dansk`, rawURL, wikiCtx, content)

	if a.cfg.Provider == nil {
		ch <- Event{Type: EventError, Content: "Ingen LLM konfigureret."}
		return
	}

	msgs := append(a.messages, provider.Message{Role: "user", Content: prompt})
	tokenCh, err := a.cfg.Provider.Stream(ctx, msgs)
	if err != nil {
		ch <- Event{Type: EventError, Content: err.Error()}
		return
	}

	var full strings.Builder
	for tok := range tokenCh {
		ch <- Event{Type: EventStreamToken, Content: tok}
		full.WriteString(tok)
	}

	response := full.String()
	suggestedPath, body := parseWikiGetResponse(response)
	a.pendingWikiFetch = body
	a.pendingWikiPath = suggestedPath

	if suggestedPath != "" {
		ch <- Event{
			Type:    EventSystem,
			Content: fmt.Sprintf("Foreslået sti: %s — tryk Enter for at gemme", suggestedPath),
			Prefill: "/wiki-gem " + suggestedPath,
		}
	} else {
		ch <- Event{Type: EventSystem, Content: "Skriv /wiki-gem <sti> for at gemme, fx /wiki-gem concepts/emne.md"}
	}

	ch <- Event{Type: EventStreamDone, Content: response}
}

func parseWikiGetResponse(s string) (path, body string) {
	lines := strings.SplitN(s, "\n", 3)
	if len(lines) == 0 {
		return "", s
	}
	first := strings.TrimSpace(lines[0])
	if strings.Contains(first, ".md") && !strings.Contains(first, " ") {
		body = ""
		if len(lines) >= 3 {
			body = strings.TrimSpace(lines[2])
		} else if len(lines) == 2 {
			body = strings.TrimSpace(lines[1])
		}
		return first, body
	}
	return "", s
}

func (a *Agent) handleDep(ctx context.Context, module string) []Event {
	sc := dep.Check(ctx, module)
	return []Event{{Type: EventToolOutput, Content: sc.Render()}}
}

func (a *Agent) handleDeps(ctx context.Context) []Event {
	var sections []string

	legend := "✓ ingen kendte CVEer\n⚠ sårbarhed fundet\n? ikke i OSV-database\n"

	// Projektets go.mod — listen over alle tredjepartspakker projektet bruger
	gomodPath := "go.mod"
	if a.cfg.RepoRoot != "" {
		gomodPath = filepath.Join(a.cfg.RepoRoot, "go.mod")
	}
	projectMods, err := dep.ParseGoMod(gomodPath)
	if err == nil && len(projectMods) > 0 {
		scores := dep.CheckAll(ctx, projectMods)
		sections = append(sections, dep.RenderReport(
			fmt.Sprintf("Dit projekt — %d pakker fra go.mod", len(projectMods)), scores,
		))
	} else if err != nil {
		sections = append(sections, "Ingen go.mod fundet i projektet.")
	}

	// ekte-harness egne afhængigheder
	ekteMods := dep.EkteDeps()
	if len(ekteMods) > 0 {
		ekteScores := dep.CheckAll(ctx, ekteMods)
		sections = append(sections, dep.RenderReport(
			fmt.Sprintf("ekte selv — %d pakker", len(ekteMods)), ekteScores,
		))
	}

	if len(sections) == 0 {
		return []Event{{Type: EventSystem, Content: "Ingen afhængigheder fundet."}}
	}

	output := legend + "\n" + strings.Join(sections, "\n\n────────────────────────\n\n")
	return []Event{
		{Type: EventSystem, Content: fmt.Sprintf("Tjekker %d moduler...", len(projectMods)+len(dep.EkteDeps()))},
		{Type: EventToolOutput, Content: output},
	}
}

func (a *Agent) handleCompress(ctx context.Context) []Event {
	if a.cfg.Provider == nil {
		return []Event{{Type: EventError, Content: "Ingen LLM konfigureret."}}
	}
	if len(a.messages) < 4 {
		return []Event{{Type: EventSystem, Content: "Samtalen er for kort til at komprimere."}}
	}

	compressPrompt := "Lav et kort, præcist resumé af denne samtale på dansk. " +
		"Bevar alle vigtige beslutninger, kodedetaljer og kontekst. " +
		"Resuméet bruges som erstatning for samtalehistorikken, så ingenting vigtigt må gå tabt."

	msgs := append(a.messages, provider.Message{Role: "user", Content: compressPrompt})
	resp, err := a.cfg.Provider.Chat(ctx, msgs)
	if err != nil {
		return []Event{{Type: EventError, Content: "Komprimering fejlede: " + err.Error()}}
	}

	before := a.tokenCount
	a.messages = []provider.Message{
		{Role: "system", Content: "Resumé af tidligere samtale:\n\n" + resp.Content},
	}
	a.tokenCount = estimateTokens(a.messages)

	return []Event{
		{Type: EventSystem, Content: fmt.Sprintf(
			"✓ Kontekst komprimeret: %d → %d tokens", before, a.tokenCount,
		)},
		{Type: EventTokenCount, Tokens: a.tokenCount},
	}
}

func (a *Agent) handleHookList() []Event {
	if len(a.cfg.Hooks) == 0 {
		return []Event{{Type: EventSystem, Content: "Ingen hooks konfigureret.\n\nTilføj til .ekte/config.yaml:\n\n  hooks:\n    test: go test ./...\n    lint: golangci-lint run"}}
	}
	var sb strings.Builder
	sb.WriteString("Tilgængelige hooks:\n\n")
	for name, cmd := range a.cfg.Hooks {
		sb.WriteString(fmt.Sprintf("  /hook %-16s → %s\n", name, cmd))
	}
	return []Event{{Type: EventSystem, Content: strings.TrimRight(sb.String(), "\n")}}
}

func (a *Agent) handleHook(name string) []Event {
	cmd, ok := a.cfg.Hooks[name]
	if !ok {
		// fallback: .ekte/hooks/<name> som script
		script := ".ekte/hooks/" + name
		if _, err := os.Stat(script); err != nil {
			return []Event{{Type: EventSystem, Content: fmt.Sprintf("Hook ikke fundet: %s\n\nKør '/hook' for at se tilgængelige hooks.", name)}}
		}
		cmd = script
	}

	var buf bytes.Buffer
	c := exec.Command("sh", "-c", cmd)
	c.Stdout = &buf
	c.Stderr = &buf

	runErr := c.Run()

	output := strings.TrimRight(buf.String(), "\n")
	if output == "" {
		output = "(ingen output)"
	}

	header := fmt.Sprintf("hook: %s\n$ %s\n\n", name, cmd)
	toolContent := header + output

	var status string
	if runErr != nil {
		status = fmt.Sprintf("✗ Hook fejlede: %s (%v)", name, runErr)
	} else {
		status = fmt.Sprintf("✓ Hook gennemført: %s", name)
	}

	return []Event{
		{Type: EventToolOutput, Content: toolContent},
		{Type: EventSystem, Content: status},
	}
}

func denyMsg(key string) string {
	return fmt.Sprintf(
		"⛔ Operation ikke tilladt: %s\n\n"+
			"Tilføj dette til .ekte/config.yaml for at tillade:\n\n"+
			"  whitelist:\n    %s: true",
		key, key,
	)
}

func helpText() string {
	cmds := [][2]string{
		{"/skills [navn]", "vis skills — angiv navn for at aktivere"},
		{"/spec <navn>", "opret spec + git worktree"},
		{"/compress", "komprimer kontekstvindue"},
		{"/wiki \"spørgsmål\"", "søg i din personlige wiki"},
		{"/hook [navn]", "vis hooks — angiv navn for at køre"},
		{"/dep <modul>", "sikkerhedsscore for én Go-afhængighed"},
		{"/sec-check", "scan alle afhængigheder + ekte-harness"},
		{"/forresten <besked>", "side-chat med subagent (husker historik)"},
		{"/clear", "ryd samtalen"},
		{"/exit", "gem session og afslut"},
		{"/resume [nummer]", "vis eller indlæs tidligere sessioner"},
		{"/hjælp", "vis denne hjælp"},
	}
	var sb strings.Builder
	sb.WriteString("Slash commands:\n")
	for _, c := range cmds {
		sb.WriteString(fmt.Sprintf("  %-30s — %s\n", c[0], c[1]))
	}
	return sb.String()
}
