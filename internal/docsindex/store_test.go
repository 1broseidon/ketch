package docsindex

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const testKey = "https://docs.example.test/"

// seed writes two pages, one with two sections, and the library's
// metadata.
func seed(t *testing.T, s *Store) []PageData {
	t.Helper()
	key := testKey
	pages := []PageData{
		{Page: Page{URL: key + "b", Title: "B", Hash: "h-b"}, Sections: []Section{
			{Order: 0, Heading: "B", Crumbs: []string{"B"}, Text: "intro of b"},
			{Order: 1, Anchor: "two", Heading: "Two", Crumbs: []string{"B", "Two"}, Text: "second"},
		}},
		{Page: Page{URL: key + "a", Title: "A", Hash: "h-a"}, Sections: []Section{{Order: 0, Heading: "A", Text: "only a"}}},
	}
	if err := s.PutPages(key, pages); err != nil {
		t.Fatal(err)
	}
	lib := Library{Key: key, Library: "example", Names: []string{"example"}, Root: key, Title: "Example", Format: FormatVersion, Built: time.Now().UTC(), Pages: 2, Sections: 3}
	if err := s.PutLibrary(lib); err != nil {
		t.Fatal(err)
	}
	return pages
}

// A library round-trips: metadata, pages, and sections in page order.
func TestStoreRoundTrip(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "docs-index.db"))
	if libs, err := s.Libraries(); err != nil || len(libs) != 0 || s.Exists() {
		t.Fatalf("a missing file is an empty index: %v %v", libs, err)
	}
	seed(t, s)
	if got, ok, err := s.Library(testKey); err != nil || !ok || got.Title != "Example" || !got.Current() {
		t.Fatalf("Library = %+v %v %v", got, ok, err)
	}
	secs, err := s.Sections(testKey)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, sec := range secs {
		order = append(order, sec.Page[len(testKey):]+"/"+sec.Heading)
	}
	if !slices.Equal(order, []string{"a/A", "b/B", "b/Two"}) {
		t.Fatalf("section order = %v", order)
	}
	if two := secs[2]; two.Anchor != "two" || !slices.Equal(two.Crumbs, []string{"B", "Two"}) || two.Text != "second" || two.Family() != "Two" {
		t.Errorf("section = %+v", two)
	}
}

// A page written again replaces its sections rather than adding to them,
// and a deleted page takes its sections with it.
func TestStoreReplaceAndDeletePages(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "docs-index.db"))
	pages := seed(t, s)
	pages[0].Sections = pages[0].Sections[:1]
	pages[0].Page.Hash = "h-b2"
	if err := s.PutPages(testKey, pages[:1]); err != nil {
		t.Fatal(err)
	}
	if secs, _ := s.Sections(testKey); len(secs) != 2 {
		t.Errorf("a rewritten page kept its old sections: %d", len(secs))
	}
	if ps, _ := s.Pages(testKey); ps[testKey+"b"].Hash != "h-b2" || len(ps) != 2 {
		t.Errorf("pages = %+v", ps)
	}
	if err := s.DeletePages(testKey, []string{testKey + "a"}); err != nil {
		t.Fatal(err)
	}
	if secs, _ := s.Sections(testKey); len(secs) != 1 || secs[0].Page != testKey+"b" {
		t.Errorf("after delete = %+v", secs)
	}
}

