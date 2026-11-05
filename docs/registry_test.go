package docs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	config "github.com/1broseidon/ketch/internal/configbase"
)

type libraryFixture struct{}

func (libraryFixture) Search(context.Context, string, int) ([]Result, error) { return nil, nil }
func (libraryFixture) ResolveLibrary(context.Context, string, int) ([]LibraryMatch, error) {
	return nil, nil
}
func (libraryFixture) GetDocs(context.Context, string, string, int) ([]Result, error) {
	return nil, nil
}

// plainFixture is a provider without library operations.
type plainFixture struct{}

func (plainFixture) Search(context.Context, string, int) ([]Result, error) { return nil, nil }

func TestRegistryDiscoversOptionalLibraryInterface(t *testing.T) {
	previous := providers
	before := LibraryBackends()
	providers = append(Providers(), Provider{
		ID: "libraryfixture", Name: "Library Fixture",
		Usable: func(*config.Config) bool { return true },
		New:    func(*config.Config, Deps) (Searcher, error) { return libraryFixture{}, nil },
	}, Provider{
		ID: "plainfixture", Name: "Plain Fixture",
		Usable: func(*config.Config) bool { return true },
		New:    func(*config.Config, Deps) (Searcher, error) { return plainFixture{}, nil },
	})
	t.Cleanup(func() { providers = previous })
	if !slices.Equal(LibraryBackends(), append(before, "libraryfixture")) {
		t.Fatalf("library capability discovery = %v", LibraryBackends())
	}
	if ResolveBackend("libraryfixture") != "libraryfixture" || ResolveBackend("local") != "local" || ResolveBackend("plainfixture") != "context7" {
		t.Fatal("library selection or legacy resolve fallback changed")
	}
	client, err := NewFromConfig(&config.Config{}, "libraryfixture", Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.(LibraryResolver); !ok {
		t.Fatal("registry lost the optional library interface")
	}
}

func TestRegistryKeepsUnknownAndUnavailableDistinct(t *testing.T) {
	// Context7 is usable with no key (its anonymous quota), and the local
	// backend with nothing indexed (it refuses each query), so every
	// registered backend builds and only an unknown one is rejected.
	for _, backend := range []string{"context7", "local", "web", AutoBackend} {
		if _, err := NewFromConfig(&config.Config{}, backend, Deps{}); err != nil {
			t.Fatalf("%s must build with no keys, got %v", backend, err)
		}
	}
	if _, err := NewFromConfig(&config.Config{}, "unknown", Deps{}); !errors.Is(err, ErrUnknownBackend) {
		t.Fatalf("unknown error taxonomy = %v", err)
	}
}

// LibraryHelp is shared by the CLI help and the MCP descriptions: each
// library provider's LibraryHelp, labelled with its name.
func TestLibraryHelp(t *testing.T) {
	var parts []string
	for _, id := range LibraryBackends() {
		if p, _ := Lookup(id); p.LibraryHelp != "" {
			parts = append(parts, p.Name+": "+p.LibraryHelp)
		}
	}
	if len(parts) == 0 {
		t.Fatal("no library provider declares LibraryHelp; the web backend should")
	}
	want := strings.Join(parts, "; ")
	if got := LibraryHelp(); got != want {
		t.Errorf("LibraryHelp() = %q, want %q", got, want)
	}
	if got := WithLibraryHelp("a library ID"); got != "a library ID ("+want+")" {
		t.Errorf("WithLibraryHelp = %q", got)
	}
}
