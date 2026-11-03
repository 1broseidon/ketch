package cmd_test

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
)

// The local docs index end to end, through the built binary, the real
// scrape pipeline and page cache: each fixture site is indexed with docs
// index, then answered offline by -b local with the section the web
// backend finds for the same query. The isolated home has no embedding
// model, so the text is indexed alone and ranked by keywords, and the
// answer says so.
func TestDocsIndexFixtureSitesCLI(t *testing.T) {
	for _, name := range slices.Sorted(maps.Keys(docsFixtures)) {
		fx := docsFixtures[name]
		t.Run(name, func(t *testing.T) {
			isolated(t)
			site := newFixtureSite(t, fx.site)
			library := site.URL + fx.root

			code, out, stderr := cli(t, "docs", "index", library)
			if code != 0 || !strings.Contains(out, library) || !strings.Contains(stderr, "indexing text only") {
				t.Fatalf("docs index exited %d\nstdout: %s\nstderr: %s", code, out, stderr)
			}
			checkIndexScope(t, site, fx)

			checkLocalTop(t, site, fx, library)
			checkIndexLifecycle(t, library)
			mustCLI(t, "docs", "index", "--remove", library)
			if code, _, stderr := cli(t, "docs", "-b", "local", "--library", library, fx.query); code != 3 || !strings.Contains(stderr, "not indexed") {
				t.Errorf("after --remove: exit %d, %s", code, stderr)
			}
		})
	}
}

// checkLocalTop asserts -b local answers fx's query with fx's section,
// ranked by keywords and saying so.
func checkLocalTop(t *testing.T, site *fixtureSite, fx docsFixture, library string) {
	t.Helper()
	code, out, stderr := cli(t, "--json", "docs", "-b", "local", "--library", library, fx.query)
	if code != 0 {
		t.Fatalf("docs -b local exited %d: %s", code, stderr)
	}
	var results []docsResult
	if err := json.Unmarshal([]byte(out), &results); err != nil || len(results) == 0 {
		t.Fatalf("results %q: %v", out, err)
	}
	if top := results[0]; top.URL != site.URL+fx.page+"#"+fx.anchor || top.Source != "local" || top.Library != library {
		t.Errorf("top result = %+v, want %s#%s from local", top, fx.page, fx.anchor)
	}
	if !strings.Contains(stderr, "note: ranked by keywords: this library was indexed without an embedding model") {
		t.Errorf("stderr should say the answer was ranked by keywords: %q", stderr)
	}
}

// checkIndexLifecycle re-runs the build (nothing changed, nothing
// re-sectioned) and lists the index.
func checkIndexLifecycle(t *testing.T, library string) {
	t.Helper()
	out := mustCLI(t, "--json", "docs", "index", library)
	var report struct {
		New, Changed, Unchanged int
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil || report.New+report.Changed != 0 || report.Unchanged == 0 {
		t.Errorf("re-run report %s (%v)", out, err)
	}
	var listed []struct{ Root string }
	if err := json.Unmarshal([]byte(mustCLI(t, "--json", "docs", "index", "--list")), &listed); err != nil || len(listed) != 1 || listed[0].Root != library {
		t.Errorf("--list = %+v (%v)", listed, err)
	}
}

// checkIndexScope fails when the build fetched what the web backend must
// never fetch for the site (out-of-scope pages, the off-site server).
func checkIndexScope(t *testing.T, site *fixtureSite, fx docsFixture) {
	t.Helper()
	got := site.requested()
	for _, p := range fx.never {
		if slices.Contains(got, p) && !strings.HasSuffix(p, ".txt") && !strings.HasSuffix(p, ".xml") {
			t.Errorf("index fetched %s, out of scope; requests: %v", p, got)
		}
	}
	site.mu.Lock()
	defer site.mu.Unlock()
	if len(site.offsites) > 0 {
		t.Errorf("followed off-site links: %v", site.offsites)
	}
}
