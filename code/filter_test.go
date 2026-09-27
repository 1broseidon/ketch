package code

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestNormalizeRepo(t *testing.T) {
	valid := map[string]string{
		"golang/go":                                "golang/go",
		"  golang/go  ":                            "golang/go",
		"vercel/next.js":                           "vercel/next.js",
		"1broseidon/ketch":                         "1broseidon/ketch",
		"https://github.com/golang/go":             "golang/go",
		"http://github.com/golang/go/":             "golang/go",
		"https://www.github.com/golang/go.git":     "golang/go",
		"github.com/golang/go":                     "golang/go",
		"GitHub.com/Microsoft/TypeScript":          "Microsoft/TypeScript",
		"https://github.com/some_org/repo-name.js": "some_org/repo-name.js",
		"": "",
	}
	for in, want := range valid {
		got, err := NormalizeRepo(in)
		if err != nil || got != want {
			t.Errorf("NormalizeRepo(%q) = %q, %v; want %q", in, got, err, want)
			continue
		}
		// Backends normalize again, so a normalized value must survive it.
		if again, err := NormalizeRepo(got); err != nil || again != got {
			t.Errorf("NormalizeRepo is not idempotent on %q: got %q, %v", got, again, err)
		}
	}

	for _, in := range []string{
		"golang",
		"golang/go/src",
		"https://github.com/golang/go/tree/master",
		"gitlab.com/group/repo",
		"golang/go language:go",
		"repo:golang/go",
		"golang/",
		"/go",
		"../go",
		"golang/..",
		"github.com/",
	} {
		if got, err := NormalizeRepo(in); !errors.Is(err, ErrInvalidRepo) {
			t.Errorf("NormalizeRepo(%q) = %q, %v; want ErrInvalidRepo", in, got, err)
		}
	}
}

// A library caller that skips NormalizeRepo still gets validation: a
// malformed Repo never reaches a backend's query dialect, or the network.
func TestBackendsRejectInvalidRepo(t *testing.T) {
	q := Query{Term: "x", Repo: "golang/go language:go", Limit: 5}
	for name, s := range map[string]Searcher{
		"grepapp":     &GrepApp{endpoint: "http://127.0.0.1:0", client: grepAppClient},
		"sourcegraph": NewSourcegraph("http://127.0.0.1:0"),
		"github":      NewGitHub("fixture-token"),
	} {
		if _, err := s.Search(context.Background(), q); !errors.Is(err, ErrInvalidRepo) {
			t.Errorf("%s: err = %v, want ErrInvalidRepo", name, err)
		}
	}
}

func TestLiteralQualifiers(t *testing.T) {
	grepapp, _ := Lookup("grepapp")
	cases := map[string][]string{
		"repo:1broseidon/ketch NewFromConfig":                 {"repo:1broseidon/ketch"},
		"useState( Lang:tsx language:TypeScript":              {"Lang:tsx", "language:TypeScript"},
		"http.Get org:golang user:rsc path:src/ file:main.go": {"org:golang", "user:rsc", "path:src/", "file:main.go"},
		"repo:^github\\.com/golang/go$ gcController":          {"repo:^github\\.com/golang/go$"},
		// Code, not filters: no value, a URL or quoted path, a variable,
		// qualifiers the scoping list leaves out, a qualifier inside a word.
		"repo: foo":          nil,
		"file:///etc/passwd": nil,
		`path:"/api"`:        nil,
		"repo:$REPO":         nil,
		"type:string":        nil,
		"myrepo:x/y":         nil,
		"useState(":          nil,
	}
	for query, want := range cases {
		if got := grepapp.LiteralQualifiers(query); !slices.Equal(got, want) {
			t.Errorf("grepapp.LiteralQualifiers(%q) = %q, want %q", query, got, want)
		}
	}

	for _, id := range QualifierBackends() {
		p, _ := Lookup(id)
		if got := p.LiteralQualifiers("repo:golang/go lang:go x"); got != nil {
			t.Errorf("%s applies qualifiers but reported %q as literal", id, got)
		}
	}
	if got := QualifierBackends(); !slices.Equal(got, []string{"sourcegraph", "github"}) {
		t.Errorf("QualifierBackends() = %q", got)
	}
}

func TestMayLackRepo(t *testing.T) {
	grepapp, _ := Lookup("grepapp")
	sourcegraph, _ := Lookup("sourcegraph")
	scoped := Query{Term: "x", Repo: "facebook/react"}
	hit := []Result{{Repo: "facebook/react"}}

	if !grepapp.MayLackRepo(scoped, nil) {
		t.Error("an empty repo-scoped grepapp search should flag a possibly unindexed repository")
	}
	if grepapp.MayLackRepo(scoped, hit) || grepapp.MayLackRepo(Query{Term: "x"}, nil) {
		t.Error("only an empty result for a named repository is ambiguous")
	}
	if sourcegraph.MayLackRepo(scoped, nil) {
		t.Error("sourcegraph reports missing repositories itself (ErrRepoNotFound)")
	}
}

func TestAlternatives(t *testing.T) {
	if got := Alternatives("sourcegraph"); !slices.Equal(got, []string{"grepapp", "github"}) {
		t.Errorf("Alternatives(sourcegraph) = %q", got)
	}
}
