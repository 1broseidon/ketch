package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/docs"
)

// setDocsBackend overrides the docs command's --backend flag for one test,
// restoring the previous value afterwards.
func setDocsBackend(t *testing.T, backend string) {
	t.Helper()
	prev, _ := docsCmd.Flags().GetString("backend")
	if err := docsCmd.Flags().Set("backend", backend); err != nil {
		t.Fatalf("set backend flag: %v", err)
	}
	t.Cleanup(func() { docsCmd.Flags().Set("backend", prev) }) //nolint:errcheck
}

// asExitError unwraps err into an *ExitError or fails the test. This is the
// exact classification main.go turns into the process exit code.
func asExitError(t *testing.T, err error) *ExitError {
	t.Helper()
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("want *ExitError (mapped to a nonzero exit by main.go), got %T: %v", err, err)
	}
	return exitErr
}

// An unknown docs backend is a validation failure (exit 2) and the error must
// list the valid options.
func TestDocsUnknownBackendExitsValidationWithOptions(t *testing.T) {
	setDocsBackend(t, "bogus")

	exitErr := asExitError(t, runDocs(docsCmd, []string{"test"}))
	if exitErr.Code != ExitValidation {
		t.Errorf("exit code = %d, want %d (validation)", exitErr.Code, ExitValidation)
	}
	if want := "(available: " + strings.Join(docs.AvailableBackends(), ", ") + ";"; !strings.Contains(want, "context7") || !strings.Contains(want, "search") || !strings.Contains(exitErr.Error(), want) {
		t.Errorf("error should list the available backends, got: %v", exitErr)
	}
}

// --resolve returns library IDs, not URLs, so a --tag has nothing to record:
// the combination is refused up front instead of succeeding and saving nothing.
func TestDocsResolveRejectsTag(t *testing.T) {
	if err := docsCmd.Flags().Set("resolve", "true"); err != nil {
		t.Fatal(err)
	}
	if err := docsCmd.Flags().Set("tag", "libs"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = docsCmd.Flags().Set("resolve", "false")
		_ = docsCmd.Flags().Set("tag", "")
	})

	exitErr := asExitError(t, runDocs(docsCmd, []string{"react"}))
	if exitErr.Code != ExitValidation {
		t.Errorf("exit code = %d, want %d (validation)", exitErr.Code, ExitValidation)
	}
	if !strings.Contains(exitErr.Error(), "--tag cannot be combined with --resolve") {
		t.Errorf("error should name the conflicting flags, got: %v", exitErr)
	}
}

// upstreamErr must classify docs.ErrLibraryRequired as exit 2 (the caller
// must add --library), docs.ErrNotFound as exit 3 (permanently absent, not
// retryable) and everything else as exit 4 (upstream, retry may help).
func TestUpstreamErrClassification(t *testing.T) {
	libraryRequired := fmt.Errorf("web: %w: pass the docs site URL with --library", docs.ErrLibraryRequired)
	if got := asExitError(t, upstreamErr(libraryRequired, "docs search failed")); got.Code != ExitValidation {
		t.Errorf("docs.ErrLibraryRequired: exit code = %d, want %d (validation)", got.Code, ExitValidation)
	}

	notFound := fmt.Errorf("context7: library %q %w", "/no/such-lib", docs.ErrNotFound)
	if got := asExitError(t, upstreamErr(notFound, "docs fetch failed")); got.Code != ExitNotFound {
		t.Errorf("docs.ErrNotFound: exit code = %d, want %d (not found)", got.Code, ExitNotFound)
	}

	if got := asExitError(t, upstreamErr(errors.New("boom"), "docs fetch failed")); got.Code != ExitUpstream {
		t.Errorf("generic error: exit code = %d, want %d (upstream)", got.Code, ExitUpstream)
	}

	// A spent Context7 quota is upstream (ADR-0002): it succeeds again
	// after the reset, or with a key.
	limited := fmt.Errorf("context7 resolve failed: %w", &docs.RateLimitError{Limit: 200})
	if got := asExitError(t, upstreamErr(limited, "docs search failed")); got.Code != ExitUpstream {
		t.Errorf("rate limit: exit code = %d, want %d (upstream)", got.Code, ExitUpstream)
	}
}

