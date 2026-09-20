package main

import (
	"path/filepath"
	"testing"
)

func TestParseOptionsExtractMode(t *testing.T) {
	opts, err := parseOptions("check", []string{"-extract-mode", "clean"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.ExtractMode != "clean" || filepath.Base(opts.Out) != "check-default-clean" || filepath.Base(opts.Baseline) != "baseline-clean.json" {
		t.Fatalf("clean mode paths: %+v", opts)
	}
	opts, err = parseOptions("check", nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.ExtractMode != "complete" || filepath.Base(opts.Out) != "check-default" || filepath.Base(opts.Baseline) != "baseline.json" {
		t.Fatalf("default mode paths: %+v", opts)
	}
	if _, err := parseOptions("run", []string{"-extract-mode", "fast"}); err == nil {
		t.Fatal("unknown extract mode accepted")
	}
}

func TestCompatibleRunsExtractMode(t *testing.T) {
	complete, legacy, clean := &report{ExtractMode: "complete"}, &report{}, &report{ExtractMode: "clean"}
	if !compatibleRuns(complete, legacy) {
		t.Fatal("a report without the field measured the default")
	}
	if compatibleRuns(complete, clean) {
		t.Fatal("modes must match")
	}
}
