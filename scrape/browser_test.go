package scrape

import (
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/1broseidon/ketch/cookies"
	"github.com/go-rod/rod/lib/launcher"
)

// Source compatibility: NewBrowserConnWithCookies's signature was deliberately
// kept exact (not variadic) so external assignments to the function type keep
// compiling.
var _ func(string, *cookies.Jar) (BrowserConn, error) = NewBrowserConnWithCookies

// browserConnOptions must pass the operator's UA to the browser only when one
// was explicitly configured — both the presence and the value must survive the
// option boundary onto a real launcher.
func TestBrowserConnOptionsFollowConfig(t *testing.T) {
	s := New()
	if opts := s.browserConnOptions(); len(opts) != 0 {
		t.Errorf("unconfigured scraper: browserConnOptions() = %d options, want none", len(opts))
	}

	s.userAgent = "custom/1.2"
	s.userAgentConfigured = true
	opts := s.browserConnOptions()
	if len(opts) != 1 {
		t.Fatalf("configured scraper: browserConnOptions() = %d options, want 1", len(opts))
	}
	l := launcher.New()
	opts[0](l)
	if got := l.Get("user-agent"); got != "custom/1.2" {
		t.Errorf("configured scraper: option set user-agent = %q, want %q", got, "custom/1.2")
	}
}

func TestNewFromConfigUserAgentFlowsToBrowser(t *testing.T) {
	s, err := NewFromConfig(&config.Config{UserAgent: "custom/1.2"})
	if err != nil {
		t.Fatalf("NewFromConfig with user_agent: %v", err)
	}
	if !s.userAgentConfigured {
		t.Error("explicit user_agent: userAgentConfigured = false, want true")
	}
	if s.userAgent != "custom/1.2" {
		t.Errorf("explicit user_agent: userAgent = %q, want %q", s.userAgent, "custom/1.2")
	}
	if opts := s.browserConnOptions(); len(opts) != 1 {
		t.Errorf("explicit user_agent: browserConnOptions() = %d options, want 1", len(opts))
	}

	s2, err := NewFromConfig(&config.Config{})
	if err != nil {
		t.Fatalf("NewFromConfig without user_agent: %v", err)
	}
	if s2.userAgentConfigured {
		t.Error("no user_agent: userAgentConfigured = true, want false")
	}
	if s2.userAgent != DefaultUserAgent() {
		t.Errorf("no user_agent: userAgent = %q, want default %q", s2.userAgent, DefaultUserAgent())
	}
	if opts := s2.browserConnOptions(); len(opts) != 0 {
		t.Errorf("no user_agent: browserConnOptions() = %d options, want none", len(opts))
	}
}

// WithUserAgent must be a no-op for an empty UA so the default launch never
// carries a --user-agent switch.
func TestWithUserAgentEmptyIsNoOp(t *testing.T) {
	l := launcher.New()
	WithUserAgent("")(l)
	if l.Has("user-agent") {
		t.Error("WithUserAgent(\"\"): launcher got a user-agent flag, want none")
	}

	WithUserAgent("custom/1.2")(l)
	if got := l.Get("user-agent"); got != "custom/1.2" {
		t.Errorf("WithUserAgent(\"custom/1.2\"): launcher user-agent = %q, want %q", got, "custom/1.2")
	}
}

// stripHeadlessUA must turn the headless product token into the normal-mode
// one and leave everything else — version, platform, engine tokens — intact,
// and must report no change for UAs that never carried the token.
func TestStripHeadlessUA(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		changed bool
	}{
		{
			name:    "linux headless chromium",
			in:      "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/151.0.0.0 Safari/537.36",
			want:    "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36",
			changed: true,
		},
		{
			name:    "macos headless chrome",
			in:      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/140.0.0.0 Safari/537.36",
			want:    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36",
			changed: true,
		},
		{
			name:    "already normal mode",
			in:      "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36",
			want:    "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36",
			changed: false,
		},
		{
			name:    "non-chromium",
			in:      "Mozilla/5.0 (X11; Linux x86_64; rv:153.0) Gecko/20100101 Firefox/153.0",
			want:    "Mozilla/5.0 (X11; Linux x86_64; rv:153.0) Gecko/20100101 Firefox/153.0",
			changed: false,
		},
		{
			name:    "empty",
			in:      "",
			want:    "",
			changed: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := stripHeadlessUA(tt.in)
			if got != tt.want {
				t.Errorf("stripHeadlessUA(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if changed != tt.changed {
				t.Errorf("stripHeadlessUA(%q) changed = %v, want %v", tt.in, changed, tt.changed)
			}
		})
	}
}

// A configured UA must win over the headless strip: NewBrowserConnOptions
// decides by whether the launcher already carries a user-agent switch, so the
// option must register one the launcher can see.
func TestWithUserAgentMarksLauncher(t *testing.T) {
	l := launcher.New()
	if l.Has("user-agent") {
		t.Fatal("fresh launcher already has user-agent")
	}
	WithUserAgent("")(l)
	if l.Has("user-agent") {
		t.Error("WithUserAgent(\"\") must not set user-agent")
	}
	WithUserAgent("custom/1.2")(l)
	if !l.Has("user-agent") {
		t.Error("WithUserAgent(\"custom/1.2\") did not set user-agent")
	}
}
