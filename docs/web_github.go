// Owner: chunk A (inputs and sources).

package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/1broseidon/ketch/httpx"
)

const githubAPIBase = "https://api.github.com"

// ghSearchPage is GitHub's default search page size: searchRepos asks for
// more only when n exceeds it, so the request URL does not depend on n.
const ghSearchPage = 30

// ghClient reads repository metadata from the GitHub REST API. It
// implements repoSource. Requests carry X-GitHub-Api-Version: 2022-11-28
// and, when token returns one, Authorization: Bearer. token is called on
// the first request, never at construction: resolving it can run
// `gh auth token`, and providers are constructed with no config just to
// probe capabilities. Errors never include the token.
type ghClient struct {
	client  *http.Client
	base    string        // API base, no trailing slash
	token   func() string // "" for unauthenticated requests
	timeout time.Duration // per API call; 0 is apiCallTimeout
}

// newGHClient returns a client for api.github.com. A nil token means
// unauthenticated. It performs no I/O and does not call token.
func newGHClient(client *http.Client, token func() string) *ghClient {
	if token == nil {
		token = func() string { return "" }
	}
	return &ghClient{client: client, base: githubAPIBase, token: token, timeout: apiCallTimeout}
}

var _ repoSource = (*ghClient)(nil)

// ghRepoJSON is the fields of a GitHub repository object that repo and
// searchRepos read.
type ghRepoJSON struct {
	FullName    string  `json:"full_name"`
	Homepage    *string `json:"homepage"`
	Description *string `json:"description"`
	Fork        bool    `json:"fork"`
	Archived    bool    `json:"archived"`
	Stars       int     `json:"stargazers_count"`
	Language    *string `json:"language"`
	Parent      *struct {
		FullName string `json:"full_name"`
	} `json:"parent"`
}

// info converts r, with false when it names no repository.
func (r ghRepoJSON) info() (repoInfo, bool) {
	key, ok := ghFullName(r.FullName)
	if !ok {
		return repoInfo{}, false
	}
	info := repoInfo{key: key, fork: r.Fork, archived: r.Archived, stars: r.Stars}
	if r.Homepage != nil {
		info.homepage = strings.TrimSpace(*r.Homepage)
	}
	if r.Description != nil {
		info.description = strings.TrimSpace(*r.Description)
	}
	if r.Language != nil {
		info.language = strings.TrimSpace(*r.Language)
	}
	if r.Parent != nil {
		info.parent, _ = ghFullName(r.Parent.FullName)
	}
	return info, true
}

// ghFullName parses "Owner/Name" into a lower-cased key.
func ghFullName(full string) (repoKey, bool) {
	owner, name, ok := strings.Cut(strings.TrimSpace(full), "/")
	if !ok || owner == "" || name == "" {
		return repoKey{}, false
	}
	return repoKey{owner: strings.ToLower(owner), name: strings.ToLower(name)}, true
}

// repo fetches one repository, following renames; see repoSource. A
// renamed repository answers 301 to /repositories/<id>, which the client
// follows (same host, so the token goes along); the key returned is the
// canonical full_name.
func (g *ghClient) repo(ctx context.Context, key repoKey) (repoInfo, error) {
	if !isOwnerRepo(key.String()) {
		return repoInfo{}, fmt.Errorf("github: %q is not a repository: %w", key.String(), ErrNotFound)
	}
	var r ghRepoJSON
	p := "/repos/" + url.PathEscape(key.owner) + "/" + url.PathEscape(key.name)
	if err := g.get(ctx, "repository "+key.String(), p, nil, &r); err != nil {
		return repoInfo{}, err
	}
	info, ok := r.info()
	if !ok {
		return repoInfo{}, fmt.Errorf("github repository %s: response names no repository", key)
	}
	return info, nil
}

