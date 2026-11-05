package docs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestTruncate(t *testing.T) {
	in := []Result{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	for _, tc := range []struct{ limit, want int }{{0, 3}, {-1, 3}, {2, 2}, {3, 3}, {9, 3}} {
		if got := len(Truncate(in, tc.limit)); got != tc.want {
			t.Errorf("Truncate(limit=%d) = %d results, want %d", tc.limit, got, tc.want)
		}
	}
	if Truncate(nil, 2) != nil {
		t.Errorf("Truncate(nil) should stay nil")
	}
}

// The web backend's LibraryMatch fields are omitted when empty, so a
// Context7 match marshals exactly as it did before they existed.
func TestLibraryMatchJSONUnchangedForContext7(t *testing.T) {
	m := LibraryMatch{ID: "/facebook/react", Title: "React", Description: "UI library", TotalSnippets: 10, TrustScore: 9, Versions: []string{"v18"}}
	got, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"id":"/facebook/react","title":"React","description":"UI library","totalSnippets":10,"trustScore":9,"versions":["v18"]}`; string(got) != want {
		t.Errorf("Context7 match JSON changed:\n got %s\nwant %s", got, want)
	}
	if got, _ := json.Marshal(LibraryMatch{ID: "/x/y"}); string(got) != `{"id":"/x/y","title":"","description":"","totalSnippets":0,"trustScore":0,"versions":null}` {
		t.Errorf("zero match JSON changed: %s", got)
	}
	web := LibraryMatch{ID: "https://click.palletsprojects.com/en/stable/", Version: "8.5.x", Evidence: []string{"pypi click lists it as Documentation"}, Packages: []string{"pypi:click"}}
	got, _ = json.Marshal(web)
	for _, key := range []string{`"version":"8.5.x"`, `"evidence":["pypi click lists it as Documentation"]`, `"packages":["pypi:click"]`} {
		if !strings.Contains(string(got), key) {
			t.Errorf("web match JSON %s lacks %s", got, key)
		}
	}
}

func TestAmbiguousLibraryError(t *testing.T) {
	amb := &AmbiguousLibraryError{Library: "widget", Matches: []LibraryMatch{
		{ID: "https://widget.dev/", Title: "Widget", Packages: []string{"npm:widget", "github:acme/widget"}},
		{ID: "https://widget.readthedocs.io/en/stable/", Title: "widget", Packages: []string{"pypi:widget"}},
		{ID: "https://bare.example/"},
	}}
	err := fmt.Errorf("web: %w", amb)
	if !errors.Is(err, ErrAmbiguousLibrary) || !errors.Is(err, ErrLibraryRequired) || errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is: ambiguous %v, library required %v, not found %v",
			errors.Is(err, ErrAmbiguousLibrary), errors.Is(err, ErrLibraryRequired), errors.Is(err, ErrNotFound))
	}
	var got *AmbiguousLibraryError
	if !errors.As(err, &got) || len(got.Matches) != 3 {
		t.Fatalf("errors.As = %v", got)
	}
	want := `ambiguous library "widget": 3 libraries match; pass one with --library (CLI) or library (MCP): ` +
		`https://widget.dev/ (Widget; npm:widget, github:acme/widget), ` +
		`https://widget.readthedocs.io/en/stable/ (widget; pypi:widget), ` +
		`https://bare.example/`
	if amb.Error() != want {
		t.Errorf("message:\n got %s\nwant %s", amb.Error(), want)
	}
}
