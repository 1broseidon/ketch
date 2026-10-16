package httpx

import (
	"errors"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// redacted stands in for a header value wherever Redactor removes one.
const redacted = "[redacted]"

// Redactor removes the http_headers values configured for one origin from
// text that an instance backend would otherwise surface: a client error, a
// quoted response body, a doctor detail. An instance can echo a value it was
// sent into any of them, whether in a body, in a redirect's Location (which
// net/http quotes when it cannot follow it), or in the URL of a failed hop.
//
// Beside each value it removes the forms that value takes when echoed through
// a URL or a quoted string: trimmed, query- and path-escaped, and
// backslash-escaped. A nil *Redactor is valid and removes nothing.
type Redactor struct {
	forms    []string // longest first, so a value that contains another wins
	replacer *strings.Replacer
	longest  int // length of the longest form
}

// NewRedactor returns a Redactor for the values headers configures for
// rawURL's origin, matched exactly as WithOriginHeaders matches them, or nil
// when there are none. Values for other origins are not sent to rawURL, so an
// instance cannot echo them and they are left alone.
func NewRedactor(headers map[string]map[string]string, rawURL string) *Redactor {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil
	}
	byOrigin, err := parseOriginHeaders(headers)
	if err != nil {
		return nil // nothing is sent for an invalid configuration
	}
	seen := make(map[string]bool)
	var forms []string
	for _, values := range byOrigin[origin(u)] {
		for _, value := range values {
			for _, form := range valueForms(value) {
				if form != "" && !seen[form] {
					seen[form] = true
					forms = append(forms, form)
				}
			}
		}
	}
	if len(forms) == 0 {
		return nil
	}
	sort.Slice(forms, func(i, j int) bool {
		if len(forms[i]) != len(forms[j]) {
			return len(forms[i]) > len(forms[j])
		}
		return forms[i] < forms[j]
	})
	pairs := make([]string, 0, 2*len(forms))
	for _, form := range forms {
		pairs = append(pairs, form, redacted)
	}
	return &Redactor{forms: forms, replacer: strings.NewReplacer(pairs...), longest: len(forms[0])}
}

// valueForms lists value and the ways an instance may echo it back. A server
// sees a value without its surrounding whitespace.
func valueForms(value string) []string {
	trimmed := strings.TrimSpace(value)
	quoted := strconv.Quote(trimmed)
	return []string{
		value,
		trimmed,
		url.QueryEscape(trimmed),
		url.PathEscape(trimmed),
		quoted[1 : len(quoted)-1],
	}
}

// String returns s with every configured value replaced by "[redacted]".
func (r *Redactor) String(s string) string {
	if r == nil {
		return s
	}
	return r.replacer.Replace(s)
}

// Error returns err with its message redacted. An error that carries no value
// is returned as it is. Otherwise the result keeps errors.Is working for the
// causes callers classify on (context cancellation, a backend's not-found
// error), but exposes neither the original message nor, through Unwrap or As,
// the original chain.
func (r *Redactor) Error(err error) error {
	if r == nil || err == nil {
		return err
	}
	msg := err.Error()
	clean := r.replacer.Replace(msg)
	if clean == msg {
		return err
	}
	return &redactedError{msg: clean, err: err}
}

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }

func (e *redactedError) Is(target error) bool { return errors.Is(e.err, target) }

// Excerpt reads at most maxBytes of body, redacted. It redacts before it
// cuts: it reads far enough past maxBytes that a value straddling the cut is
// whole, and so removed, rather than left as a leading fragment. When body
// goes on past what Excerpt read, any fragment of a value at the end of that
// is dropped for the same reason. Read errors end the excerpt.
func (r *Redactor) Excerpt(body io.Reader, maxBytes int) string {
	limit := maxBytes
	if r != nil {
		limit += r.longest
	}
	raw, _ := io.ReadAll(io.LimitReader(body, int64(limit)+1))
	more := len(raw) > limit
	if more {
		raw = raw[:limit]
	}
	text := r.String(string(raw))
	if more {
		text = r.dropFragment(text)
	}
	if len(text) > maxBytes {
		text = text[:maxBytes]
	}
	return text
}

// dropFragment removes the longest suffix of s that is a proper prefix of a
// value: what is left of a value that the end of the input cut short.
func (r *Redactor) dropFragment(s string) string {
	if r == nil {
		return s
	}
	drop := 0
	for _, form := range r.forms {
		for n := min(len(form)-1, len(s)); n > drop; n-- {
			if strings.HasSuffix(s, form[:n]) {
				drop = n
				break
			}
		}
	}
	return s[:len(s)-drop]
}
