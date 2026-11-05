package docs

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/1broseidon/ketch/internal/testutil"
	"github.com/1broseidon/ketch/search"
)

// Wrong-project and wrong-kind answers from keyless resolution (task-27):
// (a) a package's README links its parent framework's or a dependency's
// docs, and that other project's site was taken for the package's; (b) a
// docs root landing on a site's home page took its blog, news and other
// post sections for docs. Every library and host here is invented; the
// worlds are in testdata/keylesssafety/<case>, served under their real
// hostnames by a FixtureTransport.

// textFetcher is fixtureFetcher with the page text a converter would give:
// the title, and the headings, paragraphs, list items and code in order.
type textFetcher struct{ fixtureFetcher }

func (f *textFetcher) FetchPage(ctx context.Context, u string) (PageContent, error) {
	raw, err := f.FetchRaw(ctx, u)
	if err != nil {
		return PageContent{}, err
	}
	if !looksLikeHTML(raw.Body, raw.ContentType) {
		return PageContent{Markdown: string(raw.Body), URL: raw.URL}, nil
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw.Body))
	if err != nil {
		return PageContent{}, err
	}
	var md strings.Builder
	doc.Find("h1, h2, p, li, pre").Each(func(_ int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		switch goquery.NodeName(s) {
		case "h1":
			md.WriteString("# " + text + "\n\n")
		case "h2":
			md.WriteString("## " + text + "\n\n")
		case "pre":
			md.WriteString("```\n" + text + "\n```\n\n")
		case "li":
			md.WriteString("- " + text + "\n")
		default:
			md.WriteString(text + "\n\n")
		}
	})
	return PageContent{Title: doc.Find("title").Text(), Markdown: md.String(), URL: raw.URL}, nil
}

// safetyWorld is a keyless backend over testdata/keylesssafety/<name>:
// the registries and sites as files laid out for testutil.LoadDir (what
// no file covers answers 404), and .search-hits.txt (a dotfile, which
// LoadDir skips), the web search's results in order. No working-directory
// hint.
func safetyWorld(t *testing.T, name string) (*Web, *testutil.FixtureTransport) {
	t.Helper()
	dir := filepath.Join("testdata", "keylesssafety", name)
	ft := testutil.NewFixtureTransport()
	if err := ft.LoadDir(dir); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: notFoundFallback{ft}}
	pkgs := newPkgClient(client)
	pkgs.retryWait = time.Millisecond
	w := NewWeb(&textFetcher{fixtureFetcher{client: client}})
	w.pkgs = pkgs
	w.popularity = newPopularityClient(pkgs)
	var results []search.Result
	if hits, err := os.ReadFile(filepath.Join(dir, ".search-hits.txt")); err == nil {
		for _, u := range strings.Fields(string(hits)) {
			results = append(results, search.Result{URL: u})
		}
	}
	w.sites = &fakeSites{hits: results}
	w.githubOn = func() bool { return false }
	w.workdirHint = func() []ecosystem { return nil }
	return w, ft
}

// (a) An extension's README links its framework's docs, whose sidebar
// lists the extension's package and repository among the framework's
// ecosystem. The README's link and the site's link to the package made
// two edges, and the framework's docs answered for the extension. The
// site names itself after the framework (its title, its host) and links
// the framework's repository: another project's docs, never the
// package's, so the name is refused.
func TestKeylessReadmeFrameworkDocsRefused(t *testing.T) {
	w, _ := safetyWorld(t, "extension-readme")
	log := &resolveLog{}
	matches, err := w.resolveLogged(context.Background(), mustSpec(t, "pypi:gridiron-vault"), 0, log)
	if err == nil {
		t.Fatalf("resolved %v: the framework's docs answered for its extension", matchIDs(matches))
	}
	assertReasons(t, log, "https://docs.gridiron.example/ names another project (gridiron)")
}

// isBlogLike reports a result URL in a blog-like section.
func isBlogLike(u string) bool {
	for _, s := range []string{"/blog/", "/news/", "/posts/", "/announcements/", "/showcase/", "/community/", "/2019/08/"} {
		if strings.Contains(u, s) {
			return true
		}
	}
	return false
}

