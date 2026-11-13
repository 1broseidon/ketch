package docs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
	"github.com/1broseidon/ketch/search"
)

func TestPinnedReadAndCache(t *testing.T) {
	f := originSite()
	m := verifiedMatch(ownRoot, "registry names the docs", nil)
	m.version = "2.1.3" // the official docs need not have a version in their URL
	s := originSearch(t, f, []search.Result{hit(fanRoot+"lib/max-conns", "Lib docs")}, sfResolved{match: m})
	ctx := context.Background()
	for i := range 2 {
		o := s.GetDocsSelect(ctx, "lib@2", "pool max_connections", 0)
		if o.Status != Found || o.Library != ownRoot || len(o.Results) == 0 || o.Results[0].Version != "2.1.3" {
			t.Fatalf("call %d: %s, %s, %v, %v", i, o.Status, o.Library, o.Results, o.Err())
		}
		if i == 0 {
			s.resolveName = func(context.Context, sfRequest) sfResolved { t.Fatal("warm call resolved again"); return sfResolved{} }
		}
	}
	// A cached answer for an unconfirmed or different release is unusable.
	req := sfRequest{library: "lib@2", query: "pool max_connections", spec: libSpec{raw: "lib@2", name: "lib", version: "2"}}
	r, _, ok := s.loadRead(req)
	if !ok {
		t.Fatal("missing question cache")
	}
	for _, pin := range []string{"", "9.0"} {
		r.Pin = pin
		b, _ := json.Marshal(r)
		s.web.resolutionCache().PutResolution(sfReadKey(req), string(b))
		if _, ok := s.fromRead(ctx, req, &sfTrace{}); ok {
			t.Fatalf("accepted cached pin %q", pin)
		}
	}
}

func TestPinnedReadCannotFallBack(t *testing.T) {
	for _, mode := range []string{"missing", "timeout", "unknown_version", "unreadable", "missing_terms"} {
		t.Run(mode, func(t *testing.T) {
			f := originSite()
			m := verifiedMatch(ownRoot, "registry names the docs", nil)
			m.version = "2.1"
			r := sfResolved{match: m}
			switch mode {
			case "missing":
				r.match, r.err = nil, ErrNotFound
			case "timeout":
				r.match, r.err = nil, context.DeadlineExceeded
			case "unknown_version":
				m.version = ""
			case "unreadable":
				delete(f.pages, ownRoot)
				delete(f.pages, ownRoot+"guide/pool")
			case "missing_terms":
				f.pages[ownRoot+"guide/pool"] = fakePage{title: "Intro", markdown: "# Intro\n\nWelcome to the library."}
			}
			s := originSearch(t, f, []search.Result{hit(fanRoot+"lib/max-conns", "Lib docs")}, r)
			o := s.GetDocsSelect(context.Background(), "lib@2", "max_connections", 0)
			if answered(o) || strings.HasPrefix(o.Library, fanRoot) {
				t.Fatalf("pin escaped: %+v", o)
			}
			if mode == "timeout" && !errors.Is(o.Err(), context.DeadlineExceeded) {
				t.Fatalf("lost timeout: %v", o.Err())
			}
			for _, call := range f.called() {
				if strings.Contains(call, "libtips.test") {
					t.Fatalf("read unpinned site: %s", call)
				}
			}
		})
	}
}

func TestPinnedSiteVersionLabels(t *testing.T) {
	for _, labels := range [][2]string{{"2.0", "20"}, {"4", "4x"}, {"stable", "6.1"}} {
		t.Run(labels[0], func(t *testing.T) {
			m := verifiedMatch(ownRoot, "registry names the docs", nil)
			m.version = labels[1]
			s := originSearch(t, originSite(), nil, sfResolved{match: m})
			for range 2 {
				o := s.GetDocsSelect(context.Background(), "lib@"+labels[0], "max_connections", 0)
				if !answered(o) || len(o.Results) == 0 || o.Results[0].Version != labels[1] {
					t.Fatalf("verified site label lost: %+v", o)
				}
			}
		})
	}
}

func TestOfflineNeverFetches(t *testing.T) {
	f := exampleSite()
	s, _, searches := searchFirstOver(t, f, exampleHits())
	ctx := WithLocalOptions(context.Background(), LocalOptions{Offline: true})
	for _, library := range []string{"", "example", "example@2", idxRoot} {
		var o Outcome
		if library == "" {
			o = s.SearchSelect(ctx, "middleware", 0)
		} else {
			o = s.GetDocsSelect(ctx, library, "middleware", 0)
		}
		if o.Status != Refused || !errors.Is(o.Err(), ErrNotIndexed) {
			t.Errorf("%q: %+v", library, o)
		}
	}
	for _, name := range []string{"example", "example@2", idxRoot, ""} {
		if o := s.ResolveSelect(ctx, name, 0); o.Status != Refused {
			t.Errorf("resolve %q: %+v", name, o)
		}
	}
	if o := s.SectionSelect(ctx, "", idxRoot+"guide/middleware#order", 0, 0); o.Err() == nil {
		t.Errorf("unindexed section answered: %+v", o)
	}
	if searches.Load() != 0 || len(f.called()) != 0 {
		t.Fatalf("offline used network: %d, %v", searches.Load(), f.called())
	}
}

func TestIndexedResolutionIgnoresURLSchemes(t *testing.T) {
	libs := []docsindex.Library{
		{Root: "https://docs.ollama.com/", Library: "https://docs.ollama.com/", Title: "Ollama"},
		{Root: "http://docs.example.test/", Title: "Example"},
		{Root: "https://www.python-httpx.org/", Title: "HTTPX"},
	}
	for _, query := range []string{"http", "https"} {
		if got := indexedNamed(libs, query); len(got) != 0 {
			t.Fatalf("%s matched URLs: %+v", query, got)
		}
		if got := nearIndexed(libs, query); len(got) != 0 {
			t.Fatalf("%s suggested URLs: %+v", query, got)
		}
	}
	libs = append(libs, docsindex.Library{Root: "https://pkg.go.dev/net/http/", Title: "http", Version: "1.26"})
	if got := indexedNamed(libs, "http"); len(got) != 1 || got[0].Title != "http" {
		t.Fatalf("real http library lost: %+v", got)
	}
	if got := indexedNamed(libs, "http@1.25"); len(got) != 0 {
		t.Fatalf("wrong indexed release: %+v", got)
	}
	if got := indexedNamed(libs, "http@1.26"); len(got) != 1 {
		t.Fatalf("pinned indexed release lost: %+v", got)
	}
}
