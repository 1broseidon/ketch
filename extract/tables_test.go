package extract

import (
	"strings"
	"testing"
)

// A table whose cells hold blocks the table plugin cannot put in a row (a
// list, a code block, a quote, a rule, a heading: an option table listing
// its values, a quota table with a note heading in a cell) is still a
// Markdown table, each block a line of its cell; a table that cannot be one
// (a table in a cell, a layout table) prints each of its cells on its own,
// never run together ("NameDescription").
func TestTableCellsKeepTheirBoundaries(t *testing.T) {
	t.Parallel()
	row := func(cell string) string {
		return `<!doctype html><html><body><table id="t"><tr><th>Name</th><th>Description</th></tr>` +
			`<tr><td><code>k1</code></td><td>` + cell + `</td></tr><tr><td><code>k2</code></td><td>plain</td></tr></table></body></html>`
	}
	for cell, want := range map[string]string{
		`<ul><li>fast</li><li>slow <code>x</code></li></ul>`:    "|`k1`|• fast  <br />• slow `x`|",
		`<ol><li>one</li><li>two</li></ol>`:                     "|`k1`|1\\. one  <br />2\\. two|",
		`<blockquote>quoted</blockquote>`:                       "|`k1`|quoted|",
		`a<hr>b`:                                                "|`k1`|a  <br />b|",
		`<h6>Note</h6><p>Shared with other services.</p>`:       "|`k1`|Note<br /><br />Shared with other services.|",
		"<pre><code>line1\nline2</code></pre>":                  "|`k1`|`line1`  <br />`line2`|",
		`<div><p>p1</p></div><div><div><p>p2</p></div></div>`:   "|`k1`|p1<br /><br />p2|",
		`<p>Values:</p><ul><li>a<ul><li>a1</li></ul></li></ul>`: "• a1",
	} {
		md, err := ExtractSelector(row(cell), "#t")
		if err != nil {
			t.Fatal(err)
		}
		assertContainsAll(t, md, "|Name|Description|", "|---|---|", want, "|`k2`|plain|")
	}
	nested, err := ExtractSelector(row(`<table><tr><td>x</td><td>y</td></tr></table>`), "#t")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(nested, "NameDescription") || strings.Contains(nested, "`k2`plain") {
		t.Errorf("an outer table's cells run together:\n%s", nested)
	}
	assertContainsAll(t, nested, "|x|y|")
	layout, err := ExtractSelector(`<html><body><div id="t"><table role="presentation"><tr><td>Nav</td><td>Layout</td></tr></table></div></body></html>`, "#t")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(layout, "NavLayout") || strings.Contains(layout, "|Nav|") {
		t.Errorf("a layout table's cells: %q", layout)
	}
}
