package docs

import (
	"context"
	"slices"
	"testing"
)

// A version's llms manifest can list source pages outside its root while
// llms-full has unattributed text. Neither supplies usable page URLs.
func TestDiscoveryFallsBackPastUnusableLLMS(t *testing.T) {
	const rootURL = "https://cache.example.test/en/stable/"
	root := rootOf(rootURL)
	for _, mode := range []string{"live", "index"} {
		t.Run(mode, func(t *testing.T) {
			f := &fakeFetcher{raw: map[string]fakeRaw{
				rootURL + "llms.txt":      plain("# Cache\n\n- [Guide](https://cache.example.test/_sources/guide.md)\n"),
				rootURL + "llms-full.txt": plain("# Cache\n\nConfigure cached sessions and expiration.\n"),
				rootURL:                   html(`<html><head><title>Cache</title></head><body><a href="guide.html">Guide</a><a href="/en/old/guide.html">Old guide</a></body></html>`),
			}}
			w := NewWeb(f)
			var site webSite
			var err error
			if mode == "live" {
				site, err = w.discover(context.Background(), root)
			} else {
				var indexed indexSite
				indexed, err = w.enumerate(context.Background(), root)
				site = indexed.site
			}
			if err != nil {
				t.Fatal(err)
			}
			got := urlsOf(candidateLinks(site, root))
			if !slices.Contains(got, rootURL+"guide.html") {
				t.Fatalf("usable guide missing: %v", got)
			}
			for _, u := range got {
				if !root.contains(rootOf(u).u) {
					t.Fatalf("version scope widened: %s", u)
				}
			}
		})
	}
}
