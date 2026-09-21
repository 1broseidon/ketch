package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tarEntry struct {
	name string
	body []byte
	flag byte
}

func packArchive(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		flag := e.flag
		if flag == 0 {
			flag = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: flag}
		if flag == tar.TypeSymlink {
			hdr.Linkname, hdr.Size = "/etc/passwd", 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if flag == tar.TypeReg {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractArchiveAcceptsOnlyFlatFixtureFiles(t *testing.T) {
	t.Parallel()
	good := packArchive(t, []tarEntry{{name: "./", flag: tar.TypeDir}, {name: "./a.txt", body: []byte("a")}, {name: "a.html.gz", body: []byte("z")}})
	dest := filepath.Join(t.TempDir(), "testdata")
	n, err := extractArchive(bytes.NewReader(good), dest)
	if err != nil || n != 2 {
		t.Fatalf("extract: n=%d err=%v", n, err)
	}
	for _, name := range []string{"a.txt", "a.html.gz"} {
		if _, err := os.Stat(filepath.Join(dest, name)); err != nil {
			t.Fatal(err)
		}
	}
	for name, entries := range map[string][]tarEntry{
		"traversal": {{name: "../evil.txt", body: []byte("x")}},
		"subdir":    {{name: "sub/a.txt", body: []byte("x")}},
		"absolute":  {{name: "/tmp/a.txt", body: []byte("x")}},
		"symlink":   {{name: "a.txt", flag: tar.TypeSymlink}},
		"stray":     {{name: "README.md", body: []byte("x")}},
	} {
		if _, err := extractArchive(bytes.NewReader(packArchive(t, entries)), filepath.Join(t.TempDir(), "testdata")); err == nil || !strings.Contains(err.Error(), "unexpected archive entry") {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	if _, err := extractArchive(bytes.NewReader([]byte("not gzip")), filepath.Join(t.TempDir(), "testdata")); err == nil {
		t.Fatal("plain bytes accepted as an archive")
	}
}

// setup restores a missing testdata/ from the archive named in archive.json
// (a local path here), refuses a checksum mismatch, and is a no-op once the
// files are back.
func TestSetupFetchesTheCorpusArchive(t *testing.T) {
	t.Parallel()
	dir, c := testCorpus(t)
	if err := writeJSON(filepath.Join(dir, "corpus.json"), c); err != nil {
		t.Fatal(err)
	}
	var entries []tarEntry
	for _, name := range []string{"sample.html.gz", "sample.txt"} {
		body, err := os.ReadFile(filepath.Join(dir, "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, tarEntry{name: name, body: body})
	}
	archive := packArchive(t, entries)
	tarball := filepath.Join(dir, "corpus.tar.gz")
	if err := os.WriteFile(tarball, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "testdata")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadCorpus(dir); err == nil || !strings.Contains(err.Error(), "setup") {
		t.Fatalf("missing fixtures should point at setup: %v", err)
	}
	write := func(sha string) {
		spec, _ := json.Marshal(archiveSpec{URL: tarball, SHA256: sha, Files: 2})
		if err := os.WriteFile(filepath.Join(dir, "archive.json"), spec, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("0000")
	if err := ensureTestdata(context.Background(), dir, ""); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("wrong sha accepted: %v", err)
	}
	write(digest(archive))
	if err := ensureTestdata(context.Background(), dir, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadCorpus(dir); err != nil {
		t.Fatal(err)
	}
	// Present files win over an unreachable archive.
	write("0000")
	if err := ensureTestdata(context.Background(), dir, "https://127.0.0.1:1/none.tar.gz"); err != nil {
		t.Fatal(err)
	}
}
