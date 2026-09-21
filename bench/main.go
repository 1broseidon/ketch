package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"time"
)

const usage = `Ketch extraction benchmark

  go -C bench run . setup             fetch the corpus archive if needed, validate snapshots and annotations
  go -C bench run . run               measure the current binary (built automatically)
  go -C bench run . check             compare with baseline.json, fail on regressions
  go -C bench run . update-baseline   explicitly replace the accepted baseline
  go -C bench run . live              fetch candidate snapshots; never replace gold data

Accuracy runs are offline with temporary config and tag paths; extract uses no page cache.
run reports known misses; check --strict also fails every critical miss.
Use -h after a command to list options.
`

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := entry(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}

func entry(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(usage)
		return nil
	}
	command := args[0]
	switch command {
	case "setup", "run", "check", "update-baseline", "live":
	default:
		return fmt.Errorf("unknown command %q\n%s", command, usage)
	}
	opts, err := parseOptions(command, args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if command == "setup" {
		if err := ensureTestdata(ctx, opts.Dir, opts.Archive); err != nil {
			return err
		}
	}
	fixtures, hash, err := loadCorpus(opts.Dir)
	if err != nil {
		return err
	}
	if err := validateReferences(fixtures); err != nil {
		return err
	}
	return dispatch(ctx, command, fixtures, hash, opts)
}

func parseOptions(command string, args []string) (options, error) {
	opts := options{Mode: "default", ExtractMode: "complete", Iterations: 7, Warmup: 1, Workers: 1, Timeout: 20 * time.Second, SpeedRatio: 2, AccuracyDrop: .005}
	set := flag.NewFlagSet(command, flag.ContinueOnError)
	set.StringVar(&opts.Dir, "dir", ".", "corpus directory (default: this bench directory; invoke via go -C bench or cd bench first)")
	if command == "setup" {
		set.StringVar(&opts.Archive, "archive", "", "corpus archive URL or local .tar.gz path; overrides archive.json")
	}
	if command != "setup" {
		set.StringVar(&opts.Out, "out", "", "output directory; defaults to bench/.runs/<command>-<mode>")
		set.DurationVar(&opts.Timeout, "timeout", opts.Timeout, "timeout per invocation or live fetch")
	}
	if command != "setup" && command != "live" {
		measurementFlags(set, &opts)
	}
	if command == "check" || command == "update-baseline" {
		set.StringVar(&opts.Baseline, "baseline", "", "baseline JSON path; defaults to bench/baseline.json")
	}
	if command == "check" {
		set.Float64Var(&opts.SpeedRatio, "speed-ratio", opts.SpeedRatio, "maximum per-page median slowdown (also requires >2ms increase)")
		set.Float64Var(&opts.AccuracyDrop, "accuracy-drop", opts.AccuracyDrop, "maximum absolute token recall/precision decrease per page")
	}
	if err := set.Parse(args); err != nil {
		return opts, err
	}
	return finishOptions(set, opts, command)
}

func measurementFlags(set *flag.FlagSet, opts *options) {
	set.StringVar(&opts.Binary, "binary", "", "existing ketch binary; otherwise build current worktree outside timing")
	set.StringVar(&opts.Mode, "mode", opts.Mode, "default or selector (assisted recovery, reported separately)")
	set.StringVar(&opts.ExtractMode, "extract-mode", opts.ExtractMode, "extraction mode under test: complete or clean (the binary runs with KETCH_EXTRACT_MODE set to it)")
	set.IntVar(&opts.Iterations, "iterations", opts.Iterations, "measured invocations per page")
	set.IntVar(&opts.Warmup, "warmup", opts.Warmup, "unmeasured warmup invocations per page")
	set.IntVar(&opts.Workers, "workers", opts.Workers, "concurrent page workers; baseline comparisons require matching count")
	set.BoolVar(&opts.Strict, "strict", false, "fail any critical assertion, including known baseline misses")
}

func finishOptions(set *flag.FlagSet, opts options, command string) (options, error) {
	if set.NArg() != 0 {
		return opts, fmt.Errorf("unexpected arguments: %v", set.Args())
	}
	if err := validateOptions(opts); err != nil {
		return opts, err
	}
	var err error
	opts.Dir, err = filepath.Abs(opts.Dir)
	if err != nil {
		return opts, err
	}
	if opts.Out == "" {
		opts.Out = filepath.Join(opts.Dir, ".runs", command+"-"+opts.Mode+opts.suffix())
	}
	if opts.Baseline == "" {
		opts.Baseline = filepath.Join(opts.Dir, "baseline"+opts.suffix()+".json")
	}
	return opts, nil
}

func validateOptions(o options) error {
	if o.Mode != "default" && o.Mode != "selector" {
		return fmt.Errorf("mode must be default or selector")
	}
	if o.ExtractMode != "complete" && o.ExtractMode != "clean" {
		return fmt.Errorf("extract-mode must be complete or clean")
	}
	if o.Iterations < 1 || o.Warmup < 0 || o.Workers < 1 || o.Workers > 32 {
		return fmt.Errorf("iterations >=1, warmup >=0 and workers 1..32 required")
	}
	if o.Timeout <= 0 || !validThresholds(o.SpeedRatio, o.AccuracyDrop) {
		return fmt.Errorf("invalid timeout or regression thresholds")
	}
	return nil
}

func validThresholds(ratio, drop float64) bool {
	return !math.IsNaN(ratio) && !math.IsInf(ratio, 0) && ratio >= 1 &&
		!math.IsNaN(drop) && !math.IsInf(drop, 0) && drop >= 0 && drop <= 1
}

func dispatch(ctx context.Context, command string, fixtures []fixture, hash string, opts options) error {
	if command == "setup" {
		fmt.Printf("Validated %d pinned pages; corpus %s\n", len(fixtures), hash)
		return nil
	}
	if command == "live" {
		return runLive(ctx, fixtures, opts)
	}
	var baseline report
	if command == "check" {
		if _, err := readJSON(opts.Baseline, &baseline); err != nil {
			return err
		}
	}
	r, err := execute(ctx, fixtures, hash, opts)
	if err != nil {
		return err
	}
	if err := saveReport(r, opts.Out); err != nil {
		return err
	}
	fmt.Printf("%d/%d checks; %d/%d critical; %d failed pages. Report: %s\n", r.Summary.ChecksPassed, r.Summary.ChecksTotal,
		r.Summary.CriticalPassed, r.Summary.CriticalTotal, r.Summary.FailedPages, filepath.Join(opts.Out, "RESULTS.md"))
	return finishCommand(command, r, &baseline, opts)
}

func finishCommand(command string, r, baseline *report, opts options) error {
	var failures []string
	if r.Summary.FailedPages > 0 {
		failures = append(failures, "execution failure or nondeterministic output")
	}
	if opts.Strict {
		failures = append(failures, strictFailures(r)...)
	}
	if command == "check" {
		failures = append(failures, compareReports(r, baseline, opts)...)
	}
	for _, failure := range failures {
		fmt.Fprintln(os.Stderr, "FAIL:", failure)
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d benchmark failures", len(failures))
	}
	if command == "update-baseline" {
		if err := writeJSON(opts.Baseline, r); err != nil {
			return err
		}
		fmt.Printf("Baseline saved with %d known check failures. This is a regression baseline, not a release approval.\n", r.Summary.ChecksTotal-r.Summary.ChecksPassed)
	}
	return nil
}
