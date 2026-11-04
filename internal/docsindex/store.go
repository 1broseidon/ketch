// Package docsindex is the local docs index's storage (ADR-0007): one
// bbolt file, docs-index.db in ketch's cache directory, separate from the
// page cache and the tag index and opened for one transaction at a time
// (ADR-0006), so queries from MCP servers and the CLI share it with an
// indexing run.
//
// Each indexed docs root is a library: its metadata in the libraries
// bucket, and its pages, sections, section vectors, snippet units and
// unit vectors in a bucket of its own. One library is one version of one
// library's docs (its root is that version's docs root), so several
// versions sit side by side under their own keys. Vectors are float16
// (embed.AppendHalf), one or more per section (a long section is embedded
// as several windows) and one per unit, and are only comparable with
// vectors of the model recorded in the library's metadata.
package docsindex

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/1broseidon/ketch/internal/boltfile"
	"github.com/1broseidon/ketch/internal/embed"
	bolt "go.etcd.io/bbolt"
)

// FormatVersion is the index layout this code writes. A library written
// in another format is rebuilt, never read (Library.Current): format 1
// had no snippet units, no cross-version labels and looser version
// scoping.
const FormatVersion = 2

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
	// length; "" and 0 for an index of text alone (no model installed).
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
	// library, newest last, each with its query vector (float16): a
	// question asked again, or in other words, is answered from here
	// without searching.
	Asked []Asked `json:"asked,omitempty"`
}

// Asked is one question answered from a library.
type Asked struct {
	Question string `json:"q"`
	Vector   []byte `json:"v,omitempty"` // float16, the library's Dims
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
}

// Section is one heading's worth of a page, as the web backend splits
// pages (never inside fenced code).
type Section struct {
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
	// model. Not part of the JSON record: stored apart.
	Vectors     [][]float32 `json:"-"`
	UnitVectors [][]float32 `json:"-"`
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

// Store is the index file at a path. It holds no handle: every method
// opens the file for one transaction.
type Store struct {
	path string
	// wait bounds how long an operation waits for another process's lock.
	wait time.Duration
}

// Lock waits: a query waits a second for an indexing run's write batch,
// as the page cache does; a build's writes wait longer, behind queries
// and other builds.
const (
	readWait  = time.Second
	writeWait = 10 * time.Second
)

// Open returns the store at path (nothing is opened yet).
func Open(path string) *Store { return &Store{path: path, wait: readWait} }

// DefaultPath is the index file: KETCH_DOCS_INDEX_PATH when set, else
// docs-index.db in ketch's cache directory.
func DefaultPath() (string, error) {
	if p := os.Getenv("KETCH_DOCS_INDEX_PATH"); p != "" {
		return p, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cache dir: %w", err)
	}
	return filepath.Join(base, "ketch", "docs-index.db"), nil
}

// Path is the store's file.
func (s *Store) Path() string { return s.path }

// Exists reports whether the file exists: a missing one holds nothing,
// and reading it costs a stat.
func (s *Store) Exists() bool {
	_, err := os.Stat(s.path)
	return err == nil
}

// Size is the file's size in bytes (0 when missing).
func (s *Store) Size() int64 {
	fi, err := os.Stat(s.path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

var (
	librariesBucket = []byte("libraries")
	pagesBucket     = []byte("pages")
	sectionsBucket  = []byte("sections")
	vectorsBucket   = []byte("vectors")
	unitVecsBucket  = []byte("unit-vectors")
)

// libBucket names a library's own bucket.
func libBucket(key string) []byte { return []byte("lib:" + key) }

// sectionKey orders a page's sections: the page URL, a NUL, the order.
func sectionKey(page string, order int) []byte {
	return binary.BigEndian.AppendUint32(append([]byte(page), 0), uint32(order))
}

func (s *Store) view(fn func(*bolt.Tx) error) error {
	return boltfile.Tx(s.path, "docs index", false, s.wait, nil, fn)
}

func (s *Store) update(fn func(*bolt.Tx) error) error {
	return boltfile.Tx(s.path, "docs index", true, writeWait, [][]byte{librariesBucket}, fn)
}

// Libraries lists every indexed library, by key.
func (s *Store) Libraries() ([]Library, error) {
	var out []Library
	err := s.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(librariesBucket)
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			var l Library
			if err := json.Unmarshal(v, &l); err != nil {
				return fmt.Errorf("docs index: library record: %w", err)
			}
			out = append(out, l)
			return nil
		})
	})
	return out, err
}

