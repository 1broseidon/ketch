//go:build bench

package httpx

// The bench record/replay hook. Only builds with `-tags bench` carry this
// file; release builds use bench_off.go, whose benchTransport is the
// identity, and never read these variables. ketch-bench builds ketch with
// the tag to pin the network for `ketch docs`:
//
//	KETCH_HTTP_RECORD=<dir>  every exchange goes to the network and is saved
//	                         in dir: <key>-<seq>.json plus bodies/<sha256>
//	KETCH_HTTP_REPLAY=<path> a directory written by RECORD, or a .tar.gz of
//	                         one; only recorded exchanges are served, and an
//	                         unrecorded request fails with ErrUnrecorded
//	KETCH_HTTP_BODIES=<dir>  extra place to find bodies/<sha256> when
//	                         replaying (a world shared by several cases)
//	KETCH_HTTP_LOG=<file>    one JSON line per request, in any mode
//
// An exchange is keyed by method, URL and the SHA-256 of the request body.
// Identical requests are numbered in the order they start, and replay hands
// recorded responses out in that order; a key requested more often than it
// was recorded gets its last response again, logged as "reused". Transport
// errors (refused connections, timeouts) are recorded and replayed as
// errors. Request headers are never stored.
//
// The hook wraps every client from New and Default and http.DefaultTransport,
// so http.DefaultClient (go-rod's DevTools lookup among others) is covered
// too. A headless browser fetches pages through Chrome's own network stack,
// which no Go transport sees; its DevTools handshake on a loopback port is
// what reaches the hook, and in replay that is an unrecorded request, so a
// browser render fails detectably instead of reaching the live network.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrUnrecorded is the replay error for a request the recorded world lacks.
var ErrUnrecorded = errors.New("ketch bench replay: unrecorded request")

const (
	envRecord = "KETCH_HTTP_RECORD"
	envReplay = "KETCH_HTTP_REPLAY"
	envBodies = "KETCH_HTTP_BODIES"
	envLog    = "KETCH_HTTP_LOG"

	maxRecordBody = 64 << 20
)

func init() {
	http.DefaultTransport = wrapBench(http.DefaultTransport)
}

func benchTransport(t *http.Transport) http.RoundTripper { return wrapBench(t) }

func wrapBench(base http.RoundTripper) http.RoundTripper {
	if _, ok := base.(*benchRT); ok {
		return base
	}
	return &benchRT{base: base}
}

type benchRT struct{ base http.RoundTripper }

type benchSeenKey struct{}

// Exchange is one recorded request and its outcome, as stored in
// <key>-<seq>.json. Status 0 with Error set is a transport failure.
type Exchange struct {
	Method      string      `json:"method"`
	URL         string      `json:"url"`
	BodySHA256  string      `json:"request_body_sha256,omitempty"`
	Seq         int         `json:"seq"`
	Status      int         `json:"status,omitempty"`
	Proto       string      `json:"proto,omitempty"`
	Header      http.Header `json:"header,omitempty"`
	RespSHA256  string      `json:"response_sha256,omitempty"`
	RespBytes   int         `json:"response_bytes"`
	Error       string      `json:"error,omitempty"`
	BodyError   string      `json:"body_error,omitempty"`
	ElapsedMS   float64     `json:"elapsed_ms"`
	RecordedAt  string      `json:"recorded_at"`
	ErrTimeout  bool        `json:"error_timeout,omitempty"`
	ErrCanceled bool        `json:"error_canceled,omitempty"`
}

// ExchangeKey is the key an exchange is stored and replayed under.
func ExchangeKey(method, rawURL string, body []byte) string {
	sum := sha256.Sum256(body)
	h := sha256.Sum256([]byte(strings.ToUpper(method) + " " + rawURL + " " + hex.EncodeToString(sum[:])))
	return hex.EncodeToString(h[:8])
}

type benchState struct {
	err    error
	record string
	replay *replayWorld
	log    *os.File

	mu   sync.Mutex
	next map[string]int // record: next seq per key; replay: requests served per key
}

var benchOnce = sync.OnceValue(loadBench)

func loadBench() *benchState {
	s := &benchState{next: map[string]int{}}
	rec, rep := os.Getenv(envRecord), os.Getenv(envReplay)
	if p := os.Getenv(envLog); p != "" {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			s.err = fmt.Errorf("ketch bench: open %s: %w", envLog, err)
			return s
		}
		s.log = f
	}
	switch {
	case rec != "" && rep != "":
		s.err = fmt.Errorf("ketch bench: set %s or %s, not both", envRecord, envReplay)
	case rec != "":
		if err := os.MkdirAll(filepath.Join(rec, "bodies"), 0o755); err != nil {
			s.err = fmt.Errorf("ketch bench: %s: %w", envRecord, err)
		}
		s.record = rec
	case rep != "":
		w, err := loadWorld(rep, os.Getenv(envBodies))
		if err != nil {
			s.err = fmt.Errorf("ketch bench: %s: %w", envReplay, err)
		}
		s.replay = w
	}
	if s.err != nil {
		s.logLine(map[string]any{"event": "config_error", "error": s.err.Error()})
	}
	return s
}

