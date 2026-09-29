package cmd_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/scrape"
)

func TestScrapeBatchAllInvalidURLsExitValidation(t *testing.T) {
	isolated(t)
	code, out, stderr := cli(t, "scrape", "--raw", "--force-browser", "--no-cache", "--json",
		`["file:///ketch-invalid-url-test.html","data:text/html,hello"]`)
	if code != 2 || out != "" || !strings.Contains(stderr, "invalid URL") {
		t.Fatalf("exit=%d stdout=%q stderr=%q, want exit 2 and invalid URL error", code, out, stderr)
	}
}

func TestScrapeBatchKeepsSuccessWhenAnotherURLIsInvalid(t *testing.T) {
	isolated(t)
	server := pageServer(t)
	t.Cleanup(server.Close)
	goodURL := server.URL + "/good"
	input, err := json.Marshal([]string{goodURL, "file:///ketch-invalid-url-test.html"})
	if err != nil {
		t.Fatal(err)
	}

	code, out, stderr := cli(t, "scrape", "--no-cache", "--no-llms-txt", "--json", string(input))
	if code != 0 || !strings.Contains(stderr, "invalid URL") {
		t.Fatalf("exit=%d stderr=%q, want exit 0 with invalid URL warning", code, stderr)
	}
	var pages []scrape.Page
	if err := json.Unmarshal([]byte(out), &pages); err != nil {
		t.Fatalf("decode scrape output %q: %v", out, err)
	}
	if len(pages) != 1 || pages[0].URL != goodURL {
		t.Fatalf("pages=%+v, want only successful page %q", pages, goodURL)
	}
}
