// Owner: chunk C (versions and adapters).

package docs

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// versionLabelRe matches a version label: a path segment or host label
// such as 5.2, v3.1.1, 2.x, 4x, 1.0rc1, next, stable or main (plan §6).
// Bare integers match too; callers accept them only at a root's own
// version position or beside at least two integer siblings. web_rank.go's
// collapseVariants shares it.
var versionLabelRe = regexp.MustCompile(`(?i)^(?:v?\d+(?:\.(?:\d+|x))*(?:[-.]?(?:rc|beta|alpha)\d*)?|\d+x|next|latest|stable|current|dev|devel|devdocs|main|master|canary|nightly|unstable|lts)$`)

// versionAliasClass groups the alias labels matchVersion treats as one:
// any of the "current" class names the version the site serves as
// current, any of the "dev" class its development docs.
var versionAliasClass = map[string]string{
	"latest": "current", "stable": "current", "current": "current",
	"dev": "dev", "devel": "dev", "devdocs": "dev", "next": "dev", "main": "dev",
	"master": "dev", "canary": "dev", "nightly": "dev", "unstable": "dev",
}

// labelCurrent labels a version whose own label is unknown: the
// unversioned docs a site serves beside its versioned ones (vite.dev
// beside v2.vite.dev, Docusaurus /docs/ beside /docs/2.x/).
const labelCurrent = "current"

// errNoVersionList is a version source with nothing to say: the file is
// absent or unreadable, or the page names none. siteVersions skips it.
var errNoVersionList = errors.New("no version list")

// segmentLabel returns the version label a path segment or host label
// carries: the segment itself when versionLabelRe matches it once a semver
// operator is dropped (docs.rs ~1.38), or the part after an "@"
// (pkg.go.dev cobra@v1.8.0). Bare integers of three or more digits (404,
// 500, 2024) are never versions.
func segmentLabel(seg string) (string, bool) {
	if i := strings.LastIndexByte(seg, '@'); i > 0 {
		seg = seg[i+1:]
	}
	seg = strings.TrimLeft(seg, "~^=")
	if seg == "" || !versionLabelRe.MatchString(seg) || isBareInt(seg) && len(seg) > 2 {
		return "", false
	}
	return seg, true
}