// Library returns the library at key.
func (s *Store) Library(key string) (Library, bool, error) {
	var l Library
	found := false
	err := s.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(librariesBucket)
		if b == nil {
			return nil
		}
		v := b.Get([]byte(key))
		if v == nil {
			return nil
		}
		found = true
		return json.Unmarshal(v, &l)
	})
	return l, found, err
}

// PutLibrary writes a library's metadata: the last write of a build, which
// makes it visible to queries.
func (s *Store) PutLibrary(l Library) error {
	v, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return s.update(func(tx *bolt.Tx) error { return tx.Bucket(librariesBucket).Put([]byte(l.Key), v) })
}

// Remove deletes a library and everything stored for it. Removing nothing
// is not an error; found says whether there was anything.
func (s *Store) Remove(key string) (found bool, err error) {
	if !s.Exists() {
		return false, nil
	}
	err = s.update(func(tx *bolt.Tx) error {
		if tx.Bucket(librariesBucket).Get([]byte(key)) != nil {
			found = true
			if err := tx.Bucket(librariesBucket).Delete([]byte(key)); err != nil {
				return err
			}
		}
		if tx.Bucket(libBucket(key)) != nil {
			found = true
			return tx.DeleteBucket(libBucket(key))
		}
		return nil
	})
	return found, err
}

// Pages returns a library's stored pages by URL.
func (s *Store) Pages(key string) (map[string]Page, error) {
	out := map[string]Page{}
	err := s.view(func(tx *bolt.Tx) error {
		lb := tx.Bucket(libBucket(key))
		if lb == nil || lb.Bucket(pagesBucket) == nil {
			return nil
		}
		return lb.Bucket(pagesBucket).ForEach(func(_, v []byte) error {
			var p Page
			if err := json.Unmarshal(v, &p); err != nil {
				return err
			}
			out[p.URL] = p
			return nil
		})
	})
	return out, err
}

