package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/crawl"
	"github.com/1broseidon/ketch/scrape"
)

func TestCrawlCollectorReturnsFallbackPageWithError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	col := &crawlCollector{ctx: ctx, cancel: cancel, maxPages: 3}
	col.collect(crawl.Result{
		Page:   &scrape.Page{URL: "https://example.com", Title: "cached", Markdown: "saved page"},
		Status: "unchanged",
		URL:    "https://example.com",
		Error:  "link discovery incomplete; cached page returned after backfill failed: upstream unavailable",
	})
	if len(col.pages) != 1 || col.pages[0].Title != "cached" {
		t.Fatalf("pages = %+v, want the cached page", col.pages)
	}
	if len(col.errs) != 1 || !strings.Contains(col.errs[0].Error, "link discovery incomplete") {
		t.Fatalf("errors = %+v, want partial-crawl diagnostic", col.errs)
	}
}