func (b *benchRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Context().Value(benchSeenKey{}) != nil {
		return b.base.RoundTrip(req)
	}
	s := benchOnce()
	if s.err != nil {
		return nil, s.err
	}
	if s.record == "" && s.replay == nil && s.log == nil {
		return b.base.RoundTrip(req)
	}
	out := req.WithContext(context.WithValue(req.Context(), benchSeenKey{}, true))
	body, err := requestBody(out)
	if err != nil {
		return nil, err
	}
	key := ExchangeKey(req.Method, req.URL.String(), body)
	switch {
	case s.replay != nil:
		return s.serve(out, key)
	case s.record != "":
		return s.recordTrip(b.base, out, key, body)
	}
	start := time.Now()
	resp, err := b.base.RoundTrip(out)
	line := map[string]any{"event": "live", "method": req.Method, "url": req.URL.Redacted(), "ms": since(start)}
	if err != nil {
		line["error"] = err.Error()
	} else {
		line["status"] = resp.StatusCode
	}
	s.logLine(line)
	return resp, err
}

// requestBody reads and restores req's body (req is already a copy).
func requestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	data, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	return data, nil
}

func (s *benchState) recordTrip(base http.RoundTripper, req *http.Request, key string, body []byte) (*http.Response, error) {
	s.mu.Lock()
	seq := s.next[key]
	s.next[key] = seq + 1
	s.mu.Unlock()
	ex := Exchange{Method: req.Method, URL: req.URL.String(), Seq: seq, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if len(body) > 0 {
		ex.BodySHA256 = sha(body)
	}
	start := time.Now()
	resp, err := base.RoundTrip(req)
	if err != nil {
		ex.Error, ex.ElapsedMS = err.Error(), since(start)
		ex.ErrTimeout = errors.Is(err, context.DeadlineExceeded) || isTimeout(err)
		ex.ErrCanceled = errors.Is(err, context.Canceled)
		s.save(key, &ex, nil)
		return nil, err
	}
	data, rerr := io.ReadAll(io.LimitReader(resp.Body, maxRecordBody+1))
	_ = resp.Body.Close()
	if len(data) > maxRecordBody {
		data, rerr = data[:maxRecordBody], fmt.Errorf("ketch bench record: body exceeds %d bytes", maxRecordBody)
	}
	ex.Status, ex.Proto, ex.Header = resp.StatusCode, resp.Proto, resp.Header.Clone()
	ex.RespSHA256, ex.RespBytes, ex.ElapsedMS = sha(data), len(data), since(start)
	if rerr != nil {
		ex.BodyError = rerr.Error()
	}
	s.save(key, &ex, data)
	resp.Body = &replayBody{r: bytes.NewReader(data), err: rerr}
	if rerr == nil {
		resp.ContentLength = int64(len(data))
	}
	return resp, nil
}

// save writes the body (content-addressed) and the exchange. A failure to
// save is logged: the world then lacks the exchange, and its replay fails.
func (s *benchState) save(key string, ex *Exchange, data []byte) {
	err := func() error {
		if ex.RespSHA256 != "" {
			if err := writeOnce(filepath.Join(s.record, "bodies", ex.RespSHA256), data); err != nil {
				return err
			}
		}
		meta, err := json.MarshalIndent(ex, "", "  ")
		if err != nil {
			return err
		}
		for {
			name := filepath.Join(s.record, fmt.Sprintf("%s-%03d.json", key, ex.Seq))
			f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if errors.Is(err, os.ErrExist) { // another process recorded this key
				ex.Seq++
				if meta, err = json.MarshalIndent(ex, "", "  "); err != nil {
					return err
				}
				continue
			}
			if err != nil {
				return err
			}
			_, werr := f.Write(append(meta, '\n'))
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			return werr
		}
	}()
	line := map[string]any{"event": "recorded", "method": ex.Method, "url": redactURL(ex.URL), "key": key, "seq": ex.Seq,
		"status": ex.Status, "bytes": ex.RespBytes, "ms": ex.ElapsedMS}
	if ex.Error != "" {
		line["error"] = ex.Error
	}
	if err != nil {
		line["event"], line["save_error"] = "record_failed", err.Error()
	}
	s.logLine(line)
}

func writeOnce(name string, data []byte) error {
	if _, err := os.Stat(name); err == nil {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(name), ".body-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), name)
}

func (s *benchState) serve(req *http.Request, key string) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	n := s.next[key]
	s.next[key] = n + 1
	s.mu.Unlock()
	line := map[string]any{"event": "served", "method": req.Method, "url": req.URL.Redacted(), "key": key, "seq": n}
	recorded := s.replay.exchanges[key]
	if len(recorded) == 0 {
		line["event"] = "miss"
		s.logLine(line)
		return nil, fmt.Errorf("%w: %s %s", ErrUnrecorded, req.Method, req.URL.Redacted())
	}
	if n >= len(recorded) {
		line["event"] = "reused"
		n = len(recorded) - 1
	}
	ex := recorded[n]
	if ex.Error != "" {
		line["error"] = ex.Error
		s.logLine(line)
		return nil, &replayedError{msg: ex.Error, timeout: ex.ErrTimeout, canceled: ex.ErrCanceled}
	}
	data, err := s.replay.body(ex.RespSHA256)
	if err != nil {
		line["event"], line["error"] = "miss", err.Error()
		s.logLine(line)
		return nil, fmt.Errorf("%w: %s %s: %v", ErrUnrecorded, req.Method, req.URL.Redacted(), err)
	}
	line["status"], line["bytes"] = ex.Status, len(data)
	s.logLine(line)
	var berr error
	if ex.BodyError != "" {
		berr = errors.New(ex.BodyError)
	}
	resp := &http.Response{
		Status:        fmt.Sprintf("%d %s", ex.Status, http.StatusText(ex.Status)),
		StatusCode:    ex.Status,
		Proto:         ex.Proto,
		Header:        ex.Header.Clone(),
		Body:          &replayBody{r: bytes.NewReader(data), err: berr},
		ContentLength: int64(len(data)),
		Request:       req,
	}
	if resp.Header == nil {
		resp.Header = http.Header{}
	}
	if berr != nil {
		resp.ContentLength = -1
	}
	resp.ProtoMajor, resp.ProtoMinor, _ = http.ParseHTTPVersion(ex.Proto)
	return resp, nil
}

// replayedError is a recorded transport failure. It keeps the timeout and
// cancellation classes callers test with errors.Is and net.Error.
type replayedError struct {
	msg               string
	timeout, canceled bool
}

func (e *replayedError) Error() string   { return e.msg }
func (e *replayedError) Timeout() bool   { return e.timeout }
func (e *replayedError) Temporary() bool { return false }
func (e *replayedError) Is(target error) bool {
	return (e.timeout && target == context.DeadlineExceeded) || (e.canceled && target == context.Canceled)
}

// replayBody yields the recorded bytes, then the recorded read error.
type replayBody struct {
	r   *bytes.Reader
	err error
}

func (b *replayBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if errors.Is(err, io.EOF) && b.err != nil {
		return n, b.err
	}
	return n, err
}

func (b *replayBody) Close() error { return nil }

type replayWorld struct {
	exchanges map[string][]Exchange
	dirs      []string          // where bodies/<sha> may live
	bodies    map[string][]byte // archive bodies
}

func loadWorld(src, extraBodies string) (*replayWorld, error) {
	w := &replayWorld{exchanges: map[string][]Exchange{}, bodies: map[string][]byte{}}
	if strings.HasSuffix(src, ".tar.gz") || strings.HasSuffix(src, ".tgz") {
		if err := w.loadArchive(src); err != nil {
			return nil, err
		}
	} else if err := w.loadDir(src); err != nil {
		return nil, err
	}
	if extraBodies != "" {
		w.dirs = append(w.dirs, extraBodies)
	}
	for key, list := range w.exchanges {
		sort.Slice(list, func(i, j int) bool { return list[i].Seq < list[j].Seq })
		w.exchanges[key] = list
	}
	return w, nil
}

func (w *replayWorld) add(name string, data []byte) error {
	var ex Exchange
	if err := json.Unmarshal(data, &ex); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	key := strings.TrimSuffix(path.Base(name), ".json")
	if i := strings.LastIndexByte(key, '-'); i > 0 {
		key = key[:i]
	}
	w.exchanges[key] = append(w.exchanges[key], ex)
	return nil
}

func (w *replayWorld) loadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		if err := w.add(e.Name(), data); err != nil {
			return err
		}
	}
	w.dirs = append(w.dirs, filepath.Join(dir, "bodies"))
	return nil
}

func (w *replayWorld) loadArchive(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxRecordBody+1))
		if err != nil {
			return err
		}
		clean := path.Clean(hdr.Name)
		switch {
		case path.Base(path.Dir(clean)) == "bodies":
			w.bodies[path.Base(clean)] = data
		case strings.HasSuffix(clean, ".json"):
			if err := w.add(clean, data); err != nil {
				return err
			}
		}
	}
}

func (w *replayWorld) body(sum string) ([]byte, error) {
	if sum == "" {
		return nil, nil
	}
	data, ok := w.bodies[sum]
	if !ok {
		var err error
		for _, dir := range w.dirs {
			if data, err = os.ReadFile(filepath.Join(dir, sum)); err == nil {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("recorded body %s is missing", sum)
		}
	}
	if sha(data) != sum {
		return nil, fmt.Errorf("recorded body %s does not match its hash", sum)
	}
	return data, nil
}

func (s *benchState) logLine(fields map[string]any) {
	if s.log == nil {
		return
	}
	switch {
	case s.replay != nil:
		fields["mode"] = "replay"
	case s.record != "":
		fields["mode"] = "record"
	default:
		fields["mode"] = "live"
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.log.Write(append(data, '\n'))
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func since(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Redacted()
}
