package docstore

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/1broseidon/ketch/crawl"
	"github.com/1broseidon/ketch/scrape"
)

// Source names how a library's pages were (or will be) obtained.
type Source string

const (
	// SourceLLMSFull is a single llms-full.txt document holding the whole
	// site's docs as markdown: one fetch, no crawl.
	SourceLLMSFull Source = "llms-full"
	// SourceLLMS is an llms.txt index whose links are fetched as pages.
	SourceLLMS Source = "llms"
	// SourceSitemap is a sitemap (or sitemap index) whose URLs are fetched.
	SourceSitemap Source = "sitemap"
	// SourceCrawl is a same-host BFS crawl from the seed, scoped to Prefix.
	SourceCrawl Source = "crawl"
)

// probeTimeout bounds each discovery probe (llms.txt, robots.txt, sitemap).
const probeTimeout = 10 * time.Second

// Plan is what Discover decided to fetch. URLs is the filtered candidate list
// for list-backed sources (empty for SourceCrawl, whose page set is only
// known after crawling). Candidates counts URLs before the prefix filter.
type Plan struct {
	Seed       string   `json:"seed"`
	Source     Source   `json:"source"`
	SourceURL  string   `json:"source_url"`
	Prefix     string   `json:"prefix,omitempty"`
	Candidates int      `json:"candidates"`
	URLs       []string `json:"-"`
}

// DiscoverOptions steer Discover. Prefix overrides the path prefix derived
// from the seed ("" derives it; "/" means the whole host). ForceSitemap
// treats the seed as a sitemap URL regardless of its name.
type DiscoverOptions struct {
	Prefix       string
	ForceSitemap bool
}

// Discover picks the cheapest reliable way to obtain a site's docs from the
// seed URL, deterministically and without any web search:
//
//  1. a seed that is itself llms-full.txt, llms.txt, or a sitemap is used as is;
//  2. otherwise the origin's /llms-full.txt, then /llms.txt (as a link list);
//  3. then sitemaps named in /robots.txt, then /sitemap.xml;
//  4. otherwise a BFS crawl from the seed.
//
// List-backed sources are filtered to the seed's host and Prefix. When the
// filter leaves nothing, discovery falls through to the next source, so a
// sitemap that never mentions the docs path still ends in a crawl of it.
func Discover(ctx context.Context, s *scrape.Scraper, seed string, opts DiscoverOptions) (*Plan, error) {
	u, err := parseSeed(seed)
	if err != nil {
		return nil, err
	}
	prefix := opts.Prefix
	if prefix == "" {
		prefix = derivePrefix(u.Path)
	} else if prefix == "/" {
		prefix = ""
	}
	plan := &Plan{Seed: u.String(), Prefix: prefix}
	origin := u.Scheme + "://" + u.Host
	base := strings.ToLower(path.Base(u.Path))

	switch {
	case base == "llms-full.txt":
		plan.Source, plan.SourceURL, plan.URLs, plan.Candidates = SourceLLMSFull, u.String(), []string{u.String()}, 1
		return plan, nil
	case base == "llms.txt":
		return discoverLLMSList(ctx, s, plan, u.String(), u.Hostname())
	case opts.ForceSitemap || strings.HasSuffix(base, ".xml") || strings.Contains(base, "sitemap"):
		return discoverSitemaps(ctx, s, plan, []string{u.String()}, u.Hostname(), true)
	}

	if body, ok := probeText(ctx, s, origin+"/llms-full.txt"); ok && looksLikeMarkdown(body) {
		plan.Source, plan.SourceURL, plan.URLs, plan.Candidates = SourceLLMSFull, origin+"/llms-full.txt", []string{origin + "/llms-full.txt"}, 1
		return plan, nil
	}
	if body, ok := probeText(ctx, s, origin+"/llms.txt"); ok {
		if p, err := planLLMSList(plan, origin+"/llms.txt", body, u.Hostname()); err == nil && len(p.URLs) > 0 {
			return p, nil
		}
	}

	sitemaps := robotsSitemaps(ctx, s, origin)
	if len(sitemaps) == 0 {
		sitemaps = []string{origin + "/sitemap.xml"}
	}
	if p, err := discoverSitemaps(ctx, s, plan, sitemaps, u.Hostname(), false); err == nil && len(p.URLs) > 0 {
		return p, nil
	}

	plan.Source, plan.SourceURL = SourceCrawl, u.String()
	return plan, nil
}

func parseSeed(seed string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(seed))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("seed must be an absolute http(s) URL, got %q", seed)
	}
	u.Fragment = ""
	return u, nil
}

// derivePrefix turns the seed path into the allow prefix: "/docs/" and
// "/docs" both give "/docs"; a root or empty path gives "" (whole host). A
// seed that is a file (has an extension) uses its directory.
func derivePrefix(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	if path.Ext(p) != "" {
		p = path.Dir(p)
		if p == "/" || p == "." {
			return ""
		}
	}
	return p
}

