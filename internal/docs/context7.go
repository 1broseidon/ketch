package docs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Context7 searches library documentation via the Context7 API.
type Context7 struct {
	apiKey string
	client *http.Client
}

// NewContext7 creates a new Context7 docs backend.
func NewContext7(apiKey string) *Context7 {
	return &Context7{
		apiKey: apiKey,
		client: &http.Client{},
	}
}

// LibraryMatch is a resolved library from Context7's search endpoint.
type LibraryMatch struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	CodeSnippets int      `json:"codeSnippets"`
	Trust        string   `json:"trust"`
	Versions     []string `json:"versions"`
}

type context7DocsResponse struct {
	CodeSnippets []context7CodeSnippet `json:"codeSnippets"`
	InfoSnippets []context7InfoSnippet `json:"infoSnippets"`
}

type context7CodeSnippet struct {
	CodeTitle       string              `json:"codeTitle"`
	CodeDescription string              `json:"codeDescription"`
	CodeLanguage    string              `json:"codeLanguage"`
	CodeID          string              `json:"codeId"`
	CodeList        []context7CodeEntry `json:"codeList"`
}

type context7CodeEntry struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

type context7InfoSnippet struct {
	PageID     string `json:"pageId"`
	Breadcrumb string `json:"breadcrumb"`
	Content    string `json:"content"`
}

// Search resolves a library from the query and fetches documentation.
func (c *Context7) Search(query string, limit int) ([]Result, error) {
	libs, err := c.ResolveLibrary(query)
	if err != nil {
		return nil, fmt.Errorf("context7 resolve failed: %w", err)
	}
	if len(libs) == 0 {
		return nil, fmt.Errorf("context7: no library found for %q", query)
	}

	return c.GetDocs(libs[0].ID, query, 4000)
}

// ResolveLibrary searches Context7 for libraries matching the given name.
func (c *Context7) ResolveLibrary(name string) ([]LibraryMatch, error) {
	u := fmt.Sprintf("https://context7.com/api/v1/search?q=%s", url.QueryEscape(name))

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("context7 resolve request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("context7: invalid API key (set via: ketch config set context7_api_key <key>)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("context7 resolve returned status %d", resp.StatusCode)
	}

	var matches []LibraryMatch
	if err := json.NewDecoder(resp.Body).Decode(&matches); err != nil {
		return nil, fmt.Errorf("failed to decode context7 resolve response: %w", err)
	}

	return matches, nil
}

// GetDocs fetches documentation snippets for a resolved library ID.
func (c *Context7) GetDocs(libraryID, query string, tokens int) ([]Result, error) {
	u := fmt.Sprintf("https://context7.com/api/v2/context?libraryId=%s&query=%s&type=json&tokens=%d",
		url.QueryEscape(libraryID), url.QueryEscape(query), tokens)

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("context7 docs request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("context7: invalid API key (set via: ketch config set context7_api_key <key>)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("context7 docs returned status %d", resp.StatusCode)
	}

	var dr context7DocsResponse
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		return nil, fmt.Errorf("failed to decode context7 docs response: %w", err)
	}

	var results []Result

	for _, cs := range dr.CodeSnippets {
		snippet := ""
		if len(cs.CodeList) > 0 {
			snippet = cs.CodeList[0].Code
		}
		results = append(results, Result{
			Library: libraryID,
			Title:   cs.CodeTitle,
			Snippet: snippet,
			URL:     cs.CodeID,
			Source:  "context7",
		})
	}

	for _, is := range dr.InfoSnippets {
		results = append(results, Result{
			Library:    libraryID,
			Title:      is.Breadcrumb,
			Breadcrumb: is.Breadcrumb,
			Snippet:    is.Content,
			URL:        is.PageID,
			Source:     "context7",
		})
	}

	return results, nil
}
