// Owner: chunk A (inputs and sources).

package docs

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"

	"github.com/1broseidon/ketch/httpx"
)

// Registry API bases; tests point a pkgClient's fields at fixtures instead.
const (
	npmRegistryBase = "https://registry.npmjs.org"
	pypiBase        = "https://pypi.org"
	cratesBase      = "https://crates.io"
	goProxyBase     = "https://proxy.golang.org"
	pkgsiteBase     = "https://pkg.go.dev"
)

const (
	// apiCallTimeout bounds each registry or GitHub API call (plan §4),
	// here and in the resolver.
	apiCallTimeout = 8 * time.Second
	// pkgMaxBody caps an npm, crates.io, Go or GitHub response: each is a
	// few KB, so more is not the API we expect.
	pkgMaxBody = 4 << 20
	// pypiMaxBody caps what is read of a PyPI response looking for "info",
	// which comes first; the rest (every release's files) can run to MBs.
	pypiMaxBody = 16 << 20
)

// pkgClient reads package metadata from npm, PyPI, crates.io and the Go
// module proxy and pkg.go.dev. It implements pkgSource. Every request sends
// ua as its User-Agent (crates.io answers 403 without one); PyPI responses
// are stream-decoded only as far as their "info" object.
type pkgClient struct {
	client                              *http.Client
	npm, pypi, crates, goproxy, pkgsite string // API bases, no trailing slash
	ua                                  string
	timeout                             time.Duration // per API call; 0 is apiCallTimeout
	// pkgsiteSlot and cratesSlot take turns at pkg.go.dev and crates.io
	// (getPatient): both answer bursts with 429, and crates.io asks
	// crawlers for one request at a time. nil for none. retryWait
	// overrides patientWait (tests).
	pkgsiteSlot, cratesSlot *turns
	retryWait               time.Duration
}

// newPkgClient returns a pkgClient for the public registries over client.
// It performs no I/O.
func newPkgClient(client *http.Client) *pkgClient {
	return &pkgClient{
		client:  client,
		npm:     npmRegistryBase,
		pypi:    pypiBase,
		crates:  cratesBase,
		goproxy: goProxyBase,
		pkgsite: pkgsiteBase,
		ua:      webUserAgent,
		timeout: apiCallTimeout,

		pkgsiteSlot: newTurns(0),
		cratesSlot:  newTurns(cratesGap),
	}
}

var _ pkgSource = (*pkgClient)(nil)

// lookup fetches one package; see pkgSource. name is normalized for eco
// first. A 404 (or the Go proxy's 410) wraps ErrNotFound; a 5xx, another
// status, a network failure or an unreadable body is a plain error.
func (c *pkgClient) lookup(ctx context.Context, eco ecosystem, name string) (pkgInfo, error) {
	name = lookupName(eco, name)
	var (
		info pkgInfo
		err  error
	)
	switch eco {
	case ecoNPM:
		info, err = c.lookupNPMAnyCase(ctx, name)
	case ecoPyPI:
		info, err = c.lookupPyPI(ctx, name)
	case ecoCrates:
		info, err = c.lookupCrate(ctx, name)
	case ecoGo:
		info, err = c.lookupGo(ctx, name)
	default:
		return pkgInfo{}, fmt.Errorf("%q is not a package registry", eco)
	}
	if err != nil {
		return pkgInfo{}, err
	}
	return finishPkg(info), nil
}

// lookupAll looks spec up in every registry it allows; see pkgSource. The
// order is npm, PyPI, crates, Go.
func (c *pkgClient) lookupAll(ctx context.Context, spec libSpec) []pkgLookup {
	ecos := registriesFor(spec)
	out := make([]pkgLookup, len(ecos))
	var wg sync.WaitGroup
	for i, eco := range ecos {
		out[i] = pkgLookup{eco: eco, name: lookupName(eco, spec.name)}
		wg.Go(func() {
			out[i].info, out[i].err = c.lookup(ctx, eco, out[i].name)
		})
	}
	wg.Wait()
	return out
}

