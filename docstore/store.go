package docstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/1broseidon/ketch/extract/sections"
	_ "modernc.org/sqlite" // pure-Go SQLite with FTS5; keeps CGO_ENABLED=0
)

// SectionMaxChars caps one indexed section. Longer sections are split at
// paragraph boundaries by sections.Split so a single hit stays readable
// and a page with one giant section still yields ranked pieces.
const SectionMaxChars = 4000

// ErrNotFound reports a library name that is not in the store.
var ErrNotFound = errors.New("library not found")

// ErrEmpty reports an ingest that produced no indexable content.
var ErrEmpty = errors.New("no indexable content")

// libraryName is the accepted shape of a library name: a short lower-case
// slug so it is safe in a manifest, on a command line, and as an FTS filter.
var libraryName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidateName reports whether name is a legal library name.
func ValidateName(name string) error {
	if !libraryName.MatchString(name) {
		return fmt.Errorf("invalid library name %q: use lower-case letters, digits, '.', '_' or '-' (max 64 chars)", name)
	}
	return nil
}

// Library is one stored documentation library and how it was built.
type Library struct {
	Name      string    `json:"name"`
	Version   string    `json:"version,omitempty"`
	Seed      string    `json:"seed"`
	Source    Source    `json:"source"`
	SourceURL string    `json:"source_url,omitempty"`
	Prefix    string    `json:"prefix,omitempty"`
	MaxPages  int       `json:"max_pages,omitempty"`
	Pages     int       `json:"pages"`
	Sections  int       `json:"sections"`
	AddedAt   time.Time `json:"added_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Page is one fetched document belonging to a library.
type Page struct {
	URL          string
	Title        string
	Markdown     string
	ETag         string
	LastModified string
	ContentHash  string
}

// Hit is one ranked section returned by Search.
type Hit struct {
	Library    string
	Version    string
	URL        string // page URL, with the heading anchor appended when known
	PageTitle  string
	Title      string
	Breadcrumb string
	Body       string
	Rank       float64 // bm25 score; lower is better
}

// Query scopes a Search. An empty Libraries slice searches every library.
type Query struct {
	Text      string
	Libraries []string
	Limit     int
}

// Store is an open docs database. It is safe for concurrent use.
type Store struct {
	db   *sql.DB
	path string
}

// Exists reports whether a docs database has been created under dir. It is
// the no-I/O usability check for the local provider.
func Exists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, DBFile))
	return err == nil
}

// Open opens (creating if needed) the docs database under dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create docs dir: %w", err)
	}
	path := filepath.Join(dir, DBFile)
	// busy_timeout lets two ketch processes (an agent adding, another
	// searching) share the file without spurious "database is locked".
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("open docs db: %w", err)
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS libraries (
	name       TEXT PRIMARY KEY,
	version    TEXT NOT NULL DEFAULT '',
	seed       TEXT NOT NULL,
	source     TEXT NOT NULL,
	source_url TEXT NOT NULL DEFAULT '',
	prefix     TEXT NOT NULL DEFAULT '',
	max_pages  INTEGER NOT NULL DEFAULT 0,
	pages      INTEGER NOT NULL DEFAULT 0,
	sections   INTEGER NOT NULL DEFAULT 0,
	added_at   TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS pages (
	library       TEXT NOT NULL,
	url           TEXT NOT NULL,
	title         TEXT NOT NULL DEFAULT '',
	etag          TEXT NOT NULL DEFAULT '',
	last_modified TEXT NOT NULL DEFAULT '',
	content_hash  TEXT NOT NULL DEFAULT '',
	markdown      TEXT NOT NULL,
	PRIMARY KEY (library, url)
);
CREATE VIRTUAL TABLE IF NOT EXISTS sections USING fts5(
	library UNINDEXED,
	url UNINDEXED,
	anchor UNINDEXED,
	page_title UNINDEXED,
	title,
	breadcrumb,
	body,
	tokenize = 'porter unicode61'
);
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT OR IGNORE INTO meta(key, value) VALUES ('schema_version', '1');
`

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("init docs schema: %w", err)
	}
	return nil
}

