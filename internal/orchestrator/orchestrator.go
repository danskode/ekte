// Package orchestrator implementerer Fase 1 af ekte's multi-agent-flow: en
// orchestrator nedbryder en spec i delopgaver, lader specialiserede subagenter
// løse dem, scorer og itererer, og samler de godkendte delsvar til én løsning.
//
// Ansvarsfordeling (jf. AIDD): hovedagenten ejer spec + endelig implementering,
// orchestratoren organiserer arbejdet og scorer, subagenterne udfører. Subagent-
// output er FORSLAG — ikke betroede direktiver; hovedagenten validerer mod
// kodebasen før implementering.
//
// Fase 1 kører subagenter sekventielt med hårde lofter (max delopgaver,
// max iterationer) for at holde token-/omkostningsforbruget bevidst — vigtigt for
// lokale modeller. Parallelisme er Fase 2.
package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/danskode/ekte/internal/provider"
)

const (
	maxSubTasks      = 6
	maxAssembleChars = 4000 // loft pr. delsvar i assemble — token-budget mod store/ondsindede outputs
)

type SubTask struct {
	Title     string `json:"title"`
	Brief     string `json:"brief"`
	Specialty string `json:"specialty"`
}

type SubResult struct {
	Task       SubTask
	Output     string
	Score      int
	Critique   string
	Iterations int
	Accepted   bool
}

type Solution struct {
	Spec       string
	SubResults []SubResult
	Assembled  string
}

type Options struct {
	MaxIterations int // pr. subagent (default 2)
	AcceptScore   int // 0-100, accept-tærskel (default 80)
}

func (o Options) withDefaults() Options {
	if o.MaxIterations <= 0 {
		o.MaxIterations = 2
	}
	if o.AcceptScore <= 0 {
		o.AcceptScore = 80
	}
	return o
}

type Orchestrator struct {
	p        provider.Provider
	eval     provider.Provider // evaluator-model — adskilt rolle mod self-evaluation bias
	opts     Options
	progress func(string)
}

func New(p provider.Provider, opts Options, progress func(string)) *Orchestrator {
	return &Orchestrator{p: p, eval: p, opts: opts.withDefaults(), progress: progress}
}

// SetEvaluator sætter en separat provider til scoring. Bruges til ægte
// provider-per-rolle (fx en stærkere/anden model som skeptisk evaluator), så
// bedømmelsen ikke laves af samme model der skrev løsningen. Nil ignoreres.
func (o *Orchestrator) SetEvaluator(p provider.Provider) {
	if p != nil {
		o.eval = p
	}
}

func (o *Orchestrator) emit(msg string) {
	if o.progress != nil {
		o.progress(msg)
	}
}

var fenceRe = regexp.MustCompile("(?s)^\\s*```(?:json)?\\s*\\n?|\\n?```\\s*$")

func stripFence(s string) string {
	return strings.TrimSpace(fenceRe.ReplaceAllString(strings.TrimSpace(s), ""))
}

func (o *Orchestrator) chat(ctx context.Context, system, user string) (string, error) {
	return chatWith(ctx, o.p, system, user)
}

