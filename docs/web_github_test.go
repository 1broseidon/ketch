package docs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1broseidon/ketch/internal/testutil"
)

// recordedGHClient is a ghClient over the recorded GitHub fixtures, with the
// given token.
func recordedGHClient(t *testing.T, token string) (*ghClient, *testutil.FixtureTransport) {
	t.Helper()
	ft := testutil.LoadFixtures(t, githubFixtureDir)
	return newGHClient(ft.Client(), func() string { return token }), ft
}

func TestGHClientRepo(t *testing.T) {
	g, ft := recordedGHClient(t, "")
	ctx := context.Background()
	tests := []struct {
		key  repoKey
		want repoInfo
	}{
		{repoKey{"pallets", "click"}, repoInfo{key: repoKey{"pallets", "click"}, homepage: "https://click.palletsprojects.com",
			description: "Python composable command line interface toolkit", stars: 17801, language: "Python"}},
		// Renamed repositories answer 301; the key is the canonical name.
		{repoKey{"facebook", "react"}, repoInfo{key: repoKey{"react", "react"}, homepage: "https://react.dev",
			description: "The library for web and native user interfaces.", stars: 251894, language: "JavaScript"}},
		{repoKey{"requests", "requests"}, repoInfo{key: repoKey{"psf", "requests"}, homepage: "https://requests.readthedocs.io/en/latest/",
			description: "A simple, yet elegant, HTTP library.", stars: 54728, language: "Python"}},
		// A homepage without a scheme is returned as published.
		{repoKey{"clap-rs", "clap"}, repoInfo{key: repoKey{"clap-rs", "clap"}, homepage: "docs.rs/clap",
			description: "A full featured, fast Command Line Argument Parser for Rust", stars: 16802, language: "Rust"}},
		{repoKey{"pyo3", "tokio"}, repoInfo{key: repoKey{"pyo3", "tokio"}, archived: true, stars: 245,
			description: "Asyncio event loop written in Rust language", language: "Rust"}},
	}
	for _, tt := range tests {
		got, err := g.repo(ctx, tt.key)
		if err != nil {
			t.Errorf("repo(%s): %v", tt.key, err)
			continue
		}
		if got != tt.want {
			t.Errorf("repo(%s)\n got %+v\nwant %+v", tt.key, got, tt.want)
		}
	}
	for _, key := range []repoKey{{"uggedal", "click"}, {"dheerajmpai", "saenews"}} {
		_, err := g.repo(ctx, key)
		if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), key.String()) {
			t.Errorf("repo(%s) = %v, want ErrNotFound naming it", key, err)
		}
	}
	if _, err := g.repo(ctx, repoKey{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("repo(zero key) = %v, want ErrNotFound", err)
	}
	if u := ft.Unmatched(); len(u) != 0 {
		t.Errorf("unmatched requests: %v", u)
	}
	for _, c := range ft.Calls() {
		if c.Header.Get("X-GitHub-Api-Version") != "2022-11-28" || c.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("%s headers: %v", c, c.Header)
		}
		if c.Header.Get("Authorization") != "" {
			t.Errorf("%s sent Authorization with no token", c)
		}
	}
}

func TestGHClientForkParent(t *testing.T) {
	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://api.github.com/repos/someone/click", testutil.Fixture{
		Body: `{"full_name":"Someone/click","homepage":null,"description":null,"fork":true,"archived":false,
			"stargazers_count":3,"parent":{"full_name":"pallets/click"}}`,
	})
	ft.Handle(http.MethodGet, "https://api.github.com/repos/odd/shape", testutil.Fixture{Body: `{"message":"ok"}`})
	g := newGHClient(ft.Client(), nil)
	got, err := g.repo(context.Background(), repoKey{"someone", "click"})
	if err != nil {
		t.Fatal(err)
	}
	want := repoInfo{key: repoKey{"someone", "click"}, fork: true, parent: repoKey{"pallets", "click"}, stars: 3}
	if got != want {
		t.Errorf("fork = %+v, want %+v", got, want)
	}
	if _, err := g.repo(context.Background(), repoKey{"odd", "shape"}); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("a response naming no repository = %v, want a plain error", err)
	}
}

