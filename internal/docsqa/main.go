// Command docsqa runs the docs QA cases (design/docs-evidence.md, QA)
// against a ketch binary and grades each fact at the pipeline's four
// stages from the answer's trace (ketch docs --trace), and each case as a
// reader meets it: answered (every fact printed), led (every lead
// printed), the tokens printed and those before the first fact. A
// development tool, not part of the ketch binary:
//
//	go run ./internal/docsqa -ketch ./ketch -sandbox /tmp/ketch-qa -cases internal/docsqa/cases/duo.json
//
// The binary runs in the sandbox directory: its own HOME (page cache,
// browser, Context7 quota file), config, tag index and docs index, so the
// user's ketch state is never read or written. Each case's library is
// indexed there once (ketch docs index), and kept for later runs. Each
// question runs once. It prints two tables (stages, readers) and writes
// the same as JSON; it exits 1 when an output check fails, unless
// -no-fail. The trace is read leniently: fields an older binary does not
// write are left empty, and fields it writes that the runner does not know
// are ignored, so binaries of every round are graded alike.
//
// Two modes run no binary. -verify checks a cases file's quotes against the
// sandbox's index and writes the cases that can be graded; -compare prints
// one table for several results files:
//
//	go run ./internal/docsqa -sandbox /tmp/ketch-qa -cases candidates.json -verify broad.json
//	go run ./internal/docsqa -compare old.json,new.json -labels old,new -split tune
//
// -split tune|confirm|all restricts a run or a comparison to one half of
// the broad set (each case's split; cases/broad.README.md).
//
// -flows runs the flow tests instead (flows.go): a model answers each
// scenario with only ketch's MCP docs tool, and the run is graded.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/docsindex"
)

// qaCase is one question with its known answer: facts the answer must
// print, leads (words the first call must print so that the next call is
// exact, for a question whose words are not the docs' words), and terms
// the docs lack.
type qaCase struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Library  string   `json:"library"`
	Facts    []fact   `json:"facts"`
	Leads    []string `json:"leads,omitempty"`
	NotFound []string `json:"not_found"`
	Budget   int      `json:"budget"`
	// Kind labels the case for reports (the kind of documentation); the
	// grading does not read it.
	Kind string `json:"kind,omitempty"`
	// Split is the half of the broad set the case's site is in: "tune",
	// where rules are chosen, or "confirm", run once per candidate build
	// and never tuned on. A case without one is on the tune side.
	Split string `json:"split,omitempty"`
}

// inSplit reports a case of half split (its Split field) kept by want:
// "" and "all" keep every case.
func inSplit(split, want string) bool {
	if want == "" || want == "all" {
		return true
	}
	if split == "" {
		split = "tune"
	}
	return split == want
}

// fact is a quote the answer must carry, and the section (url#anchor) it is
// in. A fact stated in more than one way lists Alternatives instead (or as
// well): it passes a check when every quote of any one alternative passes
// it.
type fact struct {
	Quote        string     `json:"quote,omitempty"`
	Source       string     `json:"source,omitempty"`
	Alternatives [][]anchor `json:"alternatives,omitempty"`
}

// anchor is one quote of an alternative and the section it is in.
type anchor struct {
	Quote  string `json:"quote"`
	Source string `json:"source"`
}

// alternatives is f's alternatives: its own quote first, when it has one.
func (f fact) alternatives() [][]anchor {
	var out [][]anchor
	if f.Quote != "" {
		out = append(out, []anchor{{Quote: f.Quote, Source: f.Source}})
	}
	return append(out, f.Alternatives...)
}

