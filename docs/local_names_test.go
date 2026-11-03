package docs

import (
	"context"
	"slices"
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
