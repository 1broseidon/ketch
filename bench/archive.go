package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// archiveSpec is bench/archive.json: where the pinned corpus files live. The
// 500 snapshots and references are a 22 MB tarball published outside this
// repository; testdata/ is gitignored and `setup` fetches it. corpus.json
// still pins every file by hash, so the archive cannot drift unnoticed.
type archiveSpec struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Files  int    `json:"files"`
}

const maxArchiveBytes = 256 << 20

var rxFixtureName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*\.(html\.gz|txt)$`)

// ensureTestdata leaves testdata/ complete: a no-op when every corpus file is
// there, otherwise a download (or a local tarball when override is a path),
// checksum, and extraction.
func ensureTestdata(ctx context.Context, dir, override string) error {
	var c corpus
	if _, err := readJSON(filepath.Join(dir, "corpus.json"), &c); err != nil {
		return err
	}
	testdata := filepath.Join(dir, "testdata")
	if len(missingFixtures(dir, c)) == 0 {
		fmt.Printf("Corpus files present in %s\n", testdata)
		return nil
	}
	var spec archiveSpec
	if _, err := readJSON(filepath.Join(dir, "archive.json"), &spec); err != nil {
		return fmt.Errorf("testdata is incomplete and archive.json is unreadable: %w", err)
	}
	if override != "" {
		spec.URL = override
	}
	if spec.URL == "" || spec.SHA256 == "" {
		return errors.New("archive.json must name the archive url and sha256")
	}
	fmt.Printf("Fetching corpus archive %s\n", spec.URL)
	data, err := readArchive(ctx, spec.URL)
	if err != nil {
		return err
	}
	if got := digest(data); got != spec.SHA256 {
		return fmt.Errorf("archive checksum mismatch: got %s, want %s", got, spec.SHA256)
	}
	n, err := extractArchive(bytes.NewReader(data), testdata)
	if err != nil {
		return err
	}
	if still := missingFixtures(dir, c); len(still) > 0 {
		return fmt.Errorf("archive lacks %d corpus files (first: %s)", len(still), still[0])
	}
	fmt.Printf("Extracted %d files into %s\n", n, testdata)
	return nil
}

func missingFixtures(dir string, c corpus) []string {
	var missing []string
	for _, s := range c.Pages {
		for _, name := range []string{s.ID + ".html.gz", s.ID + ".txt"} {
			if _, err := os.Stat(filepath.Join(dir, "testdata", name)); err != nil {
				missing = append(missing, name)
			}
		}
	}
	return missing
}

func readArchive(ctx context.Context, source string) ([]byte, error) {
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		return readBounded(source, maxArchiveBytes)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch archive: %s returned %s", source, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArchiveBytes {
		return nil, fmt.Errorf("archive exceeds %d bytes", maxArchiveBytes)
	}
	return data, nil
}

// extractArchive unpacks a gzip tarball of flat fixture files into dest,
// replacing whatever was there. Only regular files with fixture names are
// accepted: no directories, links, or paths.
func extractArchive(r io.Reader, dest string) (int, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("archive is not gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	partial := dest + ".partial"
	if err := os.RemoveAll(partial); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(partial, 0o755); err != nil {
		return 0, err
	}
	count := 0
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("read archive: %w", err)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		name := strings.TrimPrefix(path.Clean(hdr.Name), "./")
		if hdr.Typeflag != tar.TypeReg || !rxFixtureName.MatchString(name) {
			return 0, fmt.Errorf("unexpected archive entry %q", hdr.Name)
		}
		if hdr.Size > maxInputBytes {
			return 0, fmt.Errorf("%s exceeds %d bytes", name, maxInputBytes)
		}
		if err := writeEntry(filepath.Join(partial, name), io.LimitReader(tr, hdr.Size)); err != nil {
			return 0, err
		}
		count++
	}
	if err := os.RemoveAll(dest); err != nil {
		return 0, err
	}
	if err := os.Rename(partial, dest); err != nil {
		return 0, err
	}
	return count, nil
}

func writeEntry(name string, r io.Reader) error {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
