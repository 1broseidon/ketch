package docs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// fakeAutoWeb is the web backend as the auto chain sees it: canned answers
// or errors, and the calls it got.
type fakeAutoWeb struct {
	results []Result
	matches []LibraryMatch
	err     error

	mu    sync.Mutex
	calls []string
}

func (f *fakeAutoWeb) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

func (f *fakeAutoWeb) Search(_ context.Context, query string, _ int) ([]Result, error) {
	f.record("search " + query)
	return f.results, f.err
}

func (f *fakeAutoWeb) GetDocs(_ context.Context, lib, query string, _ int) ([]Result, error) {
	f.record("docs " + lib + " " + query)
	return f.results, f.err
}

func (f *fakeAutoWeb) ResolveLibrary(_ context.Context, name string, _ int) ([]LibraryMatch, error) {
	f.record("resolve " + name)
	return f.matches, f.err
}

// widgetAmbiguity is web's refusal of "widget": npm's on widgetjs.dev and
// PyPI's on Read the Docs, as the cmd fixtures resolve it.
func widgetAmbiguity() error {
	return &AmbiguousLibraryError{Library: "widget", Matches: []LibraryMatch{
		{ID: "https://widgetjs.dev/docs/", Title: "Introduction", Packages: []string{"npm:widget", "github:widgetjs/widget"}},
		{ID: "https://widget.readthedocs.io/en/stable/", Title: "Widget 2.4 documentation", Packages: []string{"pypi:widget", "github:pywidget/widget"}},
	}}
}

// c7Calls records the Context7 requests a test server got.
type c7Calls struct {
	mu   sync.Mutex
	urls []string
}

func (c *c7Calls) add(u string) {
	c.mu.Lock()
	c.urls = append(c.urls, u)
	c.mu.Unlock()
}

func (c *c7Calls) list() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.urls...)
}

// autoWith is an auto chain over web and a Context7 whose search ranks
// pick first for any query, and whose docs for any library are one
// snippet labelled with it.
func autoWith(t *testing.T, web *fakeAutoWeb, pick string) (*Auto, *c7Calls) {
	t.Helper()
	calls := &c7Calls{}
	c7 := newTestContext7(t, func(w http.ResponseWriter, r *http.Request) {
		calls.add(r.URL.Path + "?" + r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/context" {
			fmt.Fprintf(w, `{"codeSnippets":[{"codeTitle":"From Context7","codeId":"https://example.com/doc","codeList":[{"code":"x"}]}],"infoSnippets":[]}`)
			return
		}
		fmt.Fprintf(w, `{"results":[{"id":%q,"title":"Picked"}]}`, pick)
	})
	return NewAuto(web, c7), calls
}

var webAnswer = []Result{{Library: "https://react.dev/", Title: "useState", URL: "https://react.dev/reference/react/useState", Source: "web"}}

// Web answers first; Context7 is never asked when it does, and an empty
// answer is still web's answer.
func TestAutoWebAnswerIsKept(t *testing.T) {
	for _, results := range [][]Result{webAnswer, nil} {
		web := &fakeAutoWeb{results: results}
		a, calls := autoWith(t, web, "/facebook/react")
		got, sel, err := a.SearchSelect(context.Background(), "react useState", 5)
		if err != nil || sel.Backend != "web" || len(sel.Failures) != 0 || len(got) != len(results) {
			t.Fatalf("web answered %d: got %v, %+v, %v", len(results), got, sel, err)
		}
		if c := calls.list(); len(c) != 0 {
			t.Errorf("Context7 asked after web answered: %v", c)
		}
	}
}

// Each kind of web refusal falls through to Context7, in that order, and
// the answer says Context7 gave it, with web's failure reported.
func TestAutoFallsThroughOnRefusal(t *testing.T) {
	for name, webErr := range map[string]error{
		"no library identified": fmt.Errorf("%w: no library identified in %q", ErrLibraryRequired, "zzlib hooks"),
		"not found":             fmt.Errorf("web: %w", ErrNotFound),
		"upstream":              errors.New("web: reading \"zzlib\" as library: pypi.org returned 503"),
	} {
		t.Run(name, func(t *testing.T) {
			web := &fakeAutoWeb{err: webErr}
			a, calls := autoWith(t, web, "/zz/zzlib")
			got, sel, err := a.SearchSelect(context.Background(), "zzlib hooks", 5)
			if err != nil {
				t.Fatalf("fallthrough: %v", err)
			}
			if sel.Backend != "context7" || len(sel.Failures) != 1 || sel.Failures[0].Backend != "web" || !errors.Is(sel.Failures[0].Err, webErr) {
				t.Errorf("selection = %+v, want context7 after web's failure", sel)
			}
			if len(got) != 1 || got[0].Source != "context7" || got[0].Library != "/zz/zzlib" {
				t.Errorf("results = %+v, want Context7's", got)
			}
			if len(web.calls) != 1 {
				t.Errorf("web calls = %v, want one, first", web.calls)
			}
			if c := calls.list(); len(c) != 2 || !strings.HasPrefix(c[0], "/api/v1/search?query=zzlib+hooks") {
				t.Errorf("Context7 calls = %v, want its resolve then docs", c)
			}
		})
	}
}

