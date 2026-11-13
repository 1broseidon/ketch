package main

// The flow tests (design/docs-evidence.md, Flows): a model given only the
// docs tool of ketch's own MCP server answers a scenario's question in a
// sandbox with built indexes, and the run is recorded and graded: whether
// the evidence reached the caller, whether the answer is right (a judge
// model), whether its citations are ones the tool printed, and what it
// took (calls, tokens read, calls to one source).
//
//	go run ./internal/docsqa -flows internal/docsqa/flows/scenarios.json \
//	    -ketch /tmp/ketch -build round9 -model sonnet -sandboxes /tmp/flow-sb -out /tmp/flow-runs
//
// The caller is the Claude Code CLI in headless mode (claude -p) with no
// built-in tool, no settings, no session persistence and one MCP server:
// the ketch binary under sandbox-exec with outbound network denied, its
// HOME, XDG directories, config, docs index and tag index in the
// scenario's sandbox. Every run checks that the CLI offered the caller the
// docs tool and nothing else. A tool result read from a web search stops
// the batch. -flows-verify checks the scenarios' quotes against the
// sandboxes' indexes and writes each scenario's unverifiable reason into
// the file; -regrade grades saved transcripts again (-rejudge asks the
// judge again too).

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/docsindex"
)

var (
	flowsPath      = flag.String("flows", "", "run the flow tests of this scenarios file (internal/docsqa/flows/scenarios.json) with -ketch, -build and -model")
	flowsVerify    = flag.Bool("flows-verify", false, "with -flows: check every scenario's quotes against its sandbox's index, write the unverifiable reasons into the file and exit")
	flowSandboxes  = flag.String("sandboxes", "/tmp/flow-sb", "with -flows: the directory holding one sandbox per scenario sandbox name")
	flowBuild      = flag.String("build", "", "with -flows: the build's label, the first directory level of the transcripts")
	flowModel      = flag.String("model", "sonnet", "with -flows: the caller model (a claude --model value)")
	flowJudgeModel = flag.String("judge", "opus", "with -flows: the judge model (a claude --model value)")
	flowOut        = flag.String("out", "/tmp/flow-runs", "with -flows: transcripts go to <out>/<build>/<model>/<scenario>.json")
	flowParallel   = flag.Int("parallel", 4, "with -flows: runs at once")
	flowRunN       = flag.Int("run", 1, "with -flows: the run number (a repeat writes <scenario>.r<run>.json)")
	flowClaude     = flag.String("claude", "claude", "with -flows: the Claude Code CLI")
	flowMaxCalls   = flag.Int("max-calls", 10, "with -flows: the caller's tool calls allowed (the CLI's turn cap is this plus one)")
	flowRegrade    = flag.Bool("regrade", false, "with -flows: grade the saved transcripts of -build and -model again instead of running")
	flowRejudge    = flag.Bool("rejudge", false, "with -regrade: ask the judge again")
	flowHaikuOnly  = flag.Bool("haiku-sample", false, "with -flows: run only the scenarios marked for the Haiku sample")
	flowNoNetDeny  = "(version 1)(allow default)(deny network-outbound (remote ip))"
	flowDocsTool   = "mcp__ketch__docs"
	flowOtherTools = []string{"mcp__ketch__search", "mcp__ketch__code", "mcp__ketch__scrape", "mcp__ketch__crawl", "mcp__ketch__tag"}
)

// flowScenario is one question with what a right answer states and what
// it must say the docs do not cover. The caller sees the question only.
type flowScenario struct {
	N        int        `json:"n"`
	ID       string     `json:"id"`
	Flow     string     `json:"flow"`
	Sandbox  string     `json:"sandbox"`
	Cases    []string   `json:"cases"`
	Question string     `json:"question"`
	Facts    []flowFact `json:"facts"`
	// NotCovered are parts of the question the sources do not cover: a
	// right answer says so for each.
	NotCovered []string `json:"not_covered"`
	// Haiku marks the scenarios of the Haiku sample.
	Haiku bool `json:"haiku,omitempty"`
	// Unverifiable is why a fact's gold quote is in no index of the
	// sandbox (-flows-verify); such a scenario runs but is not counted.
	Unverifiable string `json:"unverifiable,omitempty"`
}

// flowFact is a required fact: a plain statement for the judge, and the
// case's gold quotes (fact: a quote, or alternatives) for the evidence
// check, from the case's library.
type flowFact struct {
	Case      string `json:"case"`
	Library   string `json:"library"`
	Statement string `json:"statement"`
	fact
}

func readScenarios(path string) ([]flowScenario, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []flowScenario
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, s := range out {
		if s.ID == "" || s.Question == "" || s.Sandbox == "" || s.Flow == "" {
			return nil, fmt.Errorf("%s: scenario %q needs id, flow, sandbox and question", path, s.ID)
		}
	}
	return out, nil
}

// flowRun is one run's record: the transcript file.
type flowRun struct {
	Scenario     string            `json:"scenario"`
	N            int               `json:"n"`
	Flow         string            `json:"flow"`
	Run          int               `json:"run"`
	Build        string            `json:"build"`
	Ketch        string            `json:"ketch"`
	Sandbox      string            `json:"sandbox"`
	Model        string            `json:"model"`
	ModelUsed    []string          `json:"model_resolved"`
	Started      time.Time         `json:"started"`
	WallMS       int64             `json:"wall_ms"`
	SystemPrompt string            `json:"system_prompt"`
	Question     string            `json:"question"`
	Args         []string          `json:"cli_args"`
	MCPConfig    json.RawMessage   `json:"mcp_config"`
	InitTools    []string          `json:"init_tools"`
	InitMCP      json.RawMessage   `json:"init_mcp_servers,omitempty"`
	Calls        []flowCall        `json:"calls"`
	Answer       string            `json:"answer"`
	Texts        []string          `json:"assistant_texts,omitempty"`
	Subtype      string            `json:"result_subtype"`
	Terminal     string            `json:"terminal_reason,omitempty"`
	NumTurns     int               `json:"num_turns"`
	CostUSD      float64           `json:"cost_usd"`
	Usage        json.RawMessage   `json:"usage,omitempty"`
	ModelUsage   json.RawMessage   `json:"model_usage,omitempty"`
	Exit         int               `json:"exit"`
	Stderr       string            `json:"stderr,omitempty"`
	Stream       []json.RawMessage `json:"stream"`
	Grade        flowGrade         `json:"grade"`
}

