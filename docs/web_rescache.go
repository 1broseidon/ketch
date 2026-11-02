package docs

import (
	"encoding/json"
	"slices"
	"strings"
)

// resolutionCacheVersion versions the cached value's shape and the rules
// that produced it: a change to either starts a fresh cache.
const resolutionCacheVersion = "docs-resolution/v2"

// cachedResolution is what the resolution cache keeps for one pick.
type cachedResolution struct {
	Root     string   `json:"root"`
	Version  string   `json:"version,omitempty"`
	Title    string   `json:"title,omitempty"`
	Packages []string `json:"packages,omitempty"`
	Project  string   `json:"project,omitempty"`
	Trust    float64  `json:"trust,omitempty"`
	// Weight is the pick's weight (stars, or weekly downloads keyless)
	// when it was read: a bare query's readings compare it.
	Weight *int `json:"weight,omitempty"`
}

// resolutionKey keys a single-library pick by everything that decides it:
// the spec (ecosystem, name, version), whether it is a bare query's guessed
// or lean reading, whether GitHub was asked, and the ecosystem hint.
func resolutionKey(spec libSpec, strict, lean, keyless bool, hint []ecosystem) string {
	mode := "github"
	if keyless {
		mode = "keyless"
	}
	h := make([]string, 0, len(hint))
	for _, e := range hint {
		h = append(h, string(e))
	}
	slices.Sort(h)
	reading := boolText(strict)
	if lean && !strict {
		reading = "lean"
	}
	return strings.Join([]string{resolutionCacheVersion, mode, reading, strings.Join(h, ","),
		string(spec.eco), strings.ToLower(spec.name), strings.ToLower(spec.version)}, "|")
}

func boolText(b bool) string {
	if b {
		return "strict"
	}
	return "-"
}

// resolutionCache is the fetcher's ResolutionCache, nil without one.
func (w *Web) resolutionCache() ResolutionCache {
	rc, _ := w.fetcher.(ResolutionCache)
	return rc
}

// loadResolution is a cached pick for key, as a match docsAt can search.
func (w *Web) loadResolution(key string) (webMatch, bool) {
	rc := w.resolutionCache()
	if rc == nil {
		return webMatch{}, false
	}
	raw, ok := rc.GetResolution(key)
	if !ok {
		return webMatch{}, false
	}
	var c cachedResolution
	if json.Unmarshal([]byte(raw), &c) != nil {
		return webMatch{}, false
	}
	root, err := parseWebRoot(c.Root)
	if err != nil || !isPublicWebURL(root.u) {
		return webMatch{}, false
	}
	return webMatch{
		root: root, version: c.Version, title: c.Title, packages: c.Packages, project: c.Project, trust: c.Trust,
		primary: true, stars: weightOr(c.Weight, -1),
		evidence: []evidence{{kind: evNote, detail: "resolved earlier and cached"}},
	}, true
}

// storeResolution caches a verified pick under key.
func (w *Web) storeResolution(key string, m webMatch) {
	rc := w.resolutionCache()
	if rc == nil || m.root.u == nil {
		return
	}
	c := cachedResolution{Root: m.root.String(), Version: m.version, Title: m.title, Packages: m.packages, Project: m.project, Trust: m.trust}
	if m.stars >= 0 {
		c.Weight = &m.stars
	}
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	rc.PutResolution(key, string(b))
}

// weightOr is *n, or def when n is nil.
func weightOr(n *int, def int) int {
	if n == nil {
		return def
	}
	return *n
}
