package docs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1broseidon/ketch/internal/testutil"
)

// The fixtures under testdata/context7 were recorded from context7.com with
// no key on 2026-11-02: a search for react, useState docs for
// /reactjs/react.dev, and the library_redirected answer for /facebook/react.
// Each carries the anonymous quota headers Context7 sent (ratelimit-limit
// 200, ratelimit-reset 1796083200 = 2026-12-01 00:00 UTC).
var context7FixtureDir = filepath.Join("testdata", "context7")

// authSpy records the Authorization header of every request it passes on.
type authSpy struct {
	next http.RoundTripper

	mu    sync.Mutex
	auths []string
	sent  []bool
	urls  []string
}

func (s *authSpy) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	_, sent := req.Header["Authorization"]
	s.auths = append(s.auths, req.Header.Get("Authorization"))
	s.sent = append(s.sent, sent)
	s.urls = append(s.urls, req.URL.String())
	s.mu.Unlock()
	return s.next.RoundTrip(req)
}

func (s *authSpy) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.urls)
}

// recordedContext7 is a Context7 client with apiKey over the recorded
// fixtures (plus any extra ones the test adds to the returned transport),
// with a quota tracker of its own.
func recordedContext7(t *testing.T, apiKey string) (*Context7, *testutil.FixtureTransport, *authSpy) {
	t.Helper()
	ft := testutil.LoadFixtures(t, context7FixtureDir)
	spy := &authSpy{next: ft}
	return &Context7{apiKey: apiKey, client: &http.Client{Transport: spy}, quota: &context7QuotaTracker{}}, ft, spy
}

// With no key Context7 answers from its anonymous quota: no Authorization
// header is sent at all, the results are Context7's as before, and the
// quota the responses report is remembered.
func TestContext7KeylessRecorded(t *testing.T) {
	c, _, spy := recordedContext7(t, "")
	ctx := context.Background()

	libs, err := c.ResolveLibrary(ctx, "react", 3)
	if err != nil {
		t.Fatalf("keyless resolve: %v", err)
	}
	if len(libs) != 3 || libs[0].ID != "/reactjs/react.dev" || libs[1].ID != "/react/react" {
		t.Fatalf("keyless resolve = %+v, want the recorded top three", libs)
	}

	results, err := c.GetDocs(ctx, "/reactjs/react.dev", "useState", 300)
	if err != nil {
		t.Fatalf("keyless docs: %v", err)
	}
	if len(results) == 0 || results[0].Title != "useState(initialState)" || results[0].Source != "context7" || results[0].Library != "/reactjs/react.dev" {
		t.Fatalf("keyless docs = %+v", results)
	}

	assertNoAuthorization(t, spy)
	q := c.quota.last
	if !q.known || q.remaining != 189 || q.limit != 200 || !q.reset.Equal(time.Unix(1796083200, 0)) {
		t.Errorf("quota after the calls = %+v, want 189 of 200 resetting 2026-12-01", q)
	}
}

func assertNoAuthorization(t *testing.T, spy *authSpy) {
	t.Helper()
	for i, sent := range spy.sent {
		if sent {
			t.Errorf("request %s sent Authorization %q with no key", spy.urls[i], spy.auths[i])
		}
	}
}

// With a key nothing changes: every request carries it as a bearer token.
func TestContext7KeyedSendsBearer(t *testing.T) {
	c, _, spy := recordedContext7(t, "test-key")
	if _, err := c.ResolveLibrary(context.Background(), "react", 1); err != nil {
		t.Fatal(err)
	}
	if spy.count() != 1 || spy.auths[0] != "Bearer test-key" {
		t.Fatalf("keyed request Authorization = %q", spy.auths)
	}
}

