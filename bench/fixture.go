package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
)

var validID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func digest(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func readJSON(path string, value any) ([]byte, error) {
	data, err := readBounded(path, 32<<20)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("%s: trailing JSON data", path)
	}
	return data, nil
}

func loadCorpus(dir string) ([]fixture, string, error) {
	return loadCorpusWithin(dir, maxCorpusBytes)
}

func loadCorpusWithin(dir string, budget int) ([]fixture, string, error) {
	var c corpus
	data, err := readJSON(filepath.Join(dir, "corpus.json"), &c)
	if err != nil {
		return nil, "", err
	}
	if c.Version != schemaVersion || len(c.Pages) == 0 {
		return nil, "", fmt.Errorf("unsupported or empty corpus")
	}
	seen := make(map[string]bool)
	sources := make(map[string]bool)
	contents := make(map[string]bool)
	fixtures := make([]fixture, 0, len(c.Pages))
	for _, spec := range c.Pages {
		if err := validateSpec(spec, seen); err != nil {
			return nil, "", err
		}
		if err := uniqueSource(spec, sources, contents); err != nil {
			return nil, "", err
		}
		f, err := readFixture(dir, spec)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", fmt.Errorf("%s: %w; run `go -C bench run . setup` to fetch the corpus archive", spec.ID, err)
		}
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", spec.ID, err)
		}
		budget -= len(f.HTML) + len(f.Reference)
		if budget < 0 {
			return nil, "", fmt.Errorf("corpus exceeds aggregate HTML/reference byte limit")
		}
		fixtures = append(fixtures, fixture{Spec: spec, Dir: dir, ReferenceWords: len(words(f.Reference))})
	}
	return fixtures, digest(data), nil
}

func uniqueSource(s pageSpec, sources, contents map[string]bool) error {
	u, err := url.Parse(s.FinalURL)
	if err != nil {
		return err
	}
	u.Fragment = ""
	key := u.String()
	if sources[key] || contents[s.HTMLSHA] {
		return fmt.Errorf("%s: duplicate final URL or HTML snapshot", s.ID)
	}
	sources[key], contents[s.HTMLSHA] = true, true
	return nil
}

func validateSpec(s pageSpec, seen map[string]bool) error {
	if !validID.MatchString(s.ID) || seen[s.ID] {
		return fmt.Errorf("invalid or duplicate page id %q", s.ID)
	}
	seen[s.ID] = true
	switch s.Cohort {
	case "", "regression", "expansion", "heldout", "scale", "scale-new-sites":
	default:
		return fmt.Errorf("%s: unsupported cohort %q", s.ID, s.Cohort)
	}
	if s.Site == "" || s.Kind == "" || s.Review == "" || len(s.Checks) == 0 {
		return fmt.Errorf("%s: missing site, kind, annotation notes or checks", s.ID)
	}
	if err := validateSourceMetadata(s); err != nil {
		return err
	}
	return validateAssertions(s.Checks)
}

func validateSourceMetadata(s pageSpec) error {
	for _, raw := range []string{s.URL, s.FinalURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return fmt.Errorf("%s: invalid HTTP URL %q", s.ID, raw)
		}
	}
	if _, err := cascadia.ParseGroup(s.Selector); err != nil {
		return fmt.Errorf("%s: invalid reference selector: %w", s.ID, err)
	}
	if s.Exclude != "" {
		if _, err := cascadia.ParseGroup(s.Exclude); err != nil {
			return fmt.Errorf("%s: invalid reference exclusions: %w", s.ID, err)
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, s.CapturedAt); err != nil {
		return fmt.Errorf("%s: invalid capture timestamp: %w", s.ID, err)
	}
	return nil
}

func validateAssertions(checks []assertion) error {
	seen := make(map[string]bool)
	for _, a := range checks {
		if a.ID == "" || seen[a.ID] {
			return fmt.Errorf("empty or duplicate assertion id %q", a.ID)
		}
		seen[a.ID] = true
		switch a.Kind {
		case "text", "absent", "heading", "code", "link":
			if strings.TrimSpace(a.Text) == "" {
				return fmt.Errorf("%s: empty assertion text", a.ID)
			}
		case "table":
			if len(a.Headers) < 2 || len(a.Headers) != len(a.Cells) {
				return fmt.Errorf("%s: table needs matching headers and cells", a.ID)
			}
		default:
			return fmt.Errorf("%s: unsupported assertion kind %q", a.ID, a.Kind)
		}
	}
	return nil
}

func readFixture(dir string, s pageSpec) (fixture, error) {
	packed, err := os.Open(filepath.Join(dir, "testdata", s.ID+".html.gz"))
	if err != nil {
		return fixture{}, err
	}
	defer func() { _ = packed.Close() }()
	reader, err := gzip.NewReader(io.LimitReader(packed, 32<<20))
	if err != nil {
		return fixture{}, err
	}
	defer func() { _ = reader.Close() }()
	html, err := io.ReadAll(io.LimitReader(reader, maxInputBytes+1))
	if err != nil || len(html) > maxInputBytes {
		return fixture{}, fmt.Errorf("invalid or oversized HTML snapshot: %v", err)
	}
	ref, err := readBounded(filepath.Join(dir, "testdata", s.ID+".txt"), maxInputBytes)
	if err != nil {
		return fixture{}, err
	}
	if digest(html) != s.HTMLSHA || digest(ref) != s.ReferenceSHA {
		return fixture{}, fmt.Errorf("snapshot/reference checksum mismatch; review changes before updating corpus.json")
	}
	if len(words(string(ref))) == 0 {
		return fixture{}, fmt.Errorf("empty reference text")
	}
	return fixture{Spec: s, HTML: html, Reference: string(ref)}, nil
}

func readBounded(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", path, limit)
	}
	return data, nil
}

func materialize(f fixture) (fixture, error) {
	if f.Dir == "" {
		return f, nil
	}
	return readFixture(f.Dir, f.Spec)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
