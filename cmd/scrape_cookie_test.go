package cmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// jarFile writes a cookies.txt and returns its path.
func jarFile(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// cmdWithCookieFlag returns a throwaway command exposing the cookie-file flag,
// optionally pre-set to value.
func cmdWithCookieFlag(t *testing.T, value string, set bool) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "x"}
	c.Flags().String("cookie-file", "", "")
	if set {
		if err := c.Flags().Set("cookie-file", value); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestCookieFileFlagOverridesConfig(t *testing.T) {
	jar := jarFile(t, "127.0.0.1\tFALSE\t/\tFALSE\t0\tsession\tflagsecret")

	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte("<html><body>hi</body></html>"))
	}))
	defer srv.Close()

	t.Run("flag supplies jar", func(t *testing.T) {
		gotCookie = ""
		s, err := newScraper(cmdWithCookieFlag(t, jar, true))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if _, err := s.CachedScrape(context.Background(), nil, srv.URL); err != nil {
			t.Fatalf("CachedScrape: %v", err)
		}
		if gotCookie != "session=flagsecret" {
			t.Fatalf("Cookie = %q, want session=flagsecret", gotCookie)
		}
	})

	t.Run("empty flag overrides config and disables cookies", func(t *testing.T) {
		gotCookie = ""
		orig := cfg.CookieFile
		cfg.CookieFile = jar
		defer func() { cfg.CookieFile = orig }()

		s, err := newScraper(cmdWithCookieFlag(t, "", true))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if _, err := s.CachedScrape(context.Background(), nil, srv.URL); err != nil {
			t.Fatalf("CachedScrape: %v", err)
		}
		if gotCookie != "" {
			t.Fatalf("Cookie = %q, want empty (flag override disables)", gotCookie)
		}
	})
}

func TestBackgroundCrawlValidatesCookieFileBeforeDetach(t *testing.T) {
	err := validateBackgroundCrawl(cmdWithCookieFlag(t, filepath.Join(t.TempDir(), "missing.txt"), true), "https://example.com")
	if err == nil {
		t.Fatal("expected invalid cookie file to fail parent validation")
	}
	if !strings.Contains(err.Error(), "invalid cookie_file") {
		t.Fatalf("error = %q, want clear cookie_file error", err)
	}
}

func TestScrapeOutputNeverContainsCookieValue(t *testing.T) {
	const secret = "sekrit-value-xyz"
	jar := jarFile(t, "127.0.0.1\tFALSE\t/\tFALSE\t0\tsession\t"+secret)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><head><title>t</title></head><body><p>body text</p></body></html>"))
	}))
	defer srv.Close()

	s, err := newScraper(cmdWithCookieFlag(t, jar, true))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	page, err := s.CachedScrape(context.Background(), nil, srv.URL)
	if err != nil {
		t.Fatalf("CachedScrape: %v", err)
	}
	out := captureStdout(t, func() { printPage(page) })
	if strings.Contains(out, secret) {
		t.Fatal("scrape output leaked a cookie value")
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = writer
	fn()
	os.Stdout = orig
	_ = writer.Close()
	data, _ := io.ReadAll(reader)
	_ = reader.Close()
	return string(data)
}
