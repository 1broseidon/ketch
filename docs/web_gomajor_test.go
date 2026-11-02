package docs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestGoMajorModules(t *testing.T) {
	for _, c := range []struct {
		module, want string
		out          []string
	}{
		{"github.com/redis/go-redis", "9", []string{"github.com/redis/go-redis/v9", "github.com/redis/go-redis"}},
		{"github.com/jackc/pgx/v5", "4", []string{"github.com/jackc/pgx/v4", "github.com/jackc/pgx"}},
		{"github.com/labstack/echo/v4", "3.3", []string{"github.com/labstack/echo/v3", "github.com/labstack/echo"}},
		{"github.com/urfave/cli/v2", "1", []string{"github.com/urfave/cli"}},
		{"github.com/spf13/cobra", "1.8", []string{"github.com/spf13/cobra"}},
		{"gopkg.in/yaml.v3", "2", []string{"gopkg.in/yaml.v2"}},
		{"github.com/some/v2tool", "3", []string{"github.com/some/v2tool/v3", "github.com/some/v2tool"}},
		{"github.com/spf13/cobra", "latest", nil},
	} {
		if got := goMajorModules(c.module, c.want); !slices.Equal(got, c.out) {
			t.Errorf("goMajorModules(%q, %q) = %q, want %q", c.module, c.want, got, c.out)
		}
	}
}

// +incompatible is build metadata, not a pre-release: echo's v3 releases
// are releases.
func TestParseVerNumBuildMetadata(t *testing.T) {
	n, ok := parseVerNum("v3.3.10+incompatible")
	if !ok || n.pre != "" || !slices.Equal(n.parts, []int{3, 3, 10}) {
		t.Errorf("parseVerNum = %+v %v", n, ok)
	}
	if n, _ := parseVerNum("v3.3.8-retag+incompatible"); n.pre != "retag" {
		t.Errorf("a pre-release with build metadata lost its pre-release: %+v", n)
	}
}

// goModulePkgs lists Go module versions by module path.
type goModulePkgs struct {
	fakePkgs
	versions map[string][]string
	asked    []string
}

func (f *goModulePkgs) goVersions(_ context.Context, module string) ([]string, error) {
	f.asked = append(f.asked, module)
	return f.versions[module], nil
}

// Registry docs at a pinned major live at that major's module: go-redis@9
// is github.com/redis/go-redis/v9, pgx@3 the base path's +incompatible
// releases, and a module's own major needs no other.
func TestGoModuleVersionsFollowsMajorPaths(t *testing.T) {
	pkgs := &goModulePkgs{versions: map[string][]string{
		"github.com/redis/go-redis":    {"v6.15.9+incompatible", "v6.15.8+incompatible"},
		"github.com/redis/go-redis/v9": {"v9.7.0", "v9.6.1"},
		"github.com/jackc/pgx/v5":      {"v5.7.1"},
		"github.com/jackc/pgx":         {"v3.6.2+incompatible", "v2.11.0+incompatible"},
	}}
	w := &Web{pkgs: pkgs}
	ctx := context.Background()
	for _, c := range []struct {
		module, want, root, label string
	}{
		{"github.com/redis/go-redis", "9", "https://pkg.go.dev/github.com/redis/go-redis/v9@v9.7.0", "v9.7.0"},
		{"github.com/redis/go-redis", "6", "https://pkg.go.dev/github.com/redis/go-redis@v6.15.9+incompatible", "v6.15.9+incompatible"},
		{"github.com/jackc/pgx/v5", "3", "https://pkg.go.dev/github.com/jackc/pgx@v3.6.2+incompatible", "v3.6.2+incompatible"},
		{"github.com/jackc/pgx/v5", "5.7", "https://pkg.go.dev/github.com/jackc/pgx/v5@v5.7.1", "v5.7.1"},
	} {
		pkg := pkgInfo{eco: ecoGo, name: c.module, registryDocs: pkgsiteBase + "/" + c.module}
		got, versions := w.goModuleVersions(ctx, pkg, c.want)
		sv, ok := registryVersionRoot(got, c.want, versions)
		if !ok || sv.root != c.root || sv.label != c.label {
			t.Errorf("%s@%s = %+v %v; want %s (%s)", c.module, c.want, sv, ok, c.root, c.label)
		}
	}
	// A major no module publishes is not found.
	pkg := pkgInfo{eco: ecoGo, name: "github.com/redis/go-redis", registryDocs: pkgsiteBase + "/github.com/redis/go-redis"}
	got, versions := w.goModuleVersions(ctx, pkg, "12")
	if _, ok := registryVersionRoot(got, "12", versions); ok {
		t.Error("go-redis@12 resolved")
	}
}

