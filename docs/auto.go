package docs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	config "github.com/1broseidon/ketch/internal/configbase"
)

// AutoBackend is the docs backend that chains the providers, as search's
// auto does: the local index first (no network) when it holds the
// library, then web (keyless, no quota), then Context7 (keyless on its
// anonymous quota, or keyed). It is not a provider: it owns no endpoint or
// settings, and it is never the default until decision-10's accuracy gates
// pass.
const AutoBackend = "auto"

// SelectingSearcher is a docs backend that reports each call as an
// Outcome: Auto, which dispatches to several providers and reports each
// one's part, and Local, whose answers can carry a reason (it ranked by
// keywords).
type SelectingSearcher interface {
	Searcher
	LibraryResolver
	SearchSelect(ctx context.Context, query string, limit int) Outcome
	GetDocsSelect(ctx context.Context, libraryID, query string, tokens int) Outcome
	ResolveSelect(ctx context.Context, name string, limit int) Outcome
}

// autoWeb is what Auto needs from the web backend.
type autoWeb interface {
	Searcher
	LibraryResolver
}

// autoLocal is what Auto needs from the local index backend.
type autoLocal interface {
	SearchSelect(ctx context.Context, query string, limit int) Outcome
	GetDocsSelect(ctx context.Context, libraryID, query string, tokens int) Outcome
}

// Auto chains the local index, the web backend and Context7. The local
// index answers first when it holds the library (ADR-0007); otherwise, or
// when it finds nothing there, the chain below runs as it did without it.
// Web answers next; Context7 is
// asked only when web refuses — it identified no library, the library it
// named was not found, it found several, or a source failed — never to
// replace an answer web gave (an empty one included), and never when the
// caller named a provider with --backend. A name web found ambiguous is
// answered from Context7 only when Context7's pick is one of web's
// candidates, and after any other refusal only when the query names the
// pick (decision-13 on the board): a wrong project is worse than none. A library pinned to an ecosystem or version
// is never handed to Context7, whose matches carry neither, and a docs URL
// is web's alone.
type Auto struct {
	local autoLocal // nil: no local index in the chain
	web   autoWeb
	c7    *Context7
}

// NewAuto chains web and Context7.
func NewAuto(web autoWeb, c7 *Context7) *Auto { return &Auto{web: web, c7: c7} }

// WithLocal puts a local index backend first in the chain.
func (a *Auto) WithLocal(l autoLocal) *Auto {
	a.local = l
	return a
}

// newAutoFromConfig builds the chain from c: the local index, the web
// backend with its name resolution sources, and Context7 with c's key or
// none.
func newAutoFromConfig(c *config.Config, deps Deps) *Auto {
	return NewAuto(newWebFromConfig(c, deps), NewContext7(c.String("context7_api_key"))).WithLocal(newLocalFromConfig())
}

// Search answers a bare query; see SearchSelect.
func (a *Auto) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	o := a.SearchSelect(ctx, query, limit)
	return o.Results, o.Err()
}

// ResolveLibrary resolves a library name; see ResolveSelect.
func (a *Auto) ResolveLibrary(ctx context.Context, name string, limit int) ([]LibraryMatch, error) {
	o := a.ResolveSelect(ctx, name, limit)
	return o.Matches, o.Err()
}

// GetDocs fetches docs for a library; see GetDocsSelect.
func (a *Auto) GetDocs(ctx context.Context, libraryID, query string, tokens int) ([]Result, error) {
	o := a.GetDocsSelect(ctx, libraryID, query, tokens)
	return o.Results, o.Err()
}

// The auto chain is one state machine for all three calls:
//
//	local ──answered──▶ done
//	 │  (search and docs only; resolve starts at web)
//	 ├─not_indexed─▶ web, no local attempt recorded
//	 └─refused/failed─▶ web, the local attempt kept
//
//	web ──answered──▶ done (any local attempt kept)
//	 │
//	 └─refused/failed─▶ handoff? ──no──▶ web's outcome
//	                       │
//	                      yes─▶ context7 ──answered──▶ done (earlier attempts kept)
//	                               └─refused/failed─▶ web's outcome, every attempt kept
//
// The local index answers only for a library it holds (by its docs root,
// a name or spec it was indexed under, or the library a bare query names
// first); a library it does not hold is not an attempt at all, so a call
// for an unindexed library reads exactly as it did without the index.
// A local refusal (nothing matched) or failure (the index unreadable)
// hands the call to web like any other.
//
// Handoff is refused when the caller cancelled, or the request pins its
// library (an ecosystem, a version or a docs URL), which Context7 cannot
// honour. Context7's pick must pass accept: one of web's candidates when web
// found the name ambiguous, else a library the query names. An empty web
// answer is a refusal (ErrNoResults) like any other (decision-20). When
// neither answers, the outcome is web's refusal or failure, whose message
// is the more useful (it lists candidates, versions or what failed), and
// Context7's attempt is reported beside it.