// Libraries lists every stored library in name order.
func (s *Store) Libraries() ([]Library, error) {
	rows, err := s.db.Query(`SELECT name, version, seed, source, source_url, prefix, max_pages, pages, sections, added_at, updated_at FROM libraries ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var libs []Library
	for rows.Next() {
		lib, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		libs = append(libs, lib)
	}
	return libs, rows.Err()
}

// Library returns one library by name, or ErrNotFound.
func (s *Store) Library(name string) (*Library, error) {
	row := s.db.QueryRow(`SELECT name, version, seed, source, source_url, prefix, max_pages, pages, sections, added_at, updated_at FROM libraries WHERE name = ?`, name)
	lib, err := scanLibrary(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	if err != nil {
		return nil, err
	}
	return &lib, nil
}

type scanner interface{ Scan(dest ...any) error }

func scanLibrary(r scanner) (Library, error) {
	var lib Library
	var source, added, updated string
	if err := r.Scan(&lib.Name, &lib.Version, &lib.Seed, &source, &lib.SourceURL, &lib.Prefix, &lib.MaxPages, &lib.Pages, &lib.Sections, &added, &updated); err != nil {
		return Library{}, err
	}
	lib.Source = Source(source)
	lib.AddedAt, _ = time.Parse(time.RFC3339, added)
	lib.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return lib, nil
}

// Remove deletes a library and everything indexed under it. Removing a name
// that is not stored is ErrNotFound.
func (s *Store) Remove(name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.Exec(`DELETE FROM libraries WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	if _, err := tx.Exec(`DELETE FROM pages WHERE library = ?`, name); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sections WHERE library = ?`, name); err != nil {
		return err
	}
	return tx.Commit()
}

// Replace atomically replaces a library's pages and index with the given
// pages, chunking each with sections.Split. Pages that yield no sections
// are still stored (their markdown is kept) but contribute nothing to the
// index. It returns the stored library with counts filled in. A page set
// that yields zero sections overall is ErrEmpty and leaves the store
// unchanged.
func (s *Store) Replace(lib Library, pages []Page) (*Library, error) {
	type chunk struct {
		page Page
		secs []sections.Section
	}
	var chunks []chunk
	total := 0
	for _, p := range pages {
		secs := sections.Split(p.Title, p.Markdown, SectionMaxChars)
		total += len(secs)
		chunks = append(chunks, chunk{page: p, secs: secs})
	}
	if total == 0 {
		return nil, ErrEmpty
	}

	now := time.Now().UTC().Truncate(time.Second)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	var added string
	switch err := tx.QueryRow(`SELECT added_at FROM libraries WHERE name = ?`, lib.Name).Scan(&added); {
	case errors.Is(err, sql.ErrNoRows):
		added = now.Format(time.RFC3339)
	case err != nil:
		return nil, err
	}
	for _, stmt := range []string{`DELETE FROM pages WHERE library = ?`, `DELETE FROM sections WHERE library = ?`} {
		if _, err := tx.Exec(stmt, lib.Name); err != nil {
			return nil, err
		}
	}

	insPage, err := tx.Prepare(`INSERT INTO pages(library, url, title, etag, last_modified, content_hash, markdown) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer insPage.Close()
	insSec, err := tx.Prepare(`INSERT INTO sections(library, url, anchor, page_title, title, breadcrumb, body) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer insSec.Close()

	for _, c := range chunks {
		p := c.page
		if _, err := insPage.Exec(lib.Name, p.URL, p.Title, p.ETag, p.LastModified, p.ContentHash, p.Markdown); err != nil {
			return nil, fmt.Errorf("store page %s: %w", p.URL, err)
		}
		for _, sec := range c.secs {
			if _, err := insSec.Exec(lib.Name, p.URL, sec.Anchor, p.Title, sec.Heading, strings.Join(sec.Breadcrumb, " › "), sec.Body); err != nil {
				return nil, fmt.Errorf("index section of %s: %w", p.URL, err)
			}
		}
	}

	lib.Pages, lib.Sections = len(pages), total
	lib.AddedAt, _ = time.Parse(time.RFC3339, added)
	lib.UpdatedAt = now
	if _, err := tx.Exec(`INSERT OR REPLACE INTO libraries(name, version, seed, source, source_url, prefix, max_pages, pages, sections, added_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		lib.Name, lib.Version, lib.Seed, string(lib.Source), lib.SourceURL, lib.Prefix, lib.MaxPages, lib.Pages, lib.Sections, added, now.Format(time.RFC3339)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &lib, nil
}

// Search runs an FTS5 query over sections, ranked by bm25 with heading and
// breadcrumb weighted above body. Free-text input is turned into an
// all-terms match first; when that finds nothing, an any-term match runs so
// question-shaped queries still return the closest sections. Library names
// in q.Libraries that are not stored simply match nothing.
func (s *Store) Search(ctx context.Context, q Query) ([]Hit, error) {
	terms := Terms(q.Text)
	if len(terms) == 0 {
		return nil, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 10
	}
	hits, err := s.search(ctx, ftsMatch(terms, "AND"), q.Libraries, limit)
	if err != nil || len(hits) > 0 || len(terms) == 1 {
		return hits, err
	}
	return s.search(ctx, ftsMatch(terms, "OR"), q.Libraries, limit)
}

func (s *Store) search(ctx context.Context, match string, libraries []string, limit int) ([]Hit, error) {
	// Column order in the FTS table: library, url, anchor, page_title
	// (unindexed, weight 0), then title, breadcrumb, body.
	// The FTS5 table cannot be aliased or joined in the MATCH clause without
	// the table name becoming ambiguous, so it is queried alone and the
	// library version is pulled in by a correlated subquery.
	const rank = `bm25(sections, 0, 0, 0, 0, 10.0, 4.0, 1.0)`
	query := `SELECT library, (SELECT version FROM libraries WHERE name = sections.library), url, anchor, page_title, title, breadcrumb, body, ` + rank + ` AS r
		FROM sections WHERE sections MATCH ?`
	args := []any{match}
	if len(libraries) > 0 {
		query += ` AND library IN (?` + strings.Repeat(",?", len(libraries)-1) + `)`
		for _, name := range libraries {
			args = append(args, name)
		}
	}
	query += ` ORDER BY r LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("docs search: %w", err)
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var h Hit
		var anchor string
		if err := rows.Scan(&h.Library, &h.Version, &h.URL, &anchor, &h.PageTitle, &h.Title, &h.Breadcrumb, &h.Body, &h.Rank); err != nil {
			return nil, err
		}
		if anchor != "" {
			h.URL += "#" + anchor
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// Terms splits free text into FTS-safe search terms: runs of letters and
// digits, lower-cased, de-duplicated, in order of first appearance. FTS5
// query syntax characters never reach the engine.
func Terms(text string) []string {
	var terms []string
	seen := map[string]bool{}
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		t := strings.ToLower(cur.String())
		cur.Reset()
		if !seen[t] {
			seen[t] = true
			terms = append(terms, t)
		}
	}
	for _, r := range text {
		if isWordRune(r) {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return terms
}

// isWordRune mirrors the unicode61 tokenizer: letters and digits are token
// characters, everything else (including '_' and '.') separates tokens.
func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }

// ftsMatch joins quoted terms with the given FTS5 boolean operator.
func ftsMatch(terms []string, op string) string {
	quoted := make([]string, len(terms))
	for i, t := range terms {
		quoted[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	return strings.Join(quoted, " "+op+" ")
}

// Source names how a library's pages were obtained. It is recorded on the
// Library so `docs list` and a project manifest can say where content came
// from; the ingest package decides which applies.
type Source string

const (
	// SourceLLMSFull is a single llms-full.txt document holding the whole
	// site's docs as markdown: one fetch, no crawl.
	SourceLLMSFull Source = "llms-full"
	// SourceLLMS is an llms.txt index whose links are fetched as pages.
	SourceLLMS Source = "llms"
	// SourceSitemap is a sitemap (or sitemap index) whose URLs are fetched.
	SourceSitemap Source = "sitemap"
	// SourceCrawl is a same-host BFS crawl from the seed, scoped to a prefix.
	SourceCrawl Source = "crawl"
)
