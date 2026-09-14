package doctor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
)

type goldenTransport struct{}

func (goldenTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"results":[]}`))}, nil
}

func TestRegistryDoctorCompatibilityGolden(t *testing.T) {
	for _, key := range []string{"KETCH_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
		t.Setenv(key, "")
	}
	t.Setenv("PATH", t.TempDir())
	cfg := config.Defaults()
	client := &http.Client{Transport: goldenTransport{}}
	var checks []Check
	for _, s := range buildSpecs(&cfg, client) {
		if s.surface != "search" && s.surface != "code" && s.surface != "docs" {
			continue
		}
		status, detail := s.probe(context.Background())
		checks = append(checks, Check{Surface: s.surface, Backend: s.backend, Status: status, Detail: detail, Required: s.required})
	}
	data, err := json.MarshalIndent(checks, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	assertDoctorGolden(t, data)

	for _, c := range checks {
		wantRequired := c.Backend == cfg.Backend || c.Backend == cfg.CodeBackend || c.Backend == cfg.DocsBackend
		if c.Required != wantRequired {
			t.Errorf("required changed for %s", c.Backend)
		}
	}
}

func assertDoctorGolden(t *testing.T, data []byte) {
	t.Helper()
	path := filepath.Join("testdata", "providers.json")
	if os.Getenv("UPDATE_REGISTRY_GOLDENS") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) {
		t.Fatalf("doctor provider report changed\n%s", data)
	}
}
