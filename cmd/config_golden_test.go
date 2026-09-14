package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/doctor"
)

func assertCompatibilityGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_REGISTRY_GOLDENS") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s changed\nwant:\n%s\ngot:\n%s", name, want, got)
	}
}

func TestRegistryConfigCompatibilityGolden(t *testing.T) {
	for _, key := range []string{"KETCH_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
		t.Setenv(key, "")
	}
	t.Setenv("PATH", t.TempDir())
	c := config.Defaults()
	for _, name := range []string{"defaults", "configured"} {
		t.Run(name, func(t *testing.T) {
			if name == "configured" {
				for key, value := range map[string]string{"brave_api_key": "fixture-one", "brave_api_keys": "[\"fixture-one\",\"fixture-two\"]", "context7_api_key": "fixture-docs", "github_token": "fixture-github", "firecrawl_url": "http://localhost:3002/"} {
					if err := applyConfigSet(&c, key, value); err != nil {
						t.Fatal(err)
					}
				}
			}
			info, err := json.MarshalIndent(buildConfigInfo(c, "/fixture/config.json"), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			assertCompatibilityGolden(t, "config-"+name+".json", info)
			onDisk, err := json.MarshalIndent(c, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			assertCompatibilityGolden(t, "config-file-"+name+".json", onDisk)
			var loaded config.Config
			if err := json.Unmarshal(onDisk, &loaded); err != nil {
				t.Fatal(err)
			}
			roundTrip, err := json.MarshalIndent(loaded, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if string(roundTrip) != string(onDisk) {
				t.Fatal("config round trip changed bytes")
			}
		})
	}
}

func TestRegistryDoctorTextGolden(t *testing.T) {
	old := cfg
	cfg = config.Defaults()
	t.Cleanup(func() { cfg = old })
	checks := []doctor.Check{
		{Surface: "search", Backend: "brave", Status: doctor.StatusNoKey, Detail: "missing fixture key", LatencyMS: 0, Required: true},
		{Surface: "code", Backend: "grepapp", Status: doctor.StatusOK, LatencyMS: 12, Required: true},
		{Surface: "docs", Backend: "context7", Status: doctor.StatusMisconfigured, Detail: "fixture rejected", LatencyMS: 3, Required: true},
		{Surface: "browser", Backend: "none", Status: doctor.StatusSkipped, Detail: "not configured", LatencyMS: 0},
	}
	assertCompatibilityGolden(t, "doctor.txt", []byte(captureStdout(t, func() { printDoctorReport(checks) })))
	if blockingCount(checks) != 2 {
		t.Fatal("doctor exit gating changed")
	}
}