// goVersions lists a module's versions from the proxy; see pkgSource.
// Lines that are not semantic versions are dropped.
func (c *pkgClient) goVersions(ctx context.Context, module string) ([]string, error) {
	what := "go " + module
	if !isModulePath(module) {
		return nil, fmt.Errorf("%s: %w (not a module path)", what, ErrNotFound)
	}
	var versions []string
	err := c.get(ctx, what, c.goproxy+"/"+goEscapePath(module)+"/@v/list", pkgMaxBody, func(r io.Reader) error {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			if v := strings.TrimSpace(sc.Text()); parseSemver(v) != nil {
				versions = append(versions, v)
			}
		}
		return sc.Err()
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(versions, func(i, j int) bool { return semverLess(versions[j], versions[i]) })
	return versions, nil
}

// goModule is a module pkg.go.dev's search found, with the number of
// modules importing it.
type goModule struct {
	path      string
	importers int64
}

// searchGoPage is searchGoModules over pkg.go.dev's search page, which
// answers where its API's search (/v1/search) is held to a few requests a
// minute: each result's package and module path and its importer count
// (packages importing it). A module whose result is one of its packages
// (cosmos-sdk/server) is reported at the module path the page names.
func (c *pkgClient) searchGoPage(ctx context.Context, name string, n int) ([]goModule, error) {
	u := c.pkgsite + "/search?" + url.Values{"q": {name}, "m": {"package"}}.Encode()
	var mods []goModule
	err := c.getPatient(ctx, c.pkgsiteSlot, "pkg.go.dev search "+name, u, func(r io.Reader) error {
		doc, err := goquery.NewDocumentFromReader(r)
		if err != nil {
			return err
		}
		doc.Find(".SearchSnippet").Each(func(_ int, snip *goquery.Selection) {
			path := strings.TrimSpace(snip.Find("a[data-test-id=snippet-title]").AttrOr("data-clicked-package", ""))
			if path == "" {
				return
			}
			if mod, ok := strings.CutPrefix(collapseSpace(snip.Find(".SearchSnippet-sub strong").First().Text()), "Other packages in module "); ok {
				path = strings.TrimSuffix(mod, ":")
			}
			count := strings.ReplaceAll(strings.TrimSpace(snip.Find(".SearchSnippet-infoLabel strong").First().Text()), ",", "")
			importers, _ := strconv.ParseInt(count, 10, 64)
			mods = append(mods, goModule{path: path, importers: importers})
		})
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.SliceStable(mods, func(i, j int) bool { return mods[i].importers > mods[j].importers })
	if n > 0 && len(mods) > n {
		mods = mods[:n]
	}
	return mods, nil
}

// goImportedBy reads the "Imported by" count on a module's pkg.go.dev
// page; 0 when the page shows none.
func (c *pkgClient) goImportedBy(ctx context.Context, module string) (int64, error) {
	var n int64
	err := c.getPatient(ctx, c.pkgsiteSlot, "pkg.go.dev "+module, c.pkgsite+"/"+module, func(r io.Reader) error {
		doc, err := goquery.NewDocumentFromReader(r)
		if err != nil {
			return err
		}
		doc.Find("a[aria-label='Go to Imported By'], [data-test-id=UnitHeader-importedby] a").EachWithBreak(func(_ int, a *goquery.Selection) bool {
			m := importedByRe.FindStringSubmatch(collapseSpace(a.Text()))
			if m == nil {
				return true
			}
			n, _ = strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
			return false
		})
		return nil
	})
	return n, err
}

// importedByRe reads "Imported by: 191,418".
var importedByRe = regexp.MustCompile(`Imported by:?\s*([0-9][0-9,]*)`)

// Patient requests: services that answer a burst of requests with 429
// (pypistats.org, pkg.go.dev) are asked one request at a time per slot and
// retried while they answer 429 or 5xx, waiting what the response's
// Retry-After asks (at most patientMaxWait), else patientWait doubling.
const (
	patientAttempts = 5
	patientWait     = time.Second
	patientMaxWait  = 5 * time.Second
)

// getPatient is get for such a service: it waits its turn in slot (nil:
// no turn taking), and tries up to patientAttempts times while the
// service answers 429 or 5xx, backing off from patientWait (c.retryWait in
// tests), within the caller's deadline. A 404 or 410 wraps ErrNotFound.
func (c *pkgClient) getPatient(ctx context.Context, slot *turns, what, u string, read func(io.Reader) error) error {
	release, err := slot.take(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	defer release()
	wait := cmp.Or(c.retryWait, patientWait)
	attempts := patientAttempts
	if speculative(ctx) {
		attempts = 1 // a guessed reading: its failure vetoes nothing, so it waits for nothing
	}
	for attempt := range attempts {
		if attempt > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return fmt.Errorf("%s: %w (after %v)", what, ctx.Err(), err)
			}
			wait = min(2*wait, patientMaxWait)
		}
		var retry bool
		var asked time.Duration
		if retry, asked, err = c.tryOnce(ctx, what, u, read); !retry {
			return err
		}
		if asked > 0 && c.retryWait == 0 {
			wait = min(asked, patientMaxWait)
		}
	}
	return err
}

// cratesGap spaces crates.io requests: its crawler policy asks for at most
// one a second. A variable only so tests over recorded fixtures need not
// wait it out.
var cratesGap = time.Second

// turns takes turns at one service: one request at a time, and at least
// gap between the starts of two. A nil *turns takes no turns.
type turns struct {
	ch   chan struct{}
	gap  time.Duration
	last time.Time // guarded by ch
}

func newTurns(gap time.Duration) *turns { return &turns{ch: make(chan struct{}, 1), gap: gap} }

// take waits for t's turn, and the gap since the last one, within ctx.
func (t *turns) take(ctx context.Context) (release func(), err error) {
	if t == nil {
		return func() {}, nil
	}
	select {
	case t.ch <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if wait := time.Until(t.last.Add(t.gap)); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			<-t.ch
			return nil, ctx.Err()
		}
	}
	t.last = time.Now()
	return func() { <-t.ch }, nil
}

type speculativeKey struct{}

// withSpeculative marks ctx as a bare query's guessed reading
// (resolution.strict): its registry lookups are tried once, without
// retries, since nothing waits on or is vetoed by their failure (vetoes).
func withSpeculative(ctx context.Context) context.Context {
	return context.WithValue(ctx, speculativeKey{}, true)
}

func speculative(ctx context.Context) bool {
	v, _ := ctx.Value(speculativeKey{}).(bool)
	return v
}

// tryOnce is one getPatient request: retry reports a failure worth
// another try (a network error, 429, 5xx), with the wait the response's
// Retry-After asks for, if any.
func (c *pkgClient) tryOnce(ctx context.Context, what, u string, read func(io.Reader) error) (retry bool, wait time.Duration, err error) {
	resp, err := c.send(ctx, what, u)
	if err != nil {
		return true, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch status := resp.StatusCode; {
	case status == http.StatusNotFound || status == http.StatusGone:
		return false, 0, fmt.Errorf("%s: %w", what, ErrNotFound)
	case transientStatus(status):
		if n, aerr := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); aerr == nil && n > 0 {
			wait = time.Duration(n) * time.Second
		}
		return true, wait, fmt.Errorf("%s: status %d", what, status)
	case status != http.StatusOK:
		return false, 0, fmt.Errorf("%s: status %d", what, status)
	}
	if err := read(io.LimitReader(resp.Body, pkgMaxBody)); err != nil {
		return false, 0, fmt.Errorf("%s: unreadable response: %w", what, err)
	}
	return false, 0, nil
}

// get fetches u within the per-call budget and hands a 200 body, capped at
// limit bytes, to read. what names the lookup in errors ("npm react").
func (c *pkgClient) get(ctx context.Context, what, u string, limit int64, read func(io.Reader) error) error {
	timeout := c.timeout
	if timeout <= 0 {
		timeout = apiCallTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := c.send(ctx, what, u)
	if err == nil && transientStatus(resp.StatusCode) {
		// One retry for a registry that is briefly busy (crates.io answers
		// a burst with 429): a bare query asks for up to three names at once.
		wait := retryWait(resp.Header)
		_ = resp.Body.Close()
		select {
		case <-time.After(wait):
			resp, err = c.send(ctx, what, u)
		case <-ctx.Done():
			return fmt.Errorf("%s: registry returned status %d", what, resp.StatusCode)
		}
	}
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%s: registry returned status %d", what, resp.StatusCode)
	}
	if err := read(io.LimitReader(resp.Body, limit)); err != nil {
		return fmt.Errorf("%s: unreadable response: %w", what, err)
	}
	return nil
}

// send makes one GET of u with the client's User-Agent, and the Accept
// header the context carries (withAccept), if any.
func (c *pkgClient) send(ctx context.Context, what, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	req.Header.Set("User-Agent", c.ua)
	if accept, ok := ctx.Value(acceptKey{}).(string); ok {
		req.Header.Set("Accept", accept)
	}
	resp, err := orDefaultClient(c.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return resp, nil
}

// acceptKey carries the Accept header a registry request asks for.
type acceptKey struct{}

// releases lists the releases a registry publishes for a package, newest
// first by version, pre-releases included: npm's abbreviated metadata
// (its versions object) and PyPI's JSON (its releases object), read as a
// stream of keys. Other registries are not asked: crates are documented
// per release on docs.rs, Go modules by goVersions.
func (c *pkgClient) releases(ctx context.Context, eco ecosystem, name string) ([]string, error) {
	var u, field string
	var limit int64
	switch eco {
	case ecoNPM:
		// The abbreviated document: versions without their READMEs.
		ctx = context.WithValue(ctx, acceptKey{}, "application/vnd.npm.install-v1+json")
		u, field, limit = c.npm+"/"+npmPath(name), "versions", pypiMaxBody
	case ecoPyPI:
		u, field, limit = c.pypi+"/pypi/"+url.PathEscape(normalizeName(ecoPyPI, name))+"/json", "releases", pypiMaxBody
	default:
		return nil, fmt.Errorf("%s %s: %w (no release list)", eco, name, ErrNotFound)
	}
	var out []string
	err := c.get(ctx, string(eco)+" releases "+name, u, limit, func(r io.Reader) (err error) {
		out, err = objectKeys(r, field)
		return err
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, okA := parseVerNum(out[i])
		b, okB := parseVerNum(out[j])
		return okA && (!okB || compareVer(a, b) > 0)
	})
	return out, nil
}

// objectKeys reads the keys of the top-level JSON object's field, an
// object, skipping every value.
func objectKeys(r io.Reader, field string) ([]string, error) {
	dec := json.NewDecoder(r)
	if tok, err := dec.Token(); err != nil {
		return nil, err
	} else if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	var skip json.RawMessage
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if key != field {
			if err := dec.Decode(&skip); err != nil {
				return nil, err
			}
			continue
		}
		if tok, err := dec.Token(); err != nil {
			return nil, err
		} else if d, ok := tok.(json.Delim); !ok || d != '{' {
			return nil, fmt.Errorf("%s is not an object", field)
		}
		var keys []string
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			if s, ok := k.(string); ok {
				keys = append(keys, s)
			}
			if err := dec.Decode(&skip); err != nil {
				return nil, err
			}
		}
		return keys, nil
	}
	return nil, fmt.Errorf("no %q object", field)
}

