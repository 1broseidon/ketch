package docs

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseLibSpec(t *testing.T) {
	tests := []struct {
		in      string
		url     string
		eco     ecosystem
		name    string
		version string
	}{
		// URLs are taken whole; an @ inside a path segment stays (pkg.go.dev).
		{in: "https://docs.astro.build", url: "https://docs.astro.build"},
		{in: "  https://docs.djangoproject.com/en/stable/ ", url: "https://docs.djangoproject.com/en/stable/"},
		{in: "http://click.palletsprojects.com/", url: "http://click.palletsprojects.com/"},
		{in: "https://pkg.go.dev/github.com/spf13/cobra@v1.8.0", url: "https://pkg.go.dev/github.com/spf13/cobra@v1.8.0"},
		{in: "https://www.npmjs.com/package/@astrojs/starlight", url: "https://www.npmjs.com/package/@astrojs/starlight"},
		// Bare names, with and without a version.
		{in: "click", eco: ecoAny, name: "click"},
		{in: "django@5.2", eco: ecoAny, name: "django", version: "5.2"},
		{in: "next@14", eco: ecoAny, name: "next", version: "14"},
		{in: "tokio@~1.38", eco: ecoAny, name: "tokio", version: "~1.38"},
		{in: "next@canary", eco: ecoAny, name: "next", version: "canary"},
		{in: "chart.js", eco: ecoAny, name: "chart.js"},
		{in: "zope.interface", eco: ecoAny, name: "zope.interface"},
		{in: "Django", eco: ecoAny, name: "Django"},
		// Scoped npm names keep their leading @.
		{in: "@astrojs/starlight", eco: ecoNPM, name: "@astrojs/starlight"},
		{in: "@astrojs/starlight@0.30", eco: ecoNPM, name: "@astrojs/starlight", version: "0.30"},
		{in: "npm:@astrojs/starlight@0.30", eco: ecoNPM, name: "@astrojs/starlight", version: "0.30"},
		// Prefixes, in any case.
		{in: "npm:react", eco: ecoNPM, name: "react"},
		{in: "NPM:React", eco: ecoNPM, name: "React"},
		{in: "pypi:Django@5.2", eco: ecoPyPI, name: "Django", version: "5.2"},
		{in: "pypi:zope.interface", eco: ecoPyPI, name: "zope.interface"},
		{in: "crates:serde", eco: ecoCrates, name: "serde"},
		{in: "cargo:tokio@1.38", eco: ecoCrates, name: "tokio", version: "1.38"},
		{in: "crates:serde_json", eco: ecoCrates, name: "serde_json"},
		{in: "go:github.com/spf13/cobra@v1.8.0", eco: ecoGo, name: "github.com/spf13/cobra", version: "v1.8.0"},
		{in: "github:pallets/click", eco: ecoGitHub, name: "pallets/click"},
		{in: "github:vercel/next.js@14", eco: ecoGitHub, name: "vercel/next.js", version: "14"},
		// Shapes: a module path, owner/repo.
		{in: "github.com/spf13/cobra", eco: ecoGo, name: "github.com/spf13/cobra"},
		{in: "golang.org/x/net", eco: ecoGo, name: "golang.org/x/net"},
		{in: "gopkg.in/yaml.v3", eco: ecoGo, name: "gopkg.in/yaml.v3"},
		{in: "github.com/BurntSushi/toml@v1.4.0", eco: ecoGo, name: "github.com/BurntSushi/toml", version: "v1.4.0"},
		{in: "pallets/click", eco: ecoGitHub, name: "pallets/click"},
		{in: "vercel/next.js", eco: ecoGitHub, name: "vercel/next.js"},
		{in: "pallets/click@8.1", eco: ecoGitHub, name: "pallets/click", version: "8.1"},
	}
	for _, tt := range tests {
		got, err := parseLibSpec(tt.in)
		if err != nil {
			t.Errorf("parseLibSpec(%q): %v", tt.in, err)
			continue
		}
		want := libSpec{raw: strings.TrimSpace(tt.in), url: tt.url, eco: tt.eco, name: tt.name, version: tt.version}
		if got != want {
			t.Errorf("parseLibSpec(%q) = %+v, want %+v", tt.in, got, want)
		}
	}
}

