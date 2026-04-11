package code

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GitHub searches code via the GitHub Code Search REST API.
type GitHub struct {
	token  string
	client *http.Client
}

// NewGitHub creates a new GitHub code search backend.
func NewGitHub(token string) *GitHub {
	return &GitHub{
		token:  token,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

type ghSearchResponse struct {
	TotalCount int      `json:"total_count"`
	Items      []ghItem `json:"items"`
}

type ghItem struct {
	Path        string        `json:"path"`
	HTMLURL     string        `json:"html_url"`
	Repository  ghRepo        `json:"repository"`
	TextMatches []ghTextMatch `json:"text_matches"`
}

type ghRepo struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
}

type ghTextMatch struct {
	Fragment string         `json:"fragment"`
	Matches  []ghMatchRange `json:"matches"`
}

type ghMatchRange struct {
	Indices []int  `json:"indices"`
	Text    string `json:"text"`
}

// Search queries GitHub Code Search and returns up to limit results.
func (g *GitHub) Search(query, lang string, limit int) ([]Result, error) {
	if g.token == "" {
		return nil, fmt.Errorf("github: token required")
	}

	full := g.buildQuery(query, lang)

	perPage := limit
	if perPage <= 0 || perPage > 100 {
		perPage = 30
	}

	u := fmt.Sprintf("https://api.github.com/search/code?q=%s&per_page=%d",
		url.QueryEscape(full), perPage)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github.text-match+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("github: invalid token (token must have 'repo' scope; check: gh auth status)")
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, g.rateLimitError(resp)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("github returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var sr ghSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("failed to decode github response: %w", err)
	}

	results := make([]Result, 0, len(sr.Items))
	for _, item := range sr.Items {
		snippet := ""
		if len(item.TextMatches) > 0 {
			tm := item.TextMatches[0]
			snippet = extractMatchedLine(tm.Fragment, tm.Matches)
		}
		results = append(results, Result{
			Repo:    item.Repository.FullName,
			Path:    item.Path,
			Snippet: snippet,
			URL:     item.HTMLURL,
			Source:  "github",
		})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

// extractMatchedLine returns the single line within fragment that contains
// the first match. Falls back to the first non-empty line if no usable match
// indices are present.
func extractMatchedLine(fragment string, matches []ghMatchRange) string {
	if len(matches) > 0 && len(matches[0].Indices) >= 1 {
		offset := matches[0].Indices[0]
		if offset >= 0 && offset < len(fragment) {
			start := strings.LastIndex(fragment[:offset], "\n") + 1
			end := strings.Index(fragment[offset:], "\n")
			if end == -1 {
				end = len(fragment)
			} else {
				end += offset
			}
			return strings.TrimSpace(fragment[start:end])
		}
	}
	for _, line := range strings.Split(fragment, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return strings.TrimSpace(fragment)
}

// buildQuery applies GitHub's code search query dialect: a language: filter.
// Note: GitHub's code-search endpoint does not accept archived: or fork:
// qualifiers (those are repo-search only), so we cannot filter them at query
// time. Users who care can scope with org:/user:/repo: instead.
func (g *GitHub) buildQuery(query, lang string) string {
	if lang != "" && !strings.Contains(query, "language:") {
		query += " language:" + lang
	}
	return query
}

// rateLimitError formats a friendly rate-limit error using GitHub's
// X-RateLimit-Reset header (Unix epoch seconds).
func (g *GitHub) rateLimitError(resp *http.Response) error {
	reset := resp.Header.Get("X-RateLimit-Reset")
	if reset != "" {
		if n, err := strconv.ParseInt(reset, 10, 64); err == nil {
			t := time.Unix(n, 0)
			wait := time.Until(t).Round(time.Second)
			return fmt.Errorf("github: rate limited (30 req/min on code search). Resets in %s at %s",
				wait, t.Format("15:04:05"))
		}
	}
	return fmt.Errorf("github: rate limited (status %d)", resp.StatusCode)
}
