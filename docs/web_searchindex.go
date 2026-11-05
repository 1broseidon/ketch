package docs

import (
	"bytes"
	"context"
	"encoding/json"
	htmlstd "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// A static docs generator's client-side search ships the whole site's
// text, one entry per section, as a file beside the pages: MkDocs'
// search/search_index.json and mdBook's searchindex.json (or a
// searchindex.js wrapping it). When a site publishes one, the query's
// pages can be found by what their sections say, not only by the titles
// and paths a sitemap or nav gives: the section that answers is often
// one heading of a page whose title names something else (a usage page's
// "Cross-references"). The index chooses which pages to fetch; the
// answers still come from the fetched pages.

var (
	// mkdocsBaseRe finds an MkDocs page's path to the site root: Material's
	// __config "base", the classic themes' base_url.
	mkdocsBaseRe = regexp.MustCompile(`(?:"base"\s*:\s*"|\bbase_url\s*=\s*["'])([^"']*)["']`)
	// searchIndexRefRe finds a search index file a page names: mdBook's
	// searchindex.json, or searchindex-<hash>.js in its inline script.
	searchIndexRefRe = regexp.MustCompile(`["']([^"'\s<>]*searchindex(?:-[0-9a-f]+)?\.js(?:on)?)["']`)
	// mdbookRootRe finds an mdBook page's path to the book's root.
	mdbookRootRe = regexp.MustCompile(`\bpath_to_root\s*=\s*["']([^"']*)["']`)
)

// searchIndexLocs returns where the page at u, whose HTML is body, says its
// site's search index is, best first: the file the page names, or for an
// MkDocs build search/search_index.json at its base, else at root. Nil
// for a page of no generator known to publish one.
func searchIndexLocs(body []byte, generator string, u *url.URL, root webRoot) []string {
	var out []string
	if m := searchIndexRefRe.FindSubmatch(body); m != nil {
		if ref, ok := resolveRef(u, string(m[1])); ok && root.sameHost(ref) {
			out = append(out, ref.String())
		}
	}
	if bytes.Contains(body, []byte("Book generated using mdBook")) {
		// mdBook before hashed asset names loads searchindex.json (else
		// searchindex.js) from path_to_root, the book's root.
		if m := mdbookRootRe.FindSubmatch(body); m != nil {
			for _, name := range []string{"searchindex.json", "searchindex.js"} {
				if loc, ok := resolveRef(u, string(m[1])+name); ok && root.sameHost(loc) {
					out = append(out, loc.String())
				}
			}
		}
	}
	g := strings.ToLower(generator)
	if strings.Contains(g, "mkdocs") || strings.Contains(g, "zensical") {
		if m := mkdocsBaseRe.FindSubmatch(body); m != nil {
			if base, ok := resolveRef(u, strings.TrimSuffix(string(m[1]), "/")+"/search/search_index.json"); ok && root.sameHost(base) {
				out = append(out, base.String())
			}
		}
		out = append(out, root.at("search/search_index.json"))
	}
	return uniqueStrings(out...)
}

// searchIndex reads the first of locs that is a search index, as sections
// of the pages in scope: heading and text from the index, the page's
// title as crumb. Nil when none could be read.
func (w *Web) searchIndex(ctx context.Context, root webRoot, locs []string, tried *[]string) []webSection {
	for _, loc := range locs {
		*tried = append(*tried, loc)
		body, from, ok := w.probe(ctx, root, loc)
		if !ok || len(body) > w.maxFullBytes {
			continue
		}
		if sections := parseSiteIndex(body, from, root); len(sections) > 0 {
			return sections
		}
	}
	return nil
}

// indexEntry is one section of a search index: its address relative to
// the index's base, heading, text, and the page title when the index
// gives it apart from the heading.
type indexEntry struct {
	loc, title, text, page string
}

// parseSiteIndex reads an MkDocs or mdBook search index served from
// from. Entries resolve against the directory holding it (MkDocs keeps
// it in search/, one below the site root).
func parseSiteIndex(body []byte, from *url.URL, root webRoot) []webSection {
	var entries []indexEntry
	base := from
	if js, ok := mdbookIndexJSON(body); ok {
		entries = mdbookEntries(js)
	} else if es, ok := mkdocsEntries(body); ok {
		entries = es
		if u, err := from.Parse("../"); err == nil {
			base = u
		}
	}
	titles := map[string]string{}
	for _, e := range entries {
		page, frag, _ := strings.Cut(e.loc, "#")
		if frag == "" && titles[page] == "" {
			titles[page] = e.title
		}
	}
	var out []webSection
	for _, e := range entries {
		page, frag, _ := strings.Cut(e.loc, "#")
		u, ok := root.resolve(base, page)
		if !ok || !isPageURL(u) || !isResultLink(u) {
			continue
		}
		pageTitle := firstNonEmpty(e.page, titles[page], e.title)
		text := indexText(e.text)
		heading := collapseSpace(indexText(e.title))
		if heading == "" && text == "" {
			continue
		}
		crumbs := []string{pageTitle}
		if frag != "" {
			crumbs = append(crumbs, heading)
		}
		out = append(out, webSection{page: len(out), pageURL: displayURL(u), heading: heading, crumbs: crumbs, anchor: frag, text: text})
	}
	return out
}

// indexText is an index entry's text as plain words: MkDocs Material keeps
// the section's HTML, mdBook plain text.
func indexText(s string) string {
	if strings.ContainsAny(s, "<&") {
		s = htmlstd.UnescapeString(htmlTagRe.ReplaceAllString(s, " "))
	}
	return strings.TrimSpace(s)
}

// mkdocsEntries reads MkDocs' search_index.json: {"docs": [{location,
// title, text}]}.
func mkdocsEntries(body []byte) ([]indexEntry, bool) {
	var idx struct {
		Docs []struct {
			Location string `json:"location"`
			Title    string `json:"title"`
			Text     string `json:"text"`
		} `json:"docs"`
	}
	if json.Unmarshal(body, &idx) != nil || len(idx.Docs) == 0 {
		return nil, false
	}
	out := make([]indexEntry, 0, len(idx.Docs))
	for _, d := range idx.Docs {
		out = append(out, indexEntry{loc: d.Location, title: d.Title, text: d.Text})
	}
	return out, true
}

// mdbookIndex is the part of mdBook's search index read here: each
// section's address, and its title, body and breadcrumbs ("Page » Section")
// by the same position.
type mdbookIndex struct {
	DocURLs []string `json:"doc_urls"`
	Index   struct {
		DocumentStore struct {
			Docs map[string]struct {
				Title       string `json:"title"`
				Body        string `json:"body"`
				Breadcrumbs string `json:"breadcrumbs"`
			} `json:"docs"`
		} `json:"documentStore"`
	} `json:"index"`
}

// mdbookIndexJSON returns mdBook's index as JSON: the file itself, or the
// object a searchindex.js script assigns, written out or as a
// JSON.parse('…') string.
func mdbookIndexJSON(body []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(body)
	if bytes.HasPrefix(trimmed, []byte("{")) {
		return trimmed, bytes.Contains(trimmed[:min(len(trimmed), 64)], []byte(`"doc_urls"`))
	}
	if i := bytes.Index(trimmed, []byte("JSON.parse(")); i >= 0 {
		s := trimmed[i+len("JSON.parse("):]
		if len(s) == 0 || s[0] != '\'' && s[0] != '"' {
			return nil, false
		}
		lit, ok := jsStringLiteral(s)
		return []byte(lit), ok
	}
	if i := bytes.Index(trimmed, []byte(`{"doc_urls"`)); i >= 0 {
		if j := bytes.LastIndexByte(trimmed, '}'); j > i {
			return trimmed[i : j+1], true
		}
	}
	return nil, false
}

// jsStringLiteral decodes the JavaScript string literal s starts with.
func jsStringLiteral(s []byte) (string, bool) {
	quote := s[0]
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c == quote:
			return b.String(), true
		case c != '\\':
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(s) {
			return "", false
		}
		switch e := s[i]; e {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'u':
			if i+4 >= len(s) {
				return "", false
			}
			r, err := strconv.ParseUint(string(s[i+1:i+5]), 16, 32)
			if err != nil {
				return "", false
			}
			b.WriteRune(rune(r))
			i += 4
		default:
			b.WriteByte(e) // \' \" \\ \/ and the rest stand for themselves
		}
	}
	return "", false
}