func chatWith(ctx context.Context, p provider.Provider, system, user string) (string, error) {
	resp, err := p.Chat(ctx, []provider.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// Decompose nedbryder spec'en i afgrænsede delopgaver.
func (o *Orchestrator) Decompose(ctx context.Context, spec string) ([]SubTask, error) {
	const sys = `Du er en orchestrator. Nedbryd brugerens udviklingsopgave i 2-5 afgrænsede, uafhængige delopgaver, hver med en specialitet (fx "backend", "tests", "sikkerhed", "ui"). Hold dem små og konkrete. Returnér KUN valid JSON uden markdown:
[{"title": "kort titel", "brief": "hvad delopgaven skal løse", "specialty": "specialitet"}]`
	raw, err := o.chat(ctx, sys, spec)
	if err != nil {
		return nil, err
	}
	var tasks []SubTask
	if err := json.Unmarshal([]byte(stripFence(raw)), &tasks); err != nil {
		return nil, fmt.Errorf("kunne ikke parse delopgaver: %w", err)
	}
	if len(tasks) > maxSubTasks {
		tasks = tasks[:maxSubTasks]
	}
	return tasks, nil
}

func (o *Orchestrator) runSubAgent(ctx context.Context, t SubTask, critique string) (string, error) {
	sys := fmt.Sprintf(`Du er en specialist-subagent inden for %q. Løs KUN din afgrænsede delopgave grundigt og konkret (kode og/eller præcis plan). Antag ikke ansvar for resten af systemet. Returnér din løsning som almindelig tekst.`, t.Specialty)
	user := fmt.Sprintf("Delopgave: %s\n\n%s", t.Title, t.Brief)
	if strings.TrimSpace(critique) != "" {
		user += "\n\nTidligere kritik at rette op på:\n" + critique
	}
	return o.chat(ctx, sys, user)
}

type scoreResult struct {
	Score    int    `json:"score"`
	Critique string `json:"critique"`
}

func (o *Orchestrator) score(ctx context.Context, spec string, t SubTask, output string) (int, string, error) {
	// Skeptisk, uafhængig evaluator-rolle (kørt på o.eval — kan være en anden model)
	// mod self-evaluation bias: modeller roser ellers konsekvent eget output.
	const sys = `Du er en UAFHÆNGIG, SKEPTISK kvalitetssikrer — IKKE forfatteren af løsningen, og du må IKKE rose den. Antag at løsningen har mangler indtil det modsatte er bevist. Vurdér STRENGT 0-100 på korrekthed, fuldstændighed og kvalitet ift. delopgaven og den overordnede spec. Træk fra for alt der er udokumenteret, uafprøvet eller blot påstået uden at være vist. En løsning der "ser rigtig ud" men ikke beviseligt løser opgaven scorer under 60. Returnér KUN valid JSON uden markdown:
{"score": N, "critique": "konkret, kritisk vurdering — hvad mangler og hvad er svagt"}`
	user := fmt.Sprintf("Overordnet spec:\n%s\n\nDelopgave: %s\n%s\n\nSubagentens løsning (bedøm den skeptisk):\n%s", spec, t.Title, t.Brief, output)
	raw, err := chatWith(ctx, o.eval, sys, user)
	if err != nil {
		return 0, "", err
	}
	var sr scoreResult
	if err := json.Unmarshal([]byte(stripFence(raw)), &sr); err != nil {
		return 0, "", fmt.Errorf("kunne ikke parse score: %w", err)
	}
	if sr.Score < 0 {
		sr.Score = 0
	}
	if sr.Score > 100 {
		sr.Score = 100
	}
	return sr.Score, sr.Critique, nil
}

func (o *Orchestrator) assemble(ctx context.Context, spec string, results []SubResult) (string, error) {
	const sys = `Du er en orchestrator. Saml de godkendte delsvar til én sammenhængende løsning klar til hovedagenten. Påpeg eksplicit eventuelle delopgaver der IKKE blev accepteret, og hvad der mangler. Returnér almindelig tekst (ikke JSON).`
	var b strings.Builder
	b.WriteString("Spec:\n" + spec + "\n\n")
	for i, r := range results {
		status := "ACCEPTERET"
		if !r.Accepted {
			status = fmt.Sprintf("IKKE accepteret (bedste score %d)", r.Score)
		}
		out := r.Output
		if rs := []rune(out); len(rs) > maxAssembleChars {
			out = string(rs[:maxAssembleChars]) + "\n…[afkortet]"
		}
		b.WriteString(fmt.Sprintf("--- Delopgave %d: %s [%s] ---\n%s\n\n", i+1, r.Task.Title, status, out))
	}
	return o.chat(ctx, sys, b.String())
}

// Run kører hele Fase 1-flowet: nedbryd → (subagent → scor → iterér) → saml.
func (o *Orchestrator) Run(ctx context.Context, spec string) (*Solution, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, fmt.Errorf("tom spec")
	}
	o.emit("Nedbryder spec i delopgaver...")
	tasks, err := o.Decompose(ctx, spec)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("ingen delopgaver udledt af spec'en")
	}

	var results []SubResult
	for i, t := range tasks {
		o.emit(fmt.Sprintf("Subagent %d/%d — %s (%s)", i+1, len(tasks), t.Title, t.Specialty))
		var output, critique string
		var score, rounds int
		accepted := false
		for rounds < o.opts.MaxIterations {
			rounds++
			out, err := o.runSubAgent(ctx, t, critique)
			if err != nil {
				return nil, err
			}
			output = out
			sc, crit, err := o.score(ctx, spec, t, out)
			if err != nil {
				return nil, err
			}
			score, critique = sc, crit
			o.emit(fmt.Sprintf("  runde %d: score %d/100", rounds, sc))
			if sc >= o.opts.AcceptScore {
				accepted = true
				break
			}
		}
		results = append(results, SubResult{
			Task: t, Output: output, Score: score,
			Critique: critique, Iterations: rounds, Accepted: accepted,
		})
	}

	o.emit("Samler delsvar til én løsning...")
	assembled, err := o.assemble(ctx, spec, results)
	if err != nil {
		return nil, err
	}
	return &Solution{Spec: spec, SubResults: results, Assembled: assembled}, nil
}
