package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type liveResult struct {
	ID            string  `json:"id"`
	URL           string  `json:"url"`
	FinalURL      string  `json:"final_url,omitempty"`
	Status        int     `json:"status"`
	FetchMS       float64 `json:"fetch_ms"`
	Bytes         int     `json:"bytes"`
	SHA           string  `json:"html_sha256,omitempty"`
	SourceChanged bool    `json:"source_changed"`
	Error         string  `json:"error,omitempty"`
}

func fetchLive(ctx context.Context, client *http.Client, page pageSpec, dir string) liveResult {
	r := liveResult{ID: page.ID, URL: page.URL}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page.URL, nil)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	req.Header.Set("User-Agent", "ketch-bench/1.0 (extraction fixture capture)")
	resp, err := client.Do(req)
	if err != nil {
		r.Error = err.Error()
		r.FetchMS = float64(time.Since(start)) / float64(time.Millisecond)
		return r
	}
	defer resp.Body.Close()
	r.Status, r.FinalURL = resp.StatusCode, resp.Request.URL.String()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxInputBytes+1))
	r.FetchMS, r.Bytes = float64(time.Since(start))/float64(time.Millisecond), len(data)
	if err := validateResponse(resp, data, err); err != nil {
		r.Error = err.Error()
		return r
	}
	r.SHA, r.SourceChanged = digest(data), digest(data) != page.HTMLSHA
	if err := writeSnapshot(filepath.Join(dir, page.ID+".html.gz"), data); err != nil {
		r.Error = err.Error()
	}
	return r
}

func validateResponse(resp *http.Response, data []byte, err error) error {
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if len(data) > maxInputBytes {
		return fmt.Errorf("response exceeds %d bytes", maxInputBytes)
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") {
		return fmt.Errorf("expected HTML, got %q", resp.Header.Get("Content-Type"))
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("empty response")
	}
	return nil
}

func writeSnapshot(path string, data []byte) error {
	var packed bytes.Buffer
	w := gzip.NewWriter(&packed)
	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, packed.Bytes(), 0o644)
}

func runLive(ctx context.Context, fixtures []fixture, opts options) error {
	if err := protectSnapshots(opts); err != nil {
		return err
	}
	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return err
	}
	client := &http.Client{Timeout: opts.Timeout}
	results := make([]liveResult, 0, len(fixtures))
	failed := 0
	for _, f := range fixtures {
		r := fetchLive(ctx, client, f.Spec, opts.Out)
		if r.Error != "" {
			failed++
		}
		results = append(results, r)
		fmt.Fprintf(os.Stderr, "%s: HTTP %d, %.1fms, drift=%t, error=%s\n", r.ID, r.Status, r.FetchMS, r.SourceChanged, r.Error)
	}
	result := struct {
		Timestamp string       `json:"timestamp"`
		Note      string       `json:"note"`
		Pages     []liveResult `json:"pages"`
	}{time.Now().UTC().Format(time.RFC3339), "Live HTTP fetch timings only. Changed source requires annotation review; pinned fixtures, references and baseline were not modified.", results}
	if err := writeJSON(filepath.Join(opts.Out, "live.json"), result); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d/%d live fetches failed; see live.json", failed, len(fixtures))
	}
	return nil
}

func protectSnapshots(opts options) error {
	if opts.Dir == "" { // In-memory HTTP fixtures used by tests.
		return nil
	}
	// Resolve existing paths too, so a symlink cannot make a candidate capture
	// overwrite the frozen source while still claiming to preserve it.
	source, sourceErr := os.Stat(filepath.Join(opts.Dir, "testdata"))
	target, targetErr := os.Stat(opts.Out)
	if sourceErr != nil {
		return sourceErr
	}
	if targetErr == nil && os.SameFile(source, target) {
		return fmt.Errorf("live output must differ from the pinned testdata directory")
	}
	return nil
}
