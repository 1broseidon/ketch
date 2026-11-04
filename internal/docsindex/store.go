// Package docsindex is the local docs index's storage (ADR-0007, amendment
// 5): one SQLite file, docs-index.sqlite in ketch's cache directory,
// separate from the page cache and the tag index, through a pure-Go SQLite
// (modernc.org/sqlite: no CGO). Like the bbolt stores it is opened for one
// operation at a time (ADR-0006) and never held between calls; SQLite's
// write-ahead log lets queries from MCP servers and the CLI read while an
// indexing run writes.
//
// Each indexed docs root is a library: a row of metadata, its pages, its
// sections, and a full-text index of its own (an FTS5 table per library,
// so term statistics are the library's, as a ranking within one library
// needs). One library is one version of one library's docs (its root is
// that version's docs root), so several versions sit side by side under
// their own keys.
//
// The full-text rows are what a Tokenizer makes of a section (heading,
// breadcrumb, identifier parts, body): the store keeps no copy of them
// (a contentless table), and a library whose rows were made by another
// tokenizer version has them rebuilt from its stored sections
// (EnsureFTS), with no network.
//
// Vectors are optional: float16 (embed.AppendHalf), one or more per
// section (a long section is embedded as several windows), present only
// when the library was built with hybrid ranking on, and comparable only
// with vectors of the model recorded in its metadata.
package docsindex

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/1broseidon/ketch/internal/embed"
	_ "modernc.org/sqlite" // the database/sql driver "sqlite"
)

// FormatVersion is the index layout this code writes. A library written
// in another format is rebuilt, never read (Library.Current): format 2 was
// the bbolt store, format 1 had no snippet units, no cross-version labels
// and looser version scoping.
const FormatVersion = 3

// Library is one indexed docs root.
type Library struct {
	// Key identifies the library in the store: its docs root URL.
	Key string `json:"key"`
	// Library is the value `docs index` was given (a name, spec or docs
	// URL); Names are the names a query may call it by, as comparison keys.
	Library string   `json:"library"`
	Names   []string `json:"names,omitempty"`
	// Aliases are the names asked for with --name, kept across builds
	// (Names is worked out again by each build, and includes them).
	Aliases []string `json:"aliases,omitempty"`
	Title   string   `json:"title,omitempty"`
	Root    string   `json:"root"`
	// Version is the version of the docs indexed: the label the request
	// named, else the one the site serves as current (when it says), else
	// "". Requested is the version the build was asked for ("" for the
	// current docs).
	Version   string `json:"version,omitempty"`
	Requested string `json:"requested,omitempty"`
	// Resolved says how the root was found: "docs_url" (given), or
	// "resolved" (a name, through the web backend's resolution, with
	// Evidence, Trust and Packages as it reported them).
	Resolved string   `json:"resolved"`
	Evidence []string `json:"evidence,omitempty"`
	Trust    float64  `json:"trust,omitempty"`
	Packages []string `json:"packages,omitempty"`
	// Model is the embedding model the vectors are from, and Dims their
	// length; "" and 0 for an index of text alone (the default).
	Model  string    `json:"model,omitempty"`
	Dims   int       `json:"dims,omitempty"`
	Format int       `json:"format"`
	Built  time.Time `json:"built_at"`
	// Pages, Sections and Vectors count what is stored. Discovered is how
	// many pages discovery listed (after collapsing versions and locales),
	// Capped whether the page cap left some out, Failed how many could not
	// be fetched, and Sources which discovery sources answered.
	Pages      int `json:"pages"`
	Sections   int `json:"sections"`
	Vectors    int `json:"vectors"`
	Units      int `json:"units,omitempty"`
	Discovered int `json:"discovered"`
	// OtherVersions is how many listed pages were left out as other
	// versions' trees (or copies of their pages).
	OtherVersions int      `json:"other_versions,omitempty"`
	Capped        bool     `json:"capped,omitempty"`
	Failed        int      `json:"failed,omitempty"`
	Sources       []string `json:"sources,omitempty"`
	// DisplayRoot is the unversioned root the current release's pages are
	// shown under, when the build indexed them at a release root whose
	// pages name the unversioned address canonical ("" otherwise). Seeds
	// is how many indexed pages a page seeder named.
	DisplayRoot string `json:"display_root,omitempty"`
	Seeds       int    `json:"seeds,omitempty"`
	// SiteName is the docs site's own name (og:site_name or its root
	// page's title, "Vue Router", "Beautiful Soup"): a name the library
	// answers to.
	SiteName string `json:"site_name,omitempty"`
	// Asked are the questions the search-first backend answered from this
	// library, newest last: a question asked again is answered from here
	// without searching.
	Asked []Asked `json:"asked,omitempty"`
}

