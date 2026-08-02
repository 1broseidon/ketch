package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/1broseidon/ketch/httpx"
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
