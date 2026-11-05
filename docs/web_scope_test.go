package docs

import (
	"strings"
	"testing"
)

// A root naming a file is scoped to the directory holding it, like one
// naming llms.txt; a segment with a version-like dot stays a directory.
func TestWebRootNamingAFileUsesItsDirectory(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://docs.example.com/guide/index.html", "https://docs.example.com/guide/"},
		{"https://docs.example.com/guide/intro.htm", "https://docs.example.com/guide/"},
		{"https://x.test/manual/install.md", "https://x.test/manual/"},
		{"https://x.test/index.php", "https://x.test/"},
		{"https://x.test/docs/INDEX.HTML", "https://x.test/docs/"},
		{"https://x.test/docs/2.x", "https://x.test/docs/2.x/"},
		{"https://x.test/docs/v3.1", "https://x.test/docs/v3.1/"},
		{"https://x.test/docs/%2e%2e/admin/", "https://x.test/admin/"},
	} {
		root, err := parseWebRoot(tc.in)
		if err != nil || root.String() != tc.want || root.prefix != strings.TrimPrefix(tc.want, "https://"+root.u.Host) {
			t.Errorf("parseWebRoot(%q) = %q (prefix %q), %v; want %q", tc.in, root.String(), root.prefix, err, tc.want)
		}
	}
}
