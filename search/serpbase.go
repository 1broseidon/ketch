package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/1broseidon/ketch/httpx"
)

// serpbaseEndpoint is the hosted SerpBase Google Search API. Auth is a
// query-param api_key — the provider's documented contract — so keys ride in
// the URL like Exa's, never in headers or bodies.
const serpbaseEndpoint = "https://api.serpbase.dev/google/search"

// SerpBase searches Google via the SerpBase REST API, returning structured
// organic results without any scraping maintenance.
type SerpBase struct {
	keys   keyPool
	client *http.Client
}

// NewSerpBase creates a new SerpBase search backend.
func NewSerpBase(apiKey string) *SerpBase {
	return newSerpBaseWithKeys([]string{apiKey})
}

func newSerpBaseWithKeys(keys []string) *SerpBase {
	return &SerpBase{keys: newKeyPool(keys), client: httpx.Default()}
}

type serpBaseResponse struct {
	OrganicResults []serpBaseResult `json:"organic_results"`
}

type serpBaseResult struct {
	Title    string `json:"title"`
	Link     string `json:"link"`
	Snippet  string `json:"snippet"`
	Position int    `json:"position"`
}

// Search queries SerpBase and returns up to limit organic results.
func (s *SerpBase) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return []Result{}, nil
	}

	key := s.keys.pick()
	resp, err := s.request(ctx, query, limit, key)
	if err != nil {
		return nil, err
	}
	if serpBaseRetryableStatus(resp.StatusCode) && s.keys.size() > 1 {
		closeSearchResponse(resp)
		key = s.keys.pickDifferent(key)
		resp, err = s.request(ctx, query, limit, key)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, serpBaseResponseError(resp, s.keys.keyLabel(key))
	}

	var sr serpBaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("failed to decode serpbase response: %w", err)
	}

	results := make([]Result, 0, limit)
	for _, r := range sr.OrganicResults {
		if len(results) >= limit {
			break
		}
		if strings.TrimSpace(r.Link) == "" {
			continue
		}
		results = append(results, Result{
			Title:       r.Title,
			URL:         r.Link,
			Description: r.Snippet,
		})
	}
	return results, nil
}

func (s *SerpBase) request(ctx context.Context, query string, limit int, key string) (*http.Response, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("num", fmt.Sprintf("%d", limit))
	params.Set("api_key", key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serpbaseEndpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serpbase request failed: %w", err)
	}
	return resp, nil
}

func serpBaseRetryableStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusTooManyRequests
}

// serpBaseResponseError maps known SerpBase statuses to actionable errors;
// 402 is the provider's pay-as-you-go "credits exhausted" signal.
func serpBaseResponseError(resp *http.Response, keyLabel string) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("serpbase: invalid API key (%s; set via: ketch config set serpbase_api_key <key>)", keyLabel)
	case http.StatusPaymentRequired:
		return fmt.Errorf("serpbase: search credits exhausted — top up at https://serpbase.dev (%s)", keyLabel)
	case http.StatusTooManyRequests:
		return fmt.Errorf("serpbase: rate limited (%s)", keyLabel)
	default:
		return serpBaseStatusError(resp)
	}
}

func serpBaseStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if detail := strings.TrimSpace(string(body)); detail != "" {
		return fmt.Errorf("serpbase returned status %d: %s", resp.StatusCode, detail)
	}
	return fmt.Errorf("serpbase returned status %d", resp.StatusCode)
}
