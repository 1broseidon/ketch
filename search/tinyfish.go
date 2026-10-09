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

const tinyfishEndpoint = "https://api.search.tinyfish.ai"

// TinyFish searches the web through the TinyFish Search API.
type TinyFish struct {
	keys   keyPool
	client *http.Client
}

// NewTinyFish creates a TinyFish search backend.
func NewTinyFish(apiKey string) *TinyFish {
	return newTinyFishWithKeys([]string{apiKey})
}

func newTinyFishWithKeys(keys []string) *TinyFish {
	return &TinyFish{keys: newKeyPool(keys), client: httpx.Default()}
}

func (t *TinyFish) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return []Result{}, nil
	}

	endpoint := tinyfishEndpoint + "?query=" + url.QueryEscape(query)
	key := t.keys.pick()
	resp, err := t.request(ctx, endpoint, key)
	if err != nil {
		return nil, err
	}
	if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) && t.keys.size() > 1 {
		closeSearchResponse(resp)
		key = t.keys.pickDifferent(key)
		resp, err = t.request(ctx, endpoint, key)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("tinyfish: invalid API key (%s; set via: ketch config set tinyfish_api_key <key>)", t.keys.keyLabel(key))
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("tinyfish: rate limited (%s)", t.keys.keyLabel(key))
	default:
		return nil, fmt.Errorf("tinyfish returned status %d", resp.StatusCode)
	}

	var payload struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode tinyfish response: %w", err)
	}
	results := make([]Result, 0, min(limit, len(payload.Results)))
	for _, r := range payload.Results {
		if len(results) >= limit {
			break
		}
		if resultURL := tinyfishResultURL(r.URL); resultURL != "" {
			results = append(results, Result{Title: r.Title, URL: resultURL, Description: r.Snippet})
		}
	}
	return results, nil
}

func tinyfishResultURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	// TinyFish sometimes returns Google's relative redirect instead of its target.
	if u.Scheme == "" && u.Host == "" && u.Path == "/url" {
		u, err = url.Parse(u.Query().Get("q"))
	}
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ""
	}
	return u.String()
}

func (t *TinyFish) request(ctx context.Context, endpoint, key string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", key)
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tinyfish request failed: %w", err)
	}
	return resp, nil
}

// ProbeTinyFish checks the provider using a caller-supplied client and endpoint.
func ProbeTinyFish(ctx context.Context, client *http.Client, endpoint, apiKey string) (health.Status, string) {
	if strings.TrimSpace(apiKey) == "" {
		return health.StatusNoKey, "API key not set (get a free key at https://agent.tinyfish.ai/api-keys then: ketch config set tinyfish_api_key <key>)"
	}
	resp, err := health.Get(ctx, client, endpoint+"?query=ketch", map[string]string{
		"Accept": "application/json", "X-API-Key": apiKey,
	})
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	defer health.Drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return health.StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		return health.StatusMisconfigured, "API key rejected (ketch config set tinyfish_api_key <key>)"
	case http.StatusTooManyRequests:
		return health.StatusOK, "reachable, key accepted (rate limited)"
	default:
		return health.StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

func tinyfishProvider() Provider {
	keys := config.KeyPool("tinyfish_api_key", "tinyfish_api_keys")
	return Provider{
		Settings: []config.Setting{keys},
		AutoRank: 45,
		ID:       "tinyfish",
		Setup:    "tinyfish: API key not set (get a free key at https://agent.tinyfish.ai/api-keys then: ketch config set tinyfish_api_key <key>)",
		Name:     "TinyFish",
		Usable:   func(c *config.Config) bool { return len(keys.Keys(c)) > 0 },
		New:      func(c *config.Config) (Searcher, error) { return newTinyFishWithKeys(keys.Keys(c)), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return health.ProbeKeyPool(keys.Keys(c), func(key string) (health.Status, string) {
				return ProbeTinyFish(ctx, client, tinyfishEndpoint, key)
			})
		},
	}
}