// A 429 is a *RateLimitError naming the quota, the reset time and the key
// fix, on resolve, docs and the bare search that wraps them.
func TestContext7RateLimited(t *testing.T) {
	for _, tc := range []struct {
		name   string
		key    string
		status int
		header map[string]string
		want   []string
	}{
		{"429 keyless", "", http.StatusTooManyRequests,
			map[string]string{"ratelimit-limit": "200", "ratelimit-remaining": "0", "ratelimit-reset": "1796083200"},
			[]string{"context7: rate limited", "keyless quota of 200 requests", "resets 2026-12-01 00:00 UTC", "ketch config set context7_api_key <key>"}},
		{"429 without headers", "", http.StatusTooManyRequests, nil,
			[]string{"keyless quota is used up", "ketch config set context7_api_key <key>"}},
		{"error status with remaining 0", "", http.StatusForbidden,
			map[string]string{"ratelimit-limit": "200", "ratelimit-remaining": "0", "ratelimit-reset": "1796083200"},
			[]string{"resets 2026-12-01 00:00 UTC"}},
		{"429 keyed", "k", http.StatusTooManyRequests,
			map[string]string{"ratelimit-limit": "1000", "ratelimit-remaining": "0", "ratelimit-reset": "1796083200"},
			[]string{"this key's quota of 1000 requests", "ketch config set context7_api_key <key>"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestContext7(t, func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
			})
			c.apiKey = tc.key
			ctx := context.Background()
			_, resolveErr := c.ResolveLibrary(ctx, "react", 1)
			_, docsErr := c.GetDocs(ctx, "/reactjs/react.dev", "hooks", 4000)
			_, searchErr := c.Search(ctx, "react hooks", 5)
			for op, err := range map[string]error{"resolve": resolveErr, "docs": docsErr, "search": searchErr} {
				var rl *RateLimitError
				if !errors.As(err, &rl) {
					t.Fatalf("%s: %v, want a *RateLimitError", op, err)
				}
				if errors.Is(err, ErrNotFound) || errors.Is(err, ErrLibraryRequired) {
					t.Errorf("%s: a rate limit must classify as upstream, got %v", op, err)
				}
				for _, want := range tc.want {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("%s: %q does not say %q", op, err, want)
					}
				}
			}
		})
	}
}

// A response that spends the quota answers, and the next request is
// refused without being sent until the quota resets.
func TestContext7SpentQuotaNotAskedAgain(t *testing.T) {
	reset := time.Now().Add(time.Hour)
	var calls int
	c := newTestContext7(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("ratelimit-limit", "200")
		w.Header().Set("ratelimit-remaining", "0")
		w.Header().Set("ratelimit-reset", strconv.FormatInt(reset.Unix(), 10))
		_, _ = w.Write([]byte(`{"results":[{"id":"/a/b","title":"B"}]}`))
	})
	c.quota = &context7QuotaTracker{}
	if libs, err := c.ResolveLibrary(context.Background(), "b", 1); err != nil || len(libs) != 1 {
		t.Fatalf("the request that spends the quota still answers: %v %v", libs, err)
	}
	_, err := c.ResolveLibrary(context.Background(), "b", 1)
	var rl *RateLimitError
	if !errors.As(err, &rl) || calls != 1 {
		t.Fatalf("after a spent quota: err %v, %d requests; want a *RateLimitError and no second request", err, calls)
	}

	// Once the reset has passed it asks again.
	c.quota.last.reset = time.Now().Add(-time.Minute)
	if _, err := c.ResolveLibrary(context.Background(), "b", 1); err != nil || calls != 2 {
		t.Fatalf("after the reset: err %v, %d requests; want a second request", err, calls)
	}
}

// Clients built for the same key share one tracker; another key, or none,
// has its own.
func TestContext7QuotaSharedPerKey(t *testing.T) {
	a, b := NewContext7("shared-key-test"), NewContext7("shared-key-test")
	if a.quota != b.quota {
		t.Error("two clients for one key track the quota apart")
	}
	if NewContext7("").quota == a.quota {
		t.Error("keyless and keyed share a quota")
	}
}