// Asked is one question answered from a library.
type Asked struct {
	Question string `json:"q"`
	Library  string `json:"lib,omitempty"` // the library the request named, as given
	Vector   []byte `json:"v,omitempty"`   // float16 query vector (hybrid ranking only)
}

// MaxAsked caps the questions a library remembers.
const MaxAsked = 200

// Current reports whether the library is in the format this code reads.
func (l Library) Current() bool { return l.Format == FormatVersion }

// Page is one indexed page.
type Page struct {
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Hash     string `json:"hash"` // of the page's title and Markdown, as fetched
	Sections int    `json:"sections"`
	Windows  int    `json:"windows,omitempty"` // vectors across its sections
	Units    int    `json:"units,omitempty"`   // snippet units across its sections
	// Version is the library version the page was indexed under, Fetched
	// when it was stored, and Source how it came in ("index": a docs
	// index build; "search": a search-first question).
	Version string    `json:"version,omitempty"`
	Fetched time.Time `json:"fetched_at,omitzero"`
	Source  string    `json:"source,omitempty"`
}

// Section is one heading's worth of a page, as the web backend splits
// pages (never inside fenced code).
type Section struct {
	// ID is the section's row in the store (0 before it is stored): what
	// a full-text hit names.
	ID      int64    `json:"-"`
	Page    string   `json:"page"`
	Order   int      `json:"order"`
	Anchor  string   `json:"anchor,omitempty"`
	Heading string   `json:"heading"`
	Crumbs  []string `json:"crumbs,omitempty"`
	Text    string   `json:"text"`
	// Units are the section's snippet units, in order: each a byte range
	// of Text, so a snippet is always the docs' own words.
	Units []Unit `json:"units,omitempty"`
	// CrossVersion is the later major version a section documents though
	// it sits in this version's tree (a migration guide to it): "" for
	// most. Ranking demotes it for questions about the indexed version.
	CrossVersion string `json:"cross_version,omitempty"`
	// Vectors are the section's windows' embeddings, in order, and
	// UnitVectors its units', one each; none when the library has no
	// model. Stored apart from the text.
	Vectors     [][]float32 `json:"-"`
	UnitVectors [][]float32 `json:"-"`
}

// Family is the second-level heading a section sits under (its
// breadcrumb's second entry), "" for a page's lead: the ranking groups a
// page's sections by it.
func (s Section) Family() string {
	if len(s.Crumbs) >= 2 {
		return s.Crumbs[1]
	}
	return ""
}

// Unit kinds.
const (
	UnitCode  = "code"  // fenced code with the prose just before it
	UnitProse = "prose" // prose no code block claimed
)

// Unit is one snippet a section offers: for a code unit, the sentences
// just before a run of fenced code blocks (Explain, possibly empty) and
// the code (Body); for a prose unit, a sentence-bounded stretch of prose
// (Body). Ranges are [start, end) byte offsets into the section's Text.
type Unit struct {
	Kind    string `json:"kind"`
	Explain [2]int `json:"explain,omitzero"`
	Body    [2]int `json:"body"`
}

// Parts returns u's explanation and body as they appear in text ("" for a
// range text does not hold, which a stored section never has).
func (u Unit) Parts(text string) (explain, body string) {
	cut := func(r [2]int) string {
		if r[0] < 0 || r[1] > len(text) || r[0] >= r[1] {
			return ""
		}
		return text[r[0]:r[1]]
	}
	return cut(u.Explain), cut(u.Body)
}

// PageData is a page with its sections, as written together.
type PageData struct {
	Page     Page
	Sections []Section
}

// FTSColumns are the full-text columns, in order: the section's heading,
// its breadcrumb above it (page title and parent headings), the parts of
// the identifiers it names, and its body.
var FTSColumns = []string{"heading", "crumbs", "ident", "body"}

// Tokenizer makes a section's full-text row: one string per FTSColumns
// entry, words separated by spaces, as the docs package normalizes them
// (the same normalization its queries get). Version names the
// normalization: rows made by another version are rebuilt.
type Tokenizer interface {
	Version() string
	Row(Section) [4]string
}

