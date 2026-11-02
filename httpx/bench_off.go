//go:build !bench

package httpx

import "net/http"

// benchTransport is the identity in release builds. Builds with the bench
// tag replace it with the record/replay hook in bench.go, which ketch-bench
// uses to pin the network; release binaries never carry that hook and never
// read its KETCH_HTTP_* variables.
func benchTransport(t *http.Transport) http.RoundTripper { return t }