// A match without evidence, a version or package specs, which is every
// Context7 match, prints the line --resolve has always printed, byte for
// byte, whatever else it carries (Context7 does report versions).
func TestPrintLibraryMatchesWithoutEvidenceIsUnchanged(t *testing.T) {
	matches := []docs.LibraryMatch{
		{ID: "/facebook/react", Title: "React", Description: "The library for web and native user interfaces.", TotalSnippets: 3215, TrustScore: 9, Versions: []string{"v18.3.1", "v19.1.0"}},
		{ID: "/reactjs/react.dev", Title: "React docs", TotalSnippets: 2477, TrustScore: 10},
		{ID: "/vercel/next.js", Title: "Next.js", TotalSnippets: 0, TrustScore: 7.5},
	}
	got := formatLibraryMatches(matches)
	want := "/facebook/react  React  (snippets: 3215, trust: 9.0)\n" +
		"/reactjs/react.dev  React docs  (snippets: 2477, trust: 10.0)\n" +
		"/vercel/next.js  Next.js  (snippets: 0, trust: 7.5)\n"
	if got != want {
		t.Errorf("resolve output changed:\n got %q\nwant %q", got, want)
	}
}

// A match with evidence says why its ID is the library's docs: trust,
// version and versions on the ID line (each only when set), then the
// package specs and the evidence on indented lines.
func TestPrintLibraryMatchesWithEvidence(t *testing.T) {
	matches := []docs.LibraryMatch{
		{
			ID: "https://click.palletsprojects.com/en/stable/", Title: "Click", TrustScore: 10,
			Version: "8.5.x", Versions: []string{"stable"},
			Packages: []string{"pypi:click", "github:pallets/click"},
			Evidence: []string{"pypi click lists it as Documentation", "github pallets/click homepage", "links to github.com/pallets/click"},
		},
		{
			ID: "https://docs.djangoproject.com/en/6.1/", Title: "Django", TrustScore: 9.5,
			Version: "6.1", Versions: []string{"6.1", "6.0", "5.2", "dev"},
			Packages: []string{"pypi:django"},
			Evidence: []string{"pypi django lists it as Documentation"},
		},
		// A docs URL passed as is: no packages, no versions, low trust.
		{ID: "https://docs.example.com/", Title: "Example", Evidence: []string{"unverified: no package or repository names this site"}},
		// Snippet counts show when a backend reports them.
		{ID: "https://docs.example.org/", Title: "Example org", TotalSnippets: 12, TrustScore: 4, Evidence: []string{"site names the project"}},
		// A docs URL with no evidence still shows the site's versions.
		{ID: "https://docs.example.net/en/2.4/", Title: "Example net", Version: "2.4", Versions: []string{"2.4", "2.3"}},
	}
	got := formatLibraryMatches(matches)
	want := "https://click.palletsprojects.com/en/stable/  Click  (trust: 10.0, version: 8.5.x, versions: stable)\n" +
		"  packages: pypi:click, github:pallets/click\n" +
		"  evidence: pypi click lists it as Documentation; github pallets/click homepage; links to github.com/pallets/click\n" +
		"https://docs.djangoproject.com/en/6.1/  Django  (trust: 9.5, version: 6.1, versions: 6.1 6.0 5.2 dev)\n" +
		"  packages: pypi:django\n" +
		"  evidence: pypi django lists it as Documentation\n" +
		"https://docs.example.com/  Example  (trust: 0.0)\n" +
		"  evidence: unverified: no package or repository names this site\n" +
		"https://docs.example.org/  Example org  (snippets: 12, trust: 4.0)\n" +
		"  evidence: site names the project\n" +
		"https://docs.example.net/en/2.4/  Example net  (trust: 0.0, version: 2.4, versions: 2.4 2.3)\n"
	if got != want {
		t.Errorf("resolve output:\n got %q\nwant %q", got, want)
	}
}