func TestParseLibSpecRejects(t *testing.T) {
	tests := []struct{ in, why string }{
		{"", "empty"},
		{"   ", "empty"},
		{"foo@", "version after @ is empty"},
		{"@astrojs/starlight@", "version after @ is empty"},
		{"maven:x", `unknown prefix "maven:"`},
		{"maven:org.apache:commons", `unknown prefix "maven:"`},
		{"ftp://example.com/docs", `unknown prefix "ftp:"`},
		{"localhost:8080/docs", "https:// scheme"},
		// A URL names its version already.
		{"https://docs.astro.build@5", "names its version already"},
		{"https://docs.astro.build/@5", "names its version already"},
		{"https://docs.djangoproject.com/en/stable/@5.2", "names its version already"},
		{"https://docs.djangoproject.com/en/stable/@latest", "names its version already"},
		{"https://user:secret@docs.example.com/", "user name or password"},
		// Names a registry could not hold.
		{"@astrojs", "needs a package name"},
		{"a/b/c", "not a package name, owner/repo or Go module path"},
		{"react router", "not a package name"},
		{"foo@1 2", "not a version"},
		{"foo@bar@1", "not a package name"},
		{"go:cobra", "Go module path"},
		{"go:github.com//cobra", "Go module path"},
		{"github:pallets", "GitHub owner/repo"},
		{"github:pallets/click/tree/main", "GitHub owner/repo"},
		{"npm:Foo Bar", "npm package name"},
		{"npm:_private", "npm package name"},
		{"pypi:-bad-", "PyPI project name"},
		{"crates:my.crate", "crate name"},
		{"crates:1abc", "crate name"},
		{"cargo:", "name is empty"},
		{"github.com/../x", "Go module path"},
	}
	for _, tt := range tests {
		_, err := parseLibSpec(tt.in)
		if err == nil {
			t.Errorf("parseLibSpec(%q) accepted it", tt.in)
			continue
		}
		if !errors.Is(err, ErrLibraryRequired) {
			t.Errorf("parseLibSpec(%q) = %v, want a validation error (ErrLibraryRequired)", tt.in, err)
		}
		if !strings.Contains(err.Error(), tt.why) {
			t.Errorf("parseLibSpec(%q) = %q, want it to say %q", tt.in, err, tt.why)
		}
	}
}

// Every spec a match lists as Packages reads back as itself.
func TestParseLibSpecReadsPackageSpecs(t *testing.T) {
	for _, s := range []string{
		pkgInfo{eco: ecoPyPI, name: "click"}.spec(),
		pkgInfo{eco: ecoNPM, name: "@astrojs/starlight"}.spec(),
		pkgInfo{eco: ecoCrates, name: "serde_json"}.spec(),
		pkgInfo{eco: ecoGo, name: "github.com/spf13/cobra"}.spec(),
		repoKey{owner: "pallets", name: "click"}.spec(),
		repoKey{owner: "vercel", name: "next.js"}.spec(),
	} {
		got, err := parseLibSpec(s)
		if err != nil {
			t.Errorf("parseLibSpec(%q): %v", s, err)
			continue
		}
		if string(got.eco)+":"+got.name != s {
			t.Errorf("parseLibSpec(%q) = %s:%s", s, got.eco, got.name)
		}
	}
}

