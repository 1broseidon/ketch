// Package httpx provides shared, tuned HTTP clients for ketch.
//
// The default transport is configured for crawling a single host at
// non-trivial concurrency: MaxIdleConnsPerHost is high enough to avoid
// per-request TLS handshakes, and every client carries a request-level
// Timeout so a hung peer cannot stall a worker forever.
package httpx

import (
	"net"
	"net/http"
	"time"
)

// DefaultTimeout is the per-request timeout for ketch HTTP clients.
const DefaultTimeout = 30 * time.Second

// DefaultMaxIdleConnsPerHost keeps enough keep-alive connections around
// to serve a typical crawl concurrency without re-handshaking.
const DefaultMaxIdleConnsPerHost = 16

// New returns an *http.Client with a tuned transport. Safe for concurrent use.
func New(timeout time.Duration, maxIdleConnsPerHost int) *http.Client {
	t := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{Transport: t, Timeout: timeout}
}

var defaultClient = New(DefaultTimeout, DefaultMaxIdleConnsPerHost)

// Default returns a process-wide shared client with ketch defaults.
func Default() *http.Client {
	return defaultClient
}
