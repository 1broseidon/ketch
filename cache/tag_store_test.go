package cache

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/1broseidon/ketch/internal/testutil"
	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

func TestTagsPathUsesNativeConfigDirectoryAndExplicitOverride(t *testing.T) {
	dir := testutil.SetIsolatedConfigHome(t)
	t.Setenv("KETCH_TAGS_PATH", "")
	native, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	path, err := TagsPath()
	if err != nil || path != filepath.Join(native, "ketch", "tags.db") {
		t.Fatalf("native path = %q, %v", path, err)
	}
	override := filepath.Join(dir, "portable", "bookmarks.db")
	t.Setenv("KETCH_TAGS_PATH", override)
	path, err = TagsPath()
	if err != nil || path != override {
		t.Fatalf("explicit path = %q, %v", path, err)
	}
	_ = NewTagIndex(time.Hour, nil)
	if _, err := os.Stat(override); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("constructing the lazy index created a database: %v", err)
	}
}

func TestTagIndexRecoversAfterItsOwnFileLockIsReleased(t *testing.T) {
	index := newTestCache(t, time.Hour)
	url := "https://example.test/docs"
	if err := index.TagResult("project", url, url, "Docs", "Project docs"); err != nil {
		t.Fatal(err)
	}
	store := index.store.(*BBoltStore)
	locked, err := bolt.Open(store.tags.path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := index.TagURL("second", url, url)
	closeErr := locked.Close()
	if !errors.Is(writeErr, bolterrors.ErrTimeout) {
		t.Fatalf("contended index write = %v, want timeout", writeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if _, err := index.TagURL("second", url, url); err != nil {
		t.Fatalf("the same index did not recover: %v", err)
	}
	view, err := index.ShowTag("second", 0)
	if err != nil || view.Entries != 1 {
		t.Fatalf("recovered write was not persisted: %+v, %v", view, err)
	}
}

func TestTagIndexDoesNotChangeExistingParentPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses native ACLs")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KETCH_TAGS_PATH", filepath.Join(dir, "tags.db"))
	index := NewTagIndex(time.Hour, nil)
	if _, err := index.TagURL("project", "https://example.test", "https://example.test"); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Stat(dir)
	if err != nil || parent.Mode().Perm() != 0o755 {
		t.Fatalf("existing parent permissions changed: %v, %v", parent, err)
	}
	file, err := os.Stat(filepath.Join(dir, "tags.db"))
	if err != nil || file.Mode().Perm() != 0o600 {
		t.Fatalf("bookmark file is not private: %v, %v", file, err)
	}
}

func TestTagIndexRejectsDirectoryWithoutChangingPermissions(t *testing.T) {
	dir := t.TempDir()
	before, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KETCH_TAGS_PATH", dir)
	if _, err := NewTagIndex(time.Hour, nil).TagURL("project", "https://example.test", "https://example.test"); err == nil {
		t.Fatal("directory accepted as a tag database")
	}
	after, err := os.Stat(dir)
	if err != nil || after.Mode().Perm() != before.Mode().Perm() {
		t.Fatalf("invalid index path changed directory permissions: %v, %v", after, err)
	}
}
