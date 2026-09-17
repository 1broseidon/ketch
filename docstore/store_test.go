package docstore

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

var samplePages = []Page{
	{URL: "https://tw.test/docs/container-queries", Title: "Container queries", Markdown: "# Container queries\n\nUse the @container variants to style based on the size of a parent.\n\n## Named containers\n\nUse @container/name to target a specific ancestor.\n"},
	{URL: "https://tw.test/docs/colors", Title: "Colors", Markdown: "# Colors\n\nUtilities for controlling text and background color.\n\n## Customizing\n\nExtend the palette in your theme.\n"},
	{URL: "https://tw.test/docs/empty", Title: "Empty", Markdown: "   \n"},
}

func TestReplaceSearchRemove(t *testing.T) {
	s := openTestStore(t)
	lib, err := s.Replace(Library{Name: "tailwind", Version: "4", Seed: "https://tw.test/docs", Source: SourceSitemap}, samplePages)
	if err != nil {
		t.Fatal(err)
	}
	if lib.Pages != 3 || lib.Sections != 4 {
		t.Fatalf("counts = %d pages / %d sections, want 3 / 4", lib.Pages, lib.Sections)
	}
	if lib.AddedAt.IsZero() || lib.UpdatedAt.IsZero() {
		t.Fatal("timestamps not set")
	}

	hits, err := s.Search(context.Background(), Query{Text: "container queries"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	top := hits[0]
	if top.Library != "tailwind" || top.Version != "4" || top.Title != "Container queries" {
		t.Errorf("top hit = %+v", top)
	}
	if top.URL != "https://tw.test/docs/container-queries#container-queries" {
		t.Errorf("url = %q", top.URL)
	}
	if !strings.Contains(top.Breadcrumb, "Container queries") {
		t.Errorf("breadcrumb = %q", top.Breadcrumb)
	}

	// Heading weight: "customizing" appears only as a heading.
	hits, _ = s.Search(context.Background(), Query{Text: "customizing"})
	if len(hits) != 1 || hits[0].Title != "Customizing" || !strings.HasSuffix(hits[0].URL, "#customizing") {
		t.Errorf("heading search = %+v", hits)
	}

	if err := s.Remove("tailwind"); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.Search(context.Background(), Query{Text: "container"}); len(hits) != 0 {
		t.Errorf("hits after remove = %+v", hits)
	}
	if err := s.Remove("tailwind"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second remove = %v, want ErrNotFound", err)
	}
	if _, err := s.Library("tailwind"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Library after remove = %v", err)
	}
}

func TestReplaceIsIdempotentAndKeepsAddedAt(t *testing.T) {
	s := openTestStore(t)
	first, err := s.Replace(Library{Name: "lib", Seed: "https://x.test", Source: SourceCrawl}, samplePages)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Replace(Library{Name: "lib", Version: "2", Seed: "https://x.test", Source: SourceCrawl}, samplePages[:1])
	if err != nil {
		t.Fatal(err)
	}
	if !second.AddedAt.Equal(first.AddedAt) {
		t.Errorf("added_at changed on replace: %v -> %v", first.AddedAt, second.AddedAt)
	}
	if second.Pages != 1 || second.Version != "2" {
		t.Errorf("replace did not overwrite: %+v", second)
	}
	libs, _ := s.Libraries()
	if len(libs) != 1 {
		t.Fatalf("libraries = %+v", libs)
	}
	hits, _ := s.Search(context.Background(), Query{Text: "colors"})
	if len(hits) != 0 {
		t.Errorf("stale sections survived replace: %+v", hits)
	}
}

// A sidebar that readability left on every page must not become a hit on
// every page; text unique to a page is untouched, and a repeat on fewer
// than BoilerplatePages pages is kept (two pages legitimately sharing a
// warning block is not furniture).
func TestReplaceDropsBoilerplateSections(t *testing.T) {
	s := openTestStore(t)
	nav := "## Navigation\n\n- [Colors](/colors)\n- [Flex](/flex)\n- [Grid](/grid)\n"
	twice := "## Note\n\nShared warning text.\n"
	pages := []Page{
		{URL: "https://x.test/a", Title: "A", Markdown: "# A\n\nalpha body\n\n" + nav + twice},
		{URL: "https://x.test/b", Title: "B", Markdown: "# B\n\nbeta body\n\n" + nav + twice},
		{URL: "https://x.test/c", Title: "C", Markdown: "# C\n\ngamma body\n\n" + nav},
	}
	lib, err := s.Replace(Library{Name: "lib", Seed: "https://x.test", Source: SourceCrawl}, pages)
	if err != nil {
		t.Fatal(err)
	}
	// 3 bodies + 2 shared notes; the 3 nav sections are gone.
	if lib.Sections != 5 {
		t.Fatalf("sections = %d, want 5", lib.Sections)
	}
	if hits, _ := s.Search(context.Background(), Query{Text: "colors flex grid"}); len(hits) != 0 {
		t.Errorf("boilerplate nav still indexed: %+v", hits)
	}
	if hits, _ := s.Search(context.Background(), Query{Text: "shared warning"}); len(hits) != 2 {
		t.Errorf("two-page repeat should be kept: %+v", hits)
	}
}

func TestReplaceEmptyIsError(t *testing.T) {
	s := openTestStore(t)
	_, err := s.Replace(Library{Name: "lib", Seed: "https://x.test", Source: SourceCrawl}, []Page{{URL: "u", Markdown: "\n"}})
	if !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v, want ErrEmpty", err)
	}
	if libs, _ := s.Libraries(); len(libs) != 0 {
		t.Errorf("empty replace wrote a library: %+v", libs)
	}
}

func TestSearchScopesToLibraries(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Replace(Library{Name: "a", Seed: "https://a.test", Source: SourceCrawl}, samplePages[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Replace(Library{Name: "b", Seed: "https://b.test", Source: SourceCrawl}, []Page{{URL: "https://b.test/x", Title: "X", Markdown: "# X\n\ncontainer sizes in b\n"}}); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Search(context.Background(), Query{Text: "container"})
	if len(all) != 3 {
		t.Fatalf("unscoped hits = %d, want 3", len(all))
	}
	onlyB, _ := s.Search(context.Background(), Query{Text: "container", Libraries: []string{"b"}})
	if len(onlyB) != 1 || onlyB[0].Library != "b" {
		t.Errorf("scoped hits = %+v", onlyB)
	}
	none, _ := s.Search(context.Background(), Query{Text: "container", Libraries: []string{"missing"}})
	if len(none) != 0 {
		t.Errorf("unknown library matched: %+v", none)
	}
}

// Question-shaped queries rarely contain every word of a section; the OR
// fallback still finds the closest one instead of returning nothing.
func TestSearchFallsBackToAnyTerm(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Replace(Library{Name: "a", Seed: "https://a.test", Source: SourceCrawl}, samplePages); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Search(context.Background(), Query{Text: "how do I customize the palette zebra"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "Customizing" {
		t.Errorf("fallback hits = %+v", hits)
	}
}

func TestSearchNeverLeaksFTSSyntax(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Replace(Library{Name: "a", Seed: "https://a.test", Source: SourceCrawl}, samplePages); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`"unbalanced`, `NEAR(a b)`, `col:umn`, `a AND`, `-`, `*`, `(`, `"" OR ""`, `http.NewRequestWithContext`} {
		if _, err := s.Search(context.Background(), Query{Text: q}); err != nil {
			t.Errorf("query %q errored: %v", q, err)
		}
	}
	if hits, _ := s.Search(context.Background(), Query{Text: "!!! ???"}); hits != nil {
		t.Errorf("punctuation-only query returned %+v", hits)
	}
}

func TestSearchLimit(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Replace(Library{Name: "a", Seed: "https://a.test", Source: SourceCrawl}, samplePages); err != nil {
		t.Fatal(err)
	}
	hits, _ := s.Search(context.Background(), Query{Text: "container OR color", Limit: 1})
	if len(hits) != 1 {
		t.Errorf("limit ignored: %d hits", len(hits))
	}
}

func TestTerms(t *testing.T) {
	cases := map[string][]string{
		"Container queries":               {"container", "queries"},
		"http.NewRequestWithContext(ctx)": {"http", "newrequestwithcontext", "ctx"},
		"snake_case and snake_case again": {"snake", "case", "and", "again"},
		`"quoted" NEAR(term) col:umn`:     {"quoted", "near", "term", "col", "umn"},
		"  ":                              nil,
		"!!! ???":                         nil,
		"Ünïcödé héading v4.0":            {"ünïcödé", "héading", "v4", "0"},
		"@container / @media (min-width)": {"container", "media", "min", "width"},
		"A a A":                           {"a"},
		"tailwind-css tailwind css":       {"tailwind", "css"},
		"C++ templates":                   {"c", "templates"},
		"日本語 テキスト":                        {"日本語", "テキスト"},
		"path/to/file.md":                 {"path", "to", "file", "md"},
		"tabs\tand\nnewlines":             {"tabs", "and", "newlines"},
	}
	for in, want := range cases {
		if got := Terms(in); !reflect.DeepEqual(got, want) {
			t.Errorf("Terms(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"tailwind", "tailwind-4", "go1.25", "a", "x_y", strings.Repeat("a", 64)} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Tailwind", "-lead", "has space", "slash/name", "a\"b", strings.Repeat("a", 65), "../etc"} {
		if err := ValidateName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	if Exists(dir) {
		t.Fatal("Exists true before Open")
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if !Exists(dir) {
		t.Fatal("Exists false after Open")
	}
}
