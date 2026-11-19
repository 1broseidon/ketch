package decide

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// Provider owns the wiring, cut-offs and health policy of one decision
// provider.
type Provider struct {
	ID    string
	Name  string
	Model string
	// Setup says what to configure when Usable is false.
	Setup string
	// Rank orders the configured providers for the "auto" setting: lower
	// is chosen first.
	Rank     int
	Cutoffs  Cutoffs
	Settings []config.Setting
	Usable   func(*config.Config) bool
	New      func(*config.Config) (Decider, error)
	Probe    func(context.Context, *http.Client, *config.Config) (health.Status, string)
}

// providers are the decision providers, in presentation order.
var providers = []Provider{
	typesafeProvider(),
	openaiProvider(),
}

// The docs_decisions values that are not a provider.
const (
	// Auto uses the best-ranked provider with a key configured, none
	// without one.
	Auto = "auto"
	// Off never asks a provider.
	Off = "off"
)

// Providers returns the descriptors in their stable presentation order.
func Providers() []Provider {
	snapshot := slices.Clone(providers)
	for i := range snapshot {
		snapshot[i].Settings = slices.Clone(snapshot[i].Settings)
	}
	return snapshot
}

// IDs returns the provider IDs in registry order.
func IDs() []string {
	out := make([]string, len(providers))
	for i, p := range providers {
		out[i] = p.ID
	}
	return out
}

// Values returns every value docs_decisions and --decisions accept.
func Values() []string { return append([]string{Auto, Off}, IDs()...) }

// Lookup finds a provider without constructing it.
func Lookup(id string) (Provider, bool) {
	for _, p := range providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// Normalize checks a docs_decisions value: "" is auto; a value that is
// neither auto, off nor a provider ID is an error naming the valid ones.
func Normalize(value string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return Auto, nil
	}
	if slices.Contains(Values(), v) {
		return v, nil
	}
	return "", fmt.Errorf("unknown decisions value %q (valid: %s)", value, strings.Join(Values(), ", "))
}

// Configured reports credentials explicitly set for the provider.
func (p Provider) Configured(cfg *config.Config) bool {
	for _, s := range p.Settings {
		if s.Configured(cfg) {
			return true
		}
	}
	return false
}

// Required reports whether a failing health check must fail doctor: the
// provider is named by docs_decisions or has a key set.
func (p Provider) Required(cfg *config.Config) bool {
	return cfg.DocsDecisions == p.ID || p.Configured(cfg)
}

// ErrUnavailable is a provider named by setting that cannot be built.
var ErrUnavailable = errors.New("decisions unavailable")

// Choose is the provider setting selects for cfg: setting "" or auto is
// the configured provider of lowest rank (none: ok false, no error); off is
// none; a provider ID is that provider, an error when it is not usable
// (its Setup text, no key material).
func Choose(cfg *config.Config, setting string) (Provider, Decider, bool, error) {
	v, err := Normalize(setting)
	if err != nil {
		return Provider{}, nil, false, err
	}
	switch v {
	case Off:
		return Provider{}, nil, false, nil
	case Auto:
		for _, p := range byRank() {
			if p.Configured(cfg) && p.Usable(cfg) {
				return build(p, cfg)
			}
		}
		return Provider{}, nil, false, nil
	}
	p, _ := Lookup(v)
	if !p.Usable(cfg) {
		return p, nil, false, fmt.Errorf("%w: %s", ErrUnavailable, p.Setup)
	}
	return build(p, cfg)
}

func build(p Provider, cfg *config.Config) (Provider, Decider, bool, error) {
	d, err := p.New(cfg)
	if err != nil {
		return p, nil, false, fmt.Errorf("%w: %s: %v", ErrUnavailable, p.ID, err)
	}
	return p, d, true, nil
}

// byRank is the providers by Rank, registry order on ties.
func byRank() []Provider {
	out := slices.Clone(providers)
	slices.SortStableFunc(out, func(a, b Provider) int { return a.Rank - b.Rank })
	return out
}

// SecretKey declares a provider's API key: a secret, redacted in output
// (key_set in ketch config), set from the config file or KETCH_<KEY>, and
// gating doctor when set.
func SecretKey(key string) config.Setting {
	return config.Setting{Key: key, Secret: true, GateDoctor: true, FileOrder: 1000, DiscoveryOrder: 1000, EnvOrder: 1000, ValidationOrder: 1000}
}
