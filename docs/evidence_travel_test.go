package docs

import (
	"slices"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// A piece brings the piece before it when it is a code block introduced by
// a lead-in ending in a colon, or when it opens with a word that points
// back. A lead-in does not bring its code, a lead-in that does not end in
// a colon is no lead-in, and table rows bring nothing.
func TestLinkPieces(t *testing.T) {
	for _, c := range []struct {
		name, text string
		want       map[int][]int // ord: the ords it brings
	}{
		{"code brings its lead-in", "Pre-populate the hashes with the lock command:\n\n```\nterraform providers lock\n```", map[int][]int{1: {0}}},
		{"lead-in in a long paragraph", strings.Repeat("Some words before it here. ", 30) + "To disable them entirely, use:\n\n```\nSET x = false;\n```", map[int][]int{31: {30}}},
		{"no colon, no lead-in", "Here is an example.\n\n```\nx = 1\n```", nil},
		{"text between them", "Use this:\n\nshell\n\n```\nx\n```\n\nNot me:\n\n| a | b |\n|---|---|\n| c | d |", map[int][]int{1: {0}}},
		{"or points back", "Return a serializable type.\n\n*or* an object that implements IntoResponse.", map[int][]int{1: {0}}},
		{"this points back", "Use `Connection.begin()` to begin once.\n\nThis object also supports a context manager.", map[int][]int{1: {0}}},
		{"in that case", "Set a URI in the directive.\n\nIn that case, it replaces the matched part.", map[int][]int{1: {0}}},
		{"the above", "```\nterraform providers lock\n```\n\nThe above command downloads every package.", map[int][]int{1: {0}}},
		{"a word that only starts like one", "Use it.\n\nThistle is a plant. Order matters.", nil},
		{"rows bring nothing", "| a | b |\n|---|---|\n| x | y |\n| or | this |", nil},
	} {
		ps, _ := splitText(c.text)
		got := map[int][]int{}
		for _, p := range ps {
			if len(p.with) > 0 {
				got[p.ord] = p.with
			}
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: links %v, want %v", c.name, got, c.want)
			continue
		}
		for k, v := range c.want {
			if !slices.Equal(got[k], v) {
				t.Errorf("%s: links %v, want %v", c.name, got, c.want)
			}
		}
	}
}

// A one-line paragraph that does not end like a sentence (a signature, a
// term and its type) heads the prose paragraph after it: the two are one
// piece. A sentence, a lead-in, a line before a list or code, and a long
// line head nothing.
func TestDefinitionHeads(t *testing.T) {
	for _, c := range []struct {
		name, text string
		want       []string
	}{
		{"signature", "PIL.ImageOps.fit(*image*, *size*) → Image\n\nReturns a resized and cropped version of the image.",
			[]string{"PIL.ImageOps.fit(*image*, *size*) → Image\n\nReturns a resized and cropped version of the image."}},
		{"term and type", "mode string (Optional, default: single)\n\nControls what happens when the automation runs again.",
			[]string{"mode string (Optional, default: single)\n\nControls what happens when the automation runs again."}},
		{"a code term", "`r.requestText`\n\nclient request body if it has not been written to a temporary file.",
			[]string{"`r.requestText`\n\nclient request body if it has not been written to a temporary file."}},
		{"a sentence", "This is a sentence.\n\nThis is another.", []string{"This is a sentence.", "This is another."}},
		{"a lead-in", "Parameters:\n\nThe name of it.", []string{"Parameters:", "The name of it."}},
		{"before a list", "name (str, optional, default: none)\n\n- the name", []string{"name (str, optional, default: none)", "- the name"}},
		{"before code", "fetch(url)\n\n```\nfetch('/x')\n```", []string{"fetch(url)\n\n```\nfetch('/x')\n```"}},
		{"long", strings.Repeat("word ", 120) + "\n\nNext paragraph.", []string{strings.TrimSpace(strings.Repeat("word ", 120)), "Next paragraph."}},
	} {
		ps, _ := splitText(c.text)
		var got []string
		for _, p := range ps {
			got = append(got, p.text)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// travelSection is a section longer than a page whose code block is the
// best piece, introduced by a lead-in that names nothing of the question,
// and a later sentence that points back at a long paragraph.
func travelSection() docsindex.Section {
	filler := strings.Repeat("Words of no interest at all here. ", 30)
	text := filler + "\n\n" + strings.Repeat("More words that say nothing. ", 30) + "To do it for several platforms at once, run:\n\n```\nterraform providers lock -platform=linux_amd64\n```\n\n" +
		filler + "\n\nGamma words fill a paragraph here.\n\nThis gamma words line points back."
	return evSection("t", 0, "t", []string{"T", "Lock"}, text)
}

// A cut group that prints a code block prints its lead-in with it, and a
// piece that points back prints the piece before it; neither is charged
// to the page a long section is cut to.
func TestWalkBringsContext(t *testing.T) {
	s := sortPool([]docsindex.Section{travelSection()}, evRoot)
	if !overPage(s.secs[0].tokens) {
		t.Fatalf("fixture: %d tokens", s.secs[0].tokens)
	}
	g := evGathered("terraform providers lock platform", 1000, map[string]int{"terraform": 50, "providers": 20, "lock": 5, "platform": 5})
	lib := docsindex.Library{Root: evRoot}
	r := evRanked(s, g, 1500, 0)
	printSections(lib, s, r, HintNone)
	if len(r.plans) != 1 {
		t.Fatalf("plans = %+v", r.plans)
	}
	res, _ := fitResults(lib, s, r, HintNone, nil)
	if !strings.Contains(res[0].Snippet, "To do it for several platforms at once, run:\n\n```\nterraform providers lock") {
		t.Errorf("the code's lead-in is not printed with it:\n%s", res[0].Snippet)
	}
	if len(r.plans[0].with) == 0 || pageTokens(s, s.secs[0], r.plans[0]) >= estimateTokens(joinPieces(s, s.secs[0], r.plans[0].pieces)) {
		t.Errorf("the lead-in is charged to the page: %+v", r.plans[0])
	}

	g = evGathered("gamma points back", 1000, map[string]int{"gamma": 2, "points": 30, "back": 30})
	r = evRanked(s, g, 1500, 0)
	printSections(lib, s, r, HintNone)
	res, _ = fitResults(lib, s, r, HintNone, nil)
	if !strings.Contains(res[0].Snippet, "Gamma words fill a paragraph here.\n\nThis gamma words line points back.") {
		t.Errorf("the piece pointed back at is not printed:\n%s", res[0].Snippet)
	}
}

// A unit holds at most unitMaxPieces pieces, in page order.
func TestUnitOf(t *testing.T) {
	text := "One.\n\nThis two.\n\nThis three.\n\nThis four.\n\nThis five."
	s := sortPool([]docsindex.Section{evSection("u", 0, "u", []string{"U"}, text)}, evRoot)
	if got := unitOf(s, 4); !slices.Equal(got, []int{1, 2, 3, 4}) {
		t.Errorf("unitOf = %v", got)
	}
	if got := unitOf(s, 0); !slices.Equal(got, []int{0}) {
		t.Errorf("unitOf(first) = %v", got)
	}
}