// flowCall is one tool call: its arguments, the result text as the caller
// received it, and what the grading reads from it.
type flowCall struct {
	N       int             `json:"n"`
	Tool    string          `json:"tool"`
	Input   json.RawMessage `json:"input"`
	Result  string          `json:"result"`
	IsError bool            `json:"is_error,omitempty"`
	// Tokens is docs.EstimateTokens of Result, as delivered (the JSON).
	Tokens int `json:"tokens"`
	Bytes  int `json:"bytes"`
	// Source is the library the call asked (its root), "" for none.
	Source string `json:"source"`
	Status string `json:"status,omitempty"`
	Reason string `json:"reason,omitempty"`
	// WebSearch: the result was read from a web search (index.source
	// search, pages_read, or reason partial_site).
	WebSearch bool `json:"web_search,omitempty"`
	// SearchBlocked: the call went to a web search, which the denied
	// network failed.
	SearchBlocked bool `json:"search_blocked,omitempty"`
	// Signals this call followed from earlier results (see signals).
	Signals []string `json:"signals,omitempty"`
}

// flowGrade is a run's grades.
type flowGrade struct {
	Invalid  string         `json:"invalid,omitempty"`
	Evidence []factEvidence `json:"evidence"`
	// EvidenceAll: every required fact's gold quote reached the caller.
	EvidenceAll bool       `json:"evidence_reached"`
	Judge       *flowJudge `json:"judge,omitempty"`
	JudgeRaw    string     `json:"judge_raw,omitempty"`
	JudgeError  string     `json:"judge_error,omitempty"`
	Right       bool       `json:"right"`
	Citations   []citation `json:"citations"`
	CiteFails   int        `json:"citation_failures"`
	Calls       int        `json:"calls"`
	TokensRead  int        `json:"tokens_read"`
	CapHit      bool       `json:"cap_hit"`
	// PerSource is calls per source; MaxPerSource the most to one,
	// OverThree when that is over three.
	PerSource    map[string]int `json:"calls_per_source"`
	MaxPerSource int            `json:"max_calls_one_source"`
	OverThree    bool           `json:"over_three"`
	Signals      map[string]int `json:"signals,omitempty"`
	WebSearch    bool           `json:"web_search,omitempty"`
	Errors       int            `json:"tool_errors,omitempty"`
	// SearchBlocked counts the calls that went to a web search and failed
	// for want of network.
	SearchBlocked int `json:"search_blocked,omitempty"`
}

// factEvidence is whether a fact's gold quote reached the caller, and at
// which call (from 1; 0 never).
type factEvidence struct {
	Statement string `json:"statement"`
	Reached   bool   `json:"reached"`
	Call      int    `json:"call"`
}

type citation struct {
	URL string `json:"url"`
	OK  bool   `json:"ok"`
	// InText: not a result's url or section, but somewhere in a result.
	InText bool `json:"in_text,omitempty"`
}

// flowJudge is the judge's verdict.
type flowJudge struct {
	Facts []struct {
		ID      string `json:"id"`
		Verdict string `json:"verdict"`
		Note    string `json:"note"`
	} `json:"facts"`
	NotCovered []struct {
		ID      string `json:"id"`
		Verdict string `json:"verdict"`
		Note    string `json:"note"`
	} `json:"not_covered"`
	Invented []struct {
		Claim  string `json:"claim"`
		Reason string `json:"reason"`
	} `json:"invented"`
}

// flowHarness is what every run of a batch shares.
type flowHarness struct {
	ketch, build, model, judge, claude, out, sandboxes string
	maxCalls, runN                                     int
	libs                                               map[string][]docsindex.Library
	abort                                              atomic.Bool
	mu                                                 sync.Mutex
}

func runFlows(ketch, only string) error {
	scenarios, err := readScenarios(*flowsPath)
	if err != nil {
		return err
	}
	scenarios = filterScenarios(scenarios, only, *flowHaikuOnly)
	h := &flowHarness{build: *flowBuild, model: *flowModel, judge: *flowJudgeModel, claude: *flowClaude, out: *flowOut, sandboxes: *flowSandboxes, maxCalls: *flowMaxCalls, runN: *flowRunN, libs: map[string][]docsindex.Library{}}
	if *flowsVerify {
		return h.verify(os.Stdout, *flowsPath)
	}
	if h.build == "" {
		return errors.New("-flows needs -build")
	}
	for _, s := range scenarios {
		if _, err := h.libraries(s.Sandbox); err != nil {
			return err
		}
	}
	if *flowRegrade {
		return h.regrade(scenarios, *flowRejudge)
	}
	if ketch == "" {
		return errors.New("-flows needs -ketch")
	}
	if h.ketch, err = filepath.Abs(ketch); err != nil {
		return err
	}
	if err := os.MkdirAll(h.runDir(), 0o755); err != nil {
		return err
	}
	return h.runBatch(scenarios, *flowParallel)
}

// runBatch runs the scenarios, parallel at once, and writes the results
// file; a tool result read from a web search stops it.
func (h *flowHarness) runBatch(scenarios []flowScenario, parallel int) error {
	jobs := make(chan flowScenario)
	var wg sync.WaitGroup
	var summaries []flowSummary
	for range max(parallel, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				if h.abort.Load() {
					continue
				}
				r, err := h.runOne(s)
				if err != nil {
					fmt.Fprintf(os.Stderr, "flows: %s: %v\n", s.ID, err)
					continue
				}
				sum := summarize(r, s)
				h.mu.Lock()
				summaries = append(summaries, sum)
				fmt.Printf("%-3d %-48s right=%-5v evidence=%-5v calls=%-2d tokens=%-6d max/source=%d cites-bad=%d invented=%d %.0fs $%.3f%s\n",
					s.N, clip(s.ID, 48), sum.Right, sum.EvidenceAll, sum.Calls, sum.TokensRead, sum.MaxPerSource, sum.CiteFails, sum.Invented, float64(sum.WallMS)/1000, sum.CostUSD, flagNote(sum))
				h.mu.Unlock()
				if r.Grade.WebSearch {
					fmt.Fprintf(os.Stderr, "flows: STOP: %s got a tool result read from a web search\n", s.ID)
					h.abort.Store(true)
				}
			}
		}()
	}
	for _, s := range scenarios {
		jobs <- s
	}
	close(jobs)
	wg.Wait()
	if err := h.writeResults(summaries); err != nil {
		return err
	}
	if h.abort.Load() {
		return errors.New("stopped: a tool result came from a web search")
	}
	return nil
}

