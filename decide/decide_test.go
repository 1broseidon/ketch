package decide

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1broseidon/ketch/health"
	config "github.com/1broseidon/ketch/internal/configbase"
)

const testKey = "sk-test-secret-0123456789"

// pageQuestions are the shape of the docs page pass: four yes/no questions
// and a choice.
func pageQuestions() []Question {
	yn := func(n string) Question {
		return Question{Name: n, Kind: YesNo, Instructions: "Is `page` " + n + "?", TrueMeans: "yes", FalseMeans: "no"}
	}
	return []Question{yn("documentation"), yn("makers"), yn("names_release"), yn("other_release"),
		{Name: "kind", Kind: Choice, Instructions: "What kind of page is `page`?", Options: []Option{
			{"reference_documentation", "a"}, {"guide_or_tutorial", "b"}, {"blog_post_or_article", "c"}, {"forum_or_qa_thread", "d"},
			{"issue_or_discussion", "e"}, {"source_code", "f"}, {"marketing_or_landing_page", "g"}, {"other", "h"}}}}
}

// serve answers every request with handler, counting them.
func serve(t *testing.T, handler func(n int, w http.ResponseWriter, body map[string]any)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testKey || r.Method != http.MethodPost {
			t.Errorf("request %s with authorization %q", r.Method, r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		handler(int(n.Add(1)), w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func recorded(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fast(p *poster) { p.backoff, p.timeout = time.Millisecond, 2*time.Second }

// A recorded System One response is read into answers: probabilities, the
// choice with every option's probability, and the input tokens.
func TestTypeSafeRecordedResponse(t *testing.T) {
	srv, n := serve(t, func(_ int, w http.ResponseWriter, body map[string]any) {
		qs := body["questions"].(map[string]any)
		if body["model"] != typesafeModel || len(qs) != 5 || body["state"].(map[string]any)["docs_question"] != "q" {
			t.Errorf("request %v", body)
		}
		_, _ = w.Write(recorded(t, "typesafe-page.json"))
	})
	resp, err := NewTypeSafe(testKey, srv.URL).Decide(context.Background(), Request{Input: map[string]string{"docs_question": "q"}, Questions: pageQuestions()})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Answers["documentation"].Probability != 0.99 || resp.Answers["kind"].Choice != "reference_documentation" ||
		resp.Answers["kind"].Probabilities["reference_documentation"] != 1 || resp.InputTokens != 9527 || resp.Requests != 1 || n.Load() != 1 {
		t.Errorf("response %+v", resp)
	}
}

// A recorded Decisions response is read the same way.
func TestOpenAIRecordedResponse(t *testing.T) {
	srv, _ := serve(t, func(_ int, w http.ResponseWriter, body map[string]any) {
		qs := body["questions"].([]any)
		in := body["input"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
		if body["model"] != openaiModel || len(qs) != 5 || in["type"] != "input_json" || in["data"].(map[string]any)["docs_question"] != "q" {
			t.Errorf("request %v", body)
		}
		if ins := qs[0].(map[string]any)["instructions"]; ins != "Is `page` documentation?\n\nTrue if: yes\nFalse if: no" {
			t.Errorf("predicate instructions %q: the meanings are folded in", ins)
		}
		if ch := qs[4].(map[string]any)["choices"].([]any); len(ch) != 8 || ch[0].(map[string]any)["value"] != "reference_documentation" {
			t.Errorf("choices %v", ch)
		}
		_, _ = w.Write(recorded(t, "openai-page.json"))
	})
	resp, err := NewOpenAI(testKey, srv.URL).Decide(context.Background(), Request{Input: map[string]string{"docs_question": "q"}, Questions: pageQuestions()})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Answers["makers"].Probability != 1 || resp.Answers["kind"].Choice != "reference_documentation" || resp.InputTokens != 9756 {
		t.Errorf("response %+v", resp)
	}
}

// An item goes into its question: System One's instructions object holds
// the item then the question; Decisions' instructions are the item as
// input_json and the text.
func TestItemShaping(t *testing.T) {
	q := Question{Name: "p0", Kind: YesNo, Instructions: "Does `passage` help?", TrueMeans: "t", FalseMeans: "f",
		Item: &Item{Name: "passage", Value: struct {
			Section string `json:"section"`
			Text    string `json:"text"`
		}{"A > B", "x < y & z"}}}
	ts, _ := typesafeQuestion(q)
	if want := `{"type":"noul","instructions":{"passage":{"section":"A > B","text":"x < y & z"},"question":"Does ` + "`passage`" + ` help?"},"criteria":{"true":"t","false":"f"}}`; string(ts) != want {
		t.Errorf("typesafe question\n got %s\nwant %s", ts, want)
	}
	oa, _ := openaiQuestion(q)
	if want := `{"type":"predicate","name":"p0","instructions":[{"type":"input_json","data":{"passage":{"section":"A > B","text":"x < y & z"}}},{"type":"input_text","text":"Does ` + "`passage`" + ` help?\n\nTrue if: t\nFalse if: f"}]}`; string(oa) != want {
		t.Errorf("openai question\n got %s\nwant %s", oa, want)
	}
}

// A request larger than System One's request size is split into parts,
// each with the whole state, answered together; a question that does not
// fit the per-question context with the state, or a request needing more
// than maxParts parts, is ErrTooLarge before any request.
func TestTypeSafeSplitsAtLimits(t *testing.T) {
	var mu sync.Mutex
	var sizes []int
	srv, n := serve(t, func(_ int, w http.ResponseWriter, body map[string]any) {
		qs := body["questions"].(map[string]any)
		mu.Lock()
		sizes = append(sizes, len(qs))
		mu.Unlock()
		if body["state"].(map[string]any)["docs_question"] != "q" {
			t.Error("a part lacks the state")
		}
		answers := map[string]any{}
		for k := range qs {
			answers[k] = map[string]any{"type": "noul", "noul": 0.25}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": answers, "usage": map[string]any{"input_tokens": 100}})
	})
	ts := NewTypeSafe(testKey, srv.URL)
	ts.requestTokens = 1000
	text := strings.Repeat("word ", 120) // about 200 estimated tokens a question
	var qs []Question
	for i := range 20 {
		qs = append(qs, Question{Name: fmt.Sprintf("p%d", i), Kind: YesNo, Instructions: text})
	}
	resp, err := ts.Decide(context.Background(), Request{Input: map[string]string{"docs_question": "q"}, Questions: qs})
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, s := range sizes {
		total += s
	}
	if n.Load() < 4 || total != 20 || len(resp.Answers) != 20 || resp.InputTokens != 100*int(n.Load()) || resp.Requests != int(n.Load()) {
		t.Errorf("%d requests of %v questions, %d answers, %d tokens", n.Load(), sizes, len(resp.Answers), resp.InputTokens)
	}
	ts.perQuest = 100
	if _, err := ts.Decide(context.Background(), Request{Input: map[string]string{"docs_question": "q"}, Questions: qs}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a question over the per-question context: %v", err)
	}
	ts.perQuest, ts.requestTokens = typesafeQuestionTokens, 210
	before := n.Load()
	if _, err := ts.Decide(context.Background(), Request{Input: map[string]string{"docs_question": "q"}, Questions: qs}); !errors.Is(err, ErrTooLarge) || n.Load() != before {
		t.Errorf("more than %d parts: %v, %d requests sent", maxParts, err, n.Load()-before)
	}
}

// Decisions takes at most 400 questions a request: more are sent in parts,
// one after another, and answered together.
func TestOpenAISplitsAt400(t *testing.T) {
	var sizes []int
	srv, _ := serve(t, func(_ int, w http.ResponseWriter, body map[string]any) {
		qs := body["questions"].([]any)
		sizes = append(sizes, len(qs))
		var answers []any
		for _, q := range qs {
			answers = append(answers, map[string]any{"type": "predicate", "name": q.(map[string]any)["name"], "probability": 0.75})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": answers, "usage": map[string]any{"input_tokens": 7}})
	})
	var qs []Question
	for i := range 901 {
		qs = append(qs, Question{Name: fmt.Sprintf("p%d", i), Kind: YesNo, Instructions: "x"})
	}
	resp, err := NewOpenAI(testKey, srv.URL).Decide(context.Background(), Request{Input: map[string]string{"q": "q"}, Questions: qs})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sizes, []int{400, 400, 101}) || len(resp.Answers) != 901 || resp.InputTokens != 21 || resp.Requests != 3 {
		t.Errorf("parts %v, %d answers, %d tokens", sizes, len(resp.Answers), resp.InputTokens)
	}
}

// A refusal or a missing answer fails the whole request.
func TestRefusedOrMissingIsAnError(t *testing.T) {
	srv, _ := serve(t, func(_ int, w http.ResponseWriter, _ map[string]any) {
		_, _ = w.Write([]byte(`{"answers":[{"type":"predicate","name":"a","probability":0.9},{"type":"refusal","name":"b"}],"usage":{"input_tokens":5}}`))
	})
	qs := []Question{{Name: "a", Kind: YesNo, Instructions: "x"}, {Name: "b", Kind: YesNo, Instructions: "y"}}
	if _, err := NewOpenAI(testKey, srv.URL).Decide(context.Background(), Request{Input: map[string]string{}, Questions: qs}); !errors.Is(err, ErrRefused) {
		t.Errorf("refusal: %v", err)
	}
	ts, _ := serve(t, func(_ int, w http.ResponseWriter, _ map[string]any) {
		_, _ = w.Write([]byte(`{"answers":{"a":{"type":"noul","noul":0.9}},"usage":{"input_tokens":5}}`))
	})
	if _, err := NewTypeSafe(testKey, ts.URL).Decide(context.Background(), Request{Input: map[string]string{}, Questions: qs}); !errors.Is(err, ErrIncomplete) {
		t.Errorf("missing answer: %v", err)
	}
	bad, _ := serve(t, func(_ int, w http.ResponseWriter, _ map[string]any) {
		_, _ = w.Write([]byte(`{"answers":{"k":{"type":"choice","choice":"nope"}},"usage":{"input_tokens":5}}`))
	})
	choice := []Question{{Name: "k", Kind: Choice, Instructions: "x", Options: []Option{{"a", "A"}}}}
	if _, err := NewTypeSafe(testKey, bad.URL).Decide(context.Background(), Request{Input: map[string]string{}, Questions: choice}); !errors.Is(err, ErrIncomplete) {
		t.Errorf("a choice not offered: %v", err)
	}
}

// Rate-limit and overload statuses are retried; other statuses are final;
// no error names the key, even when the server echoes it.
func TestRetriesAndRedaction(t *testing.T) {
	answer := `{"answers":{"a":{"type":"noul","noul":0.4}},"usage":{"input_tokens":3}}`
	srv, n := serve(t, func(i int, w http.ResponseWriter, _ map[string]any) {
		if i < 3 {
			w.WriteHeader([]int{0, http.StatusTooManyRequests, 529}[i])
			return
		}
		_, _ = w.Write([]byte(answer))
	})
	ts := NewTypeSafe(testKey, srv.URL)
	fast(&ts.post)
	qs := []Question{{Name: "a", Kind: YesNo, Instructions: "x"}}
	if resp, err := ts.Decide(context.Background(), Request{Input: map[string]string{}, Questions: qs}); err != nil || n.Load() != 3 || resp.Answers["a"].Probability != 0.4 {
		t.Errorf("after 429 and 529: %v, %d attempts", err, n.Load())
	}
	denied, m := serve(t, func(int, http.ResponseWriter, map[string]any) {})
	denied.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		m.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key ` + testKey + `"}`))
	})
	oa := NewOpenAI(testKey, denied.URL)
	fast(&oa.post)
	_, err := oa.Decide(context.Background(), Request{Input: map[string]string{}, Questions: qs})
	if err == nil || m.Load() != 1 || strings.Contains(err.Error(), testKey) || !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("401: %v after %d attempts", err, m.Load())
	}
	overloaded, k := serve(t, func(_ int, w http.ResponseWriter, _ map[string]any) { w.WriteHeader(http.StatusServiceUnavailable) })
	oa = NewOpenAI(testKey, overloaded.URL)
	fast(&oa.post)
	if _, err := oa.Decide(context.Background(), Request{Input: map[string]string{}, Questions: qs}); err == nil || k.Load() != attempts {
		t.Errorf("503 every time: %v after %d attempts", err, k.Load())
	}
}

// A timeout is final: not retried.
func TestTimeoutIsFinal(t *testing.T) {
	release := make(chan struct{})
	srv, n := serve(t, func(int, http.ResponseWriter, map[string]any) { <-release })
	defer close(release)
	ts := NewTypeSafe(testKey, srv.URL)
	ts.post.timeout, ts.post.backoff = 50*time.Millisecond, time.Millisecond
	_, err := ts.Decide(context.Background(), Request{Input: map[string]string{}, Questions: []Question{{Name: "a", Kind: YesNo, Instructions: "x"}}})
	if err == nil || !strings.Contains(err.Error(), "no answer within") || n.Load() != 1 {
		t.Errorf("timeout: %v after %d attempts", err, n.Load())
	}
}

func cfgWith(keys map[string]string) *config.Config {
	c := &config.Config{}
	for k, v := range keys {
		c.SetProvider(k, v)
	}
	return c
}

// auto picks the best-ranked provider with a key; none without one; off
// none; a provider named without its key is an error saying what to set.
func TestChoose(t *testing.T) {
	both := cfgWith(map[string]string{typesafeKey: "a", openaiKey: "b"})
	only := cfgWith(map[string]string{openaiKey: "b"})
	cases := []struct {
		name, setting string
		cfg           *config.Config
		want          string // provider ID; "" for none
		err           string // in the error; "" for none
	}{
		{"auto with both keys", "", both, "typesafe", ""},
		{"auto with an OpenAI key", Auto, only, "openai", ""},
		{"auto with no key", Auto, &config.Config{}, "", ""},
		{"off", Off, both, "", ""},
		{"named", "openai", both, "openai", ""},
		{"named without a key", "typesafe", only, "", "ketch config set typesafe_api_key <key>"},
		{"unknown", "gpt", both, "", "valid: auto, off, typesafe, openai"},
	}
	for _, c := range cases {
		p, d, ok, err := Choose(c.cfg, c.setting)
		got := ""
		if ok && d != nil {
			got = p.ID
		}
		if got != c.want || (c.err == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s: %q %v", c.name, got, err)
		}
	}
	if _, _, _, err := Choose(only, "typesafe"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("named without a key: %v", err)
	}
	if v, err := Normalize(" OpenAI "); v != "openai" || err != nil {
		t.Errorf("normalize: %q %v", v, err)
	}
}

// A provider is one descriptor in the list: settings, auto choice by rank
// and lookup follow it, with nothing else to change.
func TestRegistryAddsAProvider(t *testing.T) {
	saved := providers
	t.Cleanup(func() { providers = saved })
	fake := Provider{ID: "fake", Name: "Fake", Rank: 5, Cutoffs: Cutoffs{Low: 0.2, High: 0.8, Official: 0.85, VeryHigh: 0.95},
		Settings: []config.Setting{SecretKey("fake_api_key")},
		Usable:   func(c *config.Config) bool { return c.String("fake_api_key") != "" },
		New:      func(*config.Config) (Decider, error) { return NewTypeSafe("k", ""), nil },
		Probe: func(context.Context, *http.Client, *config.Config) (health.Status, string) {
			return health.StatusOK, ""
		}}
	providers = append(slices.Clone(saved), fake)
	if !slices.Contains(IDs(), "fake") || !slices.Contains(Values(), "fake") {
		t.Errorf("ids %v", IDs())
	}
	c := cfgWith(map[string]string{typesafeKey: "a", "fake_api_key": "b"})
	if p, _, ok, _ := Choose(c, Auto); !ok || p.ID != "fake" || p.Cutoffs.High != 0.8 {
		t.Errorf("auto chose %s: the lowest rank with a key wins", p.ID)
	}
	if !fake.Required(c) || fake.Required(&config.Config{}) {
		t.Error("required follows the key")
	}
	if s := fake.Settings[0]; !s.Secret || !s.GateDoctor || s.Key != "fake_api_key" {
		t.Errorf("setting %+v", s)
	}
}

// The probe reads the model list: no key is no_key, a rejected key
// misconfigured, nothing billed.
func TestProbeModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	for key, want := range map[string]health.Status{"": health.StatusNoKey, "good": health.StatusOK, "bad": health.StatusMisconfigured} {
		if got, detail := probeModels(context.Background(), srv.Client(), srv.URL, key, typesafeKey); got != want || strings.Contains(detail, key) && key != "" {
			t.Errorf("key %q: %s %q", key, got, detail)
		}
	}
}