// transientStatus is a status worth one retry: 429 and 5xx.
func transientStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
}

// retryWait is how long to wait before the retry: the response's
// Retry-After in seconds, at most maxRetryWait, else defaultRetryWait.
func retryWait(h http.Header) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && n >= 0 {
		return min(time.Duration(n)*time.Second, maxRetryWait)
	}
	return defaultRetryWait
}

// Registry retry waits.
const (
	defaultRetryWait = 300 * time.Millisecond
	maxRetryWait     = 2 * time.Second
)

// orDefaultClient is client, or the shared httpx client when nil.
func orDefaultClient(client *http.Client) *http.Client {
	if client == nil {
		return httpx.Default()
	}
	return client
}

// decodeJSON reads one JSON value into v.
func decodeJSON(v any) func(io.Reader) error {
	return func(r io.Reader) error { return json.NewDecoder(r).Decode(v) }
}

// finishPkg fills what every registry's answer derives the same way: the
// repository from the homepage when the metadata names none and the
// homepage is a GitHub repository (common on PyPI), and the repo key.
func finishPkg(p pkgInfo) pkgInfo {
	if p.repoURL == "" {
		if _, ok := parseRepoURL(p.homepage); ok {
			p.repoURL = p.homepage
		}
	}
	p.repo, _ = parseRepoURL(p.repoURL)
	return p
}

