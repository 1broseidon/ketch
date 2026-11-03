package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1broseidon/ketch/httpx"
)

// FixtureTransport is an http.RoundTripper that answers from canned
// responses and never reaches the network: a request with no fixture fails
// with an error naming its method and URL. It lets tests serve package
// registries, GitHub and docs sites under their real hostnames.
//
// Fixtures are keyed by method and URL, compared after normalizing what
// servers treat as equal: scheme and host case, percent-encoding of the
// path, and the order and encoding of query parameters. The fragment is
// never sent, so it is ignored.
//
// Every request is recorded, matched or not (Calls, Unmatched). Safe for
// concurrent use.
type FixtureTransport struct {
	mu       sync.Mutex
	fixtures map[string]Fixture
	calls    []FixtureCall
}

// Fixture is one canned response.
type Fixture struct {
	Status int // 0 means 200
	// Header is sent as is, after templating. Without a Content-Type a
	// non-empty body gets one from the URL's extension, else from its
	// content (JSON is application/json).
	Header http.Header
	Body   string
	// Err, when set, is returned instead of a response, like a network
	// failure.
	Err error
	// Delay holds the response back, ending early with the request
	// context's error when it is cancelled first.
	Delay time.Duration
}

// FixtureCall is one request the transport saw.
type FixtureCall struct {
	Method  string
	URL     string      // as sent
	Header  http.Header // a copy of the request's headers
	Matched bool
}

func (c FixtureCall) String() string { return c.Method + " " + c.URL }

// FixtureOrigin is the placeholder a fixture's body and header values may
// use for the origin it is served from (scheme://host): a page linking to
// itself absolutely, or a redirect's Location.
const FixtureOrigin = "{{ORIGIN}}"

// Fixture directory conventions; see LoadDir.
const (
	fixtureIndex    = "__index__"
	fixtureMetaExt  = ".meta.json"
	fixtureQuerySep = "%3F"
)

// NewFixtureTransport returns a transport with no fixtures.
func NewFixtureTransport() *FixtureTransport {
	return &FixtureTransport{fixtures: map[string]Fixture{}}
}

// LoadFixtures returns a transport serving the fixtures under dir (see
// LoadDir), failing t if dir cannot be loaded.
func LoadFixtures(t testing.TB, dir string) *FixtureTransport {
	t.Helper()
	ft := NewFixtureTransport()
	if err := ft.LoadDir(dir); err != nil {
		t.Fatal(err)
	}
	return ft
}

// Client returns an *http.Client over the transport.
func (ft *FixtureTransport) Client() *http.Client { return &http.Client{Transport: ft} }

// Handle adds a fixture for method and rawURL, replacing any earlier one.
// It panics on a URL it cannot parse: a mistake in the test.
func (ft *FixtureTransport) Handle(method, rawURL string, f Fixture) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		panic(fmt.Sprintf("testutil: fixture URL %q is not absolute", rawURL))
	}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.fixtures[fixtureKey(method, u)] = f
}

// LoadDir adds a GET fixture over https for every file under dir, laid out
// as <host>/<path>:
//
//   - A URL path ending in "/" (including the site root) is the file
//     __index__ in that directory: example.com/docs/__index__ serves
//     https://example.com/docs/.
//   - A query follows the file name after %3F (upper case), written as
//     it would be in the URL:
//     api.github.com/search/repositories%3Fq=solid&sort=stars.
//   - Characters Windows forbids in file names (: * ? " < > | \) are
//     written percent-encoded: in:name is in%3Aname.
//   - <file>.meta.json, beside it or alone, sets the response's status,
//     headers and redirect target, for example
//     {"status": 302, "location": "{{ORIGIN}}/en/6.1/"}; a location with
//     no status is a 302. Without a meta file the response is a 200.
//   - Files whose names start with "." are ignored.
//
// Bodies and header values may use the FixtureOrigin placeholder.
func (ft *FixtureTransport) LoadDir(dir string) error {
	bodies := map[string][]byte{}
	metas := map[string]fixtureMeta{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if name, ok := strings.CutSuffix(rel, fixtureMetaExt); ok {
			m, err := parseFixtureMeta(data)
			if err != nil {
				return fmt.Errorf("testutil: fixture %s: %w", rel, err)
			}
			metas[name] = m
			return nil
		}
		bodies[rel] = data
		return nil
	})
	if err != nil {
		return err
	}
	for rel := range metas {
		if _, ok := bodies[rel]; !ok {
			bodies[rel] = nil
		}
	}
	for rel, body := range bodies {
		u, err := fixtureURL(rel)
		if err != nil {
			return err
		}
		ft.Handle(http.MethodGet, u, metas[rel].fixture(body))
	}
	return nil
}

// fixtureMeta is a .meta.json file.
type fixtureMeta struct {
	Status   int               `json:"status"`
	Headers  map[string]string `json:"headers"`
	Location string            `json:"location"`
}

func parseFixtureMeta(data []byte) (fixtureMeta, error) {
	var m fixtureMeta
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	err := dec.Decode(&m)
	return m, err
}