// Web's ambiguity is settled by Context7 only when Context7 picks one of
// web's candidates; otherwise the ambiguity error stands and Context7's
// docs are never fetched.
func TestAutoAmbiguityNeedsAWebCandidate(t *testing.T) {
	t.Run("pick is a candidate", func(t *testing.T) {
		a, calls := autoWith(t, &fakeAutoWeb{err: widgetAmbiguity()}, "/pywidget/widget")
		got, sel, err := a.SearchSelect(context.Background(), "widget configure themes", 5)
		if err != nil || sel.Backend != "context7" || len(got) != 1 || got[0].Library != "/pywidget/widget" {
			t.Fatalf("got %+v, %+v, %v; want Context7's /pywidget/widget", got, sel, err)
		}
		if len(calls.list()) != 2 {
			t.Errorf("Context7 calls = %v", calls.list())
		}
	})
	t.Run("pick is another project", func(t *testing.T) {
		a, calls := autoWith(t, &fakeAutoWeb{err: widgetAmbiguity()}, "/someone/widget")
		_, sel, err := a.SearchSelect(context.Background(), "widget configure themes", 5)
		var amb *AmbiguousLibraryError
		if !errors.As(err, &amb) || !errors.Is(err, ErrLibraryRequired) {
			t.Fatalf("err = %v, want the ambiguity error to stand", err)
		}
		if !strings.Contains(err.Error(), "context7 picked /someone/widget, which is none of web's candidates") ||
			!strings.Contains(err.Error(), "https://widgetjs.dev/docs/") {
			t.Errorf("error %q should name the candidates and Context7's pick", err)
		}
		if sel.Backend != "" {
			t.Errorf("selection = %+v, want none", sel)
		}
		if c := calls.list(); len(c) != 1 {
			t.Errorf("Context7 calls = %v, want the resolve alone (no docs for a rejected pick)", c)
		}
	})
}

