package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeSpec serves a model's files from a test server.
func fakeSpec(t *testing.T, files map[string]string) (Spec, *atomic.Int32, func(string) string) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if ua := r.Header.Get("User-Agent"); ua != "ketch-test" {
			t.Errorf("User-Agent = %q", ua)
		}
		name := r.URL.Path[strings.Index(r.URL.Path, "/rev/")+len("/rev/"):]
		body, ok := files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	s := Spec{Name: "fake", ID: "org/fake", Revision: "rev", Dim: 4, QueryPrefix: "q: "}
	for _, p := range []string{"config.json", "1_Pooling/config.json", "model.safetensors"} {
		sum := sha256.Sum256([]byte(files[p]))
		s.Files = append(s.Files, File{Path: p, Size: int64(len(files[p])), SHA256: hex.EncodeToString(sum[:])})
	}
	return s, &hits, func(u string) string { return srv.URL + "/" + u }
}

// Install downloads every file, verifies it, and writes the marker last; a
// second run fetches nothing; a corrupted file is fetched again; a file
// whose hash does not match its pin fails the install and leaves no
// marker.
func TestInstallVerifiesAndResumes(t *testing.T) {
	files := map[string]string{"config.json": `{"hidden_size":4}`, "1_Pooling/config.json": `{"pooling_mode_cls_token":true}`, "model.safetensors": strings.Repeat("w", 5000)}
	s, hits, base := fakeSpec(t, files)
	client := &http.Client{Transport: rewrite{base}}
	dir := filepath.Join(t.TempDir(), "fake")
	if err := Install(context.Background(), client, "ketch-test", s, dir, nil); err != nil {
		t.Fatal(err)
	}
	if st := Check(s, dir); !st.Installed {
		t.Fatalf("Check after install: %+v", st)
	}
	if hits.Load() != 3 {
		t.Errorf("%d downloads, want 3", hits.Load())
	}
	if err := Install(context.Background(), client, "ketch-test", s, dir, nil); err != nil || hits.Load() != 3 {
		t.Errorf("re-install downloaded again (%d) or failed: %v", hits.Load(), err)
	}
	if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), []byte(strings.Repeat("x", 5000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(s, dir); err == nil {
		t.Error("Verify passed a corrupted file")
	}
	if err := Install(context.Background(), client, "ketch-test", s, dir, nil); err != nil || hits.Load() != 4 {
		t.Errorf("a corrupted file must be fetched again (%d downloads): %v", hits.Load(), err)
	}
	s.Files[2].SHA256 = strings.Repeat("0", 64)
	if err := os.Remove(filepath.Join(dir, "model.safetensors")); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), client, "ketch-test", s, dir, nil); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("a hash mismatch must fail: %v", err)
	}
	if st := Check(s, dir); st.Installed {
		t.Error("a failed install must not look installed")
	}
	if _, err := os.Stat(filepath.Join(dir, "model.safetensors.part")); !os.IsNotExist(err) {
		t.Error("a failed download must not leave its .part file")
	}
}

// rewrite sends the Hub URLs a Spec builds to the test server.
type rewrite struct{ base func(string) string }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u := r.base(strings.TrimPrefix(req.URL.Path, "/org/fake/resolve/"))
	out := req.Clone(req.Context())
	parsed, err := req.URL.Parse(u)
	if err != nil {
		return nil, err
	}
	out.URL, out.Host = parsed, parsed.Host
	return http.DefaultTransport.RoundTrip(out)
}
