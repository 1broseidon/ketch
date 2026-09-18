package mcp

import (
	"context"
	"strings"
	"unicode"

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
	URLs      []string `json:"urls,omitempty" jsonschema:"for add, the URLs to tag (a URL with no cached page is still indexed; its title and description fill in once fetched); for remove, the URLs to drop (omit to drop the whole tag)"`
}

// TagOutput is the output schema for the "tag" tool. Fields are populated per
// operation; the rest are omitted.
type TagOutput struct {
	Tag       string             `json:"tag,omitempty"`
	Entries   int                `json:"entries,omitempty"`
	Cached    int                `json:"cached,omitempty"`
	Pages     []cache.TaggedPage `json:"pages,omitempty"`
	Tags      []cache.TagSummary `json:"tags,omitempty"`
	Tagged    []string           `json:"tagged,omitempty"`
	NotCached []string           `json:"not_cached,omitempty"`
	Removed   int                `json:"removed,omitempty"`
}

func (s *Server) registerTagTool() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name: "tag",
		Description: "Label pages already fetched into the local cache, then ask what is under a label. " +
			"Use it to keep a working set for a project: tag the docs and write-ups that proved useful (or pass tag to search/scrape/crawl as you fetch), then later call operation=show to get an index of titles, URLs and descriptions instead of searching the web again. " +
			"Operations: add (tag URLs, cached or not), show (list one tag's pages), list (all tags), remove (drop a tag, or the given URLs from it). " +
			"Makes no network requests. The index is durable and outlives the cached page bodies: entries whose body has expired come back with cached=false and must be re-fetched with scrape, which restores them." +
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
		if s.cache == nil {
			return nil, TagOutput{}, errf(kindPrecondition, "the page cache is unavailable, so tags cannot be read or written")
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
		cached, err := s.cache.TagURL(in.Tag, key, url)
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
	pages, err := s.cache.Tagged(in.Tag)
	if err != nil {
		return nil, TagOutput{}, errf(kindPrecondition, "%v", err)
	}
	out := TagOutput{Tag: in.Tag, Entries: len(pages), Pages: pages}
	for _, p := range pages {
		if p.Cached {
			out.Cached++
		}
	}
	return nil, out, nil
}

func (s *Server) tagListOp() (*mcpsdk.CallToolResult, TagOutput, error) {
	tags, err := s.cache.TagList()
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
		removed, err = s.cache.RemoveTag(in.Tag)
	} else {
		keys := make([]string, 0, len(in.URLs))
		for _, url := range in.URLs {
			keys = append(keys, s.scraper.CacheKey(s.scraper.Rewrite(url)))
		}
		removed, _, err = s.cache.RemoveTagged(in.Tag, keys)
	}
	if err != nil {
		return nil, TagOutput{}, errf(kindPrecondition, "%v", err)
	}
	if removed == 0 {
		return nil, TagOutput{}, errf(kindNotFound, "nothing removed: %q holds no such entries", in.Tag)
	}
	return nil, TagOutput{Tag: in.Tag, Removed: removed}, nil
}

// validTagName mirrors the CLI's rule: the index packs the tag and the page
// key into one storage key separated by NUL, so control characters are out.
func validTagName(name string) error {
	if name == "" || strings.TrimSpace(name) != name {
		return errf(kindValidation, "tag must not be empty or padded with spaces")
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return errf(kindValidation, "tag must not contain control characters")
	}
	return nil
}

// recordTag indexes a page a fetching tool just retrieved, when that call
// passed a tag. Failures are silent for the same reason as on the CLI:
// losing an index entry must not fail the fetch that produced it.
func (s *Server) recordTag(tag, url string, page *scrape.Page) {
	if s == nil || tag == "" || page == nil || s.cache == nil {
		return
	}
	if validTagName(tag) != nil {
		return
	}
	_ = s.cache.TagPage(tag, s.scraper.CacheKey(s.scraper.Rewrite(url)), url, page)
}
