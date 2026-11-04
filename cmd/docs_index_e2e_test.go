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
// backend finds for the same query. Hybrid ranking is off by default,
// so the text is indexed alone, no model runs, and the answer is ranked
// by keywords over the full-text table without a note saying so.
func TestDocsIndexFixtureSitesCLI(t *testing.T) {
	for _, name := range slices.Sorted(maps.Keys(docsFixtures)) {
		fx := docsFixtures[name]
		t.Run(name, func(t *testing.T) {
			isolated(t)
			site := newFixtureSite(t, fx.site)
			library := site.URL + fx.root

			code, out, stderr := cli(t, "docs", "index", library)
			if code != 0 || !strings.Contains(out, library) || strings.Contains(stderr, "embed") || strings.Contains(out, "vectors") {
				t.Fatalf("docs index exited %d\nstdout: %s\nstderr: %s", code, out, stderr)
			}
			checkIndexScope(t, site, fx)

			checkLocalTop(t, site, fx, library)
			checkSectionFollowUp(t, site, fx, library)
			checkIndexLifecycle(t, library)
			mustCLI(t, "docs", "index", "--remove", library)
			if code, _, stderr := cli(t, "docs", "-b", "local", "--library", library, fx.query); code != 3 || !strings.Contains(stderr, "not indexed") {
				t.Errorf("after --remove: exit %d, %s", code, stderr)
			}
		})
	}
}

