package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/doctor"
	"github.com/1broseidon/ketch/internal/testutil"
)

// docs_backend is retired (design/docs-evidence.md, Build 1): a stored
// value, the old default's context7 or none, chooses nothing. Search is
// every call's backend; Context7 answers a call that asks for it.
func TestDocsBackendRetired(t *testing.T) {
	for _, stored := range []string{"context7", ""} {
		t.Run("stored="+stored, func(t *testing.T) {
			c := config.Defaults()
			c.DocsBackend = stored
			info := buildConfigInfo(c, "/fixture/config.json")
			if info.DocsBackend != docs.SearchBackend || info.DocsBackendNote != docs.RetiredDocsBackend {
				t.Errorf("config listing: docs_backend %q, note %q", info.DocsBackend, info.DocsBackendNote)
			}
			for _, p := range docs.Providers() {
				if want := p.ID == docs.SearchBackend; p.Required(&c) != want {
					t.Errorf("%s required = %t, want %t", p.ID, !want, want)
				}
			}
			prev := cfg
			cfg = c
			defer func() { cfg = prev }()
			for _, b := range []string{"search", "context7"} {
				label := doctorBackendLabel(doctor.Check{Surface: "docs", Backend: b})
				if want := b == "search"; strings.HasSuffix(label, "(default)") != want {
					t.Errorf("doctor label for %s = %q", b, label)
				}
			}
		})
	}
	if f := docsCmd.Flags().Lookup("backend"); f == nil || f.DefValue != docs.SearchBackend {
		t.Errorf("--backend default = %+v, want search", f)
	}
}

// config set docs_backend says the key is retired and how to ask for
// Context7, and leaves the file as it was.
func TestConfigSetDocsBackendRetired(t *testing.T) {
	path := filepath.Join(testutil.SetIsolatedConfigHome(t), "config.json")
	t.Setenv("KETCH_CONFIG", path)
	original := []byte(`{"limit":5,"docs_backend":"context7"}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"context7", "search"} {
		err := runConfigSet(nil, []string{"docs_backend", v})
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitValidation || !strings.Contains(err.Error(), "docs_backend is retired") || !strings.Contains(err.Error(), "-b context7") {
			t.Fatalf("set docs_backend %s: %v, want the retirement note", v, err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("config set rewrote the file: %s", got)
	}
	loaded, err := config.LoadFile()
	if err != nil || loaded.DocsBackend != "context7" {
		t.Fatalf("stored value = %q (%v), want it left as written", loaded.DocsBackend, err)
	}
}

// An answer from pages a search read prints what it read and its origin
// only when it says something: verified, with its evidence. An unverified
// origin on an outcome that names no library (the request named none) is
// not printed, and nothing tells the caller how to present it: no hint
// about the origin follows the body.
func TestPrintDocsResultsOrigin(t *testing.T) {
	o := docs.Outcome{Status: docs.Found, Read: []docs.PageRead{{URL: "https://tips.example.test/a", Rank: 1}},
		Origin:  "unverified, ketch could not confirm tips.example.test as the official documentation of lib",
		Results: []docs.Result{{Library: "https://tips.example.test/", PageTitle: "Tips", URL: "https://tips.example.test/a", Snippet: "Text."}}}
	out := captureStdout(t, func() {
		printDocsResults(os.Stdout, "pool", docs.SearchBackend, "", o, false, docs.HintCLI)
	})
	if !strings.Contains(out, "\nread:\n  - https://tips.example.test/a (search rank 1)\nnot_on_pages: []\n---\n") || strings.Contains(out, "origin") ||
		!strings.HasSuffix(out, "source: https://tips.example.test/a\n") || strings.Contains(out, "status") {
		t.Errorf("output:\n%s", out)
	}
	o.Origin = "verified, npm lib lists it as Documentation (resolve, trust 8.0)"
	out = captureStdout(t, func() {
		printDocsResults(os.Stdout, "pool", docs.SearchBackend, "lib", o, false, docs.HintCLI)
	})
	if !strings.Contains(out, "\norigin: verified, npm lib lists it as Documentation (resolve, trust 8.0)\n") || strings.Contains(out, "origin unverified") || strings.Contains(out, "origin by name") {
		t.Errorf("verified output:\n%s", out)
	}
}
