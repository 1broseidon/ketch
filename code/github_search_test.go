package code

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// GitHub already matches repo: exactly and case-insensitively, so the filter
// passes through as the qualifier.
func TestGitHubBuildQueryAddsRepo(t *testing.T) {
	g := NewGitHub("fixture-token")
	if got, want := g.buildQuery("NewFromConfig", "go", "1broseidon/ketch"), "NewFromConfig language:go repo:1broseidon/ketch"; got != want {
		t.Errorf("buildQuery = %q, want %q", got, want)
	}
	if got, want := g.buildQuery("NewFromConfig", "", ""), "NewFromConfig"; got != want {
		t.Errorf("buildQuery without filters = %q, want %q", got, want)
	}
}

// githubServer answers /search/code with status and body, and GraphQL star
// lookups with an empty result.
func githubServer(t *testing.T, status int, body string) (*GitHub, *string) {
	t.Helper()
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			fmt.Fprint(w, `{"data":{"nodes":[{"id":"R_1","stargazerCount":42}]}}`)
			return
		}
		query = r.URL.Query().Get("q")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	g := NewGitHub("fixture-token")
	g.client, g.apiBase = server.Client(), server.URL
	return g, &query
}

// githubUnsearchableBody is GitHub's 422 for a repo: qualifier naming a
// repository that does not exist or that the token cannot read.
const githubUnsearchableBody = `{"message":"Validation Failed","errors":[{"message":"The listed users and repositories cannot be searched either because the resources do not exist or you do not have permission to view them.","resource":"Search","field":"q","code":"invalid"}],"documentation_url":"https://docs.github.com/v3/search/"}`

// A named repository GitHub refuses to search is ErrRepoNotFound, not an
// upstream failure that invites a retry.
func TestGitHubMissingRepoIsNotFound(t *testing.T) {
	g, query := githubServer(t, http.StatusUnprocessableEntity, githubUnsearchableBody)

	_, err := g.Search(context.Background(), Query{Term: "reconcileChildFibers", Repo: "no-such-owner/nope", Limit: 5})
	if !errors.Is(err, ErrRepoNotFound) || !strings.Contains(err.Error(), "no-such-owner/nope") {
		t.Fatalf("err = %v, want ErrRepoNotFound naming the repository", err)
	}
	if !strings.Contains(*query, "repo:no-such-owner/nope") {
		t.Errorf("query = %q, want the repo: qualifier", *query)
	}

	// The same refusal of a qualifier the caller wrote into the query is
	// theirs to read; it stays the status error it was.
	if _, err := g.Search(context.Background(), Query{Term: "x repo:no-such-owner/nope", Limit: 5}); errors.Is(err, ErrRepoNotFound) || err == nil || !strings.Contains(err.Error(), "status 422") {
		t.Errorf("inline qualifier: err = %v, want the plain status error", err)
	}
}

// Other validation failures are not a missing repository.
func TestGitHubOtherValidationFailureIsNotRepoNotFound(t *testing.T) {
	g, _ := githubServer(t, http.StatusUnprocessableEntity, `{"message":"Validation Failed","errors":[{"message":"ERROR_TYPE_QUERY_PARSING_FATAL unable to parse query!"}]}`)
	_, err := g.Search(context.Background(), Query{Term: "(", Repo: "golang/go", Limit: 5})
	if err == nil || errors.Is(err, ErrRepoNotFound) {
		t.Fatalf("err = %v, want a status error that is not ErrRepoNotFound", err)
	}
}

func TestGitHubRepoScopedSearch(t *testing.T) {
	g, query := githubServer(t, http.StatusOK, `{"total_count":1,"items":[{"path":"src/runtime/mgcpacer.go","html_url":"https://github.com/golang/go/blob/master/src/runtime/mgcpacer.go","repository":{"full_name":"golang/go","node_id":"R_1"},"text_matches":[{"fragment":"var gcController gcControllerState","matches":[{"indices":[17,34],"text":"gcControllerState"}]}]}]}`)

	results, err := g.Search(context.Background(), Query{Term: "gcControllerState", Repo: "https://github.com/golang/go", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if *query != "gcControllerState repo:golang/go" {
		t.Errorf("query = %q, want the normalized repo: qualifier", *query)
	}
	if len(results) != 1 || results[0].Repo != "golang/go" || results[0].Stars != 42 {
		t.Fatalf("results = %+v", results)
	}
}
