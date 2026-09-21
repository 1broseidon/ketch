package main

import (
	"math"
	"strings"
	"testing"
	"time"
)

func sampleReport() *report {
	return &report{Version: 1, CorpusSHA: "same", Mode: "default", Platform: "test", CPUs: 4, Workers: 1, Iterations: 7, Warmup: 1,
		Pages: []pageResult{{ID: "sample", Deterministic: true, MedianMS: 10, Accuracy: accuracy{
			TokenRecall: .95, TokenPrecision: .9, Checks: []checkResult{
				{ID: "passes", Passed: true, Critical: true}, {ID: "known-miss", Passed: false, Critical: true},
			},
		}}}}
}

func TestBaselineDetectsRegressionsIndividually(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		edit func(*report)
		want string
	}{
		{"unchanged", func(*report) {}, ""},
		{"masked by improvement", func(r *report) {
			r.Pages[0].Accuracy.Checks[0].Passed = false
			r.Pages[0].Accuracy.Checks[1].Passed = true
		}, "previously passing"},
		{"known miss deleted", func(r *report) { r.Pages[0].Accuracy.Checks[1].ID = "replacement" }, "missing assertion"},
		{"missing page", func(r *report) { r.Pages = nil }, "missing page"},
		{"output varies", func(r *report) { r.Pages[0].Deterministic = false }, "nondeterminism"},
		{"process failed", func(r *report) { r.Pages[0].Errors = []string{"timeout"} }, "execution failure"},
		{"slow", func(r *report) { r.Pages[0].MedianMS = 25 }, "median"},
		{"small jitter", func(r *report) { r.Pages[0].MedianMS = 11 }, ""},
		{"recall drop", func(r *report) { r.Pages[0].Accuracy.TokenRecall = .9 }, "coverage regressed"},
		{"noise increase", func(r *report) { r.Pages[0].Accuracy.TokenPrecision = .8 }, "coverage regressed"},
		{"source changed", func(r *report) { r.CorpusSHA = "new" }, "corpus"},
		{"selector mode", func(r *report) { r.Mode = "selector" }, "incompatible"},
		{"stress mode", func(r *report) { r.Workers = 4 }, "incompatible"},
		{"different samples", func(r *report) { r.Iterations = 1 }, "incompatible"},
		{"different build", func(r *report) { r.CGOEnabled = "1" }, "incompatible"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			current, base := sampleReport(), sampleReport()
			tc.edit(current)
			failures := compareReports(current, base, options{SpeedRatio: 2, AccuracyDrop: .005})
			if tc.want == "" && len(failures) != 0 {
				t.Fatalf("unexpected failure: %v", failures)
			}
			if tc.want != "" && !strings.Contains(strings.Join(failures, "\n"), tc.want) {
				t.Fatalf("wanted %q: %v", tc.want, failures)
			}
		})
	}
}

func TestIncompatibleBaselineNamesEveryDifferingField(t *testing.T) {
	t.Parallel()
	current, base := sampleReport(), sampleReport()
	current.CGOEnabled, base.CGOEnabled = "0", "1"
	current.GoVersion, base.GoVersion = "go1.25.7", "go1.24.0"
	current.Workers = 4 // base.Workers is 1, set in sampleReport

	failures := compareReports(current, base, options{SpeedRatio: 2, AccuracyDrop: .005})
	joined := strings.Join(failures, "\n")
	for _, want := range []string{
		`incompatible baseline: cgo_enabled: baseline "1", current "0"`,
		`incompatible baseline: go_version: baseline "go1.24.0", current "go1.25.7"`,
		`incompatible baseline: workers: baseline "1", current "4"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing field diff %q in: %v", want, failures)
		}
	}
	for _, unwanted := range []string{"platform:", "cpus:", "mode:", "iterations:", "warmup:", "extract_mode:"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("reported a field that did not differ (%q) in: %v", unwanted, failures)
		}
	}
	if len(failures) != 3 {
		t.Fatalf("want exactly 3 field diffs, got %d: %v", len(failures), failures)
	}
}

func TestStrictModeRejectsKnownMisses(t *testing.T) {
	t.Parallel()
	failures := strictFailures(sampleReport())
	if len(failures) != 1 || !strings.Contains(failures[0], "known-miss") {
		t.Fatalf("known critical miss not gated: %v", failures)
	}
}

func TestThresholdsRejectNaNAndInfinity(t *testing.T) {
	t.Parallel()
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, opts := range []options{
			{Mode: "default", Iterations: 1, Workers: 1, Timeout: time.Second, SpeedRatio: bad},
			{Mode: "default", Iterations: 1, Workers: 1, Timeout: time.Second, SpeedRatio: 2, AccuracyDrop: bad},
		} {
			if err := validateOptions(opts); err == nil {
				t.Fatalf("invalid numeric gate accepted: %+v", opts)
			}
		}
	}
}

func TestCommandFlagsApplyOnlyWhereUsed(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions("run", []string{"-workers", "4", "-iterations", "25", "-warmup", "2", "-mode", "selector", "-out", "custom-output"})
	if err != nil || opts.Workers != 4 || opts.Iterations != 25 || opts.Warmup != 2 || opts.Mode != "selector" || opts.Out != "custom-output" {
		t.Fatalf("measurement flags ignored: %+v, %v", opts, err)
	}
	for _, tc := range []struct{ command, flag string }{
		{"live", "-iterations"}, {"setup", "-binary"}, {"run", "-speed-ratio"},
	} {
		if _, err := parseOptions(tc.command, []string{tc.flag, "3"}); err == nil {
			t.Errorf("%s accepted inert flag %s", tc.command, tc.flag)
		}
	}
}