// (b) A project whose docs are its site's root, beside a blog, news,
// announcements, a showcase, community posts and dated posts, all in its
// sitemap: the posts name the query more often than the docs page does.
// Pages in blog-like sections below the root are not docs: never indexed.
func TestBlogSectionsAreNotDocs(t *testing.T) {
	// `ketch docs index`, by name and by docs URL, leaves the posts out.
	for _, lib := range []string{"pypi:quillmark", "https://quillmark.example/"} {
		w, _ := safetyWorld(t, "org-blog-root")
		store := newStore(t)
		r, err := w.index(context.Background(), store, IndexRequest{Library: lib})
		if err != nil {
			t.Fatalf("index %s: %v", lib, err)
		}
		pages, err := store.Pages(r.Library.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(pages) == 0 {
			t.Errorf("index %s: no pages", lib)
		}
		for u := range pages {
			if isBlogLike(u) {
				t.Errorf("index %s: indexed post %s", lib, u)
			}
		}
	}
}

// (b) A documentation URL naming a site's home page, a landing page whose
// docs nav leads into /docs/ beside a blog: the docs root is the docs
// section, as for a homepage (docsRootsFor), and the blog's post on the
// query never answers.
func TestHomePageRootMovesToDocsSection(t *testing.T) {
	w, _ := safetyWorld(t, "landing-docs-section")
	m, err := w.resolveOne(context.Background(), mustSpec(t, "pypi:inkpot"), false)
	if err != nil || m.root.String() != "https://inkpot.example/docs/" {
		t.Fatalf("resolveOne = %s, %v; want the docs section", m.root, err)
	}
}

// (a) An npm package's README links a dependency's docs, which a web
// search for the package finds (an integrations page listing it): the
// README's link and the site's link to the npm page made two edges. The
// site is the dependency's, after the repository it links.
func TestKeylessReadmeDependencySearchHitRefused(t *testing.T) {
	w, _ := safetyWorld(t, "dependency-readme-search")
	log := &resolveLog{}
	matches, err := w.resolveLogged(context.Background(), mustSpec(t, "npm:sparrow-forms"), 0, log)
	if err == nil {
		t.Fatalf("resolved %v: a dependency's docs answered for the package", matchIDs(matches))
	}
	assertReasons(t, log, "https://formkit.example.org/docs/ names another project (formkit)")
}

// Blog-like paths below a root are out of its scope, unless the root is
// itself in such a section; a page merely named like one stays.
func TestBlogLikePaths(t *testing.T) {
	for _, tc := range []struct {
		root, page string
		want       bool // in scope
	}{
		{"https://x.example/", "https://x.example/guide/intro", true},
		{"https://x.example/", "https://x.example/blog/", false},
		{"https://x.example/", "https://x.example/blog/2021/hello", false},
		{"https://x.example/", "https://x.example/news/release", false},
		{"https://x.example/", "https://x.example/posts/tips", false},
		{"https://x.example/", "https://x.example/announcements/beta", false},
		{"https://x.example/", "https://x.example/showcase/apps", false},
		{"https://x.example/", "https://x.example/community/meetup", false},
		{"https://x.example/", "https://x.example/2019/08/why", false},
		{"https://x.example/", "https://x.example/en/blog/hello", false},
		{"https://x.example/docs/", "https://x.example/docs/v2/news/x", false},
		{"https://x.example/blog/", "https://x.example/blog/2021/hello", true},
		{"https://x.example/news/", "https://x.example/news/release", true},
		{"https://x.example/", "https://x.example/docs/2019/config", true},   // a year alone is no post date
		{"https://x.example/", "https://x.example/api/news.html", true},      // a page about news
		{"https://x.example/", "https://x.example/guide/community.md", true}, // a page, not a section
		{"https://x.example/", "https://x.example/releases/3.0", true},
	} {
		root := mustRoot(t, tc.root)
		if got := root.contains(mustURL(t, tc.page)); got != tc.want {
			t.Errorf("%s contains %s = %v, want %v", tc.root, tc.page, got, tc.want)
		}
	}
}

// The site-identity rule for a README edge: the README corroborates a
// site that names the package or links back to it, and never one that
// names another project it links (its title, llms.txt H1 or host after a
// repository or package that is not this one's).
func TestReadmeSiteIdentity(t *testing.T) {
	p := &project{pkgs: []pkgInfo{{eco: ecoPyPI, name: "gridiron-vault"}}, repo: repo("vaultsmith", "gridiron-vault")}
	own := "https://github.com/vaultsmith/gridiron-vault"
	fw := "https://github.com/gridiron/gridiron"
	for _, tc := range []struct {
		name, root, title, llmsH1 string
		links                     []string
		want                      string // "" when the README corroborates
	}{
		{"names the package", "https://vault.example.dev/", "gridiron-vault documentation", "", []string{own, fw}, ""},
		{"links back, generic title", "https://vault.example.dev/", "Docs", "", []string{own}, ""},
		{"names it in words, links the framework", "https://vault.example.dev/", "Gridiron Vault", "", []string{fw}, ""},
		{"titled after the framework it links", "https://docs.example.dev/", "Gridiron Documentation", "", []string{own, fw}, "gridiron"},
		{"llms.txt H1 after the framework", "https://docs.example.dev/", "Docs", "Gridiron", []string{own, fw}, "gridiron"},
		{"host after the framework", "https://docs.gridiron.example/", "Docs", "", []string{own, fw}, "gridiron"},
		{"after a linked package", "https://docs.example.dev/", "Ironworks guide", "", []string{own, "https://pypi.org/project/ironworks/"}, "ironworks"},
		{"names nothing, links nothing of the package", "https://docs.example.dev/", "Docs", "", []string{fw}, "?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := mustRoot(t, tc.root)
			prof := &siteProfile{requested: root.u, served: root.u, title: tc.title, meta: map[string]string{}}
			for _, l := range tc.links {
				prof.links = append(prof.links, siteLink{url: mustURL(t, l)})
			}
			llms := llmsTxt{title: tc.llmsH1}
			got, ok := readmeSiteCheck(root, prof, llms, p)
			switch {
			case tc.want == "" && !ok:
				t.Errorf("refused (%s), want corroborated", got)
			case tc.want == "?" && ok:
				t.Error("corroborated a site with nothing of the package")
			case tc.want != "" && tc.want != "?" && (ok || !strings.Contains(got, tc.want)):
				t.Errorf("readmeSiteCheck = %q, %v; want a refusal naming %s", got, ok, tc.want)
			}
		})
	}
}

