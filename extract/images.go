package extract

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// lazySources are the attributes lazy-loading scripts read an image's real
// source from, in the order they are trusted; the srcset forms hold
// candidates, of which the first is taken.
var lazySources = []string{"data-src", "data-lazy-src", "data-original", "data-lazy", "data-srcset", "data-lazy-srcset", "srcset"}

// rxPlaceholderImage matches the sources a page shows until a script swaps
// the real one in: a spacer, a blank, a pixel.
var rxPlaceholderImage = regexp.MustCompile(`(?i)^about:blank$|(^|/)(blank|spacer|placeholder|transparent|pixel|lazy|loading)[^/]*\.(gif|png|svg|webp|jpe?g)(\?|$)|/1x1[./]`)

// fixLazyImages gives lazily loaded images their source. The real image
// sits in a data- attribute until a script swaps it in, and an agent
// reading the page runs no script; without this the image, and its alt
// text, are lost. Readability did the same.
func fixLazyImages(doc *goquery.Document) {
	doc.Find("img").Each(func(_ int, img *goquery.Selection) {
		if src, _ := img.Attr("src"); src != "" && !rxPlaceholderImage.MatchString(src) {
			return
		}
		if real := lazySource(img); real != "" {
			img.SetAttr("src", real)
		}
	})
}

// lazySource returns the real source a lazily loaded image carries, or "".
func lazySource(img *goquery.Selection) string {
	for _, attr := range lazySources {
		v, ok := img.Attr(attr)
		if !ok {
			continue
		}
		if strings.HasSuffix(attr, "srcset") {
			v = firstSrcsetCandidate(v)
		}
		if v = strings.TrimSpace(v); v != "" && !strings.HasPrefix(strings.ToLower(v), "data:") {
			return v
		}
	}
	return ""
}

// firstSrcsetCandidate returns the URL of a srcset's first candidate.
func firstSrcsetCandidate(srcset string) string {
	first, _, _ := strings.Cut(srcset, ",")
	if f := strings.Fields(first); len(f) > 0 {
		return f[0]
	}
	return ""
}
