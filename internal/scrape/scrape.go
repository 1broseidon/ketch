package scrape

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/1broseidon/ketch/internal/extract"
)

// Page represents a scraped web page.
type Page struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
}

// Scraper fetches web pages and extracts content as markdown.
type Scraper struct {
	client    *http.Client
	extractor *extract.Extractor
}

// New creates a Scraper with defaults.
func New() *Scraper {
	return &Scraper{
		client:    &http.Client{},
		extractor: extract.New(),
	}
}

// Scrape fetches a URL and returns extracted markdown content.
func (s *Scraper) Scrape(rawURL string) (*Page, error) {
	body, err := s.fetch(rawURL)
	if err != nil {
		return nil, err
	}

	result, err := s.extractor.Extract(rawURL, body)
	if err != nil {
		return nil, fmt.Errorf("extraction failed for %s: %w", rawURL, err)
	}

	return &Page{
		URL:      rawURL,
		Title:    result.Title,
		Markdown: result.Markdown,
	}, nil
}

func (s *Scraper) fetch(rawURL string) (string, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ketch/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body failed: %w", err)
	}

	html := string(b)

	// Fast path check: if body looks JS-rendered (tiny or framework shell), log it.
	// Browser backend (rod) would be invoked here in the future.
	if len(strings.TrimSpace(html)) < 512 {
		return html, nil // Return what we have; browser fallback is a future extension
	}

	return html, nil
}
