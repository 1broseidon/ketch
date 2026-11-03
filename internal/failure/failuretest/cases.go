// Package failuretest holds the classification table both surfaces are tested
// against, so the CLI's exit codes and the MCP server's prefixes are checked
// from one list and cannot drift apart.
package failuretest

import (
	"context"
	"errors"
	"fmt"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/docs"
	"github.com/1broseidon/ketch/extract"
	"github.com/1broseidon/ketch/internal/failure"
	"github.com/1broseidon/ketch/scrape"
	"github.com/1broseidon/ketch/search"
	bolterrors "go.etcd.io/bbolt/errors"
)

// Case is an error as a backend would return it, and the class both surfaces
// must report for it when the fallback is Upstream.
type Case struct {
	Name   string
	Err    error
	Kind   failure.Kind
	Reason string
}

// Cases is every sentinel ketch defines, wrapped the way callers wrap them,
// plus the context and storage conditions Classify decides itself.
func Cases() []Case {
	amb := &docs.AmbiguousLibraryError{Library: "widget", Matches: []docs.LibraryMatch{{ID: "https://widget.dev/"}, {ID: "https://widget.readthedocs.io/en/stable/"}}}
	return []Case{
		{"plain error", errors.New("boom"), failure.Upstream, ""},
		{"cancelled", fmt.Errorf("fetch: %w", context.Canceled), failure.Cancelled, "cancelled"},
		{"deadline", fmt.Errorf("fetch: %w", context.DeadlineExceeded), failure.Upstream, "timeout"},
		{"cancelled beats a sentinel", errors.Join(docs.ErrNotFound, context.Canceled), failure.Cancelled, "cancelled"},
		{"sentinel beats a deadline", errors.Join(context.DeadlineExceeded, docs.ErrNotFound), failure.NotFound, "not_found"},
		{"bolt lock timeout", fmt.Errorf("open tags: %w", bolterrors.ErrTimeout), failure.Upstream, "busy"},
		{"docs not found", fmt.Errorf("context7: library %q %w", "/no/such-lib", docs.ErrNotFound), failure.NotFound, "not_found"},
		{"docs library required", fmt.Errorf("web: %w: pass the docs site URL as library", docs.ErrLibraryRequired), failure.Validation, "library_required"},
		{"docs ambiguous", fmt.Errorf("web: %w", amb), failure.Validation, "ambiguous"},
		{"docs unknown backend", fmt.Errorf("%w %q", docs.ErrUnknownBackend, "x"), failure.Validation, "unknown_backend"},
		{"search unknown backend", fmt.Errorf("%w %q", search.ErrUnknownBackend, "x"), failure.Validation, "unknown_backend"},
		{"code unknown backend", fmt.Errorf("%w %q", code.ErrUnknownBackend, "x"), failure.Validation, "unknown_backend"},
		{"code regexp unsupported", fmt.Errorf("grep: %w", code.ErrRegexpUnsupported), failure.Validation, "regexp_unsupported"},
		{"code invalid repo", fmt.Errorf("%w, e.g. golang/go", code.ErrInvalidRepo), failure.Validation, "invalid_repo"},
		{"code repo not found", fmt.Errorf("%w: facebook/react", code.ErrRepoNotFound), failure.NotFound, "repo_not_found"},
		{"scrape invalid url", fmt.Errorf("%w: ftp://x", scrape.ErrInvalidURL), failure.Validation, "invalid_url"},
		{"scrape no browser", fmt.Errorf("render: %w", scrape.ErrNoBrowser), failure.Precondition, "no_browser"},
		{"scrape bad selector", fmt.Errorf("%w: div[", scrape.ErrBadSelector), failure.Validation, "bad_selector"},
		{"scrape no match", fmt.Errorf("%w: .x", scrape.ErrSelectorNoMatch), failure.NotFound, "no_match"},
		{"scrape pdf selector", scrape.ErrPDFSelectorUnsupported, failure.Validation, "pdf_selector_unsupported"},
		{"scrape pdf raw", scrape.ErrPDFRawUnsupported, failure.Validation, "pdf_raw_unsupported"},
		{"pdf no text", fmt.Errorf("extract: %w", extract.ErrPDFNoText), failure.Precondition, "pdf_no_text"},
		{"tags unsupported", cache.ErrTagsUnsupported, failure.Precondition, "tags_unsupported"},
		{"context7 quota spent", fmt.Errorf("context7 resolve failed: %w", &docs.RateLimitError{Limit: 200}), failure.Upstream, ""},
	}
}
