package extract

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestExtractReadabilityRendersCleanTheadTable(t *testing.T) {
	t.Parallel()

	html := `<!doctype html>
<html>
<head><title>City Ages</title></head>
<body>
	<nav>Ignored navigation</nav>
	<main>
		<article>
			<h1>City Ages</h1>
			<p>This article has enough prose for readability to keep the main content and the table below describes the sample data.</p>
			<table>
				<thead>
					<tr><th>Name</th><th>City</th><th>Age</th></tr>
				</thead>
				<tbody>
					<tr><td>Max</td><td>Berlin</td><td>20</td></tr>
					<tr><td>Ada</td><td>London</td><td>37</td></tr>
				</tbody>
			</table>
		</article>
	</main>
</body>
</html>`

	result, err := New().Extract("https://example.test/cities", html)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	assertContainsAll(t, result.Markdown,
		"|Name|City|Age|",
		"|---|---|---|",
		"|Max|Berlin|20|",
		"|Ada|London|37|",
	)
}

func TestExtractRawPromotesHeaderForNoTheadTable(t *testing.T) {
	t.Parallel()

	result, err := extractRaw("", `<!doctype html>
<html>
<head><title>Pricing</title></head>
<body>
	<main>
		<table>
			<tbody>
				<tr><td>Plan</td><td>Price</td></tr>
				<tr><td>Free</td><td>$0</td></tr>
				<tr><td>Pro</td><td>$10</td></tr>
			</tbody>
		</table>
	</main>
</body>
</html>`)
	if err != nil {
		t.Fatalf("extractRaw: %v", err)
	}
	assertContainsAll(t, result.Markdown,
		"|Plan|Price|",
		"|---|---|",
		"|Free|$0|",
		"|Pro|$10|",
	)
}

func TestExtractSelectorRendersColspanRowspanWithEmptyCells(t *testing.T) {
	t.Parallel()

	markdown, err := ExtractSelector(`<!doctype html>
<html><body>
	<table id="plans">
		<thead><tr><th>Tier</th><th>Input</th><th>Output</th></tr></thead>
		<tbody>
			<tr><td rowspan="2">Pro</td><td colspan="2">Included</td></tr>
			<tr><td>$1</td><td>$2</td></tr>
		</tbody>
	</table>
</body></html>`, "#plans")
	if err != nil {
		t.Fatalf("ExtractSelector: %v", err)
	}
	assertContainsAll(t, markdown,
		"|Tier|Input|Output|",
		"|---|---|---|",
		"|Pro|Included||",
		"||$1|$2|",
	)
	if strings.Contains(markdown, "Included|Included") {
		t.Fatalf("colspan cells must be empty, not mirrored:\n%s", markdown)
	}
}

func TestExtractSelectorPreservesMultilineCellTable(t *testing.T) {
	t.Parallel()

	markdown, err := ExtractSelector(`<!doctype html>
<html><body>
	<table id="features">
		<tbody>
			<tr><td>Feature</td><td>Details</td></tr>
			<tr><td>Support</td><td>Line one<br>Line two</td></tr>
		</tbody>
	</table>
</body></html>`, "#features")
	if err != nil {
		t.Fatalf("ExtractSelector: %v", err)
	}
	assertContainsAll(t, markdown,
		"|Feature|Details|",
		"|---|---|",
		"|Support|Line one  <br />Line two|",
	)
}

func TestExtractSelectorSkipsPresentationLayoutTable(t *testing.T) {
	t.Parallel()

	markdown, err := ExtractSelector(`<!doctype html>
<html><body>
	<table id="layout" role="presentation">
		<tr><td>Nav</td><td>Layout</td></tr>
		<tr><td>A</td><td>B</td></tr>
	</table>
</body></html>`, "#layout")
	if err != nil {
		t.Fatalf("ExtractSelector: %v", err)
	}
	if strings.Contains(markdown, "|---|") || strings.Contains(markdown, "|Nav|Layout|") {
		t.Fatalf("presentation table rendered as a pipe table:\n%s", markdown)
	}
	assertContainsAll(t, markdown, "Nav", "Layout", "A", "B")
}

