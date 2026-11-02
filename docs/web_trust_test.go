package docs

import "testing"

func TestScore(t *testing.T) {
	ev := func(kind evidenceKind, detail string) evidence { return evidence{kind: kind, detail: detail} }
	cases := []struct {
		name  string
		evs   []evidence
		trust float64
		ok    bool
	}{
		{"nothing", nil, 0, false},
		{"package docs alone", []evidence{ev(evPkgDocs, "pypi x")}, 4, true},
		{"a nav chain alone is too weak", []evidence{ev(evNavChain, "npm x")}, 2, false},
		{"a nav chain per source", []evidence{ev(evNavChain, "npm x"), ev(evNavChain, "github o/x")}, 4, true},
		{"equal entries count once", []evidence{ev(evNavChain, "npm x"), ev(evNavChain, "npm x"), ev(evLinksRepo, "l")}, 4, true},
		{"registry docs", []evidence{ev(evRegistryDocs, "docs.rs"), ev(evLinksRepo, "l"), ev(evRepoAgrees, "a")}, 8, true},
		{"clamped to 10", []evidence{ev(evPkgDocs, "a"), ev(evRepoHomepage, "b"), ev(evLinksRepo, "c"), ev(evRepoAgrees, "d"), ev(evSiteNames, "e"), ev(evRTDRepo, "f")}, 10, true},
		{"clamped to 0", []evidence{ev(evDeprecated, "a"), ev(evArchived, "b")}, 0, false},
		{"V4 needs a back-link", []evidence{ev(evRelatedDomain, "d"), ev(evLinksOwner, "o"), ev(evSiteNames, "s")}, 3, false},
		{"V4 with its back-link", []evidence{ev(evRelatedDomain, "d"), ev(evLinksRepo, "l")}, 3, true},
		{"back-links verify nothing alone", []evidence{ev(evLinksRepo, "l"), ev(evLinksOwner, "o"), ev(evSiteNames, "s")}, 4, false},
		{"penalties can sink a match", []evidence{ev(evPkgHomepage, "h"), ev(evDeprecated, "d")}, 1, false},
		{"notes add nothing", []evidence{ev(evPkgHomepage, "h"), ev(evNote, "github: rate limited")}, 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trust, ok := score(tc.evs)
			if trust != tc.trust || ok != tc.ok {
				t.Errorf("score = %.1f, %v; want %.1f, %v", trust, ok, tc.trust, tc.ok)
			}
		})
	}
	// Every table row is a whole or half point, so trust is always on the
	// 0.5 grid.
	for kind, pts := range trustPoints {
		if pts*2 != float64(int(pts*2)) {
			t.Errorf("%s: %.2f points is off the 0.5 grid", kind, pts)
		}
	}
}
