package docs

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/search"
)

const (
	pgArt1        = "https://alpha.test/posts/widget-timeouts"
	pgArt2        = "https://beta.test/articles/widget"
	pgArt3        = "https://gamma.test/widget-notes"
	pgArt4        = "https://delta.test/writing/widget"
	pgArt5        = "https://epsilon.test/widget"
	pgLinkDocs    = "https://docs.widgetco.test/guide/timeouts"
	linkedRootURL = "https://docs.widgetco.test/guide/"
)

// articleSite serves five readable articles, links[i] put in article i+1's
// text, and the makers' docs page, a search result after the top five
// (the --library read of its site finds it there).
func articleSite(links ...string) (*fakeFetcher, []search.Result) {
	arts := []string{pgArt1, pgArt2, pgArt3, pgArt4, pgArt5}
	f := &fakeFetcher{pages: map[string]fakePage{
		pgLinkDocs: docsPage("Timeouts", "## Read timeout\n\nThe read_timeout setting defaults to 10 seconds. DOCSFACT."),
	}}
	var hits []search.Result
	for i, u := range arts {
		sec := "## My setup\n\nI set the read_timeout of the widget to 30 seconds, ARTFACT."
		if i < len(links) && links[i] != "" {
			sec += " See [the widget docs](" + links[i] + ") and [me](https://twitter.com/someone)."
		}
		f.pages[u] = docsPage("Widget timeouts "+string(rune('A'+i)), sec)
		hits = append(hits, hit(u, "Widget timeouts "+string(rune('A'+i))))
	}
	hits = append(hits, search.Result{URL: pgLinkDocs, Title: "Timeouts", Description: "The read_timeout setting."})
	return f, hits
}

// articleJudgments label every article an article on the question and
// the docs page the makers' docs.
func articleJudgments() *fakeDecider {
	d := &fakeDecider{pages: map[string]PageJudgments{
		pgLinkDocs: {Documentation: 0.95, Makers: 0.95, Kind: "reference_documentation"},
	}, helps: func(text string) float64 {
		switch {
		case strings.Contains(text, "ARTFACT"):
			return 0.8
		case strings.Contains(text, "DOCSFACT"):
			return 0.95
		}
		return 0.05
	}}
	for _, u := range []string{pgArt1, pgArt2, pgArt3, pgArt4, pgArt5} {
		d.pages[u] = PageJudgments{Documentation: 0.1, Makers: 0.1, Kind: "blog_post_or_article"}
	}
	return d
}

const todayNote = "no result read is documentation from the makers or a reference; the page read is an article"

func fetchedHost(f *fakeFetcher, host string) bool {
	for _, c := range f.called() {
		if strings.Contains(c, host) {
			return true
		}
	}
	return false
}

// (a) Two of the articles link to one docs site: it is read as --library
// reads it, labelled official docs, and printed, its read line saying how
// it was found, the note what happened, the articles passed over, and the
// decisions counting the extra page pass.
func TestDecidedFollowsLinkedDocs(t *testing.T) {
	f, hits := articleSite(pgLinkDocs, pgLinkDocs)
	d := articleJudgments()
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	if o.Err() != nil || o.Decisions == nil {
		t.Fatalf("outcome %v, decisions %+v", o.Err(), o.Decisions)
	}
	out := renderAll(widgetQuestion, o)
	for _, want := range []string{
		"read:\n  - " + pgLinkDocs + " (official docs, confidence high, linked from 2 of the pages read)\n",
		"  - " + pgArt1 + " (article, search rank 1): ",
		"  - " + pgArt5 + " (article, search rank 5): ",
		"note: no search result read is documentation from the makers; read " + linkedRootURL + " instead, which 2 of the 5 pages read link to\n",
		pgLinkDocs + " (official docs)\n",
		"DOCSFACT",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"ARTFACT", todayNote, "origin:", "next:"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output holds %q:\n%s", unwanted, out)
		}
	}
	if len(o.PassedOver) != 5 {
		t.Errorf("passed_over %+v, want the five articles", o.PassedOver)
	}
	// Five page passes for the articles, one for the docs page read; its
	// passage pass beside the articles' three.
	if d.count("page") != 6 || d.count("passage") != 4 || o.Decisions.Requests != len(d.reqs) || o.Decisions.InputTokens != 1000*len(d.reqs) {
		t.Errorf("page requests %d (want 6), passage requests %d (want 4), decisions %+v for %d requests", d.count("page"), d.count("passage"), o.Decisions, len(d.reqs))
	}
}

// (b) Only one article links to the site: no follow, the answer as today.
func TestDecidedFollowNeedsTwoSites(t *testing.T) {
	f, hits := articleSite(pgLinkDocs)
	d := articleJudgments()
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	if o.Note != todayNote || len(o.Read) == 0 || o.Read[0].URL == pgLinkDocs {
		t.Errorf("note %q, read %+v: want today's answer", o.Note, o.Read)
	}
	if fetchedHost(f, "docs.widgetco.test") || d.count("page") != 5 {
		t.Errorf("the linked site was read (%v) or labelled (%d page requests)", f.called(), d.count("page"))
	}
}