// isBareInt reports a label that is only digits.
func isBareInt(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// isNumericLabel reports a label that names a release (5.2, v2, 2.x),
// not an alias.
func isNumericLabel(s string) bool {
	_, ok := parseVerNum(s)
	return ok
}

// pathSegments splits a URL path into its non-empty segments.
func pathSegments(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// segmentsPath joins segments into a directory path: "/a/b/", or "/".
func segmentsPath(segs []string) string {
	if len(segs) == 0 {
		return "/"
	}
	return "/" + strings.Join(segs, "/") + "/"
}

// hostLabel returns the version label in u's first host label (v2 in
// v2.vite.dev), which must leave at least two labels behind, so a site
// called next.org is not version "next". Bare integers never count.
func hostLabel(u *url.URL) (string, bool) {
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	first, rest, ok := strings.Cut(host, ".")
	if !ok || !strings.Contains(rest, ".") || strings.ContainsAny(first, "~^=@") {
		return "", false
	}
	label, ok := segmentLabel(first)
	if !ok || isBareInt(label) {
		return "", false
	}
	return label, true
}

// rootVersion returns the version label root carries and where: seg is the
// index of the version segment among the root path's segments (after an
// optional locale), or -1 when the label is the first host label (v2. or
// main. against the site's host), with label "" and seg -1 when the root
// names no version.
//
// The first segment that is a label wins, bare integers included (a docs
// root at /3/ or /docs/16/ is a version), except on two hosts whose layout
// is known: docs.rs keeps the version second (/<crate>/<version>/<ident>/,
// and a crate may be called next), pkg.go.dev after the module's "@" (a
// module path may end in /v2).
func rootVersion(root webRoot) (label string, seg int) {
	segs := pathSegments(root.prefix)
	switch scopeHost(root.u) {
	case "docs.rs":
		if len(segs) > 1 {
			if l, ok := segmentLabel(segs[1]); ok {
				return l, 1
			}
		}
		return "", -1
	case "pkg.go.dev":
		for i, s := range segs {
			if l, ok := segmentLabel(s); ok && strings.Contains(s, "@") {
				return l, i
			}
		}
		return "", -1
	}
	for i, s := range segs {
		if l, ok := segmentLabel(s); ok {
			return l, i
		}
	}
	if l, ok := hostLabel(root.u); ok {
		return l, -1
	}
	return "", -1
}

// versionSource is one of siteVersions' sources.
type versionSource func(context.Context) (versionSet, error)

// siteVersions finds the versions the site at root publishes, from the
// first source that answers (plan §6): Read the Docs addons, mike
// versions.json, a pydata switcher, sibling links on p, the Read the Docs
// sitemap, then the Sphinx build's own VERSION. p is root's profile. A
// failing source is skipped, never an error; a site with no versions gives
// a zero versionSet.
//
// Read the Docs also supplies the project's name and repository, kept
// even when its version list is empty. When the root's own version is an
// alias (stable, latest), a Sphinx build supplies the concrete label it
// stands for (8.5.x, keeping stable as an alias), and objects.inv the
// project name when no other source gave one.
func (w *Web) siteVersions(ctx context.Context, root webRoot, p *siteProfile) versionSet {
	if p == nil {
		p = &siteProfile{}
	}
	var set versionSet
	for _, src := range w.versionSources(root, p) {
		got, err := src(ctx)
		set.project = firstNonEmpty(set.project, got.project)
		set.repoURL = firstNonEmpty(set.repoURL, got.repoURL)
		if err == nil && len(got.versions) > 0 {
			set.versions, set.source = got.versions, got.source
			break
		}
		if ctx.Err() != nil {
			return set
		}
	}
	w.addSphinxBuild(ctx, root, p, &set)
	if set.versions = tidyVersions(set.versions); len(set.versions) == 0 {
		set.versions, set.source = nil, ""
	}
	return set
}

// versionSources lists the sources worth asking for root, in priority
// order. Read the Docs sites get its API and sitemap; mike's versions.json
// is only probed on MkDocs sites with a version to look beside, or where
// the theme says mike; a switcher only when the page names its JSON.
func (w *Web) versionSources(root webRoot, p *siteProfile) []versionSource {
	_, seg := rootVersion(root)
	rtd := p.meta["readthedocs-project-slug"] != ""
	list := func(source string, f func(context.Context) ([]siteVersion, error)) versionSource {
		return func(ctx context.Context) (versionSet, error) {
			vs, err := f(ctx)
			return versionSet{versions: vs, source: source}, err
		}
	}
	var out []versionSource
	if rtd {
		out = append(out, func(ctx context.Context) (versionSet, error) { return w.rtdVersions(ctx, root, p) })
	}
	if !rtd && (mikeHinted(p) || seg >= 0 && isMkDocs(p)) {
		out = append(out, list(versionsMike, func(ctx context.Context) ([]siteVersion, error) { return w.mikeVersions(ctx, root) }))
	}
	out = append(out,
		list(versionsSwitcher, func(ctx context.Context) ([]siteVersion, error) { return w.switcherVersions(ctx, root, p) }),
		list(versionsSelect, func(context.Context) ([]siteVersion, error) { return selectVersions(root, p), nil }),
		list(versionsSiblings, func(context.Context) ([]siteVersion, error) { return siblingVersions(root, p), nil }))
	if rtd {
		out = append(out, list(versionsSitemap, func(ctx context.Context) ([]siteVersion, error) { return w.rtdSitemapVersions(ctx, root) }))
	}
	return out
}

// mikeHintRe finds Material for MkDocs' version provider in its inline
// configuration.
var mikeHintRe = regexp.MustCompile(`"provider"\s*:\s*"mike"`)

func mikeHinted(p *siteProfile) bool { return mikeHintRe.MatchString(p.inline) }

// isMkDocs reports an MkDocs (or Zensical) build by its generator meta.
func isMkDocs(p *siteProfile) bool {
	g := strings.ToLower(p.meta["generator"])
	return strings.Contains(g, "mkdocs") || strings.Contains(g, "zensical")
}

// profileURL is where p's page was served from, else the root.
func profileURL(root webRoot, p *siteProfile) *url.URL {
	if p.served != nil {
		return p.served
	}
	return root.u
}

// probeJSON fetches a discovery JSON file on root's host into v.
func (w *Web) probeJSON(ctx context.Context, root webRoot, loc string, v any) error {
	body, _, ok := w.probe(ctx, root, loc)
	if !ok {
		return errNoVersionList
	}
	return json.Unmarshal(body, v)
}

// rtdClientVersion is the addons client version sent to the Read the Docs
// API, as its own script would.
const rtdClientVersion = "0.30.0"

// rtdAddons is the part of a Read the Docs addons response read here.
type rtdAddons struct {
	Projects struct {
		Current struct {
			Name           string `json:"name"`
			DefaultVersion string `json:"default_version"`
			Repository     struct {
				URL string `json:"url"`
			} `json:"repository"`
		} `json:"current"`
	} `json:"projects"`
	Versions struct {
		Active []rtdVersion `json:"active"`
	} `json:"versions"`
}

// rtdVersion is one active version in a Read the Docs addons response.
type rtdVersion struct {
	Slug    string          `json:"slug"`
	Type    string          `json:"type"` // "branch" or "tag"
	Hidden  bool            `json:"hidden"`
	Aliases json.RawMessage `json:"aliases"`
	URLs    struct {
		Documentation string `json:"documentation"`
		VCS           string `json:"vcs"`
	} `json:"urls"`
}

// rtdVersions reads the Read the Docs addons API for the project and
// version slugs in p's meta: active versions, and the project's name and
// repository.
func (w *Web) rtdVersions(ctx context.Context, root webRoot, p *siteProfile) (versionSet, error) {
	slug := ""
	if p != nil {
		slug = p.meta["readthedocs-project-slug"]
	}
	if slug == "" {
		return versionSet{}, errNoVersionList
	}
	q := url.Values{"api-version": {"1"}, "client-version": {rtdClientVersion}, "project-slug": {slug}}
	if v := p.meta["readthedocs-version-slug"]; v != "" {
		q.Set("version-slug", v)
	}
	var doc rtdAddons
	if err := w.probeJSON(ctx, root, root.origin()+"/_/addons/?"+q.Encode(), &doc); err != nil {
		return versionSet{}, err
	}
	cur := doc.Projects.Current
	set := versionSet{project: strings.TrimSpace(cur.Name), repoURL: strings.TrimSpace(cur.Repository.URL)}
	for _, v := range doc.Versions.Active {
		if sv, ok := rtdSiteVersion(v, root, cur.DefaultVersion); ok {
			set.versions = append(set.versions, sv)
		}
	}
	if set.versions = tidyVersions(set.versions); len(set.versions) > 0 {
		set.source = versionsRTD
	}
	return set, nil
}

// rtdSiteVersion maps an active Read the Docs version; the project's
// default version is the current one. A hidden version, or one whose docs
// are not on the root's host, is skipped.
func rtdSiteVersion(v rtdVersion, root webRoot, defaultSlug string) (siteVersion, bool) {
	doc, err := url.Parse(v.URLs.Documentation)
	if v.Hidden || v.Slug == "" || err != nil || !root.sameHost(doc) {
		return siteVersion{}, false
	}
	doc.Fragment, doc.RawQuery = "", ""
	sv := siteVersion{label: v.Slug, root: doc.String(), current: v.Slug == defaultSlug}
	return withConcreteLabel(sv, rtdConcrete(v)), true
}

// rtdConcrete returns the release an alias version stands for, when Read
// the Docs says: an alias entry, or for a tag (stable is the newest tag)
// the tag its source link names (…/tree/9.1.1/).
func rtdConcrete(v rtdVersion) string {
	if isNumericLabel(v.Slug) {
		return ""
	}
	var aliases []struct {
		Slug string `json:"slug"`
	}
	if json.Unmarshal(v.Aliases, &aliases) == nil {
		for _, a := range aliases {
			if l, ok := segmentLabel(a.Slug); ok && isNumericLabel(l) {
				return l
			}
		}
	}
	if u, err := url.Parse(v.URLs.VCS); err == nil && v.Type == "tag" {
		if l, ok := segmentLabel(path.Base(strings.TrimSuffix(u.Path, "/"))); ok && isNumericLabel(l) {
			return l
		}
	}
	return ""
}

// mikeEntry is one entry of mike's versions.json.
type mikeEntry struct {
	Version string   `json:"version"`
	Title   string   `json:"title"`
	Aliases []string `json:"aliases"`
}

// versionParent returns the directory holding root's version segment and
// the root's path below that segment: /fastapi-users/ and "" for
// /fastapi-users/latest/. A root with no version segment is its own
// parent.
func versionParent(root webRoot) (dir *url.URL, tail string) {
	_, seg := rootVersion(root)
	if seg < 0 {
		return root.u, ""
	}
	segs := pathSegments(root.prefix)
	u := *root.u
	u.Path, u.RawPath = segmentsPath(segs[:seg]), ""
	if rest := segs[seg+1:]; len(rest) > 0 {
		tail = strings.Join(rest, "/") + "/"
	}
	return &u, tail
}

// mikeVersions reads mike's versions.json at the parent of root's version
// segment.
func (w *Web) mikeVersions(ctx context.Context, root webRoot) ([]siteVersion, error) {
	dir, tail := versionParent(root)
	var entries []mikeEntry
	if err := w.probeJSON(ctx, root, dir.String()+"versions.json", &entries); err != nil {
		return nil, err
	}
	var out []siteVersion
	for _, e := range entries {
		if !safeSegment(e.Version) {
			continue
		}
		u := *dir
		u.Path = dir.Path + e.Version + "/" + tail
		out = append(out, siteVersion{label: e.Version, aliases: e.Aliases, root: u.String()})
	}
	markCurrent(out, "latest", "stable", labelCurrent)
	return tidyVersions(out), nil
}

// safeSegment reports a value usable as one URL path segment.
func safeSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\?#")
}

// markCurrent marks, unless a version already is current, the first
// version carrying the first of labels any version carries, else the first
// release.
func markCurrent(vs []siteVersion, labels ...string) {
	if slices.ContainsFunc(vs, func(v siteVersion) bool { return v.current }) {
		return
	}
	for _, want := range labels {
		for i := range vs {
			if hasVersionLabel(vs[i], want) {
				vs[i].current = true
				return
			}
		}
	}
	for i := range vs {
		if isNumericLabel(vs[i].label) {
			vs[i].current = true
			return
		}
	}
}

var (
	// switcherURLRe finds the pydata-sphinx-theme switcher JSON in a page's
	// inline scripts: DOCUMENTATION_OPTIONS.theme_switcher_json_url, or
	// json_url in a JSON configuration.
	switcherURLRe = regexp.MustCompile(`(?:theme_switcher_json_url|"json_url")\s*[=:]\s*["']([^"']+)["']`)
	// switcherMatchRe finds the switcher's label for the page's own version.
	switcherMatchRe = regexp.MustCompile(`(?:theme_switcher_version_match|"version_match")\s*[=:]\s*["']([^"']*)["']`)
)

// switcherEntry is one entry of a pydata switcher JSON.
type switcherEntry struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	URL       string `json:"url"`
	Preferred bool   `json:"preferred"`
}

