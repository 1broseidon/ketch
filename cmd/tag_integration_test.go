package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/internal/testutil"
	ketchmcp "github.com/1broseidon/ketch/mcp"
	"github.com/1broseidon/ketch/scrape"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ketch-tag-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "ketch")
	build := exec.Command("go", "build", "-o", binary, "..")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build fixture binary: %v\n%s", err, out)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

const fixtureHTML = `<html><head><title>Project authentication</title></head><body><article><h1>Authentication</h1><p>This documentation explains how to configure authentication for a project, including login sessions and application settings.</p><p>Keep this useful second paragraph so the page is clearly real documentation with enough content to extract.</p></article></body></html>`

func isolated(t *testing.T) string {
	t.Helper()
	dir := testutil.SetIsolatedConfigHome(t)
	t.Setenv("KETCH_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("KETCH_NO_UPDATE_NOTIFIER", "1")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func cli(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return code, out.String(), stderr.String()
}

func mustCLI(t *testing.T, args ...string) string {
	t.Helper()
	code, out, stderr := cli(t, args...)
	if code != 0 {
		t.Fatalf("CLI %q exited %d: %s", args, code, stderr)
	}
	return out
}

func pageServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/bad" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "missing")
			return
		}
		fmt.Fprint(w, fixtureHTML)
	}))
	t.Cleanup(s.Close)
	return s
}

func cached(t *testing.T) (*cache.Cache, *cache.BBoltStore) {
	t.Helper()
	s, err := cache.NewBBoltStore(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	c := cache.NewWithStore(s, time.Hour)
	t.Cleanup(c.Close)
	return c, s
}

func examplePage(u string) *scrape.Page {
	return &scrape.Page{URL: u, Title: "Authentication reference", Markdown: "Useful authentication documentation for project integration and session handling."}
}

func session(t *testing.T, configure ...func(*config.Config)) *mcpsdk.ClientSession {
	t.Helper()
	cfg := config.Defaults()
	for _, apply := range configure {
		apply(&cfg)
	}
	srv, err := ketchmcp.NewServer(&cfg, "review")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, serverTransport) }()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "tag-review", Version: "0"}, nil)
	s, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func call(t *testing.T, s *mcpsdk.ClientSession, name string, args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := s.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func output(r *mcpsdk.CallToolResult) string {
	var out []string
	for _, item := range r.Content {
		if v, ok := item.(*mcpsdk.TextContent); ok {
			out = append(out, v.Text)
		}
	}
	return strings.Join(out, "\n")
}

func TestSingleSelectorRecordsTag(t *testing.T) {
	isolated(t)
	srv := pageServer(t)
	mustCLI(t, "scrape", srv.URL+"/doc", "--select", "article", "--tag", "selected", "--json")
	out := mustCLI(t, "tag", "show", "selected", "--json")
	var result struct {
		Entries int `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Entries != 1 {
		t.Fatalf("successful selected scrape recorded %d entries: %s", result.Entries, out)
	}
}

type libraryFixtureTransport struct{}

func (libraryFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "context7.com" || req.URL.Path != "/api/v2/context" || req.URL.Query().Get("libraryId") != "/project/docs" {
		return nil, fmt.Errorf("unexpected docs request: %s", req.URL)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{"infoSnippets":[
			{"pageId":"https://example.test/docs/auth","breadcrumb":"Authentication","content":"Configure project authentication and session handling."},
			{"pageId":"https://example.test/docs/other","breadcrumb":"Other","content":"A result outside the caller's limit."}
		]}`)),
		Request: req,
	}, nil
}

func TestMCPDirectLibraryDocsRecordsReturnedSources(t *testing.T) {
	isolated(t)
	client := httpx.Default()
	previous := client.Transport
	client.Transport = libraryFixtureTransport{}
	t.Cleanup(func() { client.Transport = previous })
	s := session(t, func(cfg *config.Config) { cfg.SetProvider("context7_api_key", "fixture") })
	r := call(t, s, "docs", map[string]any{"query": "authentication", "library": "/project/docs", "limit": 1, "tag": "project"})
	if r.IsError {
		t.Fatal(output(r))
	}
	r = call(t, s, "tag", map[string]any{"operation": "show", "tag": "project"})
	var view cache.TagView
	if err := json.Unmarshal([]byte(output(r)), &view); err != nil {
		t.Fatal(err)
	}
	if r.IsError || view.Entries != 1 || view.Pages[0].URL != "https://example.test/docs/auth" || view.Pages[0].Description == "" {
		t.Fatalf("direct-library docs did not bookmark exactly the returned source: %s", output(r))
	}
}

