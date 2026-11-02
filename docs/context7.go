package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// Context7 searches library documentation via the Context7 API. It works
// with no key on Context7's anonymous quota (rate limited per IP, about 200
// requests a month at the time of writing); a key sends the same requests
// authenticated and lifts the cap.
type Context7 struct {
	apiKey string
	client *http.Client
	// quota is the last rate limit Context7 reported for this key (or for
	// anonymous use), shared by every client in the process with the same
	// key so a long-lived MCP server stops asking once it is spent. nil
	// tracks nothing between calls.
	quota *context7QuotaTracker
}

// context7Base is the Context7 API root.
const context7Base = "https://context7.com"

// NewContext7 creates a new Context7 docs backend. An empty apiKey uses the
// anonymous quota: no Authorization header is sent.
func NewContext7(apiKey string) *Context7 {
	return &Context7{
		apiKey: apiKey,
		client: httpx.Default(),
		quota:  sharedContext7Quota(apiKey),
	}
}

// LibraryMatch is a resolved library from Context7's search endpoint.
// Field names track the upstream schema: "title" (not "name"),
// "totalSnippets" (not "codeSnippets"), and "trustScore" (numeric, not
// the older string "trust").
//
// Version, Evidence and Packages belong to the web backend. Context7 never
// sets them and they are omitted when empty, so Context7's JSON does not
// change.
type LibraryMatch struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	TotalSnippets int      `json:"totalSnippets"`
	TrustScore    float64  `json:"trustScore"`
	Versions      []string `json:"versions"`
	// Version is the version the ID's docs are for: the requested one, else
	// the site's current one, as a concrete label rather than an alias.
	Version string `json:"version,omitempty"`
	// Evidence says, one finding per entry, why the ID is this library's
	// docs ("pypi click lists it as Documentation").
	Evidence []string `json:"evidence,omitempty"`
	// Packages are the library's package and repository specs, each usable
	// as the library (pypi:click, github:pallets/click).
	Packages []string `json:"packages,omitempty"`
}

type context7SearchResponse struct {
	Results []LibraryMatch `json:"results"`
}

type context7DocsResponse struct {
	CodeSnippets []context7CodeSnippet `json:"codeSnippets"`
	InfoSnippets []context7InfoSnippet `json:"infoSnippets"`
}

type context7CodeSnippet struct {
	CodeTitle       string              `json:"codeTitle"`
	CodeDescription string              `json:"codeDescription"`
	CodeLanguage    string              `json:"codeLanguage"`
	CodeID          string              `json:"codeId"`
	CodeList        []context7CodeEntry `json:"codeList"`
}

type context7CodeEntry struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

type context7InfoSnippet struct {
	PageID     string `json:"pageId"`
	Breadcrumb string `json:"breadcrumb"`
	Content    string `json:"content"`
}

// context7ErrorResponse is the body Context7 sends alongside some 4xx
// responses. A 404 with error "no_relevant_snippets" means the library
// exists but nothing matched the query — a normal empty result, not a
// missing library. A 301 with error "library_redirected" names the
// library's new ID in redirectUrl (/facebook/react moved to /react/react);
// it carries no Location header, so the HTTP client does not follow it.
type context7ErrorResponse struct {
	Error       string `json:"error"`
	RedirectURL string `json:"redirectUrl"`
}

// Search resolves a library from the query and fetches documentation,
// returning at most limit results when limit is positive. The token budget
// bounds what Context7 sends back; limit bounds what the caller sees, so
// --limit and the MCP limit argument mean the same thing here as on every
// other surface.
func (c *Context7) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	return c.search(ctx, query, limit, nil)
}

// search is Search with a check on the library Context7 picks: accept, when
// set, sees the top-ranked match before any docs are fetched, and an error
// from it ends the search. The auto backend uses it to hold Context7 to the
// candidates the web backend found.
func (c *Context7) search(ctx context.Context, query string, limit int, accept func(LibraryMatch) error) ([]Result, error) {
	results, err := c.docsFor(ctx, query, query, 4000, accept)
	if err != nil {
		return nil, err
	}
	return Truncate(results, limit), nil
}