// switcherVersions reads the pydata-sphinx-theme switcher JSON whose URL
// p's inline scripts name. The preferred entry is the current version.
func (w *Web) switcherVersions(ctx context.Context, root webRoot, p *siteProfile) ([]siteVersion, error) {
	m := switcherURLRe.FindStringSubmatch(p.inline)
	if m == nil {
		return nil, errNoVersionList
	}
	loc, err := profileURL(root, p).Parse(m[1])
	if err != nil {
		return nil, errNoVersionList
	}
	var entries []switcherEntry
	if err := w.switcherJSON(ctx, root, loc, &entries); err != nil {
		return nil, err
	}
	var out []siteVersion
	for _, e := range entries {
		if v, ok := switcherVersion(e, root); ok {
			out = append(out, v)
		}
	}
	if m := switcherMatchRe.FindStringSubmatch(p.inline); m != nil {
		markCurrent(out, m[1])
	}
	markCurrent(out)
	return tidyVersions(out), nil
}

// switcherJSON reads the switcher JSON at loc into v: a discovery probe
// on root's host, or, where the page names one on another public host
// (scipy's docs keep theirs with the development docs on github.io), that
// file, as long as it is JSON. Only entries on root's host are versions
// either way (switcherVersion), so another host can add no docs root.
func (w *Web) switcherJSON(ctx context.Context, root webRoot, loc *url.URL, v any) error {
	if root.sameHost(loc) {
		return w.probeJSON(ctx, root, loc.String(), v)
	}
	if !isPublicWebURL(loc) {
		return errNoVersionList
	}
	pctx, cancel := context.WithTimeout(ctx, w.probeTimeout)
	defer cancel()
	raw, err := w.fetcher.FetchRaw(pctx, loc.String())
	if err != nil || looksLikeHTML(raw.Body, raw.ContentType) {
		return errNoVersionList
	}
	return json.Unmarshal(raw.Body, v)
}

// switcherVersion maps a switcher entry on root's host. Its label is the
// entry's version (numpy writes "doc/1.26"), its name's first word, or its
// URL's last segment; a different label in the URL (stable) is an alias.
func switcherVersion(e switcherEntry, root webRoot) (siteVersion, bool) {
	u, err := root.u.Parse(strings.TrimSpace(e.URL))
	if err != nil || !root.sameHost(u) {
		return siteVersion{}, false
	}
	u.Fragment, u.RawQuery = "", ""
	last := path.Base(strings.TrimSuffix(u.Path, "/"))
	name, _, _ := strings.Cut(strings.TrimSpace(e.Name), " ")
	label := ""
	for _, c := range []string{path.Base(strings.TrimSpace(e.Version)), name, last} {
		if l, ok := segmentLabel(c); ok {
			label = l
			break
		}
	}
	if label == "" {
		return siteVersion{}, false
	}
	v := siteVersion{label: label, root: u.String(), current: e.Preferred}
	if l, ok := segmentLabel(last); ok && normLabel(l) != normLabel(label) {
		v.aliases = []string{l}
	}
	return v, true
}

// selectVersions reads a version <select> menu on p whose option values
// are version labels rather than URLs (fastify.dev's "latest", "v4.29.x"):
// the switcher's script puts the chosen value where the root's own version
// segment is, or below an unversioned root. The selected option is the
// root's own version. An option's text may name the release it stands for
// ("latest (v5.12.8)", "v4.29.1"), which becomes its label, the value an
// alias. A menu needs two options that read as versions; one whose values
// are URLs is read as links instead (appendOptionLinks), and a root whose
// version is a host label takes none. Nil when no menu qualifies.
func selectVersions(root webRoot, p *siteProfile) []siteVersion {
	if p == nil || root.u == nil {
		return nil
	}
	if label, seg := rootVersion(root); label != "" && seg < 0 {
		return nil
	}
	for _, menu := range p.selects {
		if out := menuVersions(root, menu); len(tidyVersions(out)) >= 2 {
			markCurrent(out, "latest", "stable", labelCurrent)
			return tidyVersions(out)
		}
	}
	return nil
}

// menuVersions reads one menu's options as versions of root (see
// selectVersions); nil when an option is a link or a path.
func menuVersions(root webRoot, menu []selectOption) []siteVersion {
	_, seg := rootVersion(root)
	var out []siteVersion
	for _, o := range menu {
		if o.linked || strings.ContainsAny(o.value, "/?#") {
			return nil
		}
		l, ok := segmentLabel(o.value)
		if !ok || !safeSegment(o.value) || isBareInt(l) && optionRelease(o.text) == "" {
			continue // a bare number (a page-size menu's 10, 25) needs a release in its text
		}
		u := *root.u
		u.RawPath, u.RawQuery, u.Fragment = "", "", ""
		switch segs := pathSegments(root.prefix); {
		case seg >= 0:
			segs[seg] = o.value
			u.Path = segmentsPath(segs)
		case !o.selected:
			u.Path = root.prefix + o.value + "/"
		}
		v := siteVersion{label: l, root: u.String(), current: o.selected}
		out = append(out, withConcreteLabel(v, optionRelease(o.text)))
	}
	return out
}

// optionRelease returns the release an option's text names ("v5.12.8" in
// "latest (v5.12.8)"), or "".
func optionRelease(text string) string {
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == '(' || r == ')' || r == ',' || r == '[' || r == ']' }) {
		if l := linkLabel(tok); l != "" {
			return l
		}
	}
	return ""
}

