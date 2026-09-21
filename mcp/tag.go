package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/scrape"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// tag is the first published tool that is not a read-only network fetcher:
// it touches no network at all, and add/remove mutate the local index. The
// agent is both writer and reader here, which is why it is a tool rather than
// a CLI-only operator command like config/cache/doctor.

// TagInput is the input schema for the "tag" tool. Operation is an enum
// mirroring the CLI's verbs — the CLI's grammar is its own concern, but the
// four operations are the same four.
type TagInput struct {
	Operation string   `json:"operation" jsonschema:"one of: add, show, list, remove"`
	Tag       string   `json:"tag,omitempty" jsonschema:"the tag to act on; required for every operation except list"`
	Limit     *int     `json:"limit,omitempty" jsonschema:"show only: maximum entries (default 50; 0 = all)"`
	URLs      []string `json:"urls,omitempty" jsonschema:"for add, the URLs to tag (a URL with no cached page is still indexed; its title and description fill in once fetched); for remove, the URLs to drop (omit to drop the whole tag)"`
}

// TagOutput is the output schema for the "tag" tool. Fields are populated per
// operation; the rest are omitted.
type TagOutput struct {
	Tag         string             `json:"tag,omitempty"`
	Entries     int                `json:"entries"`
	Shown       int                `json:"shown"`
	CacheStatus string             `json:"cache_status,omitempty"`
	Cached      int                `json:"cached"`
	Pages       []cache.TaggedPage `json:"pages,omitempty"`
	Tags        []cache.TagSummary `json:"tags,omitempty"`
	Tagged      []string           `json:"tagged,omitempty"`
	NotCached   []string           `json:"not_cached,omitempty"`
	Removed     int                `json:"removed,omitempty"`
}

func (s *Server) registerTagTool() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name: "tag",
		Description: "Bookmark research sources under project or topic labels and revisit them across sessions. " +
			"Use it to keep a working set for a project: tag the docs and write-ups that proved useful (or pass tag to search/scrape/crawl as you fetch), then later call operation=show to get an index of titles, URLs and descriptions instead of searching the web again. " +
			"Operations: add (bookmark URLs, cached or not), show (newest 50 by default; limit=0 for all), list (all tags), remove (drop a tag, or the given URLs from it). " +
			"The index lives in independent storage and remains available when the page cache is locked. cache_status reports unavailable when cached flags could not be checked. Makes no network requests. The index is durable and outlives the cached page bodies: entries whose body has expired come back with cached=false and must be re-fetched with scrape, which restores them." +
			errTaxonomy,
		Annotations: localMutating(),
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in TagInput) (*mcpsdk.CallToolResult, TagOutput, error) {
		op := strings.ToLower(strings.TrimSpace(in.Operation))
		if op == "" {
			return nil, TagOutput{}, errf(kindValidation, "operation is required (add, show, list or remove)")
		}
		if op != "list" {
			if err := validTagName(in.Tag); err != nil {
				return nil, TagOutput{}, err
			}
		}
		if in.Limit != nil && (op != "show" || *in.Limit < 0) {
			return nil, TagOutput{}, errf(kindValidation, "limit must be zero or greater and is only valid for show")
		}

		switch op {
		case "add":
			return s.tagAdd(in)
		case "show":
			return s.tagShow(in)
		case "list":
			return s.tagListOp()
		case "remove":
			return s.tagRemove(in)
		default:
			return nil, TagOutput{}, errf(kindValidation, "unknown operation %q (want add, show, list or remove)", in.Operation)
		}
	})
}

func (s *Server) tagAdd(in TagInput) (*mcpsdk.CallToolResult, TagOutput, error) {
	if len(in.URLs) == 0 {
		return nil, TagOutput{}, errf(kindValidation, "urls is required for operation=add")
	}
	out := TagOutput{Tag: in.Tag, Tagged: []string{}, NotCached: []string{}}
	for _, url := range in.URLs {
		key := s.scraper.CacheKey(s.scraper.Rewrite(url))
		cached, err := s.tags.TagURL(in.Tag, key, url)
		if err != nil {
			return nil, TagOutput{}, errf(kindPrecondition, "%v", err)
		}
		out.Tagged = append(out.Tagged, url)
		// Indexed, but with no title or description until the page is
		// fetched; the caller may want to scrape these.
		if !cached {
			out.NotCached = append(out.NotCached, url)
		}
	}
	return nil, out, nil
}