// metaText cleans a free-text metadata value: trimmed, and "" for the
// placeholders old tooling wrote ("UNKNOWN").
func metaText(s string) string {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "unknown") || strings.EqualFold(s, "none") {
		return ""
	}
	return s
}

// npm

// npmManifest is the fields of /<name>/latest that lookup reads. Old
// packages publish odd shapes, so the loosely typed fields stay raw.
type npmManifest struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description json.RawMessage `json:"description"`
	Homepage    json.RawMessage `json:"homepage"`
	Repository  json.RawMessage `json:"repository"`
	Deprecated  json.RawMessage `json:"deprecated"`
}

// lookupName is the name to ask eco's registry for: its normalized form
// (normalizeName), except on npm, whose registry is case-sensitive and
// still serves legacy mixed-case packages (JSONStream), so the name stays
// as given there and lookupNPMAnyCase tries lower case after.
func lookupName(eco ecosystem, name string) string {
	if eco == ecoNPM {
		return strings.TrimSpace(name)
	}
	return normalizeName(eco, name)
}

// lookupNPMAnyCase looks name up on npm as given, then, when the registry
// has no such package, in lower case, as every package published since
// 2017 is named.
func (c *pkgClient) lookupNPMAnyCase(ctx context.Context, name string) (pkgInfo, error) {
	info, err := c.lookupNPM(ctx, name)
	if lower := strings.ToLower(name); errors.Is(err, ErrNotFound) && lower != name {
		return c.lookupNPM(ctx, lower)
	}
	return info, err
}

