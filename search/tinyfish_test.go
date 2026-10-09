package search

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
)

func tinyfishBackend(target string, keys ...string) *TinyFish {
	return &TinyFish{keys: deterministicPool(keys...), client: rewrittenClient(target)}
}

func TestTinyFishSearch(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/" || r.URL.Query().Get("query") != "go & docs" || r.URL.Query().Has("api_key") {
			t.Errorf("request = %s %s", r.Method, r.URL)
		}
		if r.Header.Get("X-Api-Key") != "test-key" {
			t.Error("missing X-API-Key header")
		}
		_, _ = io.WriteString(w, `{"results":[{"title":"No link","url":"  ","snippet":"skip"},{"title":"Go","url":"https://go.dev","snippet":"Go docs"},{"title":"Go blog","url":"https://go.dev/blog","snippet":"News"}]}`)
	}))
	t.Cleanup(server.Close)
	backend := tinyfishBackend(server.URL, "test-key")
	got, err := backend.Search(context.Background(), "go & docs", 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{{Title: "Go", URL: "https://go.dev", Description: "Go docs"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("results = %+v, want %+v", got, want)
	}
	got, err = backend.Search(context.Background(), "go & docs", 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("zero limit: results = %v, error = %v", got, err)
	}
}

func TestTinyFishSearchURLs(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"results":[
			{"url":"/url?opi=89978449&q=javascript%3Aalert(1)&sa=U&ved=stub"},
			{"title":"PostgreSQL JSON_TABLE","url":"/url?opi=89978449&q=https://www.postgresql.org/docs/current/functions-json.html&sa=U&ved=stub","snippet":"JSON_TABLE docs"},
			{"url":"/url?sa=U"},
			{"url":"/url?q=%2Frelative"},
			{"url":"/relative"},
			{"url":"//example.com/page"},
			{"url":"https:///missing-host"},
			{"url":"https://example.com/%zz"},
			{"url":"ftp://example.com/file"},
			{"title":"Encoded query","url":"/url?q=https%3A%2F%2Fexample.com%2F%3Fq%3Da%2Bb%26next%3D%252Fdocs&sa=U","snippet":"Keep destination encoding"},
			{"title":"Go","url":"http://go.dev/doc/","snippet":"Go docs"}
		]}`)
	}))
	t.Cleanup(server.Close)
	got, err := tinyfishBackend(server.URL, "test-key").Search(context.Background(), "postgres JSON_TABLE function", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{
		{Title: "PostgreSQL JSON_TABLE", URL: "https://www.postgresql.org/docs/current/functions-json.html", Description: "JSON_TABLE docs"},
		{Title: "Encoded query", URL: "https://example.com/?q=a+b&next=%2Fdocs", Description: "Keep destination encoding"},
		{Title: "Go", URL: "http://go.dev/doc/", Description: "Go docs"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("results = %+v, want %+v", got, want)
	}
}

func TestTinyFishErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusUnauthorized, "secret-key", "invalid API key"},
		{http.StatusForbidden, "secret-key", "invalid API key"},
		{http.StatusTooManyRequests, "secret-key", "rate limited"},
		{http.StatusInternalServerError, "secret-key", "status 500"},
		{http.StatusOK, "not json", "failed to decode tinyfish response"},
		{http.StatusOK, `{"results":[{"snippet":"` + strings.Repeat("x", 1<<20) + `"}]}`, "failed to decode tinyfish response"},
	} {
		t.Run(tc.want+http.StatusText(tc.status), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)
			_, err := tinyfishBackend(server.URL, "secret-key").Search(context.Background(), "q", 1)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "secret-key") {
				t.Fatalf("error = %v, want %q without key", err, tc.want)
			}
		})
	}
}

func TestTinyFishRotatesKeys(t *testing.T) {
	t.Parallel()
	var used []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		used = append(used, r.Header.Get("X-Api-Key"))
		if len(used) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"results":[]}`)
	}))
	t.Cleanup(server.Close)
	if _, err := tinyfishBackend(server.URL, "first", "second").Search(context.Background(), "q", 1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(used, []string{"first", "second"}) {
		t.Fatalf("keys used = %v", used)
	}
}

func TestTinyFishCancellation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("cancelled request reached the server")
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tinyfishBackend(server.URL, "test-key").Search(ctx, "q", 1); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestProbeTinyFish(t *testing.T) {
	t.Parallel()
	if status, _ := ProbeTinyFish(context.Background(), nil, tinyfishEndpoint, " "); status != health.StatusNoKey {
		t.Fatalf("blank key status = %s", status)
	}
	for _, tc := range []struct {
		code int
		want health.Status
	}{
		{http.StatusOK, health.StatusOK},
		{http.StatusUnauthorized, health.StatusMisconfigured},
		{http.StatusTooManyRequests, health.StatusOK},
		{http.StatusInternalServerError, health.StatusUnreachable},
	} {
		t.Run(http.StatusText(tc.code), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("query") != "ketch" || r.Header.Get("X-Api-Key") != "test-key" {
					t.Errorf("probe request = %s, key = %q", r.URL, r.Header.Get("X-Api-Key"))
				}
				w.WriteHeader(tc.code)
			}))
			t.Cleanup(server.Close)
			status, _ := ProbeTinyFish(context.Background(), server.Client(), server.URL, "test-key")
			if status != tc.want {
				t.Fatalf("status = %s, want %s", status, tc.want)
			}
		})
	}
}

func TestTinyFishRegistry(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	provider, ok := Lookup("tinyfish")
	if !ok || provider.InAuto(&cfg) {
		t.Fatalf("unconfigured provider: %+v, found=%t", provider, ok)
	}
	if _, err := provider.Build(&cfg); err == nil {
		t.Fatal("missing key should fail")
	}
	cfg.SetProvider("tinyfish_api_key", "test-key")
	if !provider.InAuto(&cfg) {
		t.Fatal("configured provider not in auto chain")
	}
	if _, err := provider.Build(&cfg); err != nil {
		t.Fatal(err)
	}
	if AutoChainNames(&cfg)[0] != "tinyfish" {
		t.Fatalf("auto chain = %v", AutoChainNames(&cfg))
	}
}