// SearchSelect answers a bare query from web, else from Context7 with the
// whole query, as `--backend context7` would.
func (a *Auto) SearchSelect(ctx context.Context, query string, limit int) Outcome {
	var local []Attempt
	if a.local != nil {
		o, done := localFirst(ctx, a.local.SearchSelect(ctx, query, limit))
		if done {
			return o
		}
		local = o.Attempts
	}
	results, err := a.web.Search(ctx, query, limit)
	web := attempt("web", len(results), err)
	if web.Status == Answered || ctx.Err() != nil || queryPinsLibrary(query) {
		return finish(web, results, nil, append(local, web))
	}
	accept := webCandidates(web.err)
	if accept == nil {
		accept = namedBy(query)
	}
	c7results, c7err := a.c7.docsAccepting(ctx, query, query, 4000, accept)
	c7results = Truncate(c7results, limit)
	return autoSecond(ctx, local, web, attempt("context7", len(c7results), c7err), c7results, nil)
}

// localFirst reads the local index's outcome as the chain's first step:
// done with it when it answered or the caller cancelled; else the attempts
// to keep (none for a library the index does not hold) as o.Attempts.
func localFirst(ctx context.Context, o Outcome) (Outcome, bool) {
	if o.Status == Answered || o.Status == Partial || ctx.Err() != nil {
		return o, true
	}
	if o.Reason == "not_indexed" {
		o.Attempts = nil
	}
	return o, false
}

// GetDocsSelect fetches docs for a library. A Context7 ID (/owner/repo, as
// an auto --resolve lists them) goes to Context7 directly. Anything else
// goes to web first; when web refuses a bare name or an owner/repo,
// Context7 resolves it (an owner/repo only to exactly that ID) and answers.
func (a *Auto) GetDocsSelect(ctx context.Context, libraryID, query string, tokens int) Outcome {
	if isContext7ID(libraryID) {
		results, err := a.c7.GetDocs(ctx, libraryID, query, tokens)
		return Answer("context7", results, err)
	}
	var local []Attempt
	if a.local != nil {
		o, done := localFirst(ctx, a.local.GetDocsSelect(ctx, libraryID, query, tokens))
		if done {
			return o
		}
		local = o.Attempts
	}
	results, err := a.web.GetDocs(ctx, libraryID, query, tokens)
	web := attempt("web", len(results), err)
	name, exact, ok := context7Name(libraryID)
	if web.Status == Answered || ctx.Err() != nil || !ok {
		return finish(web, results, nil, append(local, web))
	}
	accept := webCandidates(web.err)
	if accept == nil {
		accept = namedBy(name)
	}
	if exact != "" {
		accept = func(m LibraryMatch) error {
			if !strings.EqualFold(c7OwnerRepo(m.ID), exact) {
				return fmt.Errorf("context7's match for %q is %s, not that repository", libraryID, m.ID)
			}
			return nil
		}
	}
	c7results, c7err := a.c7.docsAccepting(ctx, name, query, tokens, accept)
	return autoSecond(ctx, local, web, attempt("context7", len(c7results), c7err), c7results, nil)
}

// ResolveSelect lists web's matches for a name, else Context7's when web
// finds none or fails. Context7's matches carry Context7 IDs, which
// GetDocsSelect sends back to Context7.
func (a *Auto) ResolveSelect(ctx context.Context, name string, limit int) Outcome {
	matches, err := a.web.ResolveLibrary(ctx, name, limit)
	if err == nil && len(matches) == 0 {
		err = fmt.Errorf("web: library %q %w", name, ErrNotFound)
	}
	web := attempt("web", len(matches), err)
	c7name, exact, ok := context7Name(name)
	if web.Status == Answered || ctx.Err() != nil || !ok {
		return finish(web, nil, matches, []Attempt{web})
	}
	c7matches, c7err := a.c7.ResolveLibrary(ctx, c7name, limit)
	if c7err == nil && exact != "" {
		c7matches = keepID(c7matches, exact)
	}
	if c7err == nil && len(c7matches) == 0 {
		c7err = fmt.Errorf("context7: library %q %w", c7name, ErrNotFound)
	}
	return autoSecond(ctx, nil, web, attempt("context7", len(c7matches), c7err), nil, c7matches)
}