func main() {
	ketch := flag.String("ketch", "", "the ketch binary to run (required)")
	sandbox := flag.String("sandbox", "", "the directory ketch runs in: its HOME, config, cache and indexes (required)")
	casesPath := flag.String("cases", "", "the cases file, a JSON array (required)")
	out := flag.String("json", "", "write the results as JSON here (default <sandbox>/results.json)")
	noFail := flag.Bool("no-fail", false, "exit 0 even when an output check fails (a baseline run)")
	only := flag.String("only", "", "run only these case IDs, comma-separated")
	split := flag.String("split", "all", "run (or with -compare, report) only this half of the cases: tune, confirm or all")
	traces := flag.String("traces", "", "write each case's trace in this directory (default <sandbox>/traces)")
	maxPages := flag.Int("max-pages", 0, "index a library that is not in the sandbox with ketch docs index --max-pages N (default: ketch's own)")
	compareFiles := flag.String("compare", "", "print one table for these results files (comma-separated) and exit; runs nothing")
	labels := flag.String("labels", "", "with -compare: a short name per results file, comma-separated")
	verifyOut := flag.String("verify", "", "check every quote of -cases against the sandbox's index, write the cases that can be graded here and exit; runs no question")
	flag.Parse()
	if !slices.Contains([]string{"all", "tune", "confirm"}, *split) {
		fmt.Fprintln(os.Stderr, "docsqa: -split is tune, confirm or all")
		os.Exit(2)
	}
	switch {
	case *flowsPath != "":
		exitOn(runFlows(*ketch, *only))
		return
	case *compareFiles != "":
		exitOn(compare(os.Stdout, splitList(*compareFiles), splitList(*labels), *split))
		return
	case *verifyOut != "" && *sandbox != "" && *casesPath != "":
		exitOn(verify(os.Stdout, *sandbox, *casesPath, *verifyOut))
		return
	case *ketch == "" || *sandbox == "" || *casesPath == "":
		flag.Usage()
		os.Exit(2)
	}
	if *out == "" {
		*out = filepath.Join(*sandbox, "results.json")
	}
	indexMaxPages, traceDir = *maxPages, *traces
	report, err := run(*ketch, *sandbox, *casesPath, *only, *split)
	exitOn(err)
	printTable(os.Stdout, report)
	b, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		err = os.WriteFile(*out, append(b, '\n'), 0o644)
	}
	exitOn(err)
	fmt.Printf("\nresults: %s\n", *out)
	if !*noFail && report.Totals.Output.Passed < report.Totals.Output.Checks {
		os.Exit(1)
	}
}

// exitOn exits 2 with err, when there is one.
func exitOn(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "docsqa:", err)
		os.Exit(2)
	}
}

func run(ketch, sandbox, casesPath, only, split string) (*report, error) {
	cases, err := readCases(casesPath, only)
	if err != nil {
		return nil, err
	}
	cases = slices.DeleteFunc(cases, func(c qaCase) bool { return !inSplit(c.Split, split) })
	ketch, err = filepath.Abs(ketch)
	if err != nil {
		return nil, err
	}
	sb, err := newSandbox(ketch, sandbox)
	if err != nil {
		return nil, err
	}
	built := map[string]bool{}
	r := &report{Ketch: ketch, Version: sb.version(), Sandbox: sb.dir, Cases: casesPath, Run: time.Now().UTC()}
	for _, c := range cases {
		if !built[c.Library] {
			if err := sb.ensureIndex(c.Library); err != nil {
				return nil, err
			}
			built[c.Library] = true
		}
		r.Results = append(r.Results, sb.runCase(c))
	}
	r.total()
	return r, nil
}

func readCases(path, only string) ([]qaCase, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []qaCase
	if err := json.Unmarshal(b, &cases); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	want := map[string]bool{}
	for _, id := range strings.Split(only, ",") {
		if id = strings.TrimSpace(id); id != "" {
			want[id] = true
		}
	}
	var out []qaCase
	for _, c := range cases {
		if c.ID == "" || c.Question == "" || c.Library == "" || c.Budget <= 0 {
			return nil, fmt.Errorf("%s: case %q needs id, question, library and budget", path, c.ID)
		}
		if len(want) == 0 || want[c.ID] {
			out = append(out, c)
		}
	}
	return out, nil
}

// sandbox runs the ketch binary with its state under one directory.
type sandbox struct {
	ketch, dir string
	env        []string
}

