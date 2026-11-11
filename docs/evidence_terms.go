package docs

import (
	"slices"
	"strings"
)

// Terms as written (design/docs-evidence.md, Output): the frontmatter
// names the question's terms as the question writes them, not as the
// pipeline normalizes them. A written term is one word of the question
// (its quotes and closing punctuation trimmed), and its forms are what
// the full-text table holds of it:
//
//   - an identifier the question writes (queryIdents recognises it:
//     pam_sss, PAM_SERVICE_ERR, useEffect): the whole names of its parts
//     (pamsss), not its words, which every PAM page holds;
//   - a host name (duosecurity.com): its labels as one phrase;
//   - a word joined by hyphens (retry-on-failure): its words as one
//     phrase;
//   - any other word: its normalized terms that are in the question's
//     term list (filler and stopwords are no term).
//
// Its count is the library's sections whose full-text row holds any of
// its forms; it is in not_found when that is none. A written term is
// distinctive when it is an identifier or in under a tenth of the
// library's sections (every written term when none is). Two words with
// the same forms (configure, configuration) are one term, the first
// spelling.

// writtenTerm is one term as the question writes it.
type writtenTerm struct {
	text  string // as written
	ident bool   // an identifier the question writes (queryIdents)
	match string // the full-text query for its forms
	// forms are its forms as printed text's terms (pieceTokens) name them:
	// it is in a text that names every term of one of them.
	forms       [][]string
	sections    int // the library's sections holding any of its forms
	distinctive bool
}

// TermCount is a written term and the library's sections holding it.
type TermCount struct {
	Term     string `json:"term"`
	Sections int    `json:"sections"`
}

// writtenTerms are the question's written terms, in the order it writes
// them, with the full-text query for each; terms is the question's one
// term list, which a plain word's forms must be in.
func writtenTerms(query string, terms []evidenceTerm) []writtenTerm {
	inList := map[string]bool{}
	for _, t := range terms {
		inList[t.term] = true
	}
	var out []writtenTerm
	seen := map[string]bool{}
	for _, w := range strings.Fields(query) {
		w = strings.Trim(w, queryQuotes+",.;:!?")
		if w == "" {
			continue
		}
		wt := writtenTerm{text: w}
		lower := strings.ToLower(w)
		var forms []string
		switch {
		case isHostName(lower):
			forms = []string{ftsTerm(lower)}
			wt.forms = [][]string{{lower}}
		case len(queryIdents(w)) > 0:
			wt.ident = true
			for _, k := range append(queryIdentKeys(w), queryIdents(w)...) {
				if q := ftsQuote(k); !slices.Contains(forms, q) {
					forms = append(forms, q)
					wt.forms = append(wt.forms, []string{k})
				}
			}
		case hyphenCompound(w):
			forms = []string{ftsQuote(strings.Join(rankTokens(w), " "))}
			wt.forms = [][]string{rankTokens(w)}
		default:
			for _, t := range tokenize(w) {
				if n := normTerm(t); inList[n] && !slices.Contains(forms, ftsQuote(n)) {
					forms = append(forms, ftsQuote(n))
					wt.forms = append(wt.forms, []string{n})
				}
			}
		}
		if len(forms) == 0 {
			continue
		}
		key := strings.Join(slices.Sorted(slices.Values(forms)), " ")
		if seen[key] {
			continue
		}
		seen[key] = true
		wt.match = strings.Join(forms, " OR ")
		out = append(out, wt)
	}
	return out
}

// hyphenCompound reports a word of two or more words joined by hyphens
// (retry-on-failure), letters on both sides of each.
func hyphenCompound(w string) bool {
	parts := strings.Split(w, "-")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if countLetters(p) == 0 {
			return false
		}
	}
	return true
}

// markDistinctive marks the written terms that tell the answer apart in
// a library of n sections (every term when none does).
func markDistinctive(wts []writtenTerm, n int) {
	some := false
	for i := range wts {
		wts[i].distinctive = wts[i].ident || wts[i].sections*distinctiveShare < n
		some = some || wts[i].distinctive
	}
	for i := range wts {
		wts[i].distinctive = wts[i].distinctive || !some
	}
}

// Terms shown: the frontmatter's terms lists, of the distinctive terms
// found, the identifiers the question writes and the words in under
// 1/shownShare of the library's sections; in a library too small for
// that to be one section, the words in at most shownFew. A word every
// tenth section holds tells the caller nothing; the refusal and
// not_found keep reading every distinctive term.
const (
	shownShare = 100
	shownFew   = 3
)

// shownTerm reports a written term found in a library of n sections that
// the frontmatter's terms lists.
func shownTerm(wt writtenTerm, n int) bool {
	switch {
	case wt.sections == 0:
		return false
	case wt.ident:
		return true
	case n < shownShare:
		return wt.sections <= shownFew
	}
	return wt.sections*shownShare < n
}

// termVerdict is what the written terms say about an answer: the
// distinctive terms found that the frontmatter lists (shownTerm) with
// their counts, those with no section, and whether the answer is refused
// (no distinctive term has a hit) or partial for them.
type termVerdict struct {
	found    []TermCount
	notFound []string
	refused  bool
	partial  bool
}

// verdict reads the written terms of a library of n sections: refused
// when they are some and no distinctive one has a hit; partial when a
// distinctive one has none.
func verdict(wts []writtenTerm, n int) termVerdict {
	var v termVerdict
	hit := false
	for _, wt := range wts {
		switch {
		case !wt.distinctive:
		case wt.sections > 0:
			hit = true
			if shownTerm(wt, n) {
				v.found = append(v.found, TermCount{Term: wt.text, Sections: wt.sections})
			}
		default:
			v.notFound = append(v.notFound, wt.text)
			v.partial = true
		}
	}
	v.refused = len(wts) > 0 && !hit
	return v
}

// notShown are the distinctive written terms the library holds that no
// printed result names, in its text or its heading path, as written.
func notShown(wts []writtenTerm, results []Result) []string {
	var named []map[string]bool
	for _, r := range results {
		toks := pieceTokens(r.Snippet)
		for t := range headingSet(append([]string{r.PageTitle, r.Title}, r.HeadingPath...)...) {
			toks[t] = true
		}
		for _, h := range hostTerms(r.PageTitle + " " + strings.Join(r.HeadingPath, " ")) {
			toks[h] = true
		}
		named = append(named, toks)
	}
	var out []string
	for _, wt := range wts {
		if !wt.distinctive || wt.sections == 0 || slices.ContainsFunc(named, func(toks map[string]bool) bool { return namesTerm(toks, wt) }) {
			continue
		}
		out = append(out, wt.text)
	}
	return out
}

// namesTerm reports a text whose terms (toks) name every term of one of
// wt's forms.
func namesTerm(toks map[string]bool, wt writtenTerm) bool {
	for _, f := range wt.forms {
		if len(f) > 0 && !slices.ContainsFunc(f, func(t string) bool { return !toks[t] }) {
			return true
		}
	}
	return false
}
