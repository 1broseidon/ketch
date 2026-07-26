package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/cookies"
	"github.com/1broseidon/ketch/scrape"
)

// Production endpoints. Probes take these as parameters so tests can point
// them at httptest servers.
const (
	braveEndpoint    = "https://api.search.brave.com/res/v1/web/search"
	ddgEndpoint      = "https://html.duckduckgo.com/html/"
	grepAppEndpoint  = "https://mcp.grep.app"
	exaMCPEndpoint   = "https://mcp.exa.ai/mcp"
	firecrawlSearch  = "https://api.firecrawl.dev/v2/search"
	keenableEndpoint = "https://api.keenable.ai"
	githubAPIBase    = "https://api.github.com"
	context7APIBase  = "https://context7.com"
)

// ddgUA mirrors the search backend's user agent — DDG's HTML endpoint rejects
// requests with a default Go client UA.
const ddgUA = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"

// exaEndpoint appends the API key query param exactly like the search backend
// does; keyless usage is supported.
func exaEndpoint(apiKey string) string {
	endpoint := exaMCPEndpoint
	if strings.TrimSpace(apiKey) != "" {
		v := url.Values{}
		v.Set("exaApiKey", strings.TrimSpace(apiKey))
		endpoint += "?" + v.Encode()
	}
	return endpoint
}

// probeKeyPool checks every effective key concurrently and reports the worst
// outcome. A rejected key therefore marks the whole pool misconfigured, even
// if another key succeeds. Details identify ordinals only and never values.
func probeKeyPool(keys []string, probe func(string) (Status, string)) (Status, string) {
	if len(keys) == 0 {
		return probe("")
	}
	type outcome struct {
		status Status
		detail string
	}
	outcomes := make([]outcome, len(keys))
	var wg sync.WaitGroup
	for i, key := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, detail := probe(key)
			outcomes[i] = outcome{status: status, detail: detail}
		}()
	}
	wg.Wait()

	worst := StatusOK
	for _, result := range outcomes {
		if doctorStatusPriority(result.status) > doctorStatusPriority(worst) {
			worst = result.status
		}
	}

	var details []string
	for i, result := range outcomes {
		if result.status != worst || result.detail == "" {
			continue
		}
		details = append(details, fmt.Sprintf("key %d of %d: %s", i+1, len(keys), result.detail))
	}
	if len(details) > 0 {
		return worst, strings.Join(details, "; ")
	}
	if worst == StatusOK && len(keys) > 1 {
		return StatusOK, fmt.Sprintf("%d keys accepted", len(keys))
	}
	return worst, ""
}

func doctorStatusPriority(status Status) int {
	switch status {
	case StatusMisconfigured:
		return 5
	case StatusUnreachable:
		return 4
	case StatusNoKey:
		return 3
	case StatusOK:
		return 2
	case StatusSkipped:
		return 1
	default:
		return 0
	}
}

// get performs a GET with the given headers and returns the response with its
// body unread. A nil response means the request itself failed.
func get(ctx context.Context, client *http.Client, u string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return client.Do(req)
}

// drain discards a bounded amount of body and closes it, so connections can
// be reused without buffering large responses.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
}

