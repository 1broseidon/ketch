package docs

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Ecosystem hints (design/docs-keyless.md, Pipeline 4): which registry a
// bare name most likely means, from the words of the query and from the
// project files in the working directory. An assistant asking about click
// from a Python project means PyPI's click. A hint never verifies anything
// and never adds a candidate; it only narrows a same-name contest between
// projects that verified, before the dominance rule, and only where one
// library is needed.

// queryEcoWords are query words that name an ecosystem.
var queryEcoWords = map[string]ecosystem{
	"python": ecoPyPI, "pypi": ecoPyPI, "pip": ecoPyPI,
	"npm": ecoNPM, "nodejs": ecoNPM, "javascript": ecoNPM, "typescript": ecoNPM,
	"rust": ecoCrates, "cargo": ecoCrates, "crate": ecoCrates, "crates": ecoCrates,
	"golang": ecoGo,
}

// queryEcosystems are the ecosystems a query's words name.
func queryEcosystems(query string) []ecosystem {
	var out []ecosystem
	for _, w := range strings.Fields(strings.ToLower(query)) {
		w = strings.Trim(w, ".,;:!?()[]{}\"'`")
		if eco, ok := queryEcoWords[w]; ok && !slices.Contains(out, eco) {
			out = append(out, eco)
		}
	}
	return out
}

// manifestFiles are the project files that put a directory in an
// ecosystem.
var manifestFiles = []struct {
	name string
	eco  ecosystem
}{
	{"package.json", ecoNPM},
	{"pyproject.toml", ecoPyPI}, {"setup.py", ecoPyPI}, {"setup.cfg", ecoPyPI}, {"requirements.txt", ecoPyPI}, {"Pipfile", ecoPyPI},
	{"Cargo.toml", ecoCrates},
	{"go.mod", ecoGo},
}

// workdirEcosystems are the ecosystems of the project files in the
// working directory itself (not its parents: a hint must come from the
// project the caller is in, not a repository it happens to sit inside).
func workdirEcosystems() []ecosystem {
	dir, err := os.Getwd()
	if err != nil {
		return nil
	}
	return dirEcosystems(dir)
}

// dirEcosystems are the ecosystems of dir's project files.
func dirEcosystems(dir string) []ecosystem {
	var out []ecosystem
	for _, m := range manifestFiles {
		if st, err := os.Stat(filepath.Join(dir, m.name)); err == nil && st.Mode().IsRegular() && !slices.Contains(out, m.eco) {
			out = append(out, m.eco)
		}
	}
	return out
}

type ecoHintKey struct{}

// withQueryHint carries the ecosystems a bare query's own words name to
// its resolution.
func withQueryHint(ctx context.Context, ecos []ecosystem) context.Context {
	if len(ecos) == 0 {
		return ctx
	}
	return context.WithValue(ctx, ecoHintKey{}, ecos)
}

// ecoHint is the hint for one resolution: the query's words first, else
// the working directory's project files. A hint naming every registry is
// none.
func (w *Web) ecoHint(ctx context.Context) []ecosystem {
	if ecos, ok := ctx.Value(ecoHintKey{}).([]ecosystem); ok && len(ecos) > 0 {
		return ecos
	}
	if w.workdirHint == nil {
		return nil
	}
	return w.workdirHint()
}

// projectEcosystems are the registries of a project's packages.
func projectEcosystems(p *project) []ecosystem {
	if p == nil {
		return nil
	}
	var out []ecosystem
	for _, pkg := range p.pkgs {
		if !slices.Contains(out, pkg.eco) {
			out = append(out, pkg.eco)
		}
	}
	return out
}

// hinted reports a project with a package in one of ecos.
func hinted(p *project, ecos []ecosystem) bool {
	for _, e := range projectEcosystems(p) {
		if slices.Contains(ecos, e) {
			return true
		}
	}
	return false
}
