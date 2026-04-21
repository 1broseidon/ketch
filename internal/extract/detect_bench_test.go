package extract

import (
	"strings"
	"testing"
)

// benchRealisticHTML is a representative mid-sized page with a mix of
// static article content and inline scripts — the common detect-path input.
func benchRealisticHTML() string {
	para := `<p>This paragraph exists to push the visible text above the detection
	threshold and to give the script-to-text ratio something to chew on. The
	detector walks meaningful blocks and measures visible text; larger inputs
	exercise the traversal cost more than tiny fixtures do.</p>`
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><title>Realistic</title>`)
	// ~8 KB of inline script simulating analytics/bootstrap.
	b.WriteString(`<script>`)
	b.WriteString(strings.Repeat(`window.__DATA__={"k":"v","n":1};`, 200))
	b.WriteString(`</script></head><body><div id="__next"><main><article><h1>Article</h1>`)
	b.WriteString(strings.Repeat(para, 40))
	b.WriteString(`</article></main></div>`)
	b.WriteString(`<script>`)
	b.WriteString(strings.Repeat(`console.log("load");`, 200))
	b.WriteString(`</script></body></html>`)
	return b.String()
}

func BenchmarkDetectJSShell(b *testing.B) {
	html := benchRealisticHTML()
	b.SetBytes(int64(len(html)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DetectJSShell(html)
	}
}

// benchShellHTML is a JS-shell page (hits most corroborator paths).
func benchShellHTML() string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><title>Shell</title>`)
	b.WriteString(`<script>`)
	b.WriteString(strings.Repeat(`window.__BOOTSTRAP__={"r":["a"]};`, 400))
	b.WriteString(`</script></head><body><div id="app">Loading</div>`)
	b.WriteString(`<noscript>This app requires JavaScript.</noscript>`)
	b.WriteString(`</body></html>`)
	return b.String()
}

func BenchmarkDetectJSShellShell(b *testing.B) {
	html := benchShellHTML()
	b.SetBytes(int64(len(html)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DetectJSShell(html)
	}
}
