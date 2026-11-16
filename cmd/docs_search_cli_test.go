package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/1broseidon/ketch/config"
)

// These tests run `ketch docs "<question>"` in-process through the search
// backend: a SearXNG instance (the only search provider configured) and a
// docs site, both answered by a fake transport, so nothing reaches the
// network.

const (
	searxURL = "https://searx.example.test"
	cliSite  = "https://docs.gizmo.test/docs/"
)

// searchSite is the fake web: the SearXNG JSON API answering every query
// with hits (or status, when set), the docs site's pages, and 404 for the
// rest. down fails every request, as a machine with no network.
type searchSite struct {
	hits     []map[string]string
	status   int
	down     atomic.Bool
	requests atomic.Int32
}

func (s *searchSite) RoundTrip(req *http.Request) (*http.Response, error) {
	s.requests.Add(1)
	if s.down.Load() {
		return nil, fmt.Errorf("dial tcp %s: network is unreachable", req.URL.Host)
	}
	switch {
	case "https://"+req.URL.Host == searxURL && req.URL.Path == "/search":
		if s.status != 0 {
			return fixtureResponse(req, s.status, ""), nil
		}
		b, _ := json.Marshal(map[string]any{"results": s.hits})
		return fixtureResponse(req, http.StatusOK, string(b)), nil
	case req.URL.Host == "docs.gizmo.test":
		if body, ok := gizmoPages[req.URL.Path]; ok {
			resp := fixtureResponse(req, http.StatusOK, body)
			resp.Header.Set("Content-Type", "text/html; charset=utf-8")
			return resp, nil
		}
	}
	return fixtureResponse(req, http.StatusNotFound, ""), nil
}

// gizmoPage is a docs page in HTML: its title, an H1 and sections.
func gizmoPage(title string, sections ...string) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><title>" + title + " - Gizmo docs</title></head><body><main><article><h1>" + title + "</h1>")
	b.WriteString("<p>" + title + " is part of Gizmo, a small toolkit for wiring widgets together in a service.</p>")
	for _, s := range sections {
		id, text, _ := strings.Cut(s, "|")
		b.WriteString(`<h2 id="` + id + `">` + strings.ReplaceAll(id, "-", " ") + "</h2><p>" + text + "</p>")
	}
	b.WriteString("</article></main></body></html>")
	return b.String()
}

var gizmoPages = map[string]string{
	"/docs/guide/sprockets": gizmoPage("Sprockets",
		"configure-sprockets|Set sprocket_limit in gizmo.toml to cap how many sprockets a widget opens at once; the default is eight.",
		"sprocket-events|A widget emits a sprocket event each time a sprocket opens, which a handler can log."),
	"/docs/guide/widgets": gizmoPage("Widgets",
		"create-a-widget|Create a widget with gizmo.new and give it a name; widgets open sprockets as they work.",
		"widget-lifecycle|A widget starts, opens sprockets, and stops when its service stops."),
}

func gizmoHit(path, title string) map[string]string {
	return map[string]string{"url": cliSite + path, "title": title, "content": "About " + title + "."}
}

func otherHits() []map[string]string {
	return []map[string]string{
		{"url": "https://github.com/gizmo/gizmo/issues/4", "title": "sprocket_limit ignored", "content": "An issue."},
		{"url": "https://stackoverflow.com/questions/9/gizmo-sprockets", "title": "How to cap sprockets", "content": "A question."},
	}
}

// searchEnv runs the docs command over site, SearXNG the only search
// provider.
func searchEnv(t *testing.T, site *searchSite) {
	t.Helper()
	docsEnv(t, site, func(c *config.Config) {
		c.SetProvider("searxng_url", searxURL)
		c.SearchOrder = []string{"searxng"}
	})
}

