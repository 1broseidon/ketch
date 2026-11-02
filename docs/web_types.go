package docs

// Contract for phase 2 of the web backend: resolving a library name or spec
// to its project, docs site and version. Owned by the lead; chunks A (specs,
// registries, GitHub), B (resolution, trust, wiring) and C (versions,
// framework adapters) build against these types and change them only by
// agreement.

import (
	"context"
	"net/url"

	"github.com/1broseidon/ketch/search"
)

// ecosystem names a package registry a library spec can point at.
type ecosystem string

const (
	// ecoAny is a bare name: looked up in every registry where it is a valid
	// package name.
	ecoAny    ecosystem = ""
	ecoNPM    ecosystem = "npm"
	ecoPyPI   ecosystem = "pypi"
	ecoCrates ecosystem = "crates" // also written cargo:
	ecoGo     ecosystem = "go"
	// ecoGitHub is a repository, not a registry: github:owner/repo, or a
	// bare owner/repo.
	ecoGitHub ecosystem = "github"
)

// libSpec is a parsed --library, --resolve or MCP library value: a docs URL,
// [eco:]name[@version], owner/repo, or a Go module path.
type libSpec struct {
	raw string // as given, trimmed
	// url is set, and nothing else but raw, when the spec is an HTTP(S) URL.
	url string
	// eco is the explicit prefix, or the ecosystem the name's shape implies
	// (@scope/pkg is npm, a module path is go, owner/repo is github); ecoAny
	// for a bare name.
	eco ecosystem
	// name is the package name as given (normalizeName gives each
	// registry's form), the module path for go, owner/repo for github.
	name string
	// version is the part after the last "@" at index > 0; "" for none.
	version string
}

// querySplit is one reading of a bare docs query as a library plus the
// question to search it for, from splitLibraryQuery.
type querySplit struct {
	spec  libSpec
	query string // the rest of the query, searched within the library
	// strict asks for stronger proof before the candidate is accepted: an
	// exact registry name, a verified site and trust >= 5. Set for
	// candidates guessed by joining two query words ("react router").
	strict bool
}

// repoKey identifies a GitHub repository. Both parts are lower-cased, so
// keys compare with ==.
type repoKey struct {
	owner, name string
}

// isZero reports a key that names no repository.
func (k repoKey) isZero() bool { return k.owner == "" && k.name == "" }

// String is "owner/name".
func (k repoKey) String() string { return k.owner + "/" + k.name }

// spec is the key as a library spec: "github:owner/name".
func (k repoKey) spec() string { return string(ecoGitHub) + ":" + k.String() }

// pkgInfo is one registry's metadata for one package.
type pkgInfo struct {
	eco     ecosystem
	name    string // as the registry spells it
	version string // latest stable version
	summary string
	// homepage, docsURL and repoURL are the metadata's URLs as published:
	// third-party input, not yet checked with isPublicWebURL. docsURL is a
	// URL the metadata labels as documentation (PyPI Documentation keys,
	// crates documentation); homepage may well be a GitHub page.
	homepage, docsURL, repoURL string
	repo                       repoKey // parsed from repoURL; zero when it is not a GitHub repo
	// registryDocs is the docs the registry hosts for the package:
	// https://docs.rs/<crate>/latest/<ident>/, https://pkg.go.dev/<module>;
	// "" for registries that host none.
	registryDocs string
	deprecated   bool
	downloads    int64 // recent downloads where the registry reports them (crates); 0 when unknown
}

// spec is the package as a library spec: "pypi:click".
func (p pkgInfo) spec() string { return string(p.eco) + ":" + p.name }

// pkgLookup is one registry's answer for one name, from pkgSource.lookupAll.
type pkgLookup struct {
	eco  ecosystem
	name string  // the name as queried, in the registry's normal form
	info pkgInfo // set when err is nil
	// err wraps ErrNotFound when the registry has no such package; any other
	// error is a failed source (5xx, network, unreadable body), kept so the
	// final error can be classified.
	err error
}

// repoInfo is a GitHub repository's metadata.
type repoInfo struct {
	key repoKey // canonical, after following renames
	// homepage is the repository's website field as published; it may lack
	// a scheme (clap's is "docs.rs/clap").
	homepage       string
	description    string
	fork, archived bool
	parent         repoKey // the fork's upstream; zero unless fork
	stars          int
	// language is GitHub's primary language for the repository
	// ("TypeScript"), "" when it names none.
	language string
}

// pkgSource looks packages up in the registries. Implemented by *pkgClient
// (chunk A); resolution tests use in-memory fakes.
type pkgSource interface {
	// lookup fetches one package from eco's registry (not ecoAny or
	// ecoGitHub). A package the registry does not have is an error wrapping
	// ErrNotFound.
	lookup(ctx context.Context, eco ecosystem, name string) (pkgInfo, error)
	// lookupAll looks spec up concurrently in every registry it allows: the
	// prefixed or implied one, or for a bare name each registry where it is
	// a valid name. It returns one entry per registry asked, in a stable
	// order, deprecated packages included.
	lookupAll(ctx context.Context, spec libSpec) []pkgLookup
	// goVersions lists a Go module's published versions (the proxy's
	// @v/list), newest first.
	goVersions(ctx context.Context, module string) ([]string, error)
	// searchGo returns module paths pkg.go.dev's search finds for name,
	// most imported first, at most n. Optional: an implementation may
	// return nil, nil.
	searchGo(ctx context.Context, name string, n int) ([]string, error)
}

