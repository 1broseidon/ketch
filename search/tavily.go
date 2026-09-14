package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// tavilyEndpoint is the hosted Tavily search API. Auth is Bearer-only in the
// Authorization header — never a query param or body field — so transport
// errors cannot leak the key via a URL (see Exa's opposite discipline).
const tavilyEndpoint = "https://api.tavily.com/search"

// Tavily plan/paygo limit statuses are provider-specific (not in net/http).
const (
	tavilyStatusPlanLimit  = 432
	tavilyStatusPaygoLimit = 433
)

// Tavily searches the web via the Tavily Search API.
type Tavily struct {
	keys   keyPool
	client *http.Client
}

// NewTavily creates a new Tavily search backend.
func NewTavily(apiKey string) *Tavily {
	return newTavilyWithKeys([]string{apiKey})
}

func newTavilyWithKeys(keys []string) *Tavily {
	return &Tavily{keys: newKeyPool(keys), client: httpx.Default()}
}

type tavilyRequest struct {
	Query       string `json:"query"`
	MaxResults  int    `json:"max_results"`
	SearchDepth string `json:"search_depth"`
}

type tavilyResponse struct {
	Results []tavilyResult `json:"results"`
}

type tavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// Search queries Tavily and returns up to limit results. Content is filled
// from Tavily's extracted text (richer than a SERP snippet), with Description
// set to the same string for callers that only read the short field.
func (t *Tavily) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return []Result{}, nil
	}

	body, err := json.Marshal(tavilyRequest{
		Query:       query,
		MaxResults:  limit,
		SearchDepth: "basic", // 1 credit; advanced costs more
	})
	if err != nil {
		return nil, err
	}

	key := t.keys.pick()
	resp, err := t.request(ctx, body, key)
	if err != nil {
		return nil, err
	}
	if tavilyRetryableStatus(resp.StatusCode) && t.keys.size() > 1 {
		closeSearchResponse(resp)
		key = t.keys.pickDifferent(key)
		resp, err = t.request(ctx, body, key)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, tavilyResponseError(resp, t.keys.keyLabel(key))
	}

	var tr tavilyResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("failed to decode tavily response: %w", err)
	}
	return mapTavilyResults(tr.Results, limit), nil
}

// tavilyResponseError maps known Tavily statuses to actionable errors; unknown
// statuses include a bounded body snippet for diagnosis.
func tavilyResponseError(resp *http.Response, keyLabel string) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("tavily: invalid API key (%s; set via: ketch config set tavily_api_key <key>)", keyLabel)
	case http.StatusTooManyRequests:
		return fmt.Errorf("tavily: rate limited (%s)", keyLabel)
	case tavilyStatusPlanLimit:
		return fmt.Errorf("tavily: plan limit reached (%s)", keyLabel)
	case tavilyStatusPaygoLimit:
		return fmt.Errorf("tavily: pay-as-you-go limit reached (%s)", keyLabel)
	default:
		return tavilyStatusError(resp)
	}
}

func mapTavilyResults(raw []tavilyResult, limit int) []Result {
	results := make([]Result, 0, limit)
	for _, r := range raw {
		if len(results) >= limit {
			break
		}
		if r.URL == "" {
			continue
		}
		results = append(results, Result{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Content,
			Content:     r.Content,
		})
	}
	return results
}

func (t *Tavily) request(ctx context.Context, body []byte, key string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tavilyEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily request failed: %w", err)
	}
	return resp, nil
}

func tavilyRetryableStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusTooManyRequests
}

func tavilyStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if detail := strings.TrimSpace(string(body)); detail != "" {
		return fmt.Errorf("tavily returned status %d: %s", resp.StatusCode, detail)
	}
	return fmt.Errorf("tavily returned status %d", resp.StatusCode)
}

const tavilyProbeBody = `{"query":"ketch","max_results":1,"search_depth":"basic"}`

// ProbeTavily checks the provider using a caller-supplied client and endpoint.
func ProbeTavily(ctx context.Context, client *http.Client, endpoint, apiKey string) (health.Status, string) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return health.StatusNoKey, "API key not set (get one free at https://app.tavily.com then: ketch config set tavily_api_key <key>)"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(tavilyProbeBody))
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := client.Do(req)
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	defer health.Drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return health.StatusOK, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		return health.StatusMisconfigured, "API key rejected (ketch config set tavily_api_key <key>)"
	case http.StatusTooManyRequests:
		return health.StatusOK, "reachable, key accepted (rate limited)"
	case tavilyStatusPlanLimit:
		return health.StatusOK, "reachable, key accepted (plan limit)"
	case tavilyStatusPaygoLimit:
		return health.StatusOK, "reachable, key accepted (pay-as-you-go limit)"
	default:
		return health.StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

func tavilyProvider() Provider {
	return Provider{
		Settings: []config.Setting{config.KeyPool("tavily_api_key", "tavily_api_keys", 11, 12, 7, 11)},
		ID:       "tavily",
		Setup:    "tavily: API key not set (get one free at https://app.tavily.com then: ketch config set tavily_api_key <key>)",
		Name:     "Tavily",
		Usable:   func(c *config.Config) bool { return len(c.TavilyKeys()) > 0 },
		New:      func(c *config.Config) (Searcher, error) { return newTavilyWithKeys(c.TavilyKeys()), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return health.ProbeKeyPool(c.TavilyKeys(), func(key string) (health.Status, string) {
				return ProbeTavily(ctx, client, "https://api.tavily.com/search", key)
			})
		},
	}
}
