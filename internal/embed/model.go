// Package embed is ketch's text embedding model: which models ketch
// installs and where (model.go, install.go), and the in-process runtime
// that turns passages and questions into vectors (embed.go), on
// github.com/rostamlabs/rembed in pure Go.
//
// The model is optional. Nothing here is loaded until a caller embeds, and
// a missing model is reported (Locate), never downloaded behind the
// caller's back: `ketch model install` is the only thing that fetches one.
package embed

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// File is one file of a model, pinned by size and SHA-256.
type File struct {
	Path   string // relative to the model directory, slash-separated
	Size   int64
	SHA256 string
}

// Spec is a model ketch knows how to install.
type Spec struct {
	// Name is the directory the model installs into and the short name
	// ketch shows; ID is its Hugging Face repository, recorded with every
	// vector so a change of model invalidates them.
	Name, ID string
	// Revision is the Hugging Face commit the files are downloaded from,
	// so the pinned hashes always match.
	Revision string
	Dim      int
	// QueryPrefix is prepended to questions, never to passages: bge's
	// retrieval instruction.
	QueryPrefix string
	Files       []File
}

// bgeQueryPrefix is the BGE v1.5 instruction for short queries that
// retrieve passages.
const bgeQueryPrefix = "Represent this sentence for searching relevant passages: "

// Base is bge-base-en-v1.5, the default model (768 dimensions).
var Base = Spec{
	Name: "bge-base-en-v1.5", ID: "BAAI/bge-base-en-v1.5", Revision: "a5beb1e3e68b9ab74eb54cfd186867f64f240e1a",
	Dim: 768, QueryPrefix: bgeQueryPrefix,
	Files: []File{
		{"config.json", 777, "bc00af31a4a31b74040d73370aa83b62da34c90b75eb77bfa7db039d90abd591"},
		{"modules.json", 349, "84e40c8e006c9b1d6c122e02cba9b02458120b5fb0c87b746c41e0207cf642cf"},
		{"1_Pooling/config.json", 190, "c9bef85e8bbf4b2eab4941b3fb62bd33f88686748b478f2e264d256472d9643b"},
		{"tokenizer_config.json", 366, "9261e7d79b44c8195c1cada2b453e55b00aeb81e907a6664974b4d7776172ab3"},
		{"vocab.txt", 231508, "07eced375cec144d27c900241f3e339478dec958f92fddbc551f295c992038a3"},
		{"model.safetensors", 437955512, "c7c1988aae201f80cf91a5dbbd5866409503b89dcaba877ca6dba7dd0a5167d7"},
	},
}

// Small is bge-small-en-v1.5 (384 dimensions), for `ketch model install
// --small`: a third of the download, a few points lower on retrieval.
var Small = Spec{
	Name: "bge-small-en-v1.5", ID: "BAAI/bge-small-en-v1.5", Revision: "5c38ec7c405ec4b44b94cc5a9bb96e735b38267a",
	Dim: 384, QueryPrefix: bgeQueryPrefix,
	Files: []File{
		{"config.json", 743, "094f8e891b932f2000c92cfc663bac4c62069f5d8af5b5278c4306aef3084750"},
		{"modules.json", 349, "84e40c8e006c9b1d6c122e02cba9b02458120b5fb0c87b746c41e0207cf642cf"},
		{"1_Pooling/config.json", 190, "d1caf60c96f5fba2157c0c26b76d80818fad6cf0b8eb5e73ec372ff9818eba5c"},
		{"tokenizer_config.json", 366, "9261e7d79b44c8195c1cada2b453e55b00aeb81e907a6664974b4d7776172ab3"},
		{"vocab.txt", 231508, "07eced375cec144d27c900241f3e339478dec958f92fddbc551f295c992038a3"},
		{"model.safetensors", 133466304, "3c9f31665447c8911517620762200d2245a2518d6e7208acc78cd9db317e21ad"},
	},
}

// Specs lists the installable models, the default first.
func Specs() []Spec { return []Spec{Base, Small} }

// Size is the model's total download size in bytes.
func (s Spec) Size() int64 {
	var n int64
	for _, f := range s.Files {
		n += f.Size
	}
	return n
}

