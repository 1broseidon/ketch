package scrape

import (
	"fmt"
	"strings"
)

// Version is the ketch version string embedded into DefaultUserAgent.
// cmd sets this from build-time version info at process start; library
// callers that leave it untouched get "dev".
var Version = "dev"

const projectURL = "https://github.com/1broseidon/ketch"

// DefaultUserAgent is the honest HTTP User-Agent ketch sends when no
// operator override is configured. It identifies ketch and points at the
// project; it does not impersonate a browser.
func DefaultUserAgent() string {
	v := Version
	if v == "" {
		v = "dev"
	}
	return fmt.Sprintf("ketch/%s (+%s)", v, projectURL)
}

// normalizeUserAgent returns the User-Agent to send. Empty/whitespace means
// the built-in default. Control characters are rejected — they cannot appear
// in an HTTP header value and usually indicate a mistaken paste.
func normalizeUserAgent(ua string) (string, error) {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return DefaultUserAgent(), nil
	}
	if strings.ContainsAny(ua, "\r\n\x00") {
		return "", fmt.Errorf("user_agent must not contain control characters")
	}
	return ua, nil
}
