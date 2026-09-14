// Package doctor runs cheap live health checks against every ketch surface:
// search/code/docs backends, the configured browser, and the page cache.
// Probes are read-only — they never write cache entries or mutate config —
// and each is bounded by a per-probe timeout so a full run stays fast.
package doctor

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	"github.com/1broseidon/ketch/search"
)

// Status preserves the public doctor status type.
type Status = health.Status

const (
	StatusOK            = health.StatusOK
	StatusNoKey         = health.StatusNoKey
	StatusUnreachable   = health.StatusUnreachable
	StatusMisconfigured = health.StatusMisconfigured
	StatusSkipped       = health.StatusSkipped
)

// Check is one line of the doctor report. The JSON schema is stable:
// {surface, backend, status, detail, latency_ms}.
type Check struct {
	Surface   string `json:"surface"`
	Backend   string `json:"backend"`
	Status    Status `json:"status"`
	Detail    string `json:"detail,omitempty"`
	LatencyMS int64  `json:"latency_ms"`

	// Required marks checks that gate the process exit code: the default
	// backend of each surface, backends with an API key explicitly configured,
	// the configured browser, and the cache. Not part of the JSON schema.
	Required bool `json:"-"`
}

// Bad reports whether the check found a problem (anything but ok/skipped).
func (c Check) Bad() bool {
	return c.Status != StatusOK && c.Status != StatusSkipped
}

// spec describes one check before it runs.
type spec struct {
	minTimeout time.Duration
	surface    string
	backend    string
	required   bool
	probe      func(ctx context.Context) (Status, string)
}

// DefaultTimeout is the per-probe timeout. Probes run concurrently, so the
// whole run is bounded by roughly one timeout, not the sum.
const DefaultTimeout = 3 * time.Second

// SelfHostedSearchTimeout is the budget for probes that make the instance run a
// real federated search rather than answer a single API call. SearXNG spends
// about three seconds on its own upstream engines, landing right on
// DefaultTimeout, so the default budget reports healthy instances as timed out.
const SelfHostedSearchTimeout = 10 * time.Second

// probeTimeout respects provider-owned minimum budgets and longer caller budgets.
func probeTimeout(s spec, runTimeout time.Duration) time.Duration {
	if s.minTimeout > runTimeout {
		return s.minTimeout
	}
	return runTimeout
}

// Run executes every check concurrently, each bounded by timeout
// (DefaultTimeout if <= 0), and returns results in stable surface order.
func Run(ctx context.Context, cfg *config.Config, timeout time.Duration) []Check {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	specs := buildSpecs(cfg, httpx.Default())
	checks := make([]Check, len(specs))

	var wg sync.WaitGroup
	for i, s := range specs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, probeTimeout(s, timeout))
			defer cancel()
			start := time.Now()
			status, detail := s.probe(pctx)
			checks[i] = Check{
				Surface:   s.surface,
				Backend:   s.backend,
				Status:    status,
				Detail:    detail,
				LatencyMS: time.Since(start).Milliseconds(),
				Required:  s.required,
			}
		}()
	}
	wg.Wait()
	return checks
}

// buildSpecs assembles the check list for cfg. A check is required (gates the
// exit code) when it covers the configured default backend of its surface, a
// backend whose API key is explicitly set, the configured browser, or the
// cache. Optional backends that merely lack a key stay informational.
func buildSpecs(cfg *config.Config, client *http.Client) []spec {
	var specs []spec
	for _, p := range search.Providers() {
		specs = append(specs, spec{p.MinProbeTimeout, "search", p.ID, p.Required(cfg), func(ctx context.Context) (Status, string) { return p.Probe(ctx, client, cfg) }})
	}
	for _, p := range code.Providers() {
		specs = append(specs, spec{0, "code", p.ID, p.Required(cfg), func(ctx context.Context) (Status, string) { return p.Probe(ctx, client, cfg) }})
	}
	for _, p := range docs.Providers() {
		if p.Hidden {
			continue
		}
		specs = append(specs, spec{0, "docs", p.ID, p.Required(cfg), func(ctx context.Context) (Status, string) { return p.Probe(ctx, client, cfg) }})
	}
	return append(specs,
		spec{0, "browser", browserBackendName(cfg.Browser), cfg.Browser != "", func(context.Context) (Status, string) { return checkBrowser(cfg.Browser) }},
		spec{0, "cookies", "jar", cfg.CookieFile != "", func(context.Context) (Status, string) { return checkCookieFile(cfg.CookieFile) }},
		spec{0, "cache", "bbolt", true, func(context.Context) (Status, string) { return checkCache() }})
}

// browserBackendName labels the browser check's backend column.
func browserBackendName(configured string) string {
	if configured == "" {
		return "none"
	}
	return configured
}