// ResolveLibrary searches Context7 for libraries matching the given name,
// returning at most limit matches (limit <= 0 means all). The Context7 search
// endpoint has no server-side limit parameter, so the bound is applied here —
// in the shared layer — so the CLI and MCP surfaces cannot diverge.
func (c *Context7) ResolveLibrary(ctx context.Context, name string, limit int) ([]LibraryMatch, error) {
	// Upstream param is `query` (was `q` on an older revision — API returns
	// 400 "Query is required" if we send `q`).
	resp, err := c.get(ctx, "resolve", context7Base+"/api/v1/search?query="+url.QueryEscape(name))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// A name Context7 has moved answers with the new ID: search for that
	// once instead.
	if to, ok := context7Redirect(resp); ok {
		resp.Body.Close()
		resp, err = c.get(ctx, "resolve", context7Base+"/api/v1/search?query="+url.QueryEscape(to))
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if again, ok := context7Redirect(resp); ok {
			return nil, fmt.Errorf("context7: %q redirected to %q, which redirected again to %q", name, to, again)
		}
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("context7: invalid API key (set via: ketch config set context7_api_key <key>)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("context7 resolve returned status %d", resp.StatusCode)
	}

	var body context7SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to decode context7 resolve response: %w", err)
	}
	if limit > 0 && len(body.Results) > limit {
		body.Results = body.Results[:limit]
	}
	return body.Results, nil
}

// GetDocs fetches documentation snippets for a resolved library ID. A
// library Context7 has moved (library_redirected) is followed once, and the
// results carry the ID it moved to as their Library.
func (c *Context7) GetDocs(ctx context.Context, libraryID, query string, tokens int) ([]Result, error) {
	resp, libraryID, err := c.getDocsFollowing(ctx, libraryID, query, tokens)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("context7: invalid API key (set via: ketch config set context7_api_key <key>)")
	}
	if resp.StatusCode == http.StatusNotFound {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("failed to read context7 docs response: %w", readErr)
		}
		var errBody context7ErrorResponse
		if json.Unmarshal(body, &errBody) == nil && errBody.Error == "no_relevant_snippets" {
			// The library exists but nothing matched this query — an empty
			// result, not a missing library.
			return nil, nil
		}
		// Any other 404 means the library ID does not exist — permanently
		// absent, not a transient upstream failure. Wrap the sentinel so both
		// surfaces map it to not-found instead of retryable-upstream.
		return nil, fmt.Errorf("context7: library %q %w (resolve the exact ID with: ketch docs --resolve <name>)", libraryID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("context7 docs returned status %d", resp.StatusCode)
	}

	var dr context7DocsResponse
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		return nil, fmt.Errorf("failed to decode context7 docs response: %w", err)
	}

	var results []Result

	for _, cs := range dr.CodeSnippets {
		snippet := ""
		if len(cs.CodeList) > 0 {
			snippet = cs.CodeList[0].Code
		}
		results = append(results, Result{
			Library: libraryID,
			Title:   cs.CodeTitle,
			Snippet: snippet,
			URL:     cs.CodeID,
			Source:  "context7",
		})
	}

	for _, is := range dr.InfoSnippets {
		results = append(results, Result{
			Library:    libraryID,
			Title:      is.Breadcrumb,
			Breadcrumb: is.Breadcrumb,
			Snippet:    is.Content,
			URL:        is.PageID,
			Source:     "context7",
		})
	}

	return results, nil
}

// getDocsFollowing requests libraryID's docs, following a
// library_redirected answer once. It returns the response and the ID it
// answers for.
func (c *Context7) getDocsFollowing(ctx context.Context, libraryID, query string, tokens int) (*http.Response, string, error) {
	resp, err := c.getDocs(ctx, libraryID, query, tokens)
	if err != nil {
		return nil, "", err
	}
	to, ok := context7Redirect(resp)
	if !ok {
		return resp, libraryID, nil
	}
	resp.Body.Close()
	if !strings.HasPrefix(to, "/") || to == libraryID {
		return nil, "", fmt.Errorf("context7: library %q redirected to %q, which is not another library ID", libraryID, to)
	}
	resp, err = c.getDocs(ctx, to, query, tokens)
	if err != nil {
		return nil, "", err
	}
	if again, ok := context7Redirect(resp); ok {
		resp.Body.Close()
		return nil, "", fmt.Errorf("context7: library %q redirected to %q, which redirected again to %q", libraryID, to, again)
	}
	return resp, to, nil
}

func (c *Context7) getDocs(ctx context.Context, libraryID, query string, tokens int) (*http.Response, error) {
	u := fmt.Sprintf(context7Base+"/api/v2/context?libraryId=%s&query=%s&type=json&tokens=%d",
		url.QueryEscape(libraryID), url.QueryEscape(query), tokens)
	return c.get(ctx, "docs", u)
}

