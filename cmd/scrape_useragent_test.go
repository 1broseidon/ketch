package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/1broseidon/ketch/scrape"
	"github.com/spf13/cobra"
)

func cmdWithUserAgentFlag(t *testing.T, value string, set bool) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "x"}
	c.Flags().String("user-agent", "", "")
	if set {
		if err := c.Flags().Set("user-agent", value); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestUserAgentFlagOverridesConfig(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("<html><body>hi</body></html>"))
	}))
	t.Cleanup(srv.Close)

	t.Run("flag supplies override", func(t *testing.T) {
		got = ""
		s, err := newScraper(cmdWithUserAgentFlag(t, "flag-ua/1", true))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		if _, err := s.FetchContent(context.Background(), srv.URL); err != nil {
			t.Fatalf("FetchContent: %v", err)
		}
		if got != "flag-ua/1" {
			t.Fatalf("User-Agent = %q, want flag-ua/1", got)
		}
	})

	t.Run("empty flag clears config override", func(t *testing.T) {
		got = ""
		orig := cfg.UserAgent
		cfg.UserAgent = "config-ua/1"
		t.Cleanup(func() { cfg.UserAgent = orig })

		s, err := newScraper(cmdWithUserAgentFlag(t, "", true))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		if _, err := s.FetchContent(context.Background(), srv.URL); err != nil {
			t.Fatalf("FetchContent: %v", err)
		}
		if got != scrape.DefaultUserAgent() {
			t.Fatalf("User-Agent = %q, want default %q", got, scrape.DefaultUserAgent())
		}
	})
}
