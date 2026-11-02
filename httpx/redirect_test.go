package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A context's redirect check sees every hop and can stop one; without one
// the client follows redirects as net/http does.
func TestWithRedirectCheck(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("landed"))
	}))
	defer final.Close()
	start := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/private", http.StatusFound)
	}))
	defer start.Close()

	client := New(0, 1)
	get := func(ctx context.Context) error {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, start.URL, nil)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		return err
	}
	if err := get(context.Background()); err != nil {
		t.Fatalf("no check: %v", err)
	}

	refused := errors.New("refused")
	var hops []string
	ctx := WithRedirectCheck(context.Background(), func(u *url.URL) error {
		hops = append(hops, u.String())
		if strings.HasSuffix(u.Path, "/private") {
			return refused
		}
		return nil
	})
	err := get(ctx)
	var uerr *url.Error
	if !errors.Is(err, refused) || !errors.As(err, &uerr) {
		t.Fatalf("checked redirect = %v, want the check's error in a *url.Error", err)
	}
	if len(hops) != 1 || hops[0] != final.URL+"/private" {
		t.Errorf("hops checked = %v", hops)
	}

	// WithoutRedirectCheck lifts every check, for an operator's own service.
	if err := get(WithoutRedirectCheck(ctx)); err != nil {
		t.Errorf("without the check: %v", err)
	}

	// Checks stack: an earlier one still runs.
	outer := errors.New("outer")
	ctx = WithRedirectCheck(WithRedirectCheck(context.Background(), func(*url.URL) error { return outer }), func(*url.URL) error { return nil })
	if err := get(ctx); !errors.Is(err, outer) {
		t.Errorf("stacked checks = %v, want the outer check's error", err)
	}
}
