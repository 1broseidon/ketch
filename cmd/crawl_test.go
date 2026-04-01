package cmd

import (
	"testing"

	"github.com/1broseidon/ketch/internal/crawl"
	"github.com/1broseidon/ketch/internal/extract"
	"github.com/1broseidon/ketch/internal/scrape"
)

func TestBuildCrawlJSONResultWithPage(t *testing.T) {
	r := crawl.Result{
		URL:    "https://example.com/docs/page",
		Depth:  2,
		Status: "changed",
		Source: "link",
		Page: &scrape.Page{
			URL:          "https://example.com/docs/page",
			Title:        "Example Page",
			Markdown:     "# Example Page\n\nThis is the first paragraph of content.",
			Quality:      &extract.Quality{Indexable: true, ContentType: "article"},
			ETag:         "etag-1",
			LastModified: "Wed, 26 Mar 2026 10:00:00 GMT",
			ContentHash:  "abc123",
		},
	}

	got := buildCrawlJSONResult(r)
	if got.Depth != 2 {
		t.Fatalf("Depth = %d, want 2", got.Depth)
	}
	if got.Page == nil {
		t.Fatal("Page = nil, want page data")
	}
	if got.Page.Summary != "This is the first paragraph of content." {
		t.Fatalf("Summary = %q", got.Page.Summary)
	}
	if got.Page.Words == 0 {
		t.Fatal("Words = 0, want positive word count")
	}
	if got.Page.Quality == nil || !got.Page.Quality.Indexable {
		t.Fatalf("Quality = %#v, want indexable article metadata", got.Page.Quality)
	}
}

func TestBuildCrawlJSONResultForError(t *testing.T) {
	r := crawl.Result{
		URL:   "https://example.com/fail",
		Depth: 1,
		Error: "timeout",
	}

	got := buildCrawlJSONResult(r)
	if got.Status != "error" {
		t.Fatalf("Status = %q, want error", got.Status)
	}
	if got.Page != nil {
		t.Fatal("Page != nil, want nil for error result")
	}
}
