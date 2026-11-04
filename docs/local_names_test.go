package docs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/internal/embed"
)

// Indexed libraries as a build stores them (the metadata names come from).
var (
	vueRouterLib = docsindex.Library{Key: "https://router.vuejs.org/", Root: "https://router.vuejs.org/", Library: "vue-router", Title: "Vue Router",
		Packages: []string{"npm:vue-router", "github:vuejs/router"}, Version: "5.4.0", Format: docsindex.FormatVersion}
	vueRouter3Lib = docsindex.Library{Key: "https://v3.router.vuejs.org/", Root: "https://v3.router.vuejs.org/", Library: "vue-router@3", Title: "Vue Router",
		Packages: []string{"npm:vue-router", "github:vuejs/vue-router"}, Version: "v3", Requested: "3", Format: docsindex.FormatVersion}
	vueLib = docsindex.Library{Key: "https://vuejs.org/", Root: "https://vuejs.org/", Library: "vue", Title: "Vue.js",
		Packages: []string{"npm:vue", "github:vuejs/core"}, Format: docsindex.FormatVersion}
	tanstackQueryLib = docsindex.Library{Key: "https://tanstack.com/query/latest/", Root: "https://tanstack.com/query/latest/", Library: "https://tanstack.com/query/latest/",
		Title: "TanStack Query", Packages: []string{"npm:@tanstack/react-query", "github:TanStack/query"}, Format: docsindex.FormatVersion}
	acmeLib = docsindex.Library{Key: "https://acme.example/", Root: "https://acme.example/", Library: "acme", Title: "Acme",
		Packages: []string{"pypi:acme", "github:acme/acme"}, Format: docsindex.FormatVersion}
	acmeRESTLib = docsindex.Library{Key: "https://acme.example/rest/", Root: "https://acme.example/rest/", Library: "acme-rest-framework", Title: "Acme REST framework",
		Packages: []string{"pypi:acme-rest-framework", "github:acme/acme-rest-framework"}, Format: docsindex.FormatVersion}
	honoLib = docsindex.Library{Key: "https://hono.dev/docs/", Root: "https://hono.dev/docs/", Library: "hono", Title: "Hono",
		Packages: []string{"npm:hono", "github:honojs/hono"}, Format: docsindex.FormatVersion}
	fastapiLib = docsindex.Library{Key: "https://fastapi.tiangolo.com/", Root: "https://fastapi.tiangolo.com/", Library: "fastapi", Title: "FastAPI - FastAPI",
		Packages: []string{"pypi:fastapi", "github:fastapi/fastapi"}, Format: docsindex.FormatVersion}
	astroLib = docsindex.Library{Key: "https://docs.astro.build/", Root: "https://docs.astro.build/", Library: "astro", Title: "Getting started",
		Packages: []string{"npm:astro", "github:withastro/astro"}, Format: docsindex.FormatVersion}
)

// A library's names are its full identity (packages with or without the
// ecosystem prefix and separators, repositories, the name it was indexed
// by, its docs host's distinctive label), never one word of a longer name
// it goes by: vue and router are not Vue Router, query and tanstack are
// not TanStack Query, acme, rest and framework are not Acme REST
// framework.
func TestLibraryNamesAreFullIdentities(t *testing.T) {
	for _, c := range []struct {
		lib      docsindex.Library
		has, not []string
	}{
		{vueRouterLib, []string{"vuerouter", "vuejsrouter"}, []string{"vue", "router", "vuejs"}},
		{tanstackQueryLib, []string{"tanstackreactquery", "reactquery", "tanstackquery"}, []string{"query", "tanstack", "react"}},
		{acmeRESTLib, []string{"acmerestframework", "acmeacmerestframework"}, []string{"acme", "rest", "framework"}},
		{honoLib, []string{"hono", "honojshono"}, nil},
		{fastapiLib, []string{"fastapi"}, nil},
		{astroLib, []string{"astro", "withastroastro"}, []string{"gettingstarted"}},
	} {
		primary, secondary := libraryNames(c.lib)
		names := append(primary, secondary...)
		for _, n := range c.has {
			if !slices.Contains(names, n) {
				t.Errorf("%s: names %v lack %s", c.lib.Root, names, n)
			}
		}
		for _, n := range c.not {
			if slices.Contains(names, n) {
				t.Errorf("%s: names %v include %s", c.lib.Root, names, n)
			}
		}
	}
}

