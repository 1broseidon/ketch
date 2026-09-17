package docstore

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestFindProjectManifestWinsOverGit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "pkg", "web")
	if err := os.MkdirAll(filepath.Join(sub, ".ketch"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, ManifestPath), []byte(`{"libraries":[{"name":"tw","url":"https://tw.test/docs"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(sub, "src", "components")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	p, ok, err := FindProject(deep)
	if err != nil || !ok {
		t.Fatalf("FindProject = %v %v %v", p, ok, err)
	}
	if p.Root != sub || !p.Exists || !reflect.DeepEqual(p.Names(), []string{"tw"}) {
		t.Fatalf("project = %+v", p)
	}

	// Above the manifest, only the git root is found, with no manifest yet.
	p, ok, err = FindProject(filepath.Join(root, "pkg"))
	if err != nil || !ok || p.Root != root || p.Exists || len(p.Names()) != 0 {
		t.Fatalf("git-root project = %+v %v %v", p, ok, err)
	}
}

func TestFindProjectNone(t *testing.T) {
	dir := t.TempDir()
	// t.TempDir may sit under a git checkout on some CI hosts; guard by
	// only asserting when no .git is above it.
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			t.Skip("temp dir is inside a git repository")
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	p, ok, err := FindProject(dir)
	if err != nil || ok || p != nil {
		t.Fatalf("FindProject = %v %v %v", p, ok, err)
	}
	if scope, err := ScopeFor(dir); err != nil || scope != nil {
		t.Fatalf("ScopeFor = %v %v", scope, err)
	}
}

func TestAttachDetachAndScope(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, ok, err := FindProject(root)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := p.Attach(Attachment{Name: "zeta", URL: "https://z.test/docs", MaxPages: 100}); err != nil {
		t.Fatal(err)
	}
	if err := p.Attach(Attachment{Name: "alpha", URL: "https://a.test/docs"}); err != nil {
		t.Fatal(err)
	}
	// Re-attaching replaces in place.
	if err := p.Attach(Attachment{Name: "zeta", URL: "https://z.test/docs/v2", Version: "2"}); err != nil {
		t.Fatal(err)
	}
	if !p.Exists {
		t.Fatal("manifest should exist after Attach")
	}

	reloaded, _, err := FindProject(filepath.Join(root, "deep", "er"))
	if err != nil {
		// deep/er doesn't exist; FindProject uses Abs only, so walk from root.
		reloaded, _, err = FindProject(root)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(reloaded.Names(), []string{"alpha", "zeta"}) {
		t.Fatalf("names = %v", reloaded.Names())
	}
	if reloaded.Manifest.Libraries[1].URL != "https://z.test/docs/v2" || reloaded.Manifest.Libraries[1].Version != "2" {
		t.Fatalf("re-attach did not replace: %+v", reloaded.Manifest.Libraries[1])
	}

	scope, err := ScopeFor(root)
	if err != nil || !reflect.DeepEqual(scope, []string{"alpha", "zeta"}) {
		t.Fatalf("ScopeFor = %v %v", scope, err)
	}

	found, err := reloaded.Detach("alpha")
	if err != nil || !found {
		t.Fatalf("Detach = %v %v", found, err)
	}
	found, err = reloaded.Detach("alpha")
	if err != nil || found {
		t.Fatalf("second Detach = %v %v", found, err)
	}
	scope, _ = ScopeFor(root)
	if !reflect.DeepEqual(scope, []string{"zeta"}) {
		t.Fatalf("ScopeFor after detach = %v", scope)
	}
	if _, err := os.Stat(filepath.Join(root, ManifestPath+".tmp")); err == nil {
		t.Error("temp file left behind")
	}
}

func TestAttachRejectsBadName(t *testing.T) {
	root := t.TempDir()
	p := &Project{Root: root, Path: filepath.Join(root, ManifestPath)}
	if err := p.Attach(Attachment{Name: "Bad", URL: "https://x"}); err == nil {
		t.Fatal("bad name accepted")
	}
}

func TestScopeForCorruptManifestIsAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".ketch"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestPath), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ScopeFor(root); err == nil {
		t.Fatal("corrupt manifest silently ignored")
	}
}

func TestDir(t *testing.T) {
	if got, err := Dir("/explicit/path/"); err != nil || got != "/explicit/path" {
		t.Errorf("Dir explicit = %q %v", got, err)
	}
	home, _ := os.UserHomeDir()
	if got, _ := Dir("~/docs"); got != filepath.Join(home, "docs") {
		t.Errorf("Dir tilde = %q", got)
	}
	def, err := Dir("")
	if err != nil || !filepath.IsAbs(def) || filepath.Base(def) != "docs" || filepath.Base(filepath.Dir(def)) != "ketch" {
		t.Errorf("Dir default = %q %v", def, err)
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Setenv("XDG_DATA_HOME", "/xdg")
		if got, _ := DefaultDir(); got != "/xdg/ketch/docs" {
			t.Errorf("XDG default = %q", got)
		}
	}
}