// The token is asked for on the first request, not at construction, and
// sent as a bearer token to every hop of a rename.
func TestGHClientTokenIsLazy(t *testing.T) {
	ft := testutil.LoadFixtures(t, githubFixtureDir)
	var calls atomic.Int32
	g := newGHClient(ft.Client(), func() string { calls.Add(1); return "tok-123" })
	if calls.Load() != 0 {
		t.Fatal("newGHClient resolved the token")
	}
	if _, err := g.repo(context.Background(), repoKey{"facebook", "react"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() == 0 {
		t.Error("the token was never asked for")
	}
	got := ft.Calls()
	if len(got) != 2 {
		t.Fatalf("calls = %v, want the 301 and its target", got)
	}
	for _, c := range got {
		if c.Header.Get("Authorization") != "Bearer tok-123" {
			t.Errorf("%s Authorization = %q", c, c.Header.Get("Authorization"))
		}
	}
}

func TestGHClientSearchRepos(t *testing.T) {
	g, ft := recordedGHClient(t, "")
	ctx := context.Background()
	got, err := g.searchRepos(ctx, "solid", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("searchRepos(solid, 5) = %d results", len(got))
	}
	if got[0].key != (repoKey{"solidjs", "solid"}) || got[0].stars != 36183 || got[0].homepage != "https://solidjs.com" {
		t.Errorf("top result = %+v, want solidjs/solid", got[0])
	}
	all, err := g.searchRepos(ctx, "solid", 0)
	if err != nil || len(all) != 30 {
		t.Fatalf("searchRepos(solid, 0) = %d results, %v; want the whole page", len(all), err)
	}
	for i := 1; i < len(all); i++ {
		if all[i].stars > all[i-1].stars {
			t.Errorf("results not sorted by stars: %v before %v", all[i-1], all[i])
		}
	}
	var archived bool
	for _, r := range all {
		if r.key == (repoKey{"solid", "solid"}) {
			archived = r.archived
		}
	}
	if !archived {
		t.Error("solid/solid should be in the page, archived")
	}
	if u := ft.Unmatched(); len(u) != 0 {
		t.Errorf("unmatched requests: %v (have %v)", u, ft.URLs())
	}
}

// More than a page asks for it; qualifier-like names ask nothing.
func TestGHClientSearchReposRequests(t *testing.T) {
	g, ft := recordedGHClient(t, "")
	ctx := context.Background()
	ft.Handle(http.MethodGet, "https://api.github.com/search/repositories?q=solid+in:name&sort=stars&per_page=50", testutil.Fixture{
		Body: `{"total_count":1,"items":[{"full_name":"solidjs/solid","stargazers_count":1}]}`,
	})
	if got, err := g.searchRepos(ctx, "solid", 50); err != nil || len(got) != 1 {
		t.Errorf("searchRepos(solid, 50) = %v, %v", got, err)
	}
	before := len(ft.Calls())
	for _, name := range []string{"solid user:evil", "", "a/b"} {
		if got, err := g.searchRepos(ctx, name, 5); got != nil || err != nil {
			t.Errorf("searchRepos(%q) = %v, %v; want nothing", name, got, err)
		}
	}
	if len(ft.Calls()) != before {
		t.Error("an invalid name was searched for")
	}
}

func TestGHClientRateLimits(t *testing.T) {
	reset := time.Now().Add(41 * time.Minute).Truncate(time.Second)
	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://api.github.com/repos/core/limit", testutil.Fixture{
		Status: http.StatusForbidden,
		Header: http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {strconv.FormatInt(reset.Unix(), 10)}},
		Body:   `{"message":"API rate limit exceeded for 203.0.113.9. (But here's the good news: Authenticated requests get a higher rate limit.)"}`,
	})
	ft.Handle(http.MethodGet, "https://api.github.com/search/repositories?q=solid+in:name&sort=stars", testutil.Fixture{
		Status: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"60"}},
	})
	ft.Handle(http.MethodGet, "https://api.github.com/repos/secondary/limit", testutil.Fixture{
		Status: http.StatusForbidden, Body: `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`,
	})
	ctx := context.Background()

	anon := newGHClient(ft.Client(), nil)
	_, err := anon.repo(ctx, repoKey{"core", "limit"})
	var rl *ghRateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("primary limit = %v, want *ghRateLimitError", err)
	}
	if !rl.reset.Equal(reset) || rl.authed {
		t.Errorf("rate limit = %+v, want reset %s, unauthenticated", rl, reset)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "github: rate limited (set github_token or gh auth login)") || !strings.Contains(msg, reset.Format("15:04:05")) {
		t.Errorf("message = %q", msg)
	}

	authed := newGHClient(ft.Client(), func() string { return "tok" })
	_, err = authed.searchRepos(ctx, "solid", 5)
	if !errors.As(err, &rl) || !rl.authed || time.Until(rl.reset) < 50*time.Second || time.Until(rl.reset) > 70*time.Second {
		t.Errorf("429 with Retry-After = %v (%+v)", err, rl)
	} else if strings.Contains(err.Error(), "gh auth login") {
		t.Errorf("an authenticated limit should not suggest a token: %q", err)
	}

	if _, err := anon.repo(ctx, repoKey{"secondary", "limit"}); !errors.As(err, &rl) || !rl.reset.IsZero() {
		t.Errorf("secondary limit = %v", err)
	}
	if got := (&ghRateLimitError{}).Error(); got != "github: rate limited (set github_token or gh auth login)" {
		t.Errorf("bare message = %q", got)
	}
}

