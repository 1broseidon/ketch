package docs

// The phase-2 contract (web_types.go plus the chunk stubs), pinned at
// compile time: a change to a signature another chunk calls fails to build
// here first. Owned by the lead; change it only with the contract. The
// references also keep the stubs in use until their callers land.

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"testing"
	"time"

	config "github.com/1broseidon/ketch/internal/configbase"
	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/1broseidon/ketch/search"
)

// Shared: web_types.go helpers and constants, web.go wiring.
var (
	_ siteSearcher              = (*search.Auto)(nil)
	_ siteSearcher              = siteSearchFunc(nil)
	_ func(versionSet) []string = versionSet.labels
	_ func(pkgInfo) string      = pkgInfo.spec
	_ func(repoKey) string      = repoKey.spec
	_ func(repoKey) bool        = repoKey.isZero
	_ []string                  = []string{versionsRTD, versionsMike, versionsSwitcher, versionsSiblings, versionsSitemap, versionsSphinx}
	_ []ecosystem               = []ecosystem{ecoAny, ecoNPM, ecoPyPI, ecoCrates, ecoGo, ecoGitHub}
	_                           = []float64{minTrust, strictMinTrust}
)

// Chunk A: inputs and sources.
var (
	_ func(string) (libSpec, error)                                         = parseLibSpec
	_ func(string) []querySplit                                             = splitLibraryQuery
	_ func(ecosystem, string) string                                        = normalizeName
	_ func(string) []string                                                 = nameVariants
	_ func(*http.Client) *pkgClient                                         = newPkgClient
	_ func(*pkgClient, context.Context, ecosystem, string) (pkgInfo, error) = (*pkgClient).lookup
	_ func(*pkgClient, context.Context, libSpec) []pkgLookup                = (*pkgClient).lookupAll
	_ func(*pkgClient, context.Context, string) ([]string, error)           = (*pkgClient).goVersions
	_ func(*pkgClient, context.Context, string, int) ([]string, error)      = (*pkgClient).searchGo
	_ func(*http.Client, func() string) *ghClient                           = newGHClient
	_ func(*ghClient, context.Context, repoKey) (repoInfo, error)           = (*ghClient).repo
	_ func(*ghClient, context.Context, string, int) ([]repoInfo, error)     = (*ghClient).searchRepos
	_ func(string) (repoKey, bool)                                          = parseRepoURL
	_ error                                                                 = (*ghRateLimitError)(nil)
	_ pkgSource                                                             = (*pkgClient)(nil)
	_ repoSource                                                            = (*ghClient)(nil)
)

// Chunk B: resolve core.
var (
	_ func(*Web, context.Context, *url.URL) (*siteProfile, error)            = (*Web).profile
	_ func([]byte, *url.URL, *url.URL) *siteProfile                          = parseProfile
	_ func(*siteProfile) []repoKey                                           = repoLinks
	_ func(*siteProfile) []siteLink                                          = docsNavLinks
	_ func(*url.URL, *siteProfile) []*url.URL                                = docsRootsFor
	_ func(string) string                                                    = registrableDomain
	_ func(string) bool                                                      = isNonDocsHost
	_ func(*url.URL) bool                                                    = isPublicWebURL
	_ func([]evidence) (float64, bool)                                       = score
	_ map[evidenceKind]float64                                               = trustPoints
	_ func(*Web, context.Context, libSpec, int) ([]webMatch, error)          = (*Web).resolve
	_ func(*Web, context.Context, libSpec, bool) (webMatch, error)           = (*Web).resolveOne
	_ func(*Web, context.Context, libSpec, *resolveLog) []*project           = (*Web).gatherProjects
	_ func(*Web, context.Context, *project, *resolveLog) []rootCandidate     = (*Web).candidateRoots
	_ func(*Web, context.Context, *project, rootCandidate) (webMatch, error) = (*Web).verifyRoot
	_ func(*Web, context.Context, string) []rootCandidate                    = (*Web).lastResortSearch
	_ func(string, []webMatch) (webMatch, error)                             = pickUnique
	_ func(webMatch) LibraryMatch                                            = webMatch.libraryMatch
)

// Chunk C: versions and adapters.
var (
	_ *regexp.Regexp                                                            = versionLabelRe
	_ func(webRoot) (string, int)                                               = rootVersion
	_ func(*Web, context.Context, webRoot, *siteProfile) versionSet             = (*Web).siteVersions
	_ func(*Web, context.Context, webRoot, *siteProfile) (versionSet, error)    = (*Web).rtdVersions
	_ func(*Web, context.Context, webRoot) ([]siteVersion, error)               = (*Web).mikeVersions
	_ func(*Web, context.Context, webRoot, *siteProfile) ([]siteVersion, error) = (*Web).switcherVersions
	_ func(webRoot, *siteProfile) []siteVersion                                 = siblingVersions
	_ func(string, []siteVersion) (siteVersion, bool)                           = matchVersion
	_ func(string, *url.URL) bool                                               = versionHolds
	_ func(string, string) bool                                                 = sameMajor
	_ func(pkgInfo, string, []string) (siteVersion, bool)                       = registryVersionRoot
	_ func(webRoot, *url.URL) (webRoot, bool)                                   = canonicalRoot
	_ func([]Result, webRoot, string)                                           = labelResults
	_ func(*url.URL, string, []string) (sphinxSite, bool)                       = detectSphinx
	_ func(*Web, context.Context, webRoot, sphinxSite) ([]webLink, error)       = (*Web).sphinxLinks
	_ func([]byte) (sphinxIndex, error)                                         = parseSearchIndex
	_ func([]byte) (string, string, bool)                                       = parseObjectsInvHeader
	_ func([]byte) sphinxOptions                                                = parseDocOptions
)

