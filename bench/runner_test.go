package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as a portable subprocess fixture. No shell, network,
// operator config or installed ketch binary is needed for failure-path tests.
func TestMain(m *testing.M) {
	if mode := os.Getenv("KETCH_BENCH_TEST_HELPER"); mode != "" {
		helperProcess(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helperProcess(mode string) {
	switch mode {
	case "timeout":
		time.Sleep(time.Minute)
	case "invalid":
		fmt.Print("not JSON")
	case "failed":
		fmt.Fprintln(os.Stderr, "fixture failure")
		os.Exit(7)
	case "huge":
		fmt.Print(strings.Repeat("x", 33<<20))
	default:
		markdown := "Expected content"
		if mode == "empty" {
			markdown = ""
		}
		if mode == "nondeterministic" {
			markdown += time.Now().String()
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"markdown": markdown}); err != nil {
			os.Exit(8)
		}
	}
}

func helperBinary(t *testing.T, mode string) (string, []string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return binary, append(os.Environ(), "KETCH_BENCH_TEST_HELPER="+mode)
}

func TestInvokeHandlesProcessFailures(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"valid", "empty", "invalid", "failed", "huge", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			binary, env := helperBinary(t, mode)
			timeout := 3 * time.Second
			if mode == "timeout" {
				timeout = 100 * time.Millisecond
			}
			md, _, err := invoke(context.Background(), binary, env, fixture{Spec: pageSpec{FinalURL: "https://example.test"}}, options{Timeout: timeout})
			if mode == "valid" && (err != nil || md != "Expected content") {
				t.Fatalf("valid child failed: %s, %v", md, err)
			}
			if mode != "valid" && err == nil {
				t.Fatalf("%s output accepted", mode)
			}
		})
	}
}

func TestRunPageRejectsNondeterminism(t *testing.T) {
	t.Parallel()
	binary, env := helperBinary(t, "nondeterministic")
	f := fixture{Spec: pageSpec{ID: "test", Checks: []assertion{{ID: "noise", Kind: "absent", Text: "ads"}}}, Reference: "Expected content"}
	r := runPage(context.Background(), binary, env, f, options{Iterations: 2, Timeout: 3 * time.Second})
	if r.Deterministic || r.Accuracy.Checks[0].Passed {
		t.Fatalf("nondeterministic extraction received credit: %+v", r)
	}
}

func TestCancelledWorkersKeepAllPages(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fixtures := []fixture{{Spec: pageSpec{ID: "one"}}, {Spec: pageSpec{ID: "two"}}}
	results := runWorkers(ctx, fixtures, 2, func(f fixture) pageResult {
		t.Errorf("cancelled page ran: %s", f.Spec.ID)
		return pageResult{}
	})
	if len(results) != len(fixtures) || len(results[0].Errors) == 0 || len(results[1].Errors) == 0 {
		t.Fatalf("cancelled pages disappeared: %+v", results)
	}
}

func TestOutputCapAppliesToIOCopy(t *testing.T) {
	t.Parallel()
	b := &limitedBuffer{Limit: 4}
	if _, err := io.Copy(b, strings.NewReader("12345")); err == nil {
		t.Fatal("io.Copy bypassed output cap")
	}
	if len(b.Bytes()) > 4 {
		t.Fatal("oversized output retained")
	}
}

func TestIsolatedEnvironment(t *testing.T) {
	t.Setenv("KETCH_CONFIG", "/operator/config")
	t.Setenv("KETCH_TAGS_PATH", "/operator/tags.db")
	t.Setenv("KETCH_BRAVE_KEY", "operator-secret")
	dir := t.TempDir()
	env := strings.Join(isolatedEnv(dir), "\n")
	if strings.Contains(env, "operator-secret") || strings.Contains(env, "/operator/") {
		t.Fatal("operator ketch environment leaked into benchmark")
	}
	if !strings.Contains(env, "KETCH_CONFIG="+filepath.Join(dir, "config.json")) || !strings.Contains(env, "KETCH_TAGS_PATH="+filepath.Join(dir, "tags.db")) {
		t.Fatal("isolated paths missing")
	}
}

func TestPercentiles(t *testing.T) {
	t.Parallel()
	if percentile([]float64{9, 1, 3, 2}, .5) != 2.5 || percentile([]float64{9, 1, 3, 2}, .95) != 9 {
		t.Fatal("incorrect median or nearest-rank p95")
	}
}