// plainTokens is the tokenizer of a store opened without one: the text
// as written, which FTS5's own tokenizer splits.
type plainTokens struct{}

func (plainTokens) Version() string { return "plain" }
func (plainTokens) Row(s Section) [4]string {
	return [4]string{s.Heading, strings.Join(s.Crumbs, " "), "", s.Text}
}

// Store is the index file at a path. It holds no handle: every method
// opens the file for one operation.
type Store struct {
	path string
	tok  Tokenizer
}

// busyTimeout bounds how long an operation waits for another
// connection's write (an indexing run's batch).
const busyTimeout = 10 * time.Second

// Open returns the store at path (nothing is opened yet), its full-text
// rows made by tok (nil: the text as written).
func Open(path string, tok ...Tokenizer) *Store {
	s := &Store{path: path, tok: plainTokens{}}
	if len(tok) > 0 && tok[0] != nil {
		s.tok = tok[0]
	}
	return s
}

// DefaultPath is the index file: KETCH_DOCS_INDEX_PATH when set, else
// docs-index.sqlite in ketch's cache directory.
func DefaultPath() (string, error) {
	if p := os.Getenv("KETCH_DOCS_INDEX_PATH"); p != "" {
		return p, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cache dir: %w", err)
	}
	return filepath.Join(base, "ketch", DefaultFile), nil
}

// DefaultFile is the index file's name in ketch's cache directory, and
// LegacyFile the bbolt index earlier versions wrote there, which this one
// never reads.
const (
	DefaultFile = "docs-index.sqlite"
	LegacyFile  = "docs-index.db"
)

// LegacyNote says so when an earlier version's bbolt index sits beside
// the default index file: "" otherwise.
func LegacyNote(path string) string {
	legacy := filepath.Join(filepath.Dir(path), LegacyFile)
	if filepath.Base(path) != DefaultFile || legacy == path {
		return ""
	}
	if _, err := os.Stat(legacy); err != nil {
		return ""
	}
	return fmt.Sprintf("an earlier ketch's docs index (%s) is not read by this version; rebuild what you need with ketch docs index, then delete it", legacy)
}

// ErrNotSQLite is an index file that is not a SQLite database: one an
// earlier ketch wrote (bbolt), or something else at the path.
var ErrNotSQLite = errors.New("not a SQLite docs index (an earlier ketch's bbolt index?): delete it and rebuild with ketch docs index")

// Path is the store's file.
func (s *Store) Path() string { return s.path }

// Exists reports whether the file exists: a missing one holds nothing,
// and reading it costs a stat.
func (s *Store) Exists() bool {
	_, err := os.Stat(s.path)
	return err == nil
}

// Size is the file's size in bytes, its write-ahead log included (0 when
// missing).
func (s *Store) Size() int64 {
	var n int64
	for _, p := range []string{s.path, s.path + "-wal"} {
		if fi, err := os.Stat(p); err == nil {
			n += fi.Size()
		}
	}
	return n
}

const schema = `
CREATE TABLE IF NOT EXISTS libraries(
	id   INTEGER PRIMARY KEY,
	key  TEXT NOT NULL UNIQUE,
	meta TEXT,        -- the Library as JSON; NULL until a build's last write
	fts  TEXT NOT NULL DEFAULT ''  -- the tokenizer version of its full-text rows
);
CREATE TABLE IF NOT EXISTS pages(
	lib        INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
	url        TEXT NOT NULL,
	title      TEXT NOT NULL DEFAULT '',
	hash       TEXT NOT NULL DEFAULT '',
	version    TEXT NOT NULL DEFAULT '',
	fetched_at INTEGER NOT NULL DEFAULT 0,
	source     TEXT NOT NULL DEFAULT '',
	sections   INTEGER NOT NULL DEFAULT 0,
	windows    INTEGER NOT NULL DEFAULT 0,
	units      INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY(lib, url)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS sections(
	id            INTEGER PRIMARY KEY,
	lib           INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
	page          TEXT NOT NULL,
	ord           INTEGER NOT NULL,
	anchor        TEXT NOT NULL DEFAULT '',
	heading       TEXT NOT NULL DEFAULT '',
	crumbs        TEXT NOT NULL DEFAULT '[]',  -- JSON array
	family        TEXT NOT NULL DEFAULT '',    -- the H2 it sits under
	text          TEXT NOT NULL DEFAULT '',
	units         TEXT NOT NULL DEFAULT '',    -- JSON array of snippet units
	cross_version TEXT NOT NULL DEFAULT '',
	UNIQUE(lib, page, ord)
);
CREATE TABLE IF NOT EXISTS vectors(
	section INTEGER PRIMARY KEY REFERENCES sections(id) ON DELETE CASCADE,
	windows BLOB,
	units   BLOB
);
PRAGMA user_version = 1;
`

