package mcp

import (
	"context"
	"errors"
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
	Limit   int    `json:"limit,omitempty" jsonschema:"max number of results (default: the configured limit)"`
	Regexp  bool   `json:"regexp,omitempty" jsonschema:"interpret query as a regular expression when supported by the backend"`
}

// CodeOutput is the output schema for the "code" tool. Results carries the
// same result objects as the CLI's `ketch code --json` (which emits them as
// a bare array; MCP structured content needs the object wrapper).
type CodeOutput struct {
	Results []code.Result `json:"results"`
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
		if in.Query == "" {
			return nil, CodeOutput{}, errf(kindValidation, "query is required")
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

		results, err := searcher.Search(ctx, code.Query{
			Term:   in.Query,
			Lang:   in.Lang,
			Limit:  limit,
			Regexp: in.Regexp,
		})
		if err != nil {
			if errors.Is(err, code.ErrRegexpUnsupported) {
				return nil, CodeOutput{}, errf(kindValidation, "backend %q does not support regexp (try backend=%s)", backend, strings.Join(code.RegexpBackends(), " or backend="))
			}
			return nil, CodeOutput{}, upstreamErrf(err, "code search failed")
		}

		return nil, CodeOutput{Results: results}, nil
	})
}
