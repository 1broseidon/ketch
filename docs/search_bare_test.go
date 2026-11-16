package docs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/search"
)

// A question that names no library (search_bare.go) reads the plain
// question's search in its order: the first result that is a readable
// document, then up to two more of its site and release.

// The first result is read whatever its site looks like: no docs sign, no
// vote among the sites, no name resolved from the question. The read line
// shows its search rank; the other site's hit is listed, not read.
func TestBareReadsTheSearchOrder(t *testing.T) {
	f := exampleSite()
	f.pages["https://tips.test/articles/middleware"] = fakePage{title: "Middleware tips", markdown: "# Middleware tips\n\n## Add middleware to a route\n\nCall app.use with the route's path before its handler.\n"}
	hits := []search.Result{
		hit("https://tips.test/articles/middleware", "Middleware tips"),
		hit(idxRoot+"guide/middleware", "Middleware - Example docs"),
		hit(idxRoot+"guide/routing", "Routing - Example docs"),
	}
	s, _, _ := searchFirstOver(t, f, hits)
	resolved := 0
	s.resolveName = func(context.Context, sfRequest) sfResolved { resolved++; return sfResolved{} }
	o := s.SearchSelect(context.Background(), "example add middleware to a route", 0)
	if o.Err() != nil || !slices.Equal(o.Read, []PageRead{{URL: "https://tips.test/articles/middleware", Rank: 1}}) {
		t.Fatalf("read %+v (%v), want the first result alone", o.Read, o.Err())
	}
	if resolved != 0 {
		t.Errorf("resolve ran %d times for a question that names no library", resolved)
	}
	neverRead(t, f, "docs.example.test")
	if len(o.OtherResults) != 2 || o.OtherResults[0].Rank != 2 {
		t.Errorf("other results %+v, want the docs hits listed in rank order", o.OtherResults)
	}
	if o.Origin != "" {
		t.Errorf("origin %q: an answer to a question that names no library has none", o.Origin)
	}
}

// Results that are no document by their address (an issue, a Q&A thread,
// a source file on a forge) are passed over without a fetch; a Markdown
// document on a forge is read.
func TestBareSkipsNonDocuments(t *testing.T) {
	readme := "https://github.com/example/example/blob/main/docs/middleware.md"
	f := exampleSite()
	f.raw[readme] = md("# Middleware\n\n## Add middleware to a route\n\nRegister it with app.use before the handler.\n")
	hits := []search.Result{
		hit("https://github.com/example/example/issues/12", "middleware order issue"),
		hit("https://stackoverflow.com/questions/1/example-middleware", "How to add middleware"),
		hit("https://github.com/example/example/blob/main/src/middleware.ts", "middleware.ts"),
		hit(readme, "docs/middleware.md"),
		hit(idxRoot+"guide/middleware", "Middleware - Example docs"),
	}
	s, _, _ := searchFirstOver(t, f, hits)
	o := s.SearchSelect(context.Background(), "add middleware to a route", 0)
	if o.Err() != nil || len(o.Read) != 1 || o.Read[0].Rank != 4 {
		t.Fatalf("read %+v (%v), want the Markdown document at rank 4", o.Read, o.Err())
	}
	assertNeverFetched(t, f, "issues/12", "stackoverflow", "middleware.ts")
	if len(o.Skipped) != 0 {
		t.Errorf("skipped %+v: a result that is no document is not a failed read", o.Skipped)
	}
}

