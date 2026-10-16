package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// A redirect can carry a request to any configured origin, which is then sent
// that origin's headers, so every configured value is redacted.
func TestNewRedactorCoversEveryConfiguredOrigin(t *testing.T) {
	headers := map[string]map[string]string{
		"https://a.example":      {"X-One": "alpha-secret"},
		"https://b.example:8443": {"X-Two": "beta-secret"},
	}
	if got := NewRedactor(headers).String("alpha-secret beta-secret other"); got != "[redacted] [redacted] other" {
		t.Fatalf("redacted %q", got)
	}
	t.Run("no headers", func(t *testing.T) {
		if r := NewRedactor(nil); r != nil {
			t.Fatal("no configured values needs no redactor")
		}
	})
	t.Run("invalid configuration", func(t *testing.T) {
		bad := map[string]map[string]string{"https://a.example": {"X-One": "alpha-secret"}, "not an origin": {"X": "y"}}
		if r := NewRedactor(bad); r != nil {
			t.Fatal("a configuration that sends nothing needs no redactor")
		}
	})
}

func TestRedactorStringRemovesEveryFormOfAValue(t *testing.T) {
	const value = " Bearer abc/def+ghi== "
	r := NewRedactor(map[string]map[string]string{"https://a.example": {"Authorization": value}})
	trimmed := strings.TrimSpace(value)
	for name, echoed := range map[string]string{
		"as sent":       value,
		"trimmed":       trimmed,
		"query escaped": url.QueryEscape(trimmed),
		"path escaped":  url.PathEscape(trimmed),
		"quoted":        fmt.Sprintf("%q", trimmed),
	} {
		t.Run(name, func(t *testing.T) {
			got := r.String(`Get "https://a.example/?t=` + echoed + `": boom`)
			if strings.Contains(got, "abc") || strings.Contains(got, "ghi") || !strings.Contains(got, "[redacted]") {
				t.Fatalf("redacted %q", got)
			}
		})
	}
}

func TestRedactorStringPrefersTheLongerOfOverlappingValues(t *testing.T) {
	r := NewRedactor(map[string]map[string]string{"https://a.example": {"X-One": "abcd", "X-Two": "bc"}})
	if got := r.String("x abcd x bc x"); got != "x [redacted] x [redacted] x" {
		t.Fatalf("redacted %q", got)
	}
}

func TestNilRedactorRedactsNothing(t *testing.T) {
	var r *Redactor
	err := errors.New("boom")
	if r.String("boom") != "boom" || r.Error(err) != err || r.Error(nil) != nil {
		t.Fatal("a nil redactor must pass everything through")
	}
}

func TestRedactorError(t *testing.T) {
	r := NewRedactor(map[string]map[string]string{"https://a.example": {"X-One": "s3cret-value"}})

	clean := errors.New("connection refused")
	if got := r.Error(clean); got != clean {
		t.Fatalf("an error without a value changed: %v", got)
	}
	if r.Error(nil) != nil {
		t.Fatal("nil error changed")
	}

	leaky := &url.Error{Op: "Get", URL: "https://a.example/?t=s3cret-value", Err: context.DeadlineExceeded}
	got := r.Error(fmt.Errorf("search failed: %w", leaky))
	if strings.Contains(got.Error(), "s3cret") || !strings.Contains(got.Error(), "[redacted]") {
		t.Fatalf("error = %q", got)
	}
	// Callers classify on the cause, but nothing reaches the original message.
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Fatal("errors.Is no longer sees the cause")
	}
	var urlErr *url.Error
	if errors.As(got, &urlErr) || errors.Unwrap(got) != nil {
		t.Fatal("the unredacted error is reachable")
	}
}

func TestRedactorExcerpt(t *testing.T) {
	const value = "s3cret-value"
	r := NewRedactor(map[string]map[string]string{"https://a.example": {"X-One": value}})

	t.Run("a value across the cut is removed, not cut", func(t *testing.T) {
		for _, offset := range []int{4096 - len(value), 4096 - len(value)/2, 4095} {
			got := r.Excerpt(strings.NewReader(strings.Repeat("x", offset)+value+" and more"), 4096)
			if len(got) != 4096 {
				t.Fatalf("offset %d: excerpt is %d bytes, want 4096", offset, len(got))
			}
			tail := got[offset:]
			if !strings.HasPrefix(tail, "[redacted]") && !strings.HasPrefix("[redacted]", tail) {
				t.Fatalf("offset %d: excerpt continues %q, want the redaction marker", offset, tail)
			}
		}
	})

	t.Run("a value the read cut short is dropped", func(t *testing.T) {
		long := strings.Repeat("Q7z", 70)[:200]
		long = long[:len(long)-1] + "!"
		r := NewRedactor(map[string]map[string]string{"https://a.example": {"X-One": long}})
		// Thirty values is more than the read takes, and what is read redacts to
		// well under the cut, so only the fragment at the end of the read is left.
		got := r.Excerpt(strings.NewReader(strings.Repeat(long, 30)), 4096)
		if strings.Contains(got, "Q7") || strings.Contains(got, "z") {
			t.Fatalf("excerpt kept part of a value: %q", got)
		}
		if got == "" || strings.ReplaceAll(got, "[redacted]", "") != "" {
			t.Fatalf("excerpt = %q, want only markers", got)
		}
	})

	t.Run("a body that ends in what looks like the start of a value is kept", func(t *testing.T) {
		if got := r.Excerpt(strings.NewReader("denied: s3c"), 4096); got != "denied: s3c" {
			t.Fatalf("excerpt = %q", got)
		}
	})

	t.Run("without a value it is a plain bounded read", func(t *testing.T) {
		var none *Redactor
		if got := none.Excerpt(strings.NewReader(strings.Repeat("a", 5000)), 4096); len(got) != 4096 {
			t.Fatalf("excerpt is %d bytes, want 4096", len(got))
		}
	})
}

func TestNewValueRedactor(t *testing.T) {
	if r := NewValueRedactor("", ""); r != nil {
		t.Fatalf("blank values: got %v, want nil", r)
	}
	r := NewValueRedactor("sk/1+2")
	got := r.String("Bearer sk/1+2 ?key=sk%2F1%2B2")
	if want := "Bearer [redacted] ?key=[redacted]"; got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}
}