// siblingVersions reads versions from p's links: same host (or a version
// host label beside root's), the same segments before root's version
// position, a different version label there.
//
// Under a root with no version the position is the segment below the root
// (after a locale); bare integers there need two integer siblings, and a
// lone alias (go.dev/doc/ links /doc/devel/) is not a version list. The
// root itself is the current version, labelled by its own segment, else by
// the release its version meta names, or the text of a link to itself
// (Docusaurus links /docs/ as "3.10.2"), else "current". Nil when no
// sibling is found.
func siblingVersions(root webRoot, p *siteProfile) []siteVersion {
	if p == nil || root.u == nil {
		return nil
	}
	label, seg := rootVersion(root)
	sibs := pathSiblings(root, label, seg, p.links)
	sibs = append(sibs, hostSiblings(root, label, seg, p.links)...)
	if len(sibs) == 0 || label == "" && !enoughSiblings(sibs) {
		return nil
	}
	self := siteVersion{label: label, root: root.String(), current: true}
	if !isNumericLabel(label) {
		self = withConcreteLabel(self, firstNonEmpty(metaVersion(p), selfLinkLabel(root, p.links)))
	}
	if self.label == "" {
		self.label = labelCurrent
	}
	if slices.ContainsFunc(sibs, func(v siteVersion) bool { return v.current }) {
		self.current = false // the unversioned site beside a versioned host
	}
	return foldStable(tidyVersions(append([]siteVersion{self}, sibs...)))
}

// indexChild returns the current docs a version index links below
// itself: a root with no version of its own (numpy.org/doc/,
// matplotlib.org/) whose versions include a child root labelled stable,
// current or latest, in that order of preference. Such a root is a picker
// or a landing page, not docs. ok is false for any other root.
func indexChild(root webRoot, vs versionSet) (siteVersion, bool) {
	if root.u == nil {
		return siteVersion{}, false
	}
	if label, _ := rootVersion(root); label != "" {
		return siteVersion{}, false
	}
	for _, want := range []string{"stable", labelCurrent, "latest"} {
		for _, v := range vs.versions {
			if isChildRoot(root, v.root) && hasVersionLabel(v, want) {
				return v, true
			}
		}
	}
	return siteVersion{}, false
}

// isChildRoot reports raw as a root strictly below root.
func isChildRoot(root webRoot, raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && root.contains(u) && linkKey(u) != linkKey(root.u)
}

// foldStable makes a "stable" or "current" sibling an alias of the
// current version when that is the newest release listed: Django's
// /en/6.1/ links /en/stable/, which is 6.1. ("latest" is left alone: on
// Read the Docs it is the development build.) A child of an unversioned
// root (matplotlib.org/ links /stable/) is not folded: it is a docs tree
// of its own, which indexChild moves to.
func foldStable(vs []siteVersion) []siteVersion {
	if len(vs) == 0 || !vs[0].current {
		return vs
	}
	top, ok := parseVerNum(vs[0].label)
	newer := func(v siteVersion) bool { n, ok := parseVerNum(v.label); return ok && compareVer(n, top) > 0 }
	if !ok || slices.ContainsFunc(vs[1:], newer) {
		return vs // the alias may be a newer release
	}
	first := vs[0]
	first.aliases = slices.Clone(first.aliases)
	out := []siteVersion{first}
	self, _ := parseWebRoot(first.root)
	for _, v := range vs[1:] {
		child := self.u != nil && rootVersionless(self) && isChildRoot(self, v.root)
		if (v.label == "stable" || v.label == labelCurrent) && !child {
			out[0].aliases = append(out[0].aliases, v.label)
			continue
		}
		out = append(out, v)
	}
	return out
}

// rootVersionless reports a root whose URL names no version.
func rootVersionless(root webRoot) bool {
	label, _ := rootVersion(root)
	return label == ""
}

// enoughSiblings reports siblings that make a version list: a release
// among them, or two different aliases.
func enoughSiblings(sibs []siteVersion) bool {
	return slices.ContainsFunc(sibs, func(v siteVersion) bool { return isNumericLabel(v.label) }) ||
		len(tidyVersions(sibs)) >= 2
}

// pathSiblings finds sibling version roots by path (see siblingVersions).
func pathSiblings(root webRoot, label string, seg int, links []siteLink) []siteVersion {
	segs := pathSegments(root.prefix)
	at := siblingPos{prefix: segs, pos: len(segs)}
	if label != "" && seg >= 0 {
		at = siblingPos{prefix: segs[:seg], pos: seg, tail: segs[seg+1:], own: true, label: label}
	}
	var found, ints []siteVersion
	for _, l := range links {
		v, ok := at.sibling(root, l)
		switch {
		case !ok:
		case isBareInt(v.label) && !at.own:
			ints = append(ints, v)
		default:
			found = append(found, v)
		}
	}
	if len(tidyVersions(ints)) >= 2 {
		found = append(found, ints...)
	}
	return found
}

// siblingPos is where pathSiblings looks for a version segment.
type siblingPos struct {
	prefix []string // the root's segments before the version position
	pos    int      // the version position: len(prefix)
	tail   []string // the root's segments after its version segment
	own    bool     // the position is the root's own version segment
	label  string   // the root's own label there, when own
}

// sibling reads l as a sibling version root, or reports false. A link to
// the version root itself may name its release in its text.
func (at siblingPos) sibling(root webRoot, l siteLink) (siteVersion, bool) {
	u := l.url
	if u == nil || !root.sameHost(u) {
		return siteVersion{}, false
	}
	ls := pathSegments(u.Path)
	if len(ls) <= at.pos || !equalFoldSegments(ls[:at.pos], at.prefix) {
		return siteVersion{}, false
	}
	i := at.pos
	if !at.own && len(ls) > i+1 && localeRe.MatchString(ls[i]) {
		i++ // a locale before the version: /en/4x/ below expressjs.com/
	}
	label, ok := segmentLabel(ls[i])
	if !ok || at.own && normLabel(label) == normLabel(at.label) {
		return siteVersion{}, false
	}
	vroot := *root.u
	vroot.Path, vroot.RawPath = segmentsPath(append(slices.Clone(ls[:i+1]), at.tail...)), ""
	v := siteVersion{label: label, root: vroot.String()}
	if len(ls) == i+1 {
		v = withConcreteLabel(v, linkLabel(l.text))
	}
	return v, true
}

// hostSiblings finds sibling versions on version host labels: v2.vite.dev
// beside vite.dev, or vite.dev (the current docs) and v4.vite.dev beside
// v5.vite.dev.
func hostSiblings(root webRoot, label string, seg int, links []siteLink) []siteVersion {
	host := scopeHost(root.u)
	base := host
	if label != "" && seg < 0 {
		_, base, _ = strings.Cut(host, ".")
	}
	var out []siteVersion
	for _, l := range links {
		if l.url == nil || !isWebScheme(l.url.Scheme) {
			continue
		}
		h := scopeHost(l.url)
		sib := siteVersion{label: labelCurrent, current: true}
		switch {
		case h == host:
			continue
		case h == base:
		case strings.HasSuffix(h, "."+base):
			lab, ok := hostLabel(l.url)
			if !ok || strings.Contains(strings.TrimSuffix(h, "."+base), ".") || label != "" && normLabel(lab) == normLabel(label) {
				continue
			}
			sib = siteVersion{label: lab}
		default:
			continue
		}
		sib.root = (&url.URL{Scheme: root.u.Scheme, Host: h, Path: root.prefix}).String()
		out = append(out, sib)
	}
	return out
}

