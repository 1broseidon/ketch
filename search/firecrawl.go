package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/httpx"
)

// Firecrawl searches the web via the Firecrawl v2 search API.
type Firecrawl struct {
	keys     keyPool
	client   *http.Client
	endpoint string // full POST URL (base + /v2/search)
}

// NewFirecrawl creates a new Firecrawl search backend against the hosted API.
func NewFirecrawl(apiKey string) *Firecrawl {
	return newFirecrawlWithKeys([]string{apiKey}, config.DefaultFirecrawlURL)
}

func newFirecrawlWithKeys(keys []string, baseURL string) *Firecrawl {
	return &Firecrawl{
		keys:     newKeyPool(keys),
		client:   httpx.Default(),
		endpoint: config.FirecrawlSearchURL(baseURL),
	}
}

type firecrawlRequest struct {
	Query       string `json:"query"`
	Limit       int    `json:"limit"`
	Integration string `json:"integration,omitempty"`
}

type firecrawlResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Web []firecrawlResult `json:"web"`
	} `json:"data"`
}

type firecrawlResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// Search queries Firecrawl and returns up to limit web results.
func (f *Firecrawl) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return []Result{}, nil
	}

	body, err := json.Marshal(firecrawlRequest{
		Query:       query,
		Limit:       limit,
		Integration: "_ketch",
	})
	if err != nil {
		return nil, err
	}

	key := f.keys.pick()
	resp, err := f.request(ctx, body, key)
	if err != nil {
		return nil, err
	}
	if firecrawlRetryableStatus(resp.StatusCode) && f.keys.size() > 1 {
		closeSearchResponse(resp)
		key = f.keys.pickDifferent(key)
		resp, err = f.request(ctx, body, key)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("firecrawl: invalid API key (%s; set via: ketch config set firecrawl_api_key <key>)", f.keys.keyLabel(key))
	}
	if resp.StatusCode == http.StatusPaymentRequired {
		return nil, fmt.Errorf("firecrawl: payment required (%s)", f.keys.keyLabel(key))
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("firecrawl: rate limited (%s)", f.keys.keyLabel(key))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, firecrawlStatusError(resp)
	}

	var fr firecrawlResponse
	if err := json.NewDecoder(resp.Body).Decode(&fr); err != nil {
		return nil, fmt.Errorf("failed to decode firecrawl response: %w", err)
	}

	results := make([]Result, 0, limit)
	for _, r := range fr.Data.Web {
		if len(results) >= limit {
			break
		}
		if r.URL == "" {
			continue
		}
		results = append(results, Result{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Description,
		})
	}

	return results, nil
}

func (f *Firecrawl) request(ctx context.Context, body []byte, key string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("firecrawl request failed: %w", err)
	}
	return resp, nil
}

func firecrawlRetryableStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusPaymentRequired || status == http.StatusTooManyRequests
}

func firecrawlStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if detail := strings.TrimSpace(string(body)); detail != "" {
		return fmt.Errorf("firecrawl returned status %d: %s", resp.StatusCode, detail)
	}
	return fmt.Errorf("firecrawl returned status %d", resp.StatusCode)
}