// get sends one Context7 request: authenticated when there is a key, and
// with no Authorization header at all when there is not (the anonymous
// quota). It records the rate limit the response reports, and turns a
// spent quota into a *RateLimitError: a 429, an error status with
// ratelimit-remaining 0, or — without asking — a quota an earlier response
// in this process already reported spent, until it resets. op names the
// call in transport errors ("resolve", "docs").
func (c *Context7) get(ctx context.Context, op, u string) (*http.Response, error) {
	if err := c.quota.spent(c.apiKey != "", time.Now()); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("context7 %s request failed: %w", op, err)
	}
	q := parseContext7Quota(resp.Header, time.Now())
	c.quota.observe(q)
	if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 400 && q.known && q.remaining == 0) {
		health.Drain(resp)
		return nil, &RateLimitError{Keyed: c.apiKey != "", Limit: q.limit, Reset: q.reset}
	}
	return resp, nil
}

// context7Redirect reads a library_redirected response: a 3xx whose JSON
// body names the library's new ID. It reports the ID. Another 3xx has its
// body read and is left for the caller's status check; any other status is
// left unread.
func context7Redirect(resp *http.Response) (string, bool) {
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", false
	}
	var errBody context7ErrorResponse
	if json.Unmarshal(body, &errBody) != nil || errBody.Error != "library_redirected" || errBody.RedirectURL == "" {
		return "", false
	}
	return errBody.RedirectURL, true
}

// ProbeContext7 checks the provider using a caller-supplied client and
// endpoint. With a key it probes the API as it always has. With no key it
// sends nothing: every request spends the anonymous quota (about 200 a
// month per IP, 400s included), and `ketch doctor` runs often, so it
// reports the quota the last real Context7 response left in the state file
// (context7KeylessStatus).
func ProbeContext7(ctx context.Context, client *http.Client, apiBase, apiKey string) (health.Status, string) {
	if apiKey == "" {
		return context7KeylessStatus(defaultContext7State(), time.Now())
	}
	resp, err := health.Get(ctx, client, apiBase+"/api/v1/search?query=go", map[string]string{"Authorization": "Bearer " + apiKey})
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	defer health.Drain(resp)

	q := parseContext7Quota(resp.Header, time.Now())
	if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 400 && q.known && q.remaining == 0) {
		return health.StatusOK, "reachable, key accepted (rate limited)"
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return health.StatusOK, q.detail()
	case http.StatusUnauthorized:
		return health.StatusMisconfigured, "API key rejected (ketch config set context7_api_key <key>)"
	default:
		return health.StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

// context7KeyFix ends every keyless doctor detail.
const context7KeyFix = "a key lifts the cap: ketch config set context7_api_key <key>"

// context7KeylessStatus is keyless Context7's health from the state file
// alone: ok while the last quota seen has requests left or has since reset
// (or none was seen yet), no_key while it is spent, since a key is the fix.
func context7KeylessStatus(state *context7State, now time.Time) (health.Status, string) {
	q, ok := state.load(context7StateEntry(""))
	switch {
	case !ok:
		return health.StatusOK, "keyless (quota not yet seen; Context7 allows about 200 anonymous requests a month per IP; " + context7KeyFix + ")"
	case !q.reset.IsZero() && !now.Before(q.reset):
		return health.StatusOK, "keyless (quota renewed " + formatContext7Time(q.reset) + " since last seen; " + context7KeyFix + ")"
	case q.remaining == 0:
		return health.StatusNoKey, (&RateLimitError{Limit: q.limit, Reset: q.reset}).Error() + " (as of " + formatContext7Time(q.observedAt) + ")"
	}
	left := fmt.Sprintf("~%d", q.remaining)
	if q.limit > 0 {
		left += fmt.Sprintf(" of %d", q.limit)
	}
	detail := "keyless (" + left + " left as of " + formatContext7Time(q.observedAt)
	if !q.reset.IsZero() {
		detail += ", resets " + formatContext7Time(q.reset)
	}
	return health.StatusOK, detail + "; " + context7KeyFix + ")"
}

func context7Provider() Provider {
	return Provider{
		Settings: []config.Setting{{Key: "context7_api_key", ValidationOrder: 20, Secret: true, GateDoctor: true, FileOrder: 20, DiscoveryOrder: 23, EnvOrder: 14}},
		ID:       "context7",
		Name:     "Context7",
		// Keyless use runs on the anonymous quota, so Context7 needs no
		// setup; a key only lifts the cap.
		Usable: func(*config.Config) bool { return true },
		New: func(c *config.Config, _ Deps) (Searcher, error) {
			return NewContext7(c.String("context7_api_key")), nil
		},
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return ProbeContext7(ctx, client, context7Base, c.String("context7_api_key"))
		},
	}
}