// versionMetaNames are the <meta> names a page states its own docs
// version in: DocSearch's (Docusaurus, pydata-sphinx-theme) and
// Docusaurus's own.
var versionMetaNames = []string{"docsearch:version", "docusaurus_version"}

// metaVersion returns the release p's meta says the page documents, or "".
func metaVersion(p *siteProfile) string {
	for _, name := range versionMetaNames {
		if l := linkLabel(p.meta[name]); l != "" {
			return l
		}
	}
	return ""
}

// selfLinkLabel returns the release a link to the root itself is labelled
// with, or "".
func selfLinkLabel(root webRoot, links []siteLink) string {
	key := linkKey(root.u)
	for _, l := range links {
		if l.url != nil && linkKey(l.url) == key {
			if label := linkLabel(l.text); label != "" {
				return label
			}
		}
	}
	return ""
}

// linkLabel returns a link text that is a release label ("3.10.2",
// "v2.1"), or "".
func linkLabel(text string) string {
	text = collapseSpace(text)
	if l, ok := segmentLabel(text); ok && isNumericLabel(l) && !strings.ContainsAny(text, "~^=@") {
		return l
	}
	return ""
}

// equalFoldSegments compares path segments, ignoring case.
func equalFoldSegments(a, b []string) bool {
	return slices.EqualFunc(a, b, strings.EqualFold)
}

// rtdSitemapVersions reads the version roots a Read the Docs sitemap lists
// (/en/latest/, /en/stable/, /en/9.0.x/), in the root's locale.
func (w *Web) rtdSitemapVersions(ctx context.Context, root webRoot) ([]siteVersion, error) {
	body, base, ok := w.probe(ctx, root, root.origin()+"/sitemap.xml")
	if !ok {
		return nil, errNoVersionList
	}
	_, locs := parseSitemap(gunzipMaybe(body), 500)
	locale := ""
	if segs := pathSegments(root.prefix); len(segs) > 0 && localeRe.MatchString(segs[0]) {
		locale = segs[0]
	}
	var out []siteVersion
	for _, loc := range locs {
		if v, ok := rtdSitemapVersion(loc, base, root, locale); ok {
			out = append(out, v)
		}
	}
	markCurrent(out, "stable", "latest")
	return tidyVersions(out), nil
}

// rtdSitemapVersion reads one sitemap entry as a version root: /<label>/
// or /<locale>/<label>/, in locale when that is set.
func rtdSitemapVersion(loc string, base *url.URL, root webRoot, locale string) (siteVersion, bool) {
	u, err := base.Parse(strings.TrimSpace(loc))
	if err != nil || !root.sameHost(u) {
		return siteVersion{}, false
	}
	segs := pathSegments(u.Path)
	switch {
	case len(segs) == 1:
	case len(segs) == 2 && localeRe.MatchString(segs[0]) && (locale == "" || strings.EqualFold(segs[0], locale)):
	default:
		return siteVersion{}, false
	}
	label, ok := segmentLabel(segs[len(segs)-1])
	if !ok {
		return siteVersion{}, false
	}
	u.Fragment, u.RawQuery = "", ""
	return siteVersion{label: label, root: u.String()}, true
}

// addSphinxBuild fills in what a Sphinx build says about itself: the
// concrete label of the root's version when that is an alias, or the one
// version when the site gave no list, and the project name when no other
// source gave one.
func (w *Web) addSphinxBuild(ctx context.Context, root webRoot, p *siteProfile, set *versionSet) {
	own := slices.IndexFunc(set.versions, func(v siteVersion) bool { return sameRootURL(v.root, root) })
	needLabel := len(set.versions) == 0 || own >= 0 && !isNumericLabel(set.versions[own].label)
	if !needLabel && set.project != "" {
		return
	}
	project, version, ok := w.sphinxBuild(ctx, root, p)
	if !ok {
		return
	}
	set.project = firstNonEmpty(set.project, project)
	switch {
	case !needLabel || !isNumericLabel(version):
	case len(set.versions) == 0:
		label, _ := rootVersion(root)
		set.versions = []siteVersion{withConcreteLabel(siteVersion{label: label, root: root.String(), current: true}, version)}
		set.source = versionsSphinx
	default:
		set.versions[own] = withConcreteLabel(set.versions[own], version)
	}
}

// sphinxBuild reads a Sphinx build's project and version: objects.inv's
// header, else documentation_options.js's VERSION. ok is false when p is
// not a Sphinx page or neither file says.
func (w *Web) sphinxBuild(ctx context.Context, root webRoot, p *siteProfile) (project, version string, ok bool) {
	s, ok := detectSphinx(profileURL(root, p), p.meta["generator"], p.scripts)
	if !ok {
		return "", "", false
	}
	if body, _, ok := w.probe(ctx, root, s.root.String()+"objects.inv"); ok {
		project, version, _ = parseObjectsInvHeader(body)
	}
	if version == "" {
		version = w.sphinxOptions(ctx, root, s).version
	}
	return project, version, project != "" || version != ""
}

// sameRootURL reports whether raw is root, a trailing slash or "www." aside.
func sameRootURL(raw string, root webRoot) bool {
	u, err := url.Parse(raw)
	return err == nil && linkKey(u) == linkKey(root.u)
}

// withConcreteLabel relabels v with the release concrete, keeping its old
// label as an alias. An empty concrete leaves v as it is.
func withConcreteLabel(v siteVersion, concrete string) siteVersion {
	if concrete == "" || normLabel(concrete) == normLabel(v.label) {
		return v
	}
	if v.label != "" && !hasVersionLabel(siteVersion{aliases: v.aliases}, v.label) {
		v.aliases = append([]string{v.label}, v.aliases...)
	}
	v.label = concrete
	return v
}

// tidyVersions drops versions with no label and repeated labels (the first
// stays, current if any copy was) and moves the current version first;
// only the first current one stays current.
func tidyVersions(vs []siteVersion) []siteVersion {
	var out []siteVersion
	pos := map[string]int{}
	for _, v := range vs {
		key := normLabel(v.label)
		if key == "" {
			continue
		}
		if i, ok := pos[key]; ok {
			out[i].current = out[i].current || v.current
			continue
		}
		pos[key] = len(out)
		out = append(out, v)
	}
	if cur := slices.IndexFunc(out, func(v siteVersion) bool { return v.current }); cur > 0 {
		v := out[cur]
		out = slices.Insert(slices.Delete(out, cur, cur+1), 0, v)
	}
	for i := 1; i < len(out); i++ {
		out[i].current = false
	}
	return out
}

// versionLabels returns v's label and aliases.
func versionLabels(v siteVersion) []string {
	return append([]string{v.label}, v.aliases...)
}

// hasVersionLabel reports whether v carries label, as label or alias.
func hasVersionLabel(v siteVersion, label string) bool {
	want := normLabel(label)
	for _, l := range versionLabels(v) {
		if l != "" && normLabel(l) == want {
			return true
		}
	}
	return false
}

