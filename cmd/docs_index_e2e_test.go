package cmd_test

import (
	"encoding/json"
	"github.com/1broseidon/ketch/docs"
	"maps"
	"slices"
	"strings"
	"testing"
)

// The local docs index end to end, through the built binary, the real
// scrape pipeline and page cache: each fixture site is indexed with docs
// index, then answered offline by ketch docs (search, the default backend)
// with the section the query is about. The text is indexed alone, no model
// runs, and the answer is ranked by keywords over the full-text table
// without a note saying so.
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
			checkResolveAndScope(t, site, fx, library)
			checkSectionFollowUp(t, site, fx, library)
			checkIndexLifecycle(t, library)
			checkIndexFreshness(t, library, fx.query)
			mustCLI(t, "docs", "index", "--remove", library)
			if out := mustCLI(t, "--json", "docs", "index", "--list"); strings.Contains(out, library) {
				t.Errorf("after --remove the index still lists %s: %s", library, out)
			}
		})
	}
}

// checkLocalTop asserts ketch docs answers fx's query with fx's section,
// ranked by keywords (unremarked), as sections; --snippets is gone.
func checkLocalTop(t *testing.T, site *fixtureSite, fx docsFixture, library string) {
	t.Helper()
	code, out, stderr := cli(t, "--json", "docs", "--library", library, fx.query)
	if code != 0 {
		t.Fatalf("docs exited %d: %s", code, stderr)
	}
	var answer struct {
		Status string `json:"status"`
		Index  *struct {
			Kind  string `json:"kind"`
			Pages int    `json:"pages"`
			Built string `json:"built"`
		} `json:"index"`
		Terms   []struct{ Term string } `json:"terms"`
		Results []struct {
			docsResult
			Anchors   []string `json:"anchors"`
			PageTitle string   `json:"page_title"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &answer); err != nil || len(answer.Results) == 0 {
		t.Fatalf("results %q: %v", out, err)
	}
	results := answer.Results
	if !answerFieldsOK(answer.Status, answer.Index != nil && answer.Index.Kind == "full" && answer.Index.Pages > 0 && answer.Index.Built != "", len(answer.Terms), results[0].PageTitle) {
		t.Errorf("answer fields = %+v", answer)
	}
	// The best section's page prints first, its sections in page order:
	// the fixture's section is on it.
	at := slices.IndexFunc(results, func(r struct {
		docsResult
		Anchors   []string `json:"anchors"`
		PageTitle string   `json:"page_title"`
	}) bool {
		return slices.Contains(r.Anchors, fx.anchor)
	})
	if at < 0 {
		t.Fatalf("no result is the section %s#%s: %+v", fx.page, fx.anchor, results)
	}
	if top := results[at]; !strings.HasPrefix(top.URL, site.URL+fx.page) || !strings.HasPrefix(results[0].URL, site.URL+fx.page) || top.Source != "" || top.Library != library {
		t.Errorf("top result = %+v, want the section %s#%s from the index, on the page printed first", top, fx.page, fx.anchor)
	}
	if strings.Contains(stderr, "ranked by keywords") {
		t.Errorf("keyword ranking is the only ranking, not a note: %q", stderr)
	}
	checkLocalText(t, fx, library, results[0].PageTitle, results[at].URL)
}

// checkLocalText: the text output renders the answer as Markdown, the
// frontmatter, then the top result (url) under its page's title.
func checkLocalText(t *testing.T, fx docsFixture, library, pageTitle, url string) {
	t.Helper()
	code, out, stderr := cli(t, "docs", "--library", library, fx.query)
	if code != 0 || !strings.Contains(out, "\n---\n## "+pageTitle+"\n\n") || !strings.Contains(out, "\n\nsource: "+url) ||
		!strings.Contains(out, "\nstatus: found\n") || strings.Contains(out, "result_count") {
		t.Errorf("section output (exit %d):\n%s%s", code, out, stderr)
	}
	if code, _, stderr = cli(t, "docs", "--snippets", "--library", library, fx.query); code == 0 || !strings.Contains(stderr, "unknown flag") {
		t.Errorf("--snippets exited %d (%s), want an unknown flag", code, stderr)
	}
	if code, out, _ = cli(t, "docs", "--minimal", "--library", library, fx.query); code != 0 || !strings.Contains(out, url+"\t") || strings.Contains(out, "---") {
		t.Errorf("--minimal (exit %d):\n%s", code, out)
	}
	checkTermsRefusal(t, library)
}

// checkTermsRefusal: a question none of whose terms the docs hold exits 3
// with its frontmatter printed, status refused, its not_found the answer,
// and no body; --json says the same as fields.
func checkTermsRefusal(t *testing.T, library string) {
	t.Helper()
	code, out, _ := cli(t, "docs", "--library", library, "zyzzyva quokka")
	if code != 3 || !strings.Contains(out, "\nstatus: refused (terms_not_found)\nterms: {}\nnot_found: [zyzzyva, quokka]\n---\n") || !strings.HasSuffix(out, "---\n") {
		t.Errorf("refusal (exit %d):\n%s", code, out)
	}
	code, out, _ = cli(t, "--json", "docs", "--library", library, "zyzzyva quokka")
	var answer struct {
		Status   string   `json:"status"`
		NotFound []string `json:"not_found"`
		Results  []any    `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &answer); err != nil || code != 3 || answer.Status != "refused" || !slices.Equal(answer.NotFound, []string{"zyzzyva", "quokka"}) || len(answer.Results) != 0 {
		t.Errorf("--json refusal (exit %d): %s %v", code, out, err)
	}
}

// answerFieldsOK reports an answer's JSON fields as an indexed library's
// answer has them: found, its index, its terms, a page title.
func answerFieldsOK(status string, index bool, terms int, pageTitle string) bool {
	return status == "found" && index && terms > 0 && pageTitle != ""
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

// checkIndexFreshness asserts an answer from the index checks the pages
// it cites against cache_ttl: none read again while fresh; with every page
// older than the TTL (1ns), --offline prints the answer marked stale and a
// check that reads them again (from the fixture site, unchanged) does not.
func checkIndexFreshness(t *testing.T, library, query string) {
	t.Helper()
	if out := mustCLI(t, "docs", "--offline", "--library", library, query); strings.Contains(out, "\nstale: ") {
		t.Errorf("fresh pages, --offline: stale line printed:\n%s", out)
	}
	t.Setenv("KETCH_CACHE_TTL", "1ns")
	out := mustCLI(t, "docs", "--offline", "--library", library, query)
	if !strings.Contains(out, "\nstale: unverified, built ") {
		t.Errorf("pages older than the TTL, --offline: no stale line:\n%s", out)
	}
	if out := mustCLI(t, "--json", "docs", "--library", library, query); strings.Contains(out, `"stale"`) {
		t.Errorf("pages read again and unchanged: still stale: %s", out)
	}
	t.Setenv("KETCH_CACHE_TTL", "")
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

// decodeSectionResults reads the results of an answer's --json (an
// object holding them) or a section fetch's (an array).
func decodeSectionResults(t *testing.T, out string) []sectionResult {
	t.Helper()
	var rs []sectionResult
	var err error
	if strings.HasPrefix(out, "{") {
		var answer struct {
			Results []sectionResult `json:"results"`
		}
		err = json.Unmarshal([]byte(out), &answer)
		rs = answer.Results
	} else {
		err = json.Unmarshal([]byte(out), &rs)
	}
	if err != nil || len(rs) == 0 {
		t.Fatalf("results %q: %v", out, err)
	}
	return rs
}

// checkSectionFollowUp asserts the follow-up to an answer: a result
// carries the reference to its section, a cut one (in an answer
// shown as text) ends with the command that fetches it, one shown whole
// does not, and --section returns the section from the index in the shape
// of a result, by the reference or with the library; an unknown anchor
// exits 3, a malformed reference 2.
func checkSectionFollowUp(t *testing.T, site *fixtureSite, fx docsFixture, library string) {
	t.Helper()
	rs := decodeSectionResults(t, mustCLI(t, "--json", "docs", "--library", library, fx.query))
	top := rs[0]
	for _, r := range rs {
		if slices.Contains(r.Anchors, fx.anchor) {
			top = r
			break
		}
	}
	if top.Section != top.URL || !strings.Contains(top.Section, site.URL+fx.page) {
		t.Errorf("section = %q, url %q", top.Section, top.URL)
	}
	checkPointers(t, fx, library)
	checkSectionFetch(t, site, fx, library, top)
	checkSectionRefusals(t, site, fx, top)
}

// checkPointers: a small budget cuts results; the text output marks each
// cut one's source "(cut, N of M tokens)" and ends with one line saying how to read a
// cut group in full; JSON marks it and carries no pointer.
func checkPointers(t *testing.T, fx docsFixture, library string) {
	t.Helper()
	for _, tokens := range []string{"110", ""} {
		args := []string{"docs", "--library", library, fx.query}
		if tokens != "" {
			args = append(args, "--tokens", tokens)
		}
		text := mustCLI(t, args...)
		cut := false
		for _, r := range decodeSectionResults(t, mustCLI(t, append([]string{"--json"}, args...)...)) {
			cut = cut || r.Truncated
			if r.Truncated != strings.Contains(text, "source: "+r.URL+" (cut, ") || strings.Contains(r.Snippet, "full section") {
				t.Errorf("--tokens %q: truncated=%v but the text output is:\n%s", tokens, r.Truncated, text)
			}
		}
		hint := docs.AnswerHint(docs.HintCLI)
		if cut != strings.HasSuffix(text, "\n"+hint+"\n") || strings.Count(text, hint) > 1 || strings.Contains(text, "full section:") {
			t.Errorf("--tokens %q: cut=%v but the text output is:\n%s", tokens, cut, text)
		}
	}
}

// checkSectionFetch: following the reference returns the section, from the
// index alone, by the reference or with the library; a page URL its
// sections.
func checkSectionFetch(t *testing.T, site *fixtureSite, fx docsFixture, library string, top sectionResult) {
	t.Helper()
	for _, args := range [][]string{{"--section", top.Section}, {"--library", library, "--section", top.Section}} {
		raw := mustCLI(t, append([]string{"--json", "docs"}, args...)...)
		got := decodeSectionResults(t, raw)[0]
		if got.URL != top.URL || got.Section != top.Section || got.Source != "" || got.Library != library || got.Truncated || !slices.Contains(got.Anchors, fx.anchor) {
			t.Errorf("--section %v = %+v\nwant the section %s whole", args, got, top.Section)
		}
		checkSectionJSON(t, raw, library)
	}
	checkSectionText(t, top, library)
	if page := decodeSectionResults(t, mustCLI(t, "--json", "docs", "--section", site.URL+fx.page))[0]; page.URL != site.URL+fx.page || !slices.Contains(page.Anchors, fx.anchor) {
		t.Errorf("page = %+v", page)
	}
}

// checkSectionJSON: --section --json is an answer's object, with the
// frontmatter's fields of the source and no verdict, and its results
// name no backend.
func checkSectionJSON(t *testing.T, raw, library string) {
	t.Helper()
	var head struct {
		Backend, Library string
		Index            *struct{ Kind string }
		Terms            *[]any `json:"terms"`
	}
	if err := json.Unmarshal([]byte(raw), &head); err != nil || head.Backend != "search" || head.Library != library || head.Index == nil || head.Index.Kind != "full" || head.Terms != nil || strings.Contains(raw, `"source":"search"`) {
		t.Errorf("--section --json = %s (%v)\nwant backend, library and index, no terms, no per-result source", raw, err)
	}
}

// checkSectionText: --section prints an answer's lines about its source
// (no result_count, no verdict), then the section under its heading path.
func checkSectionText(t *testing.T, top sectionResult, library string) {
	t.Helper()
	section := decodeSectionResults(t, mustCLI(t, "--json", "docs", "--section", top.Section))[0]
	out := mustCLI(t, "docs", "--section", top.Section)
	if !strings.Contains(out, "### "+section.Breadcrumb+"\n\n"+section.Snippet) || !strings.Contains(out, "\n\nsource: "+top.URL+"\n") || strings.Contains(out, "query:") {
		t.Errorf("--section text output:\n%s", out)
	}
	if !strings.HasPrefix(out, "---\nbackend: search\nlibrary: "+library+"\nindex: full, ") || strings.Contains(out, "result_count") || strings.Contains(out, "status:") {
		t.Errorf("--section frontmatter, want an answer's source lines and no result_count:\n%s", out)
	}
}

// checkSectionRefusals: an unknown anchor or page exits 3, a malformed
// reference or another backend 2.
func checkSectionRefusals(t *testing.T, site *fixtureSite, fx docsFixture, top sectionResult) {
	t.Helper()
	if code, _, stderr := cli(t, "docs", "--section", site.URL+fx.page+"#no-such-anchor"); code != 3 || !strings.Contains(stderr, "no section #no-such-anchor") {
		t.Errorf("unknown anchor: exit %d, %s", code, stderr)
	}
	if code, _, stderr := cli(t, "docs", "--section", site.URL+"/no/such/page#x"); code != 3 {
		t.Errorf("unknown page: exit %d, %s", code, stderr)
	}
	if code, _, _ := cli(t, "docs", "--section", "not-a-url"); code != 2 {
		t.Errorf("malformed reference: exit %d", code)
	}
	if code, _, _ := cli(t, "docs", "-b", "context7", "--section", top.Section); code != 2 {
		t.Errorf("--section on the context7 backend: exit %d", code)
	}
	checkSectionPages(t, top)
}

// checkSectionPages: a section longer than --tokens is read a page at a
// time, the header saying which and the last line the next page's
// command, which reads it; a page past the last, --page 0 and --page
// without --section exit 2.
func checkSectionPages(t *testing.T, top sectionResult) {
	t.Helper()
	out := mustCLI(t, "docs", "--section", top.Section, "--tokens", "60")
	if !strings.Contains(out, "\npage: 1 of ") {
		t.Fatalf("--tokens 60:\n%s", out)
	}
	next := "ketch docs --section " + top.Section + " --tokens 60 --page 2"
	if !strings.HasSuffix(out, "\nnext page: "+next+"\n") {
		t.Errorf("last line is not the next page's command:\n%s", out)
	}
	if two := mustCLI(t, "docs", "--section", top.Section, "--tokens", "60", "--page", "2"); !strings.Contains(two, "\npage: 2 of ") || two == out {
		t.Errorf("--page 2:\n%s", two)
	}
	for _, args := range [][]string{
		{"docs", "--section", top.Section, "--tokens", "60", "--page", "999"},
		{"docs", "--section", top.Section, "--page", "0"},
		{"docs", "--page", "2", "forms"},
	} {
		if code, _, stderr := cli(t, args...); code != 2 {
			t.Errorf("%v: exit %d, %s", args, code, stderr)
		}
	}
}

// checkResolveAndScope: --resolve with no name lists the indexed library
// from the index, marked indexed; a page URL as --library answers from
// that page, printing the root as library and the page as scope; a URL
// under the root that no indexed page is under is refused, exit 3.
func checkResolveAndScope(t *testing.T, site *fixtureSite, fx docsFixture, library string) {
	t.Helper()
	for _, args := range [][]string{{"docs", "--resolve"}, {"docs", "--resolve", ""}} {
		if out := mustCLI(t, args...); !strings.HasPrefix(out, library+"  ") || !strings.Contains(out, "(indexed: yes, full, ") {
			t.Errorf("%v:\n%s", args, out)
		}
	}
	page := site.URL + fx.page
	out := mustCLI(t, "docs", "--library", page, fx.query)
	if !strings.Contains(out, "\nlibrary: "+library+"\nscope: "+page+"\n") || strings.Contains(out, "source: "+site.URL+"/elsewhere") {
		t.Errorf("--library %s:\n%s", page, out)
	}
	if code, out, _ := cli(t, "docs", "--library", library+"no/such/page", fx.query); code != 3 || !strings.Contains(out, "status: refused (scope_empty)") {
		t.Errorf("empty scope: exit %d\n%s", code, out)
	}
}