func TestNotDocument(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://stackoverflow.com/questions/1/x":                        true,
		"https://unix.stackexchange.com/questions/2/y":                   true,
		"https://github.com/o/r/issues/3":                                true,
		"https://github.com/o/r/pull/4":                                  true,
		"https://github.com/o/r/commit/abc":                              true,
		"https://gitlab.com/o/r/-/merge_requests/5":                      true,
		"https://github.com/o/r/blob/main/core/src/Topic.scala":          true,
		"https://raw.githubusercontent.com/o/r/main/x.go":                true,
		"https://docs.rs/serde/1.0.0/src/serde/de.rs.html":               true,
		"https://forum.example.test/t/some-topic/123":                    true,
		"https://discuss.example.test/x":                                 true,
		"https://www.youtube.com/watch?v=x":                              true,
		"https://www.reddit.com/r/golang/comments/x/y/":                  true,
		"https://x.com/someone/status/1":                                 true,
		"https://github.com/MicrosoftDocs/azure-docs/blob/main/a/b.md":   false,
		"https://github.com/o/r":                                         false,
		"https://github.com/o/r/wiki/Setup":                              false,
		"https://gist.github.com/o/abc":                                  false,
		"https://daily.dev/posts/operators-of-death":                     false,
		"https://example.test/blog/2024/05/tips":                         false,
		"https://docs.sqlalchemy.org/en/20/_modules/examples/a/b.html":   false,
		"https://www.php.net/manual/en/function.array-map.php":           false,
		"https://www.rfc-editor.org/rfc/rfc9110.txt":                     false,
		"https://docs.python.org/3.10/library/datetime.html":             false,
		"https://learn.microsoft.com/en-us/answers/questions/1/how-to-x": true,
		// A forge's files are documents by type only.
		"https://github.com/envoyproxy/envoy/blob/main/api/envoy/config/cluster/v3/circuit_breaker.proto": true,
		"https://raw.githubusercontent.com/ansible/ansible/stable-2.11/examples/ansible.cfg":              true,
		"https://github.com/o/r/blob/main/Makefile":                                                       true,
		"https://gitlab.com/o/r/-/blob/main/config/default.ini":                                           true,
		"https://github.com/o/r/blob/main/docs/guide.rst":                                                 false,
		"https://github.com/o/r/blob/main/docs/manual.adoc":                                               false,
		"https://raw.githubusercontent.com/o/r/main/NOTES.txt":                                            false,
		"https://github.com/o/r/blob/main/README":                                                         false,
		"https://github.com/o/r/tree/main/docs":                                                           false,
		"https://go.googlesource.com/wiki/+show/abc/Go123Timer":                                           false,
		"https://github.com/open-telemetry/opentelemetry-collector/blob/main/processor/README.md":         false,
	} {
		u, _ := url.Parse(raw)
		if got := notDocument(u); got != want {
			t.Errorf("notDocument(%s) = %t, want %t", raw, got, want)
		}
	}
}

// A result that cannot be read is skipped with its cause and the next
// result read, on any site; when that is the same page of another release
// the note says so.
func TestBareFallsThroughAndNamesTheRelease(t *testing.T) {
	const host = "https://lang.example.test/"
	f := &fakeFetcher{pages: map[string]fakePage{
		host + "3/library/datetime.html": {title: "datetime", markdown: "# datetime\n\n## fromisoformat\n\nParse an ISO 8601 string with fromisoformat.\n"},
	}}
	hits := []search.Result{
		hit(host+"3.10/library/datetime.html", "datetime 3.10"),
		hit(host+"3/library/datetime.html", "datetime"),
	}
	s, _, _ := searchFirstOver(t, f, hits)
	o := s.SearchSelect(context.Background(), "fromisoformat ISO 8601", 0)
	if o.Err() != nil || len(o.Read) != 1 || o.Read[0].Rank != 2 {
		t.Fatalf("read %+v (%v), want the second result", o.Read, o.Err())
	}
	if len(o.Skipped) != 1 || !strings.Contains(o.Skipped[0].Cause, "not found") {
		t.Errorf("skipped %+v, want the first result with its cause", o.Skipped)
	}
	want := host + "3.10/library/datetime.html was skipped (" + o.Skipped[0].Cause + "); " + host + "3/library/datetime.html, read instead, is the same page of another release"
	if !strings.Contains(o.Note, want) {
		t.Errorf("note %q, want %q", o.Note, want)
	}
}

