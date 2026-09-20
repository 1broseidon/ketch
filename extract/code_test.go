package extract

import (
	"strings"
	"testing"
)

// These reduced fixtures reproduce pkg.go.dev's highlighted comments and
// npm's Prism line wrappers without depending on live documentation.
func TestExtractPreservesHighlightedCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		html string
		want string
	}{
		{
			name: "Go interface comments",
			html: `<pre>type Context interface {
	<span id="Context.Done"><span class="comment">// Done can close after cancel returns.</span>
	Done() &lt;-chan struct{}</span>
}</pre>`,
			want: "type Context interface {\n\t// Done can close after cancel returns.\n\tDone() <-chan struct{}\n}",
		},
		{
			name: "Prism YAML lines",
			html: `<pre class="prism-code language-yaml"><div class="token-line"><span>jobs:</span></div><div class="token-line">  publish:</div><div class="token-line">    permissions:</div><div class="token-line">      id-token: write <span class="token comment"># Required</span></div></pre>`,
			want: "```yaml\njobs:\n  publish:\n    permissions:\n      id-token: write # Required\n```",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			page := `<html><head><title>Developer guide</title></head><body><nav>SITECHROME</nav><article><h1>Developer guide</h1><p>` +
				strings.Repeat("This guide explains the operation and its cleanup requirements. ", 12) + `</p>` + tc.html + `</article></body></html>`
			result, err := New().Extract("https://example.test/docs/guide", page)
			if err != nil {
				t.Fatal(err)
			}
			assertContainsAll(t, result.Markdown, tc.want)
			assertContainsNone(t, result.Markdown, "SITECHROME")
		})
	}
}

func TestCodeWhitespaceAcrossConversionPaths(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, html, want string }{
		{"plain", "<pre><code>\tfirst\n\n  second &lt; third\n</code></pre>", "\tfirst\n\n  second < third"},
		{"block lines", `<pre><code class="language-text"><div>first</div><div></div><div>  second</div></code></pre>`, "first\n\n  second"},
		{"existing newlines", "<pre><div>first\n</div><div>second\n</div></pre>", "first\nsecond"},
		{"breaks", `<pre>first<br>  second<br><br>last</pre>`, "first\n  second\n\nlast"},
		{"Python token spans", `<pre class="highlight"><code class="language-python"><span class="comment"># Keep cleanup</span>
<span class="keyword">try</span>:
    use()
finally:
    close()</code></pre>`, "```python\n# Keep cleanup\ntry:\n    use()\nfinally:\n    close()\n```"},
		{"JavaScript inline tokens", `<pre><code class="language-javascript"><span class="comment">// literal markup</span>
const s = <span class="string">"&lt;div&gt;&amp;&lt;/div&gt;"</span>;</code></pre>`, "// literal markup\nconst s = \"<div>&</div>\";"},
		{"JSON block lines", `<pre class="language-json"><div>{</div><div>  "valid": true,</div><div>  "count": 2</div><div>}</div></pre>`, "```json\n{\n  \"valid\": true,\n  \"count\": 2\n}\n```"},
		{"Shell paragraphs", `<pre class="language-sh"><p>set -eu</p><p>printf '%s\n' "$value"</p></pre>`, "```sh\nset -eu\nprintf '%s\\n' \"$value\"\n```"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			md, err := ExtractSelector(tc.html, "pre")
			if err != nil {
				t.Fatal(err)
			}
			assertContainsAll(t, md, tc.want)
			raw, err := extractRaw("", tc.html)
			if err != nil {
				t.Fatal(err)
			}
			assertContainsAll(t, raw.Markdown, tc.want)
			article := `<article><h1>Reference</h1><p>` + strings.Repeat("This article explains the example and its behavior. ", 20) + `</p>` + tc.html +
				`<div class="comment">READERCOMMENT should still be removed.</div></article>`
			full, err := New().Extract("https://guide.example.test/reference", article)
			if err != nil {
				t.Fatal(err)
			}
			assertContainsAll(t, full.Markdown, tc.want)
			// A block that is chrome only by its name goes in ModeClean; the
			// default keeps what structure does not condemn.
			clean, err := NewWithMode(ModeClean).Extract("https://guide.example.test/reference", article)
			if err != nil {
				t.Fatal(err)
			}
			assertContainsAll(t, clean.Markdown, tc.want)
			assertContainsNone(t, clean.Markdown, "READERCOMMENT")
		})
	}
}

func TestInlineCodeVisibility(t *testing.T) {
	t.Parallel()
	cases := []struct {
		style  string
		hidden bool
	}{
		{"display: none", true},
		{"color: red; DISPLAY : NONE ! important", true},
		{"visibility: hidden", true},
		{"visibility: collapse", true},
		{"--display: none", false},
		{"--example: 'display:none'; color: red", false},
		{"display: none; display: inline", false},
		{"visibility:hidden; visibility:visible", false},
		{"display:none!important; display:inline", true},
		{"display:none!important; display:inline!important", false},
		{"display:none!invalid", false},
	}
	for _, tc := range cases {
		t.Run(tc.style, func(t *testing.T) {
			t.Parallel()
			if got := inlineCodeHidden(tc.style); got != tc.hidden {
				t.Fatalf("hidden=%v, want %v", got, tc.hidden)
			}
		})
	}
}

func TestCodeNormalizationIsIdempotent(t *testing.T) {
	t.Parallel()
	input := `<pre class="language-json"><div>{</div><div>  "x": "&lt;&gt;"</div><div>}</div></pre>`
	once := normalizeCodeBlocks(input)
	if twice := normalizeCodeBlocks(once); once != twice {
		t.Fatalf("normalizing fallback output twice changes code:\n%s\n%s", once, twice)
	}
}

func TestCodeNormalizationDoesNotExposeHiddenControls(t *testing.T) {
	t.Parallel()
	html := `<pre><code>visible<span hidden>HIDDEN</span><span aria-hidden="true">LINE NUMBER</span><span style="display: none">DISPLAY NONE</span><span style="visibility: hidden">INVISIBLE</span><button>COPY</button><script>SCRIPT</script><template>TEMPLATE</template></code></pre>`
	md, err := ExtractSelector(html, "pre")
	if err != nil {
		t.Fatal(err)
	}
	assertContainsAll(t, md, "visible")
	assertContainsNone(t, md, "HIDDEN", "LINE NUMBER", "DISPLAY NONE", "INVISIBLE", "COPY", "SCRIPT", "TEMPLATE")
}
