package docs

import (
	"context"
	"fmt"
)

// A version pin constrains the source; missing terms or an unreadable
// release must never send the caller to another version or another site.
func (s *SearchFirst) readPinned(ctx, cctx context.Context, req sfRequest, hits []sfHit, prefs sitePrefs, tr *sfTrace) Outcome {
	m := prefs.resolved.match
	if m == nil || m.version == "" {
		err := prefs.resolved.err
		if err == nil {
			err = ErrNotFound
		}
		return s.failed(ctx, cctx, fmt.Errorf("search: could not confirm docs for %s; pass the versioned docs URL with --library: %w", req.spec.raw, err))
	}
	req.version = m.version
	site := resolvedSite(m, chooseSites(docsHits(hits), prefs))
	tr.domains++
	o, read := s.readSite(ctx, cctx, site, s.hitProfile(cctx, site), req, verifiedOrigin(matchEvidence(m)), tr)
	return s.keep(req, read, o)
}
