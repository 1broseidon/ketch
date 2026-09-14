package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/1broseidon/ketch/updatecheck"
	"github.com/spf13/cobra"
)

type versionReleaseTransport struct {
	requests int
}

func (tr *versionReleaseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	tr.requests++
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v0.16.2"}`)),
		Header:     make(http.Header),
	}, nil
}

func TestVersionUpdateNotifier(t *testing.T) {
	for _, tt := range []struct {
		name     string
		asJSON   bool
		disabled bool
	}{
		{name: "text enabled"},
		{name: "json enabled", asJSON: true},
		{name: "text disabled", disabled: true},
		{name: "json disabled", asJSON: true, disabled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testutil.SetIsolatedConfigHome(t)
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("KETCH_NO_UPDATE_NOTIFIER", "0")
			if tt.disabled {
				t.Setenv("KETCH_NO_UPDATE_NOTIFIER", "1")
			}
			oldVersion, oldClient := version, http.DefaultClient
			t.Cleanup(func() { version, http.DefaultClient = oldVersion, oldClient })
			version = "0.16.1"
			transport := &versionReleaseTransport{}
			http.DefaultClient = &http.Client{Transport: transport}
			root := &cobra.Command{Use: "ketch"}
			root.PersistentFlags().Bool("json", tt.asJSON, "")
			root.AddCommand(&cobra.Command{Use: "version", RunE: versionCmd.RunE})
			root.SetArgs([]string{"version"})
			var runErr error
			output := captureStdout(t, func() { runErr = root.Execute() })
			if runErr != nil {
				t.Fatal(runErr)
			}
			wantRequests, wantSource := 1, "live"
			if tt.disabled {
				wantRequests, wantSource = 0, "none"
			}
			if transport.requests != wantRequests {
				t.Fatalf("release requests = %d, want %d", transport.requests, wantRequests)
			}
			if tt.asJSON {
				var payload struct {
					Version string             `json:"version"`
					Update  updatecheck.Status `json:"update"`
				}
				if err := json.Unmarshal([]byte(output), &payload); err != nil {
					t.Fatal(err)
				}
				if payload.Version != version || payload.Update.Source != wantSource || payload.Update.Available == tt.disabled {
					t.Fatalf("unexpected version output: %s", output)
				}
				return
			}
			if !strings.HasPrefix(output, "ketch 0.16.1\n") || strings.Contains(output, "Update available:") == tt.disabled {
				t.Fatalf("unexpected version output: %s", output)
			}
		})
	}
}