// normLabel is the form labels compare in: lower case, no semver operator,
// no "v" before a digit.
func normLabel(s string) string {
	s = strings.TrimLeft(strings.ToLower(strings.TrimSpace(s)), "~^=")
	if len(s) > 1 && s[0] == 'v' && s[1] >= '0' && s[1] <= '9' {
		s = s[1:]
	}
	return s
}

// verNum is a numeric version label taken apart: 5.2.3 is [5 2 3]; 2.x
// and 4x are [2] and [4] with wild set; 1.0rc1 is [1 0] with pre "rc1".
type verNum struct {
	parts []int
	wild  bool
	pre   string
}

// parseVerNum reads a label as a release number (see normLabel for what
// is ignored); ok is false for labels that are not numeric (stable, main,
// devdocs).
func parseVerNum(label string) (verNum, bool) {
	var n verNum
	s := normLabel(label)
	for {
		part, rest, ok := leadingInt(s)
		if !ok {
			break
		}
		n.parts, s = append(n.parts, part), rest
		if s == "x" || s == ".x" {
			n.wild, s = true, ""
			break
		}
		if len(s) < 2 || s[0] != '.' || s[1] < '0' || s[1] > '9' {
			break
		}
		s = s[1:]
	}
	if len(n.parts) == 0 {
		return verNum{}, false
	}
	// Build metadata (+incompatible, Go's mark for a v2+ release of a
	// module without a /vN path; +wasi-0.2) does not make a pre-release.
	s, _, _ = strings.Cut(s, "+")
	n.pre = strings.TrimLeft(s, "-.")
	return n, true
}

// leadingInt splits the decimal number at the start of s from the rest.
func leadingInt(s string) (int, string, bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	v, err := strconv.Atoi(s[:i])
	if i == 0 || err != nil {
		return 0, s, false
	}
	return v, s[i:], true
}

// verPrefixMatch reports whether one version is a prefix of the other on
// dot boundaries (5 and 5.2.1; 5.2.3 and 5.2; 2.5 and 2.x): the same major,
// never across one.
func verPrefixMatch(a, b verNum) bool {
	n := min(len(a.parts), len(b.parts))
	return n > 0 && slices.Equal(a.parts[:n], b.parts[:n])
}

// compareVer orders releases: by number, missing parts as 0, then a
// release above its pre-releases.
func compareVer(a, b verNum) int {
	for i := range max(len(a.parts), len(b.parts)) {
		if c := cmp.Compare(partAt(a, i), partAt(b, i)); c != 0 {
			return c
		}
	}
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	}
	return strings.Compare(a.pre, b.pre)
}

func partAt(n verNum, i int) int {
	if i < len(n.parts) {
		return n.parts[i]
	}
	return 0
}

// matchVersion picks the version want names (plan §6): an exact label or
// alias; an alias class (latest/stable/current to the current version,
// dev/next/main/master/canary to the dev one); else a numeric prefix on dot
// boundaries in either direction, the highest match, never across a major.
func matchVersion(want string, versions []siteVersion) (siteVersion, bool) {
	w := normLabel(want)
	if w == "" {
		return siteVersion{}, false
	}
	for _, v := range versions {
		if hasVersionLabel(v, w) {
			return v, true
		}
	}
	if class := versionAliasClass[w]; class != "" {
		return classVersion(class, versions)
	}
	wn, ok := parseVerNum(w)
	if !ok {
		return siteVersion{}, false
	}
	var best siteVersion
	var bestN verNum
	found := false
	for _, v := range versions {
		for _, l := range versionLabels(v) {
			n, ok := parseVerNum(l)
			if ok && verPrefixMatch(wn, n) && (!found || compareVer(n, bestN) > 0) {
				best, bestN, found = v, n, true
			}
		}
	}
	return best, found
}

// classVersion returns the version an alias class names: the current one
// (by flag, else by label), or the one labelled as development docs.
func classVersion(class string, versions []siteVersion) (siteVersion, bool) {
	if class == "current" {
		if i := slices.IndexFunc(versions, func(v siteVersion) bool { return v.current }); i >= 0 {
			return versions[i], true
		}
	}
	for _, v := range versions {
		for _, l := range versionLabels(v) {
			if versionAliasClass[normLabel(l)] == class {
				return v, true
			}
		}
	}
	return siteVersion{}, false
}

// versionHolds is matchVersion's post-check on a version root's served URL:
// it still carries a label that want matches. It catches alias redirects
// and docs.rs's floating semver (/tokio/1.38/ serving 1.54.1).
//
// A numeric want holds while the URL carries a release it matches on dot
// boundaries (1.38 in 1.38.2); a current alias while the URL carries that
// alias or any release (stable served as /en/6.1/); a dev alias while it
// carries any dev alias (main served as /en/dev/).
func versionHolds(want string, served *url.URL) bool {
	w := normLabel(want)
	if served == nil || w == "" {
		return false
	}
	wn, numeric := parseVerNum(w)
	class := versionAliasClass[w]
	for _, l := range urlLabels(served) {
		n, isNum := parseVerNum(l)
		nl := normLabel(l)
		switch {
		case nl == w,
			numeric && isNum && verPrefixMatch(wn, n),
			class == "dev" && versionAliasClass[nl] == class,
			class == "current" && isNum:
			return true
		}
	}
	return false
}

// versionRootHolds is the post-check for v, the version matchVersion
// picked for want, whose root was served from served. A root served where
// it was asked for holds: an alias root labelled from its build (click's
// /en/stable/ is 8.5.x) carries no release to check. One that redirected
// must still carry want, or one of v's labels (docs.rs ~1.38 serving
// 1.38.2), so /tokio/1.38/ serving 1.54.1 and /en/5.2/ redirecting to
// /en/stable/ fail.
func versionRootHolds(want string, v siteVersion, served *url.URL) bool {
	if served == nil {
		return false
	}
	if sameRootURL(v.root, webRoot{u: served}) {
		return true
	}
	return slices.ContainsFunc(append([]string{want}, versionLabels(v)...), func(l string) bool { return versionHolds(l, served) })
}

// urlLabels returns the version labels in u's path segments and first
// host label.
func urlLabels(u *url.URL) []string {
	var out []string
	for _, s := range pathSegments(u.Path) {
		if l, ok := segmentLabel(s); ok {
			out = append(out, l)
		}
	}
	if l, ok := hostLabel(u); ok {
		out = append(out, l)
	}
	return out
}

// sameMajor reports whether want and version share a major version, for
// accepting want on a site that publishes no versions.
func sameMajor(want, version string) bool {
	a, okA := parseVerNum(want)
	b, okB := parseVerNum(version)
	return okA && okB && a.parts[0] == b.parts[0]
}

// namesRelease reports want naming release on dot boundaries, no more
// precisely than release itself: 7 and 7.1 name 7.1.2, 1.38 does not name
// 1.54.1, and 7.1.2.4 is not named by 7.1.2.
func namesRelease(want, release string) bool {
	w, okW := parseVerNum(want)
	r, okR := parseVerNum(release)
	return okW && okR && len(w.parts) <= len(r.parts) && verPrefixMatch(w, r)
}

