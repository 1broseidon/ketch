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

// keenableBaseURL is the Keenable API root. Search hits /v1/search/public
// (keyless) or /v1/search (keyed); the endpoint is chosen by whether a key is
// configured.
const keenableBaseURL = "https://api.keenable.ai"

// keenableTitle is the attribution tag Keenable segments integration traffic
// by. Sent on every request so ketch usage is visible in adoption dashboards.
const keenableTitle = "Ketch"

// keenableSnippetMaxChars caps the text kept per result. Keenable returns the
// whole page on every search result, an order of magnitude more than the other
// backends here return.
const keenableSnippetMaxChars = 500

// Keenable searches via the Keenable web search API, a search index built for
// AI agents. It is keyless by default (rate-limited); an optional API key lifts
// the hourly cap and switches to the authenticated endpoint.
type Keenable struct {
	keys   keyPool
	client *http.Client
}

// NewKeenable creates a new Keenable search backend. A nil or blank apiKey uses
// the keyless public endpoint.
func NewKeenable(apiKey *string) *Keenable {
	if apiKey == nil {
		return newKeenableWithKeys(nil)
	}
	return newKeenableWithKeys([]string{*apiKey})
}

func newKeenableWithKeys(keys []string) *Keenable {
	return &Keenable{keys: newKeyPool(keys), client: httpx.Default()}
}

type keenableResponse struct {
	Results []keenableResult `json:"results"`
}

type keenableResult struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	// Snippet carries the page text. Description is the page's meta
	// description and is empty for most pages, so it is only a fallback.
	Snippet     string `json:"snippet"`
	Description string `json:"description"`
}

// text returns the result's page text, collapsed to one line and capped at
// keenableSnippetMaxChars.
func (r keenableResult) text() string {
	s := r.Snippet
	if s == "" {
		s = r.Description
	}
	s = strings.Join(strings.Fields(s), " ")
	if runes := []rune(s); len(runes) > keenableSnippetMaxChars {
		s = string(runes[:keenableSnippetMaxChars])
	}
	return s
}

// Search queries Keenable and returns up to limit results.
func (k *Keenable) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return []Result{}, nil
	}

	body, err := json.Marshal(map[string]any{"query": query, "mode": "pro"})
	if err != nil {
		return nil, err
	}

	key := k.keys.pick()
	resp, err := k.request(ctx, body, key)
	if err != nil {
		return nil, err
	}
	if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusTooManyRequests) && k.keys.size() > 1 {
		closeSearchResponse(resp)
		key = k.keys.pickDifferent(key)
		resp, err = k.request(ctx, body, key)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("keenable: invalid API key (%s; set via: ketch config set keenable_api_key <key>)", k.keys.keyLabel(key))
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("keenable: rate limited (%s; set a key to lift the cap: ketch config set keenable_api_key <key>)", k.keys.keyLabel(key))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, keenableStatusError(resp)
	}

	var kr keenableResponse
	if err := json.NewDecoder(resp.Body).Decode(&kr); err != nil {
		return nil, fmt.Errorf("failed to decode keenable response: %w", err)
	}

	results := make([]Result, 0, limit)
	for _, r := range kr.Results {
		if len(results) >= limit {
			break
		}
		results = append(results, Result{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.text(),
		})
	}

	return results, nil
}

func (k *Keenable) request(ctx context.Context, body []byte, key string) (*http.Response, error) {
	path := "/v1/search/public"
	if key != "" {
		path = "/v1/search"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, keenableBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "keenable-ketch")
	req.Header.Set("X-Keenable-Title", keenableTitle)
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	resp, err := k.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("keenable request failed: %w", err)
	}
	return resp, nil
}

func keenableStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if detail := strings.TrimSpace(string(body)); detail != "" {
		return fmt.Errorf("keenable returned status %d: %s", resp.StatusCode, detail)
	}
	return fmt.Errorf("keenable returned status %d", resp.StatusCode)
}
