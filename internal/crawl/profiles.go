package crawl

import (
	"encoding/json"
	"net/url"
	"strings"
)

type hostProfile struct {
	Name            string
	AllowedHosts    []string
	DefaultSeeds    []string
	SitemapSeedURLs []string
	TOCURL          string
	MaxConcurrency  int
	AllowURL        func(*url.URL) bool
	AllowDiscovered func(*url.URL, *url.URL) bool
	DiscoverHTML    func(string) []string
}

func profileForSeed(seed string) *hostProfile {
	u, err := url.Parse(seed)
	if err != nil {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)

	switch {
	case host == "help.okta.com" || strings.HasSuffix(host, ".help.okta.com") || host == "saml-doc.okta.com" || strings.HasSuffix(host, ".saml-doc.okta.com"):
		return oktaProfile()
	case host == "docs.pingidentity.com" || strings.HasSuffix(host, ".docs.pingidentity.com"):
		return pingProfile()
	case (host == "learn.microsoft.com" || strings.HasSuffix(host, ".learn.microsoft.com")) && strings.Contains(path, "/entra/identity/saas-apps"):
		return entraProfile()
	case host == "duo.com" || strings.HasSuffix(host, ".duo.com") || strings.HasSuffix(host, ".guide.duo.com") || strings.HasSuffix(host, ".resources.duo.com") || strings.HasSuffix(host, ".help.duo.com") || strings.HasSuffix(host, ".demo.duo.com"):
		return duoProfile()
	default:
		return nil
	}
}

func duoProfile() *hostProfile {
	return &hostProfile{
		Name: "duo",
		AllowedHosts: []string{
			"duo.com",
			"guide.duo.com",
			"resources.duo.com",
			"help.duo.com",
			"demo.duo.com",
		},
		DefaultSeeds: []string{
			"https://duo.com",
			"https://duo.com/blog",
			"https://duo.com/docs",
			"https://guide.duo.com",
			"https://resources.duo.com",
			"https://help.duo.com",
			"https://demo.duo.com",
		},
		AllowURL: defaultVendorAllow([]string{
			"duo.com",
			"guide.duo.com",
			"resources.duo.com",
			"help.duo.com",
			"demo.duo.com",
		}),
	}
}

func oktaProfile() *hostProfile {
	allowBase := defaultVendorAllow([]string{"help.okta.com", "saml-doc.okta.com"})
	return &hostProfile{
		Name:         "okta",
		AllowedHosts: []string{"help.okta.com", "saml-doc.okta.com"},
		DefaultSeeds: []string{"https://help.okta.com", "https://saml-doc.okta.com"},
		SitemapSeedURLs: []string{
			"https://help.okta.com/Sitemap-index.xml",
		},
		AllowURL: func(u *url.URL) bool {
			if !allowBase(u) {
				return false
			}
			locale := findLocaleSegment(u.Path)
			return locale == "" || locale == "en-us"
		},
	}
}

// pingAllowedTopSections lists the only Ping top-level path segments we index.
// Excluded: pingdirectory, pingfederate, pingam, pingds, sdks, auth-node-ref,
// pinggateway — these are deep infrastructure/directory/AM docs that are not
// useful for Duo-vs-competitor comparison and add thousands of crawl pages.
var pingAllowedTopSections = []string{
	"integrations",
	"configuration_guides",
	"solution-guides",
	"pingone",
	"pingoneaic",
	"pingid",
	"pingid-user-guide",
	"davinci",
	"connectors",
}

func pingProfile() *hostProfile {
	allowBase := defaultVendorAllow([]string{"docs.pingidentity.com"})
	return &hostProfile{
		Name:         "pingidentity",
		AllowedHosts: []string{"docs.pingidentity.com"},
		// Seeds are section-level entry points only — no root or product-index,
		// which would allow branching into all 26k+ pages of Ping documentation.
		DefaultSeeds: []string{
			"https://docs.pingidentity.com/integrations/",
			"https://docs.pingidentity.com/configuration_guides/",
			"https://docs.pingidentity.com/solution-guides/",
			"https://docs.pingidentity.com/pingone/",
			"https://docs.pingidentity.com/pingoneaic/",
			"https://docs.pingidentity.com/pingid/",
			"https://docs.pingidentity.com/pingid-user-guide/",
			"https://docs.pingidentity.com/davinci/",
			"https://docs.pingidentity.com/connectors/",
			// Direct seed for the Microsoft 365 SAML guide
			"https://docs.pingidentity.com/configuration_guides/microsoft_365/config_saml_o365_pf.html",
		},
		MaxConcurrency: 1,
		AllowURL: func(u *url.URL) bool {
			if !allowBase(u) {
				return false
			}
			locale := findLocaleSegment(u.Path)
			if locale != "" && locale != "en-us" {
				return false
			}
			return isAllowedPingPath(u.Path)
		},
		AllowDiscovered: pingAllowDiscoveredURL,
	}
}

