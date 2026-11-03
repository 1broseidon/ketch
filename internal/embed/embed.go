package embed

import (
	"context"
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"sync"

	"github.com/rostamlabs/rembed"
)

// Embedder turns text into unit-length vectors of Dim dimensions. ID names
// the model, so vectors from different models are never compared.
type Embedder interface {
	ID() string
	Dim() int
	// EmbedQuery embeds a question, with the model's query prefix.
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
	// EmbedPassages embeds passages as they are, one vector per text, in
	// order.
	EmbedPassages(ctx context.Context, texts []string) ([][]float32, error)
}

// passageBatch is how many passages go to the runtime per call: it spreads
// one call across GOMAXPROCS texts, so a batch a few times that keeps
// every core busy while ctx is still checked between batches.
const passageBatch = 64

// memoryHeadroom is how far above the loaded model's heap the soft memory
// limit sits (GOMEMLIMIT, when set, wins): room for a batch's scratch
// buffers (about 25 MB per worker at the full 512 tokens) without letting
// garbage from finished batches pile up to gigabytes.
const memoryHeadroom = 768 << 20

// loaded holds the models loaded in this process, by directory: an MCP
// server answering many queries loads its model once.
var loaded sync.Map // dir -> *sync.OnceValues

// Open returns the embedder for m. Nothing is read until the first Embed
// call, so a process that never embeds (a query the index does not hold,
// a keyword fallback) never pays for loading the model.
func Open(m Model) Embedder { return &runtimeEmbedder{m: m} }

// runtimeEmbedder is an Embedder over rembed, loaded lazily.
type runtimeEmbedder struct{ m Model }

func (e *runtimeEmbedder) ID() string { return e.m.ID }

// Dim is the model's dimension: from its marker when known, else from
// loading it (0 if it cannot be loaded).
func (e *runtimeEmbedder) Dim() int {
	if e.m.Dim > 0 {
		return e.m.Dim
	}
	r, err := e.runtime()
	if err != nil {
		return 0
	}
	return r.Dim()
}

func (e *runtimeEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	r, err := e.runtime()
	if err != nil {
		return nil, err
	}
	out, err := r.Embed(ctx, []string{e.m.QueryPrefix + query})
	if err != nil {
		return nil, err
	}
	return normalize(out[0]), nil
}

func (e *runtimeEmbedder) EmbedPassages(ctx context.Context, texts []string) ([][]float32, error) {
	r, err := e.runtime()
	if err != nil {
		return nil, err
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += passageBatch {
		vecs, err := r.Embed(ctx, texts[start:min(start+passageBatch, len(texts))])
		if err != nil {
			return nil, err
		}
		for _, v := range vecs {
			out = append(out, normalize(v))
		}
	}
	return out, nil
}

// runtime loads the model once per process and directory.
func (e *runtimeEmbedder) runtime() (*rembed.Embedder, error) {
	once, _ := loaded.LoadOrStore(e.m.Dir, sync.OnceValues(func() (*rembed.Embedder, error) { return load(e.m.Dir) }))
	return once.(func() (*rembed.Embedder, error))()
}

// load reads a model directory into memory, then collects the garbage the
// load left (the safetensors file is read whole) and, unless GOMEMLIMIT
// is set, caps the heap softly at the model plus memoryHeadroom.
func load(dir string) (*rembed.Embedder, error) {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		// rembed would read a missing path that looks like org/name as a
		// Hugging Face model id and download it; ketch never does.
		return nil, fmt.Errorf("embedding model: %s is not a directory", dir)
	}
	r, err := rembed.Load(dir)
	if err != nil {
		return nil, fmt.Errorf("embedding model: %w", err)
	}
	runtime.GC()
	debug.FreeOSMemory()
	if os.Getenv("GOMEMLIMIT") == "" {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		if limit := int64(ms.HeapAlloc) + memoryHeadroom; limit > 0 && limit < debug.SetMemoryLimit(-1) {
			debug.SetMemoryLimit(limit)
		}
	}
	return r, nil
}

// normalize scales v to unit length in place (the runtime already returns
// unit vectors for bge; this keeps cosine a dot product for any model).
func normalize(v []float32) []float32 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	if s == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(s))
	for i := range v {
		v[i] *= inv
	}
	return v
}

// Dot is the dot product of two vectors of the same length: their cosine
// when both are unit length.
func Dot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}
