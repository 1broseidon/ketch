package docs

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// resolveSearchFirst is the search backend over three indexed libraries
// (one with no title, as a build from a bare docs URL leaves it) and a
// web that fails any fetch, with the count of its searches.
func resolveSearchFirst(t *testing.T) (*SearchFirst, *fakeFetcher, func() int32) {
	t.Helper()
	f := &fakeFetcher{}
	s, store, searches := searchFirstOver(t, f, nil)
	built := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	for _, lib := range []docsindex.Library{
		{Key: "https://duo.example.test/docs/", Library: "https://duo.example.test/docs/", Names: []string{"ciscoduo"}, Title: "Cisco Duo", Root: "https://duo.example.test/docs/", Pages: 734},
		{Key: "https://pam.example.test/doc/libpam-doc/html/", Root: "https://pam.example.test/doc/libpam-doc/html/", Names: []string{"libpamdoc"}, Pages: 79},
		{Key: "https://sssd.example.test/", Root: "https://sssd.example.test/", Title: "sssd.example.test", Pages: 30, Capped: true},
	} {
		lib.Format, lib.Built = docsindex.FormatVersion, built
		page := lib.Root + "intro"
		if err := store.PutPages(lib.Key, []docsindex.PageData{{Page: docsindex.Page{URL: page, Hash: "h"}, Sections: []docsindex.Section{{Page: page, Heading: "Intro", Crumbs: []string{"Intro"}, Text: "Intro."}}}}); err != nil {
			t.Fatal(err)
		}
		if err := store.PutLibrary(lib); err != nil {
			t.Fatal(err)
		}
	}
	return s, f, searches.Load
}

func indexedIDs(ms []LibraryMatch) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

// Resolve answers from the local index first, with no network: an empty
// query lists every indexed library, a name, title or root holding the
// query names its library, and failing that a word of it does; each
// match is marked indexed, with what its index holds. An explicit limit
// caps them; the configured one does not.
func TestResolveIndexedFirst(t *testing.T) {
	s, f, searches := resolveSearchFirst(t)
	duo, pam, sssd := "https://duo.example.test/docs/", "https://pam.example.test/doc/libpam-doc/html/", "https://sssd.example.test/"
	ctx := WithLocalOptions(context.Background(), LocalOptions{})
	cases := map[string][]string{
		"":                                 {duo, pam, sssd},
		"  ":                               {duo, pam, sssd},
		"cisco DUO":                        {duo},
		"SSSD":                             {sssd},
		"Linux-PAM":                        {pam},
		"pam_sss":                          {pam, sssd},
		"https://duo.example.test/docs/x/": {duo},
	}
	for q, want := range cases {
		o := s.ResolveSelect(ctx, q, 1)
		if o.Err() != nil || !slices.Equal(indexedIDs(o.Matches), want) {
			t.Errorf("resolve %q = %v (%v), want %v", q, indexedIDs(o.Matches), o.Err(), want)
		}
	}
	o := s.ResolveSelect(ctx, "", 1)
	m := o.Matches[2]
	if !m.Indexed || m.Index == nil || m.Index.Summary() != "capped, 30 pages, built 2026-11-10" || m.Title != "sssd.example.test" {
		t.Errorf("match = %+v", m)
	}
	if got := s.ResolveSelect(WithLocalOptions(context.Background(), LocalOptions{Limit: 2}), "", 0); len(got.Matches) != 2 {
		t.Errorf("limit 2: %v", indexedIDs(got.Matches))
	}
	if n := searches(); n != 0 || len(f.called()) != 0 {
		t.Errorf("resolve went to the network: %d searches, fetched %v", n, f.called())
	}
	// Nothing indexed matches: the resolution goes out as before.
	if o := s.ResolveSelect(ctx, "zzzunknown", 1); o.Err() == nil || len(o.Matches) != 0 {
		t.Errorf("unindexed name = %v (%v), want the web's resolution", indexedIDs(o.Matches), o.Err())
	}
}

// With nothing indexed, an empty resolve is a refusal that says so.
func TestResolveNothingIndexed(t *testing.T) {
	s, _, _ := searchFirstOver(t, &fakeFetcher{}, nil)
	if o := s.ResolveSelect(context.Background(), "", 5); o.Status != Refused || o.Kind().String() != "not_found" {
		t.Errorf("empty resolve over no index = %s %v", o.Status, o.Err())
	}
}