// (c) A host two articles share that is never a library's docs (a forge,
// a social site, a CDN, an encyclopaedia) is never read.
func TestDecidedFollowSkipsNeverDocsHosts(t *testing.T) {
	for _, link := range []string{
		"https://github.com/widgetco/widget", "https://www.youtube.com/watch?v=abc", "https://cdn.widgetco.test/guide/",
		"https://en.wikipedia.org/wiki/Widget", "https://bit.ly/widget", "https://docs.widgetco.test/logo.png",
	} {
		f, hits := articleSite(link, link)
		d := articleJudgments()
		o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
		if o.Note != todayNote || d.count("page") != 5 {
			t.Errorf("%s: note %q, %d page requests: want today's answer", link, o.Note, d.count("page"))
		}
		for _, c := range f.called() {
			if !strings.Contains(c, ".test/") || strings.Contains(c, "widgetco") {
				t.Errorf("%s: fetched %s", link, c)
			}
		}
	}
}

// followedToday is o with the note's clause about a site tried and the
// decisions left out: what today's answer must equal.
func followedToday(o Outcome) string {
	o.Note, o.Decisions = todayNote, nil
	return renderAll(widgetQuestion, o)
}

// (d) The site read is labelled no documentation: today's answer, its
// note saying in one clause what was tried; the same answer as when the
// site cannot be read at all.
func TestDecidedFollowRejectsNotDocs(t *testing.T) {
	f, hits := articleSite(pgLinkDocs, pgLinkDocs)
	d := articleJudgments()
	d.pages[pgLinkDocs] = PageJudgments{Documentation: 0.2, Makers: 0.9, Kind: "marketing_or_landing_page"}
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	want := todayNote + "; tried " + linkedRootURL + ", which 2 of the 5 pages read link to: labelled official marketing page, not the makers' docs"
	if o.Note != want {
		t.Errorf("note %q, want %q", o.Note, want)
	}
	if len(o.Read) == 0 || o.Read[0].URL == pgLinkDocs || o.Decisions == nil || o.Decisions.Requests != len(d.reqs) {
		t.Errorf("read %+v, decisions %+v: want today's answer counting %d requests", o.Read, o.Decisions, len(d.reqs))
	}

	f2, hits2 := articleSite(pgLinkDocs, pgLinkDocs)
	delete(f2.pages, pgLinkDocs)
	o2 := decidedOver(t, f2, hits2, articleJudgments()).SearchSelect(context.Background(), widgetQuestion, 0)
	if !strings.HasPrefix(o2.Note, todayNote+"; tried "+linkedRootURL+", which 2 of the 5 pages read link to: ") {
		t.Errorf("unreadable site: note %q", o2.Note)
	}
	if a, b := followedToday(o), followedToday(o2); a != b {
		t.Errorf("a rejected site changed the answer:\n%s\n---\n%s", a, b)
	}
}

// (e) A provider error while labelling the site read: today's answer, its
// note saying decisions were unavailable for it.
func TestDecidedFollowProviderError(t *testing.T) {
	f, hits := articleSite(pgLinkDocs, pgLinkDocs)
	d := articleJudgments()
	d.err, d.failPage = errors.New("fake: HTTP 529: overloaded"), pgLinkDocs
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	want := todayNote + "; tried " + linkedRootURL + ", which 2 of the 5 pages read link to: decisions unavailable: fake: HTTP 529: overloaded"
	if o.Note != want || o.Err() != nil {
		t.Errorf("note %q, err %v; want %q", o.Note, o.Err(), want)
	}
	if len(o.Read) == 0 || o.Read[0].URL == pgLinkDocs {
		t.Errorf("read %+v: want today's answer", o.Read)
	}
	d2 := articleJudgments()
	d2.pages[pgLinkDocs] = PageJudgments{Documentation: 0.1, Makers: 0.1, Kind: "other"}
	f2, hits2 := articleSite(pgLinkDocs, pgLinkDocs)
	o2 := decidedOver(t, f2, hits2, d2).SearchSelect(context.Background(), widgetQuestion, 0)
	if a, b := followedToday(o), followedToday(o2); a != b {
		t.Errorf("a provider error changed the answer:\n%s\n---\n%s", a, b)
	}
}

