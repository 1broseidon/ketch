package search

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/health"
)

type serpBaseErrorReader struct {
	err error
}

func (r serpBaseErrorReader) Read([]byte) (int, error) { return 0, r.err }

func serpBaseBodyErrorClient(prefix string, err error) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(io.MultiReader(strings.NewReader(prefix), serpBaseErrorReader{err: err})),
		}, nil
	})}
}

func TestSerpBaseResponseBodyErrors(t *testing.T) {
	tests := []struct {
		name       string
		cause      error
		wantDetail string
	}{
		{"cancelled", context.Canceled, "context canceled"},
		{"deadline", context.DeadlineExceeded, "timed out"},
		{"interrupted", io.ErrUnexpectedEOF, "unexpected EOF"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Exercise interrupted JSON and a complete payload followed by a
			// read failure, which must not be mistaken for a successful response.
			for _, prefix := range []string{`{"status":0,"organic":[`, `{"status":0,"organic":[]}`} {
				client := serpBaseBodyErrorClient(prefix, tc.cause)
				backend := &SerpBase{keys: deterministicPool("test-key"), client: client}
				_, err := backend.Search(context.Background(), "q", 1)
				if !errors.Is(err, tc.cause) {
					t.Errorf("Search with body %q: err = %v, want %v", prefix, err, tc.cause)
				}

				status, detail := ProbeSerpBase(context.Background(), client, serpbaseEndpoint, "test-key")
				if status != health.StatusUnreachable || detail != tc.wantDetail {
					t.Errorf("Probe with body %q: got (%s, %q), want (%s, %q)", prefix, status, detail, health.StatusUnreachable, tc.wantDetail)
				}
			}
		})
	}
}
