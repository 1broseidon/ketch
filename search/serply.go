package search

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

// serplyEndpoint is the hosted Serply Google Search API. The key travels in the
// X-Api-Key header, never a query param, so a logged URL or a transport error
// cannot leak it.
const serplyEndpoint = "https://api.serply.io/v1/search/"

// serplyMaxNum is the largest page Serply serves. Larger values are accepted and
// silently clamped, and a page can come back with a couple more or fewer organic
// rows than requested, so the response is also trimmed to the caller's limit.
const serplyMaxNum = 10

// Serply searches Google via the Serply REST API, returning structured organic
// results without any scraping maintenance.
type Serply struct {
	keys   keyPool
	client *http.Client
}

// NewSerply creates a new Serply search backend.
func NewSerply(apiKey string) *Serply {
	return newSerplyWithKeys([]string{apiKey})
}

func newSerplyWithKeys(keys []string) *Serply {
	return &Serply{keys: newKeyPool(keys), client: httpx.Default()}
}

type serplyResponse struct {
	Results []serplyResult `json:"results"`
}

type serplyResult struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	Description string `json:"description"`
}

// Search queries Serply and returns up to limit organic results.
func (s *Serply) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return []Result{}, nil
	}

	u := fmt.Sprintf("%s?q=%s&num=%d", serplyEndpoint, url.QueryEscape(query), serplyRequestNum(limit))
	key := s.keys.pick()
	resp, err := s.request(ctx, u, key)
	if err != nil {
		return nil, err
	}
	if serplyRetryable(resp.StatusCode) && s.keys.size() > 1 {
		closeSearchResponse(resp)
		key = s.keys.pickDifferent(key)
		resp, err = s.request(ctx, u, key)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if err := serplyStatusError(resp, s.keys.keyLabel(key)); err != nil {
		return nil, err
	}

	var sr serplyResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("failed to decode serply response: %w", err)
	}

	results := make([]Result, 0, limit)
	for _, r := range sr.Results {
		if len(results) >= limit {
			break
		}
		if strings.TrimSpace(r.Link) == "" {
			continue
		}
		results = append(results, Result{
			Title:       r.Title,
			URL:         r.Link,
			Description: r.Description,
		})
	}

	return results, nil
}

func (s *Serply) request(ctx context.Context, endpoint, key string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serply request failed: %w", err)
	}
	return resp, nil
}

func serplyRequestNum(limit int) int {
	if limit > serplyMaxNum {
		return serplyMaxNum
	}
	return limit
}

// serplyRetryable reports whether another key might succeed.
func serplyRetryable(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusTooManyRequests
}

// serplyStatusError maps a response to an actionable error, or nil on success.
// Serply reports failures as JSON `{"detail": "..."}`, so the body is quoted for
// statuses that have no friendlier mapping.
func serplyStatusError(resp *http.Response, keyLabel string) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("serply: invalid API key (%s; set via: ketch config set serply_api_key <key>)", keyLabel)
	case http.StatusPaymentRequired:
		return fmt.Errorf("serply: search credits exhausted, top up at https://serply.io (%s)", keyLabel)
	case http.StatusTooManyRequests:
		return fmt.Errorf("serply: rate limited (%s)", keyLabel)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if detail := strings.TrimSpace(string(body)); detail != "" {
		return fmt.Errorf("serply returned status %d: %s", resp.StatusCode, detail)
	}
	return fmt.Errorf("serply returned status %d", resp.StatusCode)
}

// ProbeSerply checks the provider using a caller-supplied client and endpoint.
func ProbeSerply(ctx context.Context, client *http.Client, endpoint, apiKey string) (health.Status, string) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return health.StatusNoKey, "API key not set (get a free key at https://serply.io then: ketch config set serply_api_key <key>)"
	}
	resp, err := health.Get(ctx, client, endpoint+"?q=ketch&num=1", map[string]string{
		"Accept":    "application/json",
		"X-Api-Key": key,
	})
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	defer health.Drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return health.StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		return health.StatusMisconfigured, "API key rejected (ketch config set serply_api_key <key>)"
	case http.StatusPaymentRequired:
		return health.StatusOK, "reachable, key accepted (search credits exhausted)"
	case http.StatusTooManyRequests:
		return health.StatusOK, "reachable, key accepted (rate limited)"
	default:
		return health.StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

func serplyProvider() Provider {
	keys := config.KeyPool("serply_api_key", "serply_api_keys")
	return Provider{
		// Serply fetches Google on demand; an uncached probe measures about
		// 1-1.5s and can exceed the default doctor budget on a slow link, so
		// allow a SerpBase-sized timeout rather than report a healthy key as
		// unreachable.
		MinProbeTimeout: 10 * time.Second,
		Settings:        []config.Setting{keys},
		AutoRank:        60,
		ID:              "serply",
		Setup:           "serply: API key not set (get a free key at https://serply.io then: ketch config set serply_api_key <key>)",
		Name:            "Serply",
		Usable:          func(c *config.Config) bool { return len(keys.Keys(c)) > 0 },
		New:             func(c *config.Config) (Searcher, error) { return newSerplyWithKeys(keys.Keys(c)), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return health.ProbeKeyPool(keys.Keys(c), func(key string) (health.Status, string) {
				return ProbeSerply(ctx, client, serplyEndpoint, key)
			})
		},
	}
}