// Remove takes the library and all it stored; removing nothing reports
// so.
func TestStoreRemove(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "docs-index.db"))
	const key = "https://x.test/docs/"
	if err := s.PutPages(key, []PageData{{Page: Page{URL: key}, Sections: []Section{{Order: 0, Heading: "X", Text: "x"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutLibrary(Library{Key: key, Root: key, Format: FormatVersion}); err != nil {
		t.Fatal(err)
	}
	if found, err := s.Remove(key); err != nil || !found {
		t.Fatalf("Remove = %v %v", found, err)
	}
	if libs, _ := s.Libraries(); len(libs) != 0 {
		t.Errorf("libraries after remove = %v", libs)
	}
	if found, err := s.Remove(key); err != nil || found {
		t.Errorf("removing nothing = %v %v", found, err)
	}
}

// The store holds no handle between operations (ADR-0006): another
// process can open the file between any two calls.
func TestStoreOpensPerOperation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docs-index.db")
	s := Open(path)
	if err := s.PutLibrary(Library{Key: "k", Format: FormatVersion}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("BEGIN EXCLUSIVE; COMMIT"); err != nil {
		t.Fatalf("the file stayed locked after the write: %v", err)
	}
}

// A file that is not SQLite (an earlier ketch's bbolt index) is refused
// with ErrNotSQLite, never overwritten.
func TestStoreRefusesOtherFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docs-index.db")
	if err := os.WriteFile(path, []byte("\x00\x00\x00\x00bolt-ish page header, not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Open(path)
	if _, err := s.Libraries(); !errors.Is(err, ErrNotSQLite) {
		t.Errorf("Libraries = %v", err)
	}
	if err := s.PutLibrary(Library{Key: "k", Format: FormatVersion}); !errors.Is(err, ErrNotSQLite) {
		t.Errorf("PutLibrary = %v", err)
	}
}

// A note names an earlier ketch's bbolt index beside the default file.
func TestLegacyNote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFile)
	if n := LegacyNote(path); n != "" {
		t.Errorf("no legacy file: %q", n)
	}
	if err := os.WriteFile(filepath.Join(dir, LegacyFile), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n := LegacyNote(path); n == "" {
		t.Error("legacy file not noted")
	}
	if n := LegacyNote(filepath.Join(dir, "other.sqlite")); n != "" {
		t.Errorf("a path set by the operator is not the default: %q", n)
	}
}

type upperTokens struct{ v string }

func (u upperTokens) Version() string { return u.v }
func (u upperTokens) Row(s Section) [4]string {
	return [4]string{s.Heading, "", "ident" + s.Heading, s.Text}
}

// Full-text search finds sections by the tokenizer's rows, in every
// column.
func TestStoreSearchAndRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docs-index.db")
	s := Open(path, upperTokens{"v1"})
	seed(t, s)
	hits, err := s.Search(testKey, "intro OR second", []float64{1, 1, 1, 1}, 10)
	if err != nil || len(hits) != 2 {
		t.Fatalf("Search = %+v %v", hits, err)
	}
	hits, err = s.Search(testKey, "identtwo", []float64{1, 1, 1, 1}, 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("ident column = %+v %v", hits, err)
	}
	secs, err := s.SectionsByID([]int64{hits[0].ID})
	if err != nil || len(secs) != 1 || secs[0].Anchor != "two" {
		t.Fatalf("SectionsByID = %+v %v", secs, err)
	}
}

// A tokenizer change rebuilds a library's full-text rows from its stored
// sections, once.
func TestStoreFTSRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docs-index.db")
	s := Open(path, upperTokens{"v1"})
	seed(t, s)
	if ok, err := s.FTSCurrent(testKey); err != nil || !ok {
		t.Fatalf("FTSCurrent = %v %v", ok, err)
	}
	s2 := Open(path, upperTokens{"v2"})
	if ok, _ := s2.FTSCurrent(testKey); ok {
		t.Fatal("another tokenizer's rows read as current")
	}
	if rebuilt, err := s2.EnsureFTS(testKey); err != nil || !rebuilt {
		t.Fatalf("EnsureFTS = %v %v", rebuilt, err)
	}
	if rebuilt, _ := s2.EnsureFTS(testKey); rebuilt {
		t.Error("rebuilt twice")
	}
	if hits, _ := s2.Search(testKey, "identb", []float64{1, 1, 1, 1}, 10); len(hits) != 1 {
		t.Errorf("after rebuild = %+v", hits)
	}
	if secs, _ := s2.SectionsOfPages(testKey, []string{testKey + "b", testKey + "a"}); len(secs) != 3 || secs[0].Page != testKey+"a" {
		t.Errorf("SectionsOfPages = %+v", secs)
	}
}

// PageSections reads one page's sections in order, and none for a page the
// library lacks or one whose URL only starts like a stored page's.
func TestStorePageSections(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "docs-index.db"))
	seed(t, s)
	secs, err := s.PageSections(testKey, testKey+"b")
	if err != nil || len(secs) != 2 || secs[0].Heading != "B" || secs[1].Anchor != "two" {
		t.Fatalf("page b = %+v %v", secs, err)
	}
	for _, page := range []string{testKey + "c", testKey, testKey + "b/x"} {
		if secs, err := s.PageSections(testKey, page); err != nil || len(secs) != 0 {
			t.Errorf("%s = %+v %v", page, secs, err)
		}
	}
	if secs, err := s.PageSections("https://nope.test/", testKey+"b"); err != nil || len(secs) != 0 {
		t.Errorf("unknown library = %+v %v", secs, err)
	}
}

