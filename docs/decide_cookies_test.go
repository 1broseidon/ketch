package docs

import (
	"context"
	"strings"
	"testing"
)

// sentAbout reports a request d was sent whose input or questions carry
// text: the page's text, or its URL in a page or passage pass.
func sentAbout(d *fakeDecider, url, text string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range d.reqs {
		switch in := r.Input.(type) {
		case pageInput:
			if in.Page.URL == url || strings.Contains(in.Page.Text, text) {
				return true
			}
		case passageInput:
			if in.Page.URL == url {
				return true
			}
			for _, q := range r.Questions {
				if strings.Contains(q.Item.Value.(passageItem).Text, text) {
					return true
				}
			}
		}
	}
	return false
}

// A page read with the user's cookies is never sent to the provider: no
// page pass, no passage pass; it is unlabelled and its line says why. A
// page read without them is sent as today.
func TestDecidedCookiePageNotSent(t *testing.T) {
	f, hits := decidedSite()
	p := f.pages[pgOfficial]
	p.cookies = true
	f.pages[pgOfficial] = p
	d := decidedJudgments()
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	if o.Decisions == nil {
		t.Fatalf("no decisions: %q", o.Note)
	}
	if sentAbout(d, pgOfficial, "OFFICIALFACT") {
		t.Errorf("the page read with cookies was sent to the provider")
	}
	if !sentAbout(d, pgMirror, "MIRRORFACT") || !sentAbout(d, pgBlog, "BLOGFACT") {
		t.Errorf("pages read without cookies were not sent")
	}
	out := renderAll(widgetQuestion, o)
	if want := "  - " + pgOfficial + " (unlabelled, search rank 2): " + readCookies + "\n"; !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
}

// Every page read with cookies: no request is made and the answer is the
// plain one, its note saying why.
func TestDecidedAllCookiePages(t *testing.T) {
	f, hits := decidedSite()
	for u, p := range f.pages {
		p.cookies = true
		f.pages[u] = p
	}
	d := decidedJudgments()
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	if o.Decisions != nil || d.count("page") != 0 || d.count("passage") != 0 {
		t.Fatalf("decisions %+v, %d page and %d passage requests", o.Decisions, d.count("page"), d.count("passage"))
	}
	if !strings.Contains(o.Note, errAllCookies.Error()) {
		t.Errorf("note %q", o.Note)
	}
}

// A page read with cookies may still be the page read, as an unlabelled
// page may (here the first readable result, no page having a passage on
// the question), its read line saying it was not sent.
func TestDecidedCookiePagePrinted(t *testing.T) {
	f, hits := decidedSite()
	for _, u := range []string{pgBlog, pgOfficial, pgMirror, pgGuide} {
		if u != pgBlog {
			delete(f.pages, u)
		}
	}
	p := f.pages[pgBlog]
	p.cookies = true
	f.pages[pgBlog] = p
	f.pages[pgMirror] = docsPage("Timeouts (mirror)", "## Colours\n\nThemes only.")
	d := decidedJudgments()
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	out := renderAll(widgetQuestion, o)
	if sentAbout(d, pgBlog, "BLOGFACT") {
		t.Errorf("the page read with cookies was sent")
	}
	if want := "  - " + pgBlog + " (unlabelled, confidence low, search rank 1, " + readCookies + ")\n"; !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
}

// A linked site whose pages were read with cookies is not sent either: the
// follow is tried and not used, and the note says why.
func TestDecidedFollowCookiePagesNotSent(t *testing.T) {
	f, hits := articleSite(pgLinkDocs, pgLinkDocs)
	p := f.pages[pgLinkDocs]
	p.cookies = true
	f.pages[pgLinkDocs] = p
	d := articleJudgments()
	o := decidedOver(t, f, hits, d).SearchSelect(context.Background(), widgetQuestion, 0)
	want := todayNote + "; tried " + linkedRootURL + ", which 2 of the 5 pages read link to: " + readCookies
	if o.Note != want {
		t.Errorf("note %q\nwant %q", o.Note, want)
	}
	if sentAbout(d, pgLinkDocs, "DOCSFACT") {
		t.Error("the linked page read with cookies was sent")
	}
}