// repoSource reads GitHub. Implemented by *ghClient (chunk A).
type repoSource interface {
	// repo fetches a repository, following renames, so info.key may differ
	// from key. A missing repository is an error wrapping ErrNotFound; a
	// rate limit is a *ghRateLimitError.
	repo(ctx context.Context, key repoKey) (repoInfo, error)
	// searchRepos returns repositories whose name matches name, most stars
	// first, at most n. Rate limits as for repo. A search GitHub cut short
	// returns the hits it has with an error wrapping errIncompleteSearch.
	searchRepos(ctx context.Context, name string, n int) ([]repoInfo, error)
}

// siteSearcher is the web search used as the last resort for finding a docs
// site; *search.Auto satisfies it.
type siteSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]search.Result, error)
}

// siteLink is an anchor on a page, resolved to an absolute URL with no
// fragment.
type siteLink struct {
	url  *url.URL
	text string // the anchor's text, else its aria-label or title, space-collapsed
}

// siteProfile is what one fetch of a candidate docs root says about the
// site: built by (*Web).profile (chunk B), read by verification and by
// siteVersions (chunk C).
type siteProfile struct {
	requested *url.URL
	served    *url.URL // after redirects, as the Fetcher reported it
	title     string
	// description is the meta description, else og:description.
	description string
	// meta maps lower-cased <meta> name or property attributes to their
	// content: generator, og:site_name, readthedocs-project-slug,
	// readthedocs-version-slug.
	meta map[string]string
	// links is every anchor on the page, on any host, in document order,
	// one per URL.
	links []siteLink
	// scripts are the absolute src URLs of the page's <script> elements.
	scripts []string
	// inline is the text of the page's inline scripts, joined and capped:
	// where version switchers keep their JSON URL.
	inline string
	// fromMarkdown is set when the raw page had no usable anchors (a JS
	// shell) and links came from FetchPage's Markdown instead; meta,
	// scripts and inline are then empty.
	fromMarkdown bool
}

// siteVersion is one version a docs site publishes.
type siteVersion struct {
	label   string   // concrete where known: "6.1", "8.5.x", "v2"
	aliases []string // other labels for the same docs: "stable", "latest"
	root    string   // absolute URL of this version's docs root
	current bool     // the version the site serves as current
}

// Version sources, for versionSet.source.
const (
	versionsRTD      = "readthedocs" // Read the Docs addons API
	versionsMike     = "mike"        // mike versions.json
	versionsSwitcher = "switcher"    // pydata-sphinx-theme switcher JSON
	versionsSiblings = "siblings"    // sibling version links on the root page
	versionsSitemap  = "sitemap"     // Read the Docs version-root sitemap
	versionsSphinx   = "sphinx"      // the build's own VERSION, current only
)

// versionSet is what a docs site says about its versions, from
// (*Web).siteVersions (chunk C), with the project identity some of the same
// sources report.
type versionSet struct {
	versions []siteVersion // current first; nil when the site publishes none
	source   string        // a versions* constant; "" when versions is nil
	// project is the name the docs build declares (Read the Docs project
	// name, objects.inv Project:); "" when no source said.
	project string
	// repoURL is the repository Read the Docs records for the project, as
	// published; "" when unknown.
	repoURL string
}

// labels returns the version labels, current first, for LibraryMatch.Versions.
func (s versionSet) labels() []string {
	var out []string
	for _, v := range s.versions {
		out = append(out, v.label)
	}
	return out
}

// evidenceKind names one kind of finding that ties a docs root to a project.
// Its points are in web_trust.go's trustPoints.
type evidenceKind string

const (
	evPkgDocs       evidenceKind = "pkg-docs"       // package metadata lists the root as documentation (V1)
	evPkgHomepage   evidenceKind = "pkg-homepage"   // the package homepage is the root's site (V1)
	evRepoHomepage  evidenceKind = "repo-homepage"  // the repository homepage is the root's site (V1)
	evNavChain      evidenceKind = "nav-chain"      // the root is a homepage's docs link or docs. host (V2)
	evRegistryDocs  evidenceKind = "registry-docs"  // registry-hosted docs for the exact package (V3)
	evRelatedDomain evidenceKind = "related-domain" // same registrable domain, or <owner>.github.io (V4, needs evLinksRepo)
	evLinksRepo     evidenceKind = "links-repo"     // the root links to the canonical repo or an alias
	evLinksOwner    evidenceKind = "links-owner"    // the root links to another repo of the same owner
	evRepoAgrees    evidenceKind = "repo-agrees"    // the package's repository field is the canonical repo
	evSiteNames     evidenceKind = "site-names"     // the site names the project (llms.txt H1, objects.inv, title)
	evRTDRepo       evidenceKind = "rtd-repo"       // Read the Docs records the canonical repo
	evArchived      evidenceKind = "archived"       // the repository is archived
	evDeprecated    evidenceKind = "deprecated"     // the package is deprecated
	evNote          evidenceKind = "note"           // no points: a degraded source or an assumption
)

// evidence is one finding about a docs root; detail is the human-readable
// text shown in LibraryMatch.Evidence ("pypi click lists it as
// Documentation"), naming its source so equal findings from different
// sources stay distinct.
type evidence struct {
	kind   evidenceKind
	detail string
}