func flagNote(s flowSummary) string {
	var parts []string
	if s.Invalid != "" {
		parts = append(parts, "INVALID "+s.Invalid)
	}
	if s.CapHit {
		parts = append(parts, "cap")
	}
	if s.Unverifiable != "" {
		parts = append(parts, "unverifiable")
	}
	if len(parts) == 0 {
		return ""
	}
	return "  [" + strings.Join(parts, ", ") + "]"
}

func filterScenarios(in []flowScenario, only string, haiku bool) []flowScenario {
	want := map[string]bool{}
	for _, id := range strings.Split(only, ",") {
		if id = strings.TrimSpace(id); id != "" {
			want[id] = true
		}
	}
	var out []flowScenario
	for _, s := range in {
		if (len(want) == 0 || want[s.ID] || want[strconv.Itoa(s.N)]) && (!haiku || s.Haiku) {
			out = append(out, s)
		}
	}
	return out
}

func (h *flowHarness) runDir() string { return filepath.Join(h.out, h.build, h.model) }

func (h *flowHarness) transcriptPath(id string, run int) string {
	name := id + ".json"
	if run > 1 {
		name = fmt.Sprintf("%s.r%d.json", id, run)
	}
	return filepath.Join(h.runDir(), name)
}

func (h *flowHarness) sandboxDir(name string) string { return filepath.Join(h.sandboxes, name) }

// libraries is the sandbox's indexed libraries, as the QA runner takes
// them: current, and built by ketch docs index.
func (h *flowHarness) libraries(sandbox string) ([]docsindex.Library, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if l, ok := h.libs[sandbox]; ok {
		return l, nil
	}
	path := filepath.Join(h.sandboxDir(sandbox), "docs-index.sqlite")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("sandbox %s: %w", sandbox, err)
	}
	all, err := docsindex.Open(path).Libraries()
	if err != nil {
		return nil, fmt.Errorf("sandbox %s: %w", sandbox, err)
	}
	var libs []docsindex.Library
	for _, l := range all {
		if l.Current() && l.Resolved != "search" {
			libs = append(libs, l)
		}
	}
	if len(libs) == 0 {
		return nil, fmt.Errorf("sandbox %s holds no library", sandbox)
	}
	h.libs[sandbox] = libs
	return libs, nil
}

// systemPrompt is the caller's system prompt, with the sandbox's library
// roots one per line.
func systemPrompt(libs []docsindex.Library) string {
	roots := make([]string, 0, len(libs))
	for _, l := range libs {
		roots = append(roots, l.Root)
	}
	return "You answer questions about software and product documentation. Use only the docs tool; do not answer from memory. State each fact plainly and cite the url of the documentation section it comes from. If the documentation you can reach does not cover part of the question, say which part. Documentation sites indexed on this machine:\n" + strings.Join(roots, "\n")
}

// mcpConfig launches the ketch binary as the CLI's one MCP server: with an
// environment of the sandbox's state only (env -i), and outbound network
// denied (sandbox-exec).
func mcpConfig(ketch, sb string) json.RawMessage {
	home := filepath.Join(sb, "home")
	args := []string{"-i", "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "TMPDIR=" + os.TempDir(),
		"HOME=" + home,
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"KETCH_CONFIG=" + filepath.Join(sb, "config.json"),
		"KETCH_DOCS_INDEX_PATH=" + filepath.Join(sb, "docs-index.sqlite"),
		"KETCH_TAGS_PATH=" + filepath.Join(sb, "tags.db"),
		"KETCH_NO_UPDATE_NOTIFIER=1",
		"/usr/bin/sandbox-exec", "-p", flowNoNetDeny, ketch, "mcp", "serve"}
	b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"ketch": map[string]any{"type": "stdio", "command": "/usr/bin/env", "args": args}}})
	return b
}

// cliEnv is the environment for a nested CLI: the parent's, without the
// variables of a Claude Code session it may run in and without KETCH_*.
func cliEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "CLAUDE") || strings.HasPrefix(k, "KETCH_") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// isolatedArgs are the CLI flags every nested run takes: no built-in
// tool, no settings, no session saved, no slash commands.
func isolatedArgs() []string {
	return []string{"--tools", "", "--strict-mcp-config", "--setting-sources", "", "--no-session-persistence", "--disable-slash-commands"}
}

// callerArgs is the caller's command line.
func (h *flowHarness) callerArgs(s flowScenario, sp string, mcp json.RawMessage) []string {
	args := []string{"-p", s.Question, "--model", h.model, "--system-prompt", sp,
		"--mcp-config", string(mcp), "--allowedTools", flowDocsTool, "--disallowedTools", strings.Join(flowOtherTools, ","),
		"--permission-mode", "dontAsk", "--max-turns", strconv.Itoa(h.maxCalls + 1),
		"--output-format", "stream-json", "--verbose"}
	return append(args, isolatedArgs()...)
}