func TestRemoveURLAfterUserAgentOverride(t *testing.T) {
	isolated(t)
	srv := pageServer(t)
	u := srv.URL + "/doc"
	mustCLI(t, "scrape", u, "--no-llms-txt", "--user-agent", "lab-review/1", "--tag", "project", "--json")
	t.Log("before removal:", mustCLI(t, "tag", "show", "project", "--json"))
	code, out, stderr := cli(t, "tag", "remove", "project", u)
	if code != 0 {
		t.Fatalf("visible URL could not be removed: exit=%d stdout=%q stderr=%q", code, out, stderr)
	}
}

func TestTagAddUsesDurableIndexWithExistingPageCache(t *testing.T) {
	isolated(t)
	srv := pageServer(t)
	u := srv.URL + "/doc"
	mustCLI(t, "scrape", u, "--no-llms-txt", "--json")
	mustCLI(t, "tag", "add", "project", u, "--json")
	var result cache.TagView
	if err := json.Unmarshal([]byte(mustCLI(t, "tag", "show", "project", "--json")), &result); err != nil {
		t.Fatal(err)
	}
	if result.Entries != 1 || result.Cached != 1 || result.Pages[0].Title == "" {
		t.Fatalf("tag add did not use the durable index or cached metadata: %+v", result)
	}
}

func TestFetchSettingsDoNotDuplicateBookmarks(t *testing.T) {
	isolated(t)
	srv := pageServer(t)
	u := srv.URL + "/doc"
	for _, agent := range []string{"first-agent", "second-agent"} {
		mustCLI(t, "scrape", u, "--no-llms-txt", "--user-agent", agent, "--tag", "project", "--json")
	}
	var result cache.TagView
	if err := json.Unmarshal([]byte(mustCLI(t, "tag", "show", "project", "--json")), &result); err != nil {
		t.Fatal(err)
	}
	if result.Entries != 1 || result.Cached != 1 || result.Pages[0].URL != u {
		t.Fatalf("fetch settings changed bookmark membership: %+v", result)
	}
}

func TestColdTagAddPreservesMetadata(t *testing.T) {
	c, _ := cached(t)
	u := "https://example.test/docs"
	if err := c.TagPage("project", u, u, examplePage(u)); err != nil {
		t.Fatal(err)
	}
	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TagURL("project", u, u); err != nil {
		t.Fatal(err)
	}
	p, err := c.Tagged("project")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 1 || p[0].Title == "" || p[0].Description == "" {
		t.Fatalf("re-adding cold URL erased metadata: %+v", p)
	}
}

func TestBackfillSurvivesClearWithoutInterveningShow(t *testing.T) {
	c, _ := cached(t)
	u := "https://example.test/docs"
	if _, err := c.TagURL("project", u, u); err != nil {
		t.Fatal(err)
	}
	c.Put(u, examplePage(u), scrape.SourceHTTP)
	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	p, err := c.Tagged("project")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 1 || p[0].Title == "" {
		t.Fatalf("fetch did not persist metadata before body was cleared: %+v", p)
	}
}

type hookedStore struct {
	*cache.BBoltStore
	afterSnapshot func()
}

func (s *hookedStore) TagEntries(tag string) ([][]byte, error) {
	entries, err := s.BBoltStore.TagEntries(tag)
	if s.afterSnapshot != nil {
		f := s.afterSnapshot
		s.afterSnapshot = nil
		f()
	}
	return entries, err
}

func TestReadBackfillDoesNotResurrectDeletedTag(t *testing.T) {
	_, base := cached(t)
	store := &hookedStore{BBoltStore: base}
	c := cache.NewWithStore(store, time.Hour)
	u := "https://example.test/docs"
	if _, err := c.TagURL("project", u, u); err != nil {
		t.Fatal(err)
	}
	c.Put(u, examplePage(u), scrape.SourceHTTP)
	store.afterSnapshot = func() {
		n, err := c.RemoveTag("project")
		if err != nil || n != 1 {
			t.Fatalf("interleaved deletion removed=%d err=%v", n, err)
		}
	}
	if _, err := c.Tagged("project"); err != nil {
		t.Fatal(err)
	}
	p, err := c.Tagged("project")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 0 {
		t.Fatalf("show resurrected a deleted tag via metadata backfill: %+v", p)
	}
}

