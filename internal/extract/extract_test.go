package extract

import (
	"fmt"
	"strings"
	"testing"
)

func TestExtractIncludesMetadata(t *testing.T) {
	html := `
	<html>
	  <head>
	    <title>Example Doc</title>
	    <meta name="description" content="A concise summary for the page." />
	    <link rel="canonical" href="/docs/example" />
	  </head>
	  <body>
	    <main>
	      <h1>Getting Started</h1>
	      <p>First paragraph for users.</p>
	      <h2>Install</h2>
	      <p>Install steps go here.</p>
	      <a href="/docs/install">Install guide</a>
	      <a href="https://external.example.com/reference">External reference</a>
	    </main>
	  </body>
	</html>`

	result, err := New().Extract("https://example.com/start", html)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.Title == "" {
		t.Fatal("Title is empty")
	}
	if result.Summary != "A concise summary for the page." {
		t.Fatalf("Summary = %q", result.Summary)
	}
	if result.CanonicalURL != "https://example.com/docs/example" {
		t.Fatalf("CanonicalURL = %q", result.CanonicalURL)
	}
	if len(result.Headings) < 2 {
		t.Fatalf("Headings = %#v", result.Headings)
	}
	if result.Headings[0].Text != "Getting Started" {
		t.Fatalf("First heading = %#v", result.Headings[0])
	}
	if len(result.Links) != 2 {
		t.Fatalf("Links = %#v", result.Links)
	}
	if result.ExtractionSource == "" {
		t.Fatal("ExtractionSource is empty")
	}
}

func TestExtractFallsBackToMarkdownSummary(t *testing.T) {
	html := `<html><head><title>Fallback</title></head><body><p>First useful paragraph for fallback summary generation.</p></body></html>`

	result, err := New().Extract("https://example.com/fallback", html)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.Summary == "" {
		t.Fatal("Summary is empty")
	}
}

func TestExtractDetectsSoftRedirectQuality(t *testing.T) {
	html := `
	<html>
	  <head>
	    <title>Redirect Notice</title>
	    <meta http-equiv="refresh" content="0;url=/docs/new-location" />
	  </head>
	  <body>
	    <main>
	      <p>This page has moved.</p>
	      <a href="/docs/new-location">Continue</a>
	    </main>
	  </body>
	</html>`

	result, err := New().Extract("https://example.com/docs/old-location", html)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.Quality == nil {
		t.Fatal("Quality is nil")
	}
	if result.Quality.Indexable {
		t.Fatal("Quality.Indexable = true, want false for soft redirect")
	}
	if result.Quality.ContentType != "soft_redirect" {
		t.Fatalf("ContentType = %q", result.Quality.ContentType)
	}
	if result.Quality.RedirectTarget != "https://example.com/docs/new-location" {
		t.Fatalf("RedirectTarget = %q", result.Quality.RedirectTarget)
	}
}

func TestExtractMarksNavigationHeavyHub(t *testing.T) {
	html := `
	<html>
	  <head><title>Documentation Home</title></head>
	  <body>
	    <main>
	      <h1>Documentation</h1>
	      <p>Choose a guide below.</p>
	      <ul>
	        <li><a href="/docs/a">A</a></li>
	        <li><a href="/docs/b">B</a></li>
	        <li><a href="/docs/c">C</a></li>
	        <li><a href="/docs/d">D</a></li>
	        <li><a href="/docs/e">E</a></li>
	        <li><a href="/docs/f">F</a></li>
	        <li><a href="/docs/g">G</a></li>
	        <li><a href="/docs/h">H</a></li>
	        <li><a href="/docs/i">I</a></li>
	        <li><a href="/docs/j">J</a></li>
	        <li><a href="/docs/k">K</a></li>
	        <li><a href="/docs/l">L</a></li>
	        <li><a href="/docs/m">M</a></li>
	        <li><a href="/docs/n">N</a></li>
	        <li><a href="/docs/o">O</a></li>
	        <li><a href="/docs/p">P</a></li>
	      </ul>
	    </main>
	  </body>
	</html>`

	result, err := New().Extract("https://example.com/docs", html)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.Quality == nil {
		t.Fatal("Quality is nil")
	}
	if !result.Quality.NavigationHeavy {
		t.Fatal("NavigationHeavy = false, want true")
	}
	if result.Quality.ContentType != "hub" {
		t.Fatalf("ContentType = %q, want hub", result.Quality.ContentType)
	}
	if !result.Quality.Indexable {
		t.Fatal("Indexable = false, want true for hub pages")
	}
}

