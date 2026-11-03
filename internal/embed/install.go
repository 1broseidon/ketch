package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Install downloads s into dir from its pinned revision and verifies every
// file's size and SHA-256. A file already there with the right hash is
// kept, so re-running finishes an interrupted install without fetching
// again. The marker that makes the directory an install (Check) is
// removed first and written last, and each file is written beside its
// final name and renamed into place only once verified, so an interrupted
// install is never taken for a finished one. Progress lines go to
// progress (nil for none). userAgent is sent with every request.
func Install(ctx context.Context, client *http.Client, userAgent string, s Spec, dir string, progress io.Writer) error {
	if progress == nil {
		progress = io.Discard
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("model dir: %w", err)
	}
	if err := os.Remove(filepath.Join(dir, markerFile)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("model dir: %w", err)
	}
	for _, f := range s.Files {
		path := filepath.Join(dir, filepath.FromSlash(f.Path))
		if verifyFile(path, f) == nil {
			_, _ = fmt.Fprintf(progress, "  %s: already downloaded\n", f.Path)
			continue
		}
		if err := download(ctx, client, userAgent, s.URL(f), path, f, progress); err != nil {
			return err
		}
	}
	m, err := json.MarshalIndent(marker{ID: s.ID, Revision: s.Revision, Dim: s.Dim, QueryPrefix: s.QueryPrefix}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, markerFile), append(m, '\n'), 0o644)
}

// Verify re-hashes every file of an installed model.
func Verify(s Spec, dir string) error {
	for _, f := range s.Files {
		if err := verifyFile(filepath.Join(dir, filepath.FromSlash(f.Path)), f); err != nil {
			return err
		}
	}
	return nil
}

// download fetches url into path via path+".part", verifying it first.
func download(ctx context.Context, client *http.Client, userAgent, url, path string, f File, progress io.Writer) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", f.Path, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d from %s", f.Path, resp.StatusCode, url)
	}
	part := path + ".part"
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	h := sha256.New()
	w := &progressWriter{out: progress, name: f.Path, total: f.Size}
	n, err := io.Copy(io.MultiWriter(out, h, w), io.LimitReader(resp.Body, f.Size+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = matches(f, n, hex.EncodeToString(h.Sum(nil)))
	}
	if err != nil {
		_ = os.Remove(part)
		return fmt.Errorf("download %s: %w", f.Path, err)
	}
	return os.Rename(part, path)
}

// verifyFile checks path against f's size and hash.
func verifyFile(path string, f File) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close() //nolint:errcheck // read-only
	if fi, err := fh.Stat(); err != nil || fi.Size() != f.Size {
		return fmt.Errorf("%s: size mismatch", f.Path)
	}
	h := sha256.New()
	n, err := io.Copy(h, fh)
	if err != nil {
		return err
	}
	return matches(f, n, hex.EncodeToString(h.Sum(nil)))
}

// matches compares a file's size and hash with its pin.
func matches(f File, n int64, sum string) error {
	if n != f.Size {
		return fmt.Errorf("%s is %d bytes, want %d", f.Path, n, f.Size)
	}
	if sum != f.SHA256 {
		return fmt.Errorf("%s has SHA-256 %s, want %s", f.Path, sum, f.SHA256)
	}
	return nil
}

// progressWriter reports a large download every tenth of the way.
type progressWriter struct {
	out         io.Writer
	name        string
	total, done int64
	shown       int64
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.total < 8<<20 {
		if p.done >= p.total && p.shown == 0 {
			p.shown = 10
			_, _ = fmt.Fprintf(p.out, "  %s: %s\n", p.name, mib(p.total))
		}
		return len(b), nil
	}
	for tenth := p.done * 10 / p.total; p.shown < tenth && p.shown < 10; {
		p.shown++
		_, _ = fmt.Fprintf(p.out, "  %s: %d%% of %s\n", p.name, p.shown*10, mib(p.total))
	}
	return len(b), nil
}

// mib renders a byte count in (decimal) KB or MB, as the Hub shows sizes.
func mib(n int64) string {
	if n < 1e6 {
		return fmt.Sprintf("%d KB", (n+999)/1000)
	}
	return fmt.Sprintf("%.0f MB", float64(n)/1e6)
}
