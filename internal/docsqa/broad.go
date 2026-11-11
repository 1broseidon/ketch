package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/internal/docsindex"
)

// verify checks every quote of the cases at casesPath against the
// sandbox's docs index with the grading's normalisation, and writes to out
// the cases that can be graded: library as the index lists its root, each
// quote's source the indexed section that holds it (on the source page
// when it is there, else wherever the library serves it), alternatives with
// a quote not in the index left out. A case is dropped when one of its
// facts has no alternative left, or when a not_found term is in the index.
// Each dropped case is printed with its reason.
func verify(w io.Writer, sandboxDir, casesPath, out string) error {
	cases, err := readCases(casesPath, "")
	if err != nil {
		return err
	}
	store := docsindex.Open(filepath.Join(sandboxDir, "docs-index.sqlite"))
	libs, err := store.Libraries()
	if err != nil {
		return err
	}
	var full []docsindex.Library
	for _, l := range libs {
		if l.Current() && l.Resolved != "search" {
			full = append(full, l)
		}
	}
	pr := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	indexed := map[string]*libText{}
	var kept []qaCase
	reasons := map[string]int{}
	for _, c := range cases {
		m := docs.MatchIndexed(full, c.Library)
		reason, detail := "library not indexed", c.Library
		if len(m) > 0 {
			lt := indexed[m[0].Key]
			if lt == nil {
				if lt, err = loadLibText(store, m[0]); err != nil {
					return err
				}
				indexed[m[0].Key] = lt
			}
			c.Library = m[0].Root
			reason, detail = verifyCase(&c, lt)
		}
		if reason != "" {
			reasons[reason]++
			pr("drop %-44s %-28s %s\n", c.ID, reason, clip(detail, 120))
			continue
		}
		kept = append(kept, c)
	}
	pr("kept %d of %d cases\n", len(kept), len(cases))
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		pr("dropped, %s: %d\n", k, reasons[k])
	}
	b, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, append(b, '\n'), 0o644)
}

// verifyCase resolves c's facts against lt, the case's library's text,
// in place; reason is why c cannot be graded ("" when it can), with the
// quote, page or term that decides it.
func verifyCase(c *qaCase, lt *libText) (reason, detail string) {
	for i, f := range c.Facts {
		var alts [][]anchor
		why, whyDetail := "", ""
		for _, alt := range f.alternatives() {
			resolved, state, missing := lt.resolve(alt)
			if state == "" {
				alts = append(alts, resolved)
			} else if rankReason(state) > rankReason(why) {
				// A lost fact is reported by its closest alternative.
				why, whyDetail = state, missing
			}
		}
		if len(alts) == 0 {
			return why, whyDetail
		}
		c.Facts[i] = fact{Alternatives: alts}
		if len(alts) == 1 && len(alts[0]) == 1 {
			c.Facts[i] = fact{Quote: alts[0][0].Quote, Source: alts[0][0].Source}
		}
	}
	for _, term := range c.NotFound {
		if lt.contains(normalize(term)) {
			return "not_found term in index", term
		}
	}
	return "", ""
}

// rankReason orders the reasons a quote is not in the index from the
// furthest from the index to the closest.
func rankReason(r string) int {
	switch r {
	case "page not indexed":
		return 1
	case "quote not on page":
		return 2
	case "quote differs in markup":
		return 3
	}
	return 0
}

// libText is a library's indexed sections, normalised for the checks.
type libText struct {
	sections []indexedSection
	pages    map[string]bool // by pageKey
}

type indexedSection struct {
	page, anchor, pageKey string
	text, loose           string
}

func loadLibText(store *docsindex.Store, l docsindex.Library) (*libText, error) {
	secs, err := store.Sections(l.Key)
	if err != nil {
		return nil, err
	}
	lt := &libText{pages: map[string]bool{}}
	for _, s := range secs {
		text := normalize(s.Heading + "\n" + s.Text)
		lt.sections = append(lt.sections, indexedSection{page: s.Page, anchor: s.Anchor, pageKey: pageKey(s.Page), text: text, loose: loosen(text)})
		lt.pages[pageKey(s.Page)] = true
	}
	return lt, nil
}