func TestMissingRemovalHasSameJSONExitCode(t *testing.T) {
	isolated(t)
	plain, _, _ := cli(t, "tag", "remove", "missing")
	jsonCode, out, _ := cli(t, "tag", "remove", "missing", "--json")
	if plain != 3 || jsonCode != 3 {
		t.Fatalf("plain exit=%d; JSON exit=%d output=%s", plain, jsonCode, out)
	}
}

func TestTagShowDoesNotDependOnCookieFile(t *testing.T) {
	dir := isolated(t)
	mustCLI(t, "tag", "add", "project", "https://example.test/docs")
	cfg, _ := json.Marshal(map[string]string{"cookie_file": filepath.Join(dir, "missing.cookies")})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), cfg, 0600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := cli(t, "tag", "show", "project", "--json")
	if code != 0 {
		t.Fatalf("local index read blocked by unrelated cookie file: exit=%d stderr=%s", code, stderr)
	}
}

func TestMCPRecordsSearchHitsEvenWhenScrapeFails(t *testing.T) {
	isolated(t)
	pages := pageServer(t)
	searchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{
			{"title": "Available", "url": pages.URL + "/good", "content": "useful reference"},
			{"title": "Unavailable", "url": pages.URL + "/bad", "content": "another useful reference"},
		}})
	}))
	defer searchServer.Close()
	s := session(t)
	r := call(t, s, "search", map[string]any{"query": "local fixture", "backend": "searxng", "searxng_url": searchServer.URL, "limit": 2, "scrape": true, "tag": "project"})
	if r.IsError {
		t.Fatal(output(r))
	}
	r = call(t, s, "tag", map[string]any{"operation": "show", "tag": "project"})
	var result struct {
		Entries int `json:"entries"`
	}
	if err := json.Unmarshal([]byte(output(r)), &result); err != nil {
		t.Fatal(err)
	}
	if result.Entries != 2 {
		t.Fatalf("2 returned search hits became %d tag entries: %s", result.Entries, output(r))
	}
}

func TestMCPRejectsInvalidResearchTag(t *testing.T) {
	isolated(t)
	pages := pageServer(t)
	s := session(t)
	r := call(t, s, "scrape", map[string]any{"url": pages.URL + "/good", "no_llms_txt": true, "tag": " padded "})
	if !r.IsError || !strings.Contains(output(r), "[validation]") {
		t.Fatalf("invalid tag silently ignored; isError=%v result=%s", r.IsError, output(r))
	}
}

func TestMCPDoesNotKeepCLITagIndexLockedWhileIdle(t *testing.T) {
	isolated(t)
	_ = session(t)
	code, _, stderr := cli(t, "tag", "add", "project", "https://example.test/docs")
	if code != 0 {
		t.Fatalf("idle MCP session blocks CLI tagging: exit=%d stderr=%s", code, stderr)
	}
}

func TestMCPRecoversAfterStartupCacheLockClears(t *testing.T) {
	isolated(t)
	holder := cache.New(time.Hour)
	if holder == nil {
		t.Fatal("could not open fixture cache")
	}
	s := session(t)
	holder.Close()
	r := call(t, s, "tag", map[string]any{"operation": "add", "tag": "project", "urls": []string{"https://example.test/docs"}})
	if r.IsError {
		t.Fatalf("MCP remains unable to tag after startup lock was released: %s", output(r))
	}
}

func TestTagWriteFailureIsReported(t *testing.T) {
	isolated(t)
	pages := pageServer(t)
	tooLong := strings.Repeat("a", 33000)
	code, _, stderr := cli(t, "scrape", pages.URL+"/doc", "--no-llms-txt", "--tag", tooLong, "--json")
	if code == 0 && !strings.Contains(stderr, "warn") {
		t.Fatalf("bbolt oversized-key write failed with exit 0 and no warning: stderr=%q", stderr)
	}
}

func seedLargeTag(t *testing.T) {
	t.Helper()
	isolated(t)
	args := []string{"tag", "add", "large", "--json"}
	for i := range 61 {
		args = append(args, fmt.Sprintf("https://example.test/%03d", i))
	}
	mustCLI(t, args...)
}

