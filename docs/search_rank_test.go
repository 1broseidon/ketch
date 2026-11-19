package docs

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/search"
)

// A name@version in a bare question is a guess: unconfirmed, it is read as
// words and the question answered from the search's first docs page, exit
// 0, the search never led by it; --library with the same pin still
// refuses when resolve cannot confirm it.
func TestGuessedPinNeverRefuses(t *testing.T) {
	hits := []search.Result{hit("https://docs.reg.test/policies/unpublish", "Unpublish policy | Reg Docs")}
	f := &fakeFetcher{pages: map[string]fakePage{"https://docs.reg.test/policies/unpublish": {title: "Unpublish policy",
		markdown: "# Unpublish policy\n\n## After 72 hours\n\nA version older than 72 hours can be unpublished only if no package depends on it.\n"}}}
	var queries []string
	s := originSearch(t, f, hits, sfResolved{err: ErrNotFound})
	s.web.sites = siteSearchFunc(func(_ context.Context, q string, _ int) ([]search.Result, error) {
		queries = append(queries, q)
		return hits, nil
	})
	o := s.SearchSelect(context.Background(), "can I unpublish mylib@2.0.0 after 72 hours", 0)
	if o.Err() != nil || !answered(o) || len(o.Read) != 1 {
		t.Fatalf("guessed pin: %s (%v), read %+v; want an answer from the search's page", o.Status, o.Err(), o.Read)
	}
	for _, q := range queries {
		if strings.HasPrefix(q, "mylib 2.0.0") {
			t.Errorf("query %q led by the guessed pin", q)
		}
	}
	if fm := NewFrontmatter("q", SearchBackend, "", o).Render(); strings.Contains(fm, "\nversion:") {
		t.Errorf("an unconfirmed pin printed a version:\n%s", fm)
	}

	o = originSearch(t, f, hits, sfResolved{err: ErrNotFound}).GetDocsSelect(context.Background(), "mylib@2.0.0", "unpublish after 72 hours", 0)
	if o.Status != Refused || !errors.Is(o.Err(), ErrNotFound) || !strings.Contains(o.Err().Error(), "could not confirm docs for mylib@2.0.0") {
		t.Errorf("--library pin: %s (%v), want the refusal", o.Status, o.Err())
	}
}

func TestQuestionRelease(t *testing.T) {
	for q, want := range map[string]sfRelease{
		"Spring Boot 2.7 with spring-boot-starter-data-redis: which property": {"Spring Boot", "2.7"},
		"How do I write a custom v-model in Vue 2":                            {"Vue", "2"},
		"Laravel 9 middleware parameters":                                     {"Laravel", "9"},
		"Tailwind CSS 3 (3.4): dark mode":                                     {"Tailwind CSS", "3"},
		"TanStack Query v5 optimistic updates":                                {"TanStack Query", "5"},
		"what changed for timers in Go 1.23 or later":                         {"Go", "1.23"},
		".NET 9 versus older":                                                 {".NET", "9"},
	} {
		got, ok := questionRelease(q)
		if !ok || got != want {
			t.Errorf("questionRelease(%q) = %+v, %v; want %+v", q, got, ok, want)
		}
	}
	for _, q := range []string{
		"fanning out over a list of 500 URLs",
		"set ServerAliveInterval 15 and ServerAliveCountMax 3",
		"expireAfterSeconds: 3600 on the index",
		"Kafka topic with 64 partitions to 16",
		"retry 3 times on exit code 137",
	} {
		if got, ok := questionRelease(q); ok {
			t.Errorf("questionRelease(%q) = %+v, want none", q, got)
		}
	}
}

// Pages whose address names a release the question names, of the thing it
// names, go first; another project's page of that number does not.
func TestReleaseFirst(t *testing.T) {
	r := sfRelease{name: "Spring Boot", release: "2.7"}
	docs := docsHits(fuseHits([][]search.Result{{
		hit("https://docs.spring.test/spring-boot/reference/data/nosql.html", "NoSQL :: Spring Boot"),
		hit("https://docs.spring.test/spring-data/redis/docs/2.7.0/reference/html/", "Spring Data Redis"),
		hit("https://docs.spring.test/spring-boot/docs/2.7.x/reference/html/data.html", "Data"),
	}}))
	got := releaseFirst(docs, r)
	if got[0].u.Path != "/spring-boot/docs/2.7.x/reference/html/data.html" || got[0].pos != 0 || got[1].pos != 1 {
		t.Errorf("first %s (pos %d), want the Spring Boot 2.7.x page", got[0].u, got[0].pos)
	}
	for label, want := range map[string]bool{"2.7.x": true, "v2": false, "2.70": false, "2.7": true} {
		if releaseLabel(label, "2.7") != want {
			t.Errorf("releaseLabel(%s, 2.7) = %v", label, !want)
		}
	}
	if !urlNamesRelease(mustURL(t, "https://v2.vue.test/guide/x.html"), "2") || !urlNamesRelease(mustURL(t, "https://x.test/en/4x/api.html"), "4") ||
		urlNamesRelease(mustURL(t, "https://x.test/Task/async_stream/3"), "3") || !urlNamesRelease(mustURL(t, "https://docs.sqla.test/en/20/orm/"), "2.0") {
		t.Error("urlNamesRelease")
	}
}

