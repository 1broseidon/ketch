package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/docstore"
	"github.com/spf13/pflag"
)

// docsSite serves a two-page docs site with a sitemap.
func docsSite(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	page := func(title, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `<!doctype html><html><head><title>%s</title></head><body><main><h1>%s</h1>%s</main></body></html>`, title, title, body)
		}
	}
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>%s/docs/a</loc></url><url><loc>%s/docs/b</loc></url><url><loc>%s/blog</loc></url></urlset>`, srv.URL, srv.URL, srv.URL)
	})
	mux.HandleFunc("/docs/a", page("Alpha", "<p>The alpha page talks about widgets.</p><h2>Widget config</h2><p>Set widget_size.</p>"))
	mux.HandleFunc("/docs/b", page("Beta", "<p>The beta page talks about gadgets.</p>"))
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// inTestProject isolates the docs store and runs the test inside a fresh git
// repository so `docs add` has a project to attach to.
func inTestProject(t *testing.T) (storeDir, projectRoot string) {
	t.Helper()
	storeDir = t.TempDir()
	setDocsDir(t, storeDir)
	projectRoot = t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(projectRoot)
	return storeDir, projectRoot
}

func resetDocsAddFlags(t *testing.T) {
	t.Helper()
	docsAddCmd.SetContext(context.Background())
	docsSyncCmd.SetContext(context.Background())
	docsCmd.SetContext(context.Background())
	t.Cleanup(func() {
		docsAddCmd.Flags().VisitAll(func(f *pflag.Flag) { _ = f.Value.Set(f.DefValue); f.Changed = false })
	})
}

func TestDocsAddListRemoveRoundTrip(t *testing.T) {
	srv := docsSite(t)
	storeDir, projectRoot := inTestProject(t)
	resetDocsAddFlags(t)

	if err := runDocsAdd(docsAddCmd, []string{"widgets", srv.URL + "/docs"}); err != nil {
		t.Fatalf("docs add: %v", err)
	}

	// Stored, and attached to the project manifest.
	if !docstore.Exists(storeDir) {
		t.Fatal("store not created")
	}
	manifest, err := os.ReadFile(filepath.Join(projectRoot, docstore.ManifestPath))
	if err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
	if !strings.Contains(string(manifest), `"name": "widgets"`) || !strings.Contains(string(manifest), srv.URL+"/docs") {
		t.Fatalf("manifest = %s", manifest)
	}

	// Searchable through the docs command with -b local, scoped by project.
	setDocsBackend(t, "local")
	if err := runDocs(docsCmd, []string{"widget", "config"}); err != nil {
		t.Fatalf("docs search: %v", err)
	}
	if err := docsCmd.Flags().Set("library", "widgets"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { docsCmd.Flags().Set("library", "") }) //nolint:errcheck
	if err := runDocs(docsCmd, []string{"gadgets"}); err != nil {
		t.Fatalf("docs --library: %v", err)
	}

	if err := runDocsList(docsListCmd, nil); err != nil {
		t.Fatalf("docs list: %v", err)
	}
	if err := runDocsRemove(docsRemoveCmd, []string{"widgets"}); err != nil {
		t.Fatalf("docs remove: %v", err)
	}
	manifest, _ = os.ReadFile(filepath.Join(projectRoot, docstore.ManifestPath))
	if strings.Contains(string(manifest), "widgets") {
		t.Fatalf("remove did not detach: %s", manifest)
	}
	if got := asExitError(t, runDocsRemove(docsRemoveCmd, []string{"widgets"})); got.Code != ExitNotFound {
		t.Errorf("second remove exit = %d, want %d", got.Code, ExitNotFound)
	}
}

func TestDocsAddDryRunWritesNothing(t *testing.T) {
	srv := docsSite(t)
	storeDir, projectRoot := inTestProject(t)
	resetDocsAddFlags(t)
	if err := docsAddCmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	if err := runDocsAdd(docsAddCmd, []string{"widgets", srv.URL + "/docs"}); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, docstore.ManifestPath)); err == nil {
		t.Error("dry run wrote the manifest")
	}
	if s, err := docstore.Open(storeDir); err == nil {
		defer s.Close()
		if libs, _ := s.Libraries(); len(libs) != 0 {
			t.Errorf("dry run stored %+v", libs)
		}
	}
}

func TestDocsSyncAddsMissingLibraries(t *testing.T) {
	srv := docsSite(t)
	_, projectRoot := inTestProject(t)
	resetDocsAddFlags(t)
	p := &docstore.Project{Root: projectRoot, Path: filepath.Join(projectRoot, docstore.ManifestPath)}
	if err := p.Attach(docstore.Attachment{Name: "widgets", URL: srv.URL + "/docs", MaxPages: 1}); err != nil {
		t.Fatal(err)
	}
	if err := runDocsSync(docsSyncCmd, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	// Second sync is a no-op; --force re-adds.
	if err := runDocsSync(docsSyncCmd, nil); err != nil {
		t.Fatalf("second sync: %v", err)
	}
}

func TestDocsSyncOutsideProjectIsPrecondition(t *testing.T) {
	inTestProject(t)
	resetDocsAddFlags(t)
	if err := os.RemoveAll(".git"); err != nil {
		t.Fatal(err)
	}
	if got := asExitError(t, runDocsSync(docsSyncCmd, nil)); got.Code != ExitPrecondition {
		t.Errorf("exit = %d, want %d", got.Code, ExitPrecondition)
	}
}

func TestDocsAddExitCodes(t *testing.T) {
	inTestProject(t)
	resetDocsAddFlags(t)
	if got := asExitError(t, runDocsAdd(docsAddCmd, []string{"Bad Name", "https://x.test"})); got.Code != ExitValidation {
		t.Errorf("bad name exit = %d, want %d", got.Code, ExitValidation)
	}
	if got := asExitError(t, runDocsAdd(docsAddCmd, []string{"ok", "not-a-url"})); got.Code != ExitValidation {
		t.Errorf("bad url exit = %d, want %d", got.Code, ExitValidation)
	}
	// A site that serves only an empty page: fetched, nothing to index.
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head><title>x</title></head><body></body></html>`)
	}))
	defer empty.Close()
	if got := asExitError(t, runDocsAdd(docsAddCmd, []string{"ok", empty.URL + "/docs"})); got.Code != ExitNotFound {
		t.Errorf("empty site exit = %d, want %d: %v", got.Code, ExitNotFound, got)
	}
	// A site that is down entirely: upstream.
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	if got := asExitError(t, runDocsAdd(docsAddCmd, []string{"ok", down.URL + "/docs"})); got.Code != ExitUpstream {
		t.Errorf("down site exit = %d, want %d: %v", got.Code, ExitUpstream, got)
	}
}
