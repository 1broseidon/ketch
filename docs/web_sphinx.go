// Owner: chunk C (versions and adapters).

package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// sphinxSite is a detected Sphinx build.
type sphinxSite struct {
	// root is the build's root directory: where searchindex.js, objects.inv
	// and _static/ live.
	root *url.URL
	// options is the URL of _static/documentation_options.js; "" when the
	// page did not load it.
	options string
}

// sphinxIndex is the part of a searchindex.js the adapter reads.
type sphinxIndex struct {
	docnames []string
	titles   []string         // page titles, by docnames index
	sections map[int][]string // section titles per page, from alltitles
	objects  map[int][]string // object names per page
}

// sphinxOptions is what documentation_options.js says about the build.
type sphinxOptions struct {
	builder    string // "html", "dirhtml"; "" when not stated
	version    string // VERSION
	linkSuffix string // LINK_SUFFIX, ".html" for the html builder
}

// Caps on what one page's discovery link carries from the search index:
// enough words for linkScore, not the whole API reference.
const (
	sphinxMaxSections = 12
	sphinxMaxObjects  = 8
)

// errNotSphinxIndex is a searchindex.js that is not a Sphinx index.
var errNotSphinxIndex = errors.New("not a Sphinx search index")

// detectSphinx reports a Sphinx build from a page's generator meta and
// script URLs, page being where the page was served from.
//
// The build root is the directory holding the _static/ that
// documentation_options.js loads from (a page below the root loads
// ../_static/), on the page's host. Without that script, a generator
// naming Sphinx makes the page's own directory the root.
func detectSphinx(page *url.URL, generator string, scripts []string) (sphinxSite, bool) {
	for _, src := range scripts {
		u, err := url.Parse(strings.TrimSpace(src))
		if err != nil || !isWebScheme(u.Scheme) || page != nil && scopeHost(u) != scopeHost(page) {
			continue
		}
		dir, file := path.Split(u.Path)
		if file != "documentation_options.js" || path.Base(dir) != "_static" {
			continue
		}
		root := &url.URL{Scheme: u.Scheme, Host: u.Host, Path: strings.TrimSuffix(dir, "_static/")}
		opts := &url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
		return sphinxSite{root: root, options: opts.String()}, true
	}
	if page == nil || !strings.Contains(strings.ToLower(generator), "sphinx") {
		return sphinxSite{}, false
	}
	dir := page.Path
	if !strings.HasSuffix(dir, "/") {
		dir = path.Dir(dir) + "/"
	}
	return sphinxSite{root: &url.URL{Scheme: page.Scheme, Host: page.Host, Path: strings.ReplaceAll(dir, "//", "/")}}, true
}

// sphinxLinks builds discovery links for every page of the build from its
// searchindex.js and documentation_options.js (plan §7): html builder
// docname+".html", dirhtml docname+"/" (index is the root); the page title
// as title, section titles and some object names as description. Only
// in-scope links are returned.
//
// Read the Docs sitemaps list version roots only and many Python projects
// publish none, so this is how discovery reaches a Sphinx site's pages.
// Without documentation_options.js the html builder is assumed: it is
// Sphinx's default.
func (w *Web) sphinxLinks(ctx context.Context, root webRoot, s sphinxSite) ([]webLink, error) {
	if s.root == nil {
		return nil, errNotSphinxIndex
	}
	loc := s.root.String() + "searchindex.js"
	body, _, ok := w.probe(ctx, root, loc)
	if !ok {
		return nil, fmt.Errorf("web: no Sphinx search index at %s: %w", loc, ErrNotFound)
	}
	idx, err := parseSearchIndex(body)
	if err != nil {
		return nil, fmt.Errorf("web: %s: %w", loc, err)
	}
	opts := w.sphinxOptions(ctx, root, s)
	var links []webLink
	seen := map[string]bool{}
	for i, doc := range idx.docnames {
		u := sphinxPageURL(s.root, doc, opts)
		if u == nil || !root.contains(u) || seen[linkKey(u)] {
			continue
		}
		seen[linkKey(u)] = true
		links = append(links, webLink{url: u, title: idx.title(i), description: idx.description(i)})
		if len(links) >= webMaxURLs {
			break
		}
	}
	return links, nil
}

// sphinxOptions reads the build's documentation_options.js, at s.options
// or where Sphinx puts it; a build that does not answer gets the html
// builder's defaults.
func (w *Web) sphinxOptions(ctx context.Context, root webRoot, s sphinxSite) sphinxOptions {
	loc := s.options
	if loc == "" && s.root != nil {
		loc = s.root.String() + "_static/documentation_options.js"
	}
	if body, _, ok := w.probe(ctx, root, loc); ok {
		return parseDocOptions(body)
	}
	return sphinxOptions{linkSuffix: ".html"}
}

