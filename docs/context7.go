package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// Context7 searches library documentation via the Context7 API.
type Context7 struct {
	apiKey string
	client *http.Client
}

// NewContext7 creates a new Context7 docs backend.
func NewContext7(apiKey string) *Context7 {
	return &Context7{
		apiKey: apiKey,
		client: httpx.Default(),
	}
}

// LibraryMatch is a resolved library from Context7's search endpoint.
// Field names track the upstream schema: "title" (not "name"),
// "totalSnippets" (not "codeSnippets"), and "trustScore" (numeric, not
// the older string "trust").
type LibraryMatch struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	TotalSnippets int      `json:"totalSnippets"`
	TrustScore    float64  `json:"trustScore"`
	Versions      []string `json:"versions"`
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

// Search resolves a library from the query and fetches documentation,
// returning at most limit results when limit is positive. The token budget
// bounds what Context7 sends back; limit bounds what the caller sees, so
// --limit and the MCP limit argument mean the same thing here as on every
// other surface.
func (c *Context7) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	// Only the top-ranked library is used, so resolve just that one.
	libs, err := c.ResolveLibrary(ctx, query, 1)
	if err != nil {
		return nil, fmt.Errorf("context7 resolve failed: %w", err)
	}
	if len(libs) == 0 {
		return nil, fmt.Errorf("context7: no library found for %q", query)
	}

	results, err := c.GetDocs(ctx, libs[0].ID, query, 4000)
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
	u := fmt.Sprintf("https://context7.com/api/v1/search?query=%s", url.QueryEscape(name))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("context7 resolve request failed: %w", err)
	}
	defer resp.Body.Close()

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

// GetDocs fetches documentation snippets for a resolved library ID.
func (c *Context7) GetDocs(ctx context.Context, libraryID, query string, tokens int) ([]Result, error) {
	u := fmt.Sprintf("https://context7.com/api/v2/context?libraryId=%s&query=%s&type=json&tokens=%d",
		url.QueryEscape(libraryID), url.QueryEscape(query), tokens)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("context7 docs request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("context7: invalid API key (set via: ketch config set context7_api_key <key>)")
	}
	if resp.StatusCode == http.StatusNotFound {
		// A 404 here means the library ID does not exist — permanently absent,
		// not a transient upstream failure. Wrap the sentinel so both surfaces
		// map it to not-found instead of retryable-upstream.
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

// ProbeContext7 checks the provider using a caller-supplied client and endpoint.
func ProbeContext7(ctx context.Context, client *http.Client, apiBase, apiKey string) (health.Status, string) {
	if apiKey == "" {
		return health.StatusNoKey, "API key not set (get one then: ketch config set context7_api_key <key>)"
	}
	resp, err := health.Get(ctx, client, apiBase+"/api/v1/search?query=go", map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	defer health.Drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return health.StatusOK, ""
	case http.StatusUnauthorized:
		return health.StatusMisconfigured, "API key rejected (ketch config set context7_api_key <key>)"
	default:
		return health.StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

func context7Provider() Provider {
	return Provider{Setup: "context7: API key not set (get one then: ketch config set context7_api_key <key>)", LibrarySetup: "context7: API key not set (set with: ketch config set context7_api_key <key>)",
		Settings: []config.Setting{{Key: "context7_api_key", ValidationOrder: 20, Secret: true, GateDoctor: true, FileOrder: 20, DiscoveryOrder: 23, EnvOrder: 14}},
		ID:       "context7",
		Name:     "Context7",
		Usable:   func(c *config.Config) bool { return c.String("context7_api_key") != "" },
		New:      func(c *config.Config) (Searcher, error) { return NewContext7(c.String("context7_api_key")), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return ProbeContext7(ctx, client, "https://context7.com", c.String("context7_api_key"))
		},
	}
}