// dottedLabels returns versions with their two-digit labels also read as
// releases, "14" as "1.4", when the site's scheme is known to be that: one
// of its labels is the latest release's major and minor written without
// the dot (SQLAlchemy's 21 for 2.1.6). Otherwise, or with no release to
// check against, nil: a bare 14 may as well be release 14.
func dottedLabels(versions []siteVersion, latest string) []siteVersion {
	l, ok := parseVerNum(latest)
	if !ok || len(l.parts) < 2 || l.parts[0] > 9 || l.parts[1] > 9 {
		return nil
	}
	dotless := strconv.Itoa(l.parts[0]) + strconv.Itoa(l.parts[1])
	if !slices.ContainsFunc(versions, func(v siteVersion) bool { return hasVersionLabel(v, dotless) }) {
		return nil
	}
	out := make([]siteVersion, 0, len(versions))
	for _, v := range versions {
		v.aliases = slices.Clone(v.aliases)
		for _, label := range versionLabels(v) {
			if n := normLabel(label); len(n) == 2 && isBareInt(n) {
				v.aliases = append(v.aliases, n[:1]+"."+n[1:])
			}
		}
		out = append(out, v)
	}
	return out
}

// styledLabel writes want's release in the style of a label the site
// uses: 4x beside 5x, v3 beside v4, 2.x beside 3.x, 4.2 beside 5.1. ok is
// false when like is no release or want has fewer parts than like shows.
func styledLabel(like, want string) (string, bool) {
	l, okL := parseVerNum(like)
	w, okW := parseVerNum(want)
	if !okL || !okW || len(w.parts) < len(l.parts) || l.pre != "" {
		return "", false
	}
	prefix := ""
	if t := strings.TrimLeft(strings.ToLower(like), "~^="); strings.HasPrefix(t, "v") {
		prefix = "v"
	}
	suffix := ""
	if l.wild {
		suffix = strings.TrimPrefix(strings.ToLower(like), strings.ToLower(prefix+joinParts(l.parts)))
	}
	return prefix + joinParts(w.parts[:len(l.parts)]) + suffix, true
}

// registryVersionRoot returns the root of p's registry-hosted docs at the
// version want names: docs.rs /<crate>/~<maj.min>/<ident>/ (or the exact
// version), pkg.go.dev /<module>@<the full version from goVersions>.
// goVersions is nil except for Go modules.
//
// docs.rs reads a bare 1.38 as ^1.38 and serves 1.54.1, so anything short
// of a full version asks for ~; "latest" and the other current aliases
// keep the latest docs. Neither registry hosts development docs.
func registryVersionRoot(p pkgInfo, want string, goVersions []string) (siteVersion, bool) {
	u, err := url.Parse(p.registryDocs)
	if p.registryDocs == "" || err != nil || normLabel(want) == "" {
		return siteVersion{}, false
	}
	switch scopeHost(u) {
	case "docs.rs":
		return docsRSVersionRoot(u, want)
	case "pkg.go.dev":
		return pkgGoVersionRoot(u, want, goVersions)
	}
	return siteVersion{}, false
}

// docsRSVersionRoot puts want's version segment into a docs.rs URL.
func docsRSVersionRoot(u *url.URL, want string) (siteVersion, bool) {
	segs := pathSegments(u.Path)
	w := normLabel(want)
	n, numeric := parseVerNum(w)
	var seg, label string
	switch {
	case len(segs) < 2:
		return siteVersion{}, false
	case versionAliasClass[w] == "current":
		seg, label = "latest", "latest"
	case !numeric:
		return siteVersion{}, false
	case len(n.parts) >= 3 && !n.wild:
		seg, label = w, w
	default:
		label = joinParts(n.parts)
		seg = "~" + label
	}
	segs[1] = seg
	out := *u
	out.Path, out.RawPath, out.RawQuery, out.Fragment = segmentsPath(segs), "", "", ""
	return siteVersion{label: label, root: out.String(), current: seg == "latest"}, true
}

// joinParts writes version parts back as 1.38.
func joinParts(parts []int) string {
	s := make([]string, len(parts))
	for i, p := range parts {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, ".")
}

// pkgGoVersionRoot points a pkg.go.dev URL at the highest published
// release want names (a pre-release only when want is one). With no
// version list a full version is taken as given.
func pkgGoVersionRoot(u *url.URL, want string, goVersions []string) (siteVersion, bool) {
	out := *u
	out.Path = strings.TrimSuffix(out.Path, "/")
	if i := strings.LastIndexByte(out.Path, '@'); i > strings.LastIndexByte(out.Path, '/') {
		out.Path = out.Path[:i]
	}
	out.RawPath, out.RawQuery, out.Fragment = "", "", ""
	base := out.String()
	w := normLabel(want)
	if versionAliasClass[w] == "current" {
		return siteVersion{label: "latest", root: base, current: true}, true
	}
	wn, ok := parseVerNum(w)
	if !ok {
		return siteVersion{}, false
	}
	best := ""
	var bestN verNum
	for _, v := range goVersions {
		n, ok := parseVerNum(v)
		if ok && verPrefixMatch(wn, n) && (n.pre == "" || wn.pre != "") && (best == "" || compareVer(n, bestN) > 0) {
			best, bestN = v, n
		}
	}
	if best == "" && len(goVersions) == 0 && len(wn.parts) == 3 {
		best = "v" + w
	}
	if best == "" {
		return siteVersion{}, false
	}
	return siteVersion{label: best, root: base + "@" + best}, true
}

// goModuleVersions finds the module that publishes the release want
// names, for registry docs of Go module pkg, and returns pkg moved to it
// with that module's versions (newest first). Under semantic import
// versioning a major version from 2 on is its own module, base/vN
// (gopkg.in/name.vN), and v0 and v1 are the base path, so go-redis@9 is
// github.com/redis/go-redis/v9 and echo/v4@3 is github.com/labstack/echo
// (its v3 releases tagged +incompatible). The module want's major implies
// is tried first, then pkg's own; with neither listing such a release,
// pkg and its own versions come back unchanged.
func (w *Web) goModuleVersions(ctx context.Context, pkg pkgInfo, want string) (pkgInfo, []string) {
	if pkg.eco != ecoGo || w.pkgs == nil {
		return pkg, nil
	}
	wn, numeric := parseVerNum(want)
	var own []string
	for _, mod := range uniqueStrings(append(goMajorModules(pkg.name, want), pkg.name)...) {
		actx, cancel := context.WithTimeout(ctx, apiCallTimeout)
		versions, _ := w.pkgs.goVersions(actx, mod)
		cancel()
		if mod == pkg.name {
			own = versions
		}
		names := slices.ContainsFunc(versions, func(v string) bool { n, ok := parseVerNum(v); return ok && verPrefixMatch(wn, n) })
		if numeric && names && mod != pkg.name {
			moved := pkg
			moved.name, moved.registryDocs = mod, pkgsiteBase+"/"+mod
			return moved, versions
		}
	}
	return pkg, own
}