// mdbookEntries reads mdBook's index into entries, in doc_urls order.
func mdbookEntries(js []byte) []indexEntry {
	var idx mdbookIndex
	if json.Unmarshal(js, &idx) != nil {
		return nil
	}
	out := make([]indexEntry, 0, len(idx.DocURLs))
	for i, loc := range idx.DocURLs {
		d, ok := idx.Index.DocumentStore.Docs[strconv.Itoa(i)]
		if !ok {
			continue
		}
		page, _, _ := strings.Cut(d.Breadcrumbs, " » ")
		out = append(out, indexEntry{loc: loc, title: d.Title, text: d.Body, page: strings.TrimSpace(page)})
	}
	return out
}

// indexPages ranks the index's sections for query (rankSections, the
// ranking results get) and returns the pages of the best, best first, at
// most n, each as a link titled with its page title and described by the
// sections that matched.
func indexPages(sections []webSection, query string, n int) []webLink {
	var out []webLink
	at := map[string]int{}
	for _, s := range rankSections(sections, query) {
		if i, ok := at[s.pageURL]; ok {
			if !strings.Contains(out[i].description, s.heading) {
				out[i].description = strings.TrimSpace(out[i].description + "; " + s.heading)
			}
			continue
		}
		if len(out) == n {
			continue
		}
		u, err := url.Parse(s.pageURL)
		if err != nil {
			continue
		}
		at[s.pageURL] = len(out)
		out = append(out, webLink{url: u, title: s.crumbs[0], description: s.heading})
	}
	return out
}

// interleaveLinks merges two candidate rankings, one from each in turn,
// each page once, at most n: neither the index's ranking by section text
// nor the links' by title and path is trusted alone.
func interleaveLinks(a, b []webLink, n int) []webLink {
	var out []webLink
	seen := map[string]bool{}
	add := func(l webLink) {
		if k := displayKey(l.url); !seen[k] && len(out) < n {
			seen[k] = true
			out = append(out, l)
		}
	}
	for i := 0; i < max(len(a), len(b)); i++ {
		if i < len(a) {
			add(a[i])
		}
		if i < len(b) {
			add(b[i])
		}
	}
	return out
}
