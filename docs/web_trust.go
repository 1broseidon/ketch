// Owner: chunk B (resolve core).

package docs

import "math"

// minTrust is the lowest trust a verified root is accepted with; strict
// query splits ask for strictMinTrust.
const (
	minTrust       = 3.0
	strictMinTrust = 5.0
	maxTrust       = 10.0
)

// trustPoints is plan §5's table: what each kind of evidence adds to a
// root's TrustScore. evNote adds nothing.
var trustPoints = map[evidenceKind]float64{
	evPkgDocs:       4,
	evPkgHomepage:   3,
	evRepoHomepage:  3,
	evNavChain:      2,
	evRegistryDocs:  5,
	evRelatedDomain: 1,
	evLinksRepo:     2,
	evLinksOwner:    1,
	evRepoAgrees:    1,
	evSiteNames:     1,
	evRTDRepo:       1,
	evArchived:      -1,
	evDeprecated:    -2,
	evNote:          0,
}

// verifyingKinds are the evidence kinds that verify a root on their own
// (V1, V2 and V3); evRelatedDomain verifies only beside evLinksRepo (V4).
var verifyingKinds = map[evidenceKind]bool{
	evPkgDocs: true, evPkgHomepage: true, evRepoHomepage: true, evNavChain: true, evRegistryDocs: true,
}

// score sums evs by trustPoints, counting equal entries once, clamped to
// 0..10 and rounded to 0.5. ok reports a verified root (a V1, V2 or V3
// kind, or evRelatedDomain with evLinksRepo) scoring at least minTrust.
func score(evs []evidence) (trust float64, ok bool) {
	seen := map[evidence]bool{}
	sum := 0.0
	verified, related, links := false, false, false
	for _, e := range evs {
		if seen[e] {
			continue
		}
		seen[e] = true
		sum += trustPoints[e.kind]
		verified = verified || verifyingKinds[e.kind]
		related = related || e.kind == evRelatedDomain
		links = links || e.kind == evLinksRepo
	}
	trust = math.Round(math.Min(math.Max(sum, 0), maxTrust)*2) / 2
	return trust, (verified || related && links) && trust >= minTrust
}

// hasEvidence reports whether evs holds an entry of kind.
func hasEvidence(evs []evidence, kind evidenceKind) bool {
	for _, e := range evs {
		if e.kind == kind {
			return true
		}
	}
	return false
}

// uniqueEvidence drops repeated entries, keeping the first of each.
func uniqueEvidence(evs []evidence) []evidence {
	var out []evidence
	seen := map[evidence]bool{}
	for _, e := range evs {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}