// goMajorModules returns the module paths that may carry want's major
// version (see goModuleVersions), best first: base/vN then the base path,
// where a v2+ release made before modules is tagged +incompatible; the
// base path alone for v0 and v1. Nil when want names no release or module
// is not a module path.
func goMajorModules(module, want string) []string {
	n, ok := parseVerNum(want)
	if !ok || !isModulePath(module) {
		return nil
	}
	major := strconv.Itoa(n.parts[0])
	dir, last := path.Split(module)
	if strings.HasPrefix(module, "gopkg.in/") {
		name, _, _ := strings.Cut(last, ".v")
		return []string{dir + name + ".v" + major}
	}
	base := module
	if dir != "" && goMajorSuffix(last) {
		base = strings.TrimSuffix(dir, "/")
	}
	if n.parts[0] < 2 {
		return []string{base}
	}
	return []string{base + "/v" + major, base}
}

// goMajorSuffix reports a module path element that is a major version
// suffix: v2, v3, … (never v0 or v1).
func goMajorSuffix(elem string) bool {
	n, err := strconv.Atoi(strings.TrimPrefix(elem, "v"))
	return strings.HasPrefix(elem, "v") && err == nil && n >= 2 && "v"+strconv.Itoa(n) == elem
}

// releaseLister is a package source that lists a package's releases
// (pkgClient.releases).
type releaseLister interface {
	releases(ctx context.Context, eco ecosystem, name string) ([]string, error)
}

// releaseProbes are where a site that names its version trees by release
// (chartjs.org/docs/3.9.1/, docs.celeryq.dev/en/v4.4.7/) and lists none
// of them in its HTML would keep want's docs: the newest release the
// project's npm or PyPI package publishes that want names (a pre-release
// only when want is one), written as the release, v and the release, and
// the same for its major.minor, put where the root's own version segment
// is, or below an unversioned root. Like every probe, each counts only
// when it is served as a docs tree that holds want (probeVersion). At
// most webMaxVersionProbes, none already in have; nil when the registry
// lists no such release.
func (w *Web) releaseProbes(ctx context.Context, root webRoot, proj *project, want string, have []versionProbe) []versionProbe {
	label, seg := rootVersion(root)
	if root.u == nil || label != "" && seg < 0 {
		return nil
	}
	newest := w.newestRelease(ctx, proj, want)
	n, ok := parseVerNum(newest)
	if !ok {
		return nil
	}
	labels := []string{normLabel(newest), "v" + normLabel(newest)}
	if len(n.parts) > 2 {
		mm := joinParts(n.parts[:2])
		labels = append(labels, mm, "v"+mm)
	}
	seen := map[string]bool{}
	for _, p := range have {
		seen[linkKey(p.page)] = true
	}
	var out []versionProbe
	for _, l := range labels {
		u := *root.u
		u.RawPath, u.RawQuery, u.Fragment = "", "", ""
		if segs := pathSegments(root.prefix); seg >= 0 {
			segs[seg] = l
			u.Path = segmentsPath(segs)
		} else {
			u.Path = root.prefix + l + "/"
		}
		if !seen[linkKey(&u)] && len(out) < webMaxVersionProbes {
			seen[linkKey(&u)] = true
			out = append(out, versionProbe{v: siteVersion{label: l, root: u.String()}, page: &u})
		}
	}
	return out
}

// newestRelease is the newest release of proj's npm or PyPI package that
// want names (a pre-release only when want is one), or "".
func (w *Web) newestRelease(ctx context.Context, proj *project, want string) string {
	lister, ok := w.pkgs.(releaseLister)
	wn, numeric := parseVerNum(want)
	if !ok || !numeric || proj == nil {
		return ""
	}
	i := slices.IndexFunc(proj.pkgs, func(p pkgInfo) bool { return p.eco == ecoNPM || p.eco == ecoPyPI })
	if i < 0 {
		return ""
	}
	actx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	releases, err := lister.releases(actx, proj.pkgs[i].eco, proj.pkgs[i].name)
	cancel()
	if err != nil {
		return ""
	}
	for _, r := range releases { // newest first
		if n, ok := parseVerNum(r); ok && namesRelease(want, r) && (n.pre == "" || wn.pre != "") {
			return r
		}
	}
	return ""
}

// canonicalRoot adopts a same-host redirect of requested that only swaps
// or adds a version segment (/en/stable/ to /en/6.1/, / to /en/stable/),
// returning the root at served's version; ok is false for any other
// redirect.
//
// An added version segment may bring a locale before it (Read the Docs
// sends / to /en/latest/). A served URL naming a page (index.html) stands
// for its directory, and one that changes only the scheme or "www." is the
// requested root as served.
func canonicalRoot(requested webRoot, served *url.URL) (webRoot, bool) {
	if served == nil || requested.u == nil || !requested.sameHost(served) {
		return webRoot{}, false
	}
	got, err := parseWebRoot(served.String())
	if err != nil {
		return webRoot{}, false
	}
	req, out := pathSegments(requested.prefix), pathSegments(got.prefix)
	switch {
	case equalFoldSegments(req, out):
	case len(out) == len(req):
		if !swapsVersion(req, out) {
			return webRoot{}, false
		}
	case !addsVersion(req, out):
		return webRoot{}, false
	}
	return got, true
}

// swapsVersion reports paths of one length that differ in exactly one
// segment, a version label in both.
func swapsVersion(req, out []string) bool {
	diff := -1
	for i := range req {
		if strings.EqualFold(req[i], out[i]) {
			continue
		}
		if diff >= 0 {
			return false
		}
		diff = i
	}
	if diff < 0 {
		return false
	}
	_, okReq := segmentLabel(req[diff])
	_, okOut := segmentLabel(out[diff])
	return okReq && okOut
}

// addsVersion reports out as req with a version segment, or a locale and a
// version segment, inserted at one place.
func addsVersion(req, out []string) bool {
	extra := len(out) - len(req)
	if extra < 1 || extra > 2 {
		return false
	}
	i := 0
	for i < len(req) && strings.EqualFold(req[i], out[i]) {
		i++
	}
	if !equalFoldSegments(req[i:], out[i+extra:]) || extra == 2 && !localeRe.MatchString(out[i]) {
		return false
	}
	_, ok := segmentLabel(out[i+extra-1])
	return ok
}

// labelResults sets each result's Version: the version segment of its URL
// below root, else rootLabel. It fetches nothing.
func labelResults(results []Result, root webRoot, rootLabel string) {
	for i := range results {
		results[i].Version = rootLabel
		u, err := url.Parse(results[i].URL)
		if err != nil || root.u == nil || !root.contains(u) {
			continue
		}
		if l := pageVersion(u, root); l != "" {
			results[i].Version = l
		}
	}
}

// pageVersion returns the version label in u's first path segment below
// the root (after a locale), bare integers aside, or "".
func pageVersion(u *url.URL, root webRoot) string {
	rel := u.Path
	if root.prefix != "/" {
		rel = strings.TrimPrefix(rel, root.prefix)
	}
	segs := pathSegments(rel)
	if len(segs) > 1 && localeRe.MatchString(segs[0]) {
		segs = segs[1:]
	}
	if len(segs) == 0 {
		return ""
	}
	if l, ok := segmentLabel(segs[0]); ok && !isBareInt(l) {
		return l
	}
	return ""
}
