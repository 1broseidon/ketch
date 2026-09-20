package extract

import (
	"strings"
	"testing"
)

func TestDecodeHTML(t *testing.T) {
	cases := []struct {
		name, contentType, in, want string
	}{
		{"undeclared windows-1252", "", "<html><body><p>\x97 Charles Dickens \x93quoted\x94</p></body></html>", "— Charles Dickens “quoted”"},
		{"declared latin-1", "text/html; charset=iso-8859-1", "<p>caf\xe9</p>", "café"},
		{"meta charset", "", `<html><head><meta charset="windows-1252"></head><body>\x97</body></html>`, "\\x97"},
		{"utf-8 after an ascii kilobyte", "", strings.Repeat("<!-- padding -->", 80) + "<p>— café</p>", "— café"},
		{"utf-8 declared", "text/html; charset=utf-8", "<p>— café</p>", "— café"},
	}
	for _, c := range cases {
		got, err := DecodeHTML(strings.NewReader(c.in), c.contentType)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: want %q in %q", c.name, c.want, got)
		}
	}
}
