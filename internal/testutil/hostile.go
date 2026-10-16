package testutil

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

// The ways HostileInstance can answer.
const (
	// EchoBody fails with a 500 whose body, and a response header, carry the
	// secret, as an auth proxy's error page might.
	EchoBody = "body"
	// EchoMalformedRedirect redirects to a Location net/http cannot parse, and
	// its error quotes that Location.
	EchoMalformedRedirect = "malformed-redirect"
	// EchoRedirectLoop redirects to itself with the secret in the query, until
	// the client gives up and names the last URL.
	EchoRedirectLoop = "redirect-loop"
	// EchoDeadRedirect redirects to a closed port with the secret in the query,
	// so the dial error names the URL.
	EchoDeadRedirect = "dead-redirect"
)

// HostileModes lists every mode HostileInstance accepts.
var HostileModes = []string{EchoBody, EchoMalformedRedirect, EchoRedirectLoop, EchoDeadRedirect}

// HostileInstance starts an instance that answers every request by echoing the
// value of the request header named header, in the way mode describes: what a
// misbehaving instance, or an auth proxy in front of one, could do with a
// secret it was sent. The test fails if the instance is never sent a value.
func HostileInstance(t testing.TB, header, mode string) *httptest.Server {
	t.Helper()
	dead := closedPortURL(t)
	var received atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret := r.Header.Get(header)
		if secret != "" {
			received.Store(true)
		}
		w.Header().Set("X-Echo", secret)
		switch mode {
		case EchoMalformedRedirect:
			w.Header().Set("Location", "http://"+secret+"/%zz")
			w.WriteHeader(http.StatusFound)
		case EchoRedirectLoop:
			w.Header().Set("Location", "/loop?echo="+url.QueryEscape(secret))
			w.WriteHeader(http.StatusFound)
		case EchoDeadRedirect:
			w.Header().Set("Location", dead+"/?echo="+url.QueryEscape(secret))
			w.WriteHeader(http.StatusFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprintf(w, "denied: %s=%s", header, secret)
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		if !received.Load() {
			t.Errorf("the instance was never sent %s", header)
		}
	})
	return srv
}

// closedPortURL returns the origin of a port nothing listens on.
func closedPortURL(t testing.TB) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return "http://" + addr
}
