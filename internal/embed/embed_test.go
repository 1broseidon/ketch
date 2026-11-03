package embed

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
)

// golden is testdata/golden.json: short docs sections and questions with
// the vectors the Python reference runtime (fastembed) gives them.
type golden struct {
	Models map[string][]struct {
		Text  string `json:"text"`
		Query bool   `json:"query"`
		Vec   string `json:"vec"` // base64 float16
	} `json:"models"`
}

// installedModel returns s's directory when the model is installed (or
// KETCH_TEST_MODEL_DIR names it), else skips: the parity test needs the
// real weights, which are hundreds of megabytes and never in the repo.
func installedModel(t *testing.T, s Spec) Model {
	t.Helper()
	dir := os.Getenv("KETCH_TEST_MODEL_DIR")
	if dir == "" {
		var err error
		if dir, err = DefaultDir(s); err != nil {
			t.Skip(err)
		}
	}
	if st := Check(s, dir); !st.Installed {
		t.Skipf("%s not installed in %s (%s): ketch model install", s.Name, dir, st.Problem)
	}
	return Model{Dir: dir, ID: s.ID, Dim: s.Dim, QueryPrefix: s.QueryPrefix}
}

// The pure-Go runtime gives the reference's vectors: passages as they are,
// questions with bge's query prefix, cosine >= 0.999 for every text.
func TestEmbedParityWithReference(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	for _, s := range Specs() {
		t.Run(s.Name, func(t *testing.T) { checkParity(t, s, g) })
	}
}

// checkParity embeds s's golden texts and compares each with its
// reference vector.
func checkParity(t *testing.T, s Spec, g golden) {
	m := installedModel(t, s)
	e := Open(m)
	items := g.Models[s.Name]
	if len(items) < 24 {
		t.Fatalf("golden has %d texts for %s", len(items), s.Name)
	}
	var passages []string
	var want [][]float32
	for _, it := range items {
		raw, err := base64.StdEncoding.DecodeString(it.Vec)
		if err != nil {
			t.Fatal(err)
		}
		ref := normalize(FromHalf(nil, raw))
		if !it.Query {
			passages = append(passages, it.Text)
			want = append(want, ref)
			continue
		}
		got, err := e.EmbedQuery(context.Background(), it.Text)
		if err != nil {
			t.Fatal(err)
		}
		if c := Dot(got, ref); c < 0.999 {
			t.Errorf("query %q: cosine %.5f with the reference", it.Text, c)
		}
	}
	got, err := e.EmbedPassages(context.Background(), passages)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		if len(got[i]) != s.Dim {
			t.Fatalf("dim %d, want %d", len(got[i]), s.Dim)
		}
		if c := Dot(got[i], want[i]); c < 0.999 {
			t.Errorf("passage %q: cosine %.5f with the reference", passages[i][:min(60, len(passages[i]))], c)
		}
	}
	if e.Dim() != s.Dim || e.ID() != s.ID {
		t.Errorf("Dim/ID = %d %q", e.Dim(), e.ID())
	}
}

// Float16 storage keeps a unit vector's cosine with itself within 1e-3,
// rounds to nearest, and decodes the special values exactly.
func TestHalfRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 50 {
		v := make([]float32, 768)
		for i := range v {
			v[i] = float32(r.NormFloat64())
		}
		v = normalize(v)
		back := FromHalf(nil, AppendHalf(nil, v))
		if c := Dot(v, back); math.Abs(float64(c)-1) > 1e-3 {
			t.Fatalf("cosine after float16 = %.6f", c)
		}
	}
	for _, x := range []float32{0, 1, -1, 0.5, 65504, 1e-7, 6.1035156e-05, -2.5} {
		got := fromHalf(toHalf(x))
		if math.Abs(float64(got-x)) > math.Abs(float64(x))/1024+6e-8 {
			t.Errorf("%g -> %g", x, got)
		}
	}
	if !math.IsInf(float64(fromHalf(toHalf(1e6))), 1) || !math.IsNaN(float64(fromHalf(toHalf(float32(math.NaN()))))) {
		t.Error("overflow must be +Inf and NaN must stay NaN")
	}
	if toHalf(1+1.0/2048) != toHalf(1) || toHalf(1+3.0/2048) != toHalf(1+4.0/2048) {
		t.Error("ties must round to even")
	}
}

// Locate prefers a configured model_path, reads ketch's marker for its
// identity, and reports a path that is not a model.
func TestLocateModelPath(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if _, err := Locate(""); err != ErrNotInstalled {
		t.Fatalf("nothing installed: %v", err)
	}
	dir := t.TempDir()
	if _, err := Locate(dir); err == nil {
		t.Fatal("a directory without config.json is not a model")
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Locate(dir)
	if err != nil || m.ID != "local:"+filepath.Base(dir) || m.QueryPrefix != bgeQueryPrefix {
		t.Fatalf("unmarked dir = %+v, %v", m, err)
	}
	if err := os.WriteFile(filepath.Join(dir, markerFile), []byte(`{"id":"org/model","dim":8,"query_prefix":"q: "}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if m, err := Locate(dir); err != nil || m.ID != "org/model" || m.Dim != 8 || m.QueryPrefix != "q: " {
		t.Fatalf("marked dir = %+v, %v", m, err)
	}
}