// A value or bare query names an indexed library only by one of its full
// names: with only Vue Router indexed, "vue" names nothing (Vue's own docs
// are not indexed), and with Vue indexed too it names Vue; "acme" never
// names Acme REST framework; "tanstack query" names TanStack Query, while
// "query" names nothing. A secondary name (a host label) yields to a
// library whose full name it is. The real libraries still answer to their
// names.
func TestMatchIndexedNeverAnotherLibrary(t *testing.T) {
	keys := func(libs []docsindex.Library) []string {
		var out []string
		for _, l := range libs {
			out = append(out, l.Key)
		}
		return out
	}
	routers := []docsindex.Library{vueRouterLib, vueRouter3Lib}
	hostOnly := docsindex.Library{Key: "https://hono.example.test/", Root: "https://hono.example.test/", Library: "https://hono.example.test/", Format: docsindex.FormatVersion}
	for _, c := range []struct {
		libs  []docsindex.Library
		value string
		want  []string
	}{
		{routers, "vue", nil},
		{routers, "router", nil},
		{routers, "vue-router", []string{vueRouterLib.Key, vueRouter3Lib.Key}},
		{routers, "vue-router@3", []string{vueRouter3Lib.Key}},
		{routers, "npm:vue-router", []string{vueRouterLib.Key, vueRouter3Lib.Key}},
		{append(slices.Clone(routers), vueLib), "vue", []string{vueLib.Key}},
		{[]docsindex.Library{acmeRESTLib}, "acme", nil},
		{[]docsindex.Library{acmeRESTLib}, "rest", nil},
		{[]docsindex.Library{acmeLib, acmeRESTLib}, "acme", []string{acmeLib.Key}},
		{[]docsindex.Library{acmeLib, acmeRESTLib}, "acme-rest-framework", []string{acmeRESTLib.Key}},
		{[]docsindex.Library{tanstackQueryLib}, "tanstack query", []string{tanstackQueryLib.Key}},
		{[]docsindex.Library{tanstackQueryLib}, "@tanstack/react-query", []string{tanstackQueryLib.Key}},
		{[]docsindex.Library{tanstackQueryLib}, "query", nil},
		{[]docsindex.Library{tanstackQueryLib}, "tanstack", nil},
		{[]docsindex.Library{honoLib, hostOnly}, "hono", []string{honoLib.Key}},
		{[]docsindex.Library{hostOnly}, "hono", []string{hostOnly.Key}},
		{[]docsindex.Library{honoLib, fastapiLib, astroLib}, "fastapi", []string{fastapiLib.Key}},
		{[]docsindex.Library{honoLib, fastapiLib, astroLib}, "astro", []string{astroLib.Key}},
	} {
		if got := keys(MatchIndexed(c.libs, c.value)); !slices.Equal(got, c.want) {
			t.Errorf("%q among %v = %v, want %v", c.value, keys(c.libs), got, c.want)
		}
	}
	for q, want := range map[string]bool{
		"vue v-model on a custom component":       false,
		"two-way binding with v-model in vue":     false,
		"how do I use router guards":              false,
		"how do I push a route with vue-router":   true,
		"vue-router navigation guards":            true,
		"acme serializers":                        false,
		"how do I fetch data with tanstack query": true,
		"how do I invalidate a query":             false,
	} {
		libs := append(slices.Clone(routers), acmeRESTLib, tanstackQueryLib)
		if _, ok := libraryInQuery(libs, q); ok != want {
			t.Errorf("bare query %q named an indexed library = %v, want %v", q, ok, want)
		}
	}
}

// End to end: with only a Vue Router index, a question about Vue (by
// --library or a bare query) is refused as not_indexed, not answered from
// Vue Router's docs.
func TestLocalRefusesComponentWord(t *testing.T) {
	f, store, emb := vueSite(t), newStore(t), &bowEmbedder{id: "test/bow"}
	indexAs(t, f, store, emb, "https://vr.example.test/")
	l := NewLocal(store, func() (embed.Embedder, error) { return emb, nil })
	ctx := context.Background()
	if o := l.GetDocsSelect(ctx, "vue", "two-way binding with v-model on a component", 0); o.Status != Refused || o.Reason != "not_indexed" {
		t.Errorf("--library vue = %s/%s (%d results)", o.Status, o.Reason, len(o.Results))
	}
	if o := l.SearchSelect(ctx, "vue v-model on a custom component", 5); o.Status != Refused || o.Reason != "not_indexed" {
		t.Errorf("bare vue query = %s/%s (%d results)", o.Status, o.Reason, len(o.Results))
	}
	if o := l.GetDocsSelect(ctx, "vue-router", "router.push", 0); o.Status != Answered {
		t.Errorf("--library vue-router = %s/%s", o.Status, o.Reason)
	}
}

// A docs site's own name comes from og:site_name or its root page's title.
func TestSiteName(t *testing.T) {
	for title, want := range map[string]string{
		"Beautiful Soup Documentation — Beautiful Soup 4.14.3 documentation": "Beautiful Soup",
		"HTTPX": "HTTPX",
		"Instructor - Multi-Language Library for Structured LLM Outputs | Python, TypeScript, Go, Ruby - Instructor": "Instructor",
		"Vue Router | The official Router for Vue.js":                                                                "Vue Router",
		"Chart.js | Chart.js":           "Chart.js",
		"Introduction • SvelteKit Docs": "SvelteKit",
		"Home • Angular":                "Angular",
		"Celery - Distributed Task Queue — Celery 5.6.3 documentation": "Celery",
		"psycopg 3.3.8 documentation":                                  "psycopg",
		"A very long page title about something with many words":       "",
	} {
		if got := siteName(title, ""); got != want {
			t.Errorf("siteName(%q) = %q, want %q", title, got, want)
		}
	}
	if got := siteName("Home", "Textual Documentation"); got != "Textual" {
		t.Errorf("og:site_name = %q", got)
	}
}