// sphinxPageURL returns the page a docname is served at: the build root
// for index, docname/ (foo/index as foo/) for dirhtml, else docname plus
// the link suffix. Nil for a docname that cannot be a page there.
func sphinxPageURL(base *url.URL, doc string, opts sphinxOptions) *url.URL {
	doc = strings.Trim(doc, "/")
	var rel string
	switch {
	case doc == "":
		return nil
	case doc == "index":
	case opts.builder == "dirhtml" && path.Base(doc) == "index":
		rel = path.Dir(doc) + "/"
	case opts.builder == "dirhtml":
		rel = doc + "/"
	default:
		rel = doc + opts.linkSuffix
	}
	u := base.ResolveReference(&url.URL{Path: rel})
	u.RawQuery, u.Fragment, u.RawFragment = "", "", ""
	if !cleanDots(u) {
		return nil
	}
	return u
}

// title returns page i's title, space-collapsed. Sphinx indexes titles
// as plain text.
func (idx sphinxIndex) title(i int) string {
	if i < len(idx.titles) {
		return collapseSpace(idx.titles[i])
	}
	return ""
}

// description joins page i's section titles and object names.
func (idx sphinxIndex) description(i int) string {
	parts := append(slices.Clone(idx.sections[i]), idx.objects[i]...)
	return strings.Join(parts, ", ")
}

// rawSearchIndex is the part of searchindex.js's JSON decoded here; terms
// and titleterms, most of the file, are skipped.
type rawSearchIndex struct {
	Docnames  []string                   `json:"docnames"`
	Titles    []string                   `json:"titles"`
	Alltitles map[string][][]any         `json:"alltitles"`
	Objects   map[string]json.RawMessage `json:"objects"`
}

// parseSearchIndex parses searchindex.js: Search.setIndex(<JSON>).
//
// Sphinx before 6 wrote the object with bare keys (docnames:[…]), which
// are quoted before decoding. objects comes in both of its layouts: a list
// of [doc, type, priority, anchor, name] per prefix (Sphinx 7.3 on), or a
// map from name to [doc, type, priority, anchor].
func parseSearchIndex(body []byte) (sphinxIndex, error) {
	js, err := searchIndexJSON(body)
	if err != nil {
		return sphinxIndex{}, err
	}
	var raw rawSearchIndex
	if err := json.Unmarshal(js, &raw); err != nil {
		if json.Unmarshal(quoteBareKeys(js), &raw) != nil {
			return sphinxIndex{}, fmt.Errorf("%w: %w", errNotSphinxIndex, err)
		}
	}
	if len(raw.Docnames) == 0 {
		return sphinxIndex{}, fmt.Errorf("%w: it lists no pages", errNotSphinxIndex)
	}
	idx := sphinxIndex{docnames: raw.Docnames, titles: raw.Titles, sections: map[int][]string{}, objects: map[int][]string{}}
	idx.addSections(raw.Alltitles)
	idx.addObjects(raw.Objects)
	return idx, nil
}

// searchIndexJSON returns the argument of searchindex.js's
// Search.setIndex(…) call.
func searchIndexJSON(body []byte) ([]byte, error) {
	const call = "setIndex("
	i := bytes.Index(body, []byte(call))
	if i < 0 {
		return nil, errNotSphinxIndex
	}
	js := bytes.TrimRight(bytes.TrimSpace(body[i+len(call):]), "; \t\r\n")
	js, ok := bytes.CutSuffix(js, []byte(")"))
	if !ok {
		return nil, errNotSphinxIndex
	}
	return js, nil
}

// addSections records each page's section titles from alltitles (title to
// [[doc, anchor]]; a null anchor is the page title itself), in title order.
func (idx *sphinxIndex) addSections(all map[string][][]any) {
	titles := make([]string, 0, len(all))
	for t := range all {
		titles = append(titles, t)
	}
	sort.Strings(titles)
	for _, t := range titles {
		for _, ref := range all[t] {
			doc, ok := jsonIndex(ref, 0)
			anchor, _ := jsonAt(ref, 1).(string)
			if !ok || anchor == "" || len(idx.sections[doc]) >= sphinxMaxSections || collapseSpace(t) == idx.title(doc) {
				continue
			}
			if s := collapseSpace(t); !slices.Contains(idx.sections[doc], s) {
				idx.sections[doc] = append(idx.sections[doc], s)
			}
		}
	}
}