func (h *flowHarness) runOne(s flowScenario) (*flowRun, error) {
	libs, err := h.libraries(s.Sandbox)
	if err != nil {
		return nil, err
	}
	sb := h.sandboxDir(s.Sandbox)
	cwd, err := os.MkdirTemp("", "flow-cwd-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(cwd) }()
	sp := systemPrompt(libs)
	mcp := mcpConfig(h.ketch, sb)
	args := h.callerArgs(s, sp, mcp)
	r := &flowRun{Scenario: s.ID, N: s.N, Flow: s.Flow, Run: h.runN, Build: h.build, Ketch: h.ketch, Sandbox: sb, Model: h.model,
		SystemPrompt: sp, Question: s.Question, Args: args, MCPConfig: mcp, Started: time.Now().UTC()}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.claude, args...)
	cmd.Dir, cmd.Env = cwd, cliEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err = cmd.Run()
	r.WallMS = time.Since(start).Milliseconds()
	r.Stderr = stderr.String()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		r.Exit = exit.ExitCode()
	} else if err != nil {
		return nil, err
	}
	parseStream(r, stdout.Bytes())
	h.grade(r, s, libs, true)
	return r, h.save(r)
}

func (h *flowHarness) save(r *flowRun) error {
	b, err := marshalIndent(r)
	if err != nil {
		return err
	}
	return os.WriteFile(h.transcriptPath(r.Scenario, r.Run), b, 0o644)
}

func marshalIndent(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(v)
	return buf.Bytes(), err
}

