package extract

import (
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

func requirePipeTable(t *testing.T, markdown string) {
	t.Helper()

	if !strings.Contains(markdown, "|") {
		t.Fatalf("expected markdown pipe table, got:\n%s", markdown)
	}
	if !strings.Contains(markdown, "---") {
		t.Fatalf("expected markdown table header separator, got:\n%s", markdown)
	}
}
