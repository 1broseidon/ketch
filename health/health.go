// Package health provides shared, bounded provider health checks.
package health

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Status classifies the outcome of a single doctor check.
type Status string

const (
	// StatusOK means the check passed: configured, reachable, credentials accepted.
	StatusOK Status = "ok"
	// StatusNoKey means the backend needs a key/token and none is configured.
	StatusNoKey Status = "no_key"
	// StatusUnreachable means the endpoint did not answer (network error,
	// timeout, or a server-side failure status).
	StatusUnreachable Status = "unreachable"
	// StatusMisconfigured means the endpoint answered but rejected the setup
	// (invalid key, SearXNG JSON format blocked, missing browser binary, ...).
	// Detail carries the fix hint.
	StatusMisconfigured Status = "misconfigured"
	// StatusSkipped means the check does not apply (e.g. no browser configured).
	StatusSkipped Status = "skipped"
)

func ProbeKeyPool(keys []string, probe func(string) (Status, string)) (Status, string) {
	if len(keys) == 0 {
		return probe("")
	}
	type outcome struct {
		status Status
		detail string
	}
	outcomes := make([]outcome, len(keys))
	var wg sync.WaitGroup
	for i, key := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, detail := probe(key)
			outcomes[i] = outcome{status: status, detail: detail}
		}()
	}
	wg.Wait()

	worst := StatusOK
	for _, result := range outcomes {
		if statusPriority(result.status) > statusPriority(worst) {
			worst = result.status
		}
	}

	var details []string
	for i, result := range outcomes {
		if result.status != worst || result.detail == "" {
			continue
		}
		details = append(details, fmt.Sprintf("key %d of %d: %s", i+1, len(keys), result.detail))
	}
	if len(details) > 0 {
		return worst, strings.Join(details, "; ")
	}
	if worst == StatusOK && len(keys) > 1 {
		return StatusOK, fmt.Sprintf("%d keys accepted", len(keys))
	}
	return worst, ""
}

func statusPriority(status Status) int {
	switch status {
	case StatusMisconfigured:
		return 5
	case StatusUnreachable:
		return 4
	case StatusNoKey:
		return 3
	case StatusOK:
		return 2
	case StatusSkipped:
		return 1
	default:
		return 0
	}
}

func Get(ctx context.Context, client *http.Client, u string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return client.Do(req)
}

func Drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
}

func ProbeMCP(ctx context.Context, client *http.Client, endpoint, name string) (Status, string) {
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return StatusUnreachable, ErrorDetail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		return StatusUnreachable, ErrorDetail(err)
	}
	defer Drain(resp)

	if resp.StatusCode == http.StatusOK {
		return StatusOK, ""
	}
	return StatusUnreachable, fmt.Sprintf("%s returned status %d", name, resp.StatusCode)
}

func ProbeReachable(ctx context.Context, client *http.Client, baseURL, name string) (Status, string) {
	return ProbeReachableWithHeaders(ctx, client, baseURL, name, nil)
}

// ProbeReachableWithHeaders is ProbeReachable for an instance that may sit
// behind an auth proxy: headerNames are the http_headers names configured for
// baseURL's origin, and a rejection of them fails the probe (see HeaderHint).
func ProbeReachableWithHeaders(ctx context.Context, client *http.Client, baseURL, name string, headerNames []string) (Status, string) {
	resp, err := Get(ctx, client, baseURL, nil)
	if err != nil {
		return StatusUnreachable, ErrorDetail(err)
	}
	defer Drain(resp)

	if resp.StatusCode >= http.StatusInternalServerError {
		return StatusUnreachable, fmt.Sprintf("%s returned status %d", name, resp.StatusCode)
	}
	return HeaderHint(resp, baseURL, headerNames, StatusOK, "")
}

// HeaderHint revises a probe's own status and detail when an auth proxy may
// have turned the probe away. headerNames are the http_headers names
// configured for probeURL's origin; with none, status and detail are returned
// as they are. Otherwise a redirect that ended on another origin (where the
// headers are never sent) or a 401/403 is misconfigured and the detail points
// at http_headers. A 401/403 could equally be the instance's own answer, so
// the probe's advice for it, if it had any, is kept after the hint. Only
// header names and origins are printed: a value never reaches this package.
func HeaderHint(resp *http.Response, probeURL string, headerNames []string, status Status, detail string) (Status, string) {
	u, err := url.Parse(strings.TrimSpace(probeURL))
	if err != nil || len(headerNames) == 0 {
		return status, detail
	}
	origin := u.Scheme + "://" + u.Host
	if final := resp.Request; final != nil && final.URL != nil && !sameOrigin(u, final.URL) {
		return StatusMisconfigured, fmt.Sprintf("redirected to %s (an auth login page?) — check http_headers for %s", final.URL.Host, origin)
	}
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		return status, detail
	}
	hint := fmt.Sprintf("rejected (HTTP %d); http_headers are set for %s (%s), check they are current", resp.StatusCode, origin, strings.Join(headerNames, ", "))
	if status == StatusMisconfigured && detail != "" {
		hint += "; otherwise: " + detail
	}
	return StatusMisconfigured, hint
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	switch {
	case u.Port() != "":
		return u.Port()
	case strings.EqualFold(u.Scheme, "https"):
		return "443"
	default:
		return "80"
	}
}

func ErrorDetail(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return err.Error()
}