func TestSplitLibraryQuery(t *testing.T) {
	type split struct {
		spec   string // the candidate's raw spec
		query  string
		strict bool
	}
	tests := []struct {
		q    string
		want []split
	}{
		// A sentence names its library by syntax: the object closing it
		// after in/with/using/for. The verb after "how do I" is no name.
		{"how do I set retries in requests", []split{{"requests", "how do I set retries", false}}},
		{"How do I parse args with Click?", []split{{"Click", "How do I parse args", false}}},
		{"how can I retry a failed call using tenacity", []split{{"tenacity", "how can I retry a failed call", false}}},
		{"what is a loader in remix", []split{{"remix", "what is a loader", false}}},
		// An article or a kind noun may stand around the object.
		{"how do I add a custom header with the axios library", []split{{"axios", "how do I add a custom header library", false}}},
		{"how to pass props in a vue component", []split{{"vue", "how to pass props component", false}}},
		{"configure retries for boto3 clients", []split{
			{"configure-retries", "for boto3 clients", true},
			{"configure", "retries for boto3 clients", false},
			{"boto3", "configure retries clients", false},
		}},
		// Two words closing the query are a guess at one name.
		{"how to define loaders in react router", []split{{"react-router", "how to define loaders", true}}},
		// The subject after an auxiliary, when nothing closes the query.
		{"how does httpx follow redirects", []split{{"httpx", "how does follow redirects", false}}},
		{"Does sqlmodel support async sessions?", []split{{"sqlmodel", "Does support async sessions?", false}}},
		{"how do signals work in solid", []split{{"solid", "how do signals work", false}}},
		// "use X" names X.
		{"how do I use marshmallow to load nested data", []split{{"marshmallow", "how do I use to load nested data", false}}},
		// The topic a question opens with.
		{"In Starlette, how do I mount a sub-application?", []split{{"Starlette", "how do I mount a sub-application?", false}}},
		{"polars: how do I pivot a frame?", []split{{"polars", "how do I pivot a frame?", false}}},
		{"pandas how to merge two frames?", []split{{"pandas", "how to merge two frames?", false}}},
		// No word in the name's place: a language names no library, and an
		// article after "is" means a noun phrase.
		{"what is the best way to cache results in python", nil},
		{"how do I deploy to production", nil},
		// A keyword query's first word, unless an article or particle after
		// it makes it a verb; a closing "in X" too.
		{"react router loaders", []split{
			{"react-router", "loaders", true},
			{"react", "router loaders", false},
		}},
		{"react hooks", []split{{"react", "hooks", false}}},
		{"retries in requests", []split{
			{"retries", "in requests", false},
			{"requests", "retries", false},
		}},
		{"parse a date string with dayjs", []split{{"dayjs", "parse a date string", false}}},
		{"set up structured logging in loguru", []split{{"loguru", "set up structured logging", false}}},
		{"add middleware to an express app", []split{
			{"add-middleware", "to an express app", true},
			{"add", "middleware to an express app", false},
			{"express", "add middleware app", false},
		}},
		// API identifiers are no names.
		{"useState in react", []split{{"react", "useState", false}}},
		{"pendulum's timezone handling", []split{{"pendulum", "timezone handling", false}}},
		{"next.js app router", []split{
			{"next.js", "app router", false},
			{"next", "app router", false},
		}},
		// Explicit specs win alone.
		{"npm:react hooks", []split{{"npm:react", "hooks", false}}},
		{"use django@5.2 middleware", []split{{"django@5.2", "use middleware", false}}},
		{"@astrojs/starlight@0.30 sidebar config", []split{{"@astrojs/starlight@0.30", "sidebar config", false}}},
		{"sidebar in @astrojs/starlight", []split{{"@astrojs/starlight", "sidebar in", false}}},
		{"https://docs.astro.build content collections", []split{{"https://docs.astro.build", "content collections", false}}},
		{"github.com/spf13/cobra persistent flags", []split{{"github.com/spf13/cobra", "persistent flags", false}}},
		{"pypi:click options", []split{{"pypi:click", "options", false}}},
		{"github:pallets/click decorators", []split{{"github:pallets/click", "decorators", false}}},
		// owner/repo is weaker: the word readings follow it.
		{"pallets/click decorators", []split{{"pallets/click", "decorators", false}}},
		{"how to use async/await in tokio", []split{
			{"async/await", "how to use in tokio", false},
			{"tokio", "how to use async/await", false},
		}},
		// An email is not name@version.
		{"react hooks me@example.com", []split{
			{"react-hooks", "me@example.com", true},
			{"react", "hooks me@example.com", false},
		}},
		// Nothing left to search for.
		{"click", nil},
		{"npm:react", nil},
		{"how do I use requests", nil},
		{"", nil},
		{"   ", nil},
	}
	for _, tt := range tests {
		var got []split
		for _, s := range splitLibraryQuery(tt.q) {
			got = append(got, split{s.spec.raw, s.query, s.strict})
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("splitLibraryQuery(%q)\n got %+v\nwant %+v", tt.q, got, tt.want)
		}
	}
}

