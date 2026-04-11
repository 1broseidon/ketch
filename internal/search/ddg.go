package search

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// DDG searches DuckDuckGo's HTML interface.
type DDG struct {
	client *http.Client
}

// NewDDG creates a new DuckDuckGo search backend.
func NewDDG() *DDG {
	return &DDG{client: &http.Client{}}
}

// Search queries DuckDuckGo and returns up to limit results.
func (d *DDG) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	resp, err := d.fetchResults(ctx, query)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ddg response: %w", err)
	}

	var results []Result
	doc.Find(".result").Each(func(i int, s *goquery.Selection) {
		if len(results) >= limit {
			return
		}

		title := strings.TrimSpace(s.Find(".result__title .result__a").Text())
		href, _ := s.Find(".result__title .result__a").Attr("href")
		snippet := strings.TrimSpace(s.Find(".result__snippet").Text())

		if title == "" || href == "" {
			return
		}

		// DDG wraps URLs in a redirect; extract the actual URL
		parsed := extractDDGURL(href)

		results = append(results, Result{
			Title:       title,
			URL:         parsed,
			Description: snippet,
		})
	})

	return results, nil
}

const ddgUA = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"

func (d *DDG) fetchResults(ctx context.Context, query string) (*http.Response, error) {
	u := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))
	for range 3 {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", ddgUA)

		resp, err := d.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("ddg request failed: %w", err)
		}
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			return nil, fmt.Errorf("ddg returned status %d", resp.StatusCode)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("ddg rate limited after retries")
}

func extractDDGURL(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	if uddg := u.Query().Get("uddg"); uddg != "" {
		return uddg
	}
	return href
}
