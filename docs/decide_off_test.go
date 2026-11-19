package docs

import (
	"context"
	"errors"
	"testing"

	"github.com/1broseidon/ketch/decide"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// operatorConfig is a config with a TypeSafe key and docs_decisions set to
// setting.
func operatorConfig(setting string) config.Config {
	c := config.Defaults()
	c.SetProvider("typesafe_api_key", "fixture")
	c.DocsDecisions = setting
	return c
}

// An operator's off is final: no per-call value but off turns decisions
// on. Under auto or a provider name, a per-call off turns them off and a
// per-call provider name selects among the providers.
func TestDecisionsOperatorOffIsFinal(t *testing.T) {
	for _, tc := range []struct {
		operator, call string
		want           string // provider ID, "" for none, "off" for errDecisionsOff
	}{
		{"off", "typesafe", "off"},
		{"off", "auto", "off"},
		{"off", "off", ""},
		{"off", "", ""},
		{"", "off", ""},
		{"", "typesafe", "typesafe"},
		{"", "", "typesafe"},
		{"typesafe", "off", ""},
		{"openai", "typesafe", "typesafe"},
	} {
		p, _, ok, err := decisionsFrom(operatorConfig(tc.operator))(tc.call)
		got := ""
		switch {
		case errors.Is(err, errDecisionsOff):
			got = "off"
		case err != nil:
			t.Errorf("operator %q, call %q: %v", tc.operator, tc.call, err)
			continue
		case ok:
			got = p.ID
		}
		if got != tc.want {
			t.Errorf("operator %q, call %q: got %q, want %q", tc.operator, tc.call, got, tc.want)
		}
	}
}

// A call that asks for a provider when the operator set off gets the plain
// answer with one line in its note saying so; nothing is asked.
func TestDecisionsOperatorOffNote(t *testing.T) {
	f, hits := decidedSite()
	head, _, _ := searchFirstOver(t, f, hits)
	plain := head.SearchSelect(context.Background(), widgetQuestion, 0)
	s, _, _ := searchFirstOver(t, f, hits)
	s.decisions = decisionsFrom(operatorConfig(decide.Off))
	ctx := WithLocalOptions(context.Background(), LocalOptions{Decisions: "typesafe"})
	o := s.SearchSelect(ctx, widgetQuestion, 0)
	if o.Decisions != nil {
		t.Fatalf("decisions %+v under the operator's off", o.Decisions)
	}
	want := joinNote(plain.Note, "decisions are off in the operator's config (docs_decisions)")
	if o.Note != want {
		t.Errorf("note %q, want %q", o.Note, want)
	}
	o.Note = plain.Note
	if renderAll(widgetQuestion, o) != renderAll(widgetQuestion, plain) {
		t.Errorf("the answer differs from the plain one")
	}
}