// releasePkgs lists registry releases by package.
type releasePkgs struct {
	fakePkgs
	list map[string][]string
}

func (f *releasePkgs) releases(_ context.Context, eco ecosystem, name string) ([]string, error) {
	return f.list[string(eco)+":"+name], nil
}

// A site that names its version trees by release and lists none of them
// in its HTML (chartjs.org: /docs/latest/ beside /docs/3.9.1/, the menu
// filled in by a script) keeps chart.js@3 at the newest 3.x release the
// registry publishes. Trimmed from the live VuePress pages.
func TestPickVersionAtTheRegistrysRelease(t *testing.T) {
	w := versionSitesWeb(t)
	w.pkgs = &releasePkgs{list: map[string][]string{"npm:chart.js": {"4.5.1", "4.0.0", "3.9.1", "3.9.0", "3.0.0-beta.2", "2.9.4"}}}
	m, err := describeAt(t, w, "https://www.chartjs.org/docs/latest/", "npm:chart.js@3", "4.5.1", false)
	if err != nil || m.root.String() != "https://www.chartjs.org/docs/3.9.1/" || m.version != "3.9.1" {
		t.Fatalf("chart.js@3 = %s %q, %v; want /docs/3.9.1/", m.root, m.version, err)
	}
	// A release the site has no tree for is not found.
	if m, err := describeAt(t, w, "https://www.chartjs.org/docs/latest/", "npm:chart.js@2", "4.5.1", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("chart.js@2 = %s, %v; want not found", m.root, err)
	}
}

func TestObjectKeys(t *testing.T) {
	keys, err := objectKeys(strings.NewReader(`{"info":{"version":"5.0"},"releases":{"4.4.7":[{"x":1}],"5.0.0":[]},"urls":[]}`), "releases")
	if err != nil || !slices.Equal(keys, []string{"4.4.7", "5.0.0"}) {
		t.Errorf("objectKeys = %v, %v", keys, err)
	}
	if _, err := objectKeys(strings.NewReader(`{"info":{}}`), "releases"); err == nil {
		t.Error("a missing field read as empty")
	}
}

// releasesRT adapts a function to http.RoundTripper.
type releasesRT func(*http.Request) (*http.Response, error)

func (f releasesRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// npm's release list is its abbreviated metadata (asked for by Accept),
// PyPI's the keys of its JSON's releases; both come back newest first.
func TestPkgClientReleases(t *testing.T) {
	var accept string
	c := newPkgClient(&http.Client{Transport: releasesRT(func(r *http.Request) (*http.Response, error) {
		body := `{"name":"chart.js","dist-tags":{"latest":"4.5.1"},"versions":{"2.9.4":{},"3.9.1":{"dist":{}},"4.5.1":{},"3.10.0-beta.1":{}}}`
		if r.URL.Host == "pypi.org" {
			body = `{"info":{"name":"celery"},"last_serial":1,"releases":{"4.4.7":[{"filename":"a"}],"5.6.0":[],"4.4.10":[]},"urls":[]}`
		} else {
			accept = r.Header.Get("Accept")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	got, err := c.releases(context.Background(), ecoNPM, "chart.js")
	if err != nil || !slices.Equal(got, []string{"4.5.1", "3.10.0-beta.1", "3.9.1", "2.9.4"}) {
		t.Errorf("npm releases = %v, %v", got, err)
	}
	if accept != "application/vnd.npm.install-v1+json" {
		t.Errorf("npm Accept = %q", accept)
	}
	got, err = c.releases(context.Background(), ecoPyPI, "celery")
	if err != nil || !slices.Equal(got, []string{"5.6.0", "4.4.10", "4.4.7"}) {
		t.Errorf("pypi releases = %v, %v", got, err)
	}
	if _, err := c.releases(context.Background(), ecoCrates, "serde"); !errors.Is(err, ErrNotFound) {
		t.Errorf("crates releases: %v", err)
	}
}