// The --library and --resolve help say what each library provider accepts,
// from its descriptor: shared help text names no provider of its own.
func TestDocsLibraryHelpFollowsDescriptors(t *testing.T) {
	var helps []string
	for _, id := range docs.LibraryBackends() {
		p, _ := docs.Lookup(id)
		if p.LibraryHelp != "" {
			helps = append(helps, p.Name+": "+p.LibraryHelp)
		}
	}
	if len(helps) == 0 {
		t.Fatal("no library provider declares LibraryHelp; the web backend should")
	}
	for _, flag := range []string{"library", "resolve"} {
		usage := docsCmd.Flags().Lookup(flag).Usage
		for _, want := range append(helps, docs.LibraryProviderNames()...) {
			if !strings.Contains(usage, want) {
				t.Errorf("--%s help %q does not mention %q", flag, usage, want)
			}
		}
	}
	if !strings.Contains(docsCmd.Long, helps[0]) {
		t.Errorf("docs help %q does not say what a library can be (%q)", docsCmd.Long, helps[0])
	}
}

// A name more than one library answers to is input to fix (exit 2), and the
// message that lists the candidates reaches the user whole: every ID and
// every package spec, after the command's own prefix.
func TestAmbiguousLibraryIsValidationWithCandidates(t *testing.T) {
	amb := &docs.AmbiguousLibraryError{Library: "widget", Matches: []docs.LibraryMatch{
		{ID: "https://widgetjs.dev/docs/", Title: "Widget", Packages: []string{"npm:widget", "github:widgetjs/widget"}},
		{ID: "https://widget.readthedocs.io/en/stable/", Title: "Widget", Packages: []string{"pypi:widget", "github:pywidget/widget"}},
	}}
	wrapped := fmt.Errorf("web: %w", amb)
	got := asExitError(t, upstreamErr(wrapped, "docs fetch failed"))
	if got.Code != ExitValidation {
		t.Errorf("exit code = %d, want %d (validation)", got.Code, ExitValidation)
	}
	if want := "docs fetch failed: " + wrapped.Error(); got.Error() != want {
		t.Errorf("message:\n got %q\nwant %q", got.Error(), want)
	}
	for _, want := range []string{"https://widgetjs.dev/docs/", "https://widget.readthedocs.io/en/stable/", "npm:widget", "pypi:widget", "github:pywidget/widget", "--library"} {
		if !strings.Contains(got.Error(), want) {
			t.Errorf("message %q does not name %q", got.Error(), want)
		}
	}
	var target *docs.AmbiguousLibraryError
	if !errors.As(got, &target) || len(target.Matches) != 2 {
		t.Errorf("the candidates are not reachable with errors.As: %v", got)
	}
}

// The frontmatter's library is the ID the results came from: the docs root
// a name passed as --library resolved to, the library itself when the
// backend echoes it (Context7), the flag when nothing came back.
func TestPrintDocsResultsShowsResolvedLibrary(t *testing.T) {
	cases := []struct {
		name, library string
		results       []docs.Result
		want          string
	}{
		{"name resolved", "django@5.2", []docs.Result{{Library: "https://docs.djangoproject.com/en/5.2/", Title: "Middleware", URL: "https://docs.djangoproject.com/en/5.2/topics/http/middleware/", Source: "web"}}, "library: https://docs.djangoproject.com/en/5.2/\n"},
		{"id echoed", "/vercel/next.js", []docs.Result{{Library: "/vercel/next.js", Title: "Routing", URL: "https://nextjs.org/docs/routing", Source: "context7"}}, "library: /vercel/next.js\n"},
		{"no results", "django@5.2", nil, "library: django@5.2\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				printDocsResults(os.Stdout, "middleware", "web", tc.library, docs.Outcome{Results: tc.results}, false, docs.HintNone)
			})
			if !strings.Contains(out, "\n"+tc.want) || strings.Count(out, "library: ") != 1 {
				t.Errorf("frontmatter:\n%s\nwant one %q", out, tc.want)
			}
		})
	}
}

// --trace is a QA flag: it exists but help does not show it.
func TestDocsTraceFlagHidden(t *testing.T) {
	f := docsCmd.Flags().Lookup("trace")
	if f == nil || !f.Hidden {
		t.Fatalf("--trace = %+v, want a hidden flag", f)
	}
	if strings.Contains(docsCmd.UsageString(), "--trace") {
		t.Error("--trace in the docs usage")
	}
}
