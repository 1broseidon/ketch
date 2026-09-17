package docs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/docstore"
	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// seedStore writes two libraries into a fresh docs dir and returns it.
func seedStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	s, err := docstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Replace(docstore.Library{Name: "tailwind", Version: "4", Seed: "https://tw.test/docs", Source: docstore.SourceSitemap}, []docstore.Page{
		{URL: "https://tw.test/docs/container-queries", Title: "Container queries", Markdown: "# Container queries\n\nUse @container variants.\n\n## Named containers\n\nTarget an ancestor by name.\n"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Replace(docstore.Library{Name: "glamour", Seed: "https://gl.test", Source: docstore.SourceCrawl}, []docstore.Page{
		{URL: "https://gl.test/readme", Title: "Glamour", Markdown: "# Glamour\n\nRender markdown in the terminal with a container of styles.\n"},
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLocalSearchMapsResults(t *testing.T) {
	l := NewLocal(seedStore(t)).WithScope(nil)
	defer l.Close()
	results, err := l.Search(context.Background(), "named containers", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	r := results[0]
	if r.Source != "local" || r.Library != "tailwind" || r.Version != "4" || r.Title != "Named containers" {
		t.Errorf("result = %+v", r)
	}
	if r.URL != "https://tw.test/docs/container-queries#named-containers" || !strings.Contains(r.Breadcrumb, "Named containers") || !strings.Contains(r.Snippet, "ancestor") {
		t.Errorf("result = %+v", r)
	}
}

func TestLocalSearchHonoursScope(t *testing.T) {
	dir := seedStore(t)
	all, _ := NewLocal(dir).WithScope(nil).Search(context.Background(), "container", 10)
	if len(all) < 2 {
		t.Fatalf("unscoped = %+v", all)
	}
	scoped, _ := NewLocal(dir).WithScope([]string{"glamour"}).Search(context.Background(), "container", 10)
	if len(scoped) != 1 || scoped[0].Library != "glamour" {
		t.Fatalf("scoped = %+v", scoped)
	}
}

func TestLocalScopeErrorSurfaces(t *testing.T) {
	l := NewLocal(seedStore(t))
	l.scopeFn = func() ([]string, error) { return nil, errors.New("corrupt manifest") }
	if _, err := l.Search(context.Background(), "container", 5); err == nil || !strings.Contains(err.Error(), "corrupt manifest") {
		t.Fatalf("err = %v", err)
	}
}

func TestLocalResolveLibrary(t *testing.T) {
	l := NewLocal(seedStore(t))
	all, err := l.ResolveLibrary(context.Background(), "", 0)
	if err != nil || len(all) != 2 {
		t.Fatalf("all = %+v %v", all, err)
	}
	tw, _ := l.ResolveLibrary(context.Background(), "TAIL", 0)
	if len(tw) != 1 || tw[0].ID != "tailwind" || tw[0].Versions[0] != "4" || tw[0].TotalSnippets != 2 || !strings.Contains(tw[0].Description, "sitemap") {
		t.Fatalf("tw = %+v", tw)
	}
	limited, _ := l.ResolveLibrary(context.Background(), "", 1)
	if len(limited) != 1 {
		t.Fatalf("limited = %+v", limited)
	}
	none, _ := l.ResolveLibrary(context.Background(), "react", 0)
	if len(none) != 0 {
		t.Fatalf("none = %+v", none)
	}
}

func TestLocalGetDocs(t *testing.T) {
	l := NewLocal(seedStore(t))
	results, err := l.GetDocs(context.Background(), "tailwind", "container", 4000)
	if err != nil || len(results) != 2 {
		t.Fatalf("results = %+v %v", results, err)
	}
	for _, r := range results {
		if r.Library != "tailwind" {
			t.Errorf("leaked other library: %+v", r)
		}
	}
	// A tiny budget still yields the first hit.
	one, err := l.GetDocs(context.Background(), "tailwind", "container", 1)
	if err != nil || len(one) != 1 {
		t.Fatalf("budgeted = %+v %v", one, err)
	}
	_, err = l.GetDocs(context.Background(), "react", "hooks", 4000)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown library err = %v, want ErrNotFound", err)
	}
}

func TestLocalWithoutStoreIsSetupError(t *testing.T) {
	l := NewLocal(t.TempDir())
	_, err := l.Search(context.Background(), "x", 5)
	if err == nil || !strings.Contains(err.Error(), "ketch docs add") {
		t.Fatalf("err = %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLocalProviderDescriptor(t *testing.T) {
	p, ok := Lookup("local")
	if !ok || p.Hidden {
		t.Fatalf("local provider = %+v", p)
	}
	var cfg config.Config
	cfg.SetProvider("docs_dir", t.TempDir())
	if p.Usable(&cfg) {
		t.Error("usable before any library is added")
	}
	if status, detail := p.Probe(context.Background(), nil, &cfg); status != health.StatusSkipped || !strings.Contains(detail, "docs add") {
		t.Errorf("probe on empty dir = %s %q", status, detail)
	}

	cfg.SetProvider("docs_dir", seedStore(t))
	if !p.Usable(&cfg) {
		t.Error("not usable with a seeded store")
	}
	s, err := p.Build(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(LibraryResolver); !ok {
		t.Error("local should implement LibraryResolver")
	}
	if status, detail := p.Probe(context.Background(), nil, &cfg); status != health.StatusOK || !strings.Contains(detail, "2 libraries") {
		t.Errorf("probe on seeded dir = %s %q", status, detail)
	}
	if len(p.Settings) != 1 || p.Settings[0].Key != "docs_dir" || p.Settings[0].Secret {
		t.Errorf("settings = %+v", p.Settings)
	}
}
