package cache

import (
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/ketch/scrape"
)

func page(url, title, markdown string) *scrape.Page {
	return &scrape.Page{URL: url, Title: title, Markdown: markdown}
}

// put stores a page and tags it, the way a --tag fetch does.
func put(t *testing.T, c *Cache, tag, url, title, markdown string) {
	t.Helper()
	p := page(url, title, markdown)
	c.Put(url, p, scrape.SourceHTTP)
	if err := c.TagPage(tag, url, url, p); err != nil {
		t.Fatalf("TagPage(%s, %s): %v", tag, url, err)
	}
}

// TestTagIndexSurvivesClear is the decision in ADR-0004 expressed as a test:
// the index outlives the bodies it points at. Clearing every page body must
// leave the tag listing everything it listed before, marked uncached — not
// empty.
func TestTagIndexSurvivesClear(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	put(t, c, "guacamole", "https://example.com/ldap", "LDAP auth",
		"Guacamole authenticates against an LDAP directory, mapping entries to users without duplicating credentials.")
	put(t, c, "guacamole", "https://example.com/nginx", "Behind nginx",
		"Running Guacamole behind nginx requires forwarding the WebSocket upgrade headers for the tunnel.")

	before, err := c.Tagged("guacamole")
	if err != nil {
		t.Fatalf("Tagged: %v", err)
	}
	if len(before) != 2 {
		t.Fatalf("before clear: %d entries, want 2", len(before))
	}
	for _, p := range before {
		if !p.Cached {
			t.Errorf("before clear: %s should be cached", p.URL)
		}
	}

	if err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	after, err := c.Tagged("guacamole")
	if err != nil {
		t.Fatalf("Tagged after clear: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("after clear: %d entries, want 2 — the index must outlive the bodies", len(after))
	}
	for _, p := range after {
		if p.Cached {
			t.Errorf("after clear: %s reports cached, but every body was dropped", p.URL)
		}
		if p.Title == "" || p.Description == "" {
			t.Errorf("after clear: %s lost its metadata; the entry must be self-sufficient", p.URL)
		}
	}
}

// A cold entry going warm again is what makes the index useful after expiry:
// re-fetching the page restores it without re-tagging.
func TestTagColdEntryWarmsOnRefetch(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	url := "https://example.com/ldap"
	put(t, c, "guacamole", url, "LDAP auth", "Guacamole authenticates against an LDAP directory for single sign-on.")
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	pages, _ := c.Tagged("guacamole")
	if len(pages) != 1 || pages[0].Cached {
		t.Fatalf("expected one cold entry, got %+v", pages)
	}

	// The re-fetch: the same page lands under the same key.
	c.Put(url, page(url, "LDAP auth", "Guacamole authenticates against an LDAP directory for single sign-on."), scrape.SourceHTTP)

	pages, _ = c.Tagged("guacamole")
	if len(pages) != 1 {
		t.Fatalf("after refetch: %d entries, want 1", len(pages))
	}
	if !pages[0].Cached {
		t.Error("after refetch the entry should report cached again")
	}
}

// Expiry by TTL, not just an explicit Clear, must also leave the entry in
// place. This is the case the original cache-scoped design got wrong.
func TestTagIndexSurvivesTTLExpiry(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Nanosecond)
	url := "https://example.com/ldap"
	put(t, c, "guacamole", url, "LDAP auth", "Guacamole authenticates against an LDAP directory for single sign-on.")
	time.Sleep(2 * time.Millisecond)

	pages, err := c.Tagged("guacamole")
	if err != nil {
		t.Fatalf("Tagged: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("%d entries after TTL expiry, want 1", len(pages))
	}
	if pages[0].Cached {
		t.Error("an expired body must report cached=false")
	}
	if pages[0].URL != url {
		t.Errorf("URL = %q, want %q", pages[0].URL, url)
	}
}

// Naming a URL is a deliberate act, so it is indexed whether or not a body
// happens to be cached — organising URLs must not depend on when they were
// last fetched.
func TestTagURLWithoutACachedBody(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	url := "https://example.com/never-fetched"

	cached, err := c.TagURL("guacamole", url, url)
	if err != nil {
		t.Fatalf("TagURL: %v", err)
	}
	if cached {
		t.Error("TagURL reported a cached body for a page never fetched")
	}

	pages, _ := c.Tagged("guacamole")
	if len(pages) != 1 {
		t.Fatalf("%d entries, want the URL to be indexed anyway", len(pages))
	}
	if pages[0].URL != url {
		t.Errorf("URL = %q, want %q", pages[0].URL, url)
	}
	if pages[0].Cached {
		t.Error("entry should list as uncached")
	}
	if pages[0].Title != "" || pages[0].Description != "" {
		t.Errorf("entry should have no metadata yet, got %+v", pages[0])
	}
}

// ...and it becomes a proper index entry the first time the page is seen,
// without anyone re-tagging it.
func TestTagURLBackfillsMetadataOnceFetched(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	url := "https://example.com/ldap"
	if _, err := c.TagURL("guacamole", url, url); err != nil {
		t.Fatalf("TagURL: %v", err)
	}

	// The page arrives later, by any route — a plain scrape, no --tag.
	c.Put(url, page(url, "LDAP auth", "Guacamole authenticates against an LDAP directory for single sign-on."), scrape.SourceHTTP)

	pages, _ := c.Tagged("guacamole")
	if len(pages) != 1 {
		t.Fatalf("%d entries, want 1", len(pages))
	}
	if pages[0].Title != "LDAP auth" || pages[0].Description == "" {
		t.Errorf("metadata not backfilled: %+v", pages[0])
	}
	if !pages[0].Cached {
		t.Error("entry should now list as cached")
	}

	// Backfill is persisted, so it survives the body expiring again.
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	pages, _ = c.Tagged("guacamole")
	if len(pages) != 1 || pages[0].Title != "LDAP auth" {
		t.Errorf("backfilled metadata was not persisted: %+v", pages)
	}
}

func TestTagMultipleTagsShareOnePage(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	url := "https://example.com/ldap"
	p := page(url, "LDAP auth", "Guacamole authenticates against an LDAP directory for single sign-on.")
	c.Put(url, p, scrape.SourceHTTP)
	for _, tag := range []string{"guacamole", "auth"} {
		if err := c.TagPage(tag, url, url, p); err != nil {
			t.Fatalf("TagPage(%s): %v", tag, err)
		}
	}

	for _, tag := range []string{"guacamole", "auth"} {
		pages, _ := c.Tagged(tag)
		if len(pages) != 1 || pages[0].URL != url {
			t.Errorf("tag %s = %+v, want the one page", tag, pages)
		}
	}
	// One cached body, listed under both tags.
	if entries, _ := c.Stats(); entries != 1 {
		t.Errorf("cache holds %d page bodies, want 1", entries)
	}
}

// A tag whose name prefixes another's must not absorb its entries: the index
// packs tag and page key into one storage key, and the read is a prefix scan.
func TestTagNamePrefixesDoNotCollide(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	put(t, c, "doc", "https://example.com/a", "A", "The first page about a subject worth describing here.")
	put(t, c, "docs", "https://example.com/b", "B", "The second page about a subject worth describing here.")
	put(t, c, "docs", "https://example.com/c", "C", "The third page about a subject worth describing here.")

	if pages, _ := c.Tagged("doc"); len(pages) != 1 {
		t.Errorf(`tag "doc" = %d entries, want 1`, len(pages))
	}
	if pages, _ := c.Tagged("docs"); len(pages) != 2 {
		t.Errorf(`tag "docs" = %d entries, want 2`, len(pages))
	}
}

func TestTagList(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	put(t, c, "guacamole", "https://example.com/a", "A", "A page about remote desktop gateways and their configuration.")
	put(t, c, "guacamole", "https://example.com/b", "B", "Another page about remote desktop gateways and their configuration.")
	put(t, c, "postgres", "https://example.com/c", "C", "A page about relational databases and their configuration.")

	tags, err := c.TagList()
	if err != nil {
		t.Fatalf("TagList: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("TagList = %+v, want 2 tags", tags)
	}
	byName := map[string]TagSummary{}
	for _, s := range tags {
		byName[s.Name] = s
	}
	if byName["guacamole"].Entries != 2 || byName["guacamole"].Cached != 2 {
		t.Errorf("guacamole = %+v, want 2 entries / 2 cached", byName["guacamole"])
	}
	if byName["postgres"].Entries != 1 {
		t.Errorf("postgres = %+v, want 1 entry", byName["postgres"])
	}
}

func TestTagRemoveEntries(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	put(t, c, "guacamole", "https://example.com/a", "A", "A page about remote desktop gateways and their configuration.")
	put(t, c, "guacamole", "https://example.com/b", "B", "Another page about remote desktop gateways and their configuration.")

	removed, missing, err := c.RemoveTagged("guacamole", []string{"https://example.com/a"})
	if err != nil || removed != 1 || len(missing) != 0 {
		t.Fatalf("RemoveTagged = (%d, %v, %v), want (1, [], nil)", removed, missing, err)
	}
	if pages, _ := c.Tagged("guacamole"); len(pages) != 1 {
		t.Errorf("after removing one: %d entries, want 1", len(pages))
	}

	// A page that was never under the tag is reported by URL, not an error.
	removed, missing, err = c.RemoveTagged("guacamole", []string{"https://example.com/zzz"})
	if err != nil || removed != 0 || len(missing) != 1 || missing[0] != "https://example.com/zzz" {
		t.Fatalf("RemoveTagged(absent) = (%d, %v, %v), want (0, [https://example.com/zzz], nil)", removed, missing, err)
	}

	// A mixed batch reports removed and missing independently.
	put(t, c, "guacamole", "https://example.com/c", "C", "A third page about remote desktop gateways worth keeping around.")
	removed, missing, err = c.RemoveTagged("guacamole", []string{"https://example.com/b", "https://example.com/nope"})
	if err != nil || removed != 1 || len(missing) != 1 || missing[0] != "https://example.com/nope" {
		t.Fatalf("RemoveTagged(mixed) = (%d, %v, %v), want (1, [https://example.com/nope], nil)", removed, missing, err)
	}
}

func TestTagRemoveWholeTag(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	put(t, c, "guacamole", "https://example.com/a", "A", "A page about remote desktop gateways and their configuration.")
	put(t, c, "postgres", "https://example.com/c", "C", "A page about relational databases and their configuration.")

	n, err := c.RemoveTag("guacamole")
	if err != nil || n != 1 {
		t.Fatalf("RemoveTag = (%d, %v), want (1, nil)", n, err)
	}
	if pages, _ := c.Tagged("guacamole"); len(pages) != 0 {
		t.Errorf("removed tag still lists %d entries", len(pages))
	}
	if pages, _ := c.Tagged("postgres"); len(pages) != 1 {
		t.Errorf("unrelated tag lost entries: %+v", pages)
	}
	// Removing a tag is not removing pages: the index is what it acts on.
	if entries, _ := c.Stats(); entries != 2 {
		t.Errorf("page bodies = %d, want 2 — removing a tag must not touch them", entries)
	}
}

// Re-tagging refreshes an entry rather than duplicating it.
func TestTagReTagReplacesEntry(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	url := "https://example.com/a"
	put(t, c, "guacamole", url, "Old title", "The original description of this page, long enough to be kept.")
	put(t, c, "guacamole", url, "New title", "A revised description of this page, long enough to be kept.")

	pages, _ := c.Tagged("guacamole")
	if len(pages) != 1 {
		t.Fatalf("%d entries after re-tagging, want 1", len(pages))
	}
	if pages[0].Title != "New title" {
		t.Errorf("title = %q, want the refreshed one", pages[0].Title)
	}
}

func TestTaggedUnknownTagIsEmptyNotAnError(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	pages, err := c.Tagged("never-used")
	if err != nil {
		t.Fatalf("Tagged(unknown) error = %v, want nil", err)
	}
	if len(pages) != 0 {
		t.Errorf("Tagged(unknown) = %+v, want none", pages)
	}
}

func TestDescribe(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		markdown string
		want     string
	}{
		{
			"skips the heading, takes the first paragraph",
			"# LDAP authentication\n\nGuacamole supports authenticating against an LDAP directory such as OpenLDAP.",
			"Guacamole supports authenticating against an LDAP directory such as OpenLDAP.",
		},
		{
			"strips inline markdown",
			"Configure **ldap-hostname** and [the base DN](https://example.com) in your properties file today.",
			"Configure ldap-hostname and the base DN in your properties file today.",
		},
		{
			"skips short nav fragments",
			"Home\n\nDocs\n\nThe real prose about this page begins only after the breadcrumb trail.",
			"The real prose about this page begins only after the breadcrumb trail.",
		},
		{"empty markdown yields nothing", "", ""},
		{"no prose yields nothing", "# Title\n\n- a\n- b", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Describe(tc.markdown); got != tc.want {
				t.Errorf("Describe() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDescribeTruncatesOnAWordBoundary(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("configuration ", 40)
	got := Describe(long)
	if len([]rune(got)) > descriptionMax+1 { // +1 for the ellipsis
		t.Errorf("description is %d runes, want <= %d", len([]rune(got)), descriptionMax+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated description should end in an ellipsis: %q", got)
	}
	if strings.Contains(got, "configur…") {
		t.Errorf("truncation cut mid-word: %q", got)
	}
}

// TestTagResultKeepsTheSnippet covers the surfaces that return snippets
// rather than fetched pages — code hits, docs chunks, unscraped search
// results. There is no body to describe, so the snippet the caller already
// saw *is* the entry's description.
func TestTagResultKeepsTheSnippet(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	const url = "https://github.com/apache/guacamole-client/blob/main/ldap.go"
	snippet := "func authenticate(ctx context.Context, dn string) error {\n\t\treturn dir.Bind(ctx, dn)\n}"
	if err := c.TagResult("remote-access", url, url, "guacamole-client ldap.go:42", snippet); err != nil {
		t.Fatalf("TagResult: %v", err)
	}

	pages, err := c.Tagged("remote-access")
	if err != nil {
		t.Fatalf("Tagged: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("got %d entries, want 1", len(pages))
	}
	got := pages[0]
	if got.Cached {
		t.Error("a snippet result was never fetched, so its entry must report cached=false")
	}
	if got.Title != "guacamole-client ldap.go:42" {
		t.Errorf("title = %q, want the result's location", got.Title)
	}
	if strings.ContainsAny(got.Description, "\n\t") {
		t.Errorf("description = %q, want the snippet collapsed onto one line", got.Description)
	}
	if !strings.Contains(got.Description, "func authenticate(ctx context.Context, dn string) error {") {
		t.Errorf("description = %q, want it to keep the snippet", got.Description)
	}
}

// A snippet entry is not a lesser entry: scraping the URL later warms it in
// place, without losing the description the snippet gave it.
func TestTagResultWarmsWhenTheURLIsLaterScraped(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	const url = "https://example.com/ldap"
	if err := c.TagResult("remote-access", url, url, "LDAP auth", "a one-line summary from the search engine"); err != nil {
		t.Fatalf("TagResult: %v", err)
	}
	c.Put(url, page(url, "LDAP auth", "Guacamole authenticates against an LDAP directory."), scrape.SourceHTTP)

	pages, err := c.Tagged("remote-access")
	if err != nil {
		t.Fatalf("Tagged: %v", err)
	}
	if len(pages) != 1 || !pages[0].Cached {
		t.Fatalf("got %+v, want one entry reporting cached", pages)
	}
	if pages[0].Description != "a one-line summary from the search engine" {
		t.Errorf("description = %q, want the snippet kept", pages[0].Description)
	}
}

func TestOneLine(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, in, want string }{
		{"already one line", "plain text", "plain text"},
		{"indented snippet", "if err != nil {\n\t\treturn err\n}", "if err != nil { return err }"},
		{"collapses runs of space", "a   b\n\n  c", "a b c"},
		{"empty", "   \n\t", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := oneLine(tc.in, descriptionMax); got != tc.want {
				t.Errorf("oneLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	long := strings.Repeat("word ", 200)
	if got := oneLine(long, descriptionMax); len([]rune(got)) > descriptionMax {
		t.Errorf("oneLine kept %d runes, want at most %d", len([]rune(got)), descriptionMax)
	}
}

// `search --scrape --tag` records every hit from the result list first, then
// lets each successful fetch overwrite its entry. The second write has to win:
// a fetched page's own title and description are better than the engine's.
func TestTagPageOverwritesASnippetEntry(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, time.Hour)
	const url = "https://example.com/ldap"
	if err := c.TagResult("remote-access", url, url, "engine title", "engine blurb"); err != nil {
		t.Fatalf("TagResult: %v", err)
	}
	p := page(url, "LDAP authentication", "Guacamole authenticates against an LDAP directory, mapping entries to users without duplicating credentials.")
	c.Put(url, p, scrape.SourceHTTP)
	if err := c.TagPage("remote-access", url, url, p); err != nil {
		t.Fatalf("TagPage: %v", err)
	}

	pages, err := c.Tagged("remote-access")
	if err != nil {
		t.Fatalf("Tagged: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("got %d entries, want 1 — the fetch must replace the entry, not add one", len(pages))
	}
	if pages[0].Title != "LDAP authentication" {
		t.Errorf("title = %q, want the fetched page's", pages[0].Title)
	}
	if pages[0].Description == "engine blurb" {
		t.Error("description is still the engine's; the fetched page should have replaced it")
	}
}
