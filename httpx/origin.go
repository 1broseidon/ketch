package httpx

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/http/httpguts"
)

// WithOriginHeaders returns a copy of client that adds headers[origin] to
// every request sent to that origin, such as Cloudflare Access service-token
// headers for a self-hosted instance. Keys are origins (scheme://host[:port]);
// a header the request already sets is left as is. The copy shares client's
// transport, so connections stay pooled. With no headers it returns client.
//
// Headers are added per round trip, and only when that hop's scheme, host and
// effective port match. http.Client builds each redirect from the caller's
// request, which this never modifies, so a redirect to another origin (or a
// downgrade to http) is followed without them.
func WithOriginHeaders(client *http.Client, headers map[string]map[string]string) (*http.Client, error) {
	byOrigin, err := parseOriginHeaders(headers)
	if err != nil || len(byOrigin) == 0 {
		return client, err
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	c := *client
	c.Transport = &originHeaderTransport{base: base, byOrigin: byOrigin}
	return &c, nil
}

// ValidateOriginHeaders reports an invalid origin, header name, or header
// value. Errors name the origin and header, never the value.
func ValidateOriginHeaders(headers map[string]map[string]string) error {
	_, err := parseOriginHeaders(headers)
	return err
}

// OriginHeaderNames lists the names of the headers configured for rawURL's
// origin, canonical and sorted, or nil when there are none. It matches origins
// exactly as WithOriginHeaders does, so a caller can report which headers a
// request carried without handling a value.
func OriginHeaderNames(headers map[string]map[string]string, rawURL string) []string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil
	}
	byOrigin, err := parseOriginHeaders(headers)
	if err != nil || len(byOrigin[origin(u)]) == 0 {
		return nil
	}
	names := make([]string, 0, len(byOrigin[origin(u)]))
	for name := range byOrigin[origin(u)] {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func parseOriginHeaders(headers map[string]map[string]string) (map[string]http.Header, error) {
	raws := make([]string, 0, len(headers))
	for raw := range headers {
		raws = append(raws, raw)
	}
	sort.Strings(raws) // report the same error on every run
	byOrigin := make(map[string]http.Header, len(headers))
	for _, raw := range raws {
		key, err := parseOrigin(raw)
		if err != nil {
			return nil, err
		}
		if _, dup := byOrigin[key]; dup {
			return nil, fmt.Errorf("%q names the same origin as another entry", raw)
		}
		if byOrigin[key], err = parseHeaders(raw, headers[raw]); err != nil {
			return nil, err
		}
	}
	return byOrigin, nil
}

// parseOrigin accepts scheme://host[:port] with an optional trailing slash.
// It never echoes a URL that carries credentials.
func parseOrigin(raw string) (string, error) {
	if strings.Contains(raw, "@") {
		return "", fmt.Errorf("an origin must not contain credentials")
	}
	u, err := url.Parse(raw)
	if err != nil || origin(u) == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q is not an origin (want scheme://host[:port], http or https)", raw)
	}
	return origin(u), nil
}

func parseHeaders(raw string, set map[string]string) (http.Header, error) {
	h := make(http.Header, len(set))
	for name, value := range set {
		canonical := http.CanonicalHeaderKey(name)
		switch {
		case !httpguts.ValidHeaderFieldName(name):
			return nil, fmt.Errorf("%s: %q is not a valid header name", raw, name)
		case !httpguts.ValidHeaderFieldValue(value):
			return nil, fmt.Errorf("%s: header %q has an invalid value", raw, name)
		case h[canonical] != nil:
			return nil, fmt.Errorf("%s: header %q is set twice", raw, canonical)
		}
		h[canonical] = []string{value}
	}
	return h, nil
}

// origin returns u's scheme://host:port with the default port filled in, or
// "" when u is not an absolute HTTP(S) URL.
func origin(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	switch {
	case u.Hostname() == "", scheme != "http" && scheme != "https":
		return ""
	case port == "" && scheme == "https":
		port = "443"
	case port == "":
		port = "80"
	}
	return scheme + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

type originHeaderTransport struct {
	base     http.RoundTripper
	byOrigin map[string]http.Header
}

func (t *originHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	headers := t.byOrigin[origin(req.URL)]
	if len(headers) == 0 {
		return t.base.RoundTrip(req)
	}
	// A RoundTripper must not modify the caller's request.
	req = req.Clone(req.Context())
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	for name, values := range headers {
		if _, set := req.Header[name]; !set {
			req.Header[name] = append([]string(nil), values...)
		}
	}
	return t.base.RoundTrip(req)
}