// A query that pins its library (an ecosystem prefix, a version, a docs
// URL) stays web's, and so does a call the caller cancelled.
func TestAutoKeepsPinnedAndCancelledCalls(t *testing.T) {
	webErr := fmt.Errorf("web: %w", ErrNotFound)
	for _, q := range []string{"npm:react hooks", "react@18 hooks", "https://react.dev/ hooks", "@tanstack/query caching", "github:facebook/react hooks"} {
		a, calls := autoWith(t, &fakeAutoWeb{err: webErr}, "/facebook/react")
		if _, _, err := a.SearchSelect(context.Background(), q, 5); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: err %v, want web's", q, err)
		}
		if c := calls.list(); len(c) != 0 {
			t.Errorf("%q: Context7 asked: %v", q, c)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a, calls := autoWith(t, &fakeAutoWeb{err: context.Canceled}, "/facebook/react")
	if _, _, err := a.SearchSelect(ctx, "react hooks", 5); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	if c := calls.list(); len(c) != 0 {
		t.Errorf("cancelled call asked Context7: %v", c)
	}
}

// When Context7 fails too, the error is web's refusal, classified as web
// classified it, with Context7's failure in the message.
func TestAutoBothFail(t *testing.T) {
	webErr := fmt.Errorf("%w: no library identified in %q", ErrLibraryRequired, "zzlib hooks")
	c7 := newTestContext7(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ratelimit-limit", "200")
		w.Header().Set("ratelimit-remaining", "0")
		w.Header().Set("ratelimit-reset", "1796083200")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c7.apiKey = ""
	a := NewAuto(&fakeAutoWeb{err: webErr}, c7)
	_, sel, err := a.SearchSelect(context.Background(), "zzlib hooks", 5)
	if !errors.Is(err, ErrLibraryRequired) {
		t.Fatalf("err = %v, want web's validation refusal", err)
	}
	for _, want := range []string{"no library identified", "context7 fallback", "resets 2026-12-01 00:00 UTC", "ketch config set context7_api_key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
	if len(sel.Failures) != 2 || sel.Backend != "" {
		t.Errorf("selection = %+v, want both failures and no answer", sel)
	}
}

// GetDocs: a Context7 ID goes to Context7 directly, never to web.
func TestAutoGetDocsContext7ID(t *testing.T) {
	a, calls := autoWith(t, &fakeAutoWeb{results: webAnswer}, "/x/y")
	web := a.web.(*fakeAutoWeb)
	got, sel, err := a.GetDocsSelect(context.Background(), "/reactjs/react.dev", "hooks", 4000)
	if err != nil || sel.Backend != "context7" || len(web.calls) != 0 || len(got) != 1 || got[0].Library != "/reactjs/react.dev" {
		t.Fatalf("Context7 ID: %+v %+v %v; web calls %v", got, sel, err, web.calls)
	}
	if c := calls.list(); len(c) != 1 || !strings.HasPrefix(c[0], "/api/v2/context?libraryId=%2Freactjs%2Freact.dev") {
		t.Errorf("Context7 ID calls = %v", c)
	}
}

// GetDocs: a docs URL and a pinned spec stay web's.
func TestAutoGetDocsPinnedStaysWeb(t *testing.T) {
	webErr := fmt.Errorf("web: %w", ErrNotFound)
	for _, lib := range []string{"https://react.dev/", "npm:react", "react@18", "pypi:requests"} {
		a, calls := autoWith(t, &fakeAutoWeb{err: webErr}, "/facebook/react")
		if _, _, err := a.GetDocsSelect(context.Background(), lib, "hooks", 4000); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err %v, want web's", lib, err)
		}
		if c := calls.list(); len(c) != 0 {
			t.Errorf("%s: Context7 asked: %v", lib, c)
		}
	}
}

// GetDocs: a bare name falls through; an owner/repo falls through only to
// exactly that Context7 ID; an ambiguous name only to a web candidate.
func TestAutoGetDocsFallsThrough(t *testing.T) {
	ctx := context.Background()
	webErr := fmt.Errorf("web: %w", ErrNotFound)

	a, _ := autoWith(t, &fakeAutoWeb{err: webErr}, "/zz/zzlib")
	if got, sel, err := a.GetDocsSelect(ctx, "zzlib", "hooks", 4000); err != nil || sel.Backend != "context7" || got[0].Library != "/zz/zzlib" {
		t.Errorf("bare name: %+v %+v %v", got, sel, err)
	}

	a, _ = autoWith(t, &fakeAutoWeb{err: webErr}, "/Facebook/React")
	if _, sel, err := a.GetDocsSelect(ctx, "github:facebook/react", "hooks", 4000); err != nil || sel.Backend != "context7" {
		t.Errorf("owner/repo, exact pick: %+v %v", sel, err)
	}
	a, calls := autoWith(t, &fakeAutoWeb{err: webErr}, "/react/react")
	if _, _, err := a.GetDocsSelect(ctx, "facebook/react", "hooks", 4000); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "not that repository") {
		t.Errorf("owner/repo, other pick: %v", err)
	}
	if c := calls.list(); len(c) != 1 {
		t.Errorf("owner/repo, other pick: Context7 calls %v, want the resolve alone", c)
	}

	a, _ = autoWith(t, &fakeAutoWeb{err: widgetAmbiguity()}, "/someone/widget")
	var amb *AmbiguousLibraryError
	if _, _, err := a.GetDocsSelect(ctx, "widget", "themes", 4000); !errors.As(err, &amb) {
		t.Errorf("ambiguous name, foreign pick: %v, want the ambiguity", err)
	}
}

// Resolve lists web's matches, and Context7's only when web has none or
// fails.
func TestAutoResolve(t *testing.T) {
	ctx := context.Background()
	webMatches := []LibraryMatch{{ID: "https://react.dev/", Title: "React"}}
	a, calls := autoWith(t, &fakeAutoWeb{matches: webMatches}, "/facebook/react")
	if got, sel, err := a.ResolveSelect(ctx, "react", 5); err != nil || sel.Backend != "web" || len(got) != 1 || len(calls.list()) != 0 {
		t.Errorf("web matches: %+v %+v %v %v", got, sel, err, calls.list())
	}
	for name, web := range map[string]*fakeAutoWeb{
		"no matches": {},
		"not found":  {err: fmt.Errorf("web: %w", ErrNotFound)},
	} {
		a, _ := autoWith(t, web, "/zz/zzlib")
		got, sel, err := a.ResolveSelect(ctx, "zzlib", 5)
		if err != nil || sel.Backend != "context7" || len(got) != 1 || got[0].ID != "/zz/zzlib" {
			t.Errorf("%s: %+v %+v %v", name, got, sel, err)
		}
	}
	a, calls = autoWith(t, &fakeAutoWeb{err: fmt.Errorf("web: %w", ErrNotFound)}, "/x/y")
	if _, _, err := a.ResolveSelect(ctx, "npm:zzlib", 5); !errors.Is(err, ErrNotFound) || len(calls.list()) != 0 {
		t.Errorf("pinned resolve: %v, Context7 calls %v", err, calls.list())
	}
}

// candidateFor matches by identity only, never by title.
func TestAutoCandidateFor(t *testing.T) {
	cands := []LibraryMatch{
		{ID: "https://react.dev/", Title: "React", Packages: []string{"npm:react", "github:facebook/react"}},
		{ID: "https://docs.rs/widget/latest/widget/", Title: "widget", Packages: []string{"crates:widget"}},
		{ID: "https://widget.readthedocs.io/en/stable/", Title: "Widget"},
	}
	for id, want := range map[string]string{
		"/facebook/react":                               "https://react.dev/",
		"/FaceBook/React/v19.1.0":                       "https://react.dev/",
		"/reactjs/react.dev":                            "https://react.dev/",
		"/websites/react_dev":                           "https://react.dev/",
		"/websites/react_dev_reference":                 "https://react.dev/",
		"/websites/docs_rs_widget_latest_widget":        "https://docs.rs/widget/latest/widget/",
		"/websites/widget_readthedocs_io_en_stable_api": "https://widget.readthedocs.io/en/stable/",
		"/react/react":                                  "",
		"/websites/react_developer_tools":               "",
		"/rust-lang/docs.rs":                            "",
		"/websites/docs_rs_gadget":                      "",
		"/websites/widget_readthedocs_io":               "",
		"/someone/React":                                "",
		"react":                                         "",
	} {
		got, ok := candidateFor(id, cands)
		if (want == "") == ok || got.ID != want {
			t.Errorf("candidateFor(%q) = %q, %v; want %q", id, got.ID, ok, want)
		}
	}
}

func TestIsContext7ID(t *testing.T) {
	for s, want := range map[string]bool{
		"/reactjs/react.dev": true, "/vercel/next.js/v14.3.0": true, "/websites/react_dev": true,
		"react": false, "facebook/react": false, "https://react.dev/": false, "//react.dev/x": false, "/a": false, "/a/b/c/d": false,
	} {
		if got := isContext7ID(s); got != want {
			t.Errorf("isContext7ID(%q) = %v, want %v", s, got, want)
		}
	}
}

// Context7's pick for a bare query must be a library the query names. The
// rejected case is one seen live: "useState in react" resolved on Context7
// to a React Native sign-in library's docs.
func TestAutoNamedBy(t *testing.T) {
	for _, tc := range []struct {
		query, id, title string
		ok               bool
	}{
		{"useState in react", "/reactjs/react.dev", "React", true},
		{"useState in react", "/react-native-google-signin/docs", "React Native Google Sign In", false},
		{"routing in next.js", "/vercel/next.js", "Next.js", true},
		{"nextjs routing", "/vercel/next.js", "Next.js", true},
		{"react router loaders", "/remix-run/react-router", "React Router", true},
		{"zzlib hooks", "/zz/zzlib", "Something else", true},
		{"docs for hooks", "/acme/docs", "Docs", false},
		{"how do I use hooks", "/facebook/react", "React", false},
		{"tailwind dark mode", "/websites/tailwindcss_com", "Tailwind CSS", false},
		{"tailwindcss dark mode", "/websites/tailwindcss_com", "Tailwind CSS", true},
	} {
		err := namedBy(tc.query)(LibraryMatch{ID: tc.id, Title: tc.title})
		if (err == nil) != tc.ok {
			t.Errorf("namedBy(%q)(%s %q) = %v, want ok %v", tc.query, tc.id, tc.title, err, tc.ok)
		}
	}
}

// A bare query Context7 answers with a library it does not name keeps
// web's refusal, and no Context7 docs are fetched.
func TestAutoUnnamedPickRefused(t *testing.T) {
	webErr := fmt.Errorf("%w: no library identified in %q", ErrLibraryRequired, "useState in react")
	a, calls := autoWith(t, &fakeAutoWeb{err: webErr}, "/react-native-google-signin/docs")
	_, sel, err := a.SearchSelect(context.Background(), "useState in react", 5)
	if !errors.Is(err, ErrLibraryRequired) || !strings.Contains(err.Error(), "a library the query does not name") || sel.Backend != "" {
		t.Fatalf("err = %v, sel %+v; want web's refusal standing", err, sel)
	}
	if c := calls.list(); len(c) != 1 {
		t.Errorf("Context7 calls = %v, want the resolve alone", c)
	}
}