// resolve finds each quote of alt in the index and returns alt with each
// source the section that holds the quote. state is "" when every quote is
// found, else why the first missing one is not: its page is not indexed,
// or it is not on any indexed page, or only a looser match (whitespace,
// Markdown links and emphasis, typographic quotes) finds it.
func (lt *libText) resolve(alt []anchor) (out []anchor, state, missing string) {
	for _, a := range alt {
		q := normalize(a.Quote)
		pk := pageKey(a.Source)
		var hit *indexedSection
		for i := range lt.sections {
			s := &lt.sections[i]
			if strings.Contains(s.text, q) && (hit == nil || s.pageKey == pk && hit.pageKey != pk) {
				hit = s
			}
		}
		if hit == nil {
			switch {
			case lt.containsLoose(loosen(q)):
				return nil, "quote differs in markup", a.Quote
			case lt.pages[pk]:
				return nil, "quote not on page", a.Quote
			default:
				return nil, "page not indexed", a.Source
			}
		}
		src := hit.page
		if hit.anchor != "" {
			src += "#" + hit.anchor
		}
		out = append(out, anchor{Quote: a.Quote, Source: src})
	}
	return out, "", ""
}

func (lt *libText) contains(q string) bool {
	for _, s := range lt.sections {
		if strings.Contains(s.text, q) {
			return true
		}
	}
	return false
}

func (lt *libText) containsLoose(q string) bool {
	for _, s := range lt.sections {
		if strings.Contains(s.loose, q) {
			return true
		}
	}
	return false
}

// loosen is normalised text with what the bench's own matching ignores
// also gone: Markdown link targets and emphasis, typographic quotes and
// dashes, and all whitespace. It only labels a quote the index lacks.
func loosen(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if strings.HasPrefix(s[i:], "](") {
			if j := strings.IndexByte(s[i:], ')'); j > 0 {
				i += j
				continue
			}
		}
		switch c := s[i]; c {
		case ' ', '[', ']', '*', '_', '"', '\'':
			continue
		default:
			b.WriteByte(c)
		}
	}
	r := strings.NewReplacer("“", "", "”", "", "‘", "", "’", "", "—", "-", "–", "-", "…", "...")
	return r.Replace(b.String())
}

// pageKey is a page URL as the checks compare it: no scheme, www., query,
// fragment, trailing slash or .html.
func pageKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.ToLower(raw)
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	p := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/index.html"), ".html")
	return host + p
}

// compare prints one table for several results files: per case, each
// run's answered, tokens printed, tokens before the first fact and wall
// time; then each run's totals, answered by kind, and for each pair of
// runs the cases one answered and the other did not. split, when not ""
// or "all", keeps the cases of that half only.
func compare(w io.Writer, paths, labels []string, split string) error {
	if len(paths) == 0 {
		return fmt.Errorf("-compare needs results files")
	}
	var runs []*report
	for i, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var r report
		if err := json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		r.Results = slices.DeleteFunc(r.Results, func(c caseResult) bool { return !inSplit(c.Split, split) })
		for k := range r.Results {
			r.Results[k].Status = statusName(r.Results[k].Status)
		}
		runs = append(runs, &r)
		if i >= len(labels) {
			labels = append(labels, strings.TrimSuffix(filepath.Base(p), ".json"))
		}
	}
	pr := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	ids, byID := caseIndex(runs)
	compareCases(pr, runs, labels, ids, byID)
	compareTotals(pr, runs, labels)
	compareKinds(pr, runs, labels)
	pr("\n")
	for i := range runs {
		for j := i + 1; j < len(runs); j++ {
			for _, d := range [][2]int{{i, j}, {j, i}} {
				var only []string
				for _, id := range ids {
					a, okA := byID[d[0]][id]
					b, okB := byID[d[1]][id]
					if okA && okB && a.Answered && !b.Answered {
						only = append(only, id)
					}
				}
				pr("%s answered, %s not: %d %s\n", labels[d[0]], labels[d[1]], len(only), strings.Join(only, " "))
			}
		}
	}
	return nil
}

// printer prints formatted text.
type printer func(format string, a ...any)

// caseIndex is every case ID the runs hold, in first-seen order, and each
// run's results by ID.
func caseIndex(runs []*report) ([]string, []map[string]caseResult) {
	var ids []string
	seen := map[string]bool{}
	byID := make([]map[string]caseResult, len(runs))
	for i, r := range runs {
		byID[i] = map[string]caseResult{}
		for _, c := range r.Results {
			byID[i][c.ID] = c
			if !seen[c.ID] {
				seen[c.ID] = true
				ids = append(ids, c.ID)
			}
		}
	}
	return ids, byID
}

