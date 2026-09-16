package search

import (
	"strings"
	"testing"
)

// An MCP server reports failure under HTTP 200, either as a JSON-RPC error
// object or as a tool-level isError. Decoding either as an empty success would
// make an upstream failure indistinguishable from a query that matched
// nothing, which stops the auto chain from falling through to the next
// provider — the failure looks like an answer.
func TestDecodeEXAResultsSurfacesUpstreamErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload string
		wantErr string
	}{
		{
			name:    "json-rpc error",
			payload: `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"internal error"}}`,
			wantErr: "exa JSON-RPC error -32603: internal error",
		},
		{
			name:    "tool error with detail",
			payload: `{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"quota exceeded"}]}}`,
			wantErr: "exa search tool returned an error: quota exceeded",
		},
		{
			name:    "tool error without detail",
			payload: `{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[]}}`,
			wantErr: "exa search tool returned an error",
		},
		{
			name:    "malformed payload",
			payload: `{"result":`,
			wantErr: "failed to decode exa response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			results, err := decodeEXAResults(tt.payload, 5)
			if err == nil {
				t.Fatalf("decodeEXAResults() = %v, want error %q", results, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err, tt.wantErr)
			}
			if results != nil {
				t.Fatalf("results = %v, want nil on error", results)
			}
		})
	}
}

func TestDecodeEXAResultsParsesSuccess(t *testing.T) {
	t.Parallel()
	payload := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Title: First\nURL: https://example.com/one\nA summary.\n---\nTitle: Second\nURL: https://example.com/two\nAnother summary."}]}}`

	results, err := decodeEXAResults(payload, 5)
	if err != nil {
		t.Fatalf("decodeEXAResults() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Title != "First" || results[0].URL != "https://example.com/one" {
		t.Errorf("results[0] = %+v", results[0])
	}
	if results[1].URL != "https://example.com/two" {
		t.Errorf("results[1] = %+v", results[1])
	}
}

// An empty content block is a real "no matches" answer, not a failure: the
// chain must stop there rather than sweep every remaining provider.
func TestDecodeEXAResultsEmptyIsNotAnError(t *testing.T) {
	t.Parallel()
	results, err := decodeEXAResults(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`, 5)
	if err != nil {
		t.Fatalf("decodeEXAResults() error = %v, want nil", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %v, want empty", results)
	}
}

func TestDecodeEXAResultsRespectsLimit(t *testing.T) {
	t.Parallel()
	payload := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Title: First\nURL: https://example.com/one\n---\nTitle: Second\nURL: https://example.com/two\n---\nTitle: Third\nURL: https://example.com/three"}]}}`

	results, err := decodeEXAResults(payload, 2)
	if err != nil {
		t.Fatalf("decodeEXAResults() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
}
