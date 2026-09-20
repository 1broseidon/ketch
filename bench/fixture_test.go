package main

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLazyFixturesRecheckChangedSources(t *testing.T) {
	t.Parallel()
	dir, c := testCorpus(t)
	if err := writeJSON(filepath.Join(dir, "corpus.json"), c); err != nil {
		t.Fatal(err)
	}
	fixtures, _, err := loadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := fixtures[0]
	if f.Dir == "" || f.HTML != nil || f.Reference != "" || f.ReferenceWords != 2 {
		t.Fatalf("expected metadata-only fixture with reference word count: %+v", f)
	}
	if err := validateReference(f); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "testdata", "sample.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateReference(f); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("source validation accepted changed reference: %v", err)
	}
	r := runPage(context.Background(), "must-not-be-invoked", nil, f, options{})
	if len(r.Errors) != 1 || !strings.Contains(r.Errors[0], "checksum") || r.Accuracy.ReferenceWords != 2 || r.Accuracy.Checks[0].Passed {
		t.Fatalf("lazy read failure must retain page and failed-check denominator: %+v", r)
	}
}

func TestBoundedReferenceReads(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "reference.txt")
	if err := os.WriteFile(path, []byte("four"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, err := readBounded(path, 4); err != nil || string(data) != "four" {
		t.Fatalf("exact bound: %q, %v", data, err)
	}
	if _, err := readBounded(path, 3); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized reference accepted: %v", err)
	}
}

func TestPinnedCorpus(t *testing.T) {
	t.Parallel()
	fixtures, _, err := loadCorpus(".")
	if err != nil {
		t.Fatal(err)
	}
	sites := make(map[string]bool)
	cohorts := make(map[string]int)
	for _, f := range fixtures {
		u, _ := url.Parse(f.Spec.FinalURL)
		if f.Spec.Site != u.Hostname() {
			t.Fatalf("pinned site label must match actual final host: %s", f.Spec.ID)
		}
		sites[f.Spec.Site] = true
		cohorts[f.Spec.Cohort]++
	}
	if len(fixtures) < 500 || len(sites) < 70 {
		t.Fatalf("stress corpus needs at least 500 pages on 70 sites, got %d/%d", len(fixtures), len(sites))
	}
	for name, minimum := range map[string]int{"regression": 20, "expansion": 60, "heldout": 20, "scale": 320, "scale-new-sites": 80} {
		if cohorts[name] < minimum {
			t.Fatalf("missing corpus coverage: %v", cohorts)
		}
	}
	assertEvaluationSites(t, fixtures)
	if err := validateReferences(fixtures); err != nil {
		t.Fatal(err)
	}
}

func assertEvaluationSites(t *testing.T, fixtures []fixture) {
	t.Helper()
	developmentSites := make(map[string]bool)
	originalSites := make(map[string]bool)
	for _, f := range fixtures {
		if f.Spec.Cohort == "regression" || f.Spec.Cohort == "expansion" {
			developmentSites[f.Spec.Site] = true
		}
		if f.Spec.Cohort != "scale" && f.Spec.Cohort != "scale-new-sites" {
			originalSites[f.Spec.Site] = true
		}
	}
	for _, f := range fixtures {
		if f.Spec.Cohort == "heldout" && developmentSites[f.Spec.Site] {
			t.Fatalf("held-out site overlaps development corpus: %s", f.Spec.Site)
		}
		if f.Spec.Cohort == "scale-new-sites" && originalSites[f.Spec.Site] {
			t.Fatalf("new evaluation site overlaps original corpus: %s", f.Spec.Site)
		}
	}
}

func TestOriginal100Preserved(t *testing.T) {
	t.Parallel()
	var original, current corpus
	if _, err := readJSON("reports/initial100-corpus.json", &original); err != nil {
		t.Fatal(err)
	}
	if _, err := readJSON("corpus.json", &current); err != nil {
		t.Fatal(err)
	}
	if len(original.Pages) != 100 || len(current.Pages) < 100 || !reflect.DeepEqual(current.Pages[:100], original.Pages) {
		t.Fatal("original 100 source snapshots, reference hashes, assertions and cohorts must stay unchanged")
	}
}

func testCorpus(t *testing.T) (string, corpus) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "testdata"), 0o755); err != nil {
		t.Fatal(err)
	}
	body, ref := []byte("<main><h1>Expected content</h1></main>"), []byte("Expected content\n")
	spec := pageSpec{ID: "sample", Site: "example.test", Kind: "test", URL: "https://example.test", FinalURL: "https://example.test",
		CapturedAt: "2026-09-18T12:00:00Z", Selector: "main", Review: "Test source", HTMLSHA: digest(body), ReferenceSHA: digest(ref),
		Checks: []assertion{{ID: "fact", Kind: "text", Text: "Expected content"}}}
	if err := writeSnapshot(filepath.Join(dir, "testdata", "sample.html.gz"), body); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "testdata", "sample.txt"), ref, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, corpus{Version: schemaVersion, Pages: []pageSpec{spec}}
}