// An unversioned name answers from the current docs of exactly that
// project: never an older version's index built by the name (angular@18,
// vue@2), never a sibling on the same host (SvelteKit for svelte); a
// library indexed from its docs URL answers to its site's name, its
// packages and its root's distinctive path segment.
func TestUnversionedNamePicksCurrentProject(t *testing.T) {
	lib := func(root, library, site string, pkgs ...string) docsindex.Library {
		return docsindex.Library{Key: root, Root: root, Library: library, SiteName: site, Packages: pkgs, Format: docsindex.FormatVersion}
	}
	ng, ng18 := lib("https://angular.dev/", "https://angular.dev/", ""), lib("https://v18.angular.dev/", "angular@18", "Angular", "github:angular/angular")
	ng18.Version, ng18.Requested = "v18", "18"
	vue, vue2 := lib("https://vuejs.org/", "https://vuejs.org/", ""), lib("https://v2.vuejs.org/", "vue@2", "Vue.js", "npm:vue")
	vue2.Version, vue2.Requested = "v2", "2"
	kit, svelte := lib("https://svelte.dev/docs/kit/", "https://svelte.dev/docs/kit", "SvelteKit"), lib("https://svelte.dev/docs/svelte/", "https://svelte.dev/docs/svelte", "Svelte")
	bs4 := lib("https://www.crummy.com/software/BeautifulSoup/bs4/doc/", "https://www.crummy.com/software/BeautifulSoup/bs4/doc/", "Beautiful Soup")
	httpx := lib("https://www.python-httpx.org/", "https://www.python-httpx.org/", "HTTPX", "pypi:httpx")
	chart := lib("https://www.chartjs.org/docs/latest/", "https://www.chartjs.org/docs/latest/", "Chart.js", "npm:chart.js")
	chart.Version = "latest"
	libs := []docsindex.Library{ng18, ng, vue2, vue, kit, svelte, bs4, httpx, chart}
	for value, want := range map[string]string{
		"angular": ng.Key, "angular@18": ng18.Key, "vue": vue.Key, "vue@2": vue2.Key,
		"svelte": svelte.Key, "sveltekit": kit.Key, "bs4": bs4.Key, "beautiful soup": bs4.Key,
		"httpx": httpx.Key, "pypi:httpx": httpx.Key, "chart.js": chart.Key,
	} {
		got, _, err := pickVersion(libs, value)
		if err != nil || got.Key != want {
			t.Errorf("%q = %s (%v), want %s", value, got.Key, err, want)
		}
	}
	if _, _, err := pickVersion(libs, "chart.js@3"); !errors.Is(err, ErrNotIndexed) || !strings.Contains(err.Error(), "latest") {
		t.Errorf("chart.js@3 = %v", err)
	}
}

// Indexes built from a docs URL before builds recorded the site's name
// carry only their roots: a name in a root's path beats one of its host
// (svelte is svelte.dev/docs/svelte, not svelte.dev/docs/kit), a host
// label without its language word names the library (httpx for
// python-httpx.org, poetry for python-poetry.org), and a package named for
// its major release finds the docs named without it (beautifulsoup4).
func TestNamesFromRootsAlone(t *testing.T) {
	lib := func(root string) docsindex.Library {
		return docsindex.Library{Key: root, Root: root, Library: root, Format: docsindex.FormatVersion}
	}
	kit, svelte := lib("https://svelte.dev/docs/kit/"), lib("https://svelte.dev/docs/svelte/")
	httpx, poetry := lib("https://www.python-httpx.org/"), lib("https://python-poetry.org/docs/")
	bs4 := lib("https://www.crummy.com/software/BeautifulSoup/bs4/doc/")
	libs := []docsindex.Library{kit, svelte, httpx, poetry, bs4}
	for value, want := range map[string]string{
		"svelte": svelte.Key, "kit": kit.Key, "httpx": httpx.Key, "python-httpx": httpx.Key, "poetry": poetry.Key,
		"bs4": bs4.Key, "beautifulsoup4": bs4.Key, "beautifulsoup": bs4.Key,
	} {
		got, _, err := pickVersion(libs, value)
		if err != nil || got.Key != want {
			t.Errorf("%q = %s (%v), want %s", value, got.Key, err, want)
		}
	}
	for _, value := range []string{"python", "web3", "soup4"} {
		if got := MatchIndexed(libs, value); len(got) > 0 {
			t.Errorf("%q named %s", value, got[0].Key)
		}
	}
}
