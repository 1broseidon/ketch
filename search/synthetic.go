package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/1broseidon/ketch/httpx"
)

// syntheticEndpoint is the Synthetic zero-data-retention web search API.
// Auth is Bearer-only in the Authorization header.
const syntheticEndpoint = "https://api.synthetic.new/v2/search"

// Synthetic searches the web via the Synthetic /v2/search API.
type Synthetic struct {
	keys   keyPool
	client *http.Client
}

// NewSynthetic creates a new Synthetic search backend.
func NewSynthetic(apiKey string) *Synthetic {
	return newSyntheticWithKeys([]string{apiKey})
}

func newSyntheticWithKeys(keys []string) *Synthetic {
	return &Synthetic{keys: newKeyPool(keys), client: httpx.Default()}
}

type syntheticRequest struct {
	Query string `json:"query"`
}

type syntheticResponse struct {
	Results []syntheticResult `json:"results"`
}

type syntheticResult struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	Published string `json:"published"`
}

// Search queries Synthetic and returns up to limit results. The Synthetic
// API returns its own extracted text per result; Description and Content are
// both populated from it so callers reading either field get the full text.
func (s *Synthetic) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return []Result{}, nil
	}

	body, err := json.Marshal(syntheticRequest{Query: query})
	if err != nil {
		return nil, err
	}

	key := s.keys.pick()
	resp, err := s.request(ctx, body, key)
	if err != nil {
		return nil, err
	}
	if syntheticRetryableStatus(resp.StatusCode) && s.keys.size() > 1 {
		closeSearchResponse(resp)
		key = s.keys.pickDifferent(key)
		resp, err = s.request(ctx, body, key)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, syntheticResponseError(resp, s.keys.keyLabel(key))
	}

	var sr syntheticResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("failed to decode synthetic response: %w", err)
	}
	return mapSyntheticResults(sr.Results, limit), nil
}

func mapSyntheticResults(raw []syntheticResult, limit int) []Result {
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
			Description: r.Text,
			Content:     r.Text,
		})
	}
	return results
}

func (s *Synthetic) request(ctx context.Context, body []byte, key string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, syntheticEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("synthetic request failed: %w", err)
	}
	return resp, nil
}

func syntheticRetryableStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusTooManyRequests
}

func syntheticResponseError(resp *http.Response, keyLabel string) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("synthetic: invalid API key (%s; set via: ketch config set synthetic_api_key <key>)", keyLabel)
	case http.StatusTooManyRequests:
		return fmt.Errorf("synthetic: rate limited (%s)", keyLabel)
	default:
		return syntheticStatusError(resp)
	}
}

func syntheticStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if detail := string(body); detail != "" {
		return fmt.Errorf("synthetic returned status %d: %s", resp.StatusCode, detail)
	}
	return fmt.Errorf("synthetic returned status %d", resp.StatusCode)
}
