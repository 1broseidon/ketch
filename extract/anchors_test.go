package extract

import (
	"reflect"
	"testing"
)

func TestAnchorsReadHeadingTargetsAndText(t *testing.T) {
	page := `<html><body>
<nav id="sidebar"><a name="top"></a></nav>
<h1 id="field-attributes"><a name="field-attributes" class="plugin-anchor" href="#field-attributes"></a>Field attributes</h1>
<h5 id="rename"><a name="rename" href="#rename"></a><code>#[serde(rename = "name")]</code></h5>
<h3 id="token-string-string"><span class="badge">required</span> token: <code>string</code> | <code>string[]</code> <a class="header-anchor" href="#token-string-string" aria-label="Permalink to token">&#8203;</a></h3>
<section id="framing-out-a-block"><span id="label-target"></span><h3>Framing out a begin / commit block<a class="headerlink" href="#framing-out-a-block" title="Link to this heading">¶</a></h3></section>
<div id="wrapper"><h2>Opens its parent</h2></div>
<h2><a id="old-style"></a>Old style</h2>
<h4 class="callout">Pitfall</h4>
<dl><dt id="pkg.field_validator">pkg.<span>field_validator</span>(*fields)<a class="headerlink" href="#pkg.field_validator">¶</a></dt></dl>
</body></html>`
	got := Anchors(page)
	want := []Anchor{
		{ID: "field-attributes", Text: "Field attributes"},
		{ID: "rename", Text: `#[serde(rename = "name")]`},
		{ID: "token-string-string", Text: "required token: string | string[]"},
		{ID: "framing-out-a-block", Text: "Framing out a begin / commit block"},
		{ID: "wrapper", Text: "Opens its parent"},
		{ID: "old-style", Text: "Old style"},
		{ID: "pkg.field_validator", Text: "pkg.field_validator(*fields)"},
		{ID: "sidebar"}, {ID: "top"}, {ID: "label-target"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Anchors =\n%q\nwant\n%q", got, want)
	}
	if got := Anchors("<p>no targets</p>"); got == nil || len(got) != 0 {
		t.Errorf("a page with no targets = %#v, want empty and non-nil", got)
	}
}
