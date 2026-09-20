package main

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

type limitedBuffer struct {
	buf   bytes.Buffer
	Limit int
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if len(data) > b.Limit-b.buf.Len() {
		return 0, fmt.Errorf("command output exceeds %d bytes", b.Limit)
	}
	return b.buf.Write(data)
}

func (b *limitedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *limitedBuffer) String() string { return b.buf.String() }

func isolatedEnv(dir string) []string {
	var env []string
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "KETCH_") || key == "XDG_CACHE_HOME" {
			continue
		}
		env = append(env, pair)
	}
	return append(env, "KETCH_CONFIG="+filepath.Join(dir, "config.json"),
		"KETCH_TAGS_PATH="+filepath.Join(dir, "tags.db"), "XDG_CACHE_HOME="+filepath.Join(dir, "cache"),
		"KETCH_NO_UPDATE_NOTIFIER=1", "NO_COLOR=1")
}

func buildBinary(ctx context.Context, opts options, temp string) (string, error) {
	if opts.Binary != "" {
		return filepath.Abs(opts.Binary)
	}
	name := "ketch"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(temp, name)
	cmd := exec.CommandContext(ctx, "go", "build", "-o", path, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Dir = filepath.Dir(opts.Dir)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("build ketch: %w", err)
	}
	return path, nil
}

func invoke(ctx context.Context, binary string, env []string, f fixture, opts options) (string, float64, error) {
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	args := []string{"extract", "--url", f.Spec.FinalURL, "--json"}
	if opts.Mode == "selector" {
		args = append(args, "--select", f.Spec.Selector)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	cmd.WaitDelay = time.Second
	cmd.Stdin = bytes.NewReader(f.HTML)
	out := &limitedBuffer{Limit: 32 << 20}
	errOut := &limitedBuffer{Limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, errOut
	started := time.Now()
	err := cmd.Run()
	ms := float64(time.Since(started)) / float64(time.Millisecond)
	if err != nil {
		return "", ms, fmt.Errorf("extract: %w (%s; context: %v)", err, strings.TrimSpace(errOut.String()), ctx.Err())
	}
	var page struct {
		URL      string `json:"url"`
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(out.Bytes(), &page); err != nil {
		return "", ms, fmt.Errorf("invalid JSON output: %w", err)
	}
	if strings.TrimSpace(page.Markdown) == "" {
		return "", ms, fmt.Errorf("empty extraction")
	}
	return page.Markdown, ms, nil
}

func runPage(ctx context.Context, binary string, env []string, f fixture, opts options) pageResult {
	r := pageResult{ID: f.Spec.ID, Site: f.Spec.Site, Kind: f.Spec.Kind, Cohort: f.Spec.Cohort, URL: f.Spec.FinalURL,
		Deterministic: true, Accuracy: failedAccuracy(f), Errors: []string{}}
	f, err := materialize(f)
	if err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("load fixture: %v", err))
		return r
	}
	r.InputBytes = len(f.HTML)
	var first string
	for i := 0; i < opts.Warmup+opts.Iterations; i++ {
		markdown, ms, err := invoke(ctx, binary, env, f, opts)
		if err != nil {
			r.Errors = append(r.Errors, fmt.Sprintf("sample %d: %v", i, err))
			break
		}
		if first == "" {
			first, r.OutputSHA = markdown, digest([]byte(markdown))
		} else if digest([]byte(markdown)) != r.OutputSHA {
			r.Deterministic = false
		}
		if i >= opts.Warmup {
			r.SamplesMS = append(r.SamplesMS, ms)
		}
	}
	r.MedianMS, r.P95MS = percentile(r.SamplesMS, .5), percentile(r.SamplesMS, .95)
	if len(r.Errors) > 0 || !r.Deterministic {
		return r // Failed invocations never receive free "absent" passes.
	}
	finishPage(&r, first, f, opts)
	return r
}

func finishPage(r *pageResult, markdown string, f fixture, opts options) {
	r.OutputBytes = len(markdown)
	a, err := score(markdown, f.Reference, f.Spec.Checks)
	if err != nil {
		r.Errors = append(r.Errors, err.Error())
		return
	}
	r.Accuracy = a
	path := filepath.Join(opts.Out, "markdown", f.Spec.ID+".md")
	if err := os.WriteFile(path, []byte(markdown), 0o644); err != nil {
		r.Errors = append(r.Errors, err.Error())
		r.Accuracy = failedAccuracy(f)
	}
}

func percentile(samples []float64, p float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	if p == .5 && len(sorted)%2 == 0 {
		return (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}
	return sorted[max(0, int(math.Ceil(p*float64(len(sorted))))-1)]
}

func execute(ctx context.Context, fixtures []fixture, corpusSHA string, opts options) (*report, error) {
	temp, err := os.MkdirTemp("", "ketch-bench-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(temp) }()
	if err := os.WriteFile(filepath.Join(temp, "config.json"), []byte("{}\n"), 0o600); err != nil {
		return nil, err
	}
	binary, err := buildBinary(ctx, opts, temp)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(opts.Out, "markdown"), 0o755); err != nil {
		return nil, err
	}
	r := &report{Version: schemaVersion, Timestamp: time.Now().UTC().Format(time.RFC3339), CorpusSHA: corpusSHA,
		BinarySHA: digest(data), Platform: runtime.GOOS + "/" + runtime.GOARCH, CPUs: runtime.NumCPU(),
		Mode: opts.Mode, ExtractMode: opts.ExtractMode, Iterations: opts.Iterations, Warmup: opts.Warmup, Workers: opts.Workers}
	if err := recordBuildInfo(r, binary); err != nil {
		return nil, err
	}
	start := time.Now()
	env := append(isolatedEnv(temp), "KETCH_EXTRACT_MODE="+opts.ExtractMode)
	r.Pages = runWorkers(ctx, fixtures, opts.Workers, func(f fixture) pageResult {
		return runPage(ctx, binary, env, f, opts)
	})
	r.WallSeconds = time.Since(start).Seconds()
	r.Summary, r.ByKind = summarize(r.Pages)
	r.ByCohort = summarizeCohorts(r.Pages)
	return r, nil
}

func recordBuildInfo(r *report, binary string) error {
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return fmt.Errorf("read ketch Go build metadata: %w", err)
	}
	r.GoVersion = info.GoVersion
	for _, setting := range info.Settings {
		if setting.Key == "CGO_ENABLED" {
			r.CGOEnabled = setting.Value
		}
	}
	return nil
}

func runWorkers(ctx context.Context, fixtures []fixture, workers int, run func(fixture) pageResult) []pageResult {
	results := make([]pageResult, len(fixtures))
	jobs := make(chan int)
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			for index := range jobs {
				results[index] = run(fixtures[index])
			}
		})
	}
	for index := range fixtures {
		if ctx.Err() != nil {
			// Keep cancelled pages in the result denominator too.
			f := fixtures[index]
			results[index] = pageResult{ID: f.Spec.ID, Site: f.Spec.Site, Kind: f.Spec.Kind, Cohort: f.Spec.Cohort, URL: f.Spec.FinalURL,
				Errors: []string{ctx.Err().Error()}, Accuracy: failedAccuracy(f)}
			continue
		}
		jobs <- index
	}
	close(jobs)
	group.Wait()
	return results
}
