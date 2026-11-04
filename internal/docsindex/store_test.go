package docsindex

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/1broseidon/ketch/internal/embed"
	bolt "go.etcd.io/bbolt"
)

func unit(dims int, hot int) []float32 {
	v := make([]float32, dims)
	v[hot%dims] = 1
	return v
}

const testKey = "https://docs.example.test/"

// seed writes two pages, one with two sections (three vector windows),
// and the library's metadata.
func seed(t *testing.T, s *Store) []PageData {
	t.Helper()
	key := testKey
	pages := []PageData{
		{Page: Page{URL: key + "b", Title: "B", Hash: "h-b"}, Sections: []Section{
			{Order: 0, Heading: "B", Crumbs: []string{"B"}, Text: "intro of b", Vectors: [][]float32{unit(8, 0)}},
			{Order: 1, Anchor: "two", Heading: "Two", Crumbs: []string{"B", "Two"}, Text: "second", Vectors: [][]float32{unit(8, 1), unit(8, 2)}},
		}},
		{Page: Page{URL: key + "a", Title: "A", Hash: "h-a"}, Sections: []Section{{Order: 0, Heading: "A", Text: "only a"}}},
	}
	if err := s.PutPages(key, pages); err != nil {
		t.Fatal(err)
	}
	lib := Library{Key: key, Library: "example", Names: []string{"example"}, Root: key, Model: "org/m", Dims: 8, Format: FormatVersion, Built: time.Now().UTC(), Pages: 2, Sections: 3}
	if err := s.PutLibrary(lib); err != nil {
		t.Fatal(err)
	}
	return pages
}

// A library round-trips: metadata, pages, sections in page order, and
// float16 vectors, several windows per section.
func TestStoreRoundTrip(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "docs-index.db"))
	if libs, err := s.Libraries(); err != nil || len(libs) != 0 || s.Exists() {
		t.Fatalf("a missing file is an empty index: %v %v", libs, err)
	}
	seed(t, s)
	if got, ok, err := s.Library(testKey); err != nil || !ok || got.Model != "org/m" || !got.Current() {
		t.Fatalf("Library = %+v %v %v", got, ok, err)
	}
	secs, err := s.Sections(testKey, 8, true)
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
	if len(secs[2].Vectors) != 2 || embed.Dot(secs[2].Vectors[1], unit(8, 2)) != 1 || secs[0].Vectors != nil {
		t.Errorf("vectors = %v / %v", secs[2].Vectors, secs[0].Vectors)
	}
	if noVec, _ := s.Sections(testKey, 8, false); noVec[1].Vectors != nil {
		t.Error("vectors read when not asked for")
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
	if secs, _ := s.Sections(testKey, 8, true); len(secs) != 2 {
		t.Errorf("a rewritten page kept its old sections: %d", len(secs))
	}
	if ps, _ := s.Pages(testKey); ps[testKey+"b"].Hash != "h-b2" || len(ps) != 2 {
		t.Errorf("pages = %+v", ps)
	}
	if err := s.DeletePages(testKey, []string{testKey + "a"}); err != nil {
		t.Fatal(err)
	}
	if secs, _ := s.Sections(testKey, 8, true); len(secs) != 1 || secs[0].Page != testKey+"b" {
		t.Errorf("after delete = %+v", secs)
	}
}

// Re-embedding replaces a library's vectors in place, and Remove takes
// the library and all it stored; removing nothing reports so.
func TestStoreVectorsAndRemove(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "docs-index.db"))
	const key = "https://x.test/docs/"
	if err := s.PutPages(key, []PageData{{Page: Page{URL: key}, Sections: []Section{{Order: 0, Heading: "X", Text: "x", Vectors: [][]float32{unit(4, 0)}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutLibrary(Library{Key: key, Root: key, Format: FormatVersion}); err != nil {
		t.Fatal(err)
	}
	secs, _ := s.Sections(key, 4, true)
	secs[0].Vectors = [][]float32{unit(4, 3), unit(4, 1)}
	if err := s.PutVectors(key, secs); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Sections(key, 4, true); len(again[0].Vectors) != 2 || again[0].Vectors[0][3] != 1 {
		t.Errorf("re-embedded vectors = %v", again[0].Vectors)
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
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("the file stayed locked after the write: %v", err)
	}
	_ = db.Close()
}

// PageSections reads one page's sections in order, and none for a page the
// library lacks or one whose URL only starts like a stored page's.
func TestStorePageSections(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "docs-index.db"))
	seed(t, s)
	secs, err := s.PageSections(testKey, testKey+"b")
	if err != nil || len(secs) != 2 || secs[0].Heading != "B" || secs[1].Anchor != "two" || secs[1].Vectors != nil {
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