func TestExtractMarksTutorialListsAsNavigationHeavyHub(t *testing.T) {
	html := `
	<html>
	  <head><title>SaaS App configuration guides for Microsoft Entra ID</title></head>
	  <body>
	    <main>
	      <h1>SaaS App configuration guides for Microsoft Entra ID</h1>
	      <p>Browse setup guides for many apps.</p>
	      <a href="/en-us/entra/identity/saas-apps/a">A</a>
	      <a href="/en-us/entra/identity/saas-apps/b">B</a>
	      <a href="/en-us/entra/identity/saas-apps/c">C</a>
	      <a href="/en-us/entra/identity/saas-apps/d">D</a>
	      <a href="/en-us/entra/identity/saas-apps/e">E</a>
	      <a href="/en-us/entra/identity/saas-apps/f">F</a>
	      <a href="/en-us/entra/identity/saas-apps/g">G</a>
	      <a href="/en-us/entra/identity/saas-apps/h">H</a>
	      <a href="/en-us/entra/identity/saas-apps/i">I</a>
	      <a href="/en-us/entra/identity/saas-apps/j">J</a>
	      <a href="/en-us/entra/identity/saas-apps/k">K</a>
	      <a href="/en-us/entra/identity/saas-apps/l">L</a>
	      <a href="/en-us/entra/identity/saas-apps/m">M</a>
	      <a href="/en-us/entra/identity/saas-apps/n">N</a>
	      <a href="/en-us/entra/identity/saas-apps/o">O</a>
	      <a href="/en-us/entra/identity/saas-apps/p">P</a>
	      <a href="/en-us/entra/identity/saas-apps/q">Q</a>
	      <a href="/en-us/entra/identity/saas-apps/r">R</a>
	      <a href="/en-us/entra/identity/saas-apps/s">S</a>
	      <a href="/en-us/entra/identity/saas-apps/t">T</a>
	    </main>
	  </body>
	</html>`

	result, err := New().Extract("https://learn.microsoft.com/en-us/entra/identity/saas-apps/tutorial-list", html)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.Quality == nil || !result.Quality.NavigationHeavy || result.Quality.ContentType != "hub" {
		t.Fatalf("Quality = %#v, want tutorial list hub classification", result.Quality)
	}
}

func TestExtractKeepsBlogListingAsArticle(t *testing.T) {
	html := `
	<html>
	  <head><title>Duo Blog</title></head>
	  <body>
	    <main>
	      <h1>Duo Blog</h1>
	      <p>Explore recent security insights from the Duo team with detailed summaries.</p>
	      <p>Each post includes authors, dates, and strong topical context for readers.</p>
	      <a href="/blog/post-a">A</a>
	      <a href="/blog/post-b">B</a>
	      <a href="/blog/post-c">C</a>
	      <a href="/blog/post-d">D</a>
	      <a href="/blog/post-e">E</a>
	      <a href="/blog/post-f">F</a>
	      <a href="/blog/post-g">G</a>
	      <a href="/blog/post-h">H</a>
	      <a href="/blog/post-i">I</a>
	      <a href="/blog/post-j">J</a>
	      <a href="/blog/post-k">K</a>
	      <a href="/blog/post-l">L</a>
	      <a href="/blog/post-m">M</a>
	      <a href="/blog/post-n">N</a>
	      <a href="/blog/post-o">O</a>
	      <a href="/blog/post-p">P</a>
	    </main>
	  </body>
	</html>`

	result, err := New().Extract("https://duo.com/blog", html)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if result.Quality == nil {
		t.Fatal("Quality is nil")
	}
	if result.Quality.NavigationHeavy {
		t.Fatalf("Quality = %#v, want article classification for blog listing", result.Quality)
	}
	if result.Quality.ContentType != "article" {
		t.Fatalf("ContentType = %q, want article", result.Quality.ContentType)
	}
}

func TestExtractPreservesLargeLinkSets(t *testing.T) {
	var body strings.Builder
	body.WriteString(`<html><head><title>Docs Hub</title></head><body><main>`)
	for i := 0; i < 120; i++ {
		body.WriteString(fmt.Sprintf(`<a href="/docs/page-%03d">Page %03d</a>`, i, i))
	}
	body.WriteString(`</main></body></html>`)

	result, err := New().Extract("https://example.com/docs", body.String())
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(result.Links) != 120 {
		t.Fatalf("len(Links) = %d, want 120", len(result.Links))
	}
}
