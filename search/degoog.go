package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// Degoog searches a self-hosted degoog instance (https://github.com/degoog-org/degoog)
// through its GET /api/search JSON route. Like SearXNG it is a meta-search
// aggregator the operator runs themselves; unlike SearXNG it has no default
// instance URL, so the backend is opt-in via degoog_url.
type Degoog struct {
	baseURL string
	client  *http.Client
}

// NewDegoog creates a new degoog search backend for the instance at baseURL.
func NewDegoog(baseURL string) *Degoog {
	return &Degoog{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client:  httpx.Default(),
	}
}

type degoogResponse struct {
	Results []degoogResult `json:"results"`
}

type degoogResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// Search queries degoog and returns up to limit results.
func (d *Degoog) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return []Result{}, nil
	}
	u := fmt.Sprintf("%s/api/search?q=%s", d.baseURL, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("degoog request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("degoog returned status %d", resp.StatusCode)
	}

	var dr degoogResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&dr); err != nil {
		return nil, fmt.Errorf("failed to decode degoog response: %w", err)
	}

	results := make([]Result, 0, limit)
	for _, r := range dr.Results {
		if len(results) >= limit {
			break
		}
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		results = append(results, Result{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Snippet,
		})
	}
	return results, nil
}

// ProbeDegoog checks a degoog instance with the same /api/search JSON call
// ketch uses. An unset URL is misconfigured, as for SearXNG: when the backend
// is selected that blocks doctor, otherwise it is an advisory problem.
func ProbeDegoog(ctx context.Context, client *http.Client, baseURL string) (health.Status, string) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return health.StatusMisconfigured, "degoog_url not set (ketch config set degoog_url <url>)"
	}
	resp, err := health.Get(ctx, client, baseURL+"/api/search?q=ketch", map[string]string{"Accept": "application/json"})
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	defer health.Drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		var body struct {
			Results []json.RawMessage `json:"results"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
			return health.StatusMisconfigured, fmt.Sprintf("returned non-JSON response — is %s a degoog instance?", baseURL)
		}
		return health.StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		return health.StatusMisconfigured, fmt.Sprintf("returned status %d — the instance requires an API key for /api/search, which ketch does not send", resp.StatusCode)
	case http.StatusTooManyRequests:
		return health.StatusMisconfigured, "rate limited (HTTP 429) — the degoog instance is throttling ketch"
	default:
		return health.StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

// Ported from PR #29 by @wonderbeel onto the provider registry.
func degoogProvider() Provider {
	instance := config.Scalar("degoog_url")
	instance.GateDoctor = true // an explicitly configured instance must answer doctor
	return Provider{
		Settings: []config.Setting{instance},
		ID:       "degoog",
		Name:     "Degoog",
		Setup:    "degoog: instance URL not set (ketch config set degoog_url http://localhost:4444)",
		Usable:   func(c *config.Config) bool { return strings.TrimSpace(c.String("degoog_url")) != "" },
		New:      func(c *config.Config) (Searcher, error) { return NewDegoog(c.String("degoog_url")), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return ProbeDegoog(ctx, client, c.String("degoog_url"))
		},
	}
}
