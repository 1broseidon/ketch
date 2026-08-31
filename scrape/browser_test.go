package scrape

import (
	"testing"

	"github.com/1broseidon/ketch/config"
	"github.com/go-rod/rod/lib/launcher"
)

// browserConnOptions must pass the operator's UA to the browser only when one
// was explicitly configured — the unconfigured case must stay a no-op so the
// browser keeps its own User-Agent.
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
	if opts[0] == nil {
		t.Fatal("configured scraper: browserConnOptions()[0] = nil, want WithUserAgent")
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