// sqliteMagic starts every SQLite database file.
var sqliteMagic = []byte("SQLite format 3\x00")

// checkFile fails for a file at the path that is not a SQLite database.
func (s *Store) checkFile() error {
	f, err := os.Open(s.path)
	if err != nil {
		return nil // missing: created on write
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, len(sqliteMagic))
	n, err := io.ReadFull(f, head)
	if n == 0 && (err == io.EOF || err == io.ErrUnexpectedEOF) {
		return nil // empty: SQLite makes it a database
	}
	if !bytes.Equal(head[:n], sqliteMagic) {
		return fmt.Errorf("docs index %s: %w", s.path, ErrNotSQLite)
	}
	return nil
}

// dsn is the connection string: a busy wait for another writer, the
// write-ahead log (readers never block a writer), foreign keys, and
// immediate write transactions (a writer takes the lock up front).
func (s *Store) dsn(write bool) string {
	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	if write {
		q.Add("_pragma", "journal_mode(WAL)")
		q.Set("_txlock", "immediate")
	} else {
		q.Set("mode", "ro")
	}
	return "file:" + filepath.ToSlash(s.path) + "?" + q.Encode()
}

// open opens the file for one operation; the caller closes it. A write
// creates the file, its private directory and the schema.
func (s *Store) open(write bool) (*sql.DB, error) {
	if err := s.checkFile(); err != nil {
		return nil, err
	}
	if write {
		if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
			return nil, fmt.Errorf("prepare docs index: %w", err)
		}
	}
	db, err := sql.Open("sqlite", s.dsn(write))
	if err != nil {
		return nil, fmt.Errorf("docs index: %w", err)
	}
	db.SetMaxOpenConns(1)
	if write {
		if _, err := db.Exec(schema); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("docs index: %w", err)
		}
	}
	return db, nil
}

// view runs fn over a read-only connection; a missing file holds nothing
// and calls nothing.
func (s *Store) view(fn func(*sql.DB) error) error {
	if !s.Exists() {
		return nil
	}
	db, err := s.open(false)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if err := fn(db); err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil // an empty database: nothing written yet
		}
		return fmt.Errorf("docs index: %w", err)
	}
	return nil
}

// update runs fn in one write transaction.
func (s *Store) update(fn func(*sql.Tx) error) error {
	db, err := s.open(true)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("docs index: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("docs index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("docs index: %w", err)
	}
	return nil
}

