package mcp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/1broseidon/ketch/code"
	"github.com/1broseidon/ketch/internal/configbase"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeInput is the input schema for the "code" tool.
type CodeInput struct {
	Query   string `json:"query" jsonschema:"the code search query"`
	Backend string `json:"backend,omitempty" jsonschema:"code search backend (default: the configured backend)"`
	Lang    string `json:"lang,omitempty" jsonschema:"language filter appended to the query"`
	Repo    string `json:"repo,omitempty" jsonschema:"search one repository, owner/name (a github.com URL also works); matched exactly on every backend"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max number of results (default: the configured limit)"`
	Tag     string `json:"tag,omitempty" jsonschema:"record each result under this tag, retrievable later with the tag tool"`
	Regexp  bool   `json:"regexp,omitempty" jsonschema:"interpret query as a regular expression when supported by the backend"`
}

// CodeOutput is the output schema for the "code" tool. Results carries the
// same result objects as the CLI's `ketch code --json` (which emits them as
// a bare array; MCP structured content needs the object wrapper).
type CodeOutput struct {
	Warnings []string      `json:"warnings,omitempty"`
	Results  []code.Result `json:"results"`
}

func (s *Server) registerCodeTool() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name: "code",
		InputSchema: inputSchema[CodeInput](map[string]string{
			"backend": "code search backend: " + configbase.JoinNames(code.AvailableBackends()) + " (default: the configured backend)",
			"regexp":  "interpret query as a regular expression (" + strings.Join(code.RegexpBackends(), ", ") + " only)",
		}),
		Description: "Search code across open-source repositories using " + code.DescriptionNames(false) + " (default: the configured backend)." +
			errTaxonomy,
		Annotations: readOnlyOpenWorld(),
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in CodeInput) (*mcpsdk.CallToolResult, CodeOutput, error) {
		if err := validResearchTag(in.Tag); err != nil {
			return nil, CodeOutput{}, err
		}
		ctx, diagnostics := withTagDiagnostics(ctx)
		if in.Query == "" {
			return nil, CodeOutput{}, errf(kindValidation, "query is required")
		}
		repo, err := code.NormalizeRepo(in.Repo)
		if err != nil {
			return nil, CodeOutput{}, errf(kindValidation, "repo: %w", err)
		}
		backend := in.Backend
		if backend == "" {
			backend = s.cfg.CodeBackend
		}
		limit := in.Limit
		if limit <= 0 {
			limit = s.cfg.Limit
		}

		searcher, err := code.NewFromConfig(s.cfg, backend)
		if err != nil {
			return nil, CodeOutput{}, backendErrf(err, code.ErrUnknownBackend)
		}

		q := code.Query{
			Term:   in.Query,
			Lang:   in.Lang,
			Repo:   repo,
			Limit:  limit,
			Regexp: in.Regexp,
		}
		results, err := searcher.Search(ctx, q)
		if err != nil {
			return nil, CodeOutput{}, codeSearchErrf(err, backend)
		}

		s.recordResults(ctx, in.Tag, codeTagged(results))
		warnings := append(codeWarnings(backend, q, results), diagnostics.values()...)
		return nil, CodeOutput{Results: results, Warnings: warnings}, nil
	})
}

// codeSearchErrf classifies a failed search — the rule the CLI's
// codeSearchErr applies. A backend that cannot serve the request points at
// those that can: another backend may index a repository this one lacks
// ([not_found]), and regex is backend-specific ([validation]).
func codeSearchErrf(err error, backend string) error {
	switch {
	case errors.Is(err, code.ErrRegexpUnsupported):
		return errf(kindValidation, "backend %q does not support regexp (try backend=%s)", backend, strings.Join(code.RegexpBackends(), " or backend="))
	case errors.Is(err, code.ErrRepoNotFound):
		err = fmt.Errorf("%w (try backend=%s)", err, strings.Join(code.Alternatives(backend), " or backend="))
	}
	return upstreamErrf(err, "code search failed")
}

// codeWarnings flags results that may not mean what they appear to, as the
// CLI's warnCodeSearch does: a qualifier the backend matched as literal code
// instead of applying, or an empty repo-scoped search on a backend that
// indexes only some repositories.
func codeWarnings(backend string, q code.Query, results []code.Result) []string {
	p, _ := code.Lookup(backend)
	var warnings []string
	if tokens := p.LiteralQualifiers(q.Term); len(tokens) > 0 {
		quoted := make([]string, len(tokens))
		for i, t := range tokens {
			quoted[i] = strconv.Quote(t)
		}
		warnings = append(warnings, warnf(kindValidation, "%s searched %s as literal code, not as a filter: use the repo/lang options, or a backend that reads qualifiers (backend=%s)",
			backend, strings.Join(quoted, " "), strings.Join(code.QualifierBackends(), ", backend=")))
	}
	if p.MayLackRepo(q, results) {
		warnings = append(warnings, warnf(kindNotFound, "no results in %s: %s indexes a subset of public repositories and may not have it (try backend=%s)",
			q.Repo, backend, strings.Join(code.Alternatives(backend), " or backend=")))
	}
	return warnings
}

// codeTagged maps code hits onto index entries: the location is the title, the
// matching snippet the description.
func codeTagged(results []code.Result) []taggedResult {
	out := make([]taggedResult, 0, len(results))
	for _, r := range results {
		title := r.Repo
		if r.Path != "" {
			title += " " + r.Path
		}
		if r.Line > 0 {
			title += fmt.Sprintf(":%d", r.Line)
		}
		out = append(out, taggedResult{URL: r.URL, Title: title, Description: r.Snippet})
	}
	return out
}