// URL is where f is downloaded from: the pinned revision on the Hub.
func (s Spec) URL(f File) string {
	return "https://huggingface.co/" + s.ID + "/resolve/" + s.Revision + "/" + f.Path
}

// ModelsDir is where ketch installs models: ketch's cache directory, next
// to the page cache and the browser.
func ModelsDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cache dir: %w", err)
	}
	return filepath.Join(base, "ketch", "models"), nil
}

// DefaultDir is where s installs.
func DefaultDir(s Spec) (string, error) {
	dir, err := ModelsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, s.Name), nil
}

// markerFile names the model a directory holds. Install writes it last,
// so a directory with one is a finished install.
const markerFile = "ketch-model.json"

// marker is markerFile's content.
type marker struct {
	ID          string `json:"id"`
	Revision    string `json:"revision,omitempty"`
	Dim         int    `json:"dim,omitempty"`
	QueryPrefix string `json:"query_prefix"`
}

// Model is a model directory ready to load: where it is and what it is.
type Model struct {
	Dir string
	// ID identifies the model in stored vectors: the Hub repository for a
	// model ketch installed (or one whose directory carries ketch's
	// marker), else "local:" and the directory's name.
	ID          string
	Dim         int // 0 when unknown until loaded
	QueryPrefix string
}

// ErrNotInstalled reports that no embedding model is installed (and none
// is configured with model_path).
var ErrNotInstalled = errors.New("no embedding model installed (run: ketch model install)")

// Locate finds the model to use, without loading or downloading it:
// modelPath when set (a directory holding a BERT-family sentence
// transformer, as ketch installs one or as cloned from the Hub), else the
// installed bge-base, else the installed bge-small. A directory without
// ketch's marker is taken as a bge model (the query prefix bge needs)
// named by its directory. ErrNotInstalled when there is none; another
// error when modelPath is set but is not a model directory.
func Locate(modelPath string) (Model, error) {
	if modelPath != "" {
		return describe(modelPath)
	}
	for _, s := range Specs() {
		dir, err := DefaultDir(s)
		if err != nil {
			return Model{}, err
		}
		if st := Check(s, dir); st.Installed {
			return Model{Dir: dir, ID: s.ID, Dim: s.Dim, QueryPrefix: s.QueryPrefix}, nil
		}
	}
	return Model{}, ErrNotInstalled
}

// describe reads a configured model directory.
func describe(dir string) (Model, error) {
	if fi, err := os.Stat(filepath.Join(dir, "config.json")); err != nil || fi.IsDir() {
		return Model{}, fmt.Errorf("model_path %s is not a model directory (no config.json): run ketch model install, or point model_path at one", dir)
	}
	var m marker
	if b, err := os.ReadFile(filepath.Join(dir, markerFile)); err == nil && json.Unmarshal(b, &m) == nil && m.ID != "" {
		return Model{Dir: dir, ID: m.ID, Dim: m.Dim, QueryPrefix: m.QueryPrefix}, nil
	}
	return Model{Dir: dir, ID: "local:" + filepath.Base(filepath.Clean(dir)), QueryPrefix: bgeQueryPrefix}, nil
}

// Status is what Check found in a model directory.
type Status struct {
	Spec      Spec
	Dir       string
	Installed bool   // every file present at its pinned size, marker written
	Problem   string // why not, when not
}

// Check reports whether s is installed in dir, by the marker and each
// file's size: cheap enough for doctor and every query. Hashes are
// verified when the files are downloaded (Install) and by Verify.
func Check(s Spec, dir string) Status {
	st := Status{Spec: s, Dir: dir}
	b, err := os.ReadFile(filepath.Join(dir, markerFile))
	if err != nil {
		st.Problem = "not installed"
		return st
	}
	var m marker
	if json.Unmarshal(b, &m) != nil || m.ID != s.ID || m.Revision != s.Revision {
		st.Problem = "installed files are another model or revision (reinstall)"
		return st
	}
	for _, f := range s.Files {
		fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f.Path)))
		switch {
		case err != nil:
			st.Problem = f.Path + " is missing (reinstall)"
			return st
		case fi.Size() != f.Size:
			st.Problem = fmt.Sprintf("%s is %d bytes, want %d (reinstall)", f.Path, fi.Size(), f.Size)
			return st
		}
	}
	st.Installed = true
	return st
}