// A format-3 file written while the index could also hold embeddings and
// snippet units (a vectors table, units and window counts) is read and
// written as it is: those columns and that table are left alone.
func TestStoreReadsEarlierFormat3Schema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docs-index.sqlite")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE libraries(id INTEGER PRIMARY KEY, key TEXT NOT NULL UNIQUE, meta TEXT, fts TEXT NOT NULL DEFAULT '');
CREATE TABLE pages(lib INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE, url TEXT NOT NULL, title TEXT NOT NULL DEFAULT '',
	hash TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '', fetched_at INTEGER NOT NULL DEFAULT 0, source TEXT NOT NULL DEFAULT '',
	sections INTEGER NOT NULL DEFAULT 0, windows INTEGER NOT NULL DEFAULT 0, units INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(lib, url)) WITHOUT ROWID;
CREATE TABLE sections(id INTEGER PRIMARY KEY, lib INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE, page TEXT NOT NULL,
	ord INTEGER NOT NULL, anchor TEXT NOT NULL DEFAULT '', heading TEXT NOT NULL DEFAULT '', crumbs TEXT NOT NULL DEFAULT '[]',
	family TEXT NOT NULL DEFAULT '', text TEXT NOT NULL DEFAULT '', units TEXT NOT NULL DEFAULT '', cross_version TEXT NOT NULL DEFAULT '',
	UNIQUE(lib, page, ord));
CREATE TABLE vectors(section INTEGER PRIMARY KEY REFERENCES sections(id) ON DELETE CASCADE, windows BLOB, units BLOB);
INSERT INTO libraries(id, key, meta) VALUES(1, 'https://docs.example.test/', '{"key":"https://docs.example.test/","root":"https://docs.example.test/","format":3,"model":"org/m","dims":8,"vectors":1,"units":2}');
INSERT INTO pages(lib, url, hash, sections, windows, units) VALUES(1, 'https://docs.example.test/old', 'h', 1, 1, 2);
INSERT INTO sections(id, lib, page, ord, heading, text, units) VALUES(1, 1, 'https://docs.example.test/old', 0, 'Old', 'old text', '[{"kind":"prose","body":[0,3]}]');
INSERT INTO vectors(section, windows) VALUES(1, x'0000');`)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	s := Open(path)
	if lib, ok, err := s.Library(testKey); err != nil || !ok || !lib.Current() {
		t.Fatalf("library = %+v %v %v", lib, ok, err)
	}
	if err := s.PutPages(testKey, []PageData{{Page: Page{URL: testKey + "new", Hash: "n", Sections: 1}, Sections: []Section{{Order: 0, Heading: "New", Text: "new text"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePages(testKey, []string{testKey + "old"}); err != nil {
		t.Fatal(err)
	}
	if secs, err := s.Sections(testKey); err != nil || len(secs) != 1 || secs[0].Heading != "New" {
		t.Errorf("sections = %+v %v", secs, err)
	}
}
