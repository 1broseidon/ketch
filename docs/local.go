package docs

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/failure"
)

// The local docs backend answers from the local index (ADR-0007), with no
// network and no model, through the evidence pipeline (evidence.go): the
// question's terms gather a pool of sections by keywords over the
// library's full-text table (local_fts.go), the pool is cut into pieces,
// and the pieces that add the most are chosen and printed, verbatim and
// grouped by section, within about 1,800 tokens or the caller's budget.
//
// An index holds one version of a library; several versions are indexed
// side by side. A library value with a version (name@3) answers from that
// version's index or is refused as not_indexed, naming the versions that
// are; one without answers from the newest indexed version, which the
// results' Version names.

// ErrNotIndexed is a library the local docs index does not hold: a
// refusal the caller fixes by indexing it.
var ErrNotIndexed = failure.New(failure.NotFound, "not_indexed", "library not indexed (ketch docs index <library|docs-url>)")

// ErrIndexOutdated is a library indexed in an older index format, which
// this ketch does not read: the operator rebuilds it.
var ErrIndexOutdated = failure.New(failure.Precondition, "index_outdated", "library indexed in an older format; rebuild it with ketch docs index")

// outdatedErr says which library to rebuild.
func outdatedErr(lib docsindex.Library) error {
	return fmt.Errorf("local: %s was indexed in format %d, which this ketch no longer reads (format %d); rebuild it with ketch docs index %s: %w",
		lib.Root, lib.Format, docsindex.FormatVersion, lib.Root, ErrIndexOutdated)
}

// Local answers from the local docs index.
type Local struct {
	store *docsindex.Store // nil when there is no cache directory
}

// NewLocal returns the backend over store (OpenIndex).
func NewLocal(store *docsindex.Store) *Local {
	return &Local{store: store}
}

// newLocalFromConfig is the backend over the default index file. It does
// no I/O.
func newLocalFromConfig() *Local {
	var store *docsindex.Store
	if path, err := docsindex.DefaultPath(); err == nil {
		store = OpenIndex(path)
	}
	return NewLocal(store)
}

// withVersionNote adds a note naming the other indexed versions to an
// answer from the newest.
func withVersionNote(o Outcome, note string) Outcome {
	if note == "" || (o.Status != Found && o.Status != Partial) {
		return o
	}
	if o.Note != "" {
		o.Note += "; "
	}
	o.Note += note
	return o
}

// libraries lists the indexed libraries; none when the index file does not
// exist (a stat, no open). A library an earlier ketch stored as a
// question's by-product (BuiltBySearch) is never read.
func (l *Local) libraries() ([]docsindex.Library, error) {
	if l.store == nil || !l.store.Exists() {
		return nil, nil
	}
	libs, err := l.store.Libraries()
	if err != nil {
		return nil, fmt.Errorf("local: reading the docs index: %w", err)
	}
	return Readable(libs), nil
}

