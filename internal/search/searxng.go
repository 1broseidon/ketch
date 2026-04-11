package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
		client:  &http.Client{},
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