// searchRepos searches repositories by name; see repoSource: GitHub's
// in:name search sorted by stars, most first, at most n (n <= 0 is one
// page). A name with characters no repository name has (which could add
// search qualifiers) finds nothing, without a request.
func (g *ghClient) searchRepos(ctx context.Context, name string, n int) ([]repoInfo, error) {
	name = strings.TrimSpace(name)
	if !ghRepoRe.MatchString(name) {
		return nil, nil
	}
	q := url.Values{"q": {name + " in:name"}, "sort": {"stars"}}
	if n > ghSearchPage {
		q.Set("per_page", strconv.Itoa(min(n, 100)))
	}
	var sr struct {
		Items []ghRepoJSON `json:"items"`
	}
	if err := g.get(ctx, "search "+name, "/search/repositories", q, &sr); err != nil {
		return nil, err
	}
	var out []repoInfo
	for _, it := range sr.Items {
		if info, ok := it.info(); ok {
			out = append(out, info)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].stars > out[j].stars })
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// get makes one API request within the per-call budget and decodes a 200
// body into v. what names the request in a not-found error. Every error
// passes through a redactor for the token, wherever an upstream may have
// echoed it (a body, a redirect's Location, a failed hop's URL).
func (g *ghClient) get(ctx context.Context, what, path string, query url.Values, v any) error {
	timeout := g.timeout
	if timeout <= 0 {
		timeout = apiCallTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	u := g.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("github request failed: %w", err)
	}
	token := ""
	if g.token != nil {
		token = g.token()
	}
	var redact *httpx.Redactor // nil redacts nothing
	if token != "" {
		redact = httpx.NewValueRedactor(token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", webUserAgent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := orDefaultClient(g.client).Do(req)
	if err != nil {
		return redact.Error(fmt.Errorf("github request failed: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()
	if err := ghStatusError(resp, what, token != "", redact); err != nil {
		return err
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, pkgMaxBody)).Decode(v); err != nil {
		return redact.Error(fmt.Errorf("failed to decode github response: %w", err))
	}
	return nil
}

// ghStatusError classifies a non-200 response: 404 wraps ErrNotFound, a
// rate limit is a *ghRateLimitError, 401 is a rejected token; anything
// else quotes a redacted excerpt of the body, as code/github.go does.
func ghStatusError(resp *http.Response, what string, authed bool, redact *httpx.Redactor) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("github %s: %w", what, ErrNotFound)
	case http.StatusUnauthorized:
		return fmt.Errorf("github: invalid token (check: gh auth status)")
	}
	body := strings.TrimSpace(redact.Excerpt(resp.Body, 512))
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		if ghRateLimited(resp, body) {
			return &ghRateLimitError{reset: ghResetTime(resp.Header), authed: authed}
		}
	}
	return fmt.Errorf("github returned status %d: %s", resp.StatusCode, body)
}

// ghRateLimited tells GitHub's rate limits from other refusals: a 429, an
// exhausted X-RateLimit-Remaining, a Retry-After (secondary limits), or a
// message that says so.
func ghRateLimited(resp *http.Response, body string) bool {
	return resp.StatusCode == http.StatusTooManyRequests ||
		resp.Header.Get("X-RateLimit-Remaining") == "0" ||
		resp.Header.Get("Retry-After") != "" ||
		strings.Contains(strings.ToLower(body), "rate limit")
}

// ghResetTime is when a rate limit ends: X-RateLimit-Reset (Unix
// seconds), else Retry-After (seconds from now); zero when neither says.
func ghResetTime(h http.Header) time.Time {
	if n, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil && n > 0 {
		return time.Unix(n, 0)
	}
	if n, err := strconv.Atoi(h.Get("Retry-After")); err == nil && n >= 0 {
		return time.Now().Add(time.Duration(n) * time.Second)
	}
	return time.Time{}
}

// ghRateLimitError is GitHub refusing a request for its rate limit (403 or
// 429 with X-RateLimit-Remaining: 0, or a secondary limit).
type ghRateLimitError struct {
	reset  time.Time // when the limit resets; zero when GitHub did not say
	authed bool      // whether the request carried a token
}

// Error is "github: rate limited", with the token hint when the request
// had no token and the reset time when GitHub gave one.
func (e *ghRateLimitError) Error() string {
	msg := "github: rate limited"
	if !e.authed {
		msg += " (set github_token or gh auth login)"
	}
	if !e.reset.IsZero() {
		wait := max(time.Until(e.reset).Round(time.Second), 0)
		msg += fmt.Sprintf(". Resets in %s at %s", wait, e.reset.Format("15:04:05"))
	}
	return msg
}

// ghReservedOwners are github.com paths that are site pages, not
// accounts: github.com/sponsors/x is no repository.
var ghReservedOwners = map[string]bool{
	"about": true, "account": true, "apps": true, "collections": true, "contact": true, "customer-stories": true,
	"dashboard": true, "enterprise": true, "events": true, "explore": true, "features": true, "issues": true,
	"join": true, "login": true, "marketplace": true, "new": true, "notifications": true, "orgs": true,
	"organizations": true, "pricing": true, "pulls": true, "search": true, "security": true, "settings": true,
	"site": true, "sponsors": true, "topics": true, "trending": true, "users": true,
}

// parseRepoURL extracts a GitHub repository from a package's repository
// or homepage value: https, git+https, git://, ssh git@github.com:o/r,
// github:o/r and bare o/r forms. It strips .git, /tree/..., /wiki and
// similar suffixes and lower-cases the key; ok is false for anything that
// is not a GitHub repository.
//
// A scheme-less value whose first element has a dot is a host
// (github.com/o/r, but docs.rs/clap is not GitHub); without one it is the
// npm shorthand, exactly owner/repo.
func parseRepoURL(raw string) (repoKey, bool) {
	s := strings.TrimSpace(raw)
	if len(s) > 4 && strings.EqualFold(s[:4], "git+") {
		s = s[4:]
	}
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "github:"):
		return repoKeyFromPath(s[len("github:"):], true)
	case strings.HasPrefix(lower, "git@"):
		host, p, ok := strings.Cut(s[len("git@"):], ":")
		if !ok || !isGitHubHost(host) {
			return repoKey{}, false
		}
		return repoKeyFromPath(p, false)
	case strings.Contains(s, "://"):
		u, err := url.Parse(s)
		if err != nil || !isGitHubHost(u.Hostname()) || !isRepoScheme(u.Scheme) {
			return repoKey{}, false
		}
		return repoKeyFromPath(u.Path, false)
	}
	first, rest, _ := strings.Cut(s, "/")
	if strings.ContainsAny(first, ".:") {
		if !isGitHubHost(first) {
			return repoKey{}, false
		}
		return repoKeyFromPath(rest, false)
	}
	return repoKeyFromPath(s, true)
}

// repoKeyFromPath reads owner/repo from the start of a URL path, ignoring
// a query, a fragment and anything after the repository (/tree/main,
// /wiki, /issues). exact asks for nothing after it, as for shorthand.
func repoKeyFromPath(p string, exact bool) (repoKey, bool) {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 || (exact && len(parts) != 2) {
		return repoKey{}, false
	}
	owner, name := parts[0], strings.TrimSuffix(parts[1], ".git")
	if !isOwnerRepo(owner+"/"+name) || ghReservedOwners[strings.ToLower(owner)] {
		return repoKey{}, false
	}
	return repoKey{owner: strings.ToLower(owner), name: strings.ToLower(name)}, true
}

func isGitHubHost(host string) bool {
	host = strings.ToLower(host)
	return host == "github.com" || host == "www.github.com"
}

func isRepoScheme(s string) bool {
	switch strings.ToLower(s) {
	case "http", "https", "git", "ssh":
		return true
	}
	return false
}