// Every evidence kind has a row in the points table.
func TestEvidenceKindsHavePoints(t *testing.T) {
	kinds := []evidenceKind{evPkgDocs, evPkgHomepage, evRepoHomepage, evNavChain, evRegistryDocs, evRelatedDomain,
		evLinksRepo, evLinksOwner, evLinksPackage, evRepoAgrees, evSiteNames, evRTDRepo, evArchived, evDeprecated, evNote}
	for _, k := range kinds {
		if _, ok := trustPoints[k]; !ok {
			t.Errorf("evidence kind %q has no trustPoints row", k)
		}
	}
	if len(trustPoints) != len(kinds) {
		t.Errorf("trustPoints has %d rows for %d kinds", len(trustPoints), len(kinds))
	}
}

// The specs a match lists as Packages are the ones parseLibSpec reads.
func TestContractSpecs(t *testing.T) {
	if got := (pkgInfo{eco: ecoPyPI, name: "click"}).spec(); got != "pypi:click" {
		t.Errorf("pkgInfo.spec = %q", got)
	}
	k := repoKey{owner: "pallets", name: "click"}
	if k.String() != "pallets/click" || k.spec() != "github:pallets/click" || k.isZero() || !(repoKey{}).isZero() {
		t.Errorf("repoKey helpers: %q %q", k.String(), k.spec())
	}
	vs := versionSet{versions: []siteVersion{{label: "6.1", current: true}, {label: "5.2"}}}
	if got := vs.labels(); len(got) != 2 || got[0] != "6.1" || got[1] != "5.2" {
		t.Errorf("labels = %v", got)
	}
}

// The contract's struct fields, all named once so they count as used until
// the chunks read them.
var _ = []any{
	libSpec{raw: "", url: "", eco: ecoAny, name: "", version: ""},
	querySplit{spec: libSpec{}, query: "", strict: false},
	pkgInfo{eco: ecoAny, name: "", version: "", summary: "", homepage: "", docsURL: "", repoURL: "", repo: repoKey{}, registryDocs: "", deprecated: false, downloads: 0},
	pkgLookup{eco: ecoAny, name: "", info: pkgInfo{}, err: nil},
	repoInfo{key: repoKey{}, homepage: "", description: "", fork: false, archived: false, parent: repoKey{}, stars: 0},
	siteLink{url: nil, text: ""},
	siteProfile{requested: nil, served: nil, title: "", description: "", meta: nil, links: nil, scripts: nil, inline: "", fromMarkdown: false},
	siteVersion{label: "", aliases: nil, root: "", current: false},
	versionSet{versions: nil, source: "", project: "", repoURL: ""},
	evidence{kind: evNote, detail: ""},
	project{key: "", pkgs: nil, repo: repoKey{}, aliases: nil, info: nil, notes: nil, exact: false, fromSearch: false},
	rootCandidate{url: nil, evidence: nil, registry: false, search: false},
	webMatch{root: webRoot{}, title: "", description: "", trust: 0, evidence: nil, packages: nil, version: "", versions: nil, project: "", stars: 0, primary: false},
	&resolveLog{rejected: nil, failed: nil},
	ghRateLimitError{reset: time.Time{}, authed: false},
	sphinxSite{root: nil, options: ""},
	sphinxIndex{docnames: nil, titles: nil, sections: nil, objects: nil},
	sphinxOptions{builder: "", version: "", linkSuffix: ""},
}

// The docs provider builds the backend with every resolution source, and
// stays side-effect free: LibraryBackends constructs it with an empty
// config, so construction may neither touch the network nor resolve the
// GitHub token (which can run `gh auth token`).
func TestWebFromConfigIsLazy(t *testing.T) {
	ft := testutil.NewFixtureTransport()
	testutil.SwapDefaultTransport(t, ft)
	t.Setenv("KETCH_GITHUB_TOKEN", "at-construction")

	p, _ := Lookup("web")
	if p.LibraryHelp == "" {
		t.Error("the web descriptor must say what it takes as a library")
	}
	s, err := p.New(&config.Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	w := s.(*Web)
	if w.pkgs == nil || w.repos == nil || w.sites == nil || w.resolveTimeout != webResolveTimeout {
		t.Fatalf("sources not wired: pkgs %v, repos %v, sites %v, resolve timeout %s", w.pkgs, w.repos, w.sites, w.resolveTimeout)
	}
	if calls := ft.Calls(); len(calls) != 0 {
		t.Errorf("construction made requests: %v", calls)
	}
	t.Setenv("KETCH_GITHUB_TOKEN", "at-first-use")
	gh := w.repos.(*ghClient)
	if got := gh.token(); got != "at-first-use" {
		t.Errorf("token = %q: it was resolved at construction, not on first use", got)
	}
	t.Setenv("KETCH_GITHUB_TOKEN", "later")
	if got := gh.token(); got != "at-first-use" {
		t.Errorf("token = %q: it must be resolved once per backend", got)
	}
	if lib := NewWeb(nil); lib.pkgs != nil || lib.repos != nil || lib.sites != nil {
		t.Error("NewWeb must leave the resolution sources unset")
	}
}
