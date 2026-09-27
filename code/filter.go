package code

import (
	"fmt"
	"regexp"
	"strings"
)

// repoName matches owner/name: two path segments of the characters code
// hosts allow in owner and repository names.
var repoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// NormalizeRepo reduces a repository reference to the owner/name form every
// backend filters on. Besides owner/name it accepts the github.com forms an
// agent tends to have at hand: https://github.com/owner/name, with or without
// the scheme, a trailing slash or .git. An empty reference means no filter.
// Anything else is ErrInvalidRepo, because a filter that quietly matched
// nothing would read as "no such code".
func NormalizeRepo(ref string) (string, error) {
	repo := strings.TrimSpace(ref)
	if repo == "" {
		return "", nil
	}
	for _, prefix := range []string{"https://", "http://", "www.", "github.com/"} {
		if len(repo) > len(prefix) && strings.EqualFold(repo[:len(prefix)], prefix) {
			repo = repo[len(prefix):]
		}
	}
	repo = strings.TrimSuffix(strings.TrimSuffix(repo, "/"), ".git")
	owner, name, _ := strings.Cut(repo, "/")
	if !repoName.MatchString(repo) || strings.Trim(owner, ".") == "" || strings.Trim(name, ".") == "" {
		return "", fmt.Errorf("%w, e.g. golang/go (got %q)", ErrInvalidRepo, ref)
	}
	return repo, nil
}

// repo returns Repo normalized, so a library caller that passes a URL gets the
// filter the CLI would build, and a malformed value never reaches a backend's
// query dialect.
func (q Query) repo() (string, error) { return NormalizeRepo(q.Repo) }

// qualifierToken matches a scoping qualifier from the GitHub and Sourcegraph
// query dialects written as its own word: repo:owner/name, lang:go, path:src.
// A value opening with a slash, quote or $ (file:///x, path:"/a", repo:$REPO)
// reads as code more often than as a filter, so it is left alone.
var qualifierToken = regexp.MustCompile(`^(?i:repo|org|user|lang|language|path|file|filename|extension):[^\s/"'$]`)

// LiteralQualifiers returns the scoping qualifiers (repo:, lang:, path:, ...)
// written into query when this backend searches the query as literal text.
// Such a backend matches them as code instead of applying them, which usually
// finds nothing, and an empty result reads as "no such code", so callers warn
// about them. A backend that applies qualifiers returns nil.
func (p Provider) LiteralQualifiers(query string) []string {
	if p.Qualifiers {
		return nil
	}
	var found []string
	for _, field := range strings.Fields(query) {
		if qualifierToken.MatchString(field) {
			found = append(found, field)
		}
	}
	return found
}

// MayLackRepo reports whether an empty result for q may mean this backend
// does not index q.Repo, rather than that the repository has no match.
func (p Provider) MayLackRepo(q Query, results []Result) bool {
	return p.PartialIndex && q.Repo != "" && len(results) == 0
}