func TestTokenReplyPricingFixtureHasNoServerRenderedTable(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("testdata/tokenreply-pricing.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lower := strings.ToLower(string(body))
	assertContainsAll(t, lower, "tokenreply", "pricing")
	if strings.Contains(lower, "<table") {
		t.Fatalf("fixture unexpectedly contains a server-rendered table")
	}
}

func TestExtractFallsBackToRawWhenReadabilityDropsTable(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("testdata/tokenreply-pricing.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	withTable := strings.Replace(
		string(body),
		`<div id="root"></div>`,
		`<div id="root"><table><tr><td>Plan</td><td>Price</td></tr><tr><td>Pro</td><td>$10</td></tr></table></div>`,
		1,
	)
	result, err := New().Extract("https://www.tokenreply.com/pricing", withTable)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if strings.TrimSpace(result.Markdown) == "" {
		t.Fatalf("expected raw-table fallback, got empty markdown")
	}
	assertContainsAll(t, result.Markdown, "|Plan|Price|", "|---|---|", "|Pro|$10|")
}

// Regression for issue #28: when readability keeps a small table (a Wikipedia
// infobox) but drops the main data table, the raw-table fallback must still fire.
// The old guard bailed as soon as any pipe table was present, so the infobox
// masked the dropped data table and it silently vanished from the output.
func TestExtractFallsBackToRawWhenInfoboxMasksDroppedDataTable(t *testing.T) {
	t.Parallel()

	html := `<!doctype html>
<html><head><title>List of FIFA World Cup finals</title></head>
<body>
<div id="content">
	<div id="intro">
		<h1>List of FIFA World Cup finals</h1>
		<table class="infobox"><tbody>
			<tr><th>Founded</th><td>1930</td></tr>
			<tr><th>Most titles</th><td>Brazil (5)</td></tr>
		</tbody></table>
		<p>The FIFA World Cup is an international association football competition contested by the senior men's national teams of the member associations of FIFA. The championship has been awarded every four years since the inaugural tournament in 1930, except in 1942 and 1946 when it was not held because of the Second World War. This intro is padded with enough prose that readability locks onto this container as its single top candidate and scores it far above the bare results table below.</p>
	</div>
	<div id="tables">
		<h2>Results</h2>
		<table class="wikitable sortable"><tbody>
			<tr><th><a href="/y">Year</a></th><th><a href="/w">Winners</a></th><th><a href="/s">Score</a></th><th><a href="/r">Runners-up</a></th></tr>
			<tr><td><a href="/y/1930">1930</a></td><td><a href="/w/uruguay">Uruguay</a></td><td><a href="/s/1">4-2</a></td><td><a href="/w/argentina">Argentina</a></td></tr>
			<tr><td><a href="/y/2022">2022</a></td><td><a href="/w/argentina">Argentina</a></td><td><a href="/s/3">3-3</a></td><td><a href="/w/france">France</a></td></tr>
		</tbody></table>
	</div>
</div>
</body></html>`

	result, err := New().Extract("https://en.wikipedia.org/wiki/List_of_FIFA_World_Cup_finals", html)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Infobox (the small table readability kept) must still be present...
	assertContainsAll(t, result.Markdown, "|Founded|1930|")
	// ...and so must the main data table that readability dropped: its four
	// column headers and a four-column delimiter row (which only the data
	// table produces — the infobox has two columns).
	assertContainsAll(t, result.Markdown,
		"|---|---|---|---|",
		"Year",
		"Winners",
		"Runners-up",
		"Uruguay",
		"France",
	)
}

// Regression for issue #28: the raw-table fallback must not fire for tables
// that carry no content. Swapping readability's output for the full-page
// conversion costs the reader the entire site chrome, so it has to buy a real
// data table — not a footer, a prev/next bar, or a hidden shortcut dialog.
func TestExtractKeepsReadabilityWhenExtraTableIsChrome(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		table  string
		marker string
	}{
		{
			name:   "footer",
			table:  `<footer><table><tr><td>Legal</td><td>Privacy</td></tr><tr><td>Terms</td><td>Cookies</td></tr></table></footer>`,
			marker: "Cookies",
		},
		{
			// DocBook labels its prev/next chrome instead of wrapping it in <nav>.
			name:   "docbook navigation",
			table:  `<table summary="Navigation header"><tr><td>Chapter 8</td><td></td></tr><tr><td>Prev</td><td>Next</td></tr></table>`,
			marker: "Prev",
		},
		{
			name:   "hidden dialog",
			table:  `<dialog class="ShortcutsDialog"><table><tr><td>?</td><td>This menu</td></tr><tr><td>/</td><td>Search docs</td></tr></table></dialog>`,
			marker: "Search docs",
		},
		{
			name:   "presentation role",
			table:  `<table role="presentation"><tr><td>Sidebar</td><td>Ads</td></tr><tr><td>Related</td><td>Sponsored</td></tr></table>`,
			marker: "Sponsored",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := New().Extract("https://example.com/pricing", chromePage(tc.table))
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			// The real table readability already renders correctly...
			assertContainsAll(t, result.Markdown, "|Plan|Price|", "|Pro|$10|", "|Team|$30|")
			// ...must not have cost us the readability cleanup.
			assertContainsNone(t, result.Markdown, "SITECHROME", tc.marker)
		})
	}
}

