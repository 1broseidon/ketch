package docs

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/1broseidon/ketch/internal/docsindex"
)

// The library-name measurement set (design/docs-evidence.md, Round 11):
// the library and resolve values model callers passed to the docs tool
// in the docs flow tests, each with the indexed root(s) it means ("" for
// none), against the metadata of the 135 libraries of the pinned index
// they ran on. A value is split with its site, tune or confirm.

type namesFixture struct {
	Libraries []docsindex.Library `json:"libraries"`
	Values    []namesValue        `json:"values"`
}

type namesValue struct {
	Arg      string   `json:"arg"` // library or resolve
	Value    string   `json:"value"`
	Expected []string `json:"expected"`
	Split    string   `json:"split"`
}

func loadNamesFixture(t *testing.T) namesFixture {
	t.Helper()
	b, err := os.ReadFile("testdata/names/pinned.json")
	if err != nil {
		t.Fatal(err)
	}
	var f namesFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// Outcomes of one value.
const (
	nameRight = "right"
	nameWrong = "wrong"
	nameNone  = "none"
)

// libraryOutcome is what a library value answers from: the library the
// question path picks (MatchIndexed, then the newest), right when it is
// one the value means.
func libraryOutcome(libs []docsindex.Library, v namesValue) (string, string) {
	found := MatchIndexed(libs, v.Value)
	if len(found) == 0 {
		return nameNone, ""
	}
	lib, _ := newestOf(found)
	if slices.Contains(v.Expected, lib.Root) {
		return nameRight, lib.Root
	}
	return nameWrong, lib.Root
}

// resolveOutcome is what a resolve value lists from the index: right when
// a library it means is listed, wrong when others are and it is not.
func resolveOutcome(libs []docsindex.Library, v namesValue) (string, string) {
	found := indexedNamed(libs, v.Value)
	if len(found) == 0 {
		return nameNone, ""
	}
	var roots []string
	for _, l := range found {
		roots = append(roots, l.Root)
	}
	for _, r := range roots {
		if slices.Contains(v.Expected, r) {
			return nameRight, fmt.Sprintf("%d listed", len(roots))
		}
	}
	return nameWrong, fmt.Sprint(roots)
}

func namesOutcome(libs []docsindex.Library, v namesValue) (string, string) {
	if v.Arg == "resolve" {
		return resolveOutcome(libs, v)
	}
	return libraryOutcome(libs, v)
}

// TestLibraryNamesMeasured runs the measurement set. No value may answer
// from a library it does not mean; the right counts may not fall below
// what this round reached. KETCH_NAMES_DUMP=1 prints every value.
func TestLibraryNamesMeasured(t *testing.T) {
	f := loadNamesFixture(t)
	counts := map[string]map[string]int{}
	for _, v := range f.Values {
		out, got := namesOutcome(f.Libraries, v)
		key := v.Arg + " " + v.Split
		if counts[key] == nil {
			counts[key] = map[string]int{}
		}
		counts[key][out]++
		if os.Getenv("KETCH_NAMES_DUMP") != "" {
			t.Logf("%-7s %-7s %-5s %q -> %s (want %v)", v.Split, v.Arg, out, v.Value, got, v.Expected)
		}
		if out == nameWrong {
			t.Errorf("%s %q answered from %s, which it does not mean (want %v)", v.Arg, v.Value, got, v.Expected)
		}
	}
	for _, k := range []string{"library tune", "library confirm", "resolve tune", "resolve confirm"} {
		t.Logf("%-16s right %3d  wrong %2d  none %3d", k, counts[k][nameRight], counts[k][nameWrong], counts[k][nameNone])
	}
}

// A value that is one word of a multi-word indexed name whose own project
// is another thing, a vendor's name for one of its products' docs, or a
// generic word: never an answer from an indexed library (it may be listed
// as near). drop takes libraries out of the pinned set (React's own docs,
// for "react" with only React Router indexed); add puts in one the pin
// lacks.
type nameNegative struct {
	value string
	drop  []string
	add   []docsindex.Library
}

var (
	drfLib = docsindex.Library{Key: "https://www.django-rest-framework.org/", Root: "https://www.django-rest-framework.org/",
		Library: "https://www.django-rest-framework.org/", Title: "Django REST framework", SiteName: "Django REST framework", Format: docsindex.FormatVersion}
	springBootLib = docsindex.Library{Key: "https://docs.spring.io/spring-boot/", Root: "https://docs.spring.io/spring-boot/",
		Library: "https://docs.spring.io/spring-boot/", Title: "Spring Boot", SiteName: "Spring Boot", Format: docsindex.FormatVersion}
	gcsLib = docsindex.Library{Key: "https://cloud.google.com/storage/docs/", Root: "https://cloud.google.com/storage/docs/",
		Library: "https://cloud.google.com/storage/docs/", Title: "Google Cloud", SiteName: "Google Cloud", Format: docsindex.FormatVersion}
)

func nameNegatives() []nameNegative {
	return []nameNegative{
		{value: "react", drop: []string{"https://react.dev/"}},
		{value: "vue", drop: []string{"https://vuejs.org/"}},
		{value: "router"},
		{value: "django", drop: []string{"https://docs.djangoproject.com/en/6.1/"}, add: []docsindex.Library{drfLib}},
		{value: "rest", add: []docsindex.Library{drfLib}},
		{value: "spring", add: []docsindex.Library{springBootLib}},
		{value: "google", add: []docsindex.Library{gcsLib}},
		{value: "cloud", add: []docsindex.Library{gcsLib}},
		{value: "api"},
		{value: "docs"},
		{value: "testing"},
		{value: "redis", drop: []string{"https://redis.readthedocs.io/en/stable/"}},
		{value: "rust"},
		{value: "tanstack"},
		{value: "cloudflare"},
		{value: "hashicorp"},
		{value: "aws"},
		{value: "amazon"},
		{value: "android"},
		{value: "atlassian"},
		{value: "github"},
		{value: "cisco"},
		{value: "ietf"},
		{value: "freedesktop"},
		{value: "hugging face"},
		{value: "uptrace"},
		{value: "palletsprojects"},
		{value: "tiangolo"},
		{value: "@tanstack/react-query", drop: []string{"https://tanstack.com/query/latest/"}},
		{value: "python"},
		{value: "go"},
	}
}

func negativeLibs(all []docsindex.Library, n nameNegative) []docsindex.Library {
	libs := slices.DeleteFunc(slices.Clone(all), func(l docsindex.Library) bool { return slices.Contains(n.drop, l.Root) })
	return append(libs, n.add...)
}

// TestLibraryNamesNegatives: none of the negatives names an indexed
// library.
func TestLibraryNamesNegatives(t *testing.T) {
	f := loadNamesFixture(t)
	for _, n := range nameNegatives() {
		libs := negativeLibs(f.Libraries, n)
		if found := MatchIndexed(libs, n.value); len(found) > 0 {
			t.Errorf("%q names %s", n.value, found[0].Root)
		}
		if os.Getenv("KETCH_NAMES_DUMP") != "" {
			t.Logf("%q near %v", n.value, nearIndexed(libs, n.value))
		}
	}
}