// Candidates are parsed specs, at most three, never repeated.
func TestSplitLibraryQueryCandidates(t *testing.T) {
	splits := splitLibraryQuery("tenacity retry with backoff in python")
	if len(splits) != 2 {
		t.Fatalf("got %d candidates (%+v), want 2", len(splits), splits)
	}
	if s := splits[0].spec; s.eco != ecoAny || s.name != "tenacity-retry" || s.version != "" {
		t.Errorf("first candidate = %+v, want the bare name tenacity-retry", s)
	}
	for _, q := range []string{"next next next docs", "how do I use next in next"} {
		seen := map[string]bool{}
		for _, s := range splitLibraryQuery(q) {
			if seen[s.spec.raw] {
				t.Errorf("splitLibraryQuery(%q) repeats %q", q, s.spec.raw)
			}
			seen[s.spec.raw] = true
		}
	}
	got := splitLibraryQuery("npm:@astrojs/starlight@0.30 sidebar")
	if len(got) != 1 || got[0].spec.eco != ecoNPM || got[0].spec.name != "@astrojs/starlight" || got[0].spec.version != "0.30" {
		t.Errorf("explicit spec split = %+v", got)
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		eco        ecosystem
		name, want string
	}{
		{ecoPyPI, "Django", "django"},
		{ecoPyPI, "zope.interface", "zope-interface"},
		{ecoPyPI, "Foo__Bar..baz-_qux", "foo-bar-baz-qux"},
		{ecoPyPI, " requests ", "requests"},
		{ecoNPM, "React", "react"},
		{ecoNPM, "@AstroJS/Starlight", "@astrojs/starlight"},
		{ecoNPM, "chart.js", "chart.js"},
		{ecoCrates, "serde_json", "serde-json"},
		{ecoCrates, "Serde_Derive", "serde-derive"},
		{ecoGo, "github.com/BurntSushi/toml", "github.com/BurntSushi/toml"},
		{ecoGitHub, "Pallets/Click", "pallets/click"},
		{ecoAny, "Click", "click"},
	}
	for _, tt := range tests {
		if got := normalizeName(tt.eco, tt.name); got != tt.want {
			t.Errorf("normalizeName(%s, %q) = %q, want %q", tt.eco, tt.name, got, tt.want)
		}
	}
}

func TestNameVariants(t *testing.T) {
	want := []string{"solid", "solidjs", "solid.js", "solid-js", "py-solid", "solid-py", "solid-rs", "go-solid"}
	if got := nameVariants("Solid"); !slices.Equal(got, want) {
		t.Errorf("nameVariants(Solid) = %v, want %v", got, want)
	}
	got := nameVariants("next.js")
	if got[0] != "next.js" || !slices.Contains(got, "next") || !slices.Contains(got, "nextjs") || !slices.Contains(got, "next-js") {
		t.Errorf("nameVariants(next.js) = %v, want next.js first and the stem's variants", got)
	}
	if got := nameVariants("@astrojs/starlight"); got[0] != "starlight" {
		t.Errorf("nameVariants(@astrojs/starlight) = %v, want the package name's", got)
	}
	if got := nameVariants("github.com/spf13/cobra"); got[0] != "cobra" || !slices.Contains(got, "go-cobra") {
		t.Errorf("nameVariants(module) = %v", got)
	}
	if got := nameVariants(" "); got != nil {
		t.Errorf("nameVariants(blank) = %v", got)
	}
}

func TestRegistriesFor(t *testing.T) {
	all := []ecosystem{ecoNPM, ecoPyPI, ecoCrates}
	tests := []struct {
		spec string
		want []ecosystem
	}{
		{"click", all},
		{"Django", all},
		{"solid-js", all},
		{"serde_json", all},
		{"chart.js", []ecosystem{ecoNPM, ecoPyPI}},
		{"socket.io", []ecosystem{ecoNPM, ecoPyPI}},
		{"1password", []ecosystem{ecoNPM, ecoPyPI}},
		{"npm:react", []ecosystem{ecoNPM}},
		{"@astrojs/starlight", []ecosystem{ecoNPM}},
		{"pypi:click", []ecosystem{ecoPyPI}},
		{"cargo:serde", []ecosystem{ecoCrates}},
		{"github.com/spf13/cobra", []ecosystem{ecoGo}},
		{"pallets/click", nil},
		{"https://docs.astro.build", nil},
	}
	for _, tt := range tests {
		spec, err := parseLibSpec(tt.spec)
		if err != nil {
			t.Fatal(err)
		}
		if got := registriesFor(spec); !slices.Equal(got, tt.want) {
			t.Errorf("registriesFor(%q) = %v, want %v", tt.spec, got, tt.want)
		}
	}
}