// robots.txt is not consulted for a page the search returned, read at its
// exact address (as ketch scrape reads it), for a question or a library;
// a page a widening finds by itself is still refused by it.
func TestRobotsSparesOnlySearchResults(t *testing.T) {
	secret := idxRoot + "private/secret"
	f := exampleSite()
	s, _, _ := searchFirstOver(t, f, []search.Result{hit(secret, "Secret")})
	if o := s.SearchSelect(context.Background(), "not for robots", 0); len(o.Read) != 1 || o.Read[0].URL != secret {
		t.Errorf("bare question: read %+v skipped %+v (%v), want the disallowed result read", o.Read, o.Skipped, o.Err())
	}
	f = exampleSite()
	s, _, _ = searchFirstOver(t, f, []search.Result{hit(secret, "Secret"), hit(idxRoot+"guide/routing", "Routing")})
	if o := s.GetDocsSelect(context.Background(), idxRoot, "not for robots", 0); !slices.ContainsFunc(o.Read, func(p PageRead) bool { return p.URL == secret }) {
		t.Errorf("library: read %+v skipped %+v (%v), want the disallowed hit read", o.Read, o.Skipped, o.Err())
	}

	// A widening for an identifier no exact page holds reads the site's
	// own lists, and never the page robots.txt closes.
	f = exampleSite()
	f.pages[secret] = fakePage{title: "Secret", markdown: "# Secret\n\n## secret_key\n\nSet secret_key here.\n"}
	s, _, _ = searchFirstOver(t, f, []search.Result{hit(idxRoot+"guide/routing", "Routing")})
	o := s.GetDocsSelect(context.Background(), idxRoot, "define routes secret_key", 0)
	if o.Widened == nil {
		t.Fatalf("widened %+v (%v), want a widening", o.Widened, o.Err())
	}
	assertNeverFetched(t, f, "private/secret")
}

// A host a result's text cut short (docs.python.or) is no result; a
// private registry's suffix (github.io) and a test domain are.
func TestPlainHitsWellFormed(t *testing.T) {
	hits := plainHits([]search.Result{
		hit("https://docs.python.or/", "truncated"),
		hit("https://user.github.io/project/", "pages"),
		hit("https://docs.example.test/x", "test"),
		hit("https://docs.example.test/x#frag", "duplicate"),
		hit("https://docs.python.org/3/", "python"),
	}, 0)
	var got []string
	for _, h := range hits {
		got = append(got, fmt.Sprintf("%d %s", h.rank, h.u))
	}
	want := []string{"2 https://user.github.io/project/", "3 https://docs.example.test/x", "5 https://docs.python.org/3/"}
	if !slices.Equal(got, want) {
		t.Errorf("hits %v, want %v (ranks as the search gave them)", got, want)
	}
}

