package docs

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fakeLocal is the local index as the auto chain sees it: one canned
// outcome for every call, and the calls it got.
type fakeLocal struct {
	o     Outcome
	mu    sync.Mutex
	calls []string
}

func (f *fakeLocal) SearchSelect(_ context.Context, query string, _ int) Outcome {
	f.mu.Lock()
	f.calls = append(f.calls, "search "+query)
	f.mu.Unlock()
	return f.o
}

func (f *fakeLocal) GetDocsSelect(_ context.Context, lib, query string, _ int) Outcome {
	f.mu.Lock()
	f.calls = append(f.calls, "docs "+lib+" "+query)
	f.mu.Unlock()
	return f.o
}

var localAnswer = []Result{{Library: "https://hono.dev/docs/", Title: "Middleware", URL: "https://hono.dev/docs/guides/middleware", Source: "local"}}

func notIndexed() Outcome {
	return Answer("local", nil, fmt.Errorf("local: %w", ErrNotIndexed))
}

// The local index answers first when it holds the library; web and
// Context7 are never asked.
func TestAutoLocalAnswersFirst(t *testing.T) {
	web := &fakeAutoWeb{results: webAnswer}
	a, calls := autoWith(t, web, "/honojs/hono")
	local := &fakeLocal{o: Answer("local", localAnswer, nil)}
	a.WithLocal(local)
	for name, o := range map[string]Outcome{
		"search": a.SearchSelect(context.Background(), "hono middleware", 5),
		"docs":   a.GetDocsSelect(context.Background(), "hono", "middleware", 0),
	} {
		if o.Status != Answered || o.Backend != "local" || len(o.Results) != 1 || len(o.Attempts) != 1 || o.Attempts[0].Backend != "local" {
			t.Errorf("%s: outcome %+v", name, o)
		}
	}
	if len(web.calls) != 0 || len(calls.list()) != 0 {
		t.Errorf("web %v and Context7 %v asked after the local index answered", web.calls, calls.list())
	}
}

// A library the index does not hold is no attempt at all: the chain runs
// as it did without the index, and its outcome lists web (and Context7)
// alone. A Context7 ID never asks the index.
func TestAutoLocalNotIndexedIsNoAttempt(t *testing.T) {
	web := &fakeAutoWeb{results: webAnswer}
	a, _ := autoWith(t, web, "/facebook/react")
	local := &fakeLocal{o: notIndexed()}
	a.WithLocal(local)
	o := a.SearchSelect(context.Background(), "react useState", 5)
	if o.Status != Answered || o.Backend != "web" || len(o.Attempts) != 1 || o.Attempts[0].Backend != "web" {
		t.Errorf("outcome %+v, want web's answer with web's attempt alone", o)
	}
	o = a.GetDocsSelect(context.Background(), "/facebook/react", "useState", 0)
	if o.Backend != "context7" || len(local.calls) != 1 {
		t.Errorf("a Context7 ID: backend %q, local calls %v", o.Backend, local.calls)
	}
}

// When the index holds the library but nothing matched (or it could not
// be read), web answers and the local attempt is kept before web's; when
// web refuses too, Context7 follows and every attempt is listed in order.
func TestAutoLocalRefusalFallsThrough(t *testing.T) {
	refused := Answer("local", nil, fmt.Errorf("local: nothing matched: %w", ErrNoResults))
	web := &fakeAutoWeb{results: webAnswer}
	a, _ := autoWith(t, web, "/facebook/react")
	a.WithLocal(&fakeLocal{o: refused})
	o := a.GetDocsSelect(context.Background(), "react", "useState", 0)
	if o.Backend != "web" || len(o.Attempts) != 2 || o.Attempts[0].Backend != "local" || o.Attempts[0].Reason != "no_results" || o.Attempts[1].Status != Answered {
		t.Errorf("outcome %+v, want web's answer after local's refusal", o)
	}
	web = &fakeAutoWeb{err: fmt.Errorf("web: %w", ErrNotFound)}
	a, _ = autoWith(t, web, "/zz/zzlib")
	a.WithLocal(&fakeLocal{o: refused})
	o = a.SearchSelect(context.Background(), "zzlib hooks", 5)
	var order []string
	for _, at := range o.Attempts {
		order = append(order, at.Backend+":"+string(at.Status))
	}
	if o.Backend != "context7" || strings.Join(order, ",") != "local:refused,web:refused,context7:answered" {
		t.Errorf("attempts %v (backend %q)", order, o.Backend)
	}
}
