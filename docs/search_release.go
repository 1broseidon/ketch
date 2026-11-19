package docs

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// A release named in words (design/docs-evidence.md, Final shape): a
// question that says "Spring Boot 2.7", "Vue 2" or "Laravel 9" asks about
// that release. The docs hits whose address names it go first, in the
// search's order; an answer from pages none of which is marked with it
// says so in one line, and only an answer from pages marked with it
// prints the release as its version.

// sfRelease is a release the question names: the name before it as
// written ("Spring Boot") and the release ("2.7").
type sfRelease struct {
	name, release string
}

// releaseWordRe matches a release as a question writes it after a name:
// 2, 2.7, v3, 9.x, 4x, 3.4.1. A major of three digits or more is a count
// or a setting (500 URLs, as 137), not a release.
var releaseWordRe = regexp.MustCompile(`^[vV]?(\d{1,2}(?:\.\d+){0,2})(?:\.[xX]|[xX])?$`)

// releaseUnits are words after a number that make it a quantity, not a
// release: "Go 1.23 or later" is a release, "Kafka 3 partitions" is not.
var releaseUnits = map[string]bool{
	"s": true, "ms": true, "sec": true, "secs": true, "second": true, "seconds": true, "minute": true, "minutes": true,
	"hour": true, "hours": true, "day": true, "days": true, "times": true, "items": true, "urls": true, "partitions": true,
	"servers": true, "threads": true, "tasks": true, "retries": true, "tries": true, "bytes": true, "kb": true, "mb": true,
	"gb": true, "percent": true, "%": true, "characters": true, "chars": true, "rows": true, "requests": true,
}

// questionRelease is the first release the question names after a name
// that starts with a capital letter or a dot (Vue 2, .NET 9, Spring Boot
// 2.7): the name is up to two such words before it. A name written as an
// option (ServerAliveInterval 15: two inner capitals after lower-case
// letters) is a setting and its value, not a release.
func questionRelease(query string) (sfRelease, bool) {
	words := strings.Fields(query)
	for i := 1; i < len(words); i++ {
		w := strings.TrimRight(words[i], ",.;:!?)")
		m := releaseWordRe.FindStringSubmatch(w)
		if m == nil || strings.ContainsAny(words[i-1], ",;:()@=/") {
			continue
		}
		if i+1 < len(words) && releaseUnits[strings.ToLower(strings.Trim(words[i+1], ",.;:!?()"))] {
			continue
		}
		var name []string
		for j := i - 1; j >= 0 && j >= i-2; j-- {
			p := strings.Trim(words[j], "\"'(")
			if !releaseName(p) || j < i-1 && strings.ContainsAny(p, ",;:") {
				break
			}
			name = append([]string{p}, name...)
		}
		if len(name) == 0 {
			continue
		}
		return sfRelease{name: strings.Join(name, " "), release: m[1]}, true
	}
	return sfRelease{}, false
}

// releaseName reports a word a release can follow: one that starts with a
// capital letter or a dot and is not an option name (two or more
// lower-to-upper turns: ServerAliveInterval).
func releaseName(w string) bool {
	r := []rune(strings.TrimPrefix(w, "."))
	if len(r) < 2 || !unicode.IsUpper(r[0]) && !strings.HasPrefix(w, ".") {
		return false
	}
	turns := 0
	for i := 1; i < len(r); i++ {
		if unicode.IsLower(r[i-1]) && unicode.IsUpper(r[i]) {
			turns++
		}
	}
	return turns < 2
}

// releaseLabel reports a host label or path segment that names release:
// 2.7.x, v2, 9.x, 4x, 1.10, 20 for 2.0 (a release written without its
// dot), and 5.x for 5.4 (a label with an x names every release under it).
func releaseLabel(label, release string) bool {
	l := strings.ToLower(label)
	l = strings.TrimPrefix(l, "v")
	trimmed := strings.TrimSuffix(strings.TrimSuffix(l, ".x"), "x")
	wild := trimmed != l
	l = trimmed
	if l == "" || strings.Trim(l, "0123456789.") != "" {
		return false
	}
	return l == release || strings.HasPrefix(l, release+".") || wild && strings.HasPrefix(release, l+".") ||
		strings.Contains(release, ".") && !strings.Contains(l, ".") && l == strings.ReplaceAll(release, ".", "")
}

// urlNamesRelease reports an address whose first host label or one of
// whose directories names release.
func urlNamesRelease(u *url.URL, release string) bool {
	first, _, _ := strings.Cut(scopeHost(u), ".")
	if releaseLabel(first, release) {
		return true
	}
	segs := pathSegments(u.Path)
	for i, seg := range segs {
		if i == len(segs)-1 && !strings.HasSuffix(u.Path, "/") {
			break // the page's own name
		}
		if releaseLabel(seg, release) {
			return true
		}
	}
	return false
}