// A read that failed says why in words a caller can act on.
func TestReadCauseNames(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("fetch failed: %w", err) }
	for _, tc := range []struct {
		err  error
		want string
	}{
		{wrap(&net.DNSError{Err: "no such host", Name: "docs.webpack.js.org", IsNotFound: true}), readDNS},
		{wrap(&net.OpError{Op: "dial", Err: errors.New("connection refused")}), readRefused},
		{wrap(&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}), readTLS},
		{wrap(context.DeadlineExceeded), readTimeout},
	} {
		if got, _ := readCause(tc.err); got != tc.want {
			t.Errorf("readCause(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
	if cause, detail := readCause(errors.New("unexpected EOF")); cause != readOther || detail != "unexpected EOF" {
		t.Errorf("an unknown failure: %q, %q; want its message as the detail", cause, detail)
	}
}

// bySearch is a search backend answering each query from results, and the
// queries it was asked, in order.
func bySearch(s *SearchFirst, results map[string][]search.Result) *[]string {
	var asked []string
	s.web.sites = siteSearchFunc(func(_ context.Context, q string, _ int) ([]search.Result, error) {
		asked = append(asked, q)
		return results[q], nil
	})
	return &asked
}

// When the question's search gives nothing to read (no results, or none
// that is a document that reads), ONE more search runs, the question +
// "documentation", read by the same rules; the note says so, and so does a
// repeat within cache_ttl, which searches nothing.
func TestBareSearchesOnceMore(t *testing.T) {
	const q = "add middleware to a route"
	docs := []search.Result{hit(idxRoot+"guide/middleware", "Middleware - Example docs"), hit("https://tips.test/articles/middleware", "Middleware tips")}
	threads := []search.Result{hit("https://stackoverflow.com/questions/1/example-middleware", "How to add middleware")}
	for _, tc := range []struct {
		name  string
		first []search.Result
		why   string
	}{
		{"no results", nil, "the question's search returned no results"},
		{"no document", threads, "none of the question's search results is a document that reads"},
	} {
		f := exampleSite()
		s, _, _ := searchFirstOver(t, f, nil)
		asked := bySearch(s, map[string][]search.Result{q: tc.first, q + " documentation": docs})
		o := s.SearchSelect(context.Background(), q, 0)
		if o.Err() != nil || len(o.Read) == 0 || o.Read[0].URL != idxRoot+"guide/middleware" {
			t.Fatalf("%s: read %+v (%v), want the second search's first result", tc.name, o.Read, o.Err())
		}
		if !slices.Equal(*asked, []string{q, q + " documentation"}) {
			t.Errorf("%s: searched %q, want the question, then the question + documentation", tc.name, *asked)
		}
		want := tc.why + `; this answer is from a second search, the question + "documentation"`
		if fm := NewFrontmatter(q, SearchBackend, "", o).Render(); !strings.Contains(fm, "\nnote: "+want+"\n") {
			t.Errorf("%s: frontmatter\n%s\nwant note %q", tc.name, fm, want)
		}
		if len(o.OtherResults) != 1 || o.OtherResults[0].URL != "https://tips.test/articles/middleware" {
			t.Errorf("%s: other results %+v, want the second search's", tc.name, o.OtherResults)
		}
		n := len(*asked)
		again := s.SearchSelect(context.Background(), q, 0)
		if len(*asked) != n || again.Note != o.Note || !slices.Equal(again.Read, o.Read) {
			t.Errorf("%s: a repeat searched %d times, note %q; want none and the same note", tc.name, len(*asked)-n, again.Note)
		}
	}
}

// The second search never runs when the question's search gave a page
// that reads: its answer is the one before this rule.
func TestBareNoSecondSearchWhenOneReads(t *testing.T) {
	const q = "add middleware to a route"
	f := exampleSite()
	s, _, _ := searchFirstOver(t, f, nil)
	asked := bySearch(s, map[string][]search.Result{
		q: {hit("https://stackoverflow.com/questions/1/x", "thread"), hit(idxRoot+"guide/middleware", "Middleware")},
	})
	o := s.SearchSelect(context.Background(), q, 0)
	if o.Err() != nil || len(o.Read) != 1 || len(*asked) != 1 || strings.Contains(o.Note, "second search") {
		t.Errorf("read %+v, searched %q, note %q (%v): want the question's page and one search", o.Read, *asked, o.Note, o.Err())
	}
}

// When the second search gives nothing to read either: no results from
// both is nothing to print (exit 3), saying both were searched; results
// that do not read print alone, the first search's when it had any.
func TestBareBothSearchesGiveNothing(t *testing.T) {
	const q = "add middleware to a route"
	thread := hit("https://stackoverflow.com/questions/1/example-middleware", "How to add middleware")
	issue := hit("https://github.com/example/example/issues/12", "middleware order issue")

	s, _, _ := searchFirstOver(t, exampleSite(), nil)
	bySearch(s, nil)
	o := s.SearchSelect(context.Background(), q, 0)
	if !errors.Is(o.Err(), errSearchEmpty) || !strings.Contains(o.Err().Error(), `a second search, the question + "documentation" returned none either`) {
		t.Errorf("both empty: %v, want search_empty (exit 3) naming the second search", o.Err())
	}

	s, _, _ = searchFirstOver(t, exampleSite(), nil)
	bySearch(s, map[string][]search.Result{q: {thread}, q + " documentation": {issue}})
	o = s.SearchSelect(context.Background(), q, 0)
	if o.Err() != nil || o.Reason != "results_only" || len(o.OtherResults) != 1 || o.OtherResults[0].URL != thread.URL ||
		!strings.Contains(o.Note, `nor do the results of a second search, the question + "documentation"`) {
		t.Errorf("neither reads: %s %+v note %q (%v), want the first search's results alone", o.Reason, o.OtherResults, o.Note, o.Err())
	}

	s, _, _ = searchFirstOver(t, exampleSite(), nil)
	bySearch(s, map[string][]search.Result{q + " documentation": {issue}})
	o = s.SearchSelect(context.Background(), q, 0)
	if o.Err() != nil || o.Reason != "results_only" || len(o.OtherResults) != 1 || o.OtherResults[0].URL != issue.URL ||
		!strings.Contains(o.Note, `the question's search returned no results; these are a second search, the question + "documentation"`) {
		t.Errorf("first empty, second unreadable: %s %+v note %q (%v), want the second search's results", o.Reason, o.OtherResults, o.Note, o.Err())
	}
}

// The two shapes a fresh run tripped on: a search's first result a forge
// file that is no document (a .proto schema GitHub shows as chrome only, an
// example config on the raw host) is passed over without a fetch, and the
// docs page after it read.
func TestBareSkipsForgeFilesThatAreNoDocument(t *testing.T) {
	docs := "https://www.proxy.example.test/docs/circuit_breaking"
	for _, first := range []string{
		"https://github.com/envoyproxy/envoy/blob/main/api/envoy/config/cluster/v3/circuit_breaker.proto",
		"https://raw.githubusercontent.com/ansible/ansible/stable-2.11/examples/ansible.cfg",
	} {
		f := &fakeFetcher{pages: map[string]fakePage{
			first: {title: "file", markdown: "# file\n\n141 lines (119 loc) · 6.42 KB\n\nRaw\n"},
			docs:  {title: "Circuit breaking", markdown: "# Circuit breaking\n\n## Defaults\n\nmax_connections defaults to 1024 and max_pending_requests to 1024.\n"},
		}}
		s, _, _ := searchFirstOver(t, f, []search.Result{hit(first, "file"), hit(docs, "Circuit breaking")})
		o := s.SearchSelect(context.Background(), "default max_connections and max_pending_requests thresholds", 0)
		if o.Err() != nil || len(o.Read) != 1 || o.Read[0].URL != docs || o.Read[0].Rank != 2 {
			t.Errorf("%s first: read %+v (%v), want the docs page at rank 2", first, o.Read, o.Err())
		}
		assertNeverFetched(t, f, first)
	}
}

// A result that reads but gives almost no text, and holds nothing the
// question asks about, is no readable document: skipped with its cause,
// the next result read. A short page that holds an identifier the question
// writes is read.
func TestBareSkipsAlmostEmptyPages(t *testing.T) {
	stub := "https://files.example.test/view/circuit_breaker"
	docs := "https://www.proxy.example.test/docs/circuit_breaking"
	f := &fakeFetcher{pages: map[string]fakePage{
		stub: {title: "circuit_breaker", markdown: "# circuit_breaker\n\n141 lines (119 loc) · 6.42 KB\n\nRaw\n"},
		docs: {title: "Circuit breaking", markdown: "# Circuit breaking\n\n## Defaults\n\nmax_connections defaults to 1024 and max_pending_requests to 1024.\n"},
	}}
	q := "Envoy cluster circuit breakers: what are the default max_connections and max_pending_requests thresholds?"
	s, _, _ := searchFirstOver(t, f, []search.Result{hit(stub, "circuit_breaker"), hit(docs, "Circuit breaking")})
	o := s.SearchSelect(context.Background(), q, 0)
	if o.Err() != nil || len(o.Read) != 1 || o.Read[0].URL != docs {
		t.Fatalf("read %+v (%v), want the docs page", o.Read, o.Err())
	}
	if len(o.Skipped) != 1 || o.Skipped[0].URL != stub || o.Skipped[0].Cause != "almost no text" {
		t.Errorf("skipped %+v, want the stub with cause almost no text", o.Skipped)
	}
	// The same short page holding an identifier asked about is read.
	f.pages[stub] = fakePage{title: "circuit_breaker", markdown: "# circuit_breaker\n\nmax_connections: 1024 by default.\n"}
	s, _, _ = searchFirstOver(t, f, []search.Result{hit(stub, "circuit_breaker"), hit(docs, "Circuit breaking")})
	if o := s.SearchSelect(context.Background(), q, 0); len(o.Read) == 0 || o.Read[0].URL != stub {
		t.Errorf("a short page holding max_connections: read %+v (%v), want it read", o.Read, o.Err())
	}
}