func TestTagLimitsCLI(t *testing.T) {
	seedLargeTag(t)
	for _, tc := range []struct {
		flags []string
		shown int
	}{{nil, 50}, {[]string{"--limit", "0"}, 61}, {[]string{"--limit", "7"}, 7}} {
		args := append([]string{"tag", "show", "large", "--json"}, tc.flags...)
		var view cache.TagView
		if err := json.Unmarshal([]byte(mustCLI(t, args...)), &view); err != nil {
			t.Fatal(err)
		}
		if view.Entries != 61 || view.Shown != tc.shown || len(view.Pages) != tc.shown {
			t.Fatalf("limit %v: entries=%d shown=%d pages=%d", tc.flags, view.Entries, view.Shown, len(view.Pages))
		}
	}
	code, out, stderr := cli(t, "tag", "show", "large", "--minimal", "--limit", "2")
	if code != 0 || strings.Count(out, "\n") != 2 || !strings.Contains(stderr, "showing 2 of 61") {
		t.Fatalf("minimal limit: code=%d stdout=%q stderr=%q", code, out, stderr)
	}
	if code, _, _ := cli(t, "tag", "show", "large", "--limit", "-1"); code != 2 {
		t.Fatalf("negative limit exit=%d", code)
	}
}

func TestTagLimitsMCP(t *testing.T) {
	seedLargeTag(t)
	s := session(t)
	for _, tc := range []struct {
		limit *int
		shown int
	}{{nil, 50}, {new(int), 61}} {
		args := map[string]any{"operation": "show", "tag": "large"}
		if tc.limit != nil {
			args["limit"] = *tc.limit
		}
		r := call(t, s, "tag", args)
		var view cache.TagView
		if err := json.Unmarshal([]byte(output(r)), &view); err != nil {
			t.Fatal(err)
		}
		if r.IsError || view.Entries != 61 || view.Shown != tc.shown {
			t.Fatalf("MCP limit: %s", output(r))
		}
	}
	r := call(t, s, "tag", map[string]any{"operation": "show", "tag": "large", "limit": -1})
	if !r.IsError || !strings.Contains(output(r), "[validation]") {
		t.Fatalf("MCP negative limit: %s", output(r))
	}
}

func TestConcurrentTaggersWithLockedPageCache(t *testing.T) {
	isolated(t)
	holder := cache.New(time.Hour)
	if holder == nil {
		t.Fatal("open fixture cache")
	}
	defer holder.Close()
	results := make(chan error, 4)
	for worker := range 4 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			args := []string{"tag", "add", "shared", "--json", "https://example.test/common"}
			for i := range 3 {
				args = append(args, fmt.Sprintf("https://example.test/%d/%d", worker, i))
			}
			out, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
			if err != nil {
				err = fmt.Errorf("tagger %d: %w: %s", worker, err, out)
			}
			results <- err
		}()
	}
	for range 4 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var view cache.TagView
	if err := json.Unmarshal([]byte(mustCLI(t, "tag", "show", "shared", "--json")), &view); err != nil {
		t.Fatal(err)
	}
	if view.Entries != 13 || view.CacheStatus != "unavailable" {
		t.Fatalf("concurrent writers: %+v", view)
	}
}

func TestWriteWarningsPreserveResearchOutput(t *testing.T) {
	dir := isolated(t)
	if err := os.Mkdir(filepath.Join(dir, "tags.db"), 0700); err != nil {
		t.Fatal(err)
	}
	pages := pageServer(t)
	code, out, stderr := cli(t, "scrape", pages.URL+"/good", "--no-llms-txt", "--tag", "project", "--json")
	var page scrape.Page
	if code != 0 || json.Unmarshal([]byte(out), &page) != nil || page.Markdown == "" {
		t.Fatalf("lost fetched result: code=%d out=%s err=%s", code, out, stderr)
	}
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		var diagnostic map[string]map[string]string
		if err := json.Unmarshal([]byte(line), &diagnostic); err != nil || diagnostic["warning"]["code"] != "tag_write_failed" {
			t.Fatalf("unstructured write diagnostic: %q", line)
		}
	}
	if stderr == "" {
		t.Fatal("missing write diagnostic")
	}
	s := session(t)
	r := call(t, s, "scrape", map[string]any{"url": pages.URL + "/good", "no_llms_txt": true, "tag": "project"})
	var result ketchmcp.ScrapeOutput
	if err := json.Unmarshal([]byte(output(r)), &result); err != nil {
		t.Fatal(err)
	}
	if r.IsError || len(result.Results) != 1 || len(result.Warnings) == 0 {
		t.Fatalf("MCP lost results or warnings: %s", output(r))
	}
}

