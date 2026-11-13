package httpx

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// maxRedirects is net/http's own redirect limit, kept by CheckRedirect.
const maxRedirects = 10

type redirectCheckKey struct{}

// WithRedirectCheck returns a copy of ctx carrying check. A request made
// with that context through any client from this package (Default, New and
// copies of them) calls check with the URL of every redirect hop before
// following it; a non-nil error stops the request there, and the client
// returns it wrapped in a *url.Error. A check already on ctx still runs
// first. It is how a caller fetching third-party URLs keeps a redirect from
// leading into address space the original URL was not allowed to reach.
func WithRedirectCheck(ctx context.Context, check func(*url.URL) error) context.Context {
	if check == nil {
		return ctx
	}
	if prev := redirectCheck(ctx); prev != nil {
		inner := check
		check = func(u *url.URL) error {
			if err := prev(u); err != nil {
				return err
			}
			return inner(u)
		}
	}
	return context.WithValue(ctx, redirectCheckKey{}, check)
}

// WithoutRedirectCheck returns a copy of ctx with no redirect check, for a
// request made on the caller's own behalf inside a checked operation: an
// operator-configured service (a self-hosted search instance on localhost)
// may redirect where a third-party URL may not.
func WithoutRedirectCheck(ctx context.Context) context.Context {
	if redirectCheck(ctx) == nil {
		return ctx
	}
	return context.WithValue(ctx, redirectCheckKey{}, (func(*url.URL) error)(nil))
}

// HasRedirectCheck reports whether a request made with ctx has its
// redirects checked (WithRedirectCheck): a response fetched without a
// check is not one a checked request may reuse.
func HasRedirectCheck(ctx context.Context) bool { return redirectCheck(ctx) != nil }

func redirectCheck(ctx context.Context) func(*url.URL) error {
	check, _ := ctx.Value(redirectCheckKey{}).(func(*url.URL) error)
	return check
}

// CheckRedirect is the http.Client CheckRedirect policy of this package's
// clients: net/http's limit of 10 redirects, then the request context's
// WithRedirectCheck, when it has one. A client with a policy of its own
// should call it too.
func CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", len(via))
	}
	if check := redirectCheck(req.Context()); check != nil {
		return check(req.URL)
	}
	return nil
}