// streamMsg is one line of the CLI's stream-json output, the fields read.
type streamMsg struct {
	Type       string          `json:"type"`
	Subtype    string          `json:"subtype"`
	Tools      []string        `json:"tools"`
	MCPServers json.RawMessage `json:"mcp_servers"`
	Model      string          `json:"model"`
	Message    *struct {
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Result         string          `json:"result"`
	NumTurns       int             `json:"num_turns"`
	TotalCost      float64         `json:"total_cost_usd"`
	Usage          json.RawMessage `json:"usage"`
	ModelUsage     json.RawMessage `json:"modelUsage"`
	TerminalReason string          `json:"terminal_reason"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Text      string          `json:"text"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// parseStream reads the CLI's stream-json lines into r: the tools offered,
// each tool call with its result, the answer, usage and cost.
func parseStream(r *flowRun, out []byte) {
	byID := map[string]int{}
	models := map[string]bool{}
	for line := range bytes.SplitSeq(out, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		r.Stream = append(r.Stream, json.RawMessage(slices.Clone(line)))
		var m streamMsg
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		switch m.Type {
		case "system":
			if m.Subtype == "init" {
				r.InitTools, r.InitMCP = m.Tools, m.MCPServers
				if m.Model != "" {
					models[m.Model] = true
				}
			}
		case "assistant", "user":
			if m.Message == nil {
				continue
			}
			if m.Message.Model != "" {
				models[m.Message.Model] = true
			}
			readBlocks(r, m.Type, m.Message.Content, byID)
		case "result":
			r.Subtype, r.Answer, r.NumTurns, r.CostUSD = m.Subtype, m.Result, m.NumTurns, m.TotalCost
			r.Usage, r.ModelUsage, r.Terminal = m.Usage, m.ModelUsage, m.TerminalReason
			var mu map[string]json.RawMessage
			if json.Unmarshal(m.ModelUsage, &mu) == nil {
				for k := range mu {
					models[k] = true
				}
			}
		}
	}
	for k := range models {
		r.ModelUsed = append(r.ModelUsed, k)
	}
	slices.Sort(r.ModelUsed)
}

// readBlocks reads a message's content blocks into r: tool calls, their
// results (byID maps a tool_use id to its call), the assistant's text.
func readBlocks(r *flowRun, role string, content json.RawMessage, byID map[string]int) {
	var blocks []contentBlock
	if json.Unmarshal(content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		switch {
		case b.Type == "tool_use":
			byID[b.ID] = len(r.Calls)
			r.Calls = append(r.Calls, flowCall{N: len(r.Calls) + 1, Tool: b.Name, Input: b.Input})
		case b.Type == "tool_result":
			if i, ok := byID[b.ToolUseID]; ok {
				r.Calls[i].Result, r.Calls[i].IsError = resultText(b.Content), b.IsError
			}
		case b.Type == "text" && role == "assistant":
			r.Texts = append(r.Texts, b.Text)
		}
	}
}

// resultText is a tool_result's content as text: a string, or its text
// blocks joined.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

// docsResult is the docs tool's output (mcp.DocsOutput), the fields read.
type docsResult struct {
	Library   string           `json:"library"`
	Status    string           `json:"status"`
	Reason    string           `json:"reason"`
	NotFound  []string         `json:"not_found"`
	DefinedIn []docs.DefinedIn `json:"defined_in"`
	Index     *docs.IndexInfo  `json:"index"`
	Hint      string           `json:"hint"`
	Results   []docs.Result    `json:"results"`
}

// callInput is the docs tool's arguments, the fields read.
type callInput struct {
	Query   string `json:"query"`
	Library string `json:"library"`
	Section string `json:"section"`
	Page    int    `json:"page"`
	Tokens  int    `json:"tokens"`
	Resolve bool   `json:"resolve"`
	Offline bool   `json:"offline"`
}

// grade grades r against s. judge asks the judge model; without it, the
// judge's saved verdict is kept.
func (h *flowHarness) grade(r *flowRun, s flowScenario, libs []docsindex.Library, judge bool) {
	g := &r.Grade
	keep := *g
	*g = flowGrade{PerSource: map[string]int{}, Signals: map[string]int{}}
	switch {
	case len(r.InitTools) != 1 || r.InitTools[0] != flowDocsTool:
		g.Invalid = fmt.Sprintf("tools offered: %v", r.InitTools)
	case r.Subtype == "":
		g.Invalid = fmt.Sprintf("no result line (exit %d)", r.Exit)
	}
	texts := make([]string, len(r.Calls))
	var urls []string
	for i := range r.Calls {
		var callURLs []string
		texts[i], callURLs = gradeCall(g, r.Calls, i, s.Question, libs)
		urls = append(urls, callURLs...)
	}
	g.Calls = len(r.Calls)
	g.CapHit = g.Calls >= h.maxCalls || r.Subtype == "error_max_turns"
	for _, n := range g.PerSource {
		g.MaxPerSource = max(g.MaxPerSource, n)
	}
	g.OverThree = g.MaxPerSource > 3
	g.Evidence, g.EvidenceAll = evidence(s.Facts, texts)
	g.Citations = citations(r.Answer, urls, texts)
	for _, c := range g.Citations {
		if !c.OK {
			g.CiteFails++
		}
	}
	if judge {
		g.Judge, g.JudgeRaw, g.JudgeError = h.askJudge(r, s)
	} else {
		g.Judge, g.JudgeRaw, g.JudgeError = keep.Judge, keep.JudgeRaw, keep.JudgeError
	}
	g.Right = g.Invalid == "" && r.Answer != "" && judgedRight(g.Judge, len(s.Facts), len(s.NotCovered))
}

// gradeCall grades calls[i] into it and g: tokens, status, source,
// signals; it returns the text the caller read in its result and the
// urls and sections it printed.
func gradeCall(g *flowGrade, calls []flowCall, i int, question string, libs []docsindex.Library) (text string, urls []string) {
	c := &calls[i]
	if c.Tool != flowDocsTool && g.Invalid == "" {
		g.Invalid = "called " + c.Tool
	}
	c.Tokens, c.Bytes = docs.EstimateTokens(c.Result), len(c.Result)
	g.TokensRead += c.Tokens
	g.Errors += b2i(c.IsError)
	var res docsResult
	var in callInput
	_ = json.Unmarshal(c.Input, &in)
	text = c.Result
	if json.Unmarshal([]byte(c.Result), &res) == nil {
		text = strings.Join(jsonStrings(c.Result), "\n")
		c.Status, c.Reason = res.Status, res.Reason
		c.WebSearch = webSearched(in, res)
		urls = resultURLs(res)
	}
	c.Source = callSource(in, res, libs)
	if c.Source != "" {
		g.PerSource[c.Source]++
	}
	c.SearchBlocked = c.IsError && strings.Contains(c.Result, "web search failed")
	g.SearchBlocked += b2i(c.SearchBlocked)
	g.WebSearch = g.WebSearch || c.WebSearch
	c.Signals = signals(calls[:i], in, c.Source, question)
	for _, sig := range c.Signals {
		g.Signals[sig]++
	}
	return text, urls
}

// webSearched reports a result read from a web search: an answer whose
// index is source search (pages_read) or whose reason is partial_site. A
// section read is not one: since round 10 it says what it was read from
// as an answer does, and a page no index holds is read through the page
// cache (source search, 1 page read), never by searching.
func webSearched(in callInput, res docsResult) bool {
	if in.Section != "" {
		return false
	}
	return res.Index != nil && (res.Index.Source == "search" || res.Index.PagesRead > 0) || res.Reason == "partial_site"
}

// jsonStrings is every string value in the JSON text s, in document
// order: the text the caller read, unescaped.
func jsonStrings(s string) []string {
	dec := json.NewDecoder(strings.NewReader(s))
	var out []string
	for {
		t, err := dec.Token()
		if err != nil {
			return out
		}
		if v, ok := t.(string); ok {
			out = append(out, v)
		}
	}
}

// evidence reports, per fact, whether every quote of one of its
// alternatives is in a tool result (normalised as the QA runner does),
// and the first call by which one alternative was complete.
func evidence(facts []flowFact, texts []string) ([]factEvidence, bool) {
	norm := make([]string, len(texts))
	for i, t := range texts {
		norm[i] = normalize(t)
	}
	all := true
	out := []factEvidence{}
	for _, f := range facts {
		best := 0
		for _, alt := range f.alternatives() {
			at := 0
			for _, a := range alt {
				q, first := normalize(a.Quote), 0
				for i, t := range norm {
					if strings.Contains(t, q) {
						first = i + 1
						break
					}
				}
				if first == 0 {
					at = 0
					break
				}
				at = max(at, first)
			}
			if at > 0 && (best == 0 || at < best) {
				best = at
			}
		}
		out = append(out, factEvidence{Statement: f.Statement, Reached: best > 0, Call: best})
		all = all && best > 0
	}
	return out, all
}

// resultURLs are the urls and section references a docs result printed.
func resultURLs(res docsResult) []string {
	var out []string
	for _, r := range res.Results {
		out = append(out, r.URL, r.Section)
	}
	for _, d := range res.DefinedIn {
		out = append(out, d.Section)
	}
	return slices.DeleteFunc(out, func(s string) bool { return s == "" })
}

var (
	answerURL  = regexp.MustCompile("https?://[^\\s<>()\\[\\]\"'`]+")
	fencedCode = regexp.MustCompile("(?s)```.*?```")
	localHost  = regexp.MustCompile(`^https?://(localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\]|[^/]*example\.(com|org|net))([:/]|$)`)
)

// citations checks each URL of the answer against the urls and sections
// the tool printed (scheme, a trailing slash and trailing punctuation
// aside); one found only in a result's text is not OK but marked InText.
// URLs in fenced code blocks and local or example hosts are not citations.
func citations(answer string, urls, texts []string) []citation {
	printed := map[string]bool{}
	for _, u := range urls {
		printed[urlKey(u)] = true
	}
	seen := map[string]bool{}
	out := []citation{}
	for _, u := range answerURL.FindAllString(fencedCode.ReplaceAllString(answer, ""), -1) {
		u = strings.TrimRight(u, ".,;:!?*_")
		if seen[u] || localHost.MatchString(u) {
			continue
		}
		seen[u] = true
		c := citation{URL: u, OK: printed[urlKey(u)]}
		if !c.OK {
			for _, t := range texts {
				if strings.Contains(t, u) {
					c.InText = true
					break
				}
			}
		}
		out = append(out, c)
	}
	return out
}

func urlKey(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	page, anchor, ok := strings.Cut(u, "#")
	page = strings.TrimSuffix(page, "/")
	if ok {
		return page + "#" + anchor
	}
	return page
}

// callSource is the library root a call asked: the result's library, else
// the library argument, else the root whose pages hold the section.
func callSource(in callInput, res docsResult, libs []docsindex.Library) string {
	for _, v := range []string{res.Library, in.Library} {
		if v == "" {
			continue
		}
		if m := docs.MatchIndexed(libs, v); len(m) > 0 {
			return m[0].Root
		}
		return v
	}
	if in.Section != "" {
		key, best := pageKey(strings.SplitN(in.Section, "#", 2)[0]), ""
		for _, l := range libs {
			root := pageKey(l.Root)
			if strings.HasPrefix(key, root) && len(root) > len(best) {
				best = l.Root
			}
		}
		return best
	}
	return ""
}

// sectionRef is a section reference as signals compare them.
func sectionRef(s string) string {
	page, anchor, _ := strings.Cut(strings.TrimSpace(s), "#")
	return pageKey(page) + "#" + anchor
}

// signals names what the call (to source) took from earlier results:
//
//	defined_in    read a section a defined_in entry named
//	cut_section   read a section an earlier result showed cut
//	section_other read a section no defined_in or cut result named
//	page          read a page past the first
//	contents      asked in a name from a cut result's contents that the question does not write
//	not_found     asked another source for a term an earlier result listed as not_found
//	resolve       resolved a library
func signals(before []flowCall, in callInput, source, question string) []string {
	l := priorLeads(before)
	var out []string
	switch {
	case in.Resolve:
		out = append(out, "resolve")
	case in.Section != "":
		ref := sectionRef(in.Section)
		switch {
		case l.defined[ref]:
			out = append(out, "defined_in")
		case l.cut[ref]:
			out = append(out, "cut_section")
		default:
			out = append(out, "section_other")
		}
		if in.Page > 1 {
			out = append(out, "page")
		}
	case in.Query != "":
		q, question := strings.ToLower(in.Query), strings.ToLower(question)
		if slices.ContainsFunc(l.contents, func(name string) bool {
			return len(name) >= 3 && strings.Contains(q, name) && !strings.Contains(question, name)
		}) {
			out = append(out, "contents")
		}
		for term, lacking := range l.notFound {
			if strings.Contains(q, term) && source != lacking {
				out = append(out, "not_found")
				break
			}
		}
	}
	return out
}

// leads are what earlier results printed for a caller to follow.
type leads struct {
	defined, cut map[string]bool   // section references
	contents     []string          // names, lower case
	notFound     map[string]string // term → the source that lacked it
}

func priorLeads(before []flowCall) leads {
	l := leads{defined: map[string]bool{}, cut: map[string]bool{}, notFound: map[string]string{}}
	for _, c := range before {
		var res docsResult
		if json.Unmarshal([]byte(c.Result), &res) != nil {
			continue
		}
		for _, d := range res.DefinedIn {
			l.defined[sectionRef(d.Section)] = true
		}
		for _, r := range res.Results {
			if r.Truncated {
				l.cut[sectionRef(r.Section)] = true
			}
			l.contents = append(l.contents, contentNames(r.Contents)...)
		}
		for _, t := range res.NotFound {
			l.notFound[strings.ToLower(t)] = c.Source
		}
	}
	return l
}

// contentNames splits a cut result's contents line into its names, lower
// case.
func contentNames(line string) []string {
	if line == "" {
		return nil
	}
	if _, rest, ok := strings.Cut(line, ":"); ok && !strings.Contains(line[:strings.Index(line, ":")], ",") {
		line = rest
	}
	var out []string
	for _, part := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == ';' || r == '·' }) {
		part = strings.TrimSpace(strings.Trim(strings.TrimSpace(part), "`.…"))
		if part != "" {
			out = append(out, strings.ToLower(part))
		}
	}
	return out
}