func entraProfile() *hostProfile {
	allowBase := defaultVendorAllow([]string{"learn.microsoft.com"})
	return &hostProfile{
		Name:         "entra",
		AllowedHosts: []string{"learn.microsoft.com"},
		TOCURL:       "https://learn.microsoft.com/en-us/entra/identity/saas-apps/toc.json",
		DiscoverHTML: extractEntraChildURLsFromHTML,
		AllowURL: func(u *url.URL) bool {
			if !allowBase(u) {
				return false
			}
			if !isAllowedEntraSaasPath(u.Path) {
				return false
			}
			locale := findLocaleSegment(u.Path)
			if locale != "" && locale != "en-us" {
				return false
			}
			return true
		},
	}
}

func defaultVendorAllow(hosts []string) func(*url.URL) bool {
	return func(u *url.URL) bool {
		if u == nil {
			return false
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return false
		}
		host := strings.ToLower(u.Hostname())
		allowedHost := false
		for _, candidate := range hosts {
			candidate = strings.ToLower(candidate)
			if host == candidate || strings.HasSuffix(host, "."+candidate) {
				allowedHost = true
				break
			}
		}
		if !allowedHost {
			return false
		}
		path := strings.ToLower(u.Path)
		for _, suffix := range []string{".png", ".jpg", ".jpeg", ".gif", ".svg", ".pdf", ".zip", ".ico"} {
			if strings.HasSuffix(path, suffix) {
				return false
			}
		}
		return !strings.HasPrefix(path, "/cdn-cgi/")
	}
}

func findLocaleSegment(path string) string {
	segments := splitPathSegments(path)
	for _, segment := range segments {
		candidate := strings.ToLower(segment)
		if isLocaleSegment(candidate) {
			return candidate
		}
	}
	return ""
}

func isAllowedEntraSaasPath(path string) bool {
	segments := splitPathSegments(path)
	if len(segments) == 0 {
		return false
	}
	start := 0
	if isLocaleSegment(strings.ToLower(segments[0])) {
		start = 1
	}
	remaining := segments[start:]
	if len(remaining) < 3 {
		return false
	}
	if strings.ToLower(remaining[0]) != "entra" || strings.ToLower(remaining[1]) != "identity" || strings.ToLower(remaining[2]) != "saas-apps" {
		return false
	}
	return len(remaining) <= 4
}

func extractEntraChildURLsFromHTML(html string) []string {
	const marker = "/en-us/entra/identity/saas-apps/"
	lowerHTML := strings.ToLower(html)
	seen := make(map[string]struct{})
	urls := make([]string, 0, 8)
	start := 0
	for {
		idx := strings.Index(lowerHTML[start:], marker)
		if idx == -1 {
			break
		}
		idx += start + len(marker)
		end := idx
		for end < len(lowerHTML) {
			ch := lowerHTML[end]
			if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
				end++
				continue
			}
			break
		}
		slug := lowerHTML[idx:end]
		if slug != "" {
			candidate := "https://learn.microsoft.com/en-us/entra/identity/saas-apps/" + slug
			if _, ok := seen[candidate]; !ok {
				seen[candidate] = struct{}{}
				urls = append(urls, candidate)
			}
		}
		start = end
	}
	return urls
}

func discoverTOCURLs(tocText, base string) []string {
	if strings.TrimSpace(tocText) == "" {
		return nil
	}
	var data any
	if err := json.Unmarshal([]byte(tocText), &data); err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	urls := make([]string, 0, 16)
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			if href, ok := typed["href"].(string); ok {
				resolved := resolveMaybeRelative(base, href)
				if resolved != "" {
					if _, ok := seen[resolved]; !ok {
						seen[resolved] = struct{}{}
						urls = append(urls, resolved)
					}
				}
			}
			for _, value := range typed {
				walk(value)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(data)
	return urls
}

func resolveMaybeRelative(baseURL, href string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	return resolved.String()
}

func splitPathSegments(path string) []string {
	trimmed := strings.Trim(strings.ToLower(path), "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func isLocaleSegment(segment string) bool {
	if len(segment) != 5 || segment[2] != '-' {
		return false
	}
	for _, idx := range []int{0, 1, 3, 4} {
		ch := segment[idx]
		if ch < 'a' || ch > 'z' {
			return false
		}
	}
	return true
}

func pingAllowDiscoveredURL(parent, child *url.URL) bool {
	if parent == nil || child == nil {
		return false
	}
	parentSection := pingTopSection(parent.Path)
	childSection := pingTopSection(child.Path)
	if !isAllowedPingTopSection(childSection) {
		return false
	}
	if parentSection == "" {
		return true
	}
	return parentSection == childSection
}

func pingTopSection(path string) string {
	segments := splitPathSegments(path)
	if len(segments) == 0 {
		return ""
	}
	return segments[0]
}

func isAllowedPingTopSection(section string) bool {
	for _, allowed := range pingAllowedTopSections {
		if section == allowed {
			return true
		}
	}
	return false
}

func isAllowedPingPath(path string) bool {
	section := pingTopSection(path)
	if section == "" {
		return false
	}
	return isAllowedPingTopSection(section)
}
