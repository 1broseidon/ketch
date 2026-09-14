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

// Brave searches via the Brave Search API.
type Brave struct {
	keys   keyPool
	client *http.Client
}

// NewBrave creates a new Brave search backend.
func NewBrave(apiKey string) *Brave {
	return newBraveWithKeys([]string{apiKey})
}

func newBraveWithKeys(keys []string) *Brave {
	return &Brave{keys: newKeyPool(keys), client: httpx.Default()}
}

type braveResponse struct {
	Web struct {
		Results []braveResult `json:"results"`
	} `json:"web"`
}

type braveResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

const braveMaxCount = 20

// Search queries Brave and returns up to limit results.
func (b *Brave) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	count := braveRequestCount(limit)
	if count == 0 {
		return []Result{}, nil
	}

	u := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d&text_decorations=false&result_filter=web",
		url.QueryEscape(query), count)
	key := b.keys.pick()
	resp, err := b.request(ctx, u, key)
	if err != nil {
		return nil, err
	}
	if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusTooManyRequests) && b.keys.size() > 1 {
		closeSearchResponse(resp)
		key = b.keys.pickDifferent(key)
		resp, err = b.request(ctx, u, key)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("brave: invalid API key (%s; set via: ketch config set brave_api_key <key>)", b.keys.keyLabel(key))
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("brave: rate limited (%s)", b.keys.keyLabel(key))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, braveStatusError(resp)
	}

	var br braveResponse
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		return nil, fmt.Errorf("failed to decode brave response: %w", err)
	}

	results := make([]Result, 0, count)
	for _, r := range br.Web.Results {
		if len(results) >= count {
			break
		}
		results = append(results, Result{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Description,
		})
	}

	return results, nil
}

func (b *Brave) request(ctx context.Context, endpoint, key string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", key)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave request failed: %w", err)
	}
	return resp, nil
}

func braveRequestCount(limit int) int {
	if limit <= 0 {
		return 0
	}
	if limit > braveMaxCount {
		return braveMaxCount
	}
	return limit
}

func braveStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if detail := strings.TrimSpace(string(body)); detail != "" {
		return fmt.Errorf("brave returned status %d: %s", resp.StatusCode, detail)
	}
	return fmt.Errorf("brave returned status %d", resp.StatusCode)
}

// ProbeBrave checks the provider using a caller-supplied client and endpoint.
func ProbeBrave(ctx context.Context, client *http.Client, endpoint, apiKey string) (health.Status, string) {
	if apiKey == "" {
		return health.StatusNoKey, "API key not set (get one free at https://brave.com/search/api/ then: ketch config set brave_api_key <key>)"
	}
	resp, err := health.Get(ctx, client, endpoint+"?q=ketch&count=1&text_decorations=false&result_filter=web", map[string]string{
		"Accept":               "application/json",
		"X-Subscription-Token": apiKey,
	})
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	defer health.Drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return health.StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		return health.StatusMisconfigured, "API key rejected (ketch config set brave_api_key <key>)"
	case http.StatusTooManyRequests:
		return health.StatusOK, "reachable, key accepted (rate limited)"
	default:
		return health.StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

func braveProvider() Provider {
	return Provider{
		Settings: []config.Setting{config.KeyPool("brave_api_key", "brave_api_keys", 2, 3, 2, 2)},
		ID:       "brave",
		Setup:    "brave: API key not set (get one free at https://brave.com/search/api/ then: ketch config set brave_api_key <key>)",
		Name:     "Brave",
		Usable:   func(c *config.Config) bool { return len(c.BraveKeys()) > 0 },
		New:      func(c *config.Config) (Searcher, error) { return newBraveWithKeys(c.BraveKeys()), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return health.ProbeKeyPool(c.BraveKeys(), func(key string) (health.Status, string) {
				return ProbeBrave(ctx, client, "https://api.search.brave.com/res/v1/web/search", key)
			})
		},
	}
}