// (f) With no decision provider, no link is followed: nothing of the
// linked site is fetched.
func TestDecidedFollowNeedsProvider(t *testing.T) {
	f, hits := articleSite(pgLinkDocs, pgLinkDocs)
	o := decidedOver(t, f, hits, nil).SearchSelect(context.Background(), widgetQuestion, 0)
	if o.Decisions != nil || fetchedHost(f, "docs.widgetco.test") {
		t.Errorf("decisions %+v, fetched %v", o.Decisions, f.called())
	}
	head, _ := articleSite(pgLinkDocs, pgLinkDocs)
	s, _, _ := searchFirstOver(t, head, hits)
	s.SearchSelect(context.Background(), widgetQuestion, 0)
	if a, b := f.called(), head.called(); strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Errorf("no provider fetched %v, without decisions %v", a, b)
	}
}

// The candidate rule: the most source sites, then a docs-looking host,
// then the most links; a source's own host never; the root the directory
// every link shares, else the host's.
func TestLinkedCandidate(t *testing.T) {
	page := func(u, md string) *dPage {
		return &dPage{hit: sfHit{u: parsed(u)}, page: &webPage{URL: u, Markdown: md}}
	}
	link := func(u string) string { return "[x](" + u + ") " }
	pages := []*dPage{
		page("https://a.test/p", link("https://widget.test/")+link("https://docs.widget.test/api/x")+link("https://b.test/q")),
		page("https://b.test/q", link("https://widget.test/pricing-x")+link("https://widget.test/y")+link("https://docs.widget.test/guide/y")),
		page("https://c.test/r", link("https://a.test/p")),
		{hit: sfHit{u: parsed("https://d.test/s")}, cause: readJS},
	}
	got, ok := linkedCandidate(pages)
	if !ok || got.host != "docs.widget.test" || got.sources != 2 || got.root.String() != "https://docs.widget.test/guide/" {
		t.Errorf("candidate %+v %v: want docs.widget.test (2 sites, docs-looking over widget.test's 3 links), under /guide/ (/api/ is no docs directory)", got, ok)
	}
	pages[1] = page("https://b.test/q", link("https://docs.widget.test/guide/y")+link("https://docs.widget.test/guide/z/"))
	pages[0] = page("https://a.test/p", link("https://docs.widget.test/guide/x"))
	if got, _ := linkedCandidate(pages); got.root.String() != "https://docs.widget.test/guide/" {
		t.Errorf("root %s, want the directory every link shares", got.root)
	}
	for _, c := range []struct {
		links []string
		want  string
	}{
		{[]string{"https://fly.test", "https://fly.test/docs/hands-on/install/"}, "https://fly.test/docs/"},
		{[]string{"https://fly.test/docs/a", "https://fly.test/blog/b"}, "https://fly.test/docs/"},
		{[]string{"https://fly.test/a", "https://fly.test/b"}, "https://fly.test/"},
		{[]string{"https://fly.test/docs/guides/a", "https://fly.test/docs/guides/b/c"}, "https://fly.test/docs/guides/"},
	} {
		var us []*url.URL
		for _, l := range c.links {
			us = append(us, parsed(l))
		}
		if r, err := linkedRoot(us); err != nil || r.String() != c.want {
			t.Errorf("links %v: root %s (%v), want %s", c.links, r, err, c.want)
		}
	}
	if _, ok := linkedCandidate(pages[2:]); ok {
		t.Errorf("a host one site links to is a candidate")
	}
}

// (g) Three sources link to three owners' pages on a host many owners
// publish on: no candidate from that host; nor from its API, file and
// paper addresses. (h) Two sources link to one owner's repository: that
// repository is the candidate, rooted at it.
func TestLinkedCandidateTenantHosts(t *testing.T) {
	page := func(u string, links ...string) *dPage {
		md := ""
		for _, l := range links {
			md += "[x](" + l + ") "
		}
		return &dPage{hit: sfHit{u: parsed(u)}, page: &webPage{URL: u, Markdown: md}}
	}
	if got, ok := linkedCandidate([]*dPage{
		page("https://a.test/p", "https://huggingface.co/doofz/THX-01", "https://huggingface.co/api/models/x/y"),
		page("https://b.test/q", "https://huggingface.co/Cloudflare/clef", "https://huggingface.co/api/models/x/y",
			"https://huggingface.co/x/y/resolve/main/config.json", "https://huggingface.co/papers/2610.1"),
		page("https://c.test/r", "https://huggingface.co/convaiinnovations/laya", "https://huggingface.co/x/y/resolve/main/config.json",
			"https://huggingface.co/papers/2610.1"),
	}); ok {
		t.Errorf("candidate %+v: three owners on one host, an API, file and paper address are none", got)
	}
	got, ok := linkedCandidate([]*dPage{
		page("https://a.test/p", "https://huggingface.co/spaces/org/demo", "https://huggingface.co/org/model"),
		page("https://b.test/q", "https://huggingface.co/org/model/tree/main", "https://huggingface.co/other/thing"),
	})
	if !ok || got.host != "huggingface.co/org/model" || got.sources != 2 || got.root.String() != "https://huggingface.co/org/model/" {
		t.Errorf("candidate %+v %v: want the repository two sources link to, rooted at it", got, ok)
	}
	got, ok = linkedCandidate([]*dPage{
		page("https://a.test/p", "https://huggingface.co/docs/transformers/a"),
		page("https://b.test/q", "https://huggingface.co/docs/transformers/b"),
	})
	if !ok || got.root.String() != "https://huggingface.co/docs/transformers/" {
		t.Errorf("candidate %+v %v: the host's own docs, two sources under /docs/", got, ok)
	}
}

