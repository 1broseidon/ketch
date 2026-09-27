package scrape

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrInvalidURL indicates that a scrape target is not an absolute HTTP(S) URL.
var ErrInvalidURL = errors.New("invalid URL: only absolute http and https URLs are supported")

// ValidateWebURL checks the effective fetch URL before network access or a cache hit.
func ValidateWebURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) {
		return fmt.Errorf("%w: %q", ErrInvalidURL, rawURL)
	}
	return nil
}