// Refusals that are not rate limits are plain errors quoting GitHub.
func TestGHClientStatusErrors(t *testing.T) {
	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://api.github.com/repos/blocked/repo", testutil.Fixture{
		Status: http.StatusForbidden, Body: `{"message":"Repository access blocked"}`,
	})
	ft.Handle(http.MethodGet, "https://api.github.com/repos/bad/token", testutil.Fixture{Status: http.StatusUnauthorized, Body: `{"message":"Bad credentials"}`})
	ft.Handle(http.MethodGet, "https://api.github.com/repos/down/repo", testutil.Fixture{Status: http.StatusBadGateway, Body: "upstream sad"})
	ft.Handle(http.MethodGet, "https://api.github.com/repos/garbled/repo", testutil.Fixture{Body: `{"full_name":`})
	authed := newGHClient(ft.Client(), func() string { return "tok" })
	var rl *ghRateLimitError
	for key, want := range map[repoKey]string{
		{"blocked", "repo"}: "status 403: {\"message\":\"Repository access blocked\"}",
		{"bad", "token"}:    "invalid token",
		{"down", "repo"}:    "status 502: upstream sad",
		{"garbled", "repo"}: "failed to decode github response",
	} {
		_, err := authed.repo(context.Background(), key)
		if err == nil || errors.As(err, &rl) || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), want) {
			t.Errorf("repo(%s) = %v, want a plain error saying %q", key, err, want)
		}
	}
}

func TestGHClientTimeout(t *testing.T) {
	ft := testutil.NewFixtureTransport()
	ft.Handle(http.MethodGet, "https://api.github.com/repos/slow/repo", testutil.Fixture{Delay: 5 * time.Second, Body: `{}`})
	g := newGHClient(ft.Client(), func() string { return "tok" })
	g.timeout = 50 * time.Millisecond
	start := time.Now()
	_, err := g.repo(context.Background(), repoKey{"slow", "repo"})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Errorf("slow GitHub = %v after %s, want the per-call deadline", err, time.Since(start))
	}
}