// autoSecond finishes a call Context7 took over from web: Context7's
// answer with the earlier attempts kept (local's, if any, and web's), or
// web's outcome with every attempt. A caller that cancelled during
// Context7 gets a cancelled outcome.
func autoSecond(ctx context.Context, local []Attempt, web, c7 Attempt, results []Result, matches []LibraryMatch) Outcome {
	attempts := append(append(local, web), c7)
	if c7.Status == Answered {
		return finish(c7, results, matches, attempts)
	}
	if err := ctx.Err(); err != nil {
		return finish(attempt("auto", 0, fmt.Errorf("docs auto cancelled after web did not answer: %w", err)), nil, nil, attempts)
	}
	final := web
	final.err = &autoError{web: web.err, c7: c7.err}
	return finish(final, nil, nil, attempts)
}

// autoError is web's refusal and Context7's failure after it. It wraps
// web's error alone, so the call is classified as web's refusal was (an
// ambiguous name stays a validation error naming its candidates, a missing
// library not found); Context7's attempt is in the outcome and the message.
type autoError struct {
	web, c7 error
}

func (e *autoError) Error() string {
	return fmt.Sprintf("%v (context7 fallback: %v)", e.web, e.c7)
}

func (e *autoError) Unwrap() error { return e.web }

// docsAccepting resolves name to Context7's top-ranked library and fetches
// its docs for query. accept, when set, vets the library first.
func (c *Context7) docsAccepting(ctx context.Context, name, query string, tokens int, accept func(LibraryMatch) error) ([]Result, error) {
	// Only the top-ranked library is used, so resolve just that one.
	libs, err := c.ResolveLibrary(ctx, name, 1)
	if err != nil {
		return nil, fmt.Errorf("context7 resolve failed: %w", err)
	}
	if len(libs) == 0 {
		return nil, fmt.Errorf("context7: no library found for %q", name)
	}
	if accept != nil {
		if err := accept(libs[0]); err != nil {
			return nil, err
		}
	}
	return c.GetDocs(ctx, libs[0].ID, query, tokens)
}

// isContext7ID reports a Context7 library ID: /owner/repo, optionally with
// a version segment. No web library has that shape (a docs URL has a
// scheme, a spec no leading slash).
func isContext7ID(s string) bool {
	if !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") {
		return false
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, p := range parts {
		if p == "" || strings.ContainsAny(p, " \t?#:") {
			return false
		}
	}
	return true
}

// context7Name reads a library value web refused as what to ask Context7
// for: a bare name as is, and an owner/repo (github: prefix or not) as
// itself, to be matched exactly (exact). A docs URL, a version, another
// ecosystem's prefix or shape (npm:, @scope/pkg, a Go module path), or a
// malformed value is not handed over: Context7's matches carry no
// ecosystem or version to hold them to.
func context7Name(raw string) (name, exact string, ok bool) {
	raw = strings.TrimSpace(raw)
	if isHTTPURL(raw) {
		return "", "", false
	}
	spec, err := parseLibSpec(raw)
	if err != nil || spec.version != "" || spec.url != "" {
		return "", "", false
	}
	switch spec.eco {
	case ecoAny:
		return spec.name, "", true
	case ecoGitHub:
		return spec.name, spec.name, true
	}
	return "", "", false
}

// queryPinsLibrary reports a bare query that names its library with a docs
// URL, a version, or an ecosystem (a prefix such as npm:, or a scoped npm
// or Go module name). Context7 would read the words its own way and could
// answer for another version or project, so such a query stays web's. An
// unprefixed owner/repo is too weak a sign ("async/await") to count.
func queryPinsLibrary(q string) bool {
	for _, w := range strings.Fields(q) {
		w = cleanQueryWord(w)
		spec, ok := explicitSpec(w)
		if !ok {
			continue
		}
		weak := spec.url == "" && spec.version == "" && spec.eco == ecoGitHub && !strings.Contains(w, ":")
		if !weak {
			return true
		}
	}
	return false
}

// keepID keeps the matches whose ID is exactly /owner/repo (any version).
func keepID(matches []LibraryMatch, ownerRepo string) []LibraryMatch {
	var out []LibraryMatch
	for _, m := range matches {
		if c7OwnerRepo(m.ID) != "" && strings.EqualFold(c7OwnerRepo(m.ID), ownerRepo) {
			out = append(out, m)
		}
	}
	return out
}

