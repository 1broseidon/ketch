package crawl

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestFeatureNormalizeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"strips fragment", "https://example.com/page#section", "https://example.com/page"},
		{"strips utm_source", "https://example.com/page?utm_source=twitter&id=1", "https://example.com/page?id=1"},
		{"strips utm_medium", "https://example.com/?utm_medium=email", "https://example.com"},
		{"strips multiple utm params", "https://example.com/p?utm_source=a&utm_medium=b&utm_campaign=c&keep=1", "https://example.com/p?keep=1"},
		{"strips trailing slash", "https://example.com/page/", "https://example.com/page"},
		{"strips fragment and trailing slash", "https://example.com/page/#top", "https://example.com/page"},
		{"preserves query params", "https://example.com/search?q=test&page=2", "https://example.com/search?page=2&q=test"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeURL(tt.input)
			if got != tt.want {
				t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFeatureSameHostFiltering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		url      string
		seedHost string
		want     bool
	}{
		{"same host passes", "https://example.com/page", "example.com", true},
		{"different host rejected", "https://other.com/page", "example.com", false},
		{"subdomain rejected", "https://sub.example.com/page", "example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := passesFilters(tt.url, tt.seedHost, nil, nil)
			if got != tt.want {
				t.Errorf("passesFilters(%q, %q) = %v, want %v", tt.url, tt.seedHost, got, tt.want)
			}
		})
	}
}

func TestFeatureAllowFilter(t *testing.T) {
	t.Parallel()

	allow := []string{"/docs", "/api"}

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"matches /docs", "https://example.com/docs/intro", true},
		{"matches /api", "https://example.com/api/v1/users", true},
		{"no match rejected", "https://example.com/blog/post", false},
		{"root rejected", "https://example.com/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := passesFilters(tt.url, "example.com", allow, nil)
			if got != tt.want {
				t.Errorf("passesFilters(%q, allow=%v) = %v, want %v", tt.url, allow, got, tt.want)
			}
		})
	}
}

func TestFeatureDenyFilter(t *testing.T) {
	t.Parallel()

	deny := []*regexp.Regexp{
		regexp.MustCompile(`/admin`),
		regexp.MustCompile(`\.pdf$`),
	}

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"normal page passes", "https://example.com/page", true},
		{"/admin denied", "https://example.com/admin/dashboard", false},
		{"PDF denied", "https://example.com/doc.pdf", false},
		{"/docs passes", "https://example.com/docs/guide", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := passesFilters(tt.url, "example.com", nil, deny)
			if got != tt.want {
				t.Errorf("passesFilters(%q, deny) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestFeatureExtractLinksFromHTML(t *testing.T) {
	t.Parallel()

	html := `<html><body>
		<a href="/about">About</a>
		<a href="https://example.com/contact">Contact</a>
		<a href="../other">Other</a>
		<a href="javascript:void(0)">JS link</a>
		<a href="mailto:test@example.com">Email</a>
		<a href="tel:+1234567890">Phone</a>
		<a href="#section">Anchor</a>
		<a href="https://external.com/page">External</a>
	</body></html>`

	links := extractLinks("https://example.com/docs/page", nil, html)

	// Should resolve relative URLs
	found := make(map[string]bool)
	for _, l := range links {
		found[l] = true
	}

	// Relative /about should resolve to https://example.com/about
	if !found["https://example.com/about"] {
		t.Error("expected /about to resolve to https://example.com/about")
	}

	// Absolute URL preserved
	if !found["https://example.com/contact"] {
		t.Error("expected absolute URL to be preserved")
	}

	// ../other should resolve relative to /docs/page
	if !found["https://example.com/other"] {
		t.Error("expected ../other to resolve to https://example.com/other")
	}

	// External links should be included (filtering is separate)
	if !found["https://external.com/page"] {
		t.Error("expected external link to be extracted")
	}

	// javascript:, mailto:, tel:, # should be skipped
	for _, l := range links {
		if strings.HasPrefix(l, "javascript:") || strings.HasPrefix(l, "mailto:") ||
			strings.HasPrefix(l, "tel:") {
			t.Errorf("should skip non-HTTP link: %s", l)
		}
	}
}

func TestFeatureSitemapXMLParsing(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		sitemap := urlSet{
			XMLName: xml.Name{Local: "urlset"},
			URLs: []urlLoc{
				{Loc: "https://example.com/page1"},
				{Loc: "https://example.com/page2"},
				{Loc: "https://example.com/page3"},
			},
		}
		data, _ := xml.Marshal(sitemap)
		w.Write(data)
	}))
	defer server.Close()

	urls, err := fetchSitemap(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("fetchSitemap error: %v", err)
	}
	if len(urls) != 3 {
		t.Fatalf("got %d URLs, want 3", len(urls))
	}

	sort.Strings(urls)
	expected := []string{
		"https://example.com/page1",
		"https://example.com/page2",
		"https://example.com/page3",
	}
	for i, want := range expected {
		if urls[i] != want {
			t.Errorf("url[%d] = %q, want %q", i, urls[i], want)
		}
	}
}

func TestFeatureSitemapIndexParsing(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	// Sitemap index points to a child sitemap
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		idx := sitemapIndex{
			XMLName:  xml.Name{Local: "sitemapindex"},
			Sitemaps: []sitemapLoc{{Loc: server.URL + "/sitemap-pages.xml"}},
		}
		data, _ := xml.Marshal(idx)
		w.Write(data)
	})

	mux.HandleFunc("/sitemap-pages.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		us := urlSet{
			XMLName: xml.Name{Local: "urlset"},
			URLs:    []urlLoc{{Loc: "https://example.com/from-index"}},
		}
		data, _ := xml.Marshal(us)
		w.Write(data)
	})

	urls, err := fetchSitemap(context.Background(), server.URL+"/sitemap.xml")
	if err != nil {
		t.Fatalf("fetchSitemap error: %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("got %d URLs, want 1", len(urls))
	}
	if urls[0] != "https://example.com/from-index" {
		t.Errorf("url = %q, want %q", urls[0], "https://example.com/from-index")
	}
}

func TestFeatureVisitedDeduplication(t *testing.T) {
	t.Parallel()

	c := &crawler{
		visited: make(map[string]bool),
	}

	// First visit should succeed
	if !c.tryVisit("https://example.com/page") {
		t.Error("first visit should return true")
	}

	// Second visit to same URL should fail
	if c.tryVisit("https://example.com/page") {
		t.Error("second visit to same URL should return false")
	}

	// Different URL should succeed
	if !c.tryVisit("https://example.com/other") {
		t.Error("visit to different URL should return true")
	}
}

func TestFeatureCompileDeny(t *testing.T) {
	t.Parallel()

	// Valid patterns compile
	regexps, err := compileDeny([]string{`/admin`, `\.pdf$`})
	if err != nil {
		t.Fatalf("compileDeny error: %v", err)
	}
	if len(regexps) != 2 {
		t.Errorf("got %d regexps, want 2", len(regexps))
	}

	// Invalid pattern returns error
	_, err = compileDeny([]string{`[invalid`})
	if err == nil {
		t.Error("expected error for invalid regex pattern")
	}
}

func TestFeatureNormalizeURLPreservesSchemeHost(t *testing.T) {
	t.Parallel()

	got := normalizeURL("https://example.com/path?key=value")
	if got != "https://example.com/path?key=value" {
		t.Errorf("normalizeURL should preserve scheme/host/path/query, got %q", got)
	}
}