// judgedRight: every fact stated (none contradicted), every not-covered
// expectation said, nothing invented.
func judgedRight(j *flowJudge, facts, notCovered int) bool {
	if j == nil || len(j.Facts) != facts || len(j.NotCovered) != notCovered {
		return false
	}
	for _, f := range j.Facts {
		if f.Verdict != "stated" {
			return false
		}
	}
	for _, n := range j.NotCovered {
		if n.Verdict != "said" {
			return false
		}
	}
	return len(j.Invented) == 0
}

const judgeSystem = `You grade an answer to a documentation question. You get the question, the required facts (each a plain statement with the documentation's own words as gold quotes), statements of what the documentation does not cover, every tool result the answerer received, and the final answer. Reply with the JSON the schema asks for and nothing else.

facts: one entry per required fact, with its id. "stated" when the answer states the fact's substance: paraphrase, other wording, an equivalent example or a code snippet that shows it all count; details the statement gives in parentheses as an illustration need not be repeated. "contradicted" when the answer says something incompatible with the fact. Otherwise "missing".

not_covered: one entry per expectation, with its id. "said" when the answer says, in any words, that the documentation does not cover or does not mention that point (or that no such thing is documented). "not_said" otherwise, including when the answer asserts the thing exists or answers it anyway.

invented: every claim the answer presents as what the documentation says (stated as fact, or cited) that no tool result supports. Do not list claims the answer explicitly marks as its own inference, general knowledge or not covered by the documentation; do not list rewordings, summaries or direct consequences of what the tool results say, nor the answer's statements about which calls it made. An empty list when there are none.

Each note is one short sentence.`

const judgeSchema = `{"type":"object","additionalProperties":false,"required":["facts","not_covered","invented"],"properties":{
"facts":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["id","verdict","note"],"properties":{"id":{"type":"string"},"verdict":{"type":"string","enum":["stated","missing","contradicted"]},"note":{"type":"string"}}}},
"not_covered":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["id","verdict","note"],"properties":{"id":{"type":"string"},"verdict":{"type":"string","enum":["said","not_said"]},"note":{"type":"string"}}}},
"invented":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["claim","reason"],"properties":{"claim":{"type":"string"},"reason":{"type":"string"}}}}}}`

