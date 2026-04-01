package crawl

import (
	"net/url"
	"testing"
)

func TestProfileForSeedDetectsEntra(t *testing.T) {
	profile := profileForSeed("https://learn.microsoft.com/en-us/entra/identity/saas-apps")
	if profile == nil || profile.Name != "entra" {
		t.Fatalf("profileForSeed() = %#v", profile)
	}
}

func TestDiscoverTOCURLs(t *testing.T) {
	toc := `{"items":[{"href":"/en-us/entra/identity/saas-apps/tutorial-one"},{"children":[{"href":"tutorial-two"}]}]}`
	urls := discoverTOCURLs(toc, "https://learn.microsoft.com/en-us/entra/identity/saas-apps/")
	if len(urls) != 2 {
		t.Fatalf("discoverTOCURLs() = %#v", urls)
	}
}

func TestOktaProfileRejectsNonEnglishLocales(t *testing.T) {
	profile := oktaProfile()
	u, _ := parseURL("https://help.okta.com/fr-fr/content/topics/example")
	if profile.AllowURL(u) {
		t.Fatal("AllowURL() = true, want false for non-English Okta locale")
	}
}

func TestOktaProfileRejectsNestedNonEnglishLocales(t *testing.T) {
	profile := oktaProfile()
	u, _ := parseURL("https://help.okta.com/oie/ja-jp/content/topics/example.htm")
	if profile.AllowURL(u) {
		t.Fatal("AllowURL() = true, want false for nested non-English Okta locale")
	}
}

func TestOktaProfileAllowsSamlDocHost(t *testing.T) {
	profile := oktaProfile()
	u, _ := parseURL("https://saml-doc.okta.com/SAML_Docs/How-to-Configure-SAML-2.0-for-Cisco-ASA-VPN.html")
	if !profile.AllowURL(u) {
		t.Fatal("AllowURL() = false, want true for saml-doc Okta host")
	}
	if profileForSeed("https://saml-doc.okta.com") == nil || profileForSeed("https://saml-doc.okta.com").Name != "okta" {
		t.Fatal("profileForSeed() did not detect saml-doc Okta host")
	}
}

func TestEntraProfileRestrictsPath(t *testing.T) {
	profile := entraProfile()
	u, _ := parseURL("https://learn.microsoft.com/en-us/entra/other/path")
	if profile.AllowURL(u) {
		t.Fatal("AllowURL() = true, want false for non-SaaS path")
	}
}

func TestEntraProfileUsesTOCWithoutDefaultSeeds(t *testing.T) {
	profile := entraProfile()
	if len(profile.DefaultSeeds) != 0 {
		t.Fatalf("DefaultSeeds = %#v, want none to avoid redirect duplicates", profile.DefaultSeeds)
	}
	if profile.TOCURL == "" {
		t.Fatal("TOCURL is empty, want TOC-based discovery")
	}
}

func TestPingProfileRejectsNonEnglishLocales(t *testing.T) {
	profile := pingProfile()
	u, _ := parseURL("https://docs.pingidentity.com/r/fr-fr/pingfederate-120/example")
	if profile.AllowURL(u) {
		t.Fatal("AllowURL() = true, want false for non-English Ping locale")
	}
}

func TestPingProfileCapsConcurrency(t *testing.T) {
	profile := pingProfile()
	if profile.MaxConcurrency != 1 {
		t.Fatalf("MaxConcurrency = %d, want 1", profile.MaxConcurrency)
	}
}

func TestPingProfileIncludesConfigurationGuideSeed(t *testing.T) {
	profile := pingProfile()
	found := false
	for _, seed := range profile.DefaultSeeds {
		if seed == "https://docs.pingidentity.com/configuration_guides/" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("DefaultSeeds = %#v, want configuration_guides seed", profile.DefaultSeeds)
	}
}

func TestPingProfileIncludesMicrosoft365ConfigSeed(t *testing.T) {
	profile := pingProfile()
	found := false
	for _, seed := range profile.DefaultSeeds {
		if seed == "https://docs.pingidentity.com/configuration_guides/microsoft_365/config_saml_o365_pf.html" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("DefaultSeeds = %#v, want Microsoft 365 SAML config seed", profile.DefaultSeeds)
	}
}

func TestPingAllowDiscoveredURLStaysWithinBucket(t *testing.T) {
	parent, _ := parseURL("https://docs.pingidentity.com/configuration_guides/microsoft_365/config_saml_o365_pf.html")
	sameSection, _ := parseURL("https://docs.pingidentity.com/configuration_guides/microsoft_365/other.html")
	otherSection, _ := parseURL("https://docs.pingidentity.com/integrations/duosecurity/pf_is_overview_of_duo_security_integrations.html")
	disallowedSection, _ := parseURL("https://docs.pingidentity.com/pingfederate/latest/release_notes/pf_release_notes.html")

	if !pingAllowDiscoveredURL(parent, sameSection) {
		t.Fatal("pingAllowDiscoveredURL() = false, want true for same Ping section")
	}
	if pingAllowDiscoveredURL(parent, otherSection) {
		t.Fatal("pingAllowDiscoveredURL() = true, want false for different Ping section")
	}
	if pingAllowDiscoveredURL(parent, disallowedSection) {
		t.Fatal("pingAllowDiscoveredURL() = true, want false for disallowed Ping section")
	}
	root, _ := parseURL("https://docs.pingidentity.com")
	if !pingAllowDiscoveredURL(root, otherSection) {
		t.Fatal("pingAllowDiscoveredURL() = false, want root pages to branch into allowed sections")
	}
	if pingAllowDiscoveredURL(root, disallowedSection) {
		t.Fatal("pingAllowDiscoveredURL() = true, want false when branching into disallowed section")
	}
}

func TestPingProfileRejectsDisallowedSections(t *testing.T) {
	profile := pingProfile()
	u, _ := parseURL("https://docs.pingidentity.com/pingfederate/latest/release_notes/pf_release_notes.html")
	if profile.AllowURL(u) {
		t.Fatal("AllowURL() = true, want false for disallowed Ping section")
	}
}

func parseURL(raw string) (*url.URL, error) {
	return url.Parse(raw)
}
