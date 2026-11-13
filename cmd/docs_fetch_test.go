package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/1broseidon/ketch/scrape"
	"github.com/spf13/cobra"
)

// context7Fake answers the two Context7 endpoints the backend calls, in place
// of the network, for every client sharing httpx.Default's transport.
type context7Fake struct{}

func (context7Fake) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "context7.com" {
		return nil, fmt.Errorf("unexpected request to %s", r.URL)
	}
	body := ""
	switch r.URL.Path {
	case "/api/v1/search":
		body = `{"results":[{"id":"/facebook/react","title":"React","totalSnippets":10,"trustScore":9}]}`
	case "/api/v2/context":
		body = `{"infoSnippets":[{"pageId":"https://react.dev/reference/react/useState","breadcrumb":"Hooks > useState","content":"useState adds a state variable to a component."}]}`
	default:
		return nil, fmt.Errorf("unexpected Context7 path %s", r.URL.Path)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}, nil
}

// A Context7 run answers from the API and must never construct the scraper
// — not on the search path, not under --resolve, not under --library.
func TestDocsContext7RunBuildsNoScraper(t *testing.T) {
	testutil.SetIsolatedConfigHome(t)
	client := httpx.Default()
	prevTransport := client.Transport
	client.Transport = context7Fake{}
	t.Cleanup(func() { client.Transport = prevTransport })

	prevSettings := cfg.ProviderSettings
	cfg.SetProvider("context7_api_key", "test-key")
	t.Cleanup(func() { cfg.ProviderSettings = prevSettings })
	setDocsBackend(t, "context7")
	prevCtx := docsCmd.Context() // nil unless Execute ran: runDocs needs one
	docsCmd.SetContext(context.Background())
	t.Cleanup(func() { docsCmd.SetContext(prevCtx) })

	var built atomic.Int32
	prevScraper := newDocsScraper
	newDocsScraper = func(cmd *cobra.Command) (*scrape.Scraper, error) {
		built.Add(1)
		return prevScraper(cmd)
	}
	t.Cleanup(func() { newDocsScraper = prevScraper })

	cases := []struct {
		name  string
		flags map[string]string
		want  string
	}{
		{"search", nil, "useState adds a state variable"},
		{"resolve", map[string]string{"resolve": "true"}, "/facebook/react"},
		{"library", map[string]string{"library": "/facebook/react"}, "useState adds a state variable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for name, value := range tc.flags {
				if err := docsCmd.Flags().Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				_ = docsCmd.Flags().Set("resolve", "false")
				_ = docsCmd.Flags().Set("library", "")
			})
			var runErr error
			out := captureStdout(t, func() { runErr = runDocs(docsCmd, []string{"react"}) })
			if runErr != nil {
				t.Fatalf("runDocs: %v", runErr)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("output = %q, want it to contain %q", out, tc.want)
			}
			if n := built.Load(); n != 0 {
				t.Fatalf("a Context7 run built the scraper %d times, want 0", n)
			}
		})
	}
}

// countingFetcher returns a docsFetcher whose scraper constructor counts its
// calls and hands back the scrapers it built, so tests can check them closed.
func countingFetcher(t *testing.T, fail error) (*docsFetcher, *atomic.Int32) {
	t.Helper()
	testutil.SetIsolatedConfigHome(t) // the first fetch opens the page cache
	var built atomic.Int32
	return &docsFetcher{newScraper: func() (*scrape.Scraper, error) {
		built.Add(1)
		if fail != nil {
			return nil, fail
		}
		return scrape.New(), nil
	}}, &built
}

