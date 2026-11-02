package docs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
)

// Registry popularity, the keyless tie-break between same-name projects
// (design/docs-keyless.md, Pipeline 4): GitHub stars need the API, the
// registries' own download counts do not. Every count is put on one scale,
// downloads a week, so projects in different registries compare:
//
//   - npm: the downloads API's last week;
//   - PyPI: pypistats.org's last week;
//   - crates.io: recent_downloads (the last 90 days, read with the crate)
//     over 13 weeks;
//   - Go: pkg.go.dev's count of packages importing the module, times
//     goImporterWeight. The Go proxy publishes no download counts.
//
// Dominance (dominant) asks for 50x, so the scale only has to be right to
// well within that factor for the projects it separates: a squatter's tens
// of downloads beside a library's millions.

// Popularity API bases; tests point a popularityClient's fields at fixtures.
const (
	npmDownloadsBase = "https://api.npmjs.org"
	pypiStatsBase    = "https://pypistats.org"
)

// goImporterWeight is the weekly downloads one importing package stands
// for. Popular npm and PyPI packages see on the order of a thousand
// downloads a week per dependent package (react, click), and Go modules
// are fetched by their importers' builds the same way, so an importer
// counts a thousand.
const goImporterWeight = 1000

// cratesRecentWeeks is the window crates.io's recent_downloads covers.
const cratesRecentWeeks = 13

// popularitySource reads a package's popularity in weekly downloads.
// Implemented by *popularityClient.
type popularitySource interface {
	weekly(ctx context.Context, pkg pkgInfo) (int64, error)
}

// popularityClient reads download counts from the registries' public,
// keyless endpoints.
type popularityClient struct {
	npm, pypi string     // API bases, no trailing slash
	registry  *pkgClient // the registries' client: its requests, limits and pkg.go.dev turns

	// pypiSlot takes turns at pypistats.org (getPatient): it answers a
	// burst of concurrent requests (a bare query's readings) with 429s.
	pypiSlot *turns

	mu   sync.Mutex
	memo map[string]*popCall // by package spec: one read per process
}

// popCall is one package's count, read once and shared.
type popCall struct {
	done chan struct{}
	n    int64
	err  error
}

// newPopularityClient returns a client for the public endpoints, asking
// through registry (pkg.go.dev's search is registry's too). It performs no
// I/O.
func newPopularityClient(registry *pkgClient) *popularityClient {
	return &popularityClient{
		npm:      npmDownloadsBase,
		pypi:     pypiStatsBase,
		registry: registry,
		pypiSlot: newTurns(0),
		memo:     map[string]*popCall{},
	}
}

var _ popularitySource = (*popularityClient)(nil)

// weekly is pkg's downloads a week. A package the endpoint does not know
// (a 404: pypistats has no row for a package nobody downloaded) counts 0,
// as the registry has it but no one uses it; any other failure is an
// error, so the caller treats the count as unknown.
//
// Each package is read once per client; concurrent reads of it share one
// request, and a failed read is not kept.
func (c *popularityClient) weekly(ctx context.Context, pkg pkgInfo) (int64, error) {
	key := pkg.spec()
	c.mu.Lock()
	if call, ok := c.memo[key]; ok {
		c.mu.Unlock()
		select {
		case <-call.done:
			if call.err == nil {
				return call.n, nil
			}
		case <-ctx.Done():
			return 0, ctx.Err()
		}
		c.mu.Lock()
	}
	call := &popCall{done: make(chan struct{})}
	c.memo[key] = call
	c.mu.Unlock()
	call.n, call.err = c.read(ctx, pkg)
	if call.err != nil {
		c.mu.Lock()
		if c.memo[key] == call {
			delete(c.memo, key)
		}
		c.mu.Unlock()
	}
	close(call.done)
	return call.n, call.err
}

// read is weekly without the memo.
func (c *popularityClient) read(ctx context.Context, pkg pkgInfo) (int64, error) {
	var n int64
	var err error
	switch pkg.eco {
	case ecoNPM:
		var resp struct {
			Downloads int64 `json:"downloads"`
		}
		err = c.registry.get(ctx, "npm downloads "+pkg.name, c.npm+"/downloads/point/last-week/"+npmPath(pkg.name), pkgMaxBody, decodeJSON(&resp))
		n = resp.Downloads
	case ecoPyPI:
		var resp struct {
			Data struct {
				LastWeek int64 `json:"last_week"`
			} `json:"data"`
		}
		err = c.pypiStats(ctx, pkg.name, &resp)
		n = resp.Data.LastWeek
	case ecoCrates:
		return pkg.downloads / cratesRecentWeeks, nil
	case ecoGo:
		n, err = c.goImporters(ctx, pkg.name)
		n *= goImporterWeight
	default:
		return 0, fmt.Errorf("%s has no popularity source", pkg.eco)
	}
	if errors.Is(err, ErrNotFound) {
		return 0, nil
	}
	return n, err
}

// pypiStats reads pypistats.org's recent downloads for name into v,
// patiently (getPatient): it allows a few requests a second, and a bare
// query asks for several names at once.
func (c *popularityClient) pypiStats(ctx context.Context, name string, v any) error {
	u := c.pypi + "/api/packages/" + url.PathEscape(normalizeName(ecoPyPI, name)) + "/recent"
	return c.registry.getPatient(ctx, c.pypiSlot, "pypistats "+name, u, decodeJSON(v))
}

// goImporters is the number of packages importing module, from its
// pkg.go.dev page. (pkg.go.dev's API search counts importing modules too,
// but answers only a few requests a minute.)
func (c *popularityClient) goImporters(ctx context.Context, module string) (int64, error) {
	return c.registry.goImportedBy(ctx, module)
}

// readPopularity fills each project's popularity, concurrently, the sum
// over its packages; a project already read is skipped. A package whose
// count could not be read leaves its project's count unknown (popErr), a
// keyless source that did not answer.
func (w *Web) readPopularity(ctx context.Context, projects []*project) {
	if w.popularity == nil {
		return
	}
	var wg sync.WaitGroup
	for _, p := range projects {
		if p == nil || p.popKnown || p.popErr != nil || len(p.pkgs) == 0 {
			continue
		}
		wg.Go(func() {
			pctx, cancel := context.WithTimeout(ctx, apiCallTimeout)
			defer cancel()
			var sum int64
			for _, pkg := range p.pkgs {
				n, err := w.popularity.weekly(pctx, pkg)
				if err != nil {
					p.popErr = fmt.Errorf("%s: popularity: %w", pkgLabel(pkg), err)
					return
				}
				sum += n
			}
			p.pop, p.popKnown = sum, true
		})
	}
	wg.Wait()
}

// popularityText is a weekly download count as an error states it.
func popularityText(n int64) string {
	if n < 0 {
		return "downloads unknown"
	}
	return fmt.Sprintf("%d downloads a week", n)
}
