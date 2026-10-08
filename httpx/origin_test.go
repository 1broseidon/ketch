package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const secretHeader = "Cf-Access-Client-Secret"

// recorder returns a server that records the secret header it receives on
// each path, and runs next (if any) after recording.
func recorder(t *testing.T, seen map[string]string, next http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.Host+r.URL.Path] = r.Header.Get(secretHeader)
		if next != nil {
			next(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, c *http.Client, u string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return req
}

func TestWithOriginHeadersScopesHeadersToOrigin(t *testing.T) {
	seen := map[string]string{}
	other := recorder(t, seen, nil)
	instance := recorder(t, seen, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/landed", http.StatusFound)
		case "/cross":
			http.Redirect(w, r, other.URL+"/stolen", http.StatusFound)
		}
	})
	c, err := WithOriginHeaders(instance.Client(), map[string]map[string]string{
		instance.URL: {"CF-Access-Client-Secret": "s3cret"},
	})
	if err != nil {
		t.Fatal(err)
	}

	get(t, c, instance.URL+"/same")
	req := get(t, c, instance.URL+"/cross")
	get(t, c, other.URL+"/direct")

	host := strings.TrimPrefix(instance.URL, "http://")
	otherHost := strings.TrimPrefix(other.URL, "http://")
	want := map[string]string{
		host + "/same":        "s3cret",
		host + "/landed":      "s3cret", // same-origin redirect keeps them
		host + "/cross":       "s3cret",
		otherHost + "/stolen": "", // cross-origin redirect never carries them
		otherHost + "/direct": "",
	}
	for k, v := range want {
		if got, ok := seen[k]; !ok || got != v {
			t.Errorf("%s: secret = %q (seen %v), want %q", k, got, ok, v)
		}
	}
	if len(req.Header) != 0 {
		t.Errorf("caller's request was modified: %v", req.Header)
	}
}

func TestWithOriginHeadersKeepsRequestHeaders(t *testing.T) {
	seen := map[string]string{}
	srv := recorder(t, seen, nil)
	c, err := WithOriginHeaders(srv.Client(), map[string]map[string]string{srv.URL + "/": {secretHeader: "configured"}})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/explicit", nil)
	req.Header.Set(secretHeader, "from-request")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := seen[strings.TrimPrefix(srv.URL, "http://")+"/explicit"]; got != "from-request" {
		t.Fatalf("secret = %q, want the request's own value", got)
	}
}

func TestWithOriginHeadersWithoutHeadersReturnsClient(t *testing.T) {
	base := &http.Client{}
	c, err := WithOriginHeaders(base, nil)
	if err != nil || c != base {
		t.Fatalf("got %p, %v; want the same client", c, err)
	}
}

func TestOriginMatchesDefaultPortsAndCase(t *testing.T) {
	for _, tc := range []struct{ a, b string }{
		{"https://Searx.Example", "https://searx.example:443/search?q=x"},
		{"http://searx.example:80/", "HTTP://searx.example/path"},
		{"http://[::1]:8080", "http://[::1]:8080/x"},
	} {
		byOrigin, err := parseOriginHeaders(map[string]map[string]string{tc.a: {"X-A": "1"}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, tc.b, nil)
		if byOrigin[origin(req.URL)] == nil {
			t.Errorf("%s does not match %s", tc.b, tc.a)
		}
	}
	for _, other := range []string{"http://searx.example", "https://searx.example:8443", "https://evil.example"} {
		byOrigin, _ := parseOriginHeaders(map[string]map[string]string{"https://searx.example": {"X-A": "1"}})
		req := httptest.NewRequest(http.MethodGet, other, nil)
		if byOrigin[origin(req.URL)] != nil {
			t.Errorf("%s matched https://searx.example", other)
		}
	}
}

func TestValidateOriginHeadersNeverEchoesValues(t *testing.T) {
	for name, headers := range map[string]map[string]map[string]string{
		"path":           {"https://searx.example/search": {"X-A": "private-value"}},
		"userinfo":       {"https://user:private-value@searx.example": {"X-A": "v"}},
		"bad userinfo":   {"https://user:private-value@sea rx.example": {"X-A": "v"}},
		"not http":       {"ftp://searx.example": {"X-A": "private-value"}},
		"relative":       {"searx.example": {"X-A": "private-value"}},
		"duplicate":      {"https://searx.example": {"X-A": "1"}, "https://SEARX.example:443": {"X-A": "private-value"}},
		"bad name":       {"https://searx.example": {"Bad Header": "private-value"}},
		"bad value":      {"https://searx.example": {"X-A": "private-value\r\nInjected: yes"}},
		"case collision": {"https://searx.example": {"x-a": "private-value", "X-A": "private-value"}},
	} {
		err := ValidateOriginHeaders(headers)
		if err == nil {
			t.Errorf("%s: want error", name)
			continue
		}
		if strings.Contains(err.Error(), "private-value") {
			t.Errorf("%s: error echoes a value: %v", name, err)
		}
	}
}

// hops fakes every server: it records the secret each URL received and
// answers redirects[url] with a 302, anything else with a 204.
type hops struct {
	seen      map[string]string
	redirects map[string]string
}

func (h hops) RoundTrip(req *http.Request) (*http.Response, error) {
	u := req.URL.String()
	h.seen[u] = req.Header.Get(secretHeader)
	resp := &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody, Request: req}
	if loc, ok := h.redirects[u]; ok {
		resp.StatusCode = http.StatusFound
		resp.Header.Set("Location", loc)
	}
	return resp, nil
}

// Headers for one origin must never follow a redirect to another host, another
// port, or the same host over plain http.
func TestWithOriginHeadersRedirects(t *testing.T) {
	const start = "https://searx.example/search"
	for _, tc := range []struct {
		name, target, want string
	}{
		{"same origin", "https://searx.example/landed", "s3cret"},
		{"same origin, explicit default port", "https://searx.example:443/landed", "s3cret"},
		{"other host", "https://evil.example/landed", ""},
		{"other port", "https://searx.example:8443/landed", ""},
		{"https to http downgrade", "http://searx.example/landed", ""},
		{"https to http downgrade on port 443", "http://searx.example:443/landed", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := hops{seen: map[string]string{}, redirects: map[string]string{start: tc.target}}
			c, err := WithOriginHeaders(&http.Client{Transport: fake}, map[string]map[string]string{
				"https://searx.example": {secretHeader: "s3cret"},
			})
			if err != nil {
				t.Fatal(err)
			}
			get(t, c, start)
			if fake.seen[start] != "s3cret" {
				t.Fatalf("configured origin got %q", fake.seen[start])
			}
			if got, ok := fake.seen[tc.target]; !ok || got != tc.want {
				t.Fatalf("redirect target got %q (followed: %v), want %q", got, ok, tc.want)
			}
		})
	}
}
