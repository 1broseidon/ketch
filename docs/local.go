package docs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/docstore"
	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// Local searches documentation libraries stored on this machine by
// `ketch docs add`. Retrieval is SQLite FTS5 lexical ranking over
// heading-delimited sections — no network, no key, deterministic. When
// constructed inside a project (a directory tree with .ketch/docs.json),
// searches default to that project's attached libraries.
type Local struct {
	dir string

	mu    sync.Mutex
	store *docstore.Store

	scopeOnce sync.Once
	scope     []string // library names a bare Search is limited to; nil = all
	scopeErr  error
	scopeFn   func() ([]string, error)
}

// NewLocal creates a local backend over the docs directory. Bare searches
// are scoped to the project enclosing the working directory (see
// docstore.ScopeFor); the store and scope are both resolved lazily on first
// use so construction never touches disk.
func NewLocal(dir string) *Local {
	return &Local{dir: dir, scopeFn: func() ([]string, error) {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, nil // no working directory means no project; search everything
		}
		return docstore.ScopeFor(cwd)
	}}
}

// WithScope fixes the libraries a bare Search is limited to, replacing the
// working-directory project lookup. nil searches every stored library.
func (l *Local) WithScope(scope []string) *Local {
	l.scopeFn = func() ([]string, error) { return scope, nil }
	return l
}

func (l *Local) resolveScope() ([]string, error) {
	l.scopeOnce.Do(func() { l.scope, l.scopeErr = l.scopeFn() })
	return l.scope, l.scopeErr
}

// Close releases the underlying database, if it was opened.
func (l *Local) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.store == nil {
		return nil
	}
	err := l.store.Close()
	l.store = nil
	return err
}

func (l *Local) open() (*docstore.Store, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.store != nil {
		return l.store, nil
	}
	if !docstore.Exists(l.dir) {
		return nil, errors.New(localSetup)
	}
	s, err := docstore.Open(l.dir)
	if err != nil {
		return nil, err
	}
	l.store = s
	return s, nil
}

// Search returns the best-matching sections across the scoped libraries.
func (l *Local) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	s, err := l.open()
	if err != nil {
		return nil, err
	}
	scope, err := l.resolveScope()
	if err != nil {
		return nil, err
	}
	hits, err := s.Search(ctx, docstore.Query{Text: query, Libraries: scope, Limit: limit})
	if err != nil {
		return nil, err
	}
	return toResults(hits), nil
}

// ResolveLibrary lists stored libraries whose name contains name
// (case-insensitive; "" lists all), at most limit when positive. It is the
// offline counterpart of a hosted provider's library lookup.
func (l *Local) ResolveLibrary(_ context.Context, name string, limit int) ([]LibraryMatch, error) {
	s, err := l.open()
	if err != nil {
		return nil, err
	}
	libs, err := s.Libraries()
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(name))
	var matches []LibraryMatch
	for _, lib := range libs {
		if needle != "" && !strings.Contains(strings.ToLower(lib.Name), needle) {
			continue
		}
		m := LibraryMatch{
			ID:            lib.Name,
			Title:         lib.Name,
			Description:   fmt.Sprintf("%s (%s, %d pages)", lib.Seed, lib.Source, lib.Pages),
			TotalSnippets: lib.Sections,
		}
		if lib.Version != "" {
			m.Versions = []string{lib.Version}
		}
		matches = append(matches, m)
	}
	// Exact name first, then the rest alphabetically (Libraries is sorted).
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].ID == needle && matches[j].ID != needle })
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

// GetDocs searches within one stored library. tokens approximates a
// character budget (four characters per token) over the returned snippets,
// matching how the hosted provider's budget bounds its response; results
// stop before the section that would exceed it, always returning at least
// one. An unknown library is ErrNotFound.
func (l *Local) GetDocs(ctx context.Context, library, query string, tokens int) ([]Result, error) {
	s, err := l.open()
	if err != nil {
		return nil, err
	}
	if _, err := s.Library(library); err != nil {
		if errors.Is(err, docstore.ErrNotFound) {
			return nil, fmt.Errorf("local: library %q %w (list stored libraries with: ketch docs list)", library, ErrNotFound)
		}
		return nil, err
	}
	hits, err := s.Search(ctx, docstore.Query{Text: query, Libraries: []string{library}, Limit: 50})
	if err != nil {
		return nil, err
	}
	results := toResults(hits)
	if tokens <= 0 {
		return results, nil
	}
	budget := tokens * 4
	used := 0
	for i, r := range results {
		used += len(r.Snippet)
		if used > budget && i > 0 {
			return results[:i], nil
		}
	}
	return results, nil
}

func toResults(hits []docstore.Hit) []Result {
	results := make([]Result, 0, len(hits))
	for _, h := range hits {
		results = append(results, Result{
			Library:    h.Library,
			Version:    h.Version,
			Title:      h.Title,
			Breadcrumb: h.Breadcrumb,
			Snippet:    h.Body,
			URL:        h.URL,
			Source:     "local",
		})
	}
	return results
}

const localSetup = "local: no docs libraries yet (add one with: ketch docs add <name> <url>)"

// LocalDir resolves the docs directory from config (docs_dir, else the
// platform default).
func LocalDir(c *config.Config) (string, error) { return docstore.Dir(c.String("docs_dir")) }

// ProbeLocal reports the state of the local store: skipped with a hint when
// no library has been added yet, ok with a library count otherwise, and
// misconfigured when the database cannot be opened.
func ProbeLocal(dir string) (health.Status, string) {
	if !docstore.Exists(dir) {
		return health.StatusSkipped, "no libraries yet (ketch docs add <name> <url>)"
	}
	s, err := docstore.Open(dir)
	if err != nil {
		return health.StatusMisconfigured, health.ErrorDetail(err)
	}
	defer s.Close()
	libs, err := s.Libraries()
	if err != nil {
		return health.StatusMisconfigured, health.ErrorDetail(err)
	}
	if len(libs) == 0 {
		return health.StatusSkipped, "no libraries yet (ketch docs add <name> <url>)"
	}
	names := make([]string, len(libs))
	for i, lib := range libs {
		names[i] = lib.Name
	}
	return health.StatusOK, fmt.Sprintf("%d libraries (%s)", len(libs), strings.Join(names, ", "))
}

func localProvider() Provider {
	return Provider{
		ID:           "local",
		Name:         "Local",
		Setup:        localSetup,
		LibrarySetup: localSetup,
		Settings:     []config.Setting{config.Scalar("docs_dir")},
		Usable: func(c *config.Config) bool {
			dir, err := LocalDir(c)
			return err == nil && docstore.Exists(dir)
		},
		New: func(c *config.Config) (Searcher, error) {
			dir, err := LocalDir(c)
			if err != nil {
				return nil, err
			}
			return NewLocal(dir), nil
		},
		Probe: func(_ context.Context, _ *http.Client, c *config.Config) (health.Status, string) {
			dir, err := LocalDir(c)
			if err != nil {
				return health.StatusMisconfigured, health.ErrorDetail(err)
			}
			return ProbeLocal(dir)
		},
	}
}