// answer answers query from lib within opts' budget.
func (l *Local) answer(ctx context.Context, lib docsindex.Library, query string, opts LocalOptions) Outcome {
	if !lib.Current() {
		return Answer(SearchBackend, nil, outdatedErr(lib))
	}
	t := TraceFrom(ctx)
	t.ranking(lib, opts)
	a, err := l.evidence(lib, query, opts, t)
	if err != nil {
		return Answer(SearchBackend, nil, fmt.Errorf("local: reading the docs index: %w", err))
	}
	var o Outcome
	switch {
	case a.status == Refused && a.reason == ReasonScopeEmpty:
		o = Answer(SearchBackend, nil, fmt.Errorf("local: no indexed page of %s is at or under %s: %w", lib.Root, a.scope, ErrScopeEmpty))
		o.Note = fmt.Sprintf("no indexed page of %s is at or under %s; library %s answers from all of it", lib.Root, a.scope, lib.Root)
	case a.status == Refused && a.reason == ReasonTermsOutsideScope:
		o = Answer(SearchBackend, nil, fmt.Errorf("local: no page of %s under %s holds the question's distinctive terms (%s), which the library holds elsewhere: %w",
			lib.Root, a.scope, strings.Join(a.outside, ", "), ErrTermsOutsideScope))
	case a.status == Refused:
		o = Answer(SearchBackend, nil, fmt.Errorf("local: the docs of %s hold none of the question's distinctive terms (%s): %w",
			lib.Root, strings.Join(a.notFound, ", "), ErrTermsNotFound))
	case len(a.results) == 0 && opts.read > 0:
		return Answer(SearchBackend, nil, fmt.Errorf("search: nothing in the %d pages read of %s matched: %w", opts.read, lib.Root, ErrNoResults))
	case len(a.results) == 0:
		return Answer(SearchBackend, nil, fmt.Errorf("local: nothing in the index of %s matched: %w", lib.Root, ErrNoResults))
	default:
		showUnder(a.results, lib)
		pointToSections(a.results)
		o = Answer(SearchBackend, a.results, nil)
		if a.status == Partial {
			o.Status, o.Reason = Partial, a.reason
			for i := range o.Attempts {
				o.Attempts[i].Status, o.Attempts[i].Reason = Partial, a.reason
			}
		}
	}
	o.Library, o.Index, o.Terms, o.NotFound = lib.Root, a.index, a.terms, a.notFound
	o.Scope, o.OutsideScope, o.DefinedIn = a.scope, a.outside, a.definedIn
	return o
}

// showUnder shows a re-rooted current release's results at the
// unversioned addresses its pages name canonical (Library.DisplayRoot).
func showUnder(results []Result, lib docsindex.Library) {
	for i, r := range results {
		results[i].URL = shownURL(lib, r.URL)
	}
}

// pointToSections gives every result the reference to its section (its
// shown URL): what a follow-up fetch of a cut one takes. The answer says
// how, once (AnswerHint).
func pointToSections(results []Result) {
	for i, r := range results {
		results[i].Section = r.URL
	}
}

// webSectionOf is a stored section as the web backend's section. Its
// anchor was settled when it was indexed, so ranking never re-slugs it.
func webSectionOf(s docsindex.Section, i int) webSection {
	return webSection{page: i, order: s.Order, pageURL: s.Page, heading: s.Heading, crumbs: s.Crumbs, anchor: s.Anchor, source: anchorFixed, text: s.Text, links: s.Text}
}

// libraryInQuery finds the indexed library a bare query names. Readings
// are tried in the order splitLibraryQuery gives them; a reading that
// names no indexed library ends the search when it is a firm one (not a
// guess from joined words, not a common word), since that library is
// better answered live than another one answered locally.
func libraryInQuery(libs []docsindex.Library, query string) ([]docsindex.Library, bool) {
	if len(libs) == 0 {
		return nil, false
	}
	for _, s := range splitLibraryQuery(query) {
		if found := matchSpec(libs, s.spec); len(found) > 0 {
			return found, true
		}
		if !s.strict && !s.common {
			return nil, false
		}
	}
	return nil, false
}

// pickVersion is the indexed library a library value names. A value with
// a version names that version's index alone: when the library is indexed
// only at other versions, the refusal (not_indexed) names them. A value
// without one names the newest indexed version (newestOf), with a note
// naming the others.
func pickVersion(libs []docsindex.Library, value string) (docsindex.Library, string, error) {
	found := MatchIndexed(libs, value)
	if len(found) > 0 {
		lib, note := newestOf(found)
		return lib, note, nil
	}
	if name, version, ok := splitVersion(value); ok {
		if others := MatchIndexed(libs, name); len(others) > 0 {
			return docsindex.Library{}, "", fmt.Errorf("local: %s is not indexed at version %s; indexed versions: %s (ketch docs index %s@%s): %w",
				name, version, strings.Join(versionList(others), ", "), name, version, ErrNotIndexed)
		}
	}
	if near := nearIndexed(libs, value); len(near) > 0 {
		return docsindex.Library{}, "", fmt.Errorf("local: %q %w; %s", value, ErrNotIndexed, nearNote(value, near))
	}
	return docsindex.Library{}, "", fmt.Errorf("local: %q %w", value, ErrNotIndexed)
}

