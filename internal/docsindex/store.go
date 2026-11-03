// Package docsindex is the local docs index's storage (ADR-0007): one
// bbolt file, docs-index.db in ketch's cache directory, separate from the
// page cache and the tag index and opened for one transaction at a time
// (ADR-0006), so queries from MCP servers and the CLI share it with an
// indexing run.
//
// Each indexed docs root is a library: its metadata in the libraries
// bucket, and its pages, sections and section vectors in a bucket of its
// own. Vectors are float16 (embed.AppendHalf), one or more per section
// (a long section is embedded as several windows), and are only
// comparable with vectors of the model recorded in the library's
// metadata.
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
// in another format is rebuilt, never read (Library.Current).
const FormatVersion = 1

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
	Version string   `json:"version,omitempty"`
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
	Pages      int      `json:"pages"`
	Sections   int      `json:"sections"`
	Vectors    int      `json:"vectors"`
	Discovered int      `json:"discovered"`
	Capped     bool     `json:"capped,omitempty"`
	Failed     int      `json:"failed,omitempty"`
	Sources    []string `json:"sources,omitempty"`
}

// Current reports whether the library is in the format this code reads.
func (l Library) Current() bool { return l.Format == FormatVersion }

// Page is one indexed page.
type Page struct {
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Hash     string `json:"hash"` // of the page's title and Markdown, as fetched
	Sections int    `json:"sections"`
	Windows  int    `json:"windows,omitempty"` // vectors across its sections
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
	// Vectors are the section's windows' embeddings, in order; none when
	// the library has no model. Not part of the JSON record: stored apart.
	Vectors [][]float32 `json:"-"`
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
		pb, sb, vb, err := libBuckets(tx, key)
		if err != nil {
			return err
		}
		for _, pd := range pages {
			if err := deletePage(pb, sb, vb, pd.Page.URL); err != nil {
				return err
			}
			if err := putPage(pb, sb, vb, pd); err != nil {
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
		pb, sb, vb, err := libBuckets(tx, key)
		if err != nil {
			return err
		}
		for _, u := range urls {
			if err := deletePage(pb, sb, vb, u); err != nil {
				return err
			}
		}
		return nil
	})
}

// libBuckets returns (creating) a library's pages, sections and vectors.
func libBuckets(tx *bolt.Tx, key string) (pb, sb, vb *bolt.Bucket, err error) {
	lb, err := tx.CreateBucketIfNotExists(libBucket(key))
	if err != nil {
		return nil, nil, nil, err
	}
	if pb, err = lb.CreateBucketIfNotExists(pagesBucket); err != nil {
		return nil, nil, nil, err
	}
	if sb, err = lb.CreateBucketIfNotExists(sectionsBucket); err != nil {
		return nil, nil, nil, err
	}
	vb, err = lb.CreateBucketIfNotExists(vectorsBucket)
	return pb, sb, vb, err
}

func putPage(pb, sb, vb *bolt.Bucket, pd PageData) error {
	v, err := json.Marshal(pd.Page)
	if err != nil {
		return err
	}
	if err := pb.Put([]byte(pd.Page.URL), v); err != nil {
		return err
	}
	for _, sec := range pd.Sections {
		sec.Page = pd.Page.URL
		k := sectionKey(sec.Page, sec.Order)
		v, err := json.Marshal(sec)
		if err != nil {
			return err
		}
		if err := sb.Put(k, v); err != nil {
			return err
		}
		if len(sec.Vectors) > 0 {
			if err := vb.Put(k, encodeVectors(sec.Vectors)); err != nil {
				return err
			}
		}
	}
	return nil
}

// deletePage removes a page's record, sections and vectors.
func deletePage(pb, sb, vb *bolt.Bucket, url string) error {
	if err := pb.Delete([]byte(url)); err != nil {
		return err
	}
	prefix := append([]byte(url), 0)
	for _, b := range []*bolt.Bucket{sb, vb} {
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
// with its vectors (of dims dimensions each) when withVectors is set and
// the section has them.
func (s *Store) Sections(key string, dims int, withVectors bool) ([]Section, error) {
	var out []Section
	err := s.view(func(tx *bolt.Tx) error {
		lb := tx.Bucket(libBucket(key))
		if lb == nil || lb.Bucket(sectionsBucket) == nil {
			return nil
		}
		vb := lb.Bucket(vectorsBucket)
		return lb.Bucket(sectionsBucket).ForEach(func(k, v []byte) error {
			var sec Section
			if err := json.Unmarshal(v, &sec); err != nil {
				return err
			}
			if withVectors && vb != nil && dims > 0 {
				sec.Vectors = decodeVectors(vb.Get(k), dims)
			}
			out = append(out, sec)
			return nil
		})
	})
	// Keys are the page URL, a NUL and the big-endian order, so bbolt's
	// key order is page by page, sections in order.
	return out, err
}

// PutVectors replaces the vectors of the given sections (keyed by page
// URL and order), for re-embedding with another model.
func (s *Store) PutVectors(key string, secs []Section) error {
	return s.update(func(tx *bolt.Tx) error {
		_, _, vb, err := libBuckets(tx, key)
		if err != nil {
			return err
		}
		for _, sec := range secs {
			k := sectionKey(sec.Page, sec.Order)
			if len(sec.Vectors) == 0 {
				if err := vb.Delete(k); err != nil {
					return err
				}
				continue
			}
			if err := vb.Put(k, encodeVectors(sec.Vectors)); err != nil {
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
