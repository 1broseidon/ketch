package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/1broseidon/ketch/httpx"
)

// Degoog searches a degoog instance via its JSON API.
type Degoog struct {
	baseURL string
	client  *http.Client
}

// NewDegoog creates a new degoog search backend.
func NewDegoog(baseURL string) *Degoog {
	return &Degoog{
		baseURL: baseURL,
		client:  httpx.Default(),
	}
}

type degoogResponse struct {
	Results []degoogResult `json:"results"`
}

type degoogResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// Search queries degoog and returns up to limit results.
func (d *Degoog) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	u := fmt.Sprintf("%s/api/search?q=%s", d.baseURL, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("degoog request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("degoog returned status %d", resp.StatusCode)
	}

	var dr degoogResponse
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		return nil, fmt.Errorf("failed to decode degoog response: %w", err)
	}

	results := make([]Result, 0, limit)
	for _, r := range dr.Results {
		if len(results) >= limit {
			break
		}
		results = append(results, Result{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Snippet,
		})
	}

	return results, nil
}