// splitVersion splits a library spec's version off (name@3 is name, 3).
func splitVersion(value string) (name, version string, ok bool) {
	if isHTTPURL(value) {
		return "", "", false
	}
	spec, err := parseLibSpec(strings.TrimSpace(value))
	if err != nil || spec.version == "" {
		return "", "", false
	}
	name = spec.raw
	if i := strings.LastIndex(spec.raw, "@"); i > 0 {
		name = spec.raw[:i]
	}
	return name, spec.version, true
}

// versionList names the indexed libraries' versions, newest first, an
// unlabelled one as "current".
func versionList(libs []docsindex.Library) []string {
	libs = slices.Clone(libs)
	sort.SliceStable(libs, func(i, j int) bool { return newer(libs[i], libs[j]) })
	var out []string
	for _, l := range libs {
		v := l.Version
		if v == "" {
			v = labelCurrent
		}
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// newestOf picks the newest of the indexed libraries a value names, and
// when there are several, a note naming the others and a Version that
// says which this is ("current" for the unlabelled current docs).
func newestOf(found []docsindex.Library) (docsindex.Library, string) {
	best := found[0]
	for _, l := range found[1:] {
		if newer(l, best) {
			best = l
		}
	}
	if len(found) == 1 {
		return best, ""
	}
	if best.Version == "" {
		best.Version = labelCurrent
	}
	var rest []docsindex.Library
	for _, l := range found {
		if l.Key != best.Key {
			rest = append(rest, l)
		}
	}
	return best, fmt.Sprintf("answered from version %s, the newest indexed; also indexed: %s (name@version picks one)", best.Version, strings.Join(versionList(rest), ", "))
}

// newer reports whether a is a newer version of the docs than b: the
// current docs (indexed without a version, at a root naming none, or
// labelled latest, stable or current) before any release, releases by
// number, development docs (next, main) last, and among equals the most
// recently built.
func newer(a, b docsindex.Library) bool {
	ca, cb := versionRank(a), versionRank(b)
	if ca != cb {
		return ca < cb
	}
	na, _ := releaseNumbers(normLabel(a.Version))
	nb, _ := releaseNumbers(normLabel(b.Version))
	if c := compareRelease(na, nb); c != 0 {
		return c > 0
	}
	return a.Built.After(b.Built)
}

// versionRank classes an indexed library's version for newer: 0 the
// current docs, 1 a release, 2 development docs.
func versionRank(l docsindex.Library) int {
	v := normLabel(l.Version)
	switch {
	case versionAliasClass[v] == "dev":
		return 2
	case v == "" || versionAliasClass[v] == "current" || v == labelCurrent || l.DisplayRoot != "":
		return 0
	case l.Requested == "" && rootVersionLabel(webRootOf(l.Root)) == "":
		return 0 // the docs a site serves as current, labelled with their release
	}
	return 1
}

// webRootOf parses a stored root (the zero root for one that does not
// parse).
func webRootOf(raw string) webRoot {
	r, _ := parseWebRoot(raw)
	return r
}

// MatchIndexed returns the indexed libraries a library value names: a
// docs URL at or under an indexed root (or an indexed root under it), a
// root key, or a name or spec one was indexed by ([eco:]name[@version];
// with a version, only libraries indexed at a version it names); last, a
// docs URL written without its scheme (auth0.com/docs) as that URL.
func MatchIndexed(libs []docsindex.Library, value string) []docsindex.Library {
	value = strings.TrimSpace(value)
	for _, l := range libs {
		if l.Key == value || l.Root == value {
			return []docsindex.Library{l}
		}
	}
	if isHTTPURL(value) {
		return matchSpec(libs, libSpec{raw: value, url: value})
	}
	spec, err := parseLibSpec(value)
	if err != nil {
		// Not a spec ("tanstack query"): a name it was indexed by, read
		// as a comparison key.
		spec = libSpec{raw: value, name: value}
	}
	found := matchSpec(libs, spec)
	if len(found) == 0 {
		// A docs URL written without its scheme (auth0.com/docs) that no
		// name or package matches: compared as the URL it names.
		if u, ok := bareURL(value); ok {
			return matchSpec(libs, libSpec{raw: u, url: u})
		}
	}
	return found
}

// bareURL is a value shaped host[/path] with no scheme (auth0.com/docs,
// react.dev) as the https URL it names: a host of two labels or more
// whose last is letters only, and no spaces, ecosystem prefix, scope or
// version.
func bareURL(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t@:") || strings.Contains(value, "://") {
		return "", false
	}
	host, _, _ := strings.Cut(value, "/")
	labels := strings.Split(strings.ToLower(host), ".")
	if len(labels) < 2 || slices.Contains(labels, "") {
		return "", false
	}
	tld := labels[len(labels)-1]
	if len(tld) < 2 || strings.Trim(tld, "abcdefghijklmnopqrstuvwxyz") != "" {
		return "", false
	}
	u := "https://" + value
	if !isHTTPURL(u) {
		return "", false
	}
	return u, true
}

// matchSpec returns the libraries spec names: by a docs URL at or under
// their root, or by a name (libraryNames), libraries whose full identity
// it is before those it names only by a secondary name. The names come
// from each library's metadata, never from the Names a build stored.
func matchSpec(libs []docsindex.Library, spec libSpec) []docsindex.Library {
	var out []docsindex.Library
	if spec.url != "" {
		u, err := parseWebRoot(spec.url)
		if err != nil {
			return nil
		}
		for _, l := range libs {
			r, err := parseWebRoot(l.Root)
			if err == nil && (r.contains(u.u) || u.contains(r.u)) {
				out = append(out, l)
			}
		}
		return out
	}
	keys := map[string]bool{libraryKey(spec.name): true}
	scope := ""
	if i := strings.LastIndex(spec.name, "/"); i >= 0 {
		keys[libraryKey(spec.name[i+1:])] = true
		if strings.HasPrefix(spec.name, "@") {
			scope = libraryKey(spec.name[1:i])
		}
	}
	delete(keys, "")
	out = matchNames(libs, spec, keys)
	if len(out) == 0 {
		// A package named for its major release (beautifulsoup4) goes by
		// its name without the number on its docs (BeautifulSoup).
		if stems := releaseStems(keys); len(stems) > 0 {
			out = matchNames(libs, spec, stems)
		}
	}
	if scope != "" {
		// A scoped package's last part names the framework it is for as
		// often as the package (react in @testing-library/react): a
		// library it names must go by the scope too (astro of
		// @astrojs/starlight at starlight.astro.build), unless the whole
		// name is its own.
		out = slices.DeleteFunc(out, func(l docsindex.Library) bool {
			primary, specific, host := libraryNameTiers(l)
			all := slices.Concat(primary, specific, host, hostNames(webRootOf(l.Root).u))
			return !slices.Contains(all, libraryKey(spec.name)) && !hasPackage(l.Packages, spec) &&
				!slices.ContainsFunc(all, func(n string) bool { return libraryKey(n) == scope })
		})
	}
	return withProject(libs, out, spec.version)
}

// matchNames returns the libraries keys name: those whose full identity
// it is (or whose package spec names), else those a name of their own
// names (a repository's last part, a root path segment), else those their
// host's label names. A secondary name answers only when no indexed
// library goes by it more closely.
func matchNames(libs []docsindex.Library, spec libSpec, keys map[string]bool) []docsindex.Library {
	var tiers [3][]docsindex.Library
	for _, l := range libs {
		if spec.version != "" && !versionNames(spec.version, l.Version) {
			continue
		}
		primary, specific, host := libraryNameTiers(l)
		switch {
		case anyNamed(primary, keys) || hasPackage(l.Packages, spec):
			tiers[0] = append(tiers[0], l)
		case anyNamed(specific, keys):
			tiers[1] = append(tiers[1], l)
		case anyNamed(host, keys):
			tiers[2] = append(tiers[2], l)
		}
	}
	for i, t := range tiers {
		if len(t) > 0 {
			if i == 2 && !oneProject(t) {
				// A host label several projects' docs share (palletsprojects
				// for Click and Flask) names none of them.
				return nil
			}
			return t
		}
	}
	return nil
}

// oneProject reports libraries that are all one project's docs, at one
// version or several (projectRoot).
func oneProject(libs []docsindex.Library) bool {
	for _, l := range libs[1:] {
		if projectRoot(l) != projectRoot(libs[0]) {
			return false
		}
	}
	return true
}

// releaseStems are the keys that end in a major release number, without
// it (beautifulsoup4: beautifulsoup), when at least five letters remain.
func releaseStems(keys map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range keys {
		stem := strings.TrimRight(k, "0123456789")
		if stem != k && len(k)-len(stem) == 1 && len(stem) >= 5 {
			out[stem] = true
		}
	}
	return out
}

// withProject adds to found every indexed library of the same project's
// docs at another version (projectRoot: v18.angular.dev beside
// angular.dev, /docs/1.4/ beside /docs/2.0/), so a name that one version's
// index was built by finds the current docs too and newestOf chooses; with
// a version asked for, only libraries at that version.
func withProject(libs, found []docsindex.Library, version string) []docsindex.Library {
	if len(found) == 0 {
		return found
	}
	projects := map[string]bool{}
	have := map[string]bool{}
	for _, l := range found {
		projects[projectRoot(l)] = true
		have[l.Key] = true
	}
	out := found
	for _, l := range libs {
		if !have[l.Key] && projects[projectRoot(l)] && (version == "" || versionNames(version, l.Version)) {
			out = append(out, l)
		}
	}
	return out
}

// projectRoot is a library's docs root with its version set aside: the
// host without a version label (v2.vuejs.org is vuejs.org) and the path
// without version segments (duckdb.org/docs/1.4/ is duckdb.org/docs/).
func projectRoot(l docsindex.Library) string {
	r, err := parseWebRoot(l.Root)
	if err != nil {
		return l.Root
	}
	host := scopeHost(r.u)
	if first, rest, ok := strings.Cut(host, "."); ok && strings.Contains(rest, ".") {
		if label, ok := variantLabel(first); ok && strings.ContainsAny(label, "0123456789") {
			host = rest
		}
	}
	var segs []string
	for _, seg := range pathSegments(r.prefix) {
		if _, version := segmentLabel(seg); !version {
			segs = append(segs, strings.ToLower(seg))
		}
	}
	return host + "/" + strings.Join(segs, "/")
}

// anyNamed reports whether any of names is a key in keys.
func anyNamed(names []string, keys map[string]bool) bool {
	for _, n := range names {
		if keys[n] {
			return true
		}
	}
	return false
}

// hasPackage reports a package spec of the library's (pypi:click) that
// spec names exactly, versions aside.
func hasPackage(pkgs []string, spec libSpec) bool {
	want, _, _ := strings.Cut(spec.raw, "@")
	if spec.eco == ecoAny || want == "" {
		return false
	}
	for _, p := range pkgs {
		if have, _, _ := strings.Cut(p, "@"); strings.EqualFold(have, want) {
			return true
		}
	}
	return false
}

// versionNames reports whether a requested version names an indexed one:
// the same label, or a prefix of its release (5 names 5.2).
func versionNames(want, have string) bool {
	if have == "" {
		return false
	}
	want, have = strings.TrimPrefix(strings.ToLower(want), "v"), strings.TrimPrefix(strings.ToLower(have), "v")
	return want == have || strings.HasPrefix(have, want+".")
}

// probeIndex reports what the local index holds, without the network.
func probeIndex(_ context.Context, _ *http.Client, _ *config.Config) (health.Status, string) {
	path, err := docsindex.DefaultPath()
	if err != nil {
		return health.StatusMisconfigured, err.Error()
	}
	store := OpenIndex(path)
	legacy := ""
	if note := docsindex.LegacyNote(path); note != "" {
		legacy = "; " + note
	}
	if !store.Exists() {
		return health.StatusSkipped, "nothing indexed yet" + legacy
	}
	libs, err := store.Libraries()
	if err != nil {
		return health.StatusMisconfigured, fmt.Sprintf("cannot read %s: %v", path, err)
	}
	return health.StatusOK, fmt.Sprintf("%d libraries indexed in %s%s", len(Readable(libs)), path, legacy)
}
