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
// auto does: web first (keyless, no quota), then Context7 (keyless on its
// anonymous quota, or keyed). It is not a provider: it owns no endpoint or
// settings, and it is never the default until decision-10's accuracy gates
// pass.
const AutoBackend = "auto"

// BackendError is one provider's failure that the auto chain fell through.
type BackendError struct {
	Backend string
	Err     error
}

// Selection says which provider answered an auto call ("web" or
// "context7") and what failed before it.
type Selection struct {
	Backend  string
	Failures []BackendError
}

// SelectingSearcher is a docs backend that dispatches to one of several
// providers and reports which one answered. Only Auto implements it, so a
// caller that finds it reports the provider; any other backend keeps its
// output as it was.
type SelectingSearcher interface {
	Searcher
	LibraryResolver
	SearchSelect(ctx context.Context, query string, limit int) ([]Result, Selection, error)
	GetDocsSelect(ctx context.Context, libraryID, query string, tokens int) ([]Result, Selection, error)
	ResolveSelect(ctx context.Context, name string, limit int) ([]LibraryMatch, Selection, error)
}

// autoWeb is what Auto needs from the web backend.
type autoWeb interface {
	Searcher
	LibraryResolver
}

// Auto chains the web backend and Context7. Web answers first; Context7 is
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
	web autoWeb
	c7  *Context7
}

// NewAuto chains web and Context7.
func NewAuto(web autoWeb, c7 *Context7) *Auto { return &Auto{web: web, c7: c7} }

// newAutoFromConfig builds the chain from c: the web backend with its name
// resolution sources, and Context7 with c's key or none.
func newAutoFromConfig(c *config.Config, deps Deps) *Auto {
	return NewAuto(newWebFromConfig(c, deps), NewContext7(c.String("context7_api_key")))
}

// Search answers a bare query; see SearchSelect.
func (a *Auto) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	results, _, err := a.SearchSelect(ctx, query, limit)
	return results, err
}

// ResolveLibrary resolves a library name; see ResolveSelect.
func (a *Auto) ResolveLibrary(ctx context.Context, name string, limit int) ([]LibraryMatch, error) {
	matches, _, err := a.ResolveSelect(ctx, name, limit)
	return matches, err
}

// GetDocs fetches docs for a library; see GetDocsSelect.
func (a *Auto) GetDocs(ctx context.Context, libraryID, query string, tokens int) ([]Result, error) {
	results, _, err := a.GetDocsSelect(ctx, libraryID, query, tokens)
	return results, err
}

// SearchSelect answers a bare query from web, else from Context7 with the
// whole query, as `--backend context7` would, but only if the library
// Context7 picks is one the query names (namedBy) or, when web found the
// name ambiguous, one of web's candidates. A query naming its library with
// an ecosystem prefix, a version or a docs URL stays web's.
func (a *Auto) SearchSelect(ctx context.Context, query string, limit int) ([]Result, Selection, error) {
	results, err := a.web.Search(ctx, query, limit)
	if err == nil {
		return results, Selection{Backend: "web"}, nil
	}
	if ctx.Err() != nil || queryPinsLibrary(query) {
		return nil, Selection{}, err
	}
	accept := webCandidates(err)
	if accept == nil {
		accept = namedBy(query)
	}
	results, c7err := a.c7.search(ctx, query, limit, accept)
	return autoSecond(ctx, results, err, c7err)
}

// GetDocsSelect fetches docs for a library. A Context7 ID (/owner/repo, as
// an auto --resolve lists them) goes to Context7 directly. Anything else
// goes to web first; when web refuses a bare name or an owner/repo,
// Context7 resolves it (an owner/repo only to exactly that ID) and answers.
func (a *Auto) GetDocsSelect(ctx context.Context, libraryID, query string, tokens int) ([]Result, Selection, error) {
	if isContext7ID(libraryID) {
		results, err := a.c7.GetDocs(ctx, libraryID, query, tokens)
		if err != nil {
			return nil, Selection{}, err
		}
		return results, Selection{Backend: "context7"}, nil
	}
	results, err := a.web.GetDocs(ctx, libraryID, query, tokens)
	if err == nil {
		return results, Selection{Backend: "web"}, nil
	}
	name, exact, ok := context7Name(libraryID)
	if ctx.Err() != nil || !ok {
		return nil, Selection{}, err
	}
	accept := webCandidates(err)
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
	results, c7err := a.c7.docsFor(ctx, name, query, tokens, accept)
	return autoSecond(ctx, results, err, c7err)
}

// ResolveSelect lists web's matches for a name, else Context7's when web
// finds none or fails. Context7's matches carry Context7 IDs, which
// GetDocsSelect sends back to Context7.
func (a *Auto) ResolveSelect(ctx context.Context, name string, limit int) ([]LibraryMatch, Selection, error) {
	matches, err := a.web.ResolveLibrary(ctx, name, limit)
	if err == nil && len(matches) > 0 {
		return matches, Selection{Backend: "web"}, nil
	}
	if err == nil {
		err = fmt.Errorf("web: library %q %w", name, ErrNotFound)
	}
	c7name, exact, ok := context7Name(name)
	if ctx.Err() != nil || !ok {
		return nil, Selection{}, err
	}
	c7matches, c7err := a.c7.ResolveLibrary(ctx, c7name, limit)
	if c7err == nil && exact != "" {
		c7matches = keepID(c7matches, exact)
	}
	if c7err == nil && len(c7matches) == 0 {
		c7err = fmt.Errorf("context7: no library found for %q", c7name)
	}
	return autoSecond(ctx, c7matches, err, c7err)
}

// autoSecond finishes a call Context7 took over from web: Context7's
// answer with web's failure reported, or both failures.
func autoSecond[T any](ctx context.Context, out []T, webErr, c7err error) ([]T, Selection, error) {
	failures := []BackendError{{Backend: "web", Err: webErr}}
	if c7err == nil {
		return out, Selection{Backend: "context7", Failures: failures}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, Selection{Failures: failures}, fmt.Errorf("docs auto cancelled after web failed: %w", err)
	}
	return nil, Selection{Failures: append(failures, BackendError{Backend: "context7", Err: c7err})}, &autoError{web: webErr, c7: c7err}
}

// autoError is web's refusal and Context7's failure after it. It wraps
// web's error alone, so the call is classified as web's refusal was (an
// ambiguous name stays a validation error naming its candidates, a missing
// library not found); Context7's failure is in the message.
type autoError struct {
	web, c7 error
}

func (e *autoError) Error() string {
	return fmt.Sprintf("%v (context7 fallback: %v)", e.web, e.c7)
}

func (e *autoError) Unwrap() error { return e.web }

// docsFor resolves name to Context7's top-ranked library and fetches its
// docs for query. accept, when set, vets the library first.
func (c *Context7) docsFor(ctx context.Context, name, query string, tokens int, accept func(LibraryMatch) error) ([]Result, error) {
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