// The raw fallback bypasses readability, which is what resolves relative
// hrefs — so the raw path has to absolutize them itself, or every link in the
// table it just recovered points nowhere.
func TestExtractRawFallbackResolvesRelativeLinks(t *testing.T) {
	t.Parallel()

	html := chromePage(`<div id="data"><table class="wikitable">
		<tr><th>Year</th><th>Winner</th></tr>
		<tr><td><a href="/year/1930">1930</a></td><td><a href="/team/uruguay">Uruguay</a></td></tr>
		<tr><td><a href="/year/2022">2022</a></td><td><a href="/team/argentina">Argentina</a></td></tr>
	</table></div>`)

	result, err := New().Extract("https://example.com/pricing", html)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	assertContainsAll(t, result.Markdown,
		"https://example.com/year/1930",
		"https://example.com/team/uruguay",
	)
	assertContainsNone(t, result.Markdown, "](/year/1930)", "](/team/uruguay)")
}

// chromePage builds an article whose data table readability keeps on its own,
// wrapped in navigation that only the raw conversion would pull in.
func chromePage(extra string) string {
	return `<!doctype html>
<html><head><title>Pricing Guide</title></head>
<body>
<nav><ul><li><a href="/docs">SITECHROME</a></li></ul></nav>
<article>
	<h1>Pricing Guide</h1>
	<p>Choosing a plan depends on how much throughput you need and whether you want dedicated support. This paragraph is padded with enough prose that readability confidently locks onto the article element as the top scoring candidate for the main content of this page, well above the navigation and footer chrome that surrounds it on every page of the site.</p>
	<table><tbody>
		<tr><th>Plan</th><th>Price</th></tr>
		<tr><td>Pro</td><td>$10</td></tr>
		<tr><td>Team</td><td>$30</td></tr>
	</tbody></table>
	<p>All plans include unlimited seats and a thirty day money back guarantee, so you can evaluate the product without committing to an annual contract up front. Support response times differ between the tiers described in the table above this paragraph.</p>
</article>
` + extra + `
</body></html>`
}

func TestStripDataURIsReplacesBase64ImgSource(t *testing.T) {
	t.Parallel()

	payload := "iVBORw0KGgo=" // short fake PNG header (base64)
	html := fmt.Sprintf(`<html><body><img src="data:image/png;base64,%s" alt="chart" /></body></html>`, payload)

	out := stripDataURIs(html)

	if strings.Contains(out, "data:image") {
		t.Fatalf("data URI not stripped:\n%s", out)
	}
	if !strings.Contains(out, "data-uri omitted: image/png") {
		t.Fatalf("marker not present or wrong format in output:\n%s", out)
	}
}

func TestStripDataURIsLeavesNormalImagesAlone(t *testing.T) {
	t.Parallel()

	html := `<html><body><img src="https://example.com/photo.png" alt="photo" /></body></html>`
	out := stripDataURIs(html)

	if !strings.Contains(out, "https://example.com/photo.png") {
		t.Fatalf("normal image src was removed:\n%s", out)
	}
}

func TestExtractStripsDataURIsFromMarkdownOutput(t *testing.T) {
	t.Parallel()

	// A page with enough prose for readability, plus a base64 image.
	// Readability may drop the empty-src img after stripping, but the
	// data URI must never leak into the output regardless.
	html := `<!doctype html><html><head><title>Report</title></head><body>
<article>
<p>This paragraph has enough content for readability to extract it as the main article content on this page, along with any embedded images that follow this prose section.</p>
<img src="data:image/jpeg;base64,/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAMCAgMCAgMDAwMEAwMEBQgFBQQEBQoH" alt="chart" />
</article>
</body></html>`

	result, err := New().Extract("https://example.com/report", html)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if strings.Contains(result.Markdown, "data:image") {
		t.Fatalf("data URI leaked into markdown:\n%s", result.Markdown)
	}
}

func TestExtractRawStripsDataURIsAndEmitsMarker(t *testing.T) {
	t.Parallel()

	// Raw conversion (no readability) preserves the marker alt text.
	html := `<html><head><title>Report</title></head><body>
<img src="data:image/jpeg;base64,/9j/4AAQSkZJRgABAQAAAQABAAD" alt="chart" />
<p>Some page content.</p>
</body></html>`

	stripped := stripDataURIs(html)
	result, err := extractRaw("", stripped)
	if err != nil {
		t.Fatalf("extractRaw: %v", err)
	}
	if strings.Contains(result.Markdown, "data:image") {
		t.Fatalf("data URI leaked into raw markdown:\n%s", result.Markdown)
	}
	if !strings.Contains(result.Markdown, "data-uri omitted") {
		t.Fatalf("marker missing from raw markdown:\n%s", result.Markdown)
	}
}

func assertContainsNone(t *testing.T, got string, unwanted ...string) {
	t.Helper()
	for _, bad := range unwanted {
		if strings.Contains(got, bad) {
			t.Fatalf("unexpected %q in:\n%s", bad, got)
		}
	}
}

func assertContainsAll(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