// Libraries lists every indexed library, by key.
func (s *Store) Libraries() ([]Library, error) {
	var out []Library
	err := s.view(func(db *sql.DB) error {
		rows, err := db.Query(`SELECT meta FROM libraries WHERE meta IS NOT NULL ORDER BY key`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var meta string
			if err := rows.Scan(&meta); err != nil {
				return err
			}
			var l Library
			if err := json.Unmarshal([]byte(meta), &l); err != nil {
				return fmt.Errorf("library record: %w", err)
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	return out, err
}

// Library returns the library at key.
func (s *Store) Library(key string) (Library, bool, error) {
	var l Library
	found := false
	err := s.view(func(db *sql.DB) error {
		var meta string
		switch err := db.QueryRow(`SELECT meta FROM libraries WHERE key = ? AND meta IS NOT NULL`, key).Scan(&meta); {
		case errors.Is(err, sql.ErrNoRows):
			return nil
		case err != nil:
			return err
		}
		found = true
		return json.Unmarshal([]byte(meta), &l)
	})
	return l, found, err
}

// libID returns (creating) the row id of the library at key.
func libID(tx *sql.Tx, key string) (int64, error) {
	if _, err := tx.Exec(`INSERT INTO libraries(key) VALUES(?) ON CONFLICT(key) DO NOTHING`, key); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow(`SELECT id FROM libraries WHERE key = ?`, key).Scan(&id)
	return id, err
}

// ftsTable names a library's full-text table: its row id, never text from
// outside.
func ftsTable(id int64) string { return fmt.Sprintf("fts_%d", id) }

// ensureFTSTable creates a library's full-text table.
func ensureFTSTable(tx *sql.Tx, id int64) error {
	_, err := tx.Exec(fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(%s, content='', contentless_delete=1, tokenize='unicode61 remove_diacritics 2')`,
		ftsTable(id), strings.Join(FTSColumns, ", ")))
	return err
}

// PutLibrary writes a library's metadata: the last write of a build, which
// makes it visible to queries.
func (s *Store) PutLibrary(l Library) error {
	v, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return s.update(func(tx *sql.Tx) error {
		if _, err := libID(tx, l.Key); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE libraries SET meta = ? WHERE key = ?`, string(v), l.Key)
		return err
	})
}

// Remove deletes a library and everything stored for it. Removing nothing
// is not an error; found says whether there was anything.
func (s *Store) Remove(key string) (found bool, err error) {
	if !s.Exists() {
		return false, nil
	}
	err = s.update(func(tx *sql.Tx) error {
		var id int64
		switch err := tx.QueryRow(`SELECT id FROM libraries WHERE key = ?`, key).Scan(&id); {
		case errors.Is(err, sql.ErrNoRows):
			return nil
		case err != nil:
			return err
		}
		found = true
		if _, err := tx.Exec(`DROP TABLE IF EXISTS ` + ftsTable(id)); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM libraries WHERE id = ?`, id) // cascades
		return err
	})
	return found, err
}

// Pages returns a library's stored pages by URL.
func (s *Store) Pages(key string) (map[string]Page, error) {
	out := map[string]Page{}
	err := s.view(func(db *sql.DB) error {
		rows, err := db.Query(`SELECT p.url, p.title, p.hash, p.version, p.fetched_at, p.source, p.sections, p.windows, p.units
			FROM pages p JOIN libraries l ON l.id = p.lib WHERE l.key = ?`, key)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var p Page
			var fetched int64
			if err := rows.Scan(&p.URL, &p.Title, &p.Hash, &p.Version, &fetched, &p.Source, &p.Sections, &p.Windows, &p.Units); err != nil {
				return err
			}
			if fetched > 0 {
				p.Fetched = time.Unix(fetched, 0).UTC()
			}
			out[p.URL] = p
		}
		return rows.Err()
	})
	return out, err
}

// PutPages writes pages with their sections, full-text rows and vectors
// in one transaction, replacing whatever each page had.
func (s *Store) PutPages(key string, pages []PageData) error {
	if len(pages) == 0 {
		return nil
	}
	return s.update(func(tx *sql.Tx) error {
		w, err := s.writer(tx, key)
		if err != nil {
			return err
		}
		defer w.close()
		for _, pd := range pages {
			if err := w.deletePage(pd.Page.URL); err != nil {
				return err
			}
			if err := w.putPage(pd); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeletePages removes pages and their sections.
func (s *Store) DeletePages(key string, urls []string) error {
	if len(urls) == 0 {
		return nil
	}
	return s.update(func(tx *sql.Tx) error {
		w, err := s.writer(tx, key)
		if err != nil {
			return err
		}
		defer w.close()
		for _, u := range urls {
			if err := w.deletePage(u); err != nil {
				return err
			}
		}
		return nil
	})
}

// pageWriter writes one library's pages within a transaction.
type pageWriter struct {
	tx  *sql.Tx
	tok Tokenizer
	lib int64
	fts string
	stmts
}

type stmts struct {
	delFTS, delSecs, delPage, insPage, insSec, insFTS, insVec *sql.Stmt
}

func (s *Store) writer(tx *sql.Tx, key string) (*pageWriter, error) {
	id, err := libID(tx, key)
	if err != nil {
		return nil, err
	}
	if err := ensureFTSTable(tx, id); err != nil {
		return nil, err
	}
	// A library's rows are all one tokenizer's: a write by another marks
	// the whole table for a rebuild (EnsureFTS) unless it is empty.
	if _, err := tx.Exec(`UPDATE libraries SET fts = ? WHERE id = ? AND (fts = '' OR NOT EXISTS (SELECT 1 FROM sections WHERE lib = ?))`, s.tok.Version(), id, id); err != nil {
		return nil, err
	}
	w := &pageWriter{tx: tx, tok: s.tok, lib: id, fts: ftsTable(id)}
	for _, p := range []struct {
		into **sql.Stmt
		q    string
	}{
		{&w.delFTS, `DELETE FROM ` + w.fts + ` WHERE rowid IN (SELECT id FROM sections WHERE lib = ? AND page = ?)`},
		{&w.delSecs, `DELETE FROM sections WHERE lib = ? AND page = ?`},
		{&w.delPage, `DELETE FROM pages WHERE lib = ? AND url = ?`},
		{&w.insPage, `INSERT INTO pages(lib, url, title, hash, version, fetched_at, source, sections, windows, units) VALUES(?,?,?,?,?,?,?,?,?,?)`},
		{&w.insSec, `INSERT INTO sections(lib, page, ord, anchor, heading, crumbs, family, text, units, cross_version) VALUES(?,?,?,?,?,?,?,?,?,?)`},
		{&w.insFTS, `INSERT INTO ` + w.fts + `(rowid, heading, crumbs, ident, body) VALUES(?,?,?,?,?)`},
		{&w.insVec, `INSERT OR REPLACE INTO vectors(section, windows, units) VALUES(?,?,?)`},
	} {
		if *p.into, err = tx.Prepare(p.q); err != nil {
			w.close()
			return nil, err
		}
	}
	return w, nil
}

func (w *pageWriter) close() {
	for _, st := range []*sql.Stmt{w.delFTS, w.delSecs, w.delPage, w.insPage, w.insSec, w.insFTS, w.insVec} {
		if st != nil {
			_ = st.Close()
		}
	}
}

// deletePage removes a page's record, sections, full-text rows and
// vectors (cascaded).
func (w *pageWriter) deletePage(url string) error {
	if _, err := w.delFTS.Exec(w.lib, url); err != nil {
		return err
	}
	if _, err := w.delSecs.Exec(w.lib, url); err != nil {
		return err
	}
	_, err := w.delPage.Exec(w.lib, url)
	return err
}

func (w *pageWriter) putPage(pd PageData) error {
	p := pd.Page
	var fetched int64
	if !p.Fetched.IsZero() {
		fetched = p.Fetched.Unix()
	}
	if _, err := w.insPage.Exec(w.lib, p.URL, p.Title, p.Hash, p.Version, fetched, p.Source, p.Sections, p.Windows, p.Units); err != nil {
		return err
	}
	for _, sec := range pd.Sections {
		sec.Page = p.URL
		id, err := w.putSection(sec)
		if err != nil {
			return err
		}
		if err := w.putVectors(id, sec); err != nil {
			return err
		}
	}
	return nil
}

func (w *pageWriter) putSection(sec Section) (int64, error) {
	crumbs, err := json.Marshal(sec.Crumbs)
	if err != nil {
		return 0, err
	}
	units := ""
	if len(sec.Units) > 0 {
		b, err := json.Marshal(sec.Units)
		if err != nil {
			return 0, err
		}
		units = string(b)
	}
	res, err := w.insSec.Exec(w.lib, sec.Page, sec.Order, sec.Anchor, sec.Heading, string(crumbs), sec.Family(), sec.Text, units, sec.CrossVersion)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	row := w.tok.Row(sec)
	_, err = w.insFTS.Exec(id, row[0], row[1], row[2], row[3])
	return id, err
}

// putVectors stores a section's vectors, or nothing when it has none.
func (w *pageWriter) putVectors(id int64, sec Section) error {
	if len(sec.Vectors) == 0 && len(sec.UnitVectors) == 0 {
		return nil
	}
	_, err := w.insVec.Exec(id, nilIfEmpty(encodeVectors(sec.Vectors)), nilIfEmpty(encodeVectors(sec.UnitVectors)))
	return err
}

func nilIfEmpty(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// sectionCols are the columns scanSection reads, from sections s left
// joined to vectors v.
const sectionCols = `s.id, s.page, s.ord, s.anchor, s.heading, s.crumbs, s.text, s.units, s.cross_version`

// scanSection reads one section row (sectionCols, then optionally the
// window and unit vector blobs).
func scanSection(rows *sql.Rows, dims int, withVectors bool) (Section, error) {
	var sec Section
	var crumbs, units string
	dest := []any{&sec.ID, &sec.Page, &sec.Order, &sec.Anchor, &sec.Heading, &crumbs, &sec.Text, &units, &sec.CrossVersion}
	var wv, uv []byte
	if withVectors {
		dest = append(dest, &wv, &uv)
	}
	if err := rows.Scan(dest...); err != nil {
		return sec, err
	}
	if crumbs != "" && crumbs != "[]" && crumbs != "null" {
		if err := json.Unmarshal([]byte(crumbs), &sec.Crumbs); err != nil {
			return sec, err
		}
	}
	if units != "" {
		if err := json.Unmarshal([]byte(units), &sec.Units); err != nil {
			return sec, err
		}
	}
	if withVectors && dims > 0 {
		sec.Vectors, sec.UnitVectors = decodeVectors(wv, dims), decodeVectors(uv, dims)
	}
	return sec, nil
}

// querySections runs a section query and reads its rows.
func querySections(db *sql.DB, dims int, withVectors bool, q string, args ...any) ([]Section, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Section
	for rows.Next() {
		sec, err := scanSection(rows, dims, withVectors)
		if err != nil {
			return nil, err
		}
		out = append(out, sec)
	}
	return out, rows.Err()
}

// Sections returns every section of a library in page and section order,
// with its window and unit vectors (of dims dimensions each) when
// withVectors is set and the section has them.
func (s *Store) Sections(key string, dims int, withVectors bool) ([]Section, error) {
	var out []Section
	err := s.view(func(db *sql.DB) error {
		var err error
		if withVectors && dims > 0 {
			out, err = querySections(db, dims, true, `SELECT `+sectionCols+`, v.windows, v.units FROM sections s
				JOIN libraries l ON l.id = s.lib LEFT JOIN vectors v ON v.section = s.id WHERE l.key = ? ORDER BY s.page, s.ord`, key)
		} else {
			out, err = querySections(db, 0, false, `SELECT `+sectionCols+` FROM sections s
				JOIN libraries l ON l.id = s.lib WHERE l.key = ? ORDER BY s.page, s.ord`, key)
		}
		return err
	})
	return out, err
}

// PageSections returns the sections of one page of a library, in order,
// without vectors: an index lookup, so reading one section of a large
// index costs a seek, not a pass over the library. It returns none for a
// page the library does not hold.
func (s *Store) PageSections(key, page string) ([]Section, error) {
	return s.SectionsOfPages(key, []string{page})
}

// SectionsOfPages returns the sections of the given pages of a library,
// without vectors, page by page in order.
func (s *Store) SectionsOfPages(key string, pages []string) ([]Section, error) {
	if len(pages) == 0 {
		return nil, nil
	}
	list, err := json.Marshal(pages)
	if err != nil {
		return nil, err
	}
	var out []Section
	err = s.view(func(db *sql.DB) error {
		var err error
		out, err = querySections(db, 0, false, `SELECT `+sectionCols+` FROM sections s
			JOIN libraries l ON l.id = s.lib WHERE l.key = ? AND s.page IN (SELECT value FROM json_each(?)) ORDER BY s.page, s.ord`, key, string(list))
		return err
	})
	return out, err
}

// PutVectors replaces the window and unit vectors of the given sections
// (keyed by page URL and order), for embedding with another model.
func (s *Store) PutVectors(key string, secs []Section) error {
	return s.update(func(tx *sql.Tx) error {
		id, err := libID(tx, key)
		if err != nil {
			return err
		}
		find, err := tx.Prepare(`SELECT id FROM sections WHERE lib = ? AND page = ? AND ord = ?`)
		if err != nil {
			return err
		}
		defer func() { _ = find.Close() }()
		put, err := tx.Prepare(`INSERT OR REPLACE INTO vectors(section, windows, units) VALUES(?,?,?)`)
		if err != nil {
			return err
		}
		defer func() { _ = put.Close() }()
		for _, sec := range secs {
			var sid int64
			switch err := find.QueryRow(id, sec.Page, sec.Order).Scan(&sid); {
			case errors.Is(err, sql.ErrNoRows):
				continue
			case err != nil:
				return err
			}
			if _, err := put.Exec(sid, nilIfEmpty(encodeVectors(sec.Vectors)), nilIfEmpty(encodeVectors(sec.UnitVectors))); err != nil {
				return err
			}
		}
		return nil
	})
}

// Hit is one full-text match: a section's ID and its score (FTS5's bm25,
// negated: higher is better).
type Hit struct {
	ID    int64
	Score float64
}

// Search runs a full-text query (FTS5 query syntax over FTSColumns) on a
// library's sections and returns the best limit hits, best first, scored
// by bm25 with the given column weights (one per FTSColumns entry). The
// library's rows must be the store's tokenizer's (EnsureFTS).
func (s *Store) Search(key, match string, weights []float64, limit int) ([]Hit, error) {
	if len(weights) != len(FTSColumns) {
		return nil, fmt.Errorf("docs index: %d column weights for %d columns", len(weights), len(FTSColumns))
	}
	var out []Hit
	err := s.view(func(db *sql.DB) error {
		var id int64
		switch err := db.QueryRow(`SELECT id FROM libraries WHERE key = ?`, key).Scan(&id); {
		case errors.Is(err, sql.ErrNoRows):
			return nil
		case err != nil:
			return err
		}
		ws := make([]string, len(weights))
		for i, w := range weights {
			ws[i] = fmt.Sprintf("%g", w)
		}
		t := ftsTable(id)
		rows, err := db.Query(fmt.Sprintf(`SELECT rowid, -bm25(%s, %s) AS score FROM %s WHERE %s MATCH ? ORDER BY score DESC LIMIT ?`,
			t, strings.Join(ws, ", "), t, t), match, limit)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var h Hit
			if err := rows.Scan(&h.ID, &h.Score); err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

// SectionsByID returns the sections with the given IDs (of any library),
// without vectors, in no particular order.
func (s *Store) SectionsByID(ids []int64) ([]Section, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	var out []Section
	err = s.view(func(db *sql.DB) error {
		var err error
		out, err = querySections(db, 0, false, `SELECT `+sectionCols+` FROM sections s WHERE s.id IN (SELECT value FROM json_each(?))`, string(list))
		return err
	})
	return out, err
}

// FTSCurrent reports whether a library's full-text rows were made by the
// store's tokenizer (true for a library the store does not hold).
func (s *Store) FTSCurrent(key string) (bool, error) {
	current := true
	err := s.view(func(db *sql.DB) error {
		var v string
		switch err := db.QueryRow(`SELECT fts FROM libraries WHERE key = ?`, key).Scan(&v); {
		case errors.Is(err, sql.ErrNoRows):
			return nil
		case err != nil:
			return err
		}
		current = v == s.tok.Version()
		return nil
	})
	return current, err
}

// EnsureFTS rebuilds a library's full-text rows from its stored sections
// when another tokenizer version made them: no network, a second or two
// for the largest libraries. It reports whether it rebuilt.
func (s *Store) EnsureFTS(key string) (bool, error) {
	ok, err := s.FTSCurrent(key)
	if err != nil || ok {
		return false, err
	}
	return true, s.RebuildFTS(key)
}

// RebuildFTS remakes a library's full-text table from its stored
// sections with the store's tokenizer.
func (s *Store) RebuildFTS(key string) error {
	secs, err := s.Sections(key, 0, false)
	if err != nil {
		return err
	}
	return s.update(func(tx *sql.Tx) error {
		id, err := libID(tx, key)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE IF EXISTS ` + ftsTable(id)); err != nil {
			return err
		}
		if err := ensureFTSTable(tx, id); err != nil {
			return err
		}
		ins, err := tx.Prepare(`INSERT INTO ` + ftsTable(id) + `(rowid, heading, crumbs, ident, body) VALUES(?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer func() { _ = ins.Close() }()
		for _, sec := range secs {
			row := s.tok.Row(sec)
			if _, err := ins.Exec(sec.ID, row[0], row[1], row[2], row[3]); err != nil {
				return err
			}
		}
		_, err = tx.Exec(`UPDATE libraries SET fts = ? WHERE id = ?`, s.tok.Version(), id)
		return err
	})
}

// encodeVectors concatenates float16 vectors.
func encodeVectors(vecs [][]float32) []byte {
	n := 0
	for _, v := range vecs {
		n += 2 * len(v)
	}
	b := make([]byte, 0, n)
	for _, v := range vecs {
		b = embed.AppendHalf(b, v)
	}
	return b
}

// decodeVectors splits concatenated float16 vectors of dims each.
func decodeVectors(b []byte, dims int) [][]float32 {
	if len(b) == 0 || dims <= 0 || len(b)%(2*dims) != 0 {
		return nil
	}
	flat := embed.FromHalf(nil, b)
	out := make([][]float32, 0, len(flat)/dims)
	for i := 0; i < len(flat); i += dims {
		out = append(out, flat[i:i+dims:i+dims])
	}
	return out
}