// checkLocalTop asserts -b local answers fx's query with fx's section,
// ranked by keywords (the default, unremarked), as sections by default and as
// code-first snippets with --snippets.
func checkLocalTop(t *testing.T, site *fixtureSite, fx docsFixture, library string) {
	t.Helper()
	code, out, stderr := cli(t, "--json", "docs", "-b", "local", "--library", library, fx.query)
	if code != 0 {
		t.Fatalf("docs -b local exited %d: %s", code, stderr)
	}
	var results []struct {
		docsResult
		Anchors []string `json:"anchors"`
	}
	if err := json.Unmarshal([]byte(out), &results); err != nil || len(results) == 0 {
		t.Fatalf("results %q: %v", out, err)
	}
	if top := results[0]; !strings.HasPrefix(top.URL, site.URL+fx.page) || !slices.Contains(top.Anchors, fx.anchor) || top.Source != "local" || top.Library != library {
		t.Errorf("top result = %+v, want the section %s#%s from local", top, fx.page, fx.anchor)
	}
	if strings.Contains(stderr, "ranked by keywords") {
		t.Errorf("keyword ranking is the default, not a note: %q", stderr)
	}
	// The text output renders results as Markdown, sections and snippets
	// alike.
	top := results[0]
	code, out, stderr = cli(t, "docs", "-b", "local", "--library", library, fx.query)
	if code != 0 || !strings.Contains(out, "### "+top.Breadcrumb+"\n\n") || !strings.Contains(out, "\n\nsource: "+top.URL+"\n") {
		t.Errorf("section output (exit %d):\n%s%s", code, out, stderr)
	}
	code, out, _ = cli(t, "docs", "-b", "local", "--snippets", "--library", library, fx.query)
	if code != 0 || !strings.Contains(out, "\n\nsource: "+site.URL+fx.page) {
		t.Errorf("--snippets output (exit %d):\n%s", code, out)
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

// sectionResult is a local result with the follow-up fields.
type sectionResult struct {
	docsResult
	Section   string   `json:"section"`
	Truncated bool     `json:"truncated"`
	Anchors   []string `json:"anchors"`
}

func decodeSectionResults(t *testing.T, out string) []sectionResult {
	t.Helper()
	var rs []sectionResult
	if err := json.Unmarshal([]byte(out), &rs); err != nil || len(rs) == 0 {
		t.Fatalf("results %q: %v", out, err)
	}
	return rs
}

// checkSectionFollowUp asserts the follow-up to a compact answer: a result
// carries the reference to its section, a cut one (in a compact answer
// shown as text) ends with the command that fetches it, one shown whole
// does not, and --section returns the section from the index in the shape
// of a result, by the reference or with the library; an unknown anchor
// exits 3, a malformed reference 2.
func checkSectionFollowUp(t *testing.T, site *fixtureSite, fx docsFixture, library string) {
	t.Helper()
	top := decodeSectionResults(t, mustCLI(t, "--json", "docs", "-b", "local", "--library", library, fx.query))[0]
	if top.Section != top.URL || !strings.Contains(top.Section, site.URL+fx.page) {
		t.Errorf("section = %q, url %q", top.Section, top.URL)
	}
	checkPointers(t, fx, library)
	checkSectionFetch(t, site, fx, library, top)
	checkSectionRefusals(t, site, fx, top)
}

// checkPointers: a small budget cuts results; the text output points to
// each cut one's full section, JSON marks it and carries no pointer.
func checkPointers(t *testing.T, fx docsFixture, library string) {
	t.Helper()
	text := mustCLI(t, "docs", "-b", "local", "--library", library, "--tokens", "110", fx.query)
	for _, r := range decodeSectionResults(t, mustCLI(t, "--json", "docs", "-b", "local", "--library", library, "--tokens", "110", fx.query)) {
		hint := "source: " + r.URL + "\nfull section: ketch docs -b local --section " + r.Section + "\n"
		if r.Truncated != strings.Contains(text, hint) || strings.Contains(r.Snippet, "full section") {
			t.Errorf("truncated=%v but the text output is:\n%s", r.Truncated, text)
		}
	}
	if whole := mustCLI(t, "docs", "-b", "local", "--library", library, fx.query); strings.Contains(whole, "full section") {
		t.Errorf("the default view points to a section:\n%s", whole)
	}
}

// checkSectionFetch: following the reference returns the section, from the
// index alone, by the reference or with the library; a page URL its
// sections.
func checkSectionFetch(t *testing.T, site *fixtureSite, fx docsFixture, library string, top sectionResult) {
	t.Helper()
	for _, args := range [][]string{{"--section", top.Section}, {"--library", library, "--section", top.Section}} {
		got := decodeSectionResults(t, mustCLI(t, append([]string{"--json", "docs", "-b", "local"}, args...)...))[0]
		if got.URL != top.URL || got.Section != top.Section || got.Source != "local" || got.Library != library || got.Truncated || !slices.Contains(got.Anchors, fx.anchor) {
			t.Errorf("--section %v = %+v\nwant the section %s whole", args, got, top.Section)
		}
	}
	section := decodeSectionResults(t, mustCLI(t, "--json", "docs", "-b", "local", "--section", top.Section))[0]
	out := mustCLI(t, "docs", "-b", "local", "--section", top.Section)
	if !strings.Contains(out, "### "+section.Breadcrumb+"\n\n"+section.Snippet) || !strings.Contains(out, "\n\nsource: "+top.URL+"\n") || strings.Contains(out, "query:") {
		t.Errorf("--section text output:\n%s", out)
	}
	if page := decodeSectionResults(t, mustCLI(t, "--json", "docs", "-b", "local", "--section", site.URL+fx.page))[0]; page.URL != site.URL+fx.page || !slices.Contains(page.Anchors, fx.anchor) {
		t.Errorf("page = %+v", page)
	}
}

// checkSectionRefusals: an unknown anchor or page exits 3, a malformed
// reference or another backend 2.
func checkSectionRefusals(t *testing.T, site *fixtureSite, fx docsFixture, top sectionResult) {
	t.Helper()
	if code, _, stderr := cli(t, "docs", "-b", "local", "--section", site.URL+fx.page+"#no-such-anchor"); code != 3 || !strings.Contains(stderr, "no section #no-such-anchor") {
		t.Errorf("unknown anchor: exit %d, %s", code, stderr)
	}
	if code, _, stderr := cli(t, "docs", "-b", "local", "--section", site.URL+"/no/such/page#x"); code != 3 {
		t.Errorf("unknown page: exit %d, %s", code, stderr)
	}
	if code, _, _ := cli(t, "docs", "-b", "local", "--section", "not-a-url"); code != 2 {
		t.Errorf("malformed reference: exit %d", code)
	}
	if code, _, _ := cli(t, "docs", "-b", "web", "--section", top.Section); code != 2 {
		t.Errorf("--section on the web backend: exit %d", code)
	}
}
