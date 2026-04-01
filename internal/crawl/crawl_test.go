package crawl

import (
	"testing"

	"github.com/1broseidon/ketch/internal/extract"
	"github.com/1broseidon/ketch/internal/scrape"
)

func TestShouldRejectPlaceholderWithoutBrowser(t *testing.T) {
	result := &scrape.FetchResult{
		JSDetection: "likely_shell",
		Page: &scrape.Page{
			Title:    "Knowledge Base | Duo Security",
			Summary:  "Loading",
			Markdown: "Loading",
		},
	}

	if !shouldRejectPlaceholder(result, false) {
		t.Fatal("shouldRejectPlaceholder() = false, want true for low-value JS shell")
	}
	if shouldRejectPlaceholder(result, true) {
		t.Fatal("shouldRejectPlaceholder() = true, want false when browser is available")
	}
}

func TestShouldKeepRichShellContent(t *testing.T) {
	result := &scrape.FetchResult{
		JSDetection: "likely_shell",
		Page: &scrape.Page{
			Title:    "Example",
			Summary:  "Full content extracted from a rendered page",
			Markdown: "This page contains enough rendered text to keep and index safely.",
		},
	}

	if shouldRejectPlaceholder(result, false) {
		t.Fatal("shouldRejectPlaceholder() = true, want false for meaningful extracted content")
	}
}

func TestRedirectTargetForPageUsesSoftRedirectQuality(t *testing.T) {
	page := &scrape.Page{
		URL: "https://example.com/old",
		Quality: &extract.Quality{
			Indexable:      false,
			ContentType:    "soft_redirect",
			RedirectTarget: "https://example.com/new",
		},
	}

	got := redirectTargetForPage("https://example.com/old", page)
	if got != "https://example.com/new" {
		t.Fatalf("redirectTargetForPage() = %q", got)
	}
}

func TestRedirectTargetForPageSkipsSelfRedirects(t *testing.T) {
	page := &scrape.Page{
		URL: "https://example.com/old",
		Quality: &extract.Quality{
			Indexable:      false,
			ContentType:    "soft_redirect",
			RedirectTarget: "https://example.com/old",
		},
	}

	if got := redirectTargetForPage("https://example.com/old", page); got != "" {
		t.Fatalf("redirectTargetForPage() = %q, want empty self redirect", got)
	}
}
