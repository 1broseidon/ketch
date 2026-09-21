package cmd

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/search"
	"github.com/spf13/cobra"
)

// newTagFlagCmd builds a command carrying the flags validateTagFlag inspects.
// withNoCache mirrors scrape/crawl; without it mirrors search, which has no
// --no-cache flag at all — the validator must cope with both.
func newTagFlagCmd(withNoCache bool) *cobra.Command {
	c := &cobra.Command{Use: "fake"}
	c.Flags().String("tag", "", "")
	if withNoCache {
		c.Flags().Bool("no-cache", false, "")
	}
	return c
}

func TestValidateTagFlag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		withNoCache bool
		args        []string
		wantCode    int
		wantMsg     string
	}{
		{name: "no tag is fine", withNoCache: true},
		{name: "plain tag", withNoCache: true, args: []string{"--tag", "guacamole"}},
		{name: "tag on a command without --no-cache", args: []string{"--tag", "guacamole"}},
		{
			name: "tag with a slash or dot is allowed", withNoCache: true,
			args: []string{"--tag", "proj/auth.v2"},
		},
		{
			name: "tag with no-cache is allowed", withNoCache: true,
			args: []string{"--tag", "guacamole", "--no-cache"},
		},
		{
			name: "control characters rejected", withNoCache: true,
			args:     []string{"--tag", "a\tb"},
			wantCode: ExitValidation, wantMsg: "control characters",
		},
		{
			name: "padded name rejected", withNoCache: true,
			args:     []string{"--tag", " guacamole "},
			wantCode: ExitValidation, wantMsg: "empty or padded",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newTagFlagCmd(tc.withNoCache)
			if err := c.ParseFlags(tc.args); err != nil {
				t.Fatalf("ParseFlags(%q): %v", tc.args, err)
			}
			err := validateTagFlag(c, nil)
			if tc.wantCode == 0 {
				if err != nil {
					t.Fatalf("validateTagFlag(%q) = %v, want nil", tc.args, err)
				}
				return
			}
			var exitErr *ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("validateTagFlag(%q) = %v, want an ExitError", tc.args, err)
			}
			if exitErr.Code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", exitErr.Code, tc.wantCode)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantMsg)
			}
		})
	}
}

// A nil *tagWriter is the no-tag case, and every fetch path calls record
// unconditionally — so it must be safe on nil, both receiver and page.
func TestTagWriterNilIsANoOp(t *testing.T) {
	t.Parallel()
	if tw := newTagWriter("", nil); tw != nil {
		t.Errorf("newTagWriter with no tag = %v, want nil", tw)
	}
	var nilWriter *tagWriter
	nilWriter.record(nil, "https://example.com", nil) // must not panic
	nilWriter.Close()                                 // must not panic
}

// The taggable mappers are what make a tag readable when the entry will never
// have a fetched body: the title has to say where the hit was, and the
// description has to carry the snippet the caller actually saw.
func TestCodeTaggable(t *testing.T) {
	t.Parallel()
	got := codeTaggable([]code.Result{
		{Repo: "apache/guacamole-client", Path: "src/ldap.java", Line: 42, Snippet: "bind(dn)", URL: "https://example.com/a"},
		{Repo: "apache/guacamole-server", Snippet: "guac_client_init", URL: "https://example.com/b"},
	})
	want := []taggableResult{
		{URL: "https://example.com/a", Title: "apache/guacamole-client src/ldap.java:42", Description: "bind(dn)"},
		{URL: "https://example.com/b", Title: "apache/guacamole-server", Description: "guac_client_init"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("codeTaggable() = %+v, want %+v", got, want)
	}
}

func TestDocsTaggable(t *testing.T) {
	t.Parallel()
	got := docsTaggable([]docs.Result{
		{Library: "/apache/guacamole", Title: "LDAP authentication", Snippet: "Set ldap-hostname", URL: "https://example.com/ldap"},
		{Title: "Untitled chunk", Snippet: "body", URL: "https://example.com/x"},
	})
	want := []taggableResult{
		{URL: "https://example.com/ldap", Title: "/apache/guacamole LDAP authentication", Description: "Set ldap-hostname"},
		{URL: "https://example.com/x", Title: "Untitled chunk", Description: "body"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("docsTaggable() = %+v, want %+v", got, want)
	}
}

func TestSearchTaggable(t *testing.T) {
	t.Parallel()
	got := searchTaggable([]search.Result{
		{URL: "https://example.com/a", Title: "Guacamole", Description: "A clientless remote desktop gateway."},
	})
	want := []taggableResult{
		{URL: "https://example.com/a", Title: "Guacamole", Description: "A clientless remote desktop gateway."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("searchTaggable() = %+v, want %+v", got, want)
	}
}

// Without --tag, tagResults must open nothing at all: every surface calls it
// unconditionally on its result set, so the untagged path has to stay free.
func TestTagResultsWithoutATagOpensNothing(t *testing.T) {
	t.Parallel()
	c := newTagFlagCmd(false)
	tagResults(c, []taggableResult{{URL: "https://example.com", Title: "t", Description: "d"}})
}

func TestCountResults(t *testing.T) {
	t.Parallel()
	for n, want := range map[int]string{1: "1 result", 2: "2 results", 0: "0 results"} {
		if got := countResults(n); got != want {
			t.Errorf("countResults(%d) = %q, want %q", n, got, want)
		}
	}
}