// An answer marks the release a question names: its version when a page
// read is marked with it, else one note line saying none is.
func TestMarkRelease(t *testing.T) {
	r := sfRelease{name: "Laravel", release: "9"}
	o := Outcome{Status: Found, Read: []PageRead{{URL: "https://laravel.test/docs/9.x/middleware", Rank: 1}}, Results: []Result{{URL: "https://laravel.test/docs/9.x/middleware"}}}
	o = markRelease(o, r)
	if fm := NewFrontmatter("q", SearchBackend, "", o).Render(); !strings.Contains(fm, "\nversion: 9\n") || o.Note != "" {
		t.Errorf("marked page:\n%s", fm)
	}
	o = Outcome{Status: Found, Read: []PageRead{{URL: "https://laravel.test/docs/middleware", Rank: 1}}, Results: []Result{{URL: "https://laravel.test/docs/middleware", PageTitle: "Middleware"}}}
	o = markRelease(o, r)
	fm := NewFrontmatter("q", SearchBackend, "", o).Render()
	if strings.Contains(fm, "\nversion:") || !strings.Contains(fm, "\nnote: the question names Laravel 9 and no page read is marked 9: they may document another release; pass that release's docs URL as --library\n") {
		t.Errorf("unmarked pages:\n%s", fm)
	}
	o = Outcome{Status: Found, Read: []PageRead{{URL: "https://x.test/docs/db", Rank: 1}}, Results: []Result{{URL: "https://x.test/docs/db", PageTitle: "Database — Laravel 9.x"}}}
	if o = markRelease(o, r); o.Note != "" || o.Results[0].Version != "9" {
		t.Errorf("a title marked with the release: note %q, version %q", o.Note, o.Results[0].Version)
	}
	// A x label names every release under it: 5.x is marked 5.4.
	r5 := sfRelease{name: "Symfony", release: "5.4"}
	o = Outcome{Status: Found, Read: []PageRead{{URL: "https://symfony.test/doc/5.x/security.html", Rank: 1}}, Results: []Result{{URL: "https://symfony.test/doc/5.x/security.html"}}}
	if o = markRelease(o, r5); o.Note != "" || o.Results[0].Version != "5.4" {
		t.Errorf("a 5.x page for 5.4: note %q, version %q", o.Note, o.Results[0].Version)
	}
}

// Text that names the thing with the release asked speaks of it: no note,
// and no version either (the page itself is marked with none).
func TestReleaseNamedInText(t *testing.T) {
	go123 := sfRelease{name: "Go", release: "1.23"}
	for text, want := range map[string]bool{
		"Before Go 1.23, the garbage collector did not recover timers": true,
		"Since Go 1.23.1 the channel is unbuffered.":                   true,
		"As of Go 1.23. Timers are collected.":                         true,
		"Go 1.230 is no release":                                       false,
		"Go 1.2 changed scheduling":                                    false,
		"go 1.23 is lower case and still names it":                     true,
		"Lego 1.23 bricks":                                             false,
		"set GOTOOLCHAIN=go1.23 to pin the toolchain":                  false,
		"Pydantic V2 changed field defaults":                           false, // another thing
	} {
		if got := textNamesRelease(text, go123); got != want {
			t.Errorf("textNamesRelease(%q, Go 1.23) = %t, want %t", text, got, want)
		}
	}
	if !textNamesRelease("Pydantic V2 changed field defaults", sfRelease{name: "Pydantic", release: "2"}) ||
		!textNamesRelease("New in .NET 9: JsonSerializerOptions.Web", sfRelease{name: ".NET", release: "9"}) ||
		textNamesRelease("ASP.NET 9 middleware", sfRelease{name: ".NET", release: "9"}) ||
		textNamesRelease("Python 3.1 is old", sfRelease{name: "Python", release: "3.10"}) {
		t.Error("textNamesRelease: v, a leading dot or a longer release")
	}
	r := sfRelease{name: "Go", release: "1.23"}
	o := Outcome{Status: Found, Read: []PageRead{{URL: "https://pkg.test/time", Rank: 1}},
		Results: []Result{{URL: "https://pkg.test/time", Snippet: "Before Go 1.23, the garbage collector did not recover timers that had not yet expired."}}}
	if o = markRelease(o, r); o.Note != "" || o.Results[0].Version != "" {
		t.Errorf("text names Go 1.23: note %q, version %q; want neither", o.Note, o.Results[0].Version)
	}
	o = Outcome{Status: Found, Read: []PageRead{{URL: "https://pkg.test/time", Rank: 1}},
		Results: []Result{{URL: "https://pkg.test/time", Snippet: "Go 1.22 added a timer."}}}
	if o = markRelease(o, r); !strings.Contains(o.Note, "no page read is marked 1.23") {
		t.Errorf("text names another release: note %q, want the release note", o.Note)
	}
}