// A root reached only through the README never comes before the
// package's own docs: those its metadata names, or registry-hosted docs.
func TestPickRootsReadmeOnlyComesLast(t *testing.T) {
	readme := webMatch{root: mustRoot(t, "https://site.example.dev/"), evidence: []evidence{
		{kind: evPkgReadme, detail: "README"}, {kind: evLinksRepo, detail: "links"}, {kind: evSiteNames, detail: "title"},
	}}
	nav := webMatch{root: mustRoot(t, "https://own.example.dev/docs/"), evidence: []evidence{{kind: evNavChain, detail: "homepage links"}}}
	reg := webMatch{root: mustRoot(t, "https://docs.rs/x/latest/x/"), registry: true, evidence: []evidence{{kind: evRegistryDocs, detail: "docs.rs"}}}
	for _, m := range []*webMatch{&readme, &nav, &reg} {
		m.trust, _ = score(m.evidence)
	}
	if got := pickRoots([]webMatch{readme, nav}); got[0].root.String() != nav.root.String() {
		t.Errorf("pickRoots put %s first, want the metadata's %s", got[0].root, nav.root)
	}
	if got := pickRoots([]webMatch{readme, reg}); got[0].root.String() != reg.root.String() {
		t.Errorf("pickRoots put %s first, want the registry docs", got[0].root)
	}
	if got := pickRoots([]webMatch{readme}); len(got) != 1 || got[0].root.String() != readme.root.String() {
		t.Errorf("pickRoots alone = %v", got)
	}
}

// The legitimate case beside (a): an extension's README links both its
// own docs and its framework's. Its own site names it and links its
// repository (and the framework's: it documents an extension of it); the
// framework's site, listing the extension, names the framework. The
// extension's own docs answer, and only they verify.
func TestKeylessReadmeOwnDocsBesideFramework(t *testing.T) {
	w, _ := safetyWorld(t, "extension-own-docs")
	log := &resolveLog{}
	matches, err := w.resolveLogged(context.Background(), mustSpec(t, "pypi:gridiron-cache"), 0, log)
	if err != nil || len(matches) != 1 || matches[0].root.String() != "https://gridiron-cache.example.dev/" {
		t.Fatalf("resolve = %v, %v (rejected %v)", matchIDs(matches), err, log.rejected)
	}
	assertReasons(t, log, "https://docs.gridiron.example/ names another project (gridiron)")
}
