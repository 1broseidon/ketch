package code

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Sourcegraph searches code via the Sourcegraph streaming search API.
type Sourcegraph struct {
	baseURL string
	client  *http.Client
}

// NewSourcegraph creates a new Sourcegraph code search backend.
func NewSourcegraph(baseURL string) *Sourcegraph {
	return &Sourcegraph{
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// buildQuery applies Sourcegraph's query dialect: lang: filter and the
// archived/fork safety qualifiers, unless the user already specified them.
func (s *Sourcegraph) buildQuery(query, lang string) string {
	if lang != "" {
		query += " lang:" + lang
	}
	if !strings.Contains(query, "archived:") {
		query += " archived:no"
	}
	if !strings.Contains(query, "fork:") {
		query += " fork:no"
	}
	return query
}

type sseContentMatch struct {
	Type        string         `json:"type"`
	Repository  string         `json:"repository"`
	Path        string         `json:"path"`
	Language    string         `json:"language"`
	RepoStars   int            `json:"repoStars"`
	LineMatches []sseLineMatch `json:"lineMatches"`
}

type sseLineMatch struct {
	Line       string `json:"line"`
	LineNumber int    `json:"lineNumber"`
}

// Search queries Sourcegraph and returns up to limit code results.
func (s *Sourcegraph) Search(query, lang string, limit int) ([]Result, error) {
	full := s.buildQuery(query, lang)
	u := fmt.Sprintf("%s/.api/search/stream?q=%s&display=%d",
		s.baseURL, url.QueryEscape(full), limit)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sourcegraph request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sourcegraph returned status %d", resp.StatusCode)
	}

	return s.parseSSE(resp, limit)
}

func (s *Sourcegraph) parseSSE(resp *http.Response, limit int) ([]Result, error) {
	var results []Result
	var eventType string

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		if !strings.HasPrefix(line, "data:") {
			continue
		}
		if eventType != "matches" {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var matches []sseContentMatch
		if err := json.Unmarshal([]byte(data), &matches); err != nil {
			continue
		}

		for _, m := range matches {
			if m.Type != "content" || len(m.LineMatches) == 0 {
				continue
			}
			if len(results) >= limit {
				return results, nil
			}
			lm := m.LineMatches[0]
			results = append(results, Result{
				Repo:     m.Repository,
				Path:     m.Path,
				Line:     lm.LineNumber,
				Snippet:  lm.Line,
				Language: m.Language,
				Stars:    m.RepoStars,
				URL:      fmt.Sprintf("%s/%s/-/blob/%s#L%d", s.baseURL, m.Repository, m.Path, lm.LineNumber),
				Source:   "sourcegraph",
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return results, fmt.Errorf("sourcegraph stream error: %w", err)
	}

	return results, nil
}