// titleNamesRelease reports a title that has release as a word of its own
// (Django 5.2 documentation, Vue 2 Guide), or v and the release.
func titleNamesRelease(title, release string) bool {
	for _, w := range strings.FieldsFunc(title, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.'
	}) {
		w = strings.Trim(strings.ToLower(w), ".")
		if strings.TrimPrefix(w, "v") == release || strings.HasPrefix(strings.TrimPrefix(w, "v"), release+".") {
			return true
		}
	}
	return false
}

// namesThing reports a hit about the thing r names: its name run
// together (springboot, vuejs, tailwindcss) is a label of its address, or
// each of its words is a label of its address or a word of its title
// (tanstack.com/query/v5 for TanStack Query; not spring-data/redis for
// Spring Boot).
func namesThing(h sfHit, r sfRelease) bool {
	labels := map[string]bool{}
	for _, l := range append(strings.Split(scopeHost(h.u), "."), pathSegments(h.u.Path)...) {
		labels[libraryKey(l)] = true
		for _, part := range strings.FieldsFunc(l, func(c rune) bool { return c == '-' || c == '_' || c == '.' }) {
			labels[libraryKey(part)] = true
		}
	}
	for _, w := range strings.FieldsFunc(h.title, func(c rune) bool { return !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '.' }) {
		labels[libraryKey(w)] = true
	}
	words := strings.Fields(r.name)
	if k := libraryKey(r.name); len(words) > 1 && labels[k] {
		return true
	}
	for _, w := range words {
		if k := libraryKey(w); k == "" || !labels[k] {
			return false
		}
	}
	return true
}

// releaseFirst is docs with the hits about the thing r names whose
// address names its release first, each group in the search's order (pos
// renumbered).
func releaseFirst(docs []sfHit, r sfRelease) []sfHit {
	var named, rest []sfHit
	for _, h := range docs {
		if urlNamesRelease(h.u, r.release) && namesThing(h, r) {
			named = append(named, h)
		} else {
			rest = append(rest, h)
		}
	}
	if len(named) == 0 {
		return docs
	}
	out := append(named, rest...)
	for i := range out {
		out[i].pos = i
	}
	return out
}

// markRelease is o, an answer from pages a search read to a question that
// names r: its version the release when a page read is marked with it
// (address or title); else, unless the text printed names the thing with
// that release (Go 1.23, Pydantic V2: the pages speak of it), a note that
// none is marked, and what to do.
func markRelease(o Outcome, r sfRelease) Outcome {
	if !answered(o) || len(o.Read) == 0 {
		return o
	}
	marked, spoken := releaseShown(o, r)
	if !marked {
		if !spoken {
			o.Note = joinNote(o.Note, fmt.Sprintf("the question names %s %s and no page read is marked %s: they may document another release; pass that release's docs URL as --library", r.name, r.release, r.release))
		}
		return o
	}
	for i := range o.Results {
		o.Results[i].Version = r.release
	}
	return o
}

// releaseShown reports whether a page o read is marked with r's release
// (address or title), and whether the text it prints names the thing with
// that release.
func releaseShown(o Outcome, r sfRelease) (marked, spoken bool) {
	for _, p := range o.Read {
		if u, err := url.Parse(p.URL); err == nil && urlNamesRelease(u, r.release) {
			marked = true
		}
	}
	for _, res := range o.Results {
		marked = marked || titleNamesRelease(res.PageTitle, r.release)
		spoken = spoken || textNamesRelease(res.Snippet, r)
	}
	return marked, spoken
}

// releaseUnmarked reports an answer markRelease notes: none of its pages
// is marked with r's release and its text does not name it.
func releaseUnmarked(o Outcome, r sfRelease) bool {
	if !answered(o) || len(o.Read) == 0 {
		return false
	}
	marked, spoken := releaseShown(o, r)
	return !marked && !spoken
}

// textNamesRelease reports text that writes the thing r names followed by
// its release, as the question does or with a v (Go 1.23, Pydantic V2,
// .NET 9, Python 3.10.4 for 3.10; not Python 3.1 for 3.10).
func textNamesRelease(text string, r sfRelease) bool {
	words := strings.Fields(regexp.QuoteMeta(r.name))
	if len(words) == 0 || text == "" {
		return false
	}
	re, err := regexp.Compile(`(?i)(?:^|[^\w.])` + strings.Join(words, `\s+`) + `\s+v?` + regexp.QuoteMeta(r.release) + `(?:$|[^\w.]|\.(?:[\dxX]|\s|$))`)
	return err == nil && re.MatchString(text)
}