// PutPages writes pages with their sections and vectors in one
// transaction, replacing whatever each page had.
func (s *Store) PutPages(key string, pages []PageData) error {
	if len(pages) == 0 {
		return nil
	}
	return s.update(func(tx *bolt.Tx) error {
		bs, err := libBuckets(tx, key)
		if err != nil {
			return err
		}
		for _, pd := range pages {
			if err := bs.deletePage(pd.Page.URL); err != nil {
				return err
			}
			if err := bs.putPage(pd); err != nil {
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
	return s.update(func(tx *bolt.Tx) error {
		bs, err := libBuckets(tx, key)
		if err != nil {
			return err
		}
		for _, u := range urls {
			if err := bs.deletePage(u); err != nil {
				return err
			}
		}
		return nil
	})
}

// buckets are a library's pages, sections, section vectors and unit
// vectors.
type buckets struct {
	pages, sections, vectors, unitVecs *bolt.Bucket
}

// libBuckets returns (creating) a library's buckets.
func libBuckets(tx *bolt.Tx, key string) (buckets, error) {
	lb, err := tx.CreateBucketIfNotExists(libBucket(key))
	if err != nil {
		return buckets{}, err
	}
	var bs buckets
	for _, b := range []struct {
		name []byte
		into **bolt.Bucket
	}{{pagesBucket, &bs.pages}, {sectionsBucket, &bs.sections}, {vectorsBucket, &bs.vectors}, {unitVecsBucket, &bs.unitVecs}} {
		if *b.into, err = lb.CreateBucketIfNotExists(b.name); err != nil {
			return buckets{}, err
		}
	}
	return bs, nil
}

func (bs buckets) putPage(pd PageData) error {
	v, err := json.Marshal(pd.Page)
	if err != nil {
		return err
	}
	if err := bs.pages.Put([]byte(pd.Page.URL), v); err != nil {
		return err
	}
	for _, sec := range pd.Sections {
		sec.Page = pd.Page.URL
		k := sectionKey(sec.Page, sec.Order)
		v, err := json.Marshal(sec)
		if err != nil {
			return err
		}
		if err := bs.sections.Put(k, v); err != nil {
			return err
		}
		if err := putVectors(bs.vectors, k, sec.Vectors); err != nil {
			return err
		}
		if err := putVectors(bs.unitVecs, k, sec.UnitVectors); err != nil {
			return err
		}
	}
	return nil
}

// putVectors stores vecs at k, or deletes what k held when there are none.
func putVectors(b *bolt.Bucket, k []byte, vecs [][]float32) error {
	if len(vecs) == 0 {
		return b.Delete(k)
	}
	return b.Put(k, encodeVectors(vecs))
}

// deletePage removes a page's record, sections and vectors.
func (bs buckets) deletePage(url string) error {
	if err := bs.pages.Delete([]byte(url)); err != nil {
		return err
	}
	prefix := append([]byte(url), 0)
	for _, b := range []*bolt.Bucket{bs.sections, bs.vectors, bs.unitVecs} {
		var keys [][]byte
		c := b.Cursor()
		for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
			keys = append(keys, bytes.Clone(k))
		}
		for _, k := range keys {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
	}
	return nil
}

// Sections returns every section of a library in page and section order,
// with its window and unit vectors (of dims dimensions each) when
// withVectors is set and the section has them.
func (s *Store) Sections(key string, dims int, withVectors bool) ([]Section, error) {
	var out []Section
	err := s.view(func(tx *bolt.Tx) error {
		lb := tx.Bucket(libBucket(key))
		if lb == nil || lb.Bucket(sectionsBucket) == nil {
			return nil
		}
		vb, ub := lb.Bucket(vectorsBucket), lb.Bucket(unitVecsBucket)
		return lb.Bucket(sectionsBucket).ForEach(func(k, v []byte) error {
			var sec Section
			if err := json.Unmarshal(v, &sec); err != nil {
				return err
			}
			if withVectors && dims > 0 {
				if vb != nil {
					sec.Vectors = decodeVectors(vb.Get(k), dims)
				}
				if ub != nil {
					sec.UnitVectors = decodeVectors(ub.Get(k), dims)
				}
			}
			out = append(out, sec)
			return nil
		})
	})
	// Keys are the page URL, a NUL and the big-endian order, so bbolt's
	// key order is page by page, sections in order.
	return out, err
}

// PageSections returns the sections of one page of a library, in order,
// without vectors: a prefix scan over the page's keys, so reading one
// section of a large index costs a cursor seek, not a pass over the
// library. It returns none for a page the library does not hold.
func (s *Store) PageSections(key, page string) ([]Section, error) {
	var out []Section
	err := s.view(func(tx *bolt.Tx) error {
		lb := tx.Bucket(libBucket(key))
		if lb == nil || lb.Bucket(sectionsBucket) == nil {
			return nil
		}
		prefix := append([]byte(page), 0)
		c := lb.Bucket(sectionsBucket).Cursor()
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			var sec Section
			if err := json.Unmarshal(v, &sec); err != nil {
				return err
			}
			out = append(out, sec)
		}
		return nil
	})
	return out, err
}

// PutVectors replaces the window and unit vectors of the given sections
// (keyed by page URL and order), for re-embedding with another model.
func (s *Store) PutVectors(key string, secs []Section) error {
	return s.update(func(tx *bolt.Tx) error {
		bs, err := libBuckets(tx, key)
		if err != nil {
			return err
		}
		for _, sec := range secs {
			k := sectionKey(sec.Page, sec.Order)
			if err := putVectors(bs.vectors, k, sec.Vectors); err != nil {
				return err
			}
			if err := putVectors(bs.unitVecs, k, sec.UnitVectors); err != nil {
				return err
			}
		}
		return nil
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