// InScope reports whether raw is on host and under prefix.
func InScope(raw, host, prefix string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Hostname(), host) {
		return false
	}
	if prefix == "" {
		return true
	}
	p := u.Path
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}

func filterScope(urls []string, host, prefix string) []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" || !InScope(raw, host, prefix) {
			continue
		}
		key := strings.TrimRight(raw, "/")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, raw)
	}
	sort.Strings(out)
	return out
}

func discoverLLMSList(ctx context.Context, s *scrape.Scraper, plan *Plan, llmsURL, host string) (*Plan, error) {
	body, ok := probeText(ctx, s, llmsURL)
	if !ok {
		return nil, fmt.Errorf("fetch %s failed", llmsURL)
	}
	p, err := planLLMSList(plan, llmsURL, body, host)
	if err != nil {
		return nil, err
	}
	if len(p.URLs) == 0 {
		return nil, fmt.Errorf("%s lists no pages on %s under %q", llmsURL, host, plan.Prefix)
	}
	return p, nil
}

var mdLinkTarget = regexp.MustCompile(`\]\((https?://[^)\s]+)\)`)

// planLLMSList treats an llms.txt body as a link list. Links that are not
// on the seed host or under the prefix are ignored; a prefix that excludes
// every link falls back to the whole host, because llms.txt commonly points
// at a parallel .md tree the docs prefix does not cover.
func planLLMSList(plan *Plan, llmsURL, body, host string) (*Plan, error) {
	var links []string
	for _, m := range mdLinkTarget.FindAllStringSubmatch(body, -1) {
		links = append(links, m[1])
	}
	p := *plan
	p.Source, p.SourceURL, p.Candidates = SourceLLMS, llmsURL, len(links)
	p.URLs = filterScope(links, host, p.Prefix)
	if len(p.URLs) == 0 && p.Prefix != "" {
		p.URLs = filterScope(links, host, "")
	}
	if len(links) == 0 {
		return &p, fmt.Errorf("%s contains no links", llmsURL)
	}
	return &p, nil
}

func discoverSitemaps(ctx context.Context, s *scrape.Scraper, plan *Plan, sitemaps []string, host string, explicit bool) (*Plan, error) {
	p := *plan
	p.Source = SourceSitemap
	var all []string
	var used []string
	for _, sm := range sitemaps {
		probeCtx, cancel := context.WithTimeout(ctx, 2*probeTimeout)
		urls, err := crawl.FetchSitemap(probeCtx, s, sm)
		cancel()
		if err != nil {
			if explicit {
				return nil, fmt.Errorf("sitemap fetch failed: %w", err)
			}
			continue
		}
		used = append(used, sm)
		all = append(all, urls...)
	}
	if len(used) == 0 {
		return nil, fmt.Errorf("no sitemap found")
	}
	p.SourceURL = strings.Join(used, " ")
	p.Candidates = len(all)
	p.URLs = filterScope(all, host, p.Prefix)
	if explicit && len(p.URLs) == 0 {
		return nil, fmt.Errorf("sitemap lists %d URLs but none on %s under %q", len(all), host, p.Prefix)
	}
	return &p, nil
}

// robotsSitemaps returns the Sitemap: entries of /robots.txt, if any.
func robotsSitemaps(ctx context.Context, s *scrape.Scraper, origin string) []string {
	body, ok := probeText(ctx, s, origin+"/robots.txt")
	if !ok {
		return nil
	}
	var out []string
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if len(line) < 8 || !strings.EqualFold(line[:8], "sitemap:") {
			continue
		}
		if loc := strings.TrimSpace(line[8:]); loc != "" {
			out = append(out, loc)
		}
	}
	return out
}

// probeText fetches a URL expected to be plain text or markdown and returns
// its body. HTML responses (a soft-404 page, a SPA shell) are rejected.
func probeText(ctx context.Context, s *scrape.Scraper, raw string) (string, bool) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	content, err := s.FetchContent(probeCtx, raw)
	if err != nil || len(content.Body) == 0 {
		return "", false
	}
	ct := strings.ToLower(content.ContentType)
	if strings.Contains(ct, "html") || looksLikeHTML(content.Body) {
		return "", false
	}
	return string(content.Body), true
}

func looksLikeHTML(body []byte) bool {
	head := strings.ToLower(strings.TrimSpace(string(body[:min(len(body), 512)])))
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html")
}

// looksLikeMarkdown is a cheap sanity check that a claimed llms-full.txt has
// document structure rather than being a stub or an error string.
func looksLikeMarkdown(body string) bool {
	return len(body) > 200 && strings.Contains(body, "\n#")
}