func (c *pkgClient) lookupNPM(ctx context.Context, name string) (pkgInfo, error) {
	var m npmManifest
	if err := c.get(ctx, "npm "+name, c.npm+"/"+npmPath(name)+"/latest", pkgMaxBody, decodeJSON(&m)); err != nil {
		return pkgInfo{}, err
	}
	if m.Name == "" {
		m.Name = name
	}
	return pkgInfo{
		eco:        ecoNPM,
		name:       m.Name,
		version:    m.Version,
		summary:    metaText(jsonText(m.Description)),
		homepage:   metaText(jsonText(m.Homepage)),
		repoURL:    metaText(npmRepository(m.Repository)),
		deprecated: npmDeprecated(m.Deprecated),
	}, nil
}

// npmPath is a package name as a registry path: a scoped name keeps its
// "@" and "/" unescaped, as the registry serves it.
func npmPath(name string) string {
	if scope, pkg, ok := strings.Cut(name, "/"); ok {
		return url.PathEscape(scope) + "/" + url.PathEscape(pkg)
	}
	return url.PathEscape(name)
}

// npmRepository reads package.json's repository: a string ("github:o/r",
// "o/r" or a URL), an object with a url, or (in old packages) a list of
// either.
func npmRepository(raw json.RawMessage) string {
	var obj struct {
		URL string `json:"url"`
	}
	var list []json.RawMessage
	switch {
	case jsonText(raw) != "":
		return jsonText(raw)
	case json.Unmarshal(raw, &obj) == nil && obj.URL != "":
		return obj.URL
	case json.Unmarshal(raw, &list) == nil && len(list) > 0:
		return npmRepository(list[0])
	}
	return ""
}

