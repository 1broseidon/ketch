// Owner: chunk A (inputs and sources).

package docs

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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

// searchGo searches pkg.go.dev for modules; see pkgSource. Packages of
// one module count once, at the module's highest importer count. A 404
// (the API is new) is no results.
func (c *pkgClient) searchGo(ctx context.Context, name string, n int) ([]string, error) {
	var resp struct {
		Items []struct {
			ModulePath       string `json:"modulePath"`
			ImportedByCounts struct {
				Modules int64 `json:"modules"`
			} `json:"importedByCounts"`
		} `json:"items"`
	}
	u := c.pkgsite + "/v1/search?" + url.Values{"q": {name}}.Encode()
	err := c.get(ctx, "pkg.go.dev search "+name, u, pkgMaxBody, decodeJSON(&resp))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	imported := map[string]int64{}
	var mods []string
	for _, it := range resp.Items {
		if it.ModulePath == "" {
			continue
		}
		if _, seen := imported[it.ModulePath]; !seen {
			mods = append(mods, it.ModulePath)
		}
		imported[it.ModulePath] = max(imported[it.ModulePath], it.ImportedByCounts.Modules)
	}
	sort.SliceStable(mods, func(i, j int) bool { return imported[mods[i]] > imported[mods[j]] })
	if n > 0 && len(mods) > n {
		mods = mods[:n]
	}
	return mods, nil
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

// send makes one GET of u with the client's User-Agent.
func (c *pkgClient) send(ctx context.Context, what, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	req.Header.Set("User-Agent", c.ua)
	resp, err := orDefaultClient(c.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return resp, nil
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
}

// pypiLabels are the PEP 753 normalized project_urls labels for each URL
// role, the canonical label first.
var pypiLabels = map[string][]string{
	"documentation": {"documentation", "docs"},
	"homepage":      {"homepage"},
	// PEP 753's source aliases, plus "code" and "repo", which projects use.
	"source": {"source", "repository", "sourcecode", "github", "code", "repo"},
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
	if err := c.get(ctx, "crates "+name, u, pkgMaxBody, decodeJSON(&resp)); err != nil {
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