// judgePrompt is the judge's user message.
func judgePrompt(r *flowRun, s flowScenario) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Question\n\n%s\n\n# Required facts\n\n", s.Question)
	if len(s.Facts) == 0 {
		b.WriteString("None.\n")
	}
	for i, f := range s.Facts {
		fmt.Fprintf(&b, "- id f%d: %s\n", i+1, f.Statement)
		for _, alt := range f.alternatives() {
			var qs []string
			for _, a := range alt {
				qs = append(qs, strconv.Quote(a.Quote))
			}
			fmt.Fprintf(&b, "  gold quote: %s (%s)\n", strings.Join(qs, " … "), alt[0].Source)
		}
	}
	b.WriteString("\n# Not covered by the documentation\n\n")
	if len(s.NotCovered) == 0 {
		b.WriteString("None.\n")
	}
	for i, n := range s.NotCovered {
		fmt.Fprintf(&b, "- id n%d: %s\n", i+1, n)
	}
	b.WriteString("\n# Tool results the answerer received\n\n")
	if len(r.Calls) == 0 {
		b.WriteString("None: the answerer made no tool call.\n")
	}
	for _, c := range r.Calls {
		fmt.Fprintf(&b, "## Call %d: %s %s\n\n%s\n\n", c.N, c.Tool, string(c.Input), c.Result)
	}
	fmt.Fprintf(&b, "# Final answer\n\n%s\n", r.Answer)
	return b.String()
}

// askJudge grades r's answer with the judge model: its verdict, the raw
// output, and why it failed.
func (h *flowHarness) askJudge(r *flowRun, s flowScenario) (*flowJudge, string, string) {
	if r.Answer == "" {
		return nil, "", "no answer to judge"
	}
	cwd, err := os.MkdirTemp("", "flow-judge-")
	if err != nil {
		return nil, "", err.Error()
	}
	defer func() { _ = os.RemoveAll(cwd) }()
	args := append([]string{"-p", "--model", h.judge, "--system-prompt", judgeSystem, "--max-turns", "4",
		"--output-format", "json", "--json-schema", judgeSchema}, isolatedArgs()...)
	var lastErr string
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		cmd := exec.CommandContext(ctx, h.claude, args...)
		cmd.Dir, cmd.Env, cmd.Stdin = cwd, cliEnv(), strings.NewReader(judgePrompt(r, s))
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		cancel()
		raw := out.String()
		var res struct {
			Subtype    string          `json:"subtype"`
			Result     string          `json:"result"`
			Structured json.RawMessage `json:"structured_output"`
		}
		if jerr := json.Unmarshal(out.Bytes(), &res); jerr != nil {
			lastErr = fmt.Sprintf("judge output: %v; %v %s", jerr, err, clip(errb.String(), 300))
			continue
		}
		body := res.Structured
		if len(body) == 0 || string(body) == "null" {
			body = json.RawMessage(res.Result)
		}
		var j flowJudge
		if jerr := json.Unmarshal(body, &j); jerr != nil {
			lastErr = "judge verdict: " + jerr.Error()
			continue
		}
		if len(j.Facts) != len(s.Facts) || len(j.NotCovered) != len(s.NotCovered) {
			lastErr = fmt.Sprintf("judge graded %d facts of %d, %d not-covered of %d", len(j.Facts), len(s.Facts), len(j.NotCovered), len(s.NotCovered))
			continue
		}
		return &j, raw, ""
	}
	return nil, "", lastErr
}

// regrade grades the saved transcripts of the scenarios again.
func (h *flowHarness) regrade(scenarios []flowScenario, rejudge bool) error {
	var summaries []flowSummary
	entries, err := os.ReadDir(h.runDir())
	if err != nil {
		return err
	}
	byID := map[string]flowScenario{}
	for _, s := range scenarios {
		byID[s.ID] = s
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == "results.json" {
			continue
		}
		path := filepath.Join(h.runDir(), e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var r flowRun
		if err := json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		s, ok := byID[r.Scenario]
		if !ok {
			continue
		}
		libs, err := h.libraries(s.Sandbox)
		if err != nil {
			return err
		}
		h.grade(&r, s, libs, rejudge || r.Grade.Judge == nil)
		if err := h.save(&r); err != nil {
			return err
		}
		summaries = append(summaries, summarize(&r, s))
	}
	return h.writeResults(summaries)
}

// flowSummary is one run in a results file.
type flowSummary struct {
	Scenario     string         `json:"scenario"`
	N            int            `json:"n"`
	Flow         string         `json:"flow"`
	Run          int            `json:"run"`
	Unverifiable string         `json:"unverifiable,omitempty"`
	Invalid      string         `json:"invalid,omitempty"`
	Right        bool           `json:"right"`
	Verdicts     []string       `json:"fact_verdicts"`
	NotCovered   []string       `json:"not_covered_verdicts"`
	Invented     int            `json:"invented"`
	EvidenceAll  bool           `json:"evidence_reached"`
	EvidenceAt   []int          `json:"evidence_call"`
	Calls        int            `json:"calls"`
	TokensRead   int            `json:"tokens_read"`
	CapHit       bool           `json:"cap_hit"`
	MaxPerSource int            `json:"max_calls_one_source"`
	OverThree    bool           `json:"over_three"`
	CiteFails    int            `json:"citation_failures"`
	Signals      map[string]int `json:"signals,omitempty"`
	Sequence     []string       `json:"sequence"`
	WallMS       int64          `json:"wall_ms"`
	CostUSD      float64        `json:"cost_usd"`
	Models       []string       `json:"model_resolved"`
	JudgeError   string         `json:"judge_error,omitempty"`
	WebSearch    bool           `json:"web_search,omitempty"`
	ToolErrors   int            `json:"tool_errors,omitempty"`
	SearchTried  int            `json:"search_blocked,omitempty"`
}

func summarize(r *flowRun, s flowScenario) flowSummary {
	g := r.Grade
	sum := flowSummary{Scenario: s.ID, N: s.N, Flow: s.Flow, Run: r.Run, Unverifiable: s.Unverifiable, Invalid: g.Invalid, Right: g.Right,
		EvidenceAll: g.EvidenceAll, Calls: g.Calls, TokensRead: g.TokensRead, CapHit: g.CapHit, MaxPerSource: g.MaxPerSource,
		OverThree: g.OverThree, CiteFails: g.CiteFails, Signals: g.Signals, WallMS: r.WallMS, CostUSD: r.CostUSD,
		Models: r.ModelUsed, JudgeError: g.JudgeError, WebSearch: g.WebSearch, ToolErrors: g.Errors, SearchTried: g.SearchBlocked, Verdicts: []string{}, NotCovered: []string{}, EvidenceAt: []int{}}
	if j := g.Judge; j != nil {
		for _, f := range j.Facts {
			sum.Verdicts = append(sum.Verdicts, f.Verdict)
		}
		for _, n := range j.NotCovered {
			sum.NotCovered = append(sum.NotCovered, n.Verdict)
		}
		sum.Invented = len(j.Invented)
	}
	for _, e := range g.Evidence {
		sum.EvidenceAt = append(sum.EvidenceAt, e.Call)
	}
	for _, c := range r.Calls {
		sum.Sequence = append(sum.Sequence, callLabel(c))
	}
	return sum
}

// callLabel is a call in one line: section reads by their reference and
// page, queries by their words and library.
func callLabel(c flowCall) string {
	var in callInput
	_ = json.Unmarshal(c.Input, &in)
	var s string
	switch {
	case in.Section != "":
		s = "section " + in.Section
		if in.Page > 0 {
			s += fmt.Sprintf(" page=%d", in.Page)
		}
	case in.Resolve:
		s = "resolve " + strconv.Quote(in.Query)
	default:
		s = "query " + strconv.Quote(in.Query)
		if in.Library != "" {
			s += " library=" + in.Library
		}
	}
	if in.Tokens > 0 {
		s += fmt.Sprintf(" tokens=%d", in.Tokens)
	}
	if c.IsError {
		s += " → error"
	} else if c.Status != "" {
		s += " → " + c.Status
		if c.Reason != "" {
			s += " (" + c.Reason + ")"
		}
	}
	if len(c.Signals) > 0 {
		s += " [" + strings.Join(c.Signals, ",") + "]"
	}
	return s
}

// writeResults merges summaries into the build's and model's results
// file, a run replacing the same scenario's and run number's.
func (h *flowHarness) writeResults(summaries []flowSummary) error {
	path := filepath.Join(h.runDir(), "results.json")
	var file struct {
		Build string        `json:"build"`
		Ketch string        `json:"ketch,omitempty"`
		Model string        `json:"model"`
		Judge string        `json:"judge"`
		Runs  []flowSummary `json:"runs"`
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &file)
	}
	file.Build, file.Model, file.Judge = h.build, h.model, h.judge
	if h.ketch != "" {
		file.Ketch = h.ketch
	}
	for _, s := range summaries {
		file.Runs = slices.DeleteFunc(file.Runs, func(o flowSummary) bool { return o.Scenario == s.Scenario && o.Run == s.Run })
		file.Runs = append(file.Runs, s)
	}
	slices.SortFunc(file.Runs, func(a, b flowSummary) int { return cmp.Or(cmp.Compare(a.N, b.N), cmp.Compare(a.Run, b.Run)) })
	b, err := marshalIndent(file)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	printFlowTable(os.Stdout, file.Runs)
	fmt.Printf("results: %s\n", path)
	return nil
}

