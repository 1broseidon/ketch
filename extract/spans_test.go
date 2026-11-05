package extract

import (
	"strings"
	"testing"
)

func TestClampSpans(t *testing.T) {
	in := `<th colSpan="9007199254740991">a</th><td rowspan=2>b</td><td colspan='250'>c</td><td colspan="100">d</td>`
	want := `<th colSpan="100">a</th><td rowspan=2>b</td><td colspan='100'>c</td><td colspan="100">d</td>`
	if got := clampSpans(in); got != want {
		t.Errorf("clampSpans = %s, want %s", got, want)
	}
}

// A header cell spanning Number.MAX_SAFE_INTEGER columns converts in
// bounded time and memory.
func TestHugeColspanConverts(t *testing.T) {
	html := `<table><thead><tr><th colSpan="9007199254740991">Codes</th></tr></thead><tbody><tr><td>a</td><td>b</td></tr></tbody></table>`
	md, err := toMarkdown(html)
	if err != nil || !strings.Contains(md, "Codes") {
		t.Fatalf("toMarkdown = %q, %v", md, err)
	}
}