// assertNoCountKeys fails if any of entries/shown/cached appear in raw JSON
// at all — add/list/remove never set them, so plain `omitempty` must drop
// them rather than emitting a spurious "entries":0 beside the real payload.
func assertNoCountKeys(t *testing.T, op, raw string) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("%s: invalid JSON: %v (%s)", op, err, raw)
	}
	for _, key := range []string{"entries", "shown", "cached"} {
		if _, ok := m[key]; ok {
			t.Errorf("%s output carries %q, want it absent: %s", op, key, raw)
		}
	}
}

// assertCountKeys fails unless every key in want is present with exactly
// that value — used for show, where the counts must appear even when the
// true answer is zero (an empty tag).
func assertCountKeys(t *testing.T, op, raw string, want map[string]float64) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("%s: invalid JSON: %v (%s)", op, err, raw)
	}
	for key, wantVal := range want {
		field, ok := m[key]
		if !ok {
			t.Errorf("%s output missing %q, want %v: %s", op, key, wantVal, raw)
			continue
		}
		var got float64
		if err := json.Unmarshal(field, &got); err != nil {
			t.Errorf("%s: %q is not a number: %v", op, key, err)
			continue
		}
		if got != wantVal {
			t.Errorf("%s: %q = %v, want %v", op, key, got, wantVal)
		}
	}
}

// TestMCPTagOutputShapeOmitsCountsExceptOnShow guards against the "entries":
// 0 next to a real add/list/remove payload bug: those operations never
// populate entries/shown/cached, so the fields must be absent, not zero.
// show must carry them even when the true answer is genuinely zero.
func TestMCPTagOutputShapeOmitsCountsExceptOnShow(t *testing.T) {
	isolated(t)
	s := session(t)

	r := call(t, s, "tag", map[string]any{"operation": "add", "tag": "shape", "urls": []string{"https://example.test/a"}})
	if r.IsError {
		t.Fatal(output(r))
	}
	assertNoCountKeys(t, "add", output(r))

	r = call(t, s, "tag", map[string]any{"operation": "list"})
	if r.IsError {
		t.Fatal(output(r))
	}
	assertNoCountKeys(t, "list", output(r))

	r = call(t, s, "tag", map[string]any{"operation": "show", "tag": "shape"})
	if r.IsError {
		t.Fatal(output(r))
	}
	assertCountKeys(t, "show", output(r), map[string]float64{"entries": 1, "shown": 1, "cached": 0})

	r = call(t, s, "tag", map[string]any{"operation": "show", "tag": "never-used"})
	if r.IsError {
		t.Fatal(output(r))
	}
	assertCountKeys(t, "show(empty)", output(r), map[string]float64{"entries": 0, "shown": 0, "cached": 0})

	r = call(t, s, "tag", map[string]any{"operation": "remove", "tag": "shape", "urls": []string{"https://example.test/a"}})
	if r.IsError {
		t.Fatal(output(r))
	}
	assertNoCountKeys(t, "remove", output(r))
}