// c7OwnerRepo is a Context7 ID's owner/repo, without a version segment;
// "" when the ID has no such shape.
func c7OwnerRepo(id string) string {
	parts := strings.Split(strings.Trim(id, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// webCandidates holds Context7 to web's candidates when web found the name
// ambiguous: its pick must be one of them (candidateFor), or the ambiguity
// stands. For any other refusal it imposes nothing (nil).
func webCandidates(webErr error) func(LibraryMatch) error {
	var amb *AmbiguousLibraryError
	if !errors.As(webErr, &amb) {
		return nil
	}
	return func(m LibraryMatch) error {
		if _, ok := candidateFor(m.ID, amb.Matches); ok {
			return nil
		}
		return fmt.Errorf("context7 picked %s, which is none of web's candidates", m.ID)
	}
}

// candidateFor finds the web candidate a Context7 ID is, by identity
// only, never by title:
//
//   - the ID's owner/repo is a github: package spec of the candidate
//     (/pywidget/widget is github:pywidget/widget);
//   - a crawled site, /websites/<root>, is the candidate's docs root or a
//     page under it, written as Context7 writes it, with dots and slashes
//     as underscores (/websites/react_dev_reference under https://react.dev/);
//   - the ID's repository is named for the candidate's docs host, when the
//     candidate's root is that host's whole site (/reactjs/react.dev for
//     https://react.dev/; never for a shared host such as docs.rs, whose
//     roots have paths).
//
// Anything else is no match: a false miss keeps the ambiguity error, a
// false match answers for the wrong project.
func candidateFor(id string, cands []LibraryMatch) (LibraryMatch, bool) {
	ownerRepo := c7OwnerRepo(id)
	if ownerRepo == "" {
		return LibraryMatch{}, false
	}
	owner, repo, _ := strings.Cut(ownerRepo, "/")
	for _, c := range cands {
		for _, p := range c.Packages {
			if gh, ok := strings.CutPrefix(p, "github:"); ok && strings.EqualFold(gh, ownerRepo) {
				return c, true
			}
		}
		u, err := url.Parse(c.ID)
		if err != nil || u.Hostname() == "" {
			continue
		}
		host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
		rootPath := strings.Trim(u.Path, "/")
		if strings.EqualFold(owner, "websites") {
			site := c7SiteName(host + "/" + rootPath)
			got := c7SiteName(repo)
			if got == site || strings.HasPrefix(got, site+"_") {
				return c, true
			}
			continue
		}
		if rootPath == "" && strings.EqualFold(repo, host) {
			return c, true
		}
	}
	return LibraryMatch{}, false
}

// c7SiteName writes a site path as Context7 names crawled sites: lower
// case, with dots, slashes and hyphens as underscores, none at the ends.
func c7SiteName(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer(".", "_", "/", "_", "-", "_").Replace(s)
	return strings.Trim(s, "_")
}

// genericNames are words a Context7 title or repository can be ("docs",
// "/react-native-google-signin/docs") that name no library on their own.
var genericNames = map[string]bool{
	"doc": true, "docs": true, "documentation": true, "api": true, "apis": true, "reference": true,
	"guide": true, "guides": true, "website": true, "site": true, "sdk": true, "library": true,
}

// namedBy holds Context7's pick for a bare query (or a bare name) to the
// words that name a library in it: its title, or its repository's name,
// must be the name of one of the query's readings (splitLibraryQuery: the
// words its syntax puts a library's name in, "react" in "useState in
// react", "react router" run together), or the bare name itself, compared
// without case, punctuation or a .js suffix. Context7 resolves a whole
// question fuzzily: "useState in react" can come back as a React Native
// sign-in library's docs, and docs for a project the query never named are
// worse than none. A word in the question's other places ("configure" in
// "how do I configure the themes") names nothing (decision-15), and a
// generic word ("docs", "api") never counts.
func namedBy(query string) func(LibraryMatch) error {
	words := map[string]bool{}
	splits := splitLibraryQuery(query)
	if fields := strings.Fields(query); len(fields) == 1 {
		splits = []querySplit{{spec: libSpec{raw: fields[0], name: fields[0]}}}
	}
	for _, s := range splits {
		if k := libraryKey(s.spec.name); k != "" && !genericNames[k] {
			words[k] = true
		}
	}
	return func(m LibraryMatch) error {
		names := []string{libraryKey(m.Title)}
		if owner, repo, ok := strings.Cut(c7OwnerRepo(m.ID), "/"); ok && !strings.EqualFold(owner, "websites") {
			names = append(names, libraryKey(repo))
		}
		for _, n := range names {
			if n != "" && !genericNames[n] && words[n] {
				return nil
			}
		}
		return fmt.Errorf("context7 picked %s (%q), a library the query does not name", m.ID, m.Title)
	}
}

// libraryKey is a name reduced for comparison: lower case, letters and
// digits only, without a trailing "js" (Next.js, nextjs and next match).
func libraryKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	k := b.String()
	if trimmed := strings.TrimSuffix(k, "js"); len(trimmed) >= 2 {
		k = trimmed
	}
	return k
}
