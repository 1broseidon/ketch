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

const (
	parallelEndpoint            = "https://search.parallel.ai/mcp"
	parallelDescriptionMaxRunes = 300
)

type Parallel struct {
	client   *http.Client
	endpoint string
}

// NewParallel creates a keyless Parallel Search backend.
func NewParallel() *Parallel {
	return &Parallel{client: httpx.Default(), endpoint: parallelEndpoint}
}

type parallelRPCResponse struct {
	Result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type parallelSearchPayload struct {
	Results []struct {
		URL      string   `json:"url"`
		Title    string   `json:"title"`
		Excerpts []string `json:"excerpts"`
	} `json:"results"`
}

// Search calls Parallel's hosted Search MCP and maps its ranked results into
// Ketch's provider-neutral result shape. Parallel does not expose a result
// limit argument, so the requested limit is applied locally.
func (p *Parallel) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return []Result{}, nil
	}

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "web_search",
			"arguments": map[string]any{
				"objective":      query,
				"search_queries": []string{query},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("parallel request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parallelStatusError(resp)
	}

	var rpc parallelRPCResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rpc); err != nil {
		return nil, fmt.Errorf("failed to decode parallel response: %w", err)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("parallel JSON-RPC error %d: %s", rpc.Error.Code, rpc.Error.Message)
	}
	if rpc.Result.IsError {
		return nil, fmt.Errorf("parallel search tool returned an error")
	}

	results := make([]Result, 0, limit)
	foundText := false
	for _, content := range rpc.Result.Content {
		if content.Type != "text" || strings.TrimSpace(content.Text) == "" {
			continue
		}
		foundText = true
		results, err = appendParallelResults(results, content.Text, limit)
		if err != nil {
			return nil, err
		}
		if len(results) >= limit {
			break
		}
	}
	if !foundText {
		return nil, fmt.Errorf("parallel response contained no text results")
	}
	return results, nil
}

// appendParallelResults decodes one MCP text block and maps its valid results.
// Parallel can return full excerpts, so Description is bounded for listings
// while Content retains the complete normalized text.
func appendParallelResults(results []Result, text string, limit int) ([]Result, error) {
	var payload parallelSearchPayload
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return nil, fmt.Errorf("failed to decode parallel search results: %w", err)
	}
	for _, raw := range payload.Results {
		if len(results) >= limit {
			break
		}
		if strings.TrimSpace(raw.Title) == "" || strings.TrimSpace(raw.URL) == "" {
			continue
		}
		excerpts := nonEmptyStrings(raw.Excerpts)
		description := ""
		if len(excerpts) > 0 {
			description = boundedParallelDescription(excerpts[0])
		}
		results = append(results, Result{
			Title:       collapseParallelWhitespace(raw.Title),
			URL:         raw.URL,
			Description: description,
			Content:     strings.Join(excerpts, "\n"),
		})
	}
	return results, nil
}

// collapseParallelWhitespace flattens a value onto a single line, collapsing
// every run of whitespace to one space. Parallel returns extracted page text,
// so titles and excerpts can carry newlines and indentation. Results are
// printed one per line — `--minimal` emits "url\ttitle\tdescription\n" — so an
// embedded newline or tab in either field would break that row contract.
func collapseParallelWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func boundedParallelDescription(excerpt string) string {
	excerpt = collapseParallelWhitespace(excerpt)
	runes := []rune(excerpt)
	if len(runes) <= parallelDescriptionMaxRunes {
		return excerpt
	}
	return strings.TrimSpace(string(runes[:parallelDescriptionMaxRunes-1])) + "…"
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func parallelStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if detail := strings.TrimSpace(string(body)); detail != "" {
		return fmt.Errorf("parallel returned status %d: %s", resp.StatusCode, detail)
	}
	return fmt.Errorf("parallel returned status %d", resp.StatusCode)
}
