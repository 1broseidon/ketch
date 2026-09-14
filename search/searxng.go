package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// SearXNG searches a SearXNG instance via its JSON API.
type SearXNG struct {
	baseURL string
	client  *http.Client
}

// NewSearXNG creates a new SearXNG search backend.
func NewSearXNG(baseURL string) *SearXNG {
	return &SearXNG{
		baseURL: baseURL,
		client:  httpx.Default(),
	}
}

type searxngResponse struct {
	Results []searxngResult `json:"results"`
}

type searxngResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

// Search queries SearXNG and returns up to limit results.
func (s *SearXNG) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	u := fmt.Sprintf("%s/search?q=%s&format=json&pageno=1", s.baseURL, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng returned status %d", resp.StatusCode)
	}

	var sr searxngResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("failed to decode searxng response: %w", err)
	}

	results := make([]Result, 0, limit)
	for _, r := range sr.Results {
		if len(results) >= limit {
			break
		}
		results = append(results, Result{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Content,
		})
	}

	return results, nil
}

// ProbeSearxng checks the provider using a caller-supplied client and endpoint.
func ProbeSearxng(ctx context.Context, client *http.Client, baseURL string) (health.Status, string) {
	if baseURL == "" {
		return health.StatusMisconfigured, "searxng_url not set (ketch config set searxng_url <url>)"
	}
	resp, err := health.Get(ctx, client, baseURL+"/search?q=ketch&format=json&pageno=1", nil)
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var body struct {
			Results []json.RawMessage `json:"results"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
			return health.StatusMisconfigured, fmt.Sprintf("returned non-JSON response — is %s a SearXNG instance?", baseURL)
		}
		return health.StatusOK, ""
	case http.StatusForbidden:
		return health.StatusMisconfigured, `format=json is blocked (HTTP 403) — enable it in the instance's settings.yml: search.formats must include "json", then restart SearXNG`
	case http.StatusTooManyRequests:
		return health.StatusMisconfigured, "rate limited (HTTP 429) — the SearXNG limiter is throttling ketch; consider disabling the limiter for local instances"
	default:
		return health.StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

func searxngProvider() Provider {
	return Provider{
		MinProbeTimeout: 10 * time.Second,
		Settings:        []config.Setting{{Key: "searxng_url", ValidationOrder: 1, Default: "http://localhost:8081", FileOrder: 1, DiscoveryOrder: 2, EnvOrder: 1, Always: true}},
		ID:              "searxng",
		Name:            "SearXNG",
		Usable:          func(*config.Config) bool { return true },
		New:             func(c *config.Config) (Searcher, error) { return NewSearXNG(c.String("searxng_url")), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return ProbeSearxng(ctx, client, c.String("searxng_url"))
		},
	}
}