// printFlowTable prints the totals per flow of the first run of each
// scenario, unverifiable scenarios left out.
func printFlowTable(w io.Writer, runs []flowSummary) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	p("\n%-7s %-7s %-9s %-11s %-11s %-11s %-6s %-4s %-6s %-8s %-9s %-8s\n", "flow", "right", "evidence", "calls med", "calls max", "tokens med", ">3/src", "cap", "cites", "invented", "wall med", "cost")
	for _, flow := range []string{"lookup", "follow", "survey"} {
		var calls, tokens, walls []int
		right, ev, over, capped, cites, inv, n := 0, 0, 0, 0, 0, 0, 0
		cost := 0.0
		for _, r := range runs {
			if r.Flow != flow || r.Run != 1 || r.Unverifiable != "" {
				continue
			}
			n++
			calls, tokens, walls = append(calls, r.Calls), append(tokens, r.TokensRead), append(walls, int(r.WallMS/1000))
			right += b2i(r.Right)
			ev += b2i(r.EvidenceAll)
			over += b2i(r.OverThree)
			capped += b2i(r.CapHit)
			cites += b2i(r.CiteFails > 0)
			inv += b2i(r.Invented > 0)
			cost += r.CostUSD
		}
		if n == 0 {
			continue
		}
		p("%-7s %-7s %-9s %-11s %-11d %-11s %-6d %-4d %-6d %-8d %-9s $%.2f\n", flow, fmt.Sprintf("%d/%d", right, n), fmt.Sprintf("%d/%d", ev, n),
			median(calls), slices.Max(calls), median(tokens), over, capped, cites, inv, median(walls)+"s", cost)
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// verify checks every scenario's gold quotes against its sandbox's index
// (verifyCase, as -verify does for cases) and writes each scenario's
// unverifiable reason, or none, into the scenarios file.
func (h *flowHarness) verify(w io.Writer, path string) error {
	all, err := readScenarios(path)
	if err != nil {
		return err
	}
	texts := map[string]*libText{}
	for i := range all {
		s := &all[i]
		s.Unverifiable = ""
		libs, err := h.libraries(s.Sandbox)
		if err != nil {
			return err
		}
		store := docsindex.Open(filepath.Join(h.sandboxDir(s.Sandbox), "docs-index.sqlite"))
		for _, f := range s.Facts {
			m := docs.MatchIndexed(libs, f.Library)
			if len(m) == 0 {
				s.Unverifiable = "library not indexed: " + f.Library
				break
			}
			key := s.Sandbox + " " + m[0].Key
			if texts[key] == nil {
				if texts[key], err = loadLibText(store, m[0]); err != nil {
					return err
				}
			}
			c := qaCase{Facts: []fact{f.fact}}
			if reason, detail := verifyCase(&c, texts[key]); reason != "" {
				s.Unverifiable = reason + ": " + detail
				break
			}
		}
		state := "ok"
		if s.Unverifiable != "" {
			state = "UNVERIFIABLE " + s.Unverifiable
		}
		_, _ = fmt.Fprintf(w, "%-3d %-48s %s\n", s.N, clip(s.ID, 48), clip(state, 160))
	}
	b, err := marshalIndent(all)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