// An answer from pages a search read prints no version from an address's
// labels (/Task/async_stream/3/, /43/): only a pin that matched has one.
func TestNoVersionFromAddress(t *testing.T) {
	f := releaseSiteFixture()
	s, _, _ := searchFirstOver(t, f, releaseHits())
	o := s.SearchSelect(context.Background(), "frobnicate quux_mode", 0)
	if fm := NewFrontmatter("q", SearchBackend, "", o).Render(); o.Err() != nil || strings.Contains(fm, "\nversion:") {
		t.Errorf("pages under /v6/ printed a version (%v):\n%s", o.Err(), fm)
	}
}

func TestResultSnippet(t *testing.T) {
	content := "Published: N/A\n# async_stream(enumerable, fun)\n```elixir\nTask.async_stream(list, &f/1)\n```\n" +
		"Returns a stream where the given function is mapped concurrently on each element in `enumerable`, see [Task](https://x.test/Task.html).\n| a | b |"
	got := resultSnippet("Published: N/A", content)
	if !strings.HasPrefix(got, "Returns a stream where the given function is mapped concurrently on each element in enumerable, see Task.") {
		t.Errorf("snippet %q", got)
	}
	if got := resultSnippet("Published: 2024-01-02", ""); got != "" {
		t.Errorf("metadata only: %q", got)
	}
	for raw, want := range map[string]string{"N/A": "ssh_config", "": "ssh_config", "SSH config": "SSH config"} {
		if got := resultTitle(sfHit{u: mustURL(t, "http://man.bsd.test/ssh_config"), title: raw}); got != want {
			t.Errorf("resultTitle(%q) = %q, want %q", raw, got, want)
		}
	}
}

// Other results: up to five, every one with a title, within sfOtherChars.
func TestOtherResultsBound(t *testing.T) {
	var hits []sfHit
	for i := range 8 {
		u, _ := url.Parse("https://site.test/page-" + strings.Repeat("x", 40) + string(rune('a'+i)))
		hits = append(hits, sfHit{u: u, desc: strings.Repeat("word ", 60), rank: i + 1})
	}
	others := otherResults(hits, nil, nil, nil)
	chars := 0
	for _, r := range others {
		chars += len(otherLine(r))
		if r.Title == "" || r.Title == "N/A" {
			t.Errorf("untitled result printed as %q", r.Title)
		}
	}
	if len(others) == 0 || len(others) > sfOtherResults || chars > sfOtherChars {
		t.Errorf("%d results, %d chars; want at most %d within %d", len(others), chars, sfOtherResults, sfOtherChars)
	}
}

// An address an excerpt cut short (it ends the text) names no page.
func TestCitedURLsSkipCutAddresses(t *testing.T) {
	got := citedURLs("see [docs](https://docs.reg.test/policies/unpublish).", "According to [npm docs](https://docs.reg.test/unpubl")
	if len(got) != 1 || got[0] != "https://docs.reg.test/policies/unpublish" {
		t.Errorf("cited %q", got)
	}
}

// A page that only redirects (rustdoc's stubs) is read where it sends its
// reader.
func TestRedirectStubFollowed(t *testing.T) {
	stub := "https://doc.lang.test/std/sync/poison/mutex/struct.Mutex.html"
	f := &fakeFetcher{
		raw: map[string]fakeRaw{stub: html(`<html><head><meta http-equiv="refresh" content="0;URL=../../../../std/sync/struct.Mutex.html"></head><body><p>Redirecting to <a href="../../../../std/sync/struct.Mutex.html">../../../../std/sync/struct.Mutex.html</a>...</p></body></html>`)},
		pages: map[string]fakePage{
			stub: {title: "Redirection", markdown: "Redirecting to ../../../../std/sync/struct.Mutex.html..."},
			"https://doc.lang.test/std/sync/struct.Mutex.html": {title: "Mutex", markdown: "# Mutex\n\n## Poisoning\n\nThe mutexes in this module implement a strategy called poisoning.\n"},
		},
	}
	w := NewWeb(f)
	p, err := w.fetchIn(context.Background(), onSiteOf(mustURL(t, stub)), webLink{url: mustURL(t, stub)})
	if err != nil || p.URL != "https://doc.lang.test/std/sync/struct.Mutex.html" || !strings.Contains(p.Markdown, "poisoning") {
		t.Errorf("stub: %+v (%v)", p, err)
	}
}
