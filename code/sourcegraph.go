package code

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// sourcegraphMaxEventBytes bounds one SSE event; a matches batch for even the
// most common identifiers is a few megabytes at most.
const sourcegraphMaxEventBytes = 16 << 20

// Sourcegraph searches code via the Sourcegraph streaming search API.
type Sourcegraph struct {
	baseURL string
	client  *http.Client
}

// NewSourcegraph creates a new Sourcegraph code search backend.
// Streaming results can run long, so use a dedicated client without the
// default request timeout (context is the only bound).
var sourcegraphClient = httpx.New(0, httpx.DefaultMaxIdleConnsPerHost)

func NewSourcegraph(baseURL string) *Sourcegraph {
	return &Sourcegraph{
		baseURL: baseURL,
		client:  sourcegraphClient,
	}
}

// buildQuery applies Sourcegraph's query dialect: lang: filter, repo: filter
// and the archived/fork safety qualifiers, unless the user already specified
// them. The safety qualifiers de-noise an open search; a named repository is
// searched whatever its state, since excluding it would leave nothing.
func (s *Sourcegraph) buildQuery(query, lang, repo string) string {
	if lang != "" {
		query += " lang:" + lang
	}
	include := "no"
	if repo != "" {
		query += " repo:" + sourcegraphRepoPattern(repo)
		include = "yes"
	}
	if !strings.Contains(query, "archived:") {
		query += " archived:" + include
	}
	if !strings.Contains(query, "fork:") {
		query += " fork:" + include
	}
	return query
}

// sourcegraphRepoPattern anchors owner/name for Sourcegraph's repo: filter,
// an unanchored regexp over the full name (github.com/owner/name): a bare
// golang/go also matches golang/gofrontend and studygolang/gophers. The code
// host is left open for self-hosted instances.
func sourcegraphRepoPattern(repo string) string {
	return "(^|/)" + regexp.QuoteMeta(repo) + "$"
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

// sseAlert is Sourcegraph's explanation for an empty or degraded search.
type sseAlert struct {
	Title string `json:"title"`
}

// sourcegraphNoRepos is the alert title for a search whose repository
// filters matched no repository on the instance.
const sourcegraphNoRepos = "No repositories found"

// Search queries Sourcegraph and returns up to q.Limit code results.
// Sourcegraph defaults to literal matching; regex is requested via the
// patterntype:regexp qualifier.
func (s *Sourcegraph) Search(ctx context.Context, q Query) ([]Result, error) {
	repo, err := q.repo()
	if err != nil {
		return nil, err
	}
	full := s.buildQuery(q.Term, q.Lang, repo)
	if q.Regexp {
		full += " patterntype:regexp"
	}
	u := fmt.Sprintf("%s/.api/search/stream?q=%s&display=%d",
		s.baseURL, url.QueryEscape(full), q.Limit)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
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

	return s.parseSSE(resp, q.Limit, repo)
}

// parseSSE collects up to limit content matches from the event stream. For a
// repo-scoped search, the alert Sourcegraph sends when no repository matched
// becomes ErrRepoNotFound: the repository is missing from this instance,
// which an empty result would hide.
func (s *Sourcegraph) parseSSE(resp *http.Response, limit int, repo string) ([]Result, error) {
	var results []Result
	var eventType string

	scanner := bufio.NewScanner(resp.Body)
	// One "matches" event carries every match of a batch on a single data:
	// line. Popular symbols push that far past the 64KB default token size,
	// which used to fail the whole query with "token too long".
	scanner.Buffer(make([]byte, 0, 64*1024), sourcegraphMaxEventBytes)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))

		switch eventType {
		case "matches":
			var full bool
			if results, full = s.appendMatches(results, data, limit); full {
				return results, nil
			}
		case "alert":
			var alert sseAlert
			if repo != "" && len(results) == 0 && json.Unmarshal(data, &alert) == nil && alert.Title == sourcegraphNoRepos {
				return nil, fmt.Errorf("%w: %s is not on %s", ErrRepoNotFound, repo, s.baseURL)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return results, fmt.Errorf("sourcegraph stream error: %w", err)
	}

	return results, nil
}

// appendMatches adds the content matches of one "matches" event to results
// and reports whether results already held limit matches.
func (s *Sourcegraph) appendMatches(results []Result, data []byte, limit int) ([]Result, bool) {
	var matches []sseContentMatch
	if err := json.Unmarshal(data, &matches); err != nil {
		return results, false
	}
	for _, m := range matches {
		if m.Type != "content" || len(m.LineMatches) == 0 {
			continue
		}
		if len(results) >= limit {
			return results, true
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
	return results, false
}

func sourcegraphProvider() Provider {
	return Provider{Regexp: true, Qualifiers: true,
		Settings: []config.Setting{{Key: "sourcegraph_url", ValidationOrder: 21, Default: "https://sourcegraph.com", FileOrder: 21, DiscoveryOrder: 24, EnvOrder: 15}},
		ID:       "sourcegraph",
		Name:     "Sourcegraph",
		Usable:   func(*config.Config) bool { return true },
		New:      func(c *config.Config) (Searcher, error) { return NewSourcegraph(c.String("sourcegraph_url")), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return health.ProbeReachable(ctx, client, c.String("sourcegraph_url"), "sourcegraph")
		},
	}
}