// probeBrave checks the Brave Search API with a minimal one-result query.
func probeBrave(ctx context.Context, client *http.Client, endpoint, apiKey string) (Status, string) {
	if apiKey == "" {
		return StatusNoKey, "API key not set (get one free at https://brave.com/search/api/ then: ketch config set brave_api_key <key>)"
	}
	resp, err := get(ctx, client, endpoint+"?q=ketch&count=1&text_decorations=false&result_filter=web", map[string]string{
		"Accept":               "application/json",
		"X-Subscription-Token": apiKey,
	})
	if err != nil {
		return StatusUnreachable, probeErrDetail(err)
	}
	defer drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		return StatusMisconfigured, "API key rejected (ketch config set brave_api_key <key>)"
	case http.StatusTooManyRequests:
		return StatusOK, "reachable, key accepted (rate limited)"
	default:
		return StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

// probeFirecrawl checks the Firecrawl v2 search API with a minimal one-result
// query. Firecrawl requires an API key, so a missing key is a clean no_key.
func probeFirecrawl(ctx context.Context, client *http.Client, endpoint, apiKey string) (Status, string) {
	if apiKey == "" {
		return StatusNoKey, "API key not set (get one free at https://firecrawl.dev then: ketch config set firecrawl_api_key <key>)"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(`{"query":"ketch","limit":1}`))
	if err != nil {
		return StatusUnreachable, probeErrDetail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return StatusUnreachable, probeErrDetail(err)
	}
	defer drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusPaymentRequired:
		return StatusMisconfigured, "API key rejected (ketch config set firecrawl_api_key <key>)"
	case http.StatusTooManyRequests:
		return StatusOK, "reachable, key accepted (rate limited)"
	default:
		return StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

// probeKeenable checks the Keenable search API with a minimal query. Keenable
// is keyless by default (public endpoint); a configured key switches to the
// authenticated endpoint, so a rejected key surfaces as misconfigured.
func probeKeenable(ctx context.Context, client *http.Client, base, apiKey string) (Status, string) {
	key := strings.TrimSpace(apiKey)
	path := "/v1/search/public"
	if key != "" {
		path = "/v1/search"
	}
	body := strings.NewReader(`{"query":"ketch","mode":"pro"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, body)
	if err != nil {
		return StatusUnreachable, probeErrDetail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Keenable-Title", "Ketch")
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return StatusUnreachable, probeErrDetail(err)
	}
	defer drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		return StatusMisconfigured, "API key rejected (ketch config set keenable_api_key <key>)"
	case http.StatusTooManyRequests:
		return StatusOK, "reachable (rate limited; set a key to lift the cap)"
	default:
		return StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

// probeDDG checks DuckDuckGo's HTML endpoint with a minimal query.
func probeDDG(ctx context.Context, client *http.Client, endpoint string) (Status, string) {
	resp, err := get(ctx, client, endpoint+"?q=ketch", map[string]string{"User-Agent": ddgUA})
	if err != nil {
		return StatusUnreachable, probeErrDetail(err)
	}
	defer drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return StatusOK, ""
	case http.StatusAccepted:
		return StatusOK, "reachable (rate limited)"
	default:
		return StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

// probeDegoog checks a degoog instance via the same /api/search JSON call
// ketch uses.
func probeDegoog(ctx context.Context, client *http.Client, baseURL string) (Status, string) {
	if baseURL == "" {
		return StatusMisconfigured, "degoog_url not set (ketch config set degoog_url <url>)"
	}
	resp, err := get(ctx, client, baseURL+"/api/search?q=ketch", nil)
	if err != nil {
		return StatusUnreachable, probeErrDetail(err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var body struct {
			Results []json.RawMessage `json:"results"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
			return StatusMisconfigured, fmt.Sprintf("returned non-JSON response — is %s a degoog instance?", baseURL)
		}
		return StatusOK, ""
	case http.StatusTooManyRequests:
		return StatusMisconfigured, "rate limited (HTTP 429) — the degoog instance is throttling ketch"
	default:
		return StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

// probeSearxng checks a SearXNG instance via the same format=json search call
// ketch uses. It specifically detects the stock-config trap where the JSON
// format is disabled: SearXNG returns 403 for format=json unless settings.yml
// enables it.
func probeSearxng(ctx context.Context, client *http.Client, baseURL string) (Status, string) {
	if baseURL == "" {
		return StatusMisconfigured, "searxng_url not set (ketch config set searxng_url <url>)"
	}
	resp, err := get(ctx, client, baseURL+"/search?q=ketch&format=json&pageno=1", nil)
	if err != nil {
		return StatusUnreachable, probeErrDetail(err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var body struct {
			Results []json.RawMessage `json:"results"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
			return StatusMisconfigured, fmt.Sprintf("returned non-JSON response — is %s a SearXNG instance?", baseURL)
		}
		return StatusOK, ""
	case http.StatusForbidden:
		return StatusMisconfigured, `format=json is blocked (HTTP 403) — enable it in the instance's settings.yml: search.formats must include "json", then restart SearXNG`
	case http.StatusTooManyRequests:
		return StatusMisconfigured, "rate limited (HTTP 429) — the SearXNG limiter is throttling ketch; consider disabling the limiter for local instances"
	default:
		return StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

// probeMCP checks a hosted MCP endpoint (grep.app, Exa) with a tools/list
// call — the cheapest request those servers answer.
func probeMCP(ctx context.Context, client *http.Client, endpoint, name string) (Status, string) {
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return StatusUnreachable, probeErrDetail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		return StatusUnreachable, probeErrDetail(err)
	}
	defer drain(resp)

	if resp.StatusCode == http.StatusOK {
		return StatusOK, ""
	}
	return StatusUnreachable, fmt.Sprintf("%s returned status %d", name, resp.StatusCode)
}

// probeExa validates Exa credentials explicitly; unlike generic hosted MCP
// reachability, 401/403 means a configured key was rejected and 429 confirms
// that the credential reached the service.
func probeExa(ctx context.Context, client *http.Client, endpoint string, keyed bool) (Status, string) {
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return StatusUnreachable, exaProbeErrDetail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		return StatusUnreachable, exaProbeErrDetail(err)
	}
	defer drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		if keyed {
			return StatusMisconfigured, "API key rejected (ketch config set exa_api_key <key>)"
		}
		return StatusUnreachable, fmt.Sprintf("exa returned status %d", resp.StatusCode)
	case http.StatusTooManyRequests:
		return StatusOK, "reachable, key accepted (rate limited)"
	default:
		return StatusUnreachable, fmt.Sprintf("exa returned status %d", resp.StatusCode)
	}
}

// exaProbeErrDetail never renders the original error: net/http transport
// errors include the request URL, whose query string carries the Exa API key.
func exaProbeErrDetail(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "request failed: cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "request failed: timed out"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "request failed: timed out"
	}
	return "request failed: transport error"
}

// probeReachable checks that a base URL answers at all (used for Sourcegraph,
// where any HTTP response — including auth walls on private instances — means
// the instance is up).
func probeReachable(ctx context.Context, client *http.Client, baseURL, name string) (Status, string) {
	resp, err := get(ctx, client, baseURL, nil)
	if err != nil {
		return StatusUnreachable, probeErrDetail(err)
	}
	defer drain(resp)

	if resp.StatusCode >= http.StatusInternalServerError {
		return StatusUnreachable, fmt.Sprintf("%s returned status %d", name, resp.StatusCode)
	}
	return StatusOK, ""
}

// probeGitHub resolves a token through the standard chain (config →
// $GITHUB_TOKEN/$GH_TOKEN → gh CLI) and, only when one resolves, validates it
// against /rate_limit — an authed call that costs no search quota.
func probeGitHub(ctx context.Context, client *http.Client, apiBase string, resolve func() (token, source string)) (Status, string) {
	token, source := resolve()
	if token == "" {
		return StatusNoKey, "no token (ketch config set github_token <token>, $GITHUB_TOKEN, or gh auth login)"
	}
	resp, err := get(ctx, client, apiBase+"/rate_limit", map[string]string{
		"Authorization":        "Bearer " + token,
		"X-GitHub-Api-Version": "2022-11-28",
	})
	if err != nil {
		return StatusUnreachable, probeErrDetail(err)
	}
	defer drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return StatusOK, "token via " + source
	case http.StatusUnauthorized:
		return StatusMisconfigured, fmt.Sprintf("token rejected (source: %s; check: gh auth status)", source)
	default:
		return StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

// probeContext7 checks the Context7 resolve endpoint with a minimal query.
func probeContext7(ctx context.Context, client *http.Client, apiBase, apiKey string) (Status, string) {
	if apiKey == "" {
		return StatusNoKey, "API key not set (get one then: ketch config set context7_api_key <key>)"
	}
	resp, err := get(ctx, client, apiBase+"/api/v1/search?query=go", map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if err != nil {
		return StatusUnreachable, probeErrDetail(err)
	}
	defer drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return StatusOK, ""
	case http.StatusUnauthorized:
		return StatusMisconfigured, "API key rejected (ketch config set context7_api_key <key>)"
	default:
		return StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

// checkBrowser verifies the configured browser binary actually resolves to a
// file on disk or PATH. No browser configured is a clean skip — rendering is
// optional.
func checkBrowser(configured string) (Status, string) {
	if configured == "" {
		return StatusSkipped, "not configured (browser rendering disabled; optional)"
	}
	bin, err := scrape.ResolveBrowserBin(configured)
	if err != nil {
		return StatusMisconfigured, err.Error()
	}
	return StatusOK, bin
}

// checkCookieFile loads the configured jar and reports counts only — cookie
// names and values never appear in doctor output.
func checkCookieFile(path string) (Status, string) {
	if path == "" {
		return StatusSkipped, "not configured (cookie injection disabled; optional)"
	}
	jar, err := cookies.Load(path)
	if err != nil {
		return StatusMisconfigured, fmt.Sprintf("cannot load cookie file: %v (fix the path or re-export cookies.txt)", err)
	}
	detail := fmt.Sprintf("configured (%d cookies, %d expired)", jar.Len(), jar.Expired)
	if cookiePermsLoose(path) {
		detail += "; file is group/world-readable — chmod 600"
	}
	return StatusOK, detail
}

func cookiePermsLoose(path string) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	info, err := os.Stat(cookies.ExpandPath(path))
	return err == nil && info.Mode().Perm()&0o044 != 0
}

// checkCache verifies the cache directory is writable and reports entry
// count, size, and lock state via the existing read-only stats path. It never
// opens the database for writing and never touches cache entries.
func checkCache() (Status, string) {
	path, err := cache.DBPath()
	if err != nil {
		return StatusMisconfigured, fmt.Sprintf("cannot resolve cache path: %v", err)
	}
	// Writability check on the directory, not the database: a throwaway temp
	// file, removed immediately. cache.db itself is never opened for writing.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".doctor-*")
	if err != nil {
		return StatusMisconfigured, fmt.Sprintf("cache dir not writable: %v", err)
	}
	_ = tmp.Close()
	_ = os.Remove(tmp.Name())

	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return StatusOK, fmt.Sprintf("writable, empty (no cache database yet at %s)", path)
	}
	if c := cache.NewReadOnly(); c != nil {
		defer c.Close()
		entries, size := c.Stats()
		return StatusOK, fmt.Sprintf("%d entries, %s", entries, formatBytes(size))
	}
	// The database exists but a read-only open failed: another process (e.g. a
	// background crawl) holds the lock. That is healthy, just report it.
	var size int64
	if st, err := os.Stat(path); err == nil {
		size = st.Size()
	}
	return StatusOK, fmt.Sprintf("locked by another process (%s)", formatBytes(size))
}

// probeErrDetail compacts a transport error into a single-line detail.
func probeErrDetail(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return err.Error()
}

// formatBytes renders a byte count in the same style as `ketch cache`.
func formatBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
