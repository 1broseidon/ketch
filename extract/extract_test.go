package extract

import (
	"strconv"
	"strings"
	"testing"
)

func TestExtractRendersHTMLTableAsPipeTable(t *testing.T) {
	t.Parallel()

	html := `<!doctype html>
<html>
<head><title>Pricing</title></head>
<body>
	<article>
		<h1>Pricing</h1>
		<p>This page describes the available pricing plans and includes a real data table for comparison.</p>
		<table>
			<thead>
				<tr><th>Plan</th><th>Price</th></tr>
			</thead>
			<tbody>
				<tr><td>Free</td><td>$0</td></tr>
				<tr><td>Pro</td><td>$20</td></tr>
			</tbody>
		</table>
	</article>
</body>
</html>`

	result, err := New().Extract("https://example.com/pricing", html)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	requirePipeTable(t, result.Markdown)
}

func TestExtractRendersNestedTableContentAsPipeTable(t *testing.T) {
	t.Parallel()

	html := `<!doctype html>
<html>
<head><title>Endpoint Pricing</title></head>
<body>
	<main>
		<article>
			<h1>Endpoint Pricing</h1>
			<p>This page compares endpoint pricing across model providers and uses component markup inside table cells.</p>
			<table role="grid">
				<thead>
					<tr><th>Model Name</th><th>Input</th><th>Output</th></tr>
				</thead>
				<tbody>
					<tr>
						<td><div><span>OpenAI</span></div><div><span>gpt-4-turbo</span></div></td>
						<td><span>$10</span></td>
						<td><span>$30</span></td>
					</tr>
				</tbody>
			</table>
		</article>
	</main>
</body>
</html>`

	result, err := New().Extract("https://example.com/pricing", html)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	requirePipeTable(t, result.Markdown)
	if !strings.Contains(result.Markdown, "| Model Name | Input | Output |") {
		t.Fatalf("expected table header row, got:\n%s", result.Markdown)
	}
	if !strings.Contains(result.Markdown, "gpt-4-turbo") || !strings.Contains(result.Markdown, "$10") {
		t.Fatalf("expected table cell content, got:\n%s", result.Markdown)
	}
}

func TestExtractReinjectsDataTableDroppedByReadability(t *testing.T) {
	t.Parallel()

	html := largeRankingFixtureHTML(false, "")
	result, err := New().Extract("https://example.com/economy", html)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	requirePipeTable(t, result.Markdown)
	for _, want := range []string{
		"| 1 | United States | 29000 |",
		"| 2 | China | 18500 |",
		"| 223 | Economy 223 | 9777 |",
	} {
		if !strings.Contains(result.Markdown, want) {
			t.Fatalf("expected re-injected ranking row %q, got:\n%s", want, result.Markdown)
		}
	}
}

func TestExtractDoesNotDuplicateDataTableKeptByReadability(t *testing.T) {
	t.Parallel()

	html := largeRankingFixtureHTML(true, "")
	result, err := New().Extract("https://example.com/economy", html)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	row := "| 1 | United States | 29000 |"
	if got := strings.Count(result.Markdown, row); got != 1 {
		t.Fatalf("expected ranking row once, got %d occurrences:\n%s", got, result.Markdown)
	}
}

func TestExtractDoesNotReinjectLayoutTable(t *testing.T) {
	t.Parallel()

	for _, tableClass := range []string{"navbox", "sidebar", "toc", "ambox", "toccolours"} {
		tableClass := tableClass
		t.Run(tableClass, func(t *testing.T) {
			t.Parallel()

			html := largeRankingFixtureHTML(false, tableClass)
			result, err := New().Extract("https://example.com/economy", html)
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}

			if strings.Contains(result.Markdown, "United States") || strings.Contains(result.Markdown, "China") {
				t.Fatalf("expected layout table to stay out of output, got:\n%s", result.Markdown)
			}
		})
	}
}

func TestExtractDoesNotReinjectPresentationTable(t *testing.T) {
	t.Parallel()

	html := largeRankingFixtureHTML(false, "")
	html = strings.Replace(html, `<table class="wikitable sortable ">`, `<table role="presentation" class="wikitable sortable ">`, 1)
	result, err := New().Extract("https://example.com/economy", html)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	if strings.Contains(result.Markdown, "United States") || strings.Contains(result.Markdown, "China") {
		t.Fatalf("expected presentation table to stay out of output, got:\n%s", result.Markdown)
	}
}

func TestExtractSelectorRendersHTMLTableAsPipeTable(t *testing.T) {
	t.Parallel()

	html := `<!doctype html>
<html>
<body>
	<main>
		<table>
			<tr><th>Plan</th><th>Price</th></tr>
			<tr><td>Free</td><td>$0</td></tr>
			<tr><td>Pro</td><td>$20</td></tr>
		</table>
	</main>
</body>
</html>`

	markdown, err := ExtractSelector(html, "main")
	if err != nil {
		t.Fatalf("ExtractSelector() error = %v", err)
	}
	requirePipeTable(t, markdown)
}

func largeRankingFixtureHTML(tableInsideArticle bool, tableClass string) string {
	rows := make([]string, 0, 223)
	seedRows := []struct {
		rank    int
		country string
		gdp     string
	}{
		{1, "United States", "29000"},
		{2, "China", "18500"},
		{3, "Germany", "4600"},
		{4, "Japan", "4200"},
		{5, "India", "3900"},
		{6, "United Kingdom", "3500"},
		{7, "France", "3100"},
		{8, "Italy", "2300"},
		{9, "Brazil", "2200"},
		{10, "Canada", "2100"},
	}
	for _, row := range seedRows {
		rows = append(rows, rankingRow(row.rank, row.country, row.gdp))
	}
	for rank := 11; rank <= 223; rank++ {
		rows = append(rows, rankingRow(rank, "Economy "+strconv.Itoa(rank), strconv.Itoa(10000-rank)))
	}

	table := `<section class="data-section"><h2>GDP ranking</h2><table class="wikitable sortable ` + tableClass + `">
<thead><tr><th>Rank</th><th>Country</th><th>GDP</th></tr></thead>
<tbody>` + strings.Join(rows, "\n") + `</tbody>
</table></section>`

	paragraphs := strings.Repeat(`<p>The economic report explains the current outlook with enough article text for readability to select this article body as the main content.</p>`, 8)
	article := `<article><h1>Economic report</h1>` + paragraphs + `</article>`
	if tableInsideArticle {
		article = `<article><h1>Economic report</h1>` + paragraphs + table + `</article>`
		table = ""
	}

	return `<!doctype html>
<html>
<head><title>Economic report</title></head>
<body>
<header><nav>Home About Contact</nav></header>
<main>` + article + table + `</main>
<footer>Footer links</footer>
</body>
</html>`
}

func rankingRow(rank int, country, gdp string) string {
	return `<tr><td>` + strconv.Itoa(rank) + `</td><td>` + country + `</td><td>` + gdp + `</td></tr>`
}

func requirePipeTable(t *testing.T, markdown string) {
	t.Helper()

	if !strings.Contains(markdown, "|") {
		t.Fatalf("expected markdown pipe table, got:\n%s", markdown)
	}
	if !strings.Contains(markdown, "---") {
		t.Fatalf("expected markdown table header separator, got:\n%s", markdown)
	}
}