func (m fixtureMeta) fixture(body []byte) Fixture {
	f := Fixture{Status: m.Status, Header: http.Header{}, Body: string(body)}
	for k, v := range m.Headers {
		f.Header.Set(k, v)
	}
	if m.Location != "" {
		f.Header.Set("Location", m.Location)
		if f.Status == 0 {
			f.Status = http.StatusFound
		}
	}
	return f
}

// fixtureURL maps a fixture file's slash-separated path below the fixture
// directory to the https URL it serves.
func fixtureURL(rel string) (string, error) {
	host, p, ok := strings.Cut(rel, "/")
	if !ok || host == "" || p == "" {
		return "", fmt.Errorf("testutil: fixture %s: want <host>/<path>", rel)
	}
	dir, file := path.Split(p)
	query := ""
	if i := strings.Index(file, fixtureQuerySep); i >= 0 {
		file, query = file[:i], file[i+len(fixtureQuerySep):]
	}
	if file == fixtureIndex {
		file = ""
	}
	raw := "https://" + host + "/" + dir + file
	if query != "" {
		raw += "?" + query
	}
	if _, err := url.Parse(raw); err != nil {
		return "", fmt.Errorf("testutil: fixture %s: %w", rel, err)
	}
	return raw, nil
}

// fixtureKey is the normalized method and URL fixtures are matched by.
func fixtureKey(method string, u *url.URL) string {
	p := (&url.URL{Path: u.Path}).EscapedPath()
	if p == "" {
		p = "/"
	}
	key := strings.ToUpper(method) + " " + strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + p
	q := u.RawQuery
	if v, err := url.ParseQuery(q); err == nil {
		q = v.Encode()
	}
	if q != "" {
		key += "?" + q
	}
	return key
}

// RoundTrip answers req from its fixture, or fails naming the request.
func (ft *FixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
	ft.mu.Lock()
	f, ok := ft.fixtures[fixtureKey(req.Method, req.URL)]
	ft.calls = append(ft.calls, FixtureCall{Method: req.Method, URL: req.URL.String(), Header: req.Header.Clone(), Matched: ok})
	ft.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("testutil: no fixture for %s %s", req.Method, req.URL)
	}
	if err := waitFixture(req, f.Delay); err != nil {
		return nil, err
	}
	if f.Err != nil {
		return nil, f.Err
	}
	return f.response(req), nil
}

// waitFixture holds a response back for d, returning early with the
// request context's error.
func waitFixture(req *http.Request, d time.Duration) error {
	if d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-req.Context().Done():
		}
	}
	return req.Context().Err()
}

func (f Fixture) response(req *http.Request) *http.Response {
	origin := req.URL.Scheme + "://" + req.URL.Host
	body := strings.ReplaceAll(f.Body, FixtureOrigin, origin)
	header := http.Header{}
	for k, values := range f.Header {
		for _, v := range values {
			header.Add(k, strings.ReplaceAll(v, FixtureOrigin, origin))
		}
	}
	if header.Get("Content-Type") == "" && body != "" {
		header.Set("Content-Type", fixtureContentType(req.URL.Path, body))
	}
	status := f.Status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}

// fixtureContentTypes are the default content types by extension: a fixed
// table, so defaults do not depend on the machine's MIME database.
var fixtureContentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".htm":  "text/html; charset=utf-8",
	".json": "application/json",
	".js":   "text/javascript; charset=utf-8",
	".xml":  "application/xml",
	".txt":  "text/plain; charset=utf-8",
	".md":   "text/markdown; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".gz":   "application/gzip",
	".inv":  "application/octet-stream",
}

func fixtureContentType(urlPath, body string) string {
	if ct, ok := fixtureContentTypes[strings.ToLower(path.Ext(urlPath))]; ok {
		return ct
	}
	if trimmed := strings.TrimSpace(body); (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && json.Valid([]byte(trimmed)) {
		return "application/json"
	}
	return http.DetectContentType([]byte(body))
}

// Calls returns every request seen so far, in order.
func (ft *FixtureTransport) Calls() []FixtureCall {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	out := make([]FixtureCall, len(ft.calls))
	copy(out, ft.calls)
	return out
}

// Unmatched returns the requests that had no fixture, as "METHOD URL".
func (ft *FixtureTransport) Unmatched() []string {
	var out []string
	for _, c := range ft.Calls() {
		if !c.Matched {
			out = append(out, c.String())
		}
	}
	return out
}

// URLs returns the fixture keys, sorted, for a test's failure message.
func (ft *FixtureTransport) URLs() []string {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	out := make([]string, 0, len(ft.fixtures))
	for k := range ft.fixtures {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SwapDefaultTransport points httpx.Default's shared client at rt until the
// test ends. Everything holding that client (the scraper, Context7, the
// search, code and docs API clients) then answers from rt; a client that
// copies it at construction (the docs fallback fetcher) must be built
// after the swap. Tests that swap must not run in parallel.
func SwapDefaultTransport(t testing.TB, rt http.RoundTripper) {
	t.Helper()
	client := httpx.Default()
	prev := client.Transport
	client.Transport = rt
	t.Cleanup(func() { client.Transport = prev })
}
