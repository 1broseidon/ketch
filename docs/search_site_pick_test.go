package docs

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/search"
)

// The site a question names by its domain goes first over third-party
// pages about it that the search weighs more; a standards body's site
// goes first when the question names no site; neither is a refusal.
func TestOwnerAndSpecFirst(t *testing.T) {
	hits := docsHits(fuseHits([][]search.Result{{
		hit("https://docs.other.test/integrations/acme-logs", "Integrate Acme logs - Other docs"),
		hit("https://docs.other.test/integrations/acme-setup", "Acme setup - Other docs"),
		hit("https://docs.third.test/acme", "Acme - Third docs"),
		hit("https://acme.test/docs/adminapi", "Admin API - Acme docs"),
	}}))
	sites := chooseSites(hits, sitePrefs{words: questionWords("how do I read logs from the acme api")})
	if sites[0].key != "acme.test" || !sites[0].owner {
		t.Fatalf("site %s (owner %v), want acme.test, whose domain the question names", sites[0].key, sites[0].owner)
	}
	spec := docsHits(fuseHits([][]search.Result{{
		hit("https://docs.vendor.test/pkce", "PKCE - Vendor docs"),
		hit("https://docs.other.test/oauth/pkce", "PKCE - Other docs"),
		hit("https://www.rfc-editor.org/rfc/rfc7636", "RFC 7636: Proof Key for Code Exchange"),
	}}))
	sites = chooseSites(spec, sitePrefs{words: questionWords("how to generate the pkce code challenge")})
	if sites[0].key != "rfc-editor.org" {
		t.Fatalf("site %s, want the spec's site", sites[0].key)
	}
	if _, err := pickSite(sites); err != nil {
		t.Errorf("pickSite: %v; the spec is no ambiguity", err)
	}
	sites = chooseSites(spec, sitePrefs{words: questionWords("how to generate the pkce code challenge in vendor")})
	if sites[0].key != "vendor.test" {
		t.Errorf("a question naming the vendor: site %s, want vendor.test", sites[0].key)
	}
}

func TestOwnsDomain(t *testing.T) {
	words := questionWords("python requests retry with backoff in poetry and acme")
	for key, want := range map[string]bool{
		"acme.test": true, "requests.readthedocs.io": true, "python-poetry.org": true,
		"retrying.test": false, "python.org": false, "other.test": false,
	} {
		if got := ownsDomain(key, words); got != want {
			t.Errorf("ownsDomain(%s) = %v, want %v", key, got, want)
		}
	}
}

// A domain's docs trees are tried in turn: the main docs over an
// enterprise server's copy, which stays an alternative; a variant's hits
// alone give its main docs first.
func TestTreesAndVariants(t *testing.T) {
	hits := docsHits(fuseHits([][]search.Result{{
		hit("https://docs.lib.test/en/enterprise-server@3.22/actions/syntax", "Syntax - Lib docs"),
		hit("https://docs.lib.test/en/enterprise-server@3.22/actions/events", "Events - Lib docs"),
		hit("https://docs.lib.test/en/actions/syntax", "Syntax - Lib docs"),
		hit("https://help.lib.test/s/article/1", "Lib help"),
	}}))
	sites := chooseSites(hits, sitePrefs{words: questionWords("lib actions syntax")})
	if got := sites[0].root.String(); got != "https://docs.lib.test/en/" {
		t.Fatalf("root %s, want the main docs", got)
	}
	var alts []string
	for _, a := range sites[0].alts {
		alts = append(alts, a.root.String())
	}
	if len(alts) != 2 || alts[1] != "https://help.lib.test/" && alts[0] != "https://help.lib.test/" {
		t.Errorf("alternatives %q, want the enterprise tree and the help tree", alts)
	}
	sites = chooseSites(hits, sitePrefs{words: questionWords("lib enterprise server actions syntax")})
	if got := sites[0].root.String(); got != "https://docs.lib.test/en/enterprise-server@3.22/" {
		t.Errorf("enterprise asked: root %s, want the enterprise tree", got)
	}
	only := docsHits(fuseHits([][]search.Result{{
		hit("https://docs.lib.test/en/enterprise-server@3.22/actions/syntax", "Syntax - Lib docs"),
	}}))
	sites = chooseSites(only, sitePrefs{})
	if got := sites[0].root.String(); got != "https://docs.lib.test/en/" || sites[0].hits[0].u.String() != "https://docs.lib.test/en/actions/syntax" {
		t.Errorf("variant hits only: root %s, top hit %s; want the main docs' copy first", got, sites[0].hits[0].u)
	}
	if len(sites[0].alts) != 1 || sites[0].alts[0].root.String() != "https://docs.lib.test/en/enterprise-server@3.22/" {
		t.Errorf("variant hits only: the variant tree is the alternative")
	}
	if _, err := pickSite([]sfSite{{retry: true}, {score: 9}}); errors.Is(err, ErrAmbiguousSite) {
		t.Error("another tree of a site read before is no ambiguity")
	}
}