func newSandbox(ketch, dir string) (*sandbox, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	home := filepath.Join(dir, "home")
	if traceDir == "" {
		traceDir = filepath.Join(dir, "traces")
	}
	for _, d := range []string{home, traceDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	config := filepath.Join(dir, "config.json")
	if _, err := os.Stat(config); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(config, []byte("{}\n"), 0o644); err != nil {
			return nil, err
		}
	}
	// The parent's KETCH_* settings (an index path, a trace, a replay) and
	// XDG directories would reach past the sandbox: they are replaced.
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k != "HOME" && !strings.HasPrefix(k, "KETCH_") && !strings.HasPrefix(k, "XDG_") {
			env = append(env, kv)
		}
	}
	env = append(env,
		"HOME="+home,
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"KETCH_CONFIG="+config,
		"KETCH_DOCS_INDEX_PATH="+filepath.Join(dir, "docs-index.sqlite"),
		"KETCH_TAGS_PATH="+filepath.Join(dir, "tags.db"),
		"KETCH_NO_UPDATE_NOTIFIER=1",
	)
	return &sandbox{ketch: ketch, dir: dir, env: env}, nil
}

// exec runs ketch with args; stderr goes to stderr when echo is set.
func (s *sandbox) exec(echo bool, args ...string) (stdout, stderr []byte, code int, err error) {
	cmd := exec.Command(s.ketch, args...)
	cmd.Env, cmd.Dir = s.env, s.dir
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	if echo {
		cmd.Stderr = os.Stderr
	}
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return o.Bytes(), e.Bytes(), exit.ExitCode(), nil
	}
	return o.Bytes(), e.Bytes(), 0, err
}

func (s *sandbox) version() string {
	out, _, _, err := s.exec(false, "version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// indexMaxPages is -max-pages: passed to ketch docs index when above 0.
// traceDir is -traces, the directory each case's trace is written in.
var (
	indexMaxPages int
	traceDir      string
)

// ensureIndex indexes library in the sandbox unless a full index of it
// (built by ketch docs index, not by a search) is there.
func (s *sandbox) ensureIndex(library string) error {
	out, _, code, err := s.exec(false, "docs", "index", "--list", "--json")
	if err != nil || code != 0 {
		return fmt.Errorf("listing the sandbox index: exit %d %v", code, err)
	}
	var libs, full []docsindex.Library
	if err := json.Unmarshal(out, &libs); err != nil {
		return fmt.Errorf("listing the sandbox index: %w", err)
	}
	for _, l := range libs {
		if l.Current() && l.Resolved != "search" {
			full = append(full, l)
		}
	}
	if len(docs.MatchIndexed(full, library)) > 0 {
		return nil
	}
	fmt.Fprintf(os.Stderr, "docsqa: indexing %s in %s\n", library, s.dir)
	args := []string{"docs", "index", library}
	if indexMaxPages > 0 {
		args = append(args, "--max-pages", strconv.Itoa(indexMaxPages))
	}
	if _, _, code, err := s.exec(true, args...); err != nil || code != 0 {
		return fmt.Errorf("ketch docs index %s: exit %d %v", library, code, err)
	}
	return nil
}

// runCase asks the case's question once and grades it from its trace.
func (s *sandbox) runCase(c qaCase) caseResult {
	tracePath := filepath.Join(traceDir, c.ID+".json")
	_ = os.Remove(tracePath)
	start := time.Now()
	stdout, stderr, code, err := s.exec(false, "docs", "--library", c.Library, "--tokens", strconv.Itoa(c.Budget), "--trace", tracePath, c.Question)
	r := caseResult{ID: c.ID, Question: c.Question, Library: c.Library, Kind: c.Kind, Split: c.Split, Budget: c.Budget, Exit: code, WallMS: time.Since(start).Milliseconds(), Trace: tracePath}
	if err != nil {
		r.Error = err.Error()
	} else if code != 0 {
		r.Error = strings.TrimSpace(string(stderr))
	}
	tr, terr := readTrace(tracePath)
	if terr != nil {
		r.Error = strings.TrimSpace(r.Error + "; trace: " + terr.Error())
		tr = &trace{}
		tr.Output.Text = string(stdout)
	}
	grade(&r, c, tr)
	return r
}

func readTrace(path string) (*trace, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t trace
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