func (s *Server) tagShow(in TagInput) (*mcpsdk.CallToolResult, TagOutput, error) {
	limit := cache.DefaultTagLimit
	if in.Limit != nil {
		limit = *in.Limit
	}
	view, err := s.tags.ShowTag(in.Tag, limit)
	if err != nil {
		return nil, TagOutput{}, errf(kindPrecondition, "%v", err)
	}
	return nil, TagOutput{Tag: in.Tag, Entries: view.Entries, Shown: view.Shown, Cached: view.Cached, Pages: view.Pages, CacheStatus: view.CacheStatus}, nil
}

func (s *Server) tagListOp() (*mcpsdk.CallToolResult, TagOutput, error) {
	tags, err := s.tags.TagList()
	if err != nil {
		return nil, TagOutput{}, errf(kindPrecondition, "%v", err)
	}
	if tags == nil {
		tags = []cache.TagSummary{}
	}
	return nil, TagOutput{Tags: tags}, nil
}

func (s *Server) tagRemove(in TagInput) (*mcpsdk.CallToolResult, TagOutput, error) {
	var (
		removed int
		err     error
	)
	if len(in.URLs) == 0 {
		removed, err = s.tags.RemoveTag(in.Tag)
	} else {
		removed, _, err = s.tags.RemoveTagged(in.Tag, in.URLs)
	}
	if err != nil {
		return nil, TagOutput{}, errf(kindPrecondition, "%v", err)
	}
	if removed == 0 {
		return nil, TagOutput{}, errf(kindNotFound, "nothing removed: %q holds no such entries", in.Tag)
	}
	return nil, TagOutput{Tag: in.Tag, Removed: removed}, nil
}

func validTagName(name string) error {
	if err := cache.ValidateTagName(name); err != nil {
		return errf(kindValidation, "%v", err)
	}
	return nil
}

func validResearchTag(tag string) error {
	if tag == "" {
		return nil
	}
	return validTagName(tag)
}

type tagDiagnosticsKey struct{}
type tagDiagnostics struct {
	mu       sync.Mutex
	warnings []string
}

func withTagDiagnostics(ctx context.Context) (context.Context, *tagDiagnostics) {
	d := &tagDiagnostics{}
	return context.WithValue(ctx, tagDiagnosticsKey{}, d), d
}
func (d *tagDiagnostics) report(err error) {
	if err == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	message := fmt.Sprintf("[precondition] bookmark update failed: %v", err)
	for _, previous := range d.warnings {
		if previous == message {
			return
		}
	}
	d.warnings = append(d.warnings, message)
}
func (d *tagDiagnostics) values() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.warnings...)
}
func reportTagError(ctx context.Context, err error) {
	if d, ok := ctx.Value(tagDiagnosticsKey{}).(*tagDiagnostics); ok {
		d.report(err)
	}
}

func (s *Server) recordResults(ctx context.Context, tag string, results []taggedResult) {
	if s == nil || tag == "" {
		return
	}
	for _, r := range results {
		if r.URL == "" {
			continue
		}
		reportTagError(ctx, s.tags.TagResult(tag, s.scraper.CacheKey(s.scraper.Rewrite(r.URL)), r.URL, r.Title, r.Description))
	}
}

type taggedResult struct{ URL, Title, Description string }

func (s *Server) recordTag(ctx context.Context, tag, url string, page *scrape.Page) {
	if s == nil || page == nil || s.tags == nil {
		return
	}
	key := s.scraper.CacheKey(s.scraper.Rewrite(url))
	if tag == "" {
		reportTagError(ctx, s.tags.Backfill(key, url, page))
		return
	}
	reportTagError(ctx, s.tags.TagPage(tag, key, url, page))
}

func (s *Server) tagPageCache(ctx context.Context, noCache bool) *cache.Cache {
	return s.pageCache(noCache).WithTagErrorHandler(func(err error) { reportTagError(ctx, err) })
}
