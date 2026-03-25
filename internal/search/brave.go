package search

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Brave searches via the Brave Search API.
type Brave struct {
	apiKey string
	client *http.Client
}

// NewBrave creates a new Brave search backend.
func NewBrave(apiKey string) *Brave {
	return &Brave{
		apiKey: apiKey,
		client: &http.Client{},
	}
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

// Search queries Brave and returns up to limit results.
func (b *Brave) Search(query string, limit int) ([]Result, error) {
	u := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d&text_decorations=false&result_filter=web",
		url.QueryEscape(query), limit)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.apiKey)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("brave: invalid API key (set via: ketch config set brave_api_key <key>)")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("brave: rate limited")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave returned status %d", resp.StatusCode)
	}

	var br braveResponse
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		return nil, fmt.Errorf("failed to decode brave response: %w", err)
	}

	results := make([]Result, 0, limit)
	for _, r := range br.Web.Results {
		if len(results) >= limit {
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