func TestCorpusRejectsDamageAndAmbiguity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		edit func(*corpus)
		want string
	}{
		{"checksum", func(c *corpus) { c.Pages[0].HTMLSHA = "wrong" }, "checksum"},
		{"reference changed", func(c *corpus) { c.Pages[0].ReferenceSHA = "wrong" }, "checksum"},
		{"path traversal", func(c *corpus) { c.Pages[0].ID = "../file" }, "invalid"},
		{"duplicate page", func(c *corpus) { c.Pages = append(c.Pages, c.Pages[0]) }, "duplicate"},
		{"duplicate check", func(c *corpus) { c.Pages[0].Checks = append(c.Pages[0].Checks, c.Pages[0].Checks[0]) }, "duplicate"},
		{"unsupported check", func(c *corpus) { c.Pages[0].Checks[0].Kind = "made-up" }, "unsupported"},
		{"unsupported cohort", func(c *corpus) { c.Pages[0].Cohort = "typo" }, "cohort"},
		{"same URL different fragment", func(c *corpus) {
			p := c.Pages[0]
			p.ID, p.FinalURL, p.HTMLSHA = "other", p.FinalURL+"#section", "different"
			c.Pages = append(c.Pages, p)
		}, "duplicate final URL"},
		{"same content different URL", func(c *corpus) {
			p := c.Pages[0]
			p.ID, p.FinalURL = "other", "https://other.test"
			c.Pages = append(c.Pages, p)
		}, "duplicate final URL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, c := testCorpus(t)
			tc.edit(&c)
			if err := writeJSON(filepath.Join(dir, "corpus.json"), c); err != nil {
				t.Fatal(err)
			}
			_, _, err := loadCorpus(dir)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %s error, got %v", tc.want, err)
			}
		})
	}
}

func TestAggregateCorpusBudget(t *testing.T) {
	t.Parallel()
	dir, c := testCorpus(t)
	if err := writeJSON(filepath.Join(dir, "corpus.json"), c); err != nil {
		t.Fatal(err)
	}
	f, err := readFixture(dir, c.Pages[0])
	if err != nil {
		t.Fatal(err)
	}
	size := len(f.HTML) + len(f.Reference)
	if _, _, err := loadCorpusWithin(dir, size); err != nil {
		t.Fatalf("exact payload budget should fit: %v", err)
	}
	if _, _, err := loadCorpusWithin(dir, size-1); err == nil || !strings.Contains(err.Error(), "aggregate") {
		t.Fatalf("over-budget corpus accepted: %v", err)
	}
}

func TestSourceAnnotationsRejectInventedFactsAndContradictoryNoise(t *testing.T) {
	t.Parallel()
	f := fixture{HTML: []byte("<main><p>Real content</p></main><footer>Subscribe now</footer>"), Reference: "Real content",
		Spec: pageSpec{ID: "test", FinalURL: "https://example.test", Selector: "main"}}
	cases := []assertion{
		{ID: "unsupported", Kind: "text", Text: "Invented fact"},
		{ID: "contradiction", Kind: "absent", Text: "Real content"},
		{ID: "phantom noise", Kind: "absent", Text: "Never appeared on source"},
		{ID: "link", Kind: "link", Text: "https://example.test/no-source-link"},
	}
	for _, a := range cases {
		f.Spec.Checks = []assertion{a}
		if err := validateReference(f); err == nil {
			t.Errorf("bad annotation accepted: %s", a.ID)
		}
	}
	f.Spec.Checks = []assertion{{ID: "valid noise", Kind: "absent", Text: "Subscribe now"}}
	if err := validateReference(f); err != nil {
		t.Fatal(err)
	}
}

func TestSourceEvidenceHonorsExclusions(t *testing.T) {
	t.Parallel()
	f := fixture{HTML: []byte(`<main><p>Real content</p><aside class="signup">Join our mailing list</aside></main>`),
		Reference: "Real content Join our mailing list",
		Spec: pageSpec{ID: "scope", FinalURL: "https://example.test", Selector: "main", Exclude: ".signup",
			Checks: []assertion{{ID: "contaminated", Kind: "text", Text: "Join our mailing list"}}}}
	if err := validateReference(f); err == nil {
		t.Fatal("positive assertion outside the declared content scope was accepted")
	}
	f.Reference = "Real content"
	f.Spec.Checks = []assertion{{ID: "noise", Kind: "absent", Text: "Join our mailing list"}}
	if err := validateReference(f); err != nil {
		t.Fatalf("excluded noise must remain provable against the original source: %v", err)
	}
}

func TestManifestRejectsUnknownOrTrailingJSON(t *testing.T) {
	t.Parallel()
	for _, input := range []string{`{"version":1,"typo":true}`, `{"version":1} {"version":2}`} {
		path := filepath.Join(t.TempDir(), "input.json")
		if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}
		var c corpus
		if _, err := readJSON(path, &c); err == nil {
			t.Fatalf("ambiguous JSON accepted: %s", input)
		}
	}
}

func TestOverlappingReferenceRootsRejected(t *testing.T) {
	t.Parallel()
	f := fixture{HTML: []byte("<main><article><p>Real content</p></article></main>"), Reference: "Real content",
		Spec: pageSpec{ID: "test", FinalURL: "https://example.test", Selector: "main,article"}}
	if err := validateReference(f); err == nil || !strings.Contains(err.Error(), "overlapping") {
		t.Fatalf("overlapping source roots accepted: %v", err)
	}
}