// npmDeprecated reads the deprecated field: a message, or rarely true.
func npmDeprecated(raw json.RawMessage) bool {
	var b bool
	return strings.TrimSpace(jsonText(raw)) != "" || (json.Unmarshal(raw, &b) == nil && b)
}

// jsonText is raw as a string when it is a JSON string, or the first
// string of a JSON list; else "".
func jsonText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		return list[0]
	}
	return ""
}

// PyPI

// pypiInfo is the fields of a PyPI project's "info" object that lookup
// reads.
type pypiInfo struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Summary     string            `json:"summary"`
	HomePage    string            `json:"home_page"`
	DocsURL     string            `json:"docs_url"`
	ProjectURLs map[string]string `json:"project_urls"`
	Description string            `json:"description"` // the README (long_description)
}

// pypiLabels are the PEP 753 normalized project_urls labels for each URL
// role, the canonical label first.
var pypiLabels = map[string][]string{
	"documentation": {"documentation", "docs"},
	"homepage":      {"homepage"},
	// PEP 753's source aliases, plus "code" and "repo", which projects use.
	"source": {"source", "repository", "sourcecode", "github", "code", "repo"},
	// PEP 753's changelog aliases: a page of the project's docs, often.
	"changelog": {"changelog", "changes", "history", "releasenotes", "news", "whatsnew"},
}

func (c *pkgClient) lookupPyPI(ctx context.Context, name string) (pkgInfo, error) {
	var info pypiInfo
	u := c.pypi + "/pypi/" + url.PathEscape(name) + "/json"
	err := c.get(ctx, "pypi "+name, u, pypiMaxBody, func(r io.Reader) (err error) {
		info, err = decodePyPIInfo(r)
		return err
	})
	if err != nil {
		return pkgInfo{}, err
	}
	urls := pypiProjectURLs(info.ProjectURLs)
	p := pkgInfo{
		eco:      ecoPyPI,
		name:     info.Name,
		version:  info.Version,
		summary:  metaText(info.Summary),
		homepage: firstNonEmpty(urls["homepage"], metaText(info.HomePage)),
		docsURL:  firstNonEmpty(urls["documentation"], metaText(info.DocsURL)),
		repoURL:  urls["source"],
		pageURL:  urls["changelog"],
		readme:   info.Description,
		moreURLs: otherProjectURLs(info.ProjectURLs, urls),
	}
	if p.name == "" {
		p.name = name
	}
	return p, nil
}

// decodePyPIInfo reads a PyPI JSON response only as far as its top-level
// "info" object, which PyPI sends first: the releases after it can run to
// megabytes, and the body is closed unread.
func decodePyPIInfo(r io.Reader) (pypiInfo, error) {
	dec := json.NewDecoder(r)
	if tok, err := dec.Token(); err != nil {
		return pypiInfo{}, err
	} else if d, ok := tok.(json.Delim); !ok || d != '{' {
		return pypiInfo{}, errors.New("not a JSON object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return pypiInfo{}, err
		}
		if tok == "info" {
			var info pypiInfo
			err := dec.Decode(&info)
			return info, err
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return pypiInfo{}, err
		}
	}
	return pypiInfo{}, errors.New(`no "info" object`)
}

