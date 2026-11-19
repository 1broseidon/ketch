package docs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/1broseidon/ketch/decide"
)

// The questions are the prototype's, word for word: each request ketch
// sends is compared with the request the prototype sent (recorded under
// testdata/decide), through both providers' request shaping.

// captured is an httptest server that keeps the bodies it is sent and
// answers every question: TypeSafe's shape or OpenAI's.
type captured struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func (c *captured) server(t *testing.T, openai bool) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request is not JSON: %v", err)
		}
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		c.mu.Unlock()
		_ = json.NewEncoder(w).Encode(answerAll(body, openai))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// answerAll answers every question of body: yes/no at 0.5, a choice its
// first option.
func answerAll(body map[string]any, openai bool) map[string]any {
	if openai {
		var answers []any
		for _, q := range body["questions"].([]any) {
			q := q.(map[string]any)
			if q["type"] == "predicate" {
				answers = append(answers, map[string]any{"type": "predicate", "name": q["name"], "probability": 0.5})
				continue
			}
			first := q["choices"].([]any)[0].(map[string]any)["value"]
			answers = append(answers, map[string]any{"type": "choice", "name": q["name"], "choice": first,
				"probabilities": []any{map[string]any{"value": first, "probability": 1}}})
		}
		return map[string]any{"answers": answers, "usage": map[string]any{"input_tokens": 10}}
	}
	answers := map[string]any{}
	for name, q := range body["questions"].(map[string]any) {
		q := q.(map[string]any)
		if q["type"] == "noul" {
			answers[name] = map[string]any{"type": "noul", "noul": 0.5}
			continue
		}
		answers[name] = map[string]any{"type": "choice", "choice": "reference_documentation", "probabilities": map[string]any{"reference_documentation": 1}}
	}
	return map[string]any{"model": "jev-1.14.0", "answers": answers, "usage": map[string]any{"input_tokens": 10}}
}

func fixture(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "decide", name))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func sameJSON(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		g, _ := json.MarshalIndent(got, "", " ")
		w, _ := json.MarshalIndent(want, "", " ")
		t.Errorf("%s differs from the prototype's:\n got %s\nwant %s", what, g, w)
	}
}

// send sends req through a client of the provider to a capturing server
// and returns the body sent.
func send(t *testing.T, openai bool, req decide.Request) map[string]any {
	t.Helper()
	c := &captured{}
	srv := c.server(t, openai)
	var d decide.Decider = decide.NewTypeSafe("test-key", srv.URL)
	if openai {
		d = decide.NewOpenAI("test-key", srv.URL)
	}
	if _, err := d.Decide(context.Background(), req); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if len(c.bodies) != 1 {
		t.Fatalf("%d requests", len(c.bodies))
	}
	return c.bodies[0]
}

func TestDecidedPageQuestionsAsPrototype(t *testing.T) {
	c := &dCall{query: "q"}
	p := &dPage{page: &webPage{URL: "https://x.test/", Title: "X", Markdown: "# X\n\ntext"}}
	for _, openai := range []bool{false, true} {
		body := send(t, openai, c.pageRequest(p))
		name := "typesafe-page-request.json"
		if openai {
			name = "openai-page-request.json"
		}
		want := fixture(t, name)
		sameJSON(t, name+" questions", body["questions"], want["questions"])
		if body["model"] != want["model"] {
			t.Errorf("model %v, want %v", body["model"], want["model"])
		}
	}
}

func TestDecidedPassageQuestionAsPrototype(t *testing.T) {
	want := fixture(t, "typesafe-passage-request.json")
	st := want["state"].(map[string]any)
	page := st["page"].(map[string]any)
	p0 := want["questions"].(map[string]any)["p0"].(map[string]any)
	item := p0["instructions"].(map[string]any)["passage"].(map[string]any)
	req := decide.Request{
		Input: passageInput{DocsQuestion: st["docs_question"].(string), Page: passagePage{Title: page["title"].(string), URL: page["url"].(string)}},
		Questions: []decide.Question{{Name: "p0", Kind: decide.YesNo, Instructions: helpsAsk, TrueMeans: helpsTrue, FalseMeans: helpsFalse,
			Item: &decide.Item{Name: "passage", Value: passageItem{Section: item["section"].(string), Text: item["text"].(string)}}}},
	}
	body := send(t, false, req)
	sameJSON(t, "passage state", body["state"], want["state"])
	sameJSON(t, "passage question", body["questions"], want["questions"])
}

func TestDecidedOtherQuestionsAsPrototype(t *testing.T) {
	for _, openai := range []bool{false, true} {
		name := "typesafe-other-request.json"
		if openai {
			name = "openai-other-request.json"
		}
		want := fixture(t, name)
		var item map[string]any
		if openai {
			q := want["questions"].([]any)[0].(map[string]any)
			item = q["instructions"].([]any)[0].(map[string]any)["data"].(map[string]any)["search_result"].(map[string]any)
		} else {
			q := want["questions"].(map[string]any)["makers_0"].(map[string]any)
			item = q["instructions"].(map[string]any)["search_result"].(map[string]any)
		}
		value := searchResultItem{Title: item["title"].(string), URL: item["url"].(string), Snippet: item["snippet"].(string)}
		it := &decide.Item{Name: "search_result", Value: value}
		var question string
		if openai {
			question = want["input"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["data"].(map[string]any)["docs_question"].(string)
		} else {
			question = want["state"].(map[string]any)["docs_question"].(string)
		}
		req := decide.Request{Input: docsQuestionInput{DocsQuestion: question}, Questions: []decide.Question{
			{Name: "makers_0", Kind: decide.YesNo, Instructions: otherMakersAsk, Item: it, TrueMeans: makersTrue, FalseMeans: makersFalse},
			{Name: "kind_0", Kind: decide.Choice, Instructions: otherKindAsk, Item: it, Options: kindOptions},
		}}
		body := send(t, openai, req)
		sameJSON(t, name+" questions", body["questions"], want["questions"])
		if openai {
			sameJSON(t, name+" input", body["input"], want["input"])
		} else {
			sameJSON(t, name+" state", body["state"], want["state"])
		}
	}
}