func TestDocsFetcherBuildsOnFirstFetchOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("# llms.txt"))
	}))
	t.Cleanup(srv.Close)

	t.Run("close without a fetch builds nothing", func(t *testing.T) {
		f, built := countingFetcher(t, nil)
		if err := f.classify(nil, "docs search failed"); err != nil {
			t.Fatalf("classify of a clean run = %v, want nil", err)
		}
		f.Close()
		if n := built.Load(); n != 0 {
			t.Fatalf("built %d scrapers without a fetch, want 0", n)
		}
	})

	t.Run("concurrent first fetches share one scraper", func(t *testing.T) {
		f, built := countingFetcher(t, nil)
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Go(func() {
				raw, err := f.FetchRaw(context.Background(), srv.URL+"/llms.txt")
				if err == nil && string(raw.Body) != "# llms.txt" {
					err = fmt.Errorf("body = %q", raw.Body)
				}
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if n := built.Load(); n != 1 {
			t.Fatalf("built %d scrapers for 8 fetches, want 1", n)
		}

		f.Close()
		if _, err := f.FetchPage(context.Background(), srv.URL); !errors.Is(err, errDocsFetcherClosed) {
			t.Fatalf("fetch after Close = %v, want errDocsFetcherClosed", err)
		}
		if n := built.Load(); n != 1 {
			t.Fatalf("a fetch after Close rebuilt the scraper (%d builds)", n)
		}
	})
}

// A scraper that cannot be built is a precondition failure (exit 5) whatever
// the backend made of the fetch error: wrapped, reclassified or swallowed.
func TestDocsFetcherBuildFailureIsPrecondition(t *testing.T) {
	buildErr := &ExitError{Code: ExitPrecondition, Err: errors.New("invalid url_rewrites: bad regex")}
	f, built := countingFetcher(t, buildErr)
	defer f.Close()

	_, fetchErr := f.FetchRaw(context.Background(), "https://docs.example.test/llms.txt")
	if !errors.Is(fetchErr, buildErr) {
		t.Fatalf("FetchRaw = %v, want the construction error", fetchErr)
	}
	if _, err := f.FetchPage(context.Background(), "https://docs.example.test/"); !errors.Is(err, buildErr) {
		t.Fatalf("FetchPage = %v, want the same construction error", err)
	}
	if n := built.Load(); n != 1 {
		t.Fatalf("construction attempted %d times, want 1 (the failure is sticky)", n)
	}

	for name, backendErr := range map[string]error{
		"wrapped":      fmt.Errorf("web: fetch llms.txt: %w", fetchErr),
		"reclassified": fmt.Errorf("web: no docs index %w", docs.ErrNotFound),
		"swallowed":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			got := asExitError(t, f.classify(backendErr, "docs search failed"))
			if got.Code != ExitPrecondition {
				t.Fatalf("exit code = %d, want %d (precondition): %v", got.Code, ExitPrecondition, got)
			}
		})
	}
}

// A docs run that reads several JS-rendered pages says so once, on
// stderr, with a count, when it ends; nothing reaches stdout. A single
// warning keeps the scraper's own words.
func TestDocsFetcherFoldsPageWarnings(t *testing.T) {
	shell := `<!doctype html><html><head><title>App</title><script id="__NEXT_DATA__" type="application/json">{"buildId":"dev","page":"/","props":{"chunks":["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]}}</script></head><body><div id="root"></div></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(shell))
	}))
	t.Cleanup(srv.Close)
	for _, n := range []int{3, 1} {
		f, _ := countingFetcher(t, nil)
		var stderr bytes.Buffer
		f.stderr = &stderr
		for i := range n {
			_, _ = f.FetchPage(context.Background(), fmt.Sprintf("%s/page%d", srv.URL, i))
		}
		f.Close()
		want := fmt.Sprintf("warn: %d pages appear JS-rendered (first: %s/page0); configure browser for full content\n", n, srv.URL)
		if n == 1 {
			want = fmt.Sprintf("warn: %s/page0 appears JS-rendered; configure browser for full content\n", srv.URL)
		}
		if stderr.String() != want {
			t.Errorf("%d pages: stderr %q, want %q", n, stderr.String(), want)
		}
	}
}

// The folded line counts each kind of warning apart, in the order first
// seen.
func TestPageWarningsFlush(t *testing.T) {
	var w pageWarnings
	boom := errors.New("boom")
	for _, pw := range []scrape.PageWarning{
		{Kind: scrape.WarnBrowserFailed, URL: "https://a.test/1", Err: boom},
		{Kind: scrape.WarnJSRendered, URL: "https://a.test/2"},
		{Kind: scrape.WarnBrowserFailed, URL: "https://a.test/3", Err: boom},
	} {
		w.add(pw)
	}
	var out bytes.Buffer
	w.flush(&out)
	want := "warn: browser fallback failed for 2 pages (first: https://a.test/1: boom)\nwarn: https://a.test/2 appears JS-rendered; configure browser for full content\n"
	if out.String() != want {
		t.Errorf("flush:\n%s\nwant:\n%s", out.String(), want)
	}
	out.Reset()
	w.flush(&out)
	if out.Len() != 0 {
		t.Errorf("a second flush printed %q", out.String())
	}
}
