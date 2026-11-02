package docs

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimitError is Context7 refusing a request for its quota: a 429, or an
// error status with ratelimit-remaining 0, or a quota an earlier response
// already reported spent. Keyless use runs on Context7's anonymous quota
// (per IP, about 200 requests a month), so the fix it names is a key.
//
// It is an upstream failure under ADR-0002 (CLI exit 4, MCP [upstream]):
// nothing about the request was wrong, and it succeeds again after Reset.
type RateLimitError struct {
	Keyed bool      // the request carried an API key
	Limit int       // the quota's size, 0 when Context7 did not say
	Reset time.Time // when the quota renews, zero when Context7 did not say
}

func (e *RateLimitError) Error() string {
	var b strings.Builder
	b.WriteString("context7: rate limited")
	var about []string
	switch {
	case e.Keyed && e.Limit > 0:
		about = append(about, fmt.Sprintf("this key's quota of %d requests is used up", e.Limit))
	case e.Keyed:
		about = append(about, "this key's quota is used up")
	case e.Limit > 0:
		about = append(about, fmt.Sprintf("the keyless quota of %d requests is used up", e.Limit))
	default:
		about = append(about, "the keyless quota is used up")
	}
	if !e.Reset.IsZero() {
		about = append(about, "resets "+formatContext7Time(e.Reset))
	}
	b.WriteString(" (" + strings.Join(about, "; ") + ")")
	if e.Keyed {
		b.WriteString(". Use a key with a larger quota: ketch config set context7_api_key <key>")
	} else {
		b.WriteString(". Set a key to lift it: ketch config set context7_api_key <key>")
	}
	return b.String()
}

// formatContext7Time writes a reset time in UTC to the minute: the
// anonymous quota renews at the start of a month, so seconds say nothing.
func formatContext7Time(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// context7Quota is the rate limit one Context7 response reported in its
// ratelimit-limit, ratelimit-remaining and ratelimit-reset headers.
type context7Quota struct {
	known      bool // ratelimit-remaining was present and readable
	limit      int
	remaining  int
	reset      time.Time
	observedAt time.Time // when the response arrived
}

// parseContext7Quota reads a response's rate limit headers. ratelimit-reset
// is a Unix time (1796083200 is 2026-12-01 00:00 UTC); a small value is
// read as seconds from now, as the IETF RateLimit draft writes it, and
// Retry-After stands in when it is missing.
func parseContext7Quota(h http.Header, now time.Time) context7Quota {
	q := context7Quota{observedAt: now}
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("ratelimit-remaining"))); err == nil && n >= 0 {
		q.known, q.remaining = true, n
	}
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("ratelimit-limit"))); err == nil && n > 0 {
		q.limit = n
	}
	q.reset = resetTime(h.Get("ratelimit-reset"), now)
	if q.reset.IsZero() {
		q.reset = resetTime(h.Get("Retry-After"), now)
	}
	return q
}

// resetTime reads a reset header: a Unix time, or delta seconds below a
// billion (2001), or an HTTP date (Retry-After). Zero when unreadable.
func resetTime(v string, now time.Time) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
		if n < 1_000_000_000 {
			return now.Add(time.Duration(n) * time.Second)
		}
		return time.Unix(n, 0)
	}
	if t, err := http.ParseTime(v); err == nil {
		return t
	}
	return time.Time{}
}

// context7QuotaTracker remembers the last quota Context7 reported for one
// key (or for keyless use). A nil tracker remembers nothing. With a state
// file it also writes each reported quota there and, before a request,
// reads what another process saw, so a quota spent in one ketch process
// is not asked about again by the next (see context7State).
type context7QuotaTracker struct {
	mu    sync.Mutex
	last  context7Quota
	state func() *context7State // nil, or returning nil: this process only
	entry string                // the state file's entry for this key
}

// observe records a response's quota, when it reported one.
func (t *context7QuotaTracker) observe(q context7Quota) {
	if t == nil || !q.known {
		return
	}
	t.mu.Lock()
	t.last = q
	t.mu.Unlock()
	if t.state != nil {
		t.state().save(t.entry, q)
	}
}

// spent is a *RateLimitError when the last quota seen, in this process or
// (from the state file) another, says the quota is used up and it has not
// reset yet. A quota with no reset time is asked again: there is no telling
// when it renews. One another process saw counts for at most
// context7StateTrust: the anonymous quota belongs to an IP address, and a
// machine that moved networks should not wait out a month for it.
func (t *context7QuotaTracker) spent(keyed bool, now time.Time) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	q := t.last
	t.mu.Unlock()
	if !q.known && t.state != nil {
		if saved, ok := t.state().load(t.entry); ok && now.Sub(saved.observedAt) < context7StateTrust {
			q = saved
		}
	}
	if !q.known || q.remaining > 0 || q.reset.IsZero() || !now.Before(q.reset) {
		return nil
	}
	return &RateLimitError{Keyed: keyed, Limit: q.limit, Reset: q.reset}
}

var (
	context7QuotasMu sync.Mutex
	context7Quotas   = map[string]*context7QuotaTracker{}
)

// sharedContext7Quota is the process's tracker for apiKey ("" for keyless
// use), backed by the state file (found again on each use, so a changed
// cache directory is honoured). Each docs call builds its own client; the
// quota outlives them.
func sharedContext7Quota(apiKey string) *context7QuotaTracker {
	context7QuotasMu.Lock()
	defer context7QuotasMu.Unlock()
	t, ok := context7Quotas[apiKey]
	if !ok {
		t = &context7QuotaTracker{state: defaultContext7State, entry: context7StateEntry(apiKey)}
		context7Quotas[apiKey] = t
	}
	return t
}

// quotaDetail describes a reported quota for doctor: "189 of 200 requests
// left, resets 2026-12-01 00:00 UTC". Empty when the response reported none.
func (q context7Quota) detail() string {
	if !q.known {
		return ""
	}
	s := fmt.Sprintf("%d requests left", q.remaining)
	if q.limit > 0 {
		s = fmt.Sprintf("%d of %d requests left", q.remaining, q.limit)
	}
	if !q.reset.IsZero() {
		s += ", resets " + formatContext7Time(q.reset)
	}
	return s
}
