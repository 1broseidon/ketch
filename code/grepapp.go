package code

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// grepAppEndpoint is the public Grep MCP server backed by the same index as
// grep.app — a literal/regex code search over a million public GitHub repos.
// It speaks JSON-RPC over an SSE response and requires no auth or session.
const grepAppEndpoint = "https://mcp.grep.app"

// GrepApp searches code via the Grep MCP server (mcp.grep.app). Unlike
// Sourcegraph/GitHub it needs no token, but it searches literal code patterns
// (grep-style) rather than keywords.
type GrepApp struct {
	endpoint string
	client   *http.Client
}

// NewGrepApp creates a new Grep MCP code search backend.
// The MCP call streams an SSE response that can run long, so use a client
// without a request timeout (context is the only bound).
var grepAppClient = httpx.New(0, httpx.DefaultMaxIdleConnsPerHost)

func NewGrepApp() *GrepApp {
	return &GrepApp{
		endpoint: grepAppEndpoint,
		client:   grepAppClient,
	}
}

type grepMCPRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int            `json:"id"`
	Method  string         `json:"method"`
	Params  grepCallParams `json:"params"`
}

type grepCallParams struct {
	Name      string        `json:"name"`
	Arguments grepSearchArg `json:"arguments"`
}

type grepSearchArg struct {
	Query     string   `json:"query"`
	Language  []string `json:"language,omitempty"`
	UseRegexp bool     `json:"useRegexp,omitempty"`
}

type grepMCPResponse struct {
	Result *struct {
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

// Search queries the Grep MCP server and returns up to q.Limit code results.
func (g *GrepApp) Search(ctx context.Context, q Query) ([]Result, error) {
	reqBody := grepMCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: grepCallParams{
			Name: "searchGitHub",
			Arguments: grepSearchArg{
				Query:     q.Term,
				Language:  normalizeGrepLang(q.Lang),
				UseRegexp: q.Regexp,
			},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", g.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("grep.app request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grep.app returned status %d", resp.StatusCode)
	}

	texts, err := g.parseSSE(resp)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(texts))
	for _, text := range texts {
		if len(results) >= q.Limit {
			break
		}
		if r, ok := parseGrepBlock(text, q.Term); ok {
			r.Language = q.Lang
			results = append(results, r)
		}
	}
	return results, nil
}

// parseSSE reads the SSE response, decodes the first JSON-RPC payload carrying
// a result or error, and returns the per-repository text blocks.
func (g *GrepApp) parseSSE(resp *http.Response) ([]string, error) {
	scanner := bufio.NewScanner(resp.Body)
	// Snippet blocks can be large; raise the line buffer ceiling.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}

		var msg grepMCPResponse
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("grep.app error: %s", msg.Error.Message)
		}
		if msg.Result == nil {
			continue
		}
		if msg.Result.IsError {
			return nil, fmt.Errorf("grep.app search failed")
		}

		texts := make([]string, 0, len(msg.Result.Content))
		for _, c := range msg.Result.Content {
			if c.Type == "text" && c.Text != "" {
				texts = append(texts, c.Text)
			}
		}
		return texts, nil
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("grep.app stream error: %w", err)
	}
	return nil, nil
}

// parseGrepBlock converts one repository text block into a Result. The block
// has "Repository:"/"Path:"/"URL:" headers followed by "Snippets:" sections of
// the form "--- Snippet N (Line L) ---" and the matching code lines.
func parseGrepBlock(text, query string) (Result, bool) {
	r := Result{Source: "grepapp"}
	lines := strings.Split(text, "\n")

	codeStart := -1
	startLine := 0
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "Repository:"):
			r.Repo = strings.TrimSpace(strings.TrimPrefix(line, "Repository:"))
		case strings.HasPrefix(line, "Path:"):
			r.Path = strings.TrimSpace(strings.TrimPrefix(line, "Path:"))
		case strings.HasPrefix(line, "URL:"):
			r.URL = strings.TrimSpace(strings.TrimPrefix(line, "URL:"))
		case strings.HasPrefix(line, "--- Snippet") && codeStart == -1:
			startLine = parseSnippetLine(line)
			codeStart = i + 1
		}
	}

	if r.Repo == "" || r.URL == "" {
		return Result{}, false
	}

	if codeStart != -1 {
		snippet, line := extractGrepSnippet(lines[codeStart:], startLine, query)
		r.Snippet = snippet
		r.Line = line
	}
	return r, true
}

// parseSnippetLine extracts L from a "--- Snippet N (Line L) ---" header.
func parseSnippetLine(header string) int {
	open := strings.Index(header, "(Line ")
	if open == -1 {
		return 0
	}
	rest := header[open+len("(Line "):]
	end := strings.IndexByte(rest, ')')
	if end == -1 {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil {
		return 0
	}
	return n
}

// extractGrepSnippet picks the most relevant single line from a snippet's code
// lines: the first line containing the query, else the first non-empty line.
// It returns the trimmed line and its absolute line number.
func extractGrepSnippet(codeLines []string, startLine int, query string) (string, int) {
	q := strings.ToLower(query)
	firstNonEmpty := ""
	firstNonEmptyLine := 0

	for i, line := range codeLines {
		if strings.HasPrefix(line, "--- Snippet") {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if firstNonEmpty == "" {
			firstNonEmpty = trimmed
			firstNonEmptyLine = startLine + i
		}
		if strings.Contains(strings.ToLower(trimmed), q) {
			return trimmed, startLine + i
		}
	}
	return firstNonEmpty, firstNonEmptyLine
}

// normalizeGrepLang maps a user language filter to grep.app's GitHub-linguist
// language name. Returns nil (no filter) when lang is empty.
func normalizeGrepLang(lang string) []string {
	if lang == "" {
		return nil
	}
	if name, ok := grepLangNames[strings.ToLower(lang)]; ok {
		return []string{name}
	}
	// Fall back to capitalizing the first rune (e.g. "rust" -> "Rust").
	return []string{strings.ToUpper(lang[:1]) + lang[1:]}
}

// grepLangNames maps common lowercase aliases to grep.app language names.
var grepLangNames = map[string]string{
	"go":         "Go",
	"golang":     "Go",
	"py":         "Python",
	"python":     "Python",
	"js":         "JavaScript",
	"javascript": "JavaScript",
	"ts":         "TypeScript",
	"typescript": "TypeScript",
	"tsx":        "TSX",
	"jsx":        "JSX",
	"rb":         "Ruby",
	"ruby":       "Ruby",
	"rs":         "Rust",
	"rust":       "Rust",
	"java":       "Java",
	"kt":         "Kotlin",
	"kotlin":     "Kotlin",
	"c":          "C",
	"cpp":        "C++",
	"c++":        "C++",
	"cs":         "C#",
	"csharp":     "C#",
	"php":        "PHP",
	"swift":      "Swift",
	"scala":      "Scala",
	"sh":         "Shell",
	"bash":       "Shell",
	"shell":      "Shell",
	"html":       "HTML",
	"css":        "CSS",
	"sql":        "SQL",
}

func grepappProvider() Provider {
	return Provider{CLIName: "Grep (mcp.grep.app; no token, literal/regex over 1M+ public repos)", Regexp: true,
		Settings: []config.Setting{},
		ID:       "grepapp",
		Name:     "Grep (mcp.grep.app)",
		Usable:   func(*config.Config) bool { return true },
		New:      func(c *config.Config) (Searcher, error) { return NewGrepApp(), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return health.ProbeMCP(ctx, client, "https://mcp.grep.app", "grep.app")
		},
	}
}