// Ranking: a legacy edition of a page waits behind the current one, a
// failure question favours troubleshooting sections, and docs platforms'
// boilerplate is no section.
func TestLegacyTroubleBoilerplate(t *testing.T) {
	secs := []docsindex.Section{
		{Page: "https://lib.test/docs/adminapi", Crumbs: []string{"Admin API"}},
		{Page: "https://lib.test/docs/adminapi-v1", Crumbs: []string{"Admin API v1"}},
		{Page: "https://lib.test/docs/authapi-v2", Crumbs: []string{"Auth API"}},
		{Page: "https://lib.test/docs/authapi-v3", Crumbs: []string{"Auth API"}},
		{Page: "https://lib.test/docs/sso-v2", Crumbs: []string{"SSO"}},
		{Page: "https://lib.test/docs/old-guide", Crumbs: []string{"Guide (Legacy)"}},
	}
	l := newLegacyPages(secs, queryTerms("read logs from the admin api"))
	for page, want := range map[string]bool{
		"https://lib.test/docs/adminapi": false, "https://lib.test/docs/adminapi-v1": true,
		"https://lib.test/docs/authapi-v2": true, "https://lib.test/docs/authapi-v3": false,
		"https://lib.test/docs/sso-v2": false, "https://lib.test/docs/old-guide": true,
	} {
		if l[page] != want {
			t.Errorf("legacy %s = %v, want %v", page, l[page], want)
		}
	}
	if newLegacyPages(secs, queryTerms("admin api v1 logs"))["https://lib.test/docs/adminapi-v1"] {
		t.Error("a question about v1 asks for the v1 page")
	}
	for q, want := range map[string]bool{
		"kubernetes pod keeps restarting what do I do": true, "nginx reload not working": true,
		"lambda error Task timed out": true, "how do I configure a pod": false,
	} {
		if troubleQuery(q) != want {
			t.Errorf("troubleQuery(%q) = %v", q, !want)
		}
	}
	if !troubleSection(docsindex.Section{Page: "https://k.test/docs/tasks/debug/debug-application/debug-pods/"}) {
		t.Error("a debugging page is a troubleshooting section")
	}
	for crumbs, want := range map[string]bool{"Page|Agent Instructions|Querying This Documentation": true, "Page|Was this page helpful?": true, "Page|Logs": false} {
		if got := boilerplateSection(splitCrumbs(crumbs)); got != want {
			t.Errorf("boilerplateSection(%s) = %v", crumbs, got)
		}
	}
}

func splitCrumbs(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '|' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

func TestUnreadShell(t *testing.T) {
	if !unreadShell(&webPage{shell: true, Markdown: "Loading"}) {
		t.Error("a shell's placeholder is no page")
	}
	if unreadShell(&webPage{Markdown: "Loading"}) {
		t.Error("a plain page is read whatever its length")
	}
}

// A docs tree whose pages are JS shells no browser renders gives nothing:
// the same domain's other docs tree is read before another site's pages
// about it.
func TestSearchFirstTriesTheDomainsNextTree(t *testing.T) {
	f := exampleSite()
	for _, p := range []string{"1", "2", "3"} {
		f.pages["https://help.example.test/s/article/"+p] = fakePage{title: "Knowledge Base", markdown: "Loading", shell: true}
	}
	f.pages["https://docs.vendor.test/integrations/example"] = fakePage{title: "Example integration", markdown: "# Example integration\n\n## Add middleware to a route\n\nOur product adds middleware to a route of Example for you.\n"}
	hits := []search.Result{
		hit("https://help.example.test/s/article/1", "Add middleware - Example help"),
		hit("https://help.example.test/s/article/2", "Middleware - Example help"),
		hit("https://docs.vendor.test/integrations/example", "Example integration - Vendor docs"),
		hit("https://help.example.test/s/article/3", "Routes - Example help"),
		hit(idxRoot+"guide/middleware", "Middleware - Example docs"),
	}
	s, _, _, _ := searchFirstOver(t, f, hits)
	o := s.SearchSelect(context.Background(), "add middleware to a route in example", 0)
	if len(o.Results) == 0 || !strings.HasPrefix(o.Results[0].URL, idxRoot) {
		t.Fatalf("outcome %s (%v), results %+v; want the example.test docs tree", o.Status, o.Err(), o.Results)
	}
}

// A remembered question answers only the same question: a more specific
// follow-up (one more term) is searched.
func TestSearchFirstMemoryNeedsTheSameTerms(t *testing.T) {
	s, _, searches, _ := searchFirstOver(t, exampleSite(), exampleHits())
	s.localFloor = 2
	s.local.model = nil // keywords only: questions compared by their terms
	if o := s.SearchSelect(context.Background(), "add logger middleware to a route", 0); o.Status != Partial {
		t.Fatalf("first answer = %s (%v)", o.Status, o.Err())
	}
	before := searches.Load()
	if s.SearchSelect(context.Background(), "how do I add logger middleware to a route?", 0); searches.Load() != before {
		t.Error("the same terms in other filler searched again")
	}
	// Four of five terms in common (0.8) was once the same question.
	if s.SearchSelect(context.Background(), "add logger middleware to a route group", 0); searches.Load() == before {
		t.Error("a more specific follow-up was answered from memory")
	}
}

// A top hit served from another domain takes that domain's hits along.
func TestServedSiteTakesTheServedDomainsHits(t *testing.T) {
	hits := docsHits(fuseHits([][]search.Result{{
		hit("https://tool.test/docs/import/usage.html", "Import - Tool docs"),
		hit("https://developer.corp.test/tool/cli/import/usage", "Import - Tool docs"),
		hit("https://developer.corp.test/tool/cli/state", "State - Tool docs"),
		hit("https://developer.corp.test/validated/tool/import", "Import - Tool docs"),
	}}))
	sites := chooseSites(hits, sitePrefs{words: questionWords("tool import existing resource")})
	if sites[0].key != "tool.test" {
		t.Fatalf("site %s, want tool.test, which the question owns", sites[0].key)
	}
	served, _ := url.Parse("https://developer.corp.test/tool/cli/import/usage")
	site, rest := servedSite(sites[0], &siteProfile{served: served}, sites[1:])
	if site.key != "corp.test" || len(site.hits) != 3 || len(rest) != 0 {
		t.Errorf("served site %s with %d hits, %d sites left; want corp.test with every hit", site.key, len(site.hits), len(rest))
	}
}
