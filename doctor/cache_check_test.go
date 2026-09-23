package doctor

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/ketch/cache"
	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/1broseidon/ketch/scrape"
	bolt "go.etcd.io/bbolt"
)

// The cache check must tell a healthy cache, a busy one and an unreadable one
// apart. The read-only store always constructs, so the read error is the only
// signal left; losing it reported "0 entries" for a corrupt cache.
func TestCheckCacheReadStates(t *testing.T) {
	t.Run("healthy cache reports entries", func(t *testing.T) {
		testutil.SetIsolatedConfigHome(t)
		c := cache.New(time.Hour)
		c.Put("https://example.test/", &scrape.Page{URL: "https://example.test/", Markdown: "x"}, scrape.SourceHTTP)
		status, detail := checkCache()
		if status != StatusOK || !strings.HasPrefix(detail, "1 entries") {
			t.Fatalf("status = %q, detail = %q", status, detail)
		}
	})

	t.Run("cache held elsewhere is busy, not broken", func(t *testing.T) {
		testutil.SetIsolatedConfigHome(t)
		path, err := cache.DBPath()
		if err != nil {
			t.Fatal(err)
		}
		holder, err := bolt.Open(path, 0o600, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer holder.Close()
		status, detail := checkCache()
		if status != StatusOK || !strings.Contains(detail, "busy") {
			t.Fatalf("status = %q, detail = %q", status, detail)
		}
	})

	t.Run("unreadable cache is misconfigured", func(t *testing.T) {
		testutil.SetIsolatedConfigHome(t)
		path, err := cache.DBPath()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Repeat("not a bbolt file ", 512)), 0o600); err != nil {
			t.Fatal(err)
		}
		status, detail := checkCache()
		if status != StatusMisconfigured || !strings.Contains(detail, "unreadable") {
			t.Fatalf("status = %q, detail = %q", status, detail)
		}
	})
}
