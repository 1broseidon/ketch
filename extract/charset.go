package extract

import (
	"io"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
)

// DecodeHTML reads an HTML document and returns it as UTF-8. The charset
// comes from the Content-Type header, then the document's own byte-order
// mark or <meta charset>, then the bytes themselves; a page that declares
// nothing and is not valid UTF-8 is read as windows-1252, as browsers read
// it. Paul Graham's essays are windows-1252 with no declaration: without
// this their em dashes and curly quotes arrive as U+FFFD.
func DecodeHTML(r io.Reader, contentType string) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return DecodeHTMLBytes(b, contentType), nil
}

// DecodeHTMLBytes is DecodeHTML for a document already in memory. An empty
// contentType leaves the charset to the document and the bytes.
func DecodeHTMLBytes(b []byte, contentType string) string {
	enc, name, certain := charset.DetermineEncoding(b, contentType)
	// The sniff looks at the first kilobyte; a document that is valid
	// UTF-8 throughout is UTF-8 whatever the first kilobyte suggests.
	if name == "utf-8" || (!certain && utf8.Valid(b)) {
		return string(b)
	}
	out, err := enc.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

// ensureUTF8 returns html as UTF-8. A caller that read the bytes itself —
// piped input, a test — may hand over a legacy-encoded page; the fetch
// path decodes with the response's Content-Type before this is reached,
// and a document that is already valid UTF-8 is returned unchanged.
func ensureUTF8(html string) string {
	if utf8.ValidString(html) {
		return html
	}
	return DecodeHTMLBytes([]byte(html), "")
}