// compareCases prints one row per case, one column group per run:
// answered, tokens, before fact, ms.
func compareCases(pr printer, runs []*report, labels, ids []string, byID []map[string]caseResult) {
	pr("%-46s", "case")
	for _, l := range labels {
		pr(" | %-22s", clip(l, 22))
	}
	pr("\n%-46s", "")
	for range labels {
		pr(" | %3s %6s %6s %5s", "ans", "tokens", "before", "ms")
	}
	pr("\n")
	for _, id := range ids {
		pr("%-46s", clip(id, 46))
		for i := range runs {
			c, ok := byID[i][id]
			if !ok {
				pr(" | %-22s", "(not run)")
				continue
			}
			before := "-"
			if c.BeforeFact != nil {
				before = strconv.Itoa(*c.BeforeFact)
			}
			pr(" | %3s %6d %6s %5d", yesNo(c.Answered, len(c.Facts) > 0), c.Tokens, before, c.WallMS)
		}
		pr("\n")
	}
}

// compareTotals prints each run's totals.
func compareTotals(pr printer, runs []*report, labels []string) {
	pr("\n%-10s %10s %10s %10s %10s %8s %8s %13s %8s %13s  %s\n", "run", "answered", "led words", "led cut", "ans|led", "mean tok", "med tok", "med before", "med ms", "not_found", "answered, by status")
	for i, r := range runs {
		var answered, withFacts, either, withEither, words, withLeads, cut int
		var tokens, before, ms []int
		var nf tally
		statuses := map[string]int{}
		for _, c := range r.Results {
			if len(c.Facts) > 0 {
				withFacts++
				if c.Answered {
					answered++
					statuses[c.Status]++
				}
			}
			if len(c.Leads) > 0 {
				withLeads++
				if c.Led {
					words++
				}
			}
			if c.LedByCut {
				cut++
			}
			if len(c.Facts) > 0 || len(c.Leads) > 0 {
				withEither++
				if c.Answered || c.Led || c.LedByCut {
					either++
				}
			}
			tokens = append(tokens, c.Tokens)
			ms = append(ms, int(c.WallMS))
			if c.BeforeFact != nil {
				before = append(before, *c.BeforeFact)
			}
			for _, n := range c.NotFound {
				nf.add(n.Stated)
			}
		}
		pr("%-10s %10s %10s %10s %10s %8d %8s %13s %8s %13s  %s\n", clip(labels[i], 10), fmt.Sprintf("%d/%d", answered, withFacts),
			fmt.Sprintf("%d/%d", words, withLeads), fmt.Sprintf("%d/%d", cut, withFacts),
			fmt.Sprintf("%d/%d", either, withEither), mean(tokens), median(tokens), median(before), median(ms), frac(nf), statusShares(statuses))
	}
}

// compareKinds prints each run's answered cases by kind.
func compareKinds(pr printer, runs []*report, labels []string) {
	kinds := map[string]bool{}
	for _, c := range runs[0].Results {
		if c.Kind != "" {
			kinds[c.Kind] = true
		}
	}
	if len(kinds) == 0 {
		return
	}
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, k)
	}
	sort.Strings(names)
	pr("\n%-24s", "answered by kind")
	for _, l := range labels {
		pr(" %10s", clip(l, 10))
	}
	pr("\n")
	for _, k := range names {
		pr("%-24s", clip(k, 24))
		for _, r := range runs {
			var t tally
			for _, c := range r.Results {
				if c.Kind == k && len(c.Facts) > 0 {
					t.add(c.Answered)
				}
			}
			pr(" %10s", frac(t))
		}
		pr("\n")
	}
}

func mean(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	s := 0
	for _, x := range xs {
		s += x
	}
	return s / len(xs)
}

// median is xs's median, "-" for none.
func median(xs []int) string {
	if len(xs) == 0 {
		return "-"
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	if n := len(s); n%2 == 1 {
		return strconv.Itoa(s[n/2])
	} else {
		return strconv.Itoa((s[n/2-1] + s[n/2]) / 2)
	}
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