// Every way a question through the search backend ends maps to its exit
// code: any answer exits 0, the docs' text or only the search results;
// a search with no results exits 3, a search that failed 4.
func TestDocsSearchExitCodes(t *testing.T) {
	answer := append([]map[string]string{gizmoHit("guide/sprockets", "Sprockets - Gizmo docs"), gizmoHit("guide/widgets", "Widgets - Gizmo docs")}, otherHits()...)
	cases := []struct {
		name   string
		site   *searchSite
		query  string
		code   int
		stdout []string // what stdout holds, in order
		stderr string   // what the error says
	}{
		{"answer", &searchSite{hits: answer}, "sprocket_limit default", 0,
			[]string{"\nread:\n  - " + cliSite + "guide/sprockets (search rank 1)\n", "\nnot_on_pages: []\n---\n", "## Sprockets\n" + cliSite + "guide/sprockets\n", "sprocket_limit", "\n## Other results\n- sprocket_limit ignored — https://github.com/gizmo/gizmo/issues/4"}, ""},
		{"terms on no page", &searchSite{hits: answer}, "sprocket_limit xylographic", 0,
			[]string{"\nnot_on_pages: [xylographic]\n---\n"}, ""},
		{"results only", &searchSite{hits: otherHits()}, "sprocket_limit default", 0,
			[]string{"\nnote: no page was printed: ", "---\n## Other results\n- sprocket_limit ignored — https://github.com/gizmo/gizmo/issues/4 — An issue.\n", "\nunread until you read it: another result with ketch scrape <url>\n"}, ""},
		{"search empty", &searchSite{}, "sprocket_limit default", 3, nil, "no results"},
		{"search failed", &searchSite{status: http.StatusServiceUnavailable}, "sprocket_limit default", 4, nil, "search"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			searchEnv(t, tc.site)
			out, err := docsCLI(t, tc.query)
			if code := exitCode(err); code != tc.code {
				t.Fatalf("exit %d, want %d: %v\nstdout:\n%s", code, tc.code, err, out)
			}
			at := 0
			for _, want := range tc.stdout {
				i := strings.Index(out[at:], want)
				if i < 0 {
					t.Errorf("stdout lacks %q (in order):\n%s", want, out)
					break
				}
				at += i + len(want)
			}
			if tc.code != 0 && (out != "" || !strings.Contains(err.Error(), tc.stderr)) {
				t.Errorf("error %v, stdout %q", err, out)
			}
			for _, gone := range []string{"\nstatus:", "\nterms:", "\nnot_found:", "\nsource: search", "\nbackend:"} {
				if strings.Contains(out, gone) {
					t.Errorf("stdout prints %q:\n%s", gone, out)
				}
			}
		})
	}
}

// --json carries the answer's fields: the frontmatter's, the results, the
// outline, the other results and the hint; no verdict.
func TestDocsSearchJSON(t *testing.T) {
	searchEnv(t, &searchSite{hits: append([]map[string]string{gizmoHit("guide/sprockets", "Sprockets - Gizmo docs")}, otherHits()...)})
	out, err := docsCLI(t, "--json", "sprocket_limit default")
	if err != nil {
		t.Fatalf("exit %d: %v", exitCode(err), err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &top); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, k := range []string{"query", "read", "not_on_pages", "results", "other_results", "hint"} {
		if _, ok := top[k]; !ok {
			t.Errorf("no %q in %s", k, out)
		}
	}
	for _, k := range []string{"status", "reason", "terms", "not_found", "backend", "index", "not_in_index"} {
		if _, ok := top[k]; ok {
			t.Errorf("%q in %s", k, out)
		}
	}
}

// A question asked again within cache_ttl prints byte-identical output with
// the network down: no search, no page fetch.
func TestDocsSearchRepeatOffline(t *testing.T) {
	site := &searchSite{hits: append([]map[string]string{gizmoHit("guide/sprockets", "Sprockets - Gizmo docs"), gizmoHit("guide/widgets", "Widgets - Gizmo docs")}, otherHits()...)}
	searchEnv(t, site)
	for _, args := range [][]string{{"sprocket_limit default"}, {"--json", "sprocket_limit default"}} {
		site.down.Store(false)
		first, err := docsCLI(t, args...)
		if err != nil {
			t.Fatalf("%v: exit %d: %v", args, exitCode(err), err)
		}
		site.down.Store(true)
		before := site.requests.Load()
		again, err := docsCLI(t, args...)
		if err != nil || again != first {
			t.Errorf("%v: the repeat (%v) prints\n%s\nthe first printed\n%s", args, err, again, first)
		}
		if n := site.requests.Load() - before; n != 0 {
			t.Errorf("%v: the repeat made %d requests", args, n)
		}
	}
}

// A library name several libraries share is refused, exit 2, listing
// them; an offline question nothing indexed answers exits 3.
func TestDocsSearchRefusalExitCodes(t *testing.T) {
	ft := widgetFixtures(t)
	site := &searchSite{hits: otherHits()}
	docsEnv(t, fallbackTo{site: site, rest: fixtureFallback{ft}}, func(c *config.Config) {
		c.SetProvider("searxng_url", searxURL)
		c.SearchOrder = []string{"searxng"}
	})
	out, err := docsCLI(t, "--library", "widget", "configure themes")
	if code := exitCode(err); code != ExitValidation || out != "" || !strings.Contains(err.Error(), "widget") {
		t.Errorf("ambiguous library: exit %d (%v), stdout %q", code, err, out)
	}
	out, err = docsCLI(t, "--offline", "sprocket_limit default")
	if code := exitCode(err); code != ExitNotFound || out != "" {
		t.Errorf("offline miss: exit %d (%v), stdout %q", code, err, out)
	}
}

// fallbackTo answers the search and the docs site from site, the rest from
// rest.
type fallbackTo struct {
	site *searchSite
	rest http.RoundTripper
}

func (f fallbackTo) RoundTrip(req *http.Request) (*http.Response, error) {
	if "https://"+req.URL.Host == searxURL || req.URL.Host == "docs.gizmo.test" {
		return f.site.RoundTrip(req)
	}
	return f.rest.RoundTrip(req)
}