// otherProjectURLs are the project_urls values no role took, sorted and
// newline-joined (pkgInfo.moreURLs).
func otherProjectURLs(m, roles map[string]string) string {
	taken := map[string]bool{}
	for _, v := range roles {
		taken[v] = true
	}
	var out []string
	for _, v := range m {
		if v = metaText(v); v != "" && !taken[v] && !strings.ContainsAny(v, "\n") && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// pypiProjectURLs maps project_urls to roles (pypiLabels' keys) by their
// PEP 753 normalized labels. Where two labels share a role the canonical
// one wins, then the first in label order.
func pypiProjectURLs(m map[string]string) map[string]string {
	byLabel := map[string]string{}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		label := pep753Label(k)
		if v := metaText(m[k]); v != "" && byLabel[label] == "" {
			byLabel[label] = v
		}
	}
	out := map[string]string{}
	for role, labels := range pypiLabels {
		for _, l := range labels {
			if v := byLabel[l]; v != "" {
				out[role] = v
				break
			}
		}
	}
	return out
}

// pep753Label normalizes a project_urls label as PEP 753 does: ASCII
// punctuation and whitespace removed, lower case ("Source Code" is
// "sourcecode").
func pep753Label(label string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || (r <= unicode.MaxASCII && (unicode.IsPunct(r) || unicode.IsSymbol(r))) {
			return -1
		}
		return unicode.ToLower(r)
	}, label)
}

// crates.io

// crateResponse is the fields of /api/v1/crates/<name> that lookup reads.
type crateResponse struct {
	Crate struct {
		Name             string `json:"name"`
		Description      string `json:"description"`
		Homepage         string `json:"homepage"`
		Documentation    string `json:"documentation"`
		Repository       string `json:"repository"`
		MaxStableVersion string `json:"max_stable_version"`
		DefaultVersion   string `json:"default_version"`
		MaxVersion       string `json:"max_version"`
		RecentDownloads  int64  `json:"recent_downloads"`
	} `json:"crate"`
}

// lookupCrate asks for the crate with include=downloads: about 1 KB, where
// no include parameter sends every version (up to ~1 MB). That response
// carries default_version (the latest stable release) and
// recent_downloads but leaves max_stable_version null, so the version is
// the first of max_stable_version, default_version and max_version that
// is set (crates.io sends "0.0.0" for an omitted one).
func (c *pkgClient) lookupCrate(ctx context.Context, name string) (pkgInfo, error) {
	var resp crateResponse
	u := c.crates + "/api/v1/crates/" + url.PathEscape(name) + "?include=downloads"
	if err := c.getPatient(ctx, c.cratesSlot, "crates "+name, u, decodeJSON(&resp)); err != nil {
		return pkgInfo{}, err
	}
	cr := resp.Crate
	if cr.Name == "" {
		return pkgInfo{}, fmt.Errorf("crates %s: unreadable response: no crate", name)
	}
	version := ""
	for _, v := range []string{cr.MaxStableVersion, cr.DefaultVersion, cr.MaxVersion} {
		if v != "" && v != "0.0.0" {
			version = v
			break
		}
	}
	return pkgInfo{
		eco:          ecoCrates,
		name:         cr.Name,
		version:      version,
		summary:      strings.TrimSpace(cr.Description),
		homepage:     metaText(cr.Homepage),
		docsURL:      metaText(cr.Documentation),
		repoURL:      metaText(cr.Repository),
		registryDocs: docsRSURL(cr.Name, "latest"),
		downloads:    cr.RecentDownloads,
	}, nil
}

// docsRSURL is docs.rs's page for a crate's library at version ("latest",
// "~1.38" or exact): /<crate>/<version>/<crate with _ for ->/.
func docsRSURL(crate, version string) string {
	return "https://docs.rs/" + url.PathEscape(crate) + "/" + version + "/" + url.PathEscape(strings.ReplaceAll(crate, "-", "_")) + "/"
}

// Go

// goLatest is the proxy's @latest answer.
type goLatest struct {
	Version string `json:"Version"`
	Origin  struct {
		URL string `json:"URL"`
	} `json:"Origin"`
}