// addObjects records up to sphinxMaxObjects object names per page, in
// name order, skipping objects Sphinx ranks unimportant or hides.
func (idx *sphinxIndex) addObjects(objects map[string]json.RawMessage) {
	prefixes := make([]string, 0, len(objects))
	for p := range objects {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	for _, prefix := range prefixes {
		var list [][]any
		if json.Unmarshal(objects[prefix], &list) == nil {
			for _, e := range list {
				name, _ := jsonAt(e, 4).(string)
				idx.addObject(prefix, name, e)
			}
			continue
		}
		var named map[string][]any
		if json.Unmarshal(objects[prefix], &named) != nil {
			continue
		}
		names := make([]string, 0, len(named))
		for n := range named {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			idx.addObject(prefix, n, named[n])
		}
	}
}

// addObject records one object, entry being its [doc, type, priority, …].
func (idx *sphinxIndex) addObject(prefix, name string, entry []any) {
	doc, okDoc := jsonIndex(entry, 0)
	prio, okPrio := jsonAt(entry, 2).(float64)
	if !okDoc || name == "" || !okPrio || prio < 0 || prio > 1 || len(idx.objects[doc]) >= sphinxMaxObjects {
		return
	}
	if prefix != "" {
		name = prefix + "." + name
	}
	idx.objects[doc] = append(idx.objects[doc], name)
}

// jsonAt returns element i of a decoded JSON array, or nil.
func jsonAt(a []any, i int) any {
	if i < len(a) {
		return a[i]
	}
	return nil
}

// jsonIndex returns element i of a decoded JSON array as a non-negative
// integer.
func jsonIndex(a []any, i int) (int, bool) {
	f, ok := jsonAt(a, i).(float64)
	if !ok || f < 0 || f != float64(int(f)) {
		return 0, false
	}
	return int(f), true
}

// quoteBareKeys quotes the bare object keys of a JavaScript object literal
// (old Sphinx's {docnames:[…],objects:{"":{click:[…]}}}), leaving strings
// and everything else as they are.
func quoteBareKeys(js []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(js) + len(js)/16)
	inStr, esc := false, false
	prev := byte(0) // the last byte outside a string that is not a space
	for i := 0; i < len(js); i++ {
		c := js[i]
		if inStr {
			out.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr, prev = false, c
			}
			continue
		}
		if prev == '{' || prev == ',' {
			if end, ok := bareKeyAt(js, i); ok {
				out.WriteByte('"')
				out.Write(js[i:end])
				out.WriteByte('"')
				i, prev = end-1, '"'
				continue
			}
		}
		out.WriteByte(c)
		inStr = c == '"'
		if !isJSONSpace(c) {
			prev = c
		}
	}
	return out.Bytes()
}

// bareKeyAt reports a bare key starting at js[i]: a run of identifier
// bytes followed, spaces aside, by a colon. end is where the run stops.
func bareKeyAt(js []byte, i int) (end int, ok bool) {
	end = i
	for end < len(js) && isKeyByte(js[end]) {
		end++
	}
	if end == i {
		return 0, false
	}
	j := end
	for j < len(js) && isJSONSpace(js[j]) {
		j++
	}
	return end, j < len(js) && js[j] == ':'
}

func isKeyByte(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isJSONSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// parseObjectsInvHeader reads the plain-text header of an objects.inv
// (its "# Project:" and "# Version:" lines); ok is false for anything
// that is not a version 2 inventory.
func parseObjectsInvHeader(body []byte) (project, version string, ok bool) {
	lines := bytes.SplitN(body, []byte("\n"), 5)
	if len(lines) < 4 {
		return "", "", false
	}
	line := func(i int) string { return strings.TrimSpace(string(lines[i])) }
	p, okP := strings.CutPrefix(line(1), "# Project:")
	v, okV := strings.CutPrefix(line(2), "# Version:")
	if line(0) != "# Sphinx inventory version 2" || !okP || !okV || !strings.HasPrefix(line(3), "#") {
		return "", "", false
	}
	return strings.TrimSpace(p), strings.TrimSpace(v), true
}

var (
	docOptBuilderRe = regexp.MustCompile(`\bBUILDER\s*:\s*['"]([^'"]*)['"]`)
	docOptVersionRe = regexp.MustCompile(`\bVERSION\s*:\s*['"]([^'"]*)['"]`)
	docOptSuffixRe  = regexp.MustCompile(`\bLINK_SUFFIX\s*:\s*['"]([^'"]*)['"]`)
)

// parseDocOptions reads documentation_options.js. A file that does not
// state LINK_SUFFIX gets Sphinx's default, ".html".
func parseDocOptions(body []byte) sphinxOptions {
	o := sphinxOptions{linkSuffix: ".html"}
	if m := docOptBuilderRe.FindSubmatch(body); m != nil {
		o.builder = strings.TrimSpace(string(m[1]))
	}
	if m := docOptVersionRe.FindSubmatch(body); m != nil {
		o.version = strings.TrimSpace(string(m[1]))
	}
	if m := docOptSuffixRe.FindSubmatch(body); m != nil {
		o.linkSuffix = strings.TrimSpace(string(m[1]))
	}
	return o
}