// TestTagRemoveReportsMissingURLs covers item 3: a batch remove must report
// which given URLs were not under the tag, on the CLI (--json and the text
// summary) and over MCP, without disturbing the ones actually removed.
func TestTagRemoveReportsMissingURLs(t *testing.T) {
	isolated(t)
	mustCLI(t, "tag", "add", "batch", "https://example.test/kept", "https://example.test/gone")
	out := mustCLI(t, "tag", "remove", "batch", "https://example.test/gone", "https://example.test/never-tagged", "--json")
	var result struct {
		Tag     string   `json:"tag"`
		Removed int      `json:"removed"`
		Missing []string `json:"missing"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Removed != 1 || len(result.Missing) != 1 || result.Missing[0] != "https://example.test/never-tagged" {
		t.Fatalf("CLI --json remove missing report: %+v (%s)", result, out)
	}

	mustCLI(t, "tag", "add", "batch2", "https://example.test/kept2", "https://example.test/gone2")
	code, _, stderr := cli(t, "tag", "remove", "batch2", "https://example.test/gone2", "https://example.test/also-never-tagged")
	if code != 0 || !strings.Contains(stderr, "also-never-tagged") {
		t.Fatalf("CLI text summary did not mention the missing URL: exit=%d stderr=%q", code, stderr)
	}

	mustCLI(t, "tag", "add", "batch3", "https://example.test/kept3", "https://example.test/gone3")
	s := session(t)
	r := call(t, s, "tag", map[string]any{"operation": "remove", "tag": "batch3", "urls": []string{"https://example.test/gone3", "https://example.test/mcp-never-tagged"}})
	if r.IsError {
		t.Fatal(output(r))
	}
	var mcpResult struct {
		Removed int      `json:"removed"`
		Missing []string `json:"missing"`
	}
	if err := json.Unmarshal([]byte(output(r)), &mcpResult); err != nil {
		t.Fatal(err)
	}
	if mcpResult.Removed != 1 || len(mcpResult.Missing) != 1 || mcpResult.Missing[0] != "https://example.test/mcp-never-tagged" {
		t.Fatalf("MCP remove missing report: %+v (%s)", mcpResult, output(r))
	}
}

// TestTagRemoveBatchAllMissingIsNotFound: when none of the given URLs were
// under the tag, that is a not-found outcome (exit 3), same as removing a
// tag that does not exist at all — only a partial match succeeds.
func TestTagRemoveBatchAllMissingIsNotFound(t *testing.T) {
	isolated(t)
	mustCLI(t, "tag", "add", "batch4", "https://example.test/kept4")
	code, _, stderr := cli(t, "tag", "remove", "batch4", "https://example.test/not-in-the-tag")
	if code != 3 {
		t.Fatalf("batch remove with nothing matching: exit=%d stderr=%s", code, stderr)
	}
}

// TestTagAddRejectsNonHTTPURLs covers item 4: tag add must require an
// absolute http(s) URL, both on the CLI (exit 2, not the old exit 5 for an
// empty URL) and over MCP ([validation], not [precondition]). A batch with
// one bad URL must not partially tag the good ones first.
func TestTagAddRejectsNonHTTPURLs(t *testing.T) {
	isolated(t)
	const wantExit = 2 // ExitValidation (cmd/exit.go); this package builds and execs the binary rather than importing cmd
	for _, bad := range []string{"javascript:alert(1)", "ftp://host/x", "plain-word", ""} {
		code, _, stderr := cli(t, "tag", "add", "bad", bad)
		if code != wantExit {
			t.Errorf("tag add %q: exit=%d, want %d (%s)", bad, code, wantExit, stderr)
		}
	}

	// A batch with a bad URL alongside a good one must tag neither.
	code, _, stderr := cli(t, "tag", "add", "batch", "https://example.test/good", "javascript:alert(1)")
	if code != wantExit {
		t.Fatalf("mixed batch: exit=%d, want %d (%s)", code, wantExit, stderr)
	}
	out := mustCLI(t, "tag", "list", "--json")
	if strings.Contains(out, `"batch"`) {
		t.Fatalf("mixed batch partially tagged despite the bad URL: %s", out)
	}

	s := session(t)
	r := call(t, s, "tag", map[string]any{"operation": "add", "tag": "mcp-bad", "urls": []string{"ftp://host/x"}})
	if !r.IsError || !strings.Contains(output(r), "[validation]") {
		t.Fatalf("MCP tag add ftp URL: isError=%v result=%s", r.IsError, output(r))
	}
}

func TestFetchBackfillsWithoutPageCaching(t *testing.T) {
	isolated(t)
	pages := pageServer(t)
	u := pages.URL + "/doc"
	mustCLI(t, "tag", "add", "project", u)
	mustCLI(t, "scrape", u, "--no-cache", "--no-llms-txt", "--json")
	var view cache.TagView
	if err := json.Unmarshal([]byte(mustCLI(t, "tag", "show", "project", "--json")), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Pages) != 1 || view.Pages[0].Title == "" || view.Pages[0].Description == "" || view.Pages[0].Cached {
		t.Fatalf("uncached backfill: %+v", view)
	}
}