// lookupGo reads the module's latest version and repository from the
// proxy. When the proxy records no origin (modules it fetched before it
// kept one), pkg.go.dev's module endpoint is asked for repoUrl, best
// effort.
func (c *pkgClient) lookupGo(ctx context.Context, module string) (pkgInfo, error) {
	what := "go " + module
	if !isModulePath(module) {
		return pkgInfo{}, fmt.Errorf("%s: %w (not a module path)", what, ErrNotFound)
	}
	var latest goLatest
	if err := c.get(ctx, what, c.goproxy+"/"+goEscapePath(module)+"/@latest", pkgMaxBody, decodeJSON(&latest)); err != nil {
		return pkgInfo{}, err
	}
	repo := latest.Origin.URL
	if repo == "" {
		repo = c.pkgsiteRepo(ctx, module)
	}
	return pkgInfo{
		eco:          ecoGo,
		name:         module,
		version:      latest.Version,
		repoURL:      metaText(repo),
		registryDocs: pkgsiteBase + "/" + module,
	}, nil
}

// hasGoMod reports whether pkg.go.dev records a go.mod for module. The
// module proxy answers for any GitHub repository with version tags,
// synthesizing a module for code that never was one (an npm project's
// repository at v14.2.15+incompatible), so a module found only by its
// repository's path is a Go library only when it has a go.mod.
func (c *pkgClient) hasGoMod(ctx context.Context, module string) (bool, error) {
	var m struct {
		HasGoMod bool `json:"hasGoMod"`
	}
	if err := c.get(ctx, "pkg.go.dev "+module, c.pkgsite+"/v1/module/"+module, pkgMaxBody, decodeJSON(&m)); err != nil {
		return false, err
	}
	return m.HasGoMod, nil
}

// pkgsiteRepo is the repository pkg.go.dev records for module, or "".
func (c *pkgClient) pkgsiteRepo(ctx context.Context, module string) string {
	var m struct {
		RepoURL string `json:"repoUrl"`
	}
	if c.get(ctx, "pkg.go.dev "+module, c.pkgsite+"/v1/module/"+module, pkgMaxBody, decodeJSON(&m)) != nil {
		return ""
	}
	return m.RepoURL
}

// goEscapePath case-encodes a module path for the proxy: each upper-case
// letter becomes "!" and its lower case (github.com/BurntSushi/toml is
// github.com/!burnt!sushi/toml).
func goEscapePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if 'A' <= r && r <= 'Z' {
			b.WriteByte('!')
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// semver is a parsed vMAJOR.MINOR.PATCH[-pre][+build], numbers as digit
// strings so any length compares.
type semver struct {
	nums [3]string
	pre  []string
}

// parseSemver parses a Go module version, or returns nil.
func parseSemver(v string) *semver {
	rest, ok := strings.CutPrefix(v, "v")
	if !ok {
		return nil
	}
	rest, _, _ = strings.Cut(rest, "+")
	core, pre, hasPre := strings.Cut(rest, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 || (hasPre && pre == "") {
		return nil
	}
	var s semver
	for i, p := range parts {
		if !isDigits(p) {
			return nil
		}
		s.nums[i] = strings.TrimLeft(p, "0")
	}
	if hasPre {
		s.pre = strings.Split(pre, ".")
	}
	return &s
}

// semverLess reports a < b by semantic version precedence; a version that
// does not parse sorts below every one that does.
func semverLess(a, b string) bool {
	sa, sb := parseSemver(a), parseSemver(b)
	if sa == nil || sb == nil {
		return sa == nil && sb != nil
	}
	for i := range sa.nums {
		if c := compareDigits(sa.nums[i], sb.nums[i]); c != 0 {
			return c < 0
		}
	}
	return comparePrerelease(sa.pre, sb.pre) < 0
}

// comparePrerelease orders pre-release identifiers by semver rules: none
// ranks above any; numeric identifiers compare as numbers and below
// alphanumeric ones; a shorter list that is a prefix ranks lower.
func comparePrerelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := compareIdent(a[i], b[i]); c != 0 {
			return c
		}
	}
	return len(a) - len(b)
}

func compareIdent(a, b string) int {
	na, nb := isDigits(a), isDigits(b)
	switch {
	case na && nb:
		return compareDigits(strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0"))
	case na:
		return -1
	case nb:
		return 1
	}
	return strings.Compare(a, b)
}

// compareDigits compares digit strings without leading zeros as numbers.
func compareDigits(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}