// (i) The site read is official docs but nothing on it is on the
// question: today's answer, its note saying so, nothing of the site
// printed.
func TestDecidedFollowRejectsOffQuestion(t *testing.T) {
	f, hits := articleSite(pgLinkDocs, pgLinkDocs)
	d := articleJudgments()
	helps := d.helps
	d.helps = func(text string) float64 {
		if strings.Contains(text, "DOCSFACT") {
			return 0.05
		}
		return helps(text)
	}
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	want := todayNote + "; tried " + linkedRootURL + ", which 2 of the 5 pages read link to: documentation, but not on the question"
	if o.Note != want {
		t.Errorf("note %q, want %q", o.Note, want)
	}
	if out := renderAll(widgetQuestion, o); strings.Contains(out, "DOCSFACT") || len(o.Read) == 0 || o.Read[0].URL == pgLinkDocs {
		t.Errorf("the site off the question was printed:\n%s", out)
	}
}

// (j) The site read gives two official docs pages, one on the question,
// one not, and a page labelled no documentation: only the first is
// printed; the second, read and not printed, is listed among the other
// results as the followed site's, with no rank; the third is not listed.
func TestDecidedFollowPrintsOnlyOnQuestion(t *testing.T) {
	const pgOff, pgMkt = "https://docs.widgetco.test/guide/colours", "https://docs.widgetco.test/guide/pricing"
	f, hits := articleSite(pgLinkDocs, pgLinkDocs)
	f.pages[pgOff] = docsPage("Colours", "## Read timeout colours\n\nThe read_timeout widget badge is blue. OFFFACT.")
	f.pages[pgMkt] = docsPage("Pricing", "## Read timeout plans\n\nEvery plan sets the read_timeout widget default. MKTFACT.")
	hits = append(hits, search.Result{URL: pgOff, Title: "Colours", Description: "The read_timeout badge."},
		search.Result{URL: pgMkt, Title: "Pricing", Description: "The read_timeout plans."})
	d := articleJudgments()
	d.pages[pgOff] = PageJudgments{Documentation: 0.95, Makers: 0.95, Kind: "reference_documentation"}
	d.pages[pgMkt] = PageJudgments{Documentation: 0.1, Makers: 0.95, Kind: "marketing_or_landing_page"}
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	out := renderAll(widgetQuestion, o)
	if !strings.Contains(out, "DOCSFACT") || strings.Contains(out, "OFFFACT") || len(o.Read) != 1 || o.Read[0].URL != pgLinkDocs {
		t.Errorf("read %+v; want only the page on the question printed:\n%s", o.Read, out)
	}
	if d.count("page") != 8 {
		t.Errorf("page requests %d: want the site's three pages labelled", d.count("page"))
	}
	if len(o.OtherResults) == 0 || o.OtherResults[0] != (OtherResult{Title: "Colours", URL: pgOff, Label: labelOfficialDocs, Snippet: followedUnprinted}) {
		t.Errorf("other results %+v: want the unprinted docs page first, unranked", o.OtherResults)
	}
	for _, want := range []string{"## Other results\n- Colours — " + pgOff + " (official docs) — from the followed site, not printed\n",
		`another result with ketch docs --library <url> "<your question>"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, r := range o.OtherResults[1:] {
		if r.Snippet == followedUnprinted || r.URL == pgOff || r.URL == pgLinkDocs {
			t.Errorf("listed again, or a page labelled no documentation listed as the site's: %+v", r)
		}
	}
}

// A followed official docs page judged only somewhat on the question
// (context medium) is rejected: a site reached by links must reach
// context high.
func TestDecidedFollowRejectsMediumContext(t *testing.T) {
	f, hits := articleSite(pgLinkDocs, pgLinkDocs)
	d := articleJudgments()
	helps := d.helps
	d.helps = func(text string) float64 {
		if strings.Contains(text, "DOCSFACT") {
			return 0.5
		}
		return helps(text)
	}
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	want := todayNote + "; tried " + linkedRootURL + ", which 2 of the 5 pages read link to: documentation, but not on the question"
	if o.Note != want || len(o.Read) == 0 || o.Read[0].URL == pgLinkDocs {
		t.Errorf("note %q, read %+v; want today's answer and %q", o.Note, o.Read, want)
	}
}
