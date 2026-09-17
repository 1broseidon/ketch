package docstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ManifestPath is the project-relative manifest location.
const ManifestPath = ".ketch/docs.json"

// Attachment declares one library a project depends on: everything `docs
// sync` needs to rebuild it on another machine. It deliberately records the
// add options, not the fetched content.
type Attachment struct {
	Name         string `json:"name"`
	Version      string `json:"version,omitempty"`
	URL          string `json:"url"`
	Prefix       string `json:"prefix,omitempty"`
	ForceSitemap bool   `json:"sitemap,omitempty"`
	MaxPages     int    `json:"max_pages,omitempty"`
	Depth        int    `json:"depth,omitempty"`
}

// Manifest is the on-disk shape of .ketch/docs.json.
type Manifest struct {
	Libraries []Attachment `json:"libraries"`
}

// Project is a directory tree with (or eligible for) a docs manifest.
type Project struct {
	// Root is the directory holding .ketch/. Path is the manifest file.
	Root string
	Path string
	// Exists reports whether the manifest file was present on load.
	Exists   bool
	Manifest Manifest
}

// FindProject walks up from start looking for a directory that holds
// .ketch/docs.json; failing that, the nearest enclosing git repository root
// (which becomes the project on first Attach). The second result is false
// when start is in neither.
func FindProject(start string) (*Project, bool, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, false, err
	}
	var gitRoot string
	for {
		manifest := filepath.Join(dir, ManifestPath)
		if _, err := os.Stat(manifest); err == nil {
			p, err := loadProject(dir)
			return p, err == nil, err
		}
		if gitRoot == "" {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				gitRoot = dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if gitRoot == "" {
		return nil, false, nil
	}
	return &Project{Root: gitRoot, Path: filepath.Join(gitRoot, ManifestPath)}, true, nil
}

func loadProject(root string) (*Project, error) {
	p := &Project{Root: root, Path: filepath.Join(root, ManifestPath), Exists: true}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &p.Manifest); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p.Path, err)
	}
	return p, nil
}

// Names returns the attached library names in manifest order.
func (p *Project) Names() []string {
	names := make([]string, 0, len(p.Manifest.Libraries))
	for _, a := range p.Manifest.Libraries {
		names = append(names, a.Name)
	}
	return names
}

// Attach records a library (replacing an existing entry of the same name)
// and writes the manifest, creating .ketch/ if needed.
func (p *Project) Attach(a Attachment) error {
	if err := ValidateName(a.Name); err != nil {
		return err
	}
	replaced := false
	for i := range p.Manifest.Libraries {
		if p.Manifest.Libraries[i].Name == a.Name {
			p.Manifest.Libraries[i] = a
			replaced = true
		}
	}
	if !replaced {
		p.Manifest.Libraries = append(p.Manifest.Libraries, a)
	}
	sort.SliceStable(p.Manifest.Libraries, func(i, j int) bool { return p.Manifest.Libraries[i].Name < p.Manifest.Libraries[j].Name })
	return p.Save()
}

// Detach removes a library from the manifest and writes it. It reports
// whether the name was present.
func (p *Project) Detach(name string) (bool, error) {
	kept := p.Manifest.Libraries[:0]
	found := false
	for _, a := range p.Manifest.Libraries {
		if a.Name == name {
			found = true
			continue
		}
		kept = append(kept, a)
	}
	if !found {
		return false, nil
	}
	p.Manifest.Libraries = kept
	return true, p.Save()
}

// Save writes the manifest atomically (temp file + rename).
func (p *Project) Save() error {
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return err
	}
	if p.Manifest.Libraries == nil {
		p.Manifest.Libraries = []Attachment{}
	}
	data, err := json.MarshalIndent(p.Manifest, "", "  ")
	if err != nil {
		return err
	}
	tmp := p.Path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p.Path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	p.Exists = true
	return nil
}

// ScopeFor returns the library names a search from dir should default to:
// the project's attached libraries when dir is inside a project with a
// manifest, otherwise nil (every stored library). Errors reading a manifest
// are reported so a corrupt file is not silently treated as "no project".
func ScopeFor(dir string) ([]string, error) {
	p, ok, err := FindProject(dir)
	if err != nil {
		return nil, err
	}
	if !ok || !p.Exists || len(p.Manifest.Libraries) == 0 {
		return nil, nil
	}
	return p.Names(), nil
}

// ErrNoProject reports an operation that needs a project when dir is in none.
var ErrNoProject = errors.New("not inside a project (no .ketch/docs.json or git repository found)")
