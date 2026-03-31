package search

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// Tavily searches via the Tavily Search API.
type Tavily struct {
	apiKey string
	client *http.Client
}

// NewTavily creates a new Tavily search backend.
func NewTavily(apiKey string) *Tavily {
	return &Tavily{
		apiKey: apiKey,
		client: &http.Client{},
	}
}

type tavilyRequest struct {
	APIKey     string `json:"api_key"`
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
}

type tavilyResponse struct {
	Results []tavilyResult `json:"results"`
}

type tavilyResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

// Search queries Tavily and returns up to limit results.
func (t *Tavily) Search(query string, limit int) ([]Result, error) {
	body, err := json.Marshal(tavilyRequest{
		APIKey:     t.apiKey,
		Query:      query,
		MaxResults: limit,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", "https://api.tavily.com/search", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("tavily: invalid API key (set via: ketch config set tavily_api_key <key>)")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("tavily: rate limited")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tavily returned status %d", resp.StatusCode)
	}

	var tr tavilyResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("failed to decode tavily response: %w", err)
	}

	results := make([]Result, 0, limit)
	for _, r := range tr.Results {
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