// GitHub, or a proxy in front of it, that echoes the token it was sent (in
// a body, a malformed redirect, a redirect loop's URL or a dead redirect's
// URL) must not get it into the error.
func TestGHClientErrorsNeverEchoTheToken(t *testing.T) {
	for i, token := range []string{"ghp_s3cretTokenValue123", "abc/def+ghi=="} {
		for _, mode := range testutil.HostileModes {
			t.Run(fmt.Sprintf("%s/%d", mode, i), func(t *testing.T) {
				srv := testutil.HostileInstance(t, "Authorization", mode)
				g := newGHClient(&http.Client{}, func() string { return token })
				g.base = srv.URL
				_, repoErr := g.repo(context.Background(), repoKey{"pallets", "click"})
				_, searchErr := g.searchRepos(context.Background(), "click", 5)
				for _, err := range []error{repoErr, searchErr} {
					if err == nil {
						t.Fatal("a request to a failing instance succeeded")
					}
					for _, form := range []string{token, url.QueryEscape(token), url.PathEscape(token)} {
						if strings.Contains(err.Error(), form) {
							t.Fatalf("error exposed the token: %v", err)
						}
					}
					if !strings.Contains(err.Error(), "[redacted]") {
						t.Fatalf("error = %v, want the echoed token redacted", err)
					}
				}
			})
		}
	}
}

func TestParseRepoURL(t *testing.T) {
	tests := []struct {
		in   string
		want string // "" for not a GitHub repository
	}{
		{"https://github.com/pallets/click", "pallets/click"},
		{"https://github.com/pallets/click/", "pallets/click"},
		{"HTTPS://GitHub.com/Pallets/Click", "pallets/click"},
		{"http://github.com/haaksmash/solid", "haaksmash/solid"},
		{"https://www.github.com/saurvs/astro-rust", "saurvs/astro-rust"},
		{"git+https://github.com/react/react.git", "react/react"},
		{"git+https://github.com/vercel/next.js.git", "vercel/next.js"},
		{"git://github.com/uggedal/click.git", "uggedal/click"},
		{"git+ssh://git@github.com/o/r.git", "o/r"},
		{"ssh://git@github.com:22/o/r.git", "o/r"},
		{"git@github.com:vercel/next.js.git", "vercel/next.js"},
		{"github:solidjs/solid", "solidjs/solid"},
		{"solidjs/solid", "solidjs/solid"},
		{"github.com/spf13/cobra", "spf13/cobra"},
		{"https://github.com/request/request#readme", "request/request"},
		{"https://github.com/databricks/click/wiki", "databricks/click"},
		{"https://github.com/o/r/tree/main/packages/x", "o/r"},
		{"https://github.com/o/r.git/", "o/r"},
		{"https://github.com/o/r?tab=readme", "o/r"},
		{"https://github.com/dheerajmpai/saenews/archive/v_033.tar.gz", "dheerajmpai/saenews"},
		{"  https://github.com/pallets/click  ", "pallets/click"},
		// Not GitHub repositories.
		{"https://gitlab.com/imp/requests-rs.git", ""},
		{"gitlab:group/project", ""},
		{"bitbucket:o/r", ""},
		{"gist:11081aaa281", ""},
		{"docs.rs/clap", ""},
		{"https://docs.rs/clap", ""},
		{"https://solidjs.github.io/solid", ""},
		{"http://justintulloss.github.com/cobra", ""},
		{"https://gist.github.com/o/abc", ""},
		{"https://github.com/sponsors/someone", ""},
		{"https://github.com/orgs/pallets/repositories", ""},
		{"https://github.com/pallets", ""},
		{"https://github.com/", ""},
		{"ftp://github.com/o/r", ""},
		{"o/r/x", ""},
		{"UNKNOWN", ""},
		{"", ""},
		{"git@gitlab.com:o/r.git", ""},
		{"https://github.com/-bad/r", ""},
	}
	for _, tt := range tests {
		key, ok := parseRepoURL(tt.in)
		got := ""
		if ok {
			got = key.String()
		}
		if got != tt.want {
			t.Errorf("parseRepoURL(%q) = %q, %v; want %q", tt.in, got, ok, tt.want)
		}
	}
}