func TestParseContext7Quota(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		h     map[string]string
		known bool
		left  int
		reset time.Time
	}{
		{"unix reset", map[string]string{"ratelimit-remaining": "5", "ratelimit-reset": "1796083200"}, true, 5, time.Unix(1796083200, 0)},
		{"delta reset", map[string]string{"ratelimit-remaining": "0", "ratelimit-reset": "60"}, true, 0, now.Add(time.Minute)},
		{"retry-after", map[string]string{"Retry-After": "30"}, false, 0, now.Add(30 * time.Second)},
		{"none", nil, false, 0, time.Time{}},
		{"garbage", map[string]string{"ratelimit-remaining": "lots", "ratelimit-reset": "soon"}, false, 0, time.Time{}},
	} {
		h := http.Header{}
		for k, v := range tc.h {
			h.Set(k, v)
		}
		q := parseContext7Quota(h, now)
		if q.known != tc.known || q.remaining != tc.left || !q.reset.Equal(tc.reset) {
			t.Errorf("%s: %+v, want known %v remaining %d reset %v", tc.name, q, tc.known, tc.left, tc.reset)
		}
	}
}

// /facebook/react moved to /react/react: Context7 answers 301 with the new
// ID in JSON and no Location header. GetDocs follows it once and labels
// the results with the ID it moved to.
func TestContext7LibraryRedirectedFollowedOnce(t *testing.T) {
	c, ft, spy := recordedContext7(t, "")
	ft.Handle(http.MethodGet, "https://context7.com/api/v2/context?libraryId=/react/react&query=useState&type=json&tokens=200", testutil.Fixture{
		Body: `{"codeSnippets":[{"codeTitle":"useState","codeId":"https://react.dev/reference/react/useState","codeList":[{"code":"const [s, setS] = useState(0)"}]}],"infoSnippets":[]}`,
	})
	results, err := c.GetDocs(context.Background(), "/facebook/react", "useState", 200)
	if err != nil {
		t.Fatalf("redirected library: %v", err)
	}
	if len(results) != 1 || results[0].Library != "/react/react" {
		t.Fatalf("results = %+v, want one labelled /react/react", results)
	}
	if spy.count() != 2 {
		t.Errorf("%d requests, want the redirect and its target: %v", spy.count(), spy.urls)
	}
}

// A redirect to a redirect is an error, not a loop.
func TestContext7LibraryRedirectedTwiceFails(t *testing.T) {
	var calls int
	c := newTestContext7(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMovedPermanently)
		fmt.Fprintf(w, `{"error":"library_redirected","redirectUrl":"/hop/%d"}`, calls)
	})
	_, err := c.GetDocs(context.Background(), "/a/b", "q", 100)
	if err == nil || !strings.Contains(err.Error(), "redirected again") || calls != 2 {
		t.Fatalf("double redirect: %v after %d requests", err, calls)
	}
	_, err = c.ResolveLibrary(context.Background(), "/a/b", 1)
	if err == nil || !strings.Contains(err.Error(), "redirected again") {
		t.Fatalf("double redirect on resolve: %v", err)
	}
}

// A redirect on the search endpoint is followed with the new ID as the
// query.
func TestContext7ResolveFollowsLibraryRedirected(t *testing.T) {
	c := newTestContext7(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("query") == "/facebook/react" {
			w.WriteHeader(http.StatusMovedPermanently)
			_, _ = w.Write([]byte(`{"error":"library_redirected","redirectUrl":"/react/react"}`))
			return
		}
		fmt.Fprintf(w, `{"results":[{"id":%q,"title":"React"}]}`, r.URL.Query().Get("query"))
	})
	libs, err := c.ResolveLibrary(context.Background(), "/facebook/react", 1)
	if err != nil || len(libs) != 1 || libs[0].ID != "/react/react" {
		t.Fatalf("resolve through a redirect = %+v, %v", libs, err)
	}
}

// A 3xx that is not library_redirected keeps the old status error.
func TestContext7OtherRedirectIsStatusError(t *testing.T) {
	c := newTestContext7(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	})
	_, err := c.ResolveLibrary(context.Background(), "react", 1)
	if err == nil || err.Error() != "context7 resolve returned status 304" {
		t.Fatalf("304 = %v", err)
	}
}
